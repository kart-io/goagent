package performance

import (
	"fmt"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
)

// BenchmarkAgentInputWithoutPool 不使用池的基准测试
func BenchmarkAgentInputWithoutPool(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		input := &core.AgentInput{
			Task:        "test task",
			Instruction: "test instruction",
			Context:     make(map[string]interface{}, 8),
			SessionID:   "session-123",
			Timestamp:   time.Now(),
		}
		input.Context["key1"] = "value1"
		input.Context["key2"] = "value2"
		input.Context["key3"] = "value3"

		// 模拟使用
		_ = input.Task
		_ = input.Context

		// 不归还，等待 GC
	}
}

// BenchmarkAgentInputWithPool 使用池的基准测试
func BenchmarkAgentInputWithPool(b *testing.B) {
	pool := NewDataPools()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		input := pool.GetAgentInput()
		input.Task = "test task"
		input.Instruction = "test instruction"
		input.SessionID = "session-123"
		input.Timestamp = time.Now()
		input.Context["key1"] = "value1"
		input.Context["key2"] = "value2"
		input.Context["key3"] = "value3"

		// 模拟使用
		_ = input.Task
		_ = input.Context

		// 归还到池中
		pool.PutAgentInput(input)
	}
}

// BenchmarkAgentOutputWithoutPool 不使用池的基准测试
func BenchmarkAgentOutputWithoutPool(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		output := &core.AgentOutput{
			Result:  "test result",
			Status:  "success",
			Message: "test message",
			ReasoningSteps: []core.ReasoningStep{
				{Step: 1, Action: "action1", Description: "desc1", Result: "result1", Success: true},
				{Step: 2, Action: "action2", Description: "desc2", Result: "result2", Success: true},
				{Step: 3, Action: "action3", Description: "desc3", Result: "result3", Success: true},
			},
			ToolCalls: []core.ToolCall{
				{ToolName: "tool1", Success: true},
				{ToolName: "tool2", Success: true},
			},
			Metadata:  make(map[string]interface{}, 8),
			Timestamp: time.Now(),
			Latency:   100 * time.Millisecond,
		}
		output.Metadata["key1"] = "value1"
		output.Metadata["key2"] = "value2"

		// 模拟使用
		_ = output.Result
		_ = output.ReasoningSteps
		_ = output.ToolCalls

		// 不归还，等待 GC
	}
}

// BenchmarkAgentOutputWithPool 使用池的基准测试
func BenchmarkAgentOutputWithPool(b *testing.B) {
	pool := NewDataPools()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		output := pool.GetAgentOutput()
		output.Result = "test result"
		output.Status = "success"
		output.Message = "test message"

		// 使用底层数组复用（零分配）
		output.ReasoningSteps = append(output.ReasoningSteps,
			core.ReasoningStep{Step: 1, Action: "action1", Description: "desc1", Result: "result1", Success: true},
			core.ReasoningStep{Step: 2, Action: "action2", Description: "desc2", Result: "result2", Success: true},
			core.ReasoningStep{Step: 3, Action: "action3", Description: "desc3", Result: "result3", Success: true},
		)

		output.ToolCalls = append(output.ToolCalls,
			core.ToolCall{ToolName: "tool1", Success: true},
			core.ToolCall{ToolName: "tool2", Success: true},
		)

		output.Metadata["key1"] = "value1"
		output.Metadata["key2"] = "value2"
		output.Timestamp = time.Now()
		output.Latency = 100 * time.Millisecond

		// 模拟使用
		_ = output.Result
		_ = output.ReasoningSteps
		_ = output.ToolCalls

		// 归还到池中（重置切片长度）
		pool.PutAgentOutput(output)
	}
}

// BenchmarkConcurrentPoolUsage 并发池使用基准测试
func BenchmarkConcurrentPoolUsage(b *testing.B) {
	pool := NewDataPools()
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// 获取输入
			input := pool.GetAgentInput()
			input.Task = "concurrent task"
			input.Context["test"] = "value"

			// 获取输出
			output := pool.GetAgentOutput()
			output.Result = "concurrent result"
			output.ReasoningSteps = append(output.ReasoningSteps,
				core.ReasoningStep{Step: 1, Action: "test", Success: true},
			)

			// 模拟处理
			_ = input.Task
			_ = output.Result

			// 归还
			pool.PutAgentInput(input)
			pool.PutAgentOutput(output)
		}
	})
}

// BenchmarkSliceReuse 切片复用基准测试
func BenchmarkSliceReuseWithoutPool(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// 每次都创建新切片
		steps := make([]core.ReasoningStep, 0, 10)
		for j := 0; j < 5; j++ {
			steps = append(steps, core.ReasoningStep{
				Step:   j,
				Action: "action",
			})
		}
		_ = steps
	}
}

