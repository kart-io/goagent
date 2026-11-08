package tools_test

import (
	"context"
	"fmt"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

// The same typed workflow can be called directly by a host or exposed as a tool.
// A host with a known route does not need a model to dispatch the workflow.
func ExampleNewRunnableTool_workflow() {
	ctx := context.Background()
	graph := workflow.NewGraph[string]()
	check := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	check(graph.AddNode("format", func(_ context.Context, input string) (string, error) {
		return "processed: " + input, nil
	}))
	check(graph.AddEdge(constants.WorkflowStart, "format"))
	check(graph.AddEdge("format", constants.WorkflowEnd))
	run, err := graph.Compile(ctx)
	check(err)

	// Known route: invoke the workflow without a model request.
	direct, err := run.Invoke(ctx, "direct")
	check(err)
	fmt.Println(direct)

	// Agent-selected route: expose the very same workflow through a Dispatcher.
	tool, err := tools.NewRunnableTool(run, tools.RunnableToolConfig[string, string]{
		Name: "format_workflow", Description: "Run a fixed formatting workflow", Version: "v1",
		ArgsSchema: `{"type":"object","properties":{"text":{"type":"string"}},"required":["text"],"additionalProperties":false}`,
		Decode:     func(args map[string]any) (string, error) { return args["text"].(string), nil },
		Encode:     func(result string) (any, error) { return result, nil },
	})
	check(err)
	registry := tools.NewRegistry()
	check(registry.Register(tool))
	dispatcher, err := tools.NewDispatcher(registry, tools.DispatchConfig{})
	check(err)
	// An Agent receives dispatcher.Tools(); here the host supplies the tool call
	// explicitly to keep the example offline and deterministic.
	result, err := dispatcher.Invoke(ctx, tools.ToolRequest{
		Name:  "format_workflow",
		Input: &interfaces.ToolInput{CallID: "format-1", Args: map[string]any{"text": "tool"}},
	})
	check(err)
	fmt.Println(result.Result)
	// Output:
	// processed: direct
	// processed: tool
}
