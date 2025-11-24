package core

import (
	"testing"
)

// BenchmarkGetChainInput_Clear 测试使用 clear() 优化后的性能
func BenchmarkGetChainInput_Clear(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		input := GetChainInput()
		// 模拟使用
		input.Vars["key1"] = "value1"
		input.Vars["key2"] = "value2"
		input.Vars["key3"] = "value3"
		PutChainInput(input)
	}
}

// BenchmarkGetChainOutput_Clear 测试使用 clear() 优化后的性能
func BenchmarkGetChainOutput_Clear(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		output := GetChainOutput()
		// 模拟使用
		output.Metadata["key1"] = "value1"
		output.Metadata["key2"] = "value2"
		output.Metadata["key3"] = "value3"
		PutChainOutput(output)
	}
}
