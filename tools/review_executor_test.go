package tools

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

// 工具返回业务失败时，结果边界必须停止顺序后继、阻断依赖，且不能重复副作用。
func TestReviewExecutorUnsuccessfulOutputs(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  *interfaces.ToolOutput
	}{
		{"失败状态", &interfaces.ToolOutput{Success: false, Result: "未提交"}},
		{"错误字段", &interfaces.ToolOutput{Success: true, Error: "未提交"}},
		{"空输出", nil},
	} {
		for _, entry := range []string{"顺序", "并行", "依赖"} {
			t.Run(tc.name+"/"+entry, func(t *testing.T) {
				attempts, children := 0, 0
				tool := NewBaseTool("write", "", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
					attempts++
					return tc.out, nil
				})
				child := NewBaseTool("publish", "", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
					children++
					return &interfaces.ToolOutput{Success: true}, nil
				})
				executor := NewToolExecutor(WithRetryPolicy(&RetryPolicy{MaxRetries: 3}))
				var results []*ToolResult
				var err error
				switch entry {
				case "顺序":
					results, err = executor.ExecuteSequential(context.Background(), []*ToolCallRequest{{ID: "write", Tool: tool}, {ID: "publish", Tool: child}})
					require.Error(t, err)
					require.Len(t, results, 1)
					require.Zero(t, children)
				case "并行":
					results, err = executor.ExecuteParallel(context.Background(), []*ToolCallRequest{{ID: "write", Tool: tool}})
					require.Error(t, err)
				case "依赖":
					graph := NewToolGraph()
					require.NoError(t, graph.AddNode(&ToolNode{ID: "write", Tool: tool}))
					require.NoError(t, graph.AddNode(&ToolNode{ID: "publish", Tool: child}))
					require.NoError(t, graph.AddEdge("publish", "write"))
					results, err = executor.ExecuteWithDependencies(context.Background(), graph)
					require.NoError(t, err, "依赖入口通过逐节点错误报告失败")
					require.Len(t, results, 2)
					require.Error(t, results[1].Error)
					require.Zero(t, children, "失败前置不能解锁依赖")
				}
				require.Error(t, results[0].Error)
				require.Equal(t, 1, attempts, "结果校验不能重试有副作用的业务失败")
			})
		}
	}
}

// 拒绝需要保留给模型的反馈，但它不是依赖图中的成功前置。
func TestReviewExecutorRejectedFeedbackDoesNotUnlockDependencies(t *testing.T) {
	for _, entry := range []string{"顺序", "并行", "依赖"} {
		t.Run(entry, func(t *testing.T) {
			attempts, children := 0, 0
			tool := NewBaseTool("write", "", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				attempts++
				return Rejected("余额不足", map[string]interface{}{"balance": 3}), nil
			})
			child := NewBaseTool("publish", "", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				children++
				return &interfaces.ToolOutput{Success: true}, nil
			})
			executor := NewToolExecutor(WithRetryPolicy(&RetryPolicy{MaxRetries: 3}))
			var results []*ToolResult
			var err error
			switch entry {
			case "顺序":
				results, err = executor.ExecuteSequential(context.Background(), []*ToolCallRequest{{ID: "write", Tool: tool}})
			case "并行":
				results, err = executor.ExecuteParallel(context.Background(), []*ToolCallRequest{{ID: "write", Tool: tool}})
			case "依赖":
				graph := NewToolGraph()
				require.NoError(t, graph.AddNode(&ToolNode{ID: "write", Tool: tool}))
				require.NoError(t, graph.AddNode(&ToolNode{ID: "publish", Tool: child}))
				require.NoError(t, graph.AddEdge("publish", "write"))
				results, err = executor.ExecuteWithDependencies(context.Background(), graph)
				require.Len(t, results, 2)
				require.Error(t, results[1].Error)
				require.Zero(t, children)
			}
			require.NoError(t, err)
			require.NoError(t, results[0].Error)
			message, rejected := RejectedResult(results[0].Output)
			require.True(t, rejected)
			require.Equal(t, "余额不足", message)
			require.Equal(t, 1, attempts)
		})
	}
}
