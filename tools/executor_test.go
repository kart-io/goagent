package tools

import (
	"context"
	"errors"
	"testing"
	"time"
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
		func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
			// 模拟执行时间
			time.Sleep(mockTool.executionTime)

			if mockTool.shouldFail {
				return nil, errors.New("mock tool execution failed")
			}

			return &ToolOutput{
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
				Input: &ToolInput{Args: map[string]interface{}{"arg": "value1"}},
				ID:    "call1",
			},
			{
				Tool:  NewMockTool("tool2", 100*time.Millisecond, false),
				Input: &ToolInput{Args: map[string]interface{}{"arg": "value2"}},
				ID:    "call2",
			},
			{
				Tool:  NewMockTool("tool3", 100*time.Millisecond, false),
				Input: &ToolInput{Args: map[string]interface{}{"arg": "value3"}},
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
				Input: &ToolInput{Args: map[string]interface{}{}},
				ID:    "call1",
			},
			{
				Tool:  NewMockTool("tool2", 50*time.Millisecond, true),
				Input: &ToolInput{Args: map[string]interface{}{}},
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
				Input: &ToolInput{Args: map[string]interface{}{}},
				ID:    "call1",
			},
			{
				Tool:  NewMockTool("tool2", 100*time.Millisecond, false),
				Input: &ToolInput{Args: map[string]interface{}{}},
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
				Input: &ToolInput{Args: map[string]interface{}{}},
				ID:    "call1",
			},
			{
				Tool:  NewMockTool("tool2", 50*time.Millisecond, false),
				Input: &ToolInput{Args: map[string]interface{}{}},
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
			Input:        &ToolInput{Args: map[string]interface{}{}},
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
			Input: &ToolInput{Args: map[string]interface{}{}},
		}
		nodeB := &ToolNode{
			ID:    "B",
			Tool:  NewMockTool("B", 50*time.Millisecond, false),
			Input: &ToolInput{Args: map[string]interface{}{}},
		}
		nodeC := &ToolNode{
			ID:    "C",
			Tool:  NewMockTool("C", 50*time.Millisecond, false),
			Input: &ToolInput{Args: map[string]interface{}{}},
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
			Input: &ToolInput{Args: map[string]interface{}{}},
			ID:    string(rune('A' + i)),
		}
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = executor.ExecuteParallel(ctx, calls)
	}
}
