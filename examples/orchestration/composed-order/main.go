// A local-only order flow: rule graph -> isolated policy agent -> final answer.
// Repeating with the same directory replays completed operations without new calls.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/builder/contextpolicy"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/devtools"
	"github.com/kart-io/goagent/examples/orchestration/internal/demomodel"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

type order struct{ ID, Answer string }
type result struct {
	Answer                    string
	Budget                    budget.Snapshot
	NewCalls, Created, Closed int
}

func journalTools(ts []interfaces.Tool) ([]interfaces.Tool, error) {
	reg := tools.NewRegistry()
	for _, t := range ts {
		if err := reg.Register(t); err != nil {
			return nil, err
		}
	}
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{MaxConcurrency: 1})
	if err != nil {
		return nil, err
	}
	out := []interfaces.Tool{}
	byName := map[string]interfaces.Tool{}
	for _, t := range d.Tools() {
		byName[t.Name()] = t
	}
	for _, t := range ts {
		out = append(out, byName[t.Name()])
	}
	return out, nil
}
func run(ctx context.Context, dir string) (result, error) {
	var out result
	store, err := durable.NewFileStore(dir)
	if err != nil {
		return out, err
	}
	controller, err := budget.NewDurable(budget.Config{RootKey: "order/42", Version: "v1", MaxModelRequests: 4}, store)
	if err != nil {
		return out, err
	}
	recorder, err := devtools.NewRecorder(devtools.RecorderConfig{Writer: os.Stdout, QueueCapacity: 128})
	if err != nil {
		return out, err
	}
	defer func() { _ = recorder.Close(context.Background()) }()
	ctx = observe.WithObserver(budget.WithController(durable.WithJournal(durable.WithExecutionKey(ctx, "order/42/agent"), store), controller), recorder)
	var calls, created, closed atomic.Int64
	rules := workflow.NewGraph[int]()
	if err = rules.AddNode("refund_rule", func(_ context.Context, n int) (int, error) { return n + 30, nil }); err != nil {
		return out, err
	}
	if err = rules.AddEdge(workflow.START, "refund_rule"); err != nil {
		return out, err
	}
	if err = rules.AddEdge("refund_rule", workflow.END); err != nil {
		return out, err
	}
	ruleRun, err := rules.Compile(ctx)
	if err != nil {
		return out, err
	}
	ruleTool, err := tools.NewRunnableTool(ruleRun, tools.RunnableToolConfig[int, int]{Name: "refund_rule", Description: "Refund rule evidence", ArgsSchema: `{"type":"object","properties":{},"additionalProperties":false}`, Version: "v1", Decode: func(map[string]any) (int, error) { return 0, nil }, Encode: func(n int) (any, error) { return strings.Repeat(fmt.Sprintf("退款期%d天。", n), 20), nil }})
	if err != nil {
		return out, err
	}
	mem := memory.NewInMemoryManager(nil)
	childTool, err := tools.NewRunnableToolFactory(func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		created.Add(1)
		model := &demomodel.Model{Calls: &calls, Respond: func(*llm.CompletionRequest) llm.StreamChunk { return llm.StreamChunk{Content: "政策允许退款"} }}
		child, e := builder.NewSimpleBuilder(model).WithSystemPrompt("仅检查政策；父历史不传入").WithMemory(mem).WithSessionID("policy/" + uuid.NewString()).Build()
		if e != nil {
			return tools.OwnedRunnable[any, *builder.AgentOutput]{}, e
		}
		return tools.OwnedRunnable[any, *builder.AgentOutput]{Runnable: child.AsRunnable(), Close: func(context.Context) error { closed.Add(1); return nil }}, nil
	}, tools.RunnableToolConfig[any, *builder.AgentOutput]{Name: "policy_agent", Description: "Isolated policy assessment", ArgsSchema: `{"type":"object","properties":{},"additionalProperties":false}`, Version: "v1", ModelInvoking: true, Decode: func(map[string]any) (any, error) { return "订单42是否允许退款？", nil }, Encode: func(a *builder.AgentOutput) (any, error) {
		if a == nil {
			return nil, fmt.Errorf("missing child result")
		}
		return a.Result, nil
	}})
	if err != nil {
		return out, err
	}
	business, err := journalTools([]interfaces.Tool{ruleTool, childTool})
	if err != nil {
		return out, err
	}
	results, err := contextpolicy.NewDurableResultStore(store)
	if err != nil {
		return out, err
	}
	model := &demomodel.Model{Calls: &calls, Respond: func(req *llm.CompletionRequest) llm.StreamChunk {
		count := 0
		for _, m := range req.Messages {
			if m.Role == "tool" {
				count++
			}
		}
		switch count {
		case 0:
			return llm.StreamChunk{ToolCalls: []llm.ToolCall{demomodel.Call("rule", "refund_rule")}}
		case 1:
			return llm.StreamChunk{ToolCalls: []llm.ToolCall{demomodel.Call("policy", "policy_agent")}}
		default:
			return llm.StreamChunk{Content: "批准退款：满足期限与政策。"}
		}
	}}
	agent, err := builder.NewSimpleBuilder(model).WithTools(business...).WithMaxIterations(4).WithContextPolicies(func() contextpolicy.Policy {
		p, e := contextpolicy.NewOffload(contextpolicy.OffloadConfig{Store: results, MaxToolResultRunes: 40, KeepRecentToolRounds: 1, Namespace: "order-evidence"})
		if e != nil {
			panic(e)
		}
		return p
	}).WithPolicyToolAdapter(journalTools).Build()
	if err != nil {
		return out, err
	}
	mapped, err := workflow.Adapt(agent.AsRunnable(), func(s order) (any, error) {
		if s.ID == "" {
			return nil, fmt.Errorf("order ID required")
		}
		return "审核订单" + s.ID, nil
	}, func(s order, a *builder.AgentOutput) (order, error) { s.Answer = fmt.Sprint(a.Result); return s, nil })
	if err != nil {
		return out, err
	}
	final, err := mapped.Invoke(ctx, order{ID: "42"})
	if err != nil {
		return out, err
	}
	out.Answer = final.Answer
	out.NewCalls = int(calls.Load())
	out.Created = int(created.Load())
	out.Closed = int(closed.Load())
	out.Budget, err = controller.Snapshot(ctx)
	return out, err
}
func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: composed-order <store-directory>")
		os.Exit(2)
	}
	out, err := run(context.Background(), os.Args[1])
	if err != nil {
		panic(err)
	}
	fmt.Printf("%+v\n", out)
}
