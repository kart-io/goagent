package adapters

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/codec/json"
	"sync"
	"testing"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

type probe struct {
	llm.Client
	mu                   sync.Mutex
	requests, effects, n int
	store                session.Store
}

func newProbe(t *testing.T, dir string, n int) *probe {
	s, err := session.NewFileStore(dir)
	require.NoError(t, err)
	return &probe{store: s, n: n}
}
func (p *probe) Provider() constants.Provider { return constants.ProviderOpenAI }
func (p *probe) CompleteToolsStream(_ context.Context, r *llm.CompletionRequest, _ []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	p.mu.Lock()
	p.requests++
	p.mu.Unlock()
	ch := make(chan *llm.StreamChunk, 1)
	done := false
	for _, m := range r.Messages {
		if m.Role == "tool" {
			done = true
		}
	}
	if done {
		ch <- &llm.StreamChunk{Delta: "操作已处理", Done: true, FinishReason: "stop"}
	} else {
		calls := []llm.ToolCall{}
		for i := 0; i < p.n; i++ {
			c := llm.ToolCall{ID: fmt.Sprint(i), Type: "function"}
			c.Function.Name = "effect"
			c.Function.Arguments = fmt.Sprintf(`{"n":%d}`, i)
			calls = append(calls, c)
		}
		ch <- &llm.StreamChunk{Done: true, FinishReason: "tool_calls", ToolCalls: calls}
	}
	close(ch)
	return ch, nil
}
func (p *probe) run(ctx context.Context, inputs map[string]string) (*builder.AgentOutput, error) {
	ctx = durable.WithJournal(durable.WithExecutionKey(ctx, "probe-run"), session.DurableStore(p.store, "probe-session"))
	for key, input := range inputs {
		ctx = durable.WithResume(ctx, key, input)
	}
	reg := tools.NewRegistry()
	if err := reg.Register(tools.NewBaseTool("effect", "操作", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		p.mu.Lock()
		p.effects++
		p.mu.Unlock()
		return &interfaces.ToolOutput{Success: true, Result: "已执行"}, nil
	})); err != nil {
		return nil, err
	}
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{Pipeline: &tools.Pipeline{
		Pre: []tools.PreHook{func(context.Context, *tools.Call) (tools.PreDecision, error) {
			return tools.PreDecision{Kind: tools.PreAsk}, nil
		}},
		Approver: tools.ApproverFunc(func(ctx context.Context, r tools.ApprovalRequest) (tools.ApprovalResult, error) {
			raw, err := durable.AwaitInput(durable.AllowInterrupt(ctx), "approval/"+r.Call.CallID, json.RawMessage(`{"kind":"approval"}`), nil)
			if err != nil {
				return tools.ApprovalResult{}, err
			}
			var answer string
			if err = json.Unmarshal(raw, &answer); err != nil {
				return tools.ApprovalResult{}, err
			}
			if answer == "approve" {
				return tools.ApprovalResult{Outcome: tools.ApprovalAllowedOnce}, nil
			}
			return tools.ApprovalResult{Outcome: tools.ApprovalRejected, Feedback: "已拒绝"}, nil
		}),
	}})
	if err != nil {
		return nil, err
	}
	a, err := builder.NewSimpleBuilder(p).WithTools(d.Tools()...).WithSessionLog(p.store).WithSessionID("probe-session").WithDurableScope("probe-agent").WithParallelToolCalls(2).WithMaxIterations(4).Build()
	if err != nil {
		return nil, err
	}
	events, err := a.ExecuteStream(ctx, "处理操作")
	if err != nil {
		return nil, err
	}
	var out *builder.AgentOutput
	for e := range events {
		if e.Error != nil {
			err = e.Error
		}
		if e.Output != nil {
			out = e.Output
		}
	}
	return out, err
}
