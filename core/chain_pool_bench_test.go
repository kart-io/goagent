package core

import (
	"testing"
)

// BenchmarkGetChainInput 基准测试对象池的性能
func BenchmarkGetChainInput(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		input := GetChainInput()
		// 模拟使用
		input.Data = "test"
		input.Vars["key"] = "value"
		input.Options.Extra["extra"] = "data"
		PutChainInput(input)
	}
}

// BenchmarkGetChainInputParallel 并发场景下的基准测试
func BenchmarkGetChainInputParallel(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			input := GetChainInput()
			// 模拟使用
			input.Data = "test"
			input.Vars["key"] = "value"
			input.Options.Extra["extra"] = "data"
			PutChainInput(input)
		}
	})
}

// BenchmarkGetChainOutput 基准测试 ChainOutput 对象池
func BenchmarkGetChainOutput(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		output := GetChainOutput()
		// 模拟使用
		output.Data = "result"
		output.Status = "success"
		output.Metadata["key"] = "value"
		PutChainOutput(output)
	}
}

// BenchmarkChainInputWithoutPool 对比：不使用对象池的版本
func BenchmarkChainInputWithoutPool(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		input := &ChainInput{
			Vars: make(map[string]interface{}, 8),
			Options: ChainOptions{
				StopOnError: true,
				Extra:       make(map[string]interface{}, 4),
			},
		}
		// 模拟使用
		input.Data = "test"
		input.Vars["key"] = "value"
		input.Options.Extra["extra"] = "data"
		// 不放回池中，让 GC 回收
		_ = input
	}
}

// BenchmarkChainInputReuse 测试对象复用场景
func BenchmarkChainInputReuse(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		input := GetChainInput()
		// 第一次使用
		input.Data = "test1"
		input.Vars["key1"] = "value1"
		input.Vars["key2"] = "value2"
		input.Options.Extra["extra1"] = "data1"
		input.Options.Extra["extra2"] = "data2"

		// 放回池中
		PutChainInput(input)

		// 再次获取（应该是同一个对象被重用）
		input2 := GetChainInput()
		input2.Data = "test2"
		input2.Vars["key3"] = "value3"
		input2.Options.Extra["extra3"] = "data3"
		PutChainInput(input2)
	}
}