// BenchmarkSliceReuseWithPool 使用池的切片复用
func BenchmarkSliceReuseWithPool(b *testing.B) {
	pool := NewDataPools()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// 从池中获取，复用底层数组
		steps := pool.GetReasoningSlice()
		for j := 0; j < 5; j++ {
			steps = append(steps, core.ReasoningStep{
				Step:   j,
				Action: "action",
			})
		}
		_ = steps

		// 归还到池中（使用 [:0] 重置）
		pool.PutReasoningSlice(steps)
	}
}

// BenchmarkMapReuse map 复用基准测试
func BenchmarkMapReuseWithoutPool(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// 每次都创建新 map
		m := make(map[string]interface{}, 8)
		m["key1"] = "value1"
		m["key2"] = "value2"
		m["key3"] = "value3"
		m["key4"] = "value4"
		_ = m
	}
}

// BenchmarkMapReuseWithPool 使用池的 map 复用
func BenchmarkMapReuseWithPool(b *testing.B) {
	pool := NewDataPools()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// 从池中获取，复用底层存储
		m := pool.GetContextMap()
		m["key1"] = "value1"
		m["key2"] = "value2"
		m["key3"] = "value3"
		m["key4"] = "value4"
		_ = m

		// 归还到池中（清空但保留容量）
		pool.PutContextMap(m)
	}
}

// BenchmarkComplexWorkflow 复杂工作流基准测试
func BenchmarkComplexWorkflowWithoutPool(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// 创建输入
		input := &core.AgentInput{
			Task:        "complex task",
			Instruction: "detailed instruction",
			Context:     make(map[string]interface{}, 8),
			SessionID:   "session-456",
			Timestamp:   time.Now(),
		}
		input.Context["param1"] = "value1"
		input.Context["param2"] = "value2"

		// 处理并创建输出
		output := &core.AgentOutput{
			Result:  "complex result",
			Status:  "success",
			Message: "completed successfully",
			ReasoningSteps: []core.ReasoningStep{
				{Step: 1, Action: "analyze", Result: "analyzed", Success: true},
				{Step: 2, Action: "process", Result: "processed", Success: true},
				{Step: 3, Action: "verify", Result: "verified", Success: true},
			},
			ToolCalls: []core.ToolCall{
				{ToolName: "analyzer", Success: true},
				{ToolName: "processor", Success: true},
			},
			Metadata:  make(map[string]interface{}, 8),
			Timestamp: time.Now(),
			Latency:   200 * time.Millisecond,
		}
		output.Metadata["result_type"] = "complex"

		// 模拟使用
		_ = input.Task
		_ = output.Result

		// 等待 GC
	}
}

// BenchmarkComplexWorkflowWithPool 使用池的复杂工作流
func BenchmarkComplexWorkflowWithPool(b *testing.B) {
	pool := NewDataPools()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// 从池中获取输入
		input := pool.GetAgentInput()
		input.Task = "complex task"
		input.Instruction = "detailed instruction"
		input.SessionID = "session-456"
		input.Timestamp = time.Now()
		input.Context["param1"] = "value1"
		input.Context["param2"] = "value2"

		// 从池中获取输出
		output := pool.GetAgentOutput()
		output.Result = "complex result"
		output.Status = "success"
		output.Message = "completed successfully"

		// 复用切片底层数组
		output.ReasoningSteps = append(output.ReasoningSteps,
			core.ReasoningStep{Step: 1, Action: "analyze", Result: "analyzed", Success: true},
			core.ReasoningStep{Step: 2, Action: "process", Result: "processed", Success: true},
			core.ReasoningStep{Step: 3, Action: "verify", Result: "verified", Success: true},
		)

		output.ToolCalls = append(output.ToolCalls,
			core.ToolCall{ToolName: "analyzer", Success: true},
			core.ToolCall{ToolName: "processor", Success: true},
		)

		output.Metadata["result_type"] = "complex"
		output.Timestamp = time.Now()
		output.Latency = 200 * time.Millisecond

		// 模拟使用
		_ = input.Task
		_ = output.Result

		// 归还到池中
		pool.PutAgentInput(input)
		pool.PutAgentOutput(output)
	}
}

// 单元测试

