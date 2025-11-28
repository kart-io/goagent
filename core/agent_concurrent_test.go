package core

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestAgentInputConcurrentAccess 测试 AgentInput.Context 的并发访问安全性
func TestAgentInputConcurrentAccess(t *testing.T) {
	input := &AgentInput{
		Context: make(map[string]interface{}),
	}

	const numGoroutines = 100
	const numOperations = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 3) // 读、写、删除三类操作

	// 并发写入
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := "key" + string(rune(id%10))
				input.SetContext(key, id*numOperations+j)
			}
		}(i)
	}

	// 并发读取
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := "key" + string(rune(id%10))
				_, _ = input.GetContext(key)
			}
		}(i)
	}

	// 并发删除
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := "key" + string(rune(id%10))
				input.DeleteContext(key)
			}
		}(i)
	}

	wg.Wait()
	// 测试通过说明没有发生竞态条件
}

// TestAgentInputGetSetContext 测试基本的 Get/Set 操作
func TestAgentInputGetSetContext(t *testing.T) {
	input := &AgentInput{
		Context: make(map[string]interface{}),
	}

	// 测试 SetContext
	input.SetContext("key1", "value1")
	input.SetContext("key2", 123)
	input.SetContext("key3", true)

	// 测试 GetContext
	val1, ok1 := input.GetContext("key1")
	assert.True(t, ok1)
	assert.Equal(t, "value1", val1)

	val2, ok2 := input.GetContext("key2")
	assert.True(t, ok2)
	assert.Equal(t, 123, val2)

	val3, ok3 := input.GetContext("key3")
	assert.True(t, ok3)
	assert.Equal(t, true, val3)

	// 测试不存在的键
	_, ok4 := input.GetContext("nonexistent")
	assert.False(t, ok4)
}

// TestAgentInputDeleteContext 测试删除操作
func TestAgentInputDeleteContext(t *testing.T) {
	input := &AgentInput{
		Context: make(map[string]interface{}),
	}

	input.SetContext("key1", "value1")
	input.SetContext("key2", "value2")

	// 删除 key1
	input.DeleteContext("key1")

	// 验证 key1 已删除
	_, ok1 := input.GetContext("key1")
	assert.False(t, ok1)

	// 验证 key2 仍然存在
	val2, ok2 := input.GetContext("key2")
	assert.True(t, ok2)
	assert.Equal(t, "value2", val2)
}

// TestAgentInputRangeContext 测试遍历操作
func TestAgentInputRangeContext(t *testing.T) {
	input := &AgentInput{
		Context: make(map[string]interface{}),
	}

	expected := map[string]interface{}{
		"key1": "value1",
		"key2": 123,
		"key3": true,
	}

	for k, v := range expected {
		input.SetContext(k, v)
	}

	// 测试完整遍历
	collected := make(map[string]interface{})
	input.RangeContext(func(key string, value interface{}) bool {
		collected[key] = value
		return true
	})

	assert.Equal(t, expected, collected)

	// 测试提前终止
	count := 0
	input.RangeContext(func(key string, value interface{}) bool {
		count++
		return count < 2 // 只遍历前两个
	})

	assert.Equal(t, 2, count)
}

// TestAgentInputCopyContext 测试复制操作
func TestAgentInputCopyContext(t *testing.T) {
	input := &AgentInput{
		Context: make(map[string]interface{}),
	}

	expected := map[string]interface{}{
		"key1": "value1",
		"key2": 123,
		"key3": true,
	}

	for k, v := range expected {
		input.SetContext(k, v)
	}

	// 测试复制
	dst := make(map[string]interface{})
	input.CopyContext(dst)

	assert.Equal(t, expected, dst)
}

// TestAgentInputConcurrentRangeAndModify 测试并发遍历和修改
func TestAgentInputConcurrentRangeAndModify(t *testing.T) {
	input := &AgentInput{
		Context: make(map[string]interface{}),
	}

	// 初始化数据
	for i := 0; i < 10; i++ {
		input.SetContext("key"+string(rune('0'+i)), i)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// 并发遍历
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			input.RangeContext(func(key string, value interface{}) bool {
				return true
			})
		}
	}()

	// 并发修改
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			input.SetContext("key"+string(rune('0'+i%10)), i*100)
		}
	}()

	wg.Wait()
	// 测试通过说明没有发生竞态条件
}

// TestAgentInputLockUnlock 测试手动加锁/解锁
func TestAgentInputLockUnlock(t *testing.T) {
	input := &AgentInput{
		Context: make(map[string]interface{}),
	}

	// 测试写锁
	input.LockContext()
	input.Context["key1"] = "value1"
	input.Context["key2"] = "value2"
	input.UnlockContext()

	val1, ok1 := input.GetContext("key1")
	assert.True(t, ok1)
	assert.Equal(t, "value1", val1)

	// 测试读锁
	input.RLockContext()
	_ = input.Context["key1"]
	_ = input.Context["key2"]
	input.RUnlockContext()
}

// TestAgentInputNilContext 测试 nil Context 的处理
func TestAgentInputNilContext(t *testing.T) {
	input := &AgentInput{
		Context: nil,
	}

	// SetContext 应该自动初始化 map
	input.SetContext("key1", "value1")
	assert.NotNil(t, input.Context)

	val, ok := input.GetContext("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)
}

// BenchmarkAgentInputConcurrentAccess 并发访问性能基准测试
func BenchmarkAgentInputConcurrentAccess(b *testing.B) {
	input := &AgentInput{
		Context: make(map[string]interface{}),
	}

	// 预填充数据
	for i := 0; i < 100; i++ {
		input.SetContext("key"+string(rune('0'+i%10)), i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := "key" + string(rune('0'+i%10))
			if i%3 == 0 {
				input.SetContext(key, i)
			} else {
				_, _ = input.GetContext(key)
			}
			i++
		}
	})
}

// BenchmarkAgentInputSequentialAccess 顺序访问性能基准测试
func BenchmarkAgentInputSequentialAccess(b *testing.B) {
	input := &AgentInput{
		Context: make(map[string]interface{}),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := "key" + string(rune('0'+i%10))
		if i%3 == 0 {
			input.SetContext(key, i)
		} else {
			_, _ = input.GetContext(key)
		}
	}
}

// TestAgentInputConcurrentStress 压力测试
func TestAgentInputConcurrentStress(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过压力测试")
	}

	input := &AgentInput{
		Context: make(map[string]interface{}),
	}

	const numGoroutines = 1000
	const duration = 2 * time.Second

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 启动多个并发 goroutine
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					key := "key" + string(rune('0'+id%10))
					switch id % 4 {
					case 0:
						input.SetContext(key, id)
					case 1:
						_, _ = input.GetContext(key)
					case 2:
						input.DeleteContext(key)
					case 3:
						input.RangeContext(func(k string, v interface{}) bool {
							return true
						})
					}
				}
			}
		}(i)
	}

	// 运行指定时间
	time.Sleep(duration)
	close(stop)
	wg.Wait()

	// 测试通过说明在高并发压力下没有发生竞态条件
}
