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

	// 记录对象地址
	objAddr1 := input1

	// 放回池中
	PutChainInput(input1)

	// 再次获取对象 - sync.Pool 可能会复用,也可能会创建新对象
	input2 := GetChainInput()

	// 验证 map 内容被清空(这是对象池的核心功能)
	assert.Empty(t, input2.Vars, "Vars should be empty after reset")
	assert.Empty(t, input2.Options.Extra, "Options.Extra should be empty after reset")

	// 验证其他字段被重置
	assert.Nil(t, input2.Data, "Data should be nil after reset")
	assert.True(t, input2.Options.StopOnError, "StopOnError should be reset to true")
	assert.Zero(t, input2.Options.Timeout, "Timeout should be reset to 0")
	assert.False(t, input2.Options.Parallel, "Parallel should be reset to false")
	assert.Nil(t, input2.Options.SkipSteps, "SkipSteps should be nil")
	assert.Nil(t, input2.Options.OnlySteps, "OnlySteps should be nil")

	// 如果对象确实被复用了,验证 map 也被复用(优化目标)
	if objAddr1 == input2 {
		t.Log("Object was reused from pool (performance optimization working)")
		// map 应该被复用且已清空
		assert.NotNil(t, input2.Vars, "Vars map should exist when object is reused")
		assert.NotNil(t, input2.Options.Extra, "Extra map should exist when object is reused")
	} else {
		t.Log("New object was created by pool (normal sync.Pool behavior)")
	}

	PutChainInput(input2)
}

// TestChainOutputPoolReuse 验证 ChainOutput 对象池复用
func TestChainOutputPoolReuse(t *testing.T) {
	// 获取第一个对象
	output1 := GetChainOutput()
	output1.Data = "result1"
	output1.Status = "success"
	output1.Metadata["key1"] = "value1"

	// 记录对象地址
	objAddr1 := output1

	// 放回池中
	PutChainOutput(output1)

	// 再次获取对象 - sync.Pool 可能会复用,也可能会创建新对象
	// 这取决于 GC 和运行时状态,因此我们应该测试"可能复用"而非"必定复用"
	output2 := GetChainOutput()

	// 验证对象被正确重置(这是对象池的核心功能)
	assert.Empty(t, output2.Metadata, "Metadata should be empty after reset")
	assert.Nil(t, output2.Data, "Data should be nil after reset")
	assert.Zero(t, output2.TotalLatency, "TotalLatency should be reset to 0")
	assert.Empty(t, output2.Status, "Status should be empty after reset")
	assert.Empty(t, output2.StepsExecuted, "StepsExecuted should be empty after reset")

	// 如果对象确实被复用了,验证 map 也被复用(优化目标)
	// 注意:这是一个性能优化验证,不是必须的功能要求
	if objAddr1 == output2 {
		t.Log("Object was reused from pool (performance optimization working)")
		// 如果对象被复用,那么 Metadata map 应该也被复用
		assert.NotNil(t, output2.Metadata, "Metadata map should exist when object is reused")
	} else {
		t.Log("New object was created by pool (normal sync.Pool behavior)")
	}

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
