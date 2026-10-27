package main

import (
	"context"
	"fmt"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

// nativeLoopGraph demonstrates two effects inside ONE durable agent node.
// Only local receipts are written. Production destinations must atomically
// deduplicate the child ExecutionKey with their business effect.
func nativeLoopGraph(ctx context.Context, store durable.Store, b *demoBackend, client llm.ToolStreamClient, version string, before func(string) error) (*workflow.DurableGraph[string], error) {
	registry := tools.NewRegistry()
	for _, stage := range []string{"A", "B"} {
		tool := tools.NewBaseTool("record_"+stage, "Write local simulated receipt for stage "+stage, fmt.Sprintf(`{"type":"object","properties":{"stage":{"type":"string","enum":[%q]}},"required":["stage"],"additionalProperties":false}`, stage), func(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			if in.ExecutionKey == "" || in.ExecutionKey != durable.ExecutionKey(ctx) {
				return nil, fmt.Errorf("missing child execution key")
			}
			if before != nil {
				if err := before(stage); err != nil {
					return nil, err
				}
			}
			// Deduplicate by the child key, not just stage, so unstable keys would
			// create extra records and be detected by the recovery test.
			record, err := b.record(ctx, "native", in.ExecutionKey, stage)
			if err != nil {
				return nil, err
			}
			return &interfaces.ToolOutput{Success: true, Result: record.Receipt}, nil
		})
		if err := registry.Register(tool); err != nil {
			return nil, err
		}
	}
	dispatch, err := tools.NewDispatcher(registry, tools.DispatchConfig{MaxAttempts: 2})
	if err != nil {
		return nil, err
	}
	agent, err := builder.NewSimpleBuilder(client).WithTools(dispatch.Tools()...).WithMaxIterations(4).WithMaxTokens(modelMaxTokens).
		WithTimeout(4 * modelTimeout).WithSystemPrompt(`You are testing a local simulated workflow. Call record_A once, then record_B once. Arguments must be exactly {"stage":"A"} for record_A and {"stage":"B"} for record_B. You may request both tools in the same turn in A,B order. After receiving both receipts, answer briefly with both exact receipt strings. Do not call a completed tool again. No external effects are available.`).Build()
	if err != nil {
		return nil, err
	}
	node, err := workflow.Adapt(agent.AsRunnable(), func(s string) (any, error) { return s, nil }, func(_ string, out *builder.AgentOutput) (string, error) { return fmt.Sprint(out.Result), nil })
	if err != nil {
		return nil, err
	}
	graph := workflow.NewGraph[string]()
	if err = graph.AddRunnable("agent", node); err != nil {
		return nil, err
	}
	if err = graph.AddEdge(workflow.START, "agent"); err != nil {
		return nil, err
	}
	if err = graph.AddEdge("agent", workflow.END); err != nil {
		return nil, err
	}
	return graph.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "native-loop-v2/" + version})
}
