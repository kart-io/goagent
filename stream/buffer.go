package stream

import (
	"sync"

	"github.com/kart-io/goagent/core"
)

// RingBuffer 环形缓冲区实现
//
// RingBuffer 提供高效的固定大小缓冲：
// - O(1) 读写操作
// - 自动覆盖旧数据
// - 线程安全
// - 零内存分配（预分配）
type RingBuffer struct {
	buffer []*core.LegacyStreamChunk
	size   int
	head   int
	tail   int
	count  int
	mu     sync.RWMutex
}

// NewRingBuffer 创建新的环形缓冲区
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = 100
	}

	return &RingBuffer{
		buffer: make([]*core.LegacyStreamChunk, size),
		size:   size,
	}
}

// Push 添加元素到缓冲区
func (rb *RingBuffer) Push(chunk *core.LegacyStreamChunk) bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	// 写入当前 tail 位置
	rb.buffer[rb.tail] = chunk
	rb.tail = (rb.tail + 1) % rb.size

	// 如果缓冲区已满，移动 head
	if rb.count == rb.size {
		rb.head = (rb.head + 1) % rb.size
		return false // 表示覆盖了旧数据
	}

	rb.count++
	return true
}

// Pop 从缓冲区弹出元素
func (rb *RingBuffer) Pop() *core.LegacyStreamChunk {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.count == 0 {
		return nil
	}

	chunk := rb.buffer[rb.head]
	rb.buffer[rb.head] = nil // 释放引用
	rb.head = (rb.head + 1) % rb.size
	rb.count--

	return chunk
}

// Peek 查看第一个元素但不移除
func (rb *RingBuffer) Peek() *core.LegacyStreamChunk {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.count == 0 {
		return nil
	}

	return rb.buffer[rb.head]
}

// IsEmpty 检查缓冲区是否为空
func (rb *RingBuffer) IsEmpty() bool {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.count == 0
}

// IsFull 检查缓冲区是否已满
func (rb *RingBuffer) IsFull() bool {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.count == rb.size
}

// Count 返回当前元素数量
func (rb *RingBuffer) Count() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.count
}

// Size 返回缓冲区大小
func (rb *RingBuffer) Size() int {
	return rb.size
}

// Clear 清空缓冲区
func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	// 释放所有引用
	for i := 0; i < rb.size; i++ {
		rb.buffer[i] = nil
	}

	rb.head = 0
	rb.tail = 0
	rb.count = 0
}

// ToSlice 转换为切片
func (rb *RingBuffer) ToSlice() []*core.LegacyStreamChunk {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.count == 0 {
		return nil
	}

	result := make([]*core.LegacyStreamChunk, rb.count)
	idx := rb.head

	for i := 0; i < rb.count; i++ {
		result[i] = rb.buffer[idx]
		idx = (idx + 1) % rb.size
	}

	return result
}

// Resize 调整缓冲区大小
func (rb *RingBuffer) Resize(newSize int) {
	if newSize <= 0 || newSize == rb.size {
		return
	}

	rb.mu.Lock()
	defer rb.mu.Unlock()

	// 保存现有数据
	oldData := make([]*core.LegacyStreamChunk, rb.count)
	idx := rb.head
	for i := 0; i < rb.count; i++ {
		oldData[i] = rb.buffer[idx]
		idx = (idx + 1) % rb.size
	}

	// 创建新缓冲区
	rb.buffer = make([]*core.LegacyStreamChunk, newSize)
	rb.size = newSize
	rb.head = 0
	rb.count = 0

	// 恢复数据（如果新缓冲区够大）
	for i := 0; i < len(oldData) && i < newSize; i++ {
		rb.buffer[i] = oldData[i]
		rb.count++
	}

	rb.tail = rb.count % newSize
}

// Usage 返回缓冲区使用率 (0-1)
func (rb *RingBuffer) Usage() float64 {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.size == 0 {
		return 0
	}

	return float64(rb.count) / float64(rb.size)
}
