// agent-usage uses controlled model streams: no credentials or network needed.
package main

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

type scripted struct {
	*llm.MockStreamClient
	turns     [][]*llm.StreamChunk
	next      int
	failFirst bool
}

func (s *scripted) ModelName() string { return "offline-script" }
func (s *scripted) CompleteStream(ctx context.Context, r *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	return s.CompleteToolsStream(ctx, r, nil)
}
func (s *scripted) CompleteToolsStream(ctx context.Context, _ *llm.CompletionRequest, _ []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	if s.failFirst {
		s.failFirst = false
		return nil, &llm.ProviderError{StatusCode: 503, Err: fmt.Errorf("controlled transient failure")}
	}
	if s.next >= len(s.turns) {
		return nil, fmt.Errorf("unexpected model request")
	}
	turn := s.turns[s.next]
	s.next++
	ch := make(chan *llm.StreamChunk, len(turn))
	for _, chunk := range turn {
		select {
		case ch <- chunk:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	close(ch)
	return ch, nil
}
func answer(n int) []*llm.StreamChunk {
	return []*llm.StreamChunk{{Delta: "done", Done: true, FinishReason: "stop", Usage: &llm.Usage{TotalTokens: n}}}
}
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
func check(err error) {
	if err != nil {
		panic(err)
	}
}
func call(id, name string) llm.ToolCall {
	c := llm.ToolCall{ID: id, Type: "function"}
	c.Function.Name = name
	c.Function.Arguments = "{}"
	return c
}
func dispatcher(ts ...interfaces.Tool) *tools.Dispatcher {
	r := tools.NewRegistry()
	for _, t := range ts {
		check(r.Register(t))
	}
	return must(tools.NewDispatcher(r, tools.DispatchConfig{}))
}

func main() {
	dir := must(os.MkdirTemp("", "goagent-usage-demo-"))
	defer os.RemoveAll(dir)
	store := must(durable.NewFileStore(dir))
	controller := must(budget.NewDurable(budget.Config{RootKey: "demo", Version: "v1", MaxModelRequests: 5, MaxAgentCalls: 8}, store))
	ctx := budget.WithController(durable.WithJournal(durable.WithExecutionKey(context.Background(), "demo"), store), controller)
	var live, replayed atomic.Int64
	usageHook := func(_ context.Context, e budget.UsageEvent) {
		// This callback is inherited by both child Agents. Snapshots are cumulative:
		// never add Statistics.Root across callbacks, or charge replayed evidence.
		if e.Model.Replayed {
			replayed.Add(1)
		} else {
			live.Add(1)
		}
	}
	var children []interfaces.Tool
	for i, n := range []int{4, 5} {
		model := &scripted{MockStreamClient: llm.NewMockStreamClient(), turns: [][]*llm.StreamChunk{answer(n)}, failFirst: i == 1}
		child := must(builder.NewSimpleBuilder(model).WithAgentID(fmt.Sprintf("child-%d", i)).WithDurableScope(fmt.Sprintf("child-%d", i)).WithRequestError(1, func(context.Context, *builder.RequestFailure) (builder.RequestErrorDecision, error) {
			return builder.RequestErrorDecision{Action: builder.RequestRetry}, nil
		}).Build())
		children = append(children, must(tools.NewRunnableTool(child.AsRunnable(), tools.RunnableToolConfig[any, *builder.AgentOutput]{Name: fmt.Sprintf("child_%d", i), Description: "Controlled child", Version: "v1", ModelInvoking: true, ArgsSchema: `{"type":"object"}`, Decode: func(map[string]any) (any, error) { return "task", nil }, Encode: func(o *builder.AgentOutput) (any, error) { return o.Result, nil }})))
	}
	model := &scripted{MockStreamClient: llm.NewMockStreamClient(), turns: [][]*llm.StreamChunk{{{Done: true, FinishReason: "tool_calls", Usage: &llm.Usage{TotalTokens: 3}, ToolCalls: []llm.ToolCall{call("a", "child_0"), call("b", "child_1")}}}, answer(6)}}
	parent := must(builder.NewSimpleBuilder(model).WithAgentID("parent").WithDurableScope("parent").WithParallelToolCalls(2).WithTools(dispatcher(children...).Tools()...).WithUsageHook(usageHook).WithMaxIterations(2).Build())
	for pass := 0; pass < 2; pass++ {
		_, err := parent.AsRunnable().Invoke(ctx, "delegate and summarize")
		check(err)
		report := must(controller.Attribution(ctx))
		fmt.Printf("pass=%d root=%d requests=%d unknown=%d\n", pass, report.Root.KnownTokens.TotalTokens, report.Root.ReservedRequests, report.UnknownUsageEntries)
		for _, role := range report.Agents {
			fmt.Printf("  %s self=%d\n", role.AgentID, role.Self.KnownTokens.TotalTokens)
		}
		for _, c := range report.Calls {
			if c.AgentID == "parent" {
				fmt.Printf("  parent inclusive=%d\n", c.Inclusive.KnownTokens.TotalTokens)
			}
		}
	}
	fmt.Printf("hook: live=%d replay=%d\n", live.Load(), replayed.Load())
}
