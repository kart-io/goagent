package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/execution"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

type cycleResult struct{ Requests, Effects, ReplayRequests, ReplayEffects uint64 }

// syntheticModel 不保留请求历史；一毫秒延迟只用于观察取消，不模拟真实供应商性能。
type syntheticModel struct{ calls atomic.Uint64 }

func (*syntheticModel) Provider() constants.Provider { return constants.ProviderCustom }
func (m *syntheticModel) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	return m.CompleteToolsStream(ctx, req, nil)
}
func (m *syntheticModel) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, available []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	timer := time.NewTimer(time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}
	m.calls.Add(1)
	final := &llm.StreamChunk{Done: true, Content: "回执", FinishReason: "stop", Usage: &llm.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}}
	if len(available) > 0 && req.Messages[len(req.Messages)-1].Role != "tool" {
		final.Content = ""
		final.FinishReason = "tool_calls"
		call := llm.ToolCall{ID: "call", Type: "function"}
		call.Function.Name = "lookup"
		call.Function.Arguments = "{}"
		final.ToolCalls = []llm.ToolCall{call}
	}
	output := make(chan *llm.StreamChunk, 1)
	output <- final
	close(output)
	return output, nil
}

// exercise 验证持久工具循环、会话日志、重建重放、图审批和取消后的零效果。
func exercise(ctx context.Context, directory string) (got cycleResult, err error) {
	model := &syntheticModel{}
	sessions, err := session.NewFileStore(filepath.Join(directory, "sessions"))
	if err != nil {
		return got, err
	}
	for round := range 2 {
		journal := session.DurableStore(sessions, "session")
		ctrl, e := budget.NewDurable(budget.Config{RootKey: "run", Version: "v1", MaxModelRequests: 2, MaxToolCalls: 1}, journal)
		if e != nil {
			return got, e
		}
		scope, e := execution.NewScope(execution.Config{RunID: "run", SessionID: "session", Journal: journal, Budget: ctrl, Timeout: 3 * time.Second})
		if e != nil {
			return got, e
		}
		registry := tools.NewRegistry()
		e = registry.Register(tools.NewBaseTool("lookup", "读取合成记录", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			got.Effects++
			return &interfaces.ToolOutput{Success: true, Result: "回执"}, nil
		}))
		if e != nil {
			return got, e
		}
		b, _, e := execution.NewGovernedBuilder(model, scope, "assistant", registry, tools.DispatchConfig{MaxAdmissionConcurrency: 1, MaxQueued: 2, Authorize: func(context.Context, string, *interfaces.ToolInput) error { return nil }})
		if e != nil {
			return got, e
		}
		b.WithSessionLog(sessions).WithMaxIterations(3).WithOutputValidator(func(_ context.Context, out *builder.AgentOutput) error {
			if out.Result != "回执" || got.Effects != 1 {
				return fmt.Errorf("最终回执或工具效果数错误")
			}
			return nil
		})
		bound, cancel, e := scope.Bind(ctx)
		if e != nil {
			return got, e
		}
		agent, e := b.BuildContext(bound)
		before := model.calls.Load()
		if e == nil {
			_, e = agent.AsRunnable().Invoke(bound, "读取记录")
		}
		cancel()
		if e != nil {
			return got, e
		}
		if round == 1 {
			got.ReplayRequests = model.calls.Load() - before
			got.ReplayEffects = got.Effects - 1
		}
	}
	got.Requests = model.calls.Load()
	if got.Requests != 2 || got.ReplayRequests != 0 || got.ReplayEffects != 0 {
		return got, fmt.Errorf("持久重放产生额外模型请求或效果")
	}
	events, err := sessions.Read(ctx, "session", 0)
	if err != nil {
		return got, err
	}
	messages, err := session.DeriveMessages(events)
	if err != nil {
		return got, err
	}
	if len(messages) < 3 || messages[len(messages)-1].Content != "回执" {
		return got, fmt.Errorf("会话日志未保留完整工具循环")
	}
	store, err := durable.NewFileStore(filepath.Join(directory, "workflow"))
	if err != nil {
		return got, err
	}
	graph := workflow.NewGraph[int]()
	if err = graph.AddNode("effect", func(_ context.Context, state int) (int, error) { got.Effects++; return state + 1, nil }); err != nil {
		return got, err
	}
	if err = graph.AddEdge(workflow.START, "effect"); err != nil {
		return got, err
	}
	if err = graph.AddEdge("effect", workflow.END); err != nil {
		return got, err
	}
	compiled, err := graph.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v1", ApprovalNodes: []string{"effect"}})
	if err != nil {
		return got, err
	}
	wait, err := compiled.Run(ctx, "approval", 41, "")
	if !errors.Is(err, durable.ErrApprovalRequired) || !wait.Waiting || got.Effects != 1 {
		return got, fmt.Errorf("审批前产生效果或未暂停：%w", err)
	}
	done, err := compiled.Run(ctx, "approval", 999, wait.Key)
	if err != nil {
		return got, err
	}
	if !done.Done || done.State != 42 || got.Effects != 2 {
		return got, fmt.Errorf("审批恢复结果错误")
	}
	done, err = compiled.Run(ctx, "approval", 999, "")
	if err != nil {
		return got, err
	}
	if !done.Done || done.State != 42 || got.Effects != 2 {
		return got, fmt.Errorf("已完成图重放产生重复效果")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	effect := tools.NewBaseTool("cancel", "取消后禁止执行", `{}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		got.Effects++
		return &interfaces.ToolOutput{Success: true}, nil
	})
	_, err = tools.NewToolExecutor().ExecuteSequential(canceled, []*tools.ToolCallRequest{{ID: "cancel", Tool: effect}})
	if !errors.Is(err, context.Canceled) || got.Effects != 2 {
		return got, fmt.Errorf("取消后发生工具效果或返回成功")
	}
	return got, nil
}
