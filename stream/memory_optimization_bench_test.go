package stream

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/core"
)

// BenchmarkChunkPool_GetPut 测试对象池的性能
func BenchmarkChunkPool_GetPut(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			chunk := GetChunk()
			chunk.Type = core.ChunkTypeText
			chunk.Text = "test data"
			PutChunk(chunk)
		}
	})
}

// BenchmarkChunkPool_NoPool 对比不使用对象池的性能
func BenchmarkChunkPool_NoPool(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			chunk := &core.LegacyStreamChunk{
				Metadata: core.ChunkMetadata{
					Extra: make(map[string]interface{}),
				},
			}
			chunk.Type = core.ChunkTypeText
			chunk.Text = "test data"
			// 不归还到池中
			_ = chunk
		}
	})
}

// BenchmarkReader_Collect_PreAlloc 测试预分配的 Collect 性能
func BenchmarkReader_Collect_PreAlloc(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// 准备测试数据
		ch := make(chan *core.LegacyStreamChunk, 101)

		// 在后台发送数据
		go func() {
			for j := 0; j < 100; j++ {
				ch <- core.NewTextChunk("test data")
			}
			lastChunk := core.NewTextChunk("final")
			lastChunk.IsLast = true
			ch <- lastChunk
		}()

		opts := core.DefaultStreamOptions()
		reader := NewReader(context.Background(), ch, opts)
		b.StartTimer()

		// 执行收集
		chunks, err := reader.Collect()
		if err != nil {
			b.Fatal(err)
		}
		if len(chunks) != 101 {
			b.Fatalf("expected 101 chunks, got %d", len(chunks))
		}

		b.StopTimer()
		_ = reader.Close()
	}
}

// BenchmarkCollectText_Builder 测试使用 strings.Builder 的性能
func BenchmarkCollectText_Builder(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// 准备测试数据
		ch := make(chan *core.LegacyStreamChunk, 101)

		// 在后台发送数据
		go func() {
			for j := 0; j < 100; j++ {
				ch <- core.NewTextChunk("test data ")
			}
			lastChunk := core.NewTextChunk("")
			lastChunk.IsLast = true
			ch <- lastChunk
		}()

		opts := core.DefaultStreamOptions()
		reader := NewReader(context.Background(), ch, opts)
		b.StartTimer()

		// 执行文本收集
		text, err := reader.CollectText()
		if err != nil {
			b.Fatal(err)
		}
		if len(text) != 1000 { // 100 * "test data " = 1000 chars
			b.Fatalf("expected 1000 chars, got %d", len(text))
		}

		b.StopTimer()
		_ = reader.Close()
	}
}

// BenchmarkRingBuffer_ToSlice 测试 RingBuffer ToSlice 的性能
func BenchmarkRingBuffer_ToSlice(b *testing.B) {
	b.ReportAllocs()

	// 准备一个填充了数据的 RingBuffer
	rb := NewRingBuffer(100)
	for i := 0; i < 100; i++ {
		rb.Push(core.NewTextChunk("test data"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		slice := rb.ToSlice()
		if len(slice) != 100 {
			b.Fatalf("expected 100 items, got %d", len(slice))
		}
	}
}

// BenchmarkStreamChunkPool_GetPut 测试 StreamChunk 对象池
func BenchmarkStreamChunkPool_GetPut(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			chunk := GetStreamChunk()
			chunk.Data = "test data"
			chunk.Metadata["key"] = "value"
			PutStreamChunk(chunk)
		}
	})
}

// BenchmarkMemoryEfficiency_SmallStream 测试小数据流的内存效率
func BenchmarkMemoryEfficiency_SmallStream(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		ch := make(chan *core.LegacyStreamChunk, 11)

		go func() {
			for j := 0; j < 10; j++ {
				ch <- core.NewTextChunk("small")
			}
			lastChunk := core.NewTextChunk("")
			lastChunk.IsLast = true
			ch <- lastChunk
		}()

		opts := core.DefaultStreamOptions()
		reader := NewReader(context.Background(), ch, opts)
		b.StartTimer()

		_, err := reader.Collect()
		if err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		_ = reader.Close()
	}
}

// BenchmarkMemoryEfficiency_LargeStream 测试大数据流的内存效率
func BenchmarkMemoryEfficiency_LargeStream(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		ch := make(chan *core.LegacyStreamChunk, 1001)

		go func() {
			for j := 0; j < 1000; j++ {
				ch <- core.NewTextChunk("test data chunk")
			}
			lastChunk := core.NewTextChunk("")
			lastChunk.IsLast = true
			ch <- lastChunk
		}()

		opts := core.DefaultStreamOptions()
		reader := NewReader(context.Background(), ch, opts)
		b.StartTimer()

		_, err := reader.Collect()
		if err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		_ = reader.Close()
	}
}
