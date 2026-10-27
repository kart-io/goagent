package main

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

func main() {
	ctx := context.Background()
	g := workflow.NewGraph[int]()
	must(g.AddNode("rule", func(_ context.Context, n int) (int, error) { return n + 30, nil }))
	must(g.AddEdge(workflow.START, "rule"))
	must(g.AddEdge("rule", workflow.END))
	run, err := g.Compile(ctx)
	must(err)
	t, err := tools.NewRunnableTool(run, tools.RunnableToolConfig[int, int]{Name: "refund_days", Description: "Apply refund rule", ArgsSchema: `{"type":"object","properties":{},"additionalProperties":false}`, Version: "v1", Decode: func(map[string]any) (int, error) { return 0, nil }, Encode: func(n int) (any, error) { return n, nil }})
	must(err)
	reg := tools.NewRegistry()
	must(reg.Register(t))
	dispatcher, err := tools.NewDispatcher(reg, tools.DispatchConfig{})
	must(err)
	out, err := dispatcher.Invoke(ctx, tools.ToolRequest{Name: "refund_days", Input: &interfaces.ToolInput{Args: map[string]any{}}})
	must(err)
	fmt.Println(out.Result)
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