func TestDataPoolsGetPut(t *testing.T) {
	pool := NewDataPools()

	// 测试 AgentInput
	input := pool.GetAgentInput()
	if input == nil {
		t.Fatal("GetAgentInput returned nil")
	}
	input.Task = "test"
	input.Context["key"] = "value"
	pool.PutAgentInput(input)

	// 验证重置
	input2 := pool.GetAgentInput()
	if input2.Task != "" {
		t.Error("AgentInput not properly reset")
	}
	if len(input2.Context) != 0 {
		t.Error("Context not properly cleared")
	}

	// 测试 AgentOutput
	output := pool.GetAgentOutput()
	if output == nil {
		t.Fatal("GetAgentOutput returned nil")
	}
	output.Result = "result"
	output.ReasoningSteps = append(output.ReasoningSteps, core.ReasoningStep{Step: 1})
	pool.PutAgentOutput(output)

	// 验证切片零分配
	output2 := pool.GetAgentOutput()
	if len(output2.ReasoningSteps) != 0 {
		t.Error("ReasoningSteps not properly reset to zero length")
	}
	if cap(output2.ReasoningSteps) == 0 {
		t.Error("ReasoningSteps capacity should be preserved")
	}
}

func TestPooledAgentInput(t *testing.T) {
	pool := NewDataPools()

	input := NewPooledAgentInput(pool)
	defer input.Release()

	if input.Input == nil {
		t.Fatal("PooledAgentInput.Input is nil")
	}

	input.Input.Task = "test task"
	if input.Input.Task != "test task" {
		t.Error("Failed to set task")
	}
}

func TestPooledAgentOutput(t *testing.T) {
	pool := NewDataPools()

	output := NewPooledAgentOutput(pool)
	defer output.Release()

	if output.Output == nil {
		t.Fatal("PooledAgentOutput.Output is nil")
	}

	output.Output.Result = "test result"
	if output.Output.Result != "test result" {
		t.Error("Failed to set result")
	}
}

func TestCloneAgentInput(t *testing.T) {
	pool := NewDataPools()

	src := pool.GetAgentInput()
	src.Task = "original task"
	src.Context["key"] = "value"

	dst := CloneAgentInput(src, pool)

	if dst.Task != src.Task {
		t.Error("CloneAgentInput failed to copy Task")
	}
	if dst.Context["key"] != src.Context["key"] {
		t.Error("CloneAgentInput failed to copy Context")
	}

	// 修改源不应影响目标
	src.Task = "modified"
	if dst.Task == src.Task {
		t.Error("Clone should be independent")
	}

	pool.PutAgentInput(src)
	pool.PutAgentInput(dst)
}

func TestCloneAgentOutput(t *testing.T) {
	pool := NewDataPools()

	src := pool.GetAgentOutput()
	src.Result = "original result"
	src.ReasoningSteps = append(src.ReasoningSteps, core.ReasoningStep{Step: 1})

	dst := CloneAgentOutput(src, pool)

	if dst.Result != src.Result {
		t.Error("CloneAgentOutput failed to copy Result")
	}
	if len(dst.ReasoningSteps) != len(src.ReasoningSteps) {
		t.Error("CloneAgentOutput failed to copy ReasoningSteps")
	}

	// 修改源不应影响目标
	src.Result = "modified"
	if dst.Result == src.Result {
		t.Error("Clone should be independent")
	}

	pool.PutAgentOutput(src)
	pool.PutAgentOutput(dst)
}

func TestDataPoolStats(t *testing.T) {
	pool := NewDataPools()

	// 执行一些操作
	input := pool.GetAgentInput()
	pool.PutAgentInput(input)

	output := pool.GetAgentOutput()
	pool.PutAgentOutput(output)

	stats := pool.GetStats()

	if stats.InputGetCount != 1 {
		t.Errorf("Expected InputGetCount=1, got %d", stats.InputGetCount)
	}
	if stats.InputPutCount != 1 {
		t.Errorf("Expected InputPutCount=1, got %d", stats.InputPutCount)
	}
	if stats.OutputGetCount != 1 {
		t.Errorf("Expected OutputGetCount=1, got %d", stats.OutputGetCount)
	}
	if stats.OutputPutCount != 1 {
		t.Errorf("Expected OutputPutCount=1, got %d", stats.OutputPutCount)
	}
}

// 边界条件测试

func TestDataPool_OversizedSliceCapacity(t *testing.T) {
	pool := NewDataPools()

	// 创建超大容量的切片
	output := pool.GetAgentOutput()

	// 添加超过限制的元素
	for i := 0; i < 150; i++ { // 超过 maxReasoningStepsCapacity (100)
		output.ReasoningSteps = append(output.ReasoningSteps, core.ReasoningStep{Step: i})
	}

	// 归还（应该被拒绝，不放回池中）
	pool.PutAgentOutput(output)

	// 再次获取，应该是新对象
	output2 := pool.GetAgentOutput()
	if cap(output2.ReasoningSteps) > 20 {
		t.Error("Expected new output with normal capacity, got oversized capacity")
	}
}

func TestDataPool_OversizedMap(t *testing.T) {
	pool := NewDataPools()

	// 创建超大 map
	m := pool.GetContextMap()
	for i := 0; i < 50; i++ { // 超过 maxContextMapSize (32)
		m[fmt.Sprintf("key%d", i)] = i
	}

	// 归还（应该被拒绝）
	pool.PutContextMap(m)

	// 再次获取，应该是新的空 map
	m2 := pool.GetContextMap()
	if len(m2) != 0 {
		t.Error("Expected empty map, got map with entries")
	}
}

