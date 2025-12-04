package stream

import (
	"sync"

	"github.com/kart-io/goagent/core"
)

// chunkPool 是 LegacyStreamChunk 的对象池
// 使用 sync.Pool 复用对象，减少 GC 压力和内存分配
var chunkPool = sync.Pool{
	New: func() interface{} {
		return &core.LegacyStreamChunk{
			Metadata: core.ChunkMetadata{
				Extra: make(map[string]interface{}),
			},
		}
	},
}

// GetChunk 从对象池中获取一个 chunk
// 使用完毕后应调用 PutChunk 归还到池中
func GetChunk() *core.LegacyStreamChunk {
	chunk := chunkPool.Get().(*core.LegacyStreamChunk)
	return chunk
}

// PutChunk 将 chunk 归还到对象池
// 在归还前会重置 chunk 的状态，避免数据泄漏
func PutChunk(chunk *core.LegacyStreamChunk) {
	if chunk == nil {
		return
	}

	// 重置所有字段
	chunk.Type = ""
	chunk.Data = nil
	chunk.Text = ""
	chunk.IsLast = false
	chunk.Error = nil

	// 重置元数据
	chunk.Metadata = core.ChunkMetadata{
		Extra: make(map[string]interface{}),
	}

	// 归还到池中
	chunkPool.Put(chunk)
}

// streamChunkPool 是通用 StreamChunk 的对象池
var streamChunkPool = sync.Pool{
	New: func() interface{} {
		return &StreamChunk{
			Metadata: make(map[string]interface{}),
		}
	},
}

// GetStreamChunk 从对象池中获取一个 StreamChunk
func GetStreamChunk() *StreamChunk {
	return streamChunkPool.Get().(*StreamChunk)
}

// PutStreamChunk 将 StreamChunk 归还到对象池
func PutStreamChunk(chunk *StreamChunk) {
	if chunk == nil {
		return
	}

	// 重置所有字段
	chunk.Data = nil
	chunk.ChunkID = 0
	chunk.Done = false
	chunk.Error = nil

	// 清空元数据 map（保留容量）
	for k := range chunk.Metadata {
		delete(chunk.Metadata, k)
	}

	// 归还到池中
	streamChunkPool.Put(chunk)
}
