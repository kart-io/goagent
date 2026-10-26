package einocompare

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/kart-io/goagent/core"
)

// BuildWorkflow demonstrates invoking existing GoAgent Runnables inside a
// compiled Eino branch graph. Edges carry final results, not token streams.
// The fallback: prefix selects an explicit alternate route, not error recovery.
func BuildWorkflow(ctx context.Context, primary, fallback core.Runnable[string, string]) (compose.Runnable[string, string], error) {
	if primary == nil || fallback == nil {
		return nil, fmt.Errorf("both workflow routes are required")
	}
	graph := compose.NewGraph[string, string]()
	if err := graph.AddLambdaNode("validate", compose.InvokableLambda(func(ctx context.Context, in string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		in = strings.TrimSpace(in)
		if in == "" {
			return "", fmt.Errorf("input must not be empty")
		}
		return in, nil
	})); err != nil {
		return nil, err
	}
	for name, runnable := range map[string]core.Runnable[string, string]{"primary": primary, "fallback": fallback} {
		if err := graph.AddLambdaNode(name, compose.InvokableLambda(func(ctx context.Context, in string) (string, error) {
			return runnable.Invoke(ctx, strings.TrimPrefix(in, "fallback:"))
		})); err != nil {
			return nil, err
		}
		if err := graph.AddEdge(name, compose.END); err != nil {
			return nil, err
		}
	}
	if err := graph.AddEdge(compose.START, "validate"); err != nil {
		return nil, err
	}
	if err := graph.AddBranch("validate", compose.NewGraphBranch(func(ctx context.Context, in string) (string, error) {
		if strings.HasPrefix(in, "fallback:") {
			return "fallback", nil
		}
		return "primary", nil
	}, map[string]bool{"primary": true, "fallback": true})); err != nil {
		return nil, err
	}
	return graph.Compile(ctx)
}