func TestDataPool_ToolCallSliceCapacity(t *testing.T) {
	pool := NewDataPools()

	// 测试 ToolCall 切片容量限制
	s := pool.GetToolCallSlice()

	// 添加超过限制的元素
	for i := 0; i < 60; i++ { // 超过 maxToolCallsCapacity (50)
		s = append(s, core.ToolCall{ToolName: fmt.Sprintf("tool%d", i)})
	}

	// 归还（应该被拒绝）
	pool.PutToolCallSlice(s)

	// 再次获取，应该是新切片
	s2 := pool.GetToolCallSlice()
	if cap(s2) > 10 {
		t.Error("Expected new slice with normal capacity")
	}
}

func TestDataPool_NilHandling(t *testing.T) {
	pool := NewDataPools()

	// 测试所有 Put 方法的 nil 处理
	pool.PutAgentInput(nil)
	pool.PutAgentOutput(nil)
	pool.PutReasoningStep(nil)
	pool.PutToolCall(nil)
	pool.PutContextMap(nil)
	pool.PutMetadataMap(nil)
	pool.PutStringSlice(nil)
	pool.PutReasoningSlice(nil)
	pool.PutToolCallSlice(nil)

	// 不应该 panic
	t.Log("All nil handling passed")
}

func TestDataPool_DataCleanup(t *testing.T) {
	pool := NewDataPools()

	// 获取并填充数据
	input := pool.GetAgentInput()
	input.Task = "sensitive task"
	input.Context["password"] = "secret123"
	input.Context["token"] = "abc-xyz"

	// 归还
	pool.PutAgentInput(input)

	// 再次获取，数据应该被清空
	input2 := pool.GetAgentInput()
	if input2.Task != "" {
		t.Error("Task not cleaned")
	}
	if len(input2.Context) != 0 {
		t.Error("Context not cleaned")
	}
}

func TestDataPool_SliceZeroAllocation(t *testing.T) {
	pool := NewDataPools()

	output := pool.GetAgentOutput()

	// 添加一些元素
	for i := 0; i < 5; i++ {
		output.ReasoningSteps = append(output.ReasoningSteps, core.ReasoningStep{Step: i})
	}

	originalCap := cap(output.ReasoningSteps)

	// 归还
	pool.PutAgentOutput(output)

	// 再次获取
	output2 := pool.GetAgentOutput()

	// 长度应该是 0
	if len(output2.ReasoningSteps) != 0 {
		t.Errorf("Expected length 0, got %d", len(output2.ReasoningSteps))
	}

	// 容量应该保留
	if cap(output2.ReasoningSteps) < originalCap {
		t.Error("Capacity not preserved (zero allocation failed)")
	}
}

func TestDataPool_ConcurrentStress(t *testing.T) {
	pool := NewDataPools()
	iterations := 1000
	goroutines := 50

	done := make(chan bool, goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer func() { done <- true }()

			for i := 0; i < iterations; i++ {
				// 交替使用不同类型的对象
				if i%2 == 0 {
					input := pool.GetAgentInput()
					input.Task = "test"
					input.Context["key"] = "value"
					pool.PutAgentInput(input)
				} else {
					output := pool.GetAgentOutput()
					output.Result = "result"
					output.ReasoningSteps = append(output.ReasoningSteps,
						core.ReasoningStep{Step: 1})
					pool.PutAgentOutput(output)
				}
			}
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < goroutines; i++ {
		<-done
	}

	// 检查统计
	stats := pool.GetStats()
	expectedOps := int64(goroutines * iterations / 2)

	if stats.InputGetCount < expectedOps {
		t.Errorf("Input get count too low: %d < %d", stats.InputGetCount, expectedOps)
	}
	if stats.OutputGetCount < expectedOps {
		t.Errorf("Output get count too low: %d < %d", stats.OutputGetCount, expectedOps)
	}
}

func TestDataPool_MapSizeProtection(t *testing.T) {
	pool := NewDataPools()

	// 测试 Context map
	input := pool.GetAgentInput()
	for i := 0; i < 40; i++ { // 超过 maxContextMapSize
		input.Context[fmt.Sprintf("key%d", i)] = i
	}

	initialLen := len(input.Context)

	// 归还
	pool.PutAgentInput(input)

	// 再次获取
	input2 := pool.GetAgentInput()

	// 应该是新的小 map
	if len(input2.Context) != 0 {
		t.Error("Map should be empty after oversized put")
	}

	t.Logf("Map size protection working: %d -> %d", initialLen, len(input2.Context))
}
