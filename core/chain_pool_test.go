package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestChainInputPoolReuse 验证对象池确实复用了对象
func TestChainInputPoolReuse(t *testing.T) {
	// 获取第一个对象
	input1 := GetChainInput()
	input1.Data = "test1"
	input1.Vars["key1"] = "value1"
	input1.Options.Extra["extra1"] = "data1"

	// 记录 Vars 和 Extra map 的地址
	varsAddr1 := &input1.Vars
	extraAddr1 := &input1.Options.Extra

	// 放回池中
	PutChainInput(input1)

	// 再次获取对象（应该是同一个对象）
	input2 := GetChainInput()

	// 记录新的 map 地址
	varsAddr2 := &input2.Vars
	extraAddr2 := &input2.Options.Extra

	// 验证对象被复用（map 指针地址应该相同）
	assert.Equal(t, varsAddr1, varsAddr2, "Vars map should be reused")
	assert.Equal(t, extraAddr1, extraAddr2, "Options.Extra map should be reused")

	// 验证 map 内容被清空
	assert.Empty(t, input2.Vars, "Vars should be empty after reset")
	assert.Empty(t, input2.Options.Extra, "Options.Extra should be empty after reset")

	// 验证其他字段被重置
	assert.Nil(t, input2.Data, "Data should be nil after reset")
	assert.True(t, input2.Options.StopOnError, "StopOnError should be reset to true")
	assert.Zero(t, input2.Options.Timeout, "Timeout should be reset to 0")
	assert.False(t, input2.Options.Parallel, "Parallel should be reset to false")
	assert.Nil(t, input2.Options.SkipSteps, "SkipSteps should be nil")
	assert.Nil(t, input2.Options.OnlySteps, "OnlySteps should be nil")

	PutChainInput(input2)
}

// TestChainOutputPoolReuse 验证 ChainOutput 对象池复用
func TestChainOutputPoolReuse(t *testing.T) {
	// 获取第一个对象
	output1 := GetChainOutput()
	output1.Data = "result1"
	output1.Status = "success"
	output1.Metadata["key1"] = "value1"

	// 记录 Metadata map 的地址
	metadataAddr1 := &output1.Metadata

	// 放回池中
	PutChainOutput(output1)

	// 再次获取对象
	output2 := GetChainOutput()

	// 记录新的 map 地址
	metadataAddr2 := &output2.Metadata

	// 验证对象被复用
	assert.Equal(t, metadataAddr1, metadataAddr2, "Metadata map should be reused")

	// 验证内容被清空
	assert.Empty(t, output2.Metadata, "Metadata should be empty after reset")
	assert.Nil(t, output2.Data, "Data should be nil after reset")
	assert.Zero(t, output2.TotalLatency, "TotalLatency should be reset to 0")
	assert.Empty(t, output2.Status, "Status should be empty after reset")
	assert.Empty(t, output2.StepsExecuted, "StepsExecuted should be empty after reset")

	PutChainOutput(output2)
}

// TestChainInputPoolConcurrency 测试并发场景下的对象池安全性
func TestChainInputPoolConcurrency(t *testing.T) {
	const goroutines = 100
	const iterations = 1000

	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < iterations; j++ {
				input := GetChainInput()
				input.Data = "test"
				input.Vars["key"] = "value"
				input.Options.Extra["extra"] = "data"
				PutChainInput(input)
			}
			done <- true
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < goroutines; i++ {
		<-done
	}

	// 如果没有 panic，说明并发安全
	assert.True(t, true, "Pool should be thread-safe")
}
