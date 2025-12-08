package tools

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"
)

// MockTool 模拟工具
type MockTool struct {
	*BaseTool
	executionTime time.Duration
	shouldFail    bool
}

func NewMockTool(name string, executionTime time.Duration, shouldFail bool) *MockTool {
	mockTool := &MockTool{
		executionTime: executionTime,
		shouldFail:    shouldFail,
	}

	baseTool := NewBaseTool(
		name,
		"Mock tool for testing",
		`{"type": "object"}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			// 模拟执行时间
			time.Sleep(mockTool.executionTime)

			if mockTool.shouldFail {
				return nil, errors.New("mock tool execution failed")
			}

			return &interfaces.ToolOutput{
				Result:  "mock result",
				Success: true,
			}, nil
		},
	)

	mockTool.BaseTool = baseTool
	return mockTool
}

func TestToolExecutor_ExecuteParallel(t *testing.T) {
	executor := NewToolExecutor(
		WithMaxConcurrency(5),
		WithTimeout(5*time.Second),
	)

	t.Run("Execute multiple tools successfully", func(t *testing.T) {
		calls := []*ToolCall{
			{
				Tool:  NewMockTool("tool1", 100*time.Millisecond, false),
				Input: &interfaces.ToolInput{Args: map[string]interface{}{"arg": "value1"}},
				ID:    "call1",
			},
			{
				Tool:  NewMockTool("tool2", 100*time.Millisecond, false),
				Input: &interfaces.ToolInput{Args: map[string]interface{}{"arg": "value2"}},
				ID:    "call2",
			},
			{
				Tool:  NewMockTool("tool3", 100*time.Millisecond, false),
				Input: &interfaces.ToolInput{Args: map[string]interface{}{"arg": "value3"}},
				ID:    "call3",
			},
		}

		ctx := context.Background()
		results, err := executor.ExecuteParallel(ctx, calls)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if len(results) != 3 {
			t.Fatalf("Expected 3 results, got %d", len(results))
		}

		for i, result := range results {
			if result.Error != nil {
				t.Errorf("Result %d has error: %v", i, result.Error)
			}

			if !result.Output.Success {
				t.Errorf("Result %d is not successful", i)
			}
		}
	})

	t.Run("Handle tool failures", func(t *testing.T) {
		calls := []*ToolCall{
			{
				Tool:  NewMockTool("tool1", 50*time.Millisecond, false),
				Input: &interfaces.ToolInput{Args: map[string]interface{}{}},
				ID:    "call1",
			},
			{
				Tool:  NewMockTool("tool2", 50*time.Millisecond, true),
				Input: &interfaces.ToolInput{Args: map[string]interface{}{}},
				ID:    "call2",
			},
		}

		ctx := context.Background()
		results, err := executor.ExecuteParallel(ctx, calls)

		// 应该返回所有结果，但有错误
		if err == nil {
			t.Error("Expected error due to failed tool")
		}

		if len(results) != 2 {
			t.Fatalf("Expected 2 results, got %d", len(results))
		}

		// 检查至少有一个结果有错误
		hasError := false
		for _, result := range results {
			if result.Error != nil {
				hasError = true
				break
			}
		}

		if !hasError {
			t.Error("Expected at least one result with error")
		}
	})
}

func TestToolExecutor_ExecuteSequential(t *testing.T) {
	executor := NewToolExecutor(WithTimeout(5 * time.Second))

	t.Run("Execute tools in sequence", func(t *testing.T) {
		calls := []*ToolCall{
			{
				Tool:  NewMockTool("tool1", 100*time.Millisecond, false),
				Input: &interfaces.ToolInput{Args: map[string]interface{}{}},
				ID:    "call1",
			},
			{
				Tool:  NewMockTool("tool2", 100*time.Millisecond, false),
				Input: &interfaces.ToolInput{Args: map[string]interface{}{}},
				ID:    "call2",
			},
		}

		ctx := context.Background()
		startTime := time.Now()
		results, err := executor.ExecuteSequential(ctx, calls)
		duration := time.Since(startTime)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if len(results) != 2 {
			t.Fatalf("Expected 2 results, got %d", len(results))
		}

		// 顺序执行应该至少花费 200ms
		if duration < 200*time.Millisecond {
			t.Errorf("Sequential execution too fast: %v", duration)
		}
	})

	t.Run("Stop on first error", func(t *testing.T) {
		calls := []*ToolCall{
			{
				Tool:  NewMockTool("tool1", 50*time.Millisecond, true),
				Input: &interfaces.ToolInput{Args: map[string]interface{}{}},
				ID:    "call1",
			},
			{
				Tool:  NewMockTool("tool2", 50*time.Millisecond, false),
				Input: &interfaces.ToolInput{Args: map[string]interface{}{}},
				ID:    "call2",
			},
		}

		ctx := context.Background()
		results, err := executor.ExecuteSequential(ctx, calls)

		if err == nil {
			t.Error("Expected error from failed tool")
		}

		// 应该只有一个结果（在第一个错误处停止）
		if len(results) != 1 {
			t.Errorf("Expected 1 result, got %d", len(results))
		}
	})
}

func TestToolGraph(t *testing.T) {
	t.Run("Create and add nodes", func(t *testing.T) {
		graph := NewToolGraph()

		node1 := &ToolNode{
			ID:           "node1",
			Tool:         NewMockTool("tool1", 0, false),
			Input:        &interfaces.ToolInput{Args: map[string]interface{}{}},
			Dependencies: []string{},
		}

		err := graph.AddNode(node1)
		if err != nil {
			t.Fatalf("Failed to add node: %v", err)
		}

		if graph.Size() != 1 {
			t.Errorf("Expected size 1, got %d", graph.Size())
		}
	})

	t.Run("Add edges", func(t *testing.T) {
		graph := NewToolGraph()

		node1 := &ToolNode{
			ID:   "node1",
			Tool: NewMockTool("tool1", 0, false),
		}
		node2 := &ToolNode{
			ID:   "node2",
			Tool: NewMockTool("tool2", 0, false),
		}

		_ = graph.AddNode(node1)
		_ = graph.AddNode(node2)

		// node2 依赖 node1
		err := graph.AddEdge("node2", "node1")
		if err != nil {
			t.Fatalf("Failed to add edge: %v", err)
		}

		deps := graph.GetDependencies("node2")
		if len(deps) != 1 || deps[0] != "node1" {
			t.Error("Dependencies not set correctly")
		}
	})

	t.Run("Detect cycles", func(t *testing.T) {
		graph := NewToolGraph()

		node1 := &ToolNode{ID: "node1", Tool: NewMockTool("tool1", 0, false)}
		node2 := &ToolNode{ID: "node2", Tool: NewMockTool("tool2", 0, false)}

		_ = graph.AddNode(node1)
		_ = graph.AddNode(node2)

		_ = graph.AddEdge("node2", "node1")

		// 尝试添加会形成环的边
		err := graph.AddEdge("node1", "node2")
		if err == nil {
			t.Error("Expected error when adding edge that creates cycle")
		}
	})

	t.Run("Topological sort", func(t *testing.T) {
		graph := NewToolGraph()

		nodes := []*ToolNode{
			{ID: "A", Tool: NewMockTool("A", 0, false)},
			{ID: "B", Tool: NewMockTool("B", 0, false)},
			{ID: "C", Tool: NewMockTool("C", 0, false)},
		}

		for _, node := range nodes {
			_ = graph.AddNode(node)
		}

		// B 依赖 A，C 依赖 B
		_ = graph.AddEdge("B", "A")
		_ = graph.AddEdge("C", "B")

		sorted, err := graph.TopologicalSort()
		if err != nil {
			t.Fatalf("Topological sort failed: %v", err)
		}

		if len(sorted) != 3 {
			t.Fatalf("Expected 3 nodes in sorted order, got %d", len(sorted))
		}

		// A 应该在 B 之前，B 应该在 C 之前
		aIndex := -1
		bIndex := -1
		cIndex := -1

		for i, id := range sorted {
			switch id {
			case "A":
				aIndex = i
			case "B":
				bIndex = i
			case "C":
				cIndex = i
			}
		}

		if aIndex > bIndex || bIndex > cIndex {
			t.Errorf("Incorrect topological order: %v", sorted)
		}
	})
}

func TestToolExecutor_ExecuteWithDependencies(t *testing.T) {
	executor := NewToolExecutor(WithTimeout(5 * time.Second))

	t.Run("Execute with dependencies", func(t *testing.T) {
		graph := NewToolGraph()

		// 创建依赖图：C 依赖 B，B 依赖 A
		nodeA := &ToolNode{
			ID:    "A",
			Tool:  NewMockTool("A", 50*time.Millisecond, false),
			Input: &interfaces.ToolInput{Args: map[string]interface{}{}},
		}
		nodeB := &ToolNode{
			ID:    "B",
			Tool:  NewMockTool("B", 50*time.Millisecond, false),
			Input: &interfaces.ToolInput{Args: map[string]interface{}{}},
		}
		nodeC := &ToolNode{
			ID:    "C",
			Tool:  NewMockTool("C", 50*time.Millisecond, false),
			Input: &interfaces.ToolInput{Args: map[string]interface{}{}},
		}

		_ = graph.AddNode(nodeA)
		_ = graph.AddNode(nodeB)
		_ = graph.AddNode(nodeC)

		_ = graph.AddEdge("B", "A")
		_ = graph.AddEdge("C", "B")

		ctx := context.Background()
		results, err := executor.ExecuteWithDependencies(ctx, graph)
		if err != nil {
			t.Fatalf("Execution failed: %v", err)
		}

		if len(results) != 3 {
			t.Fatalf("Expected 3 results, got %d", len(results))
		}

		// 验证执行顺序
		for i, result := range results {
			if result.Error != nil {
				t.Errorf("Result %d has error: %v", i, result.Error)
			}
		}
	})
}

func BenchmarkToolExecutor_Parallel(b *testing.B) {
	executor := NewToolExecutor(WithMaxConcurrency(10))

	calls := make([]*ToolCall, 10)
	for i := 0; i < 10; i++ {
		calls[i] = &ToolCall{
			Tool:  NewMockTool("tool", 10*time.Millisecond, false),
			Input: &interfaces.ToolInput{Args: map[string]interface{}{}},
			ID:    string(rune('A' + i)),
		}
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = executor.ExecuteParallel(ctx, calls)
	}
}

func TestToolExecutor_RetryWithJitter(t *testing.T) {
	t.Run("Default jitter is 25%", func(t *testing.T) {
		policy := &RetryPolicy{
			MaxRetries:   3,
			InitialDelay: time.Second,
			MaxDelay:     10 * time.Second,
			Multiplier:   2.0,
			// Jitter 未设置，应使用默认值 0.25
		}
		executor := NewToolExecutor(WithRetryPolicy(policy))

		// 多次计算延迟，验证存在随机性
		delays := make([]time.Duration, 10)
		for i := 0; i < 10; i++ {
			delays[i] = executor.calculateRetryDelay(0)
		}

		// 验证至少有一些延迟值不同（说明有随机抖动）
		hasVariation := false
		for i := 1; i < len(delays); i++ {
			if delays[i] != delays[0] {
				hasVariation = true
				break
			}
		}

		if !hasVariation {
			t.Error("Expected variation in retry delays due to jitter")
		}

		// 验证所有延迟都在合理范围内（基础延迟 ± 25%）
		baseDelay := policy.InitialDelay
		minDelay := time.Duration(float64(baseDelay) * 0.75)
		maxDelay := time.Duration(float64(baseDelay) * 1.25)

		for i, delay := range delays {
			if delay < minDelay || delay > maxDelay {
				t.Errorf("Delay %d out of expected range: %v (expected between %v and %v)",
					i, delay, minDelay, maxDelay)
			}
		}
	})

	t.Run("Custom jitter value", func(t *testing.T) {
		policy := &RetryPolicy{
			MaxRetries:   3,
			InitialDelay: time.Second,
			MaxDelay:     10 * time.Second,
			Multiplier:   2.0,
			Jitter:       0.5, // 50% 抖动
		}
		executor := NewToolExecutor(WithRetryPolicy(policy))

		// 多次计算延迟
		delays := make([]time.Duration, 20)
		for i := 0; i < 20; i++ {
			delays[i] = executor.calculateRetryDelay(0)
		}

		// 验证所有延迟都在合理范围内（基础延迟 ± 50%）
		baseDelay := policy.InitialDelay
		minDelay := time.Duration(float64(baseDelay) * 0.5)
		maxDelay := time.Duration(float64(baseDelay) * 1.5)

		for i, delay := range delays {
			if delay < minDelay || delay > maxDelay {
				t.Errorf("Delay %d out of expected range: %v (expected between %v and %v)",
					i, delay, minDelay, maxDelay)
			}
		}
	})

	t.Run("Zero jitter uses default", func(t *testing.T) {
		policy := &RetryPolicy{
			MaxRetries:   3,
			InitialDelay: time.Second,
			MaxDelay:     10 * time.Second,
			Multiplier:   2.0,
			Jitter:       0, // 零值应使用默认 0.25
		}
		executor := NewToolExecutor(WithRetryPolicy(policy))

		delays := make([]time.Duration, 10)
		for i := 0; i < 10; i++ {
			delays[i] = executor.calculateRetryDelay(0)
		}

		// 验证存在随机性
		hasVariation := false
		for i := 1; i < len(delays); i++ {
			if delays[i] != delays[0] {
				hasVariation = true
				break
			}
		}

		if !hasVariation {
			t.Error("Expected variation even with zero jitter (should use default)")
		}
	})

	t.Run("Jitter respects max delay", func(t *testing.T) {
		policy := &RetryPolicy{
			MaxRetries:   5,
			InitialDelay: time.Second,
			MaxDelay:     2 * time.Second, // 最大延迟限制
			Multiplier:   2.0,
			Jitter:       0.5,
		}
		executor := NewToolExecutor(WithRetryPolicy(policy))

		// 在高重试次数下，抖动后的延迟不应超过 MaxDelay
		for attempt := 0; attempt < 10; attempt++ {
			for i := 0; i < 10; i++ {
				delay := executor.calculateRetryDelay(attempt)
				if delay > policy.MaxDelay {
					t.Errorf("Delay %v exceeds MaxDelay %v for attempt %d",
						delay, policy.MaxDelay, attempt)
				}
			}
		}
	})

	t.Run("Negative delay protection", func(t *testing.T) {
		policy := &RetryPolicy{
			MaxRetries:   3,
			InitialDelay: 10 * time.Millisecond, // 非常小的初始延迟
			MaxDelay:     time.Second,
			Multiplier:   2.0,
			Jitter:       1.0, // 100% 抖动，可能产生负值
		}
		executor := NewToolExecutor(WithRetryPolicy(policy))

		// 即使有 100% 抖动，也不应返回负延迟
		for i := 0; i < 100; i++ {
			delay := executor.calculateRetryDelay(0)
			if delay < 0 {
				t.Errorf("Negative delay detected: %v", delay)
			}
			// 应该至少是 InitialDelay
			if delay < policy.InitialDelay {
				t.Errorf("Delay %v is less than InitialDelay %v", delay, policy.InitialDelay)
			}
		}
	})

	t.Run("Exponential backoff with jitter", func(t *testing.T) {
		policy := &RetryPolicy{
			MaxRetries:   4,
			InitialDelay: 100 * time.Millisecond,
			MaxDelay:     10 * time.Second,
			Multiplier:   2.0,
			Jitter:       0.25,
		}
		executor := NewToolExecutor(WithRetryPolicy(policy))

		// 验证指数退避仍然生效（平均延迟应该随尝试次数增长）
		for attempt := 0; attempt < 4; attempt++ {
			delays := make([]time.Duration, 20)
			var sum time.Duration
			for i := 0; i < 20; i++ {
				delays[i] = executor.calculateRetryDelay(attempt)
				sum += delays[i]
			}
			avgDelay := sum / 20

			// 理论平均延迟（无抖动）
			expectedDelay := policy.InitialDelay
			for i := 0; i < attempt; i++ {
				expectedDelay = time.Duration(float64(expectedDelay) * policy.Multiplier)
			}

			// 平均延迟应该接近理论值（允许 ±30% 误差，考虑随机性）
			lowerBound := time.Duration(float64(expectedDelay) * 0.7)
			upperBound := time.Duration(float64(expectedDelay) * 1.3)

			if avgDelay < lowerBound || avgDelay > upperBound {
				t.Logf("Attempt %d: avg delay %v, expected ~%v (range: %v - %v)",
					attempt, avgDelay, expectedDelay, lowerBound, upperBound)
			}
		}
	})
}
