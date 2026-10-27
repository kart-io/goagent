package main

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/workflow"
	"maps"
)

type diagnostics struct {
	Values map[string]int
	Total  int
}

func build(ctx context.Context) (core.Runnable[diagnostics, diagnostics], error) {
	node := core.NewStreamRunnable(core.StreamModeFinal, func(_ context.Context, n int, y func(core.StreamChunk[int]) error) error {
		return y(core.StreamChunk[int]{Done: true, Data: n})
	})
	branches := []workflow.Branch[diagnostics]{}
	for _, name := range []string{"cpu", "memory"} {
		name := name
		r, err := workflow.Adapt(node, func(d diagnostics) (int, error) {
			n, ok := d.Values[name]
			if !ok {
				return 0, fmt.Errorf("missing %s", name)
			}
			return n, nil
		}, func(d diagnostics, n int) (diagnostics, error) { d.Values[name] = n; return d, nil })
		if err != nil {
			return nil, err
		}
		branches = append(branches, workflow.Branch[diagnostics]{Name: name, Run: r.Invoke})
	}
	g := workflow.NewGraph[diagnostics]()
	if err := g.AddParallel("diagnose", workflow.ParallelConfig[diagnostics]{Branches: branches, Clone: func(d diagnostics) diagnostics { d.Values = maps.Clone(d.Values); return d }, Merge: func(_ context.Context, d diagnostics, out []diagnostics) (diagnostics, error) {
		d.Values = maps.Clone(d.Values)
		d.Total = out[0].Values["cpu"] + out[1].Values["memory"]
		return d, nil
	}}); err != nil {
		return nil, err
	}
	if err := g.AddEdge(workflow.START, "diagnose"); err != nil {
		return nil, err
	}
	if err := g.AddEdge("diagnose", workflow.END); err != nil {
		return nil, err
	}
	return g.Compile(ctx)
}
func main() {
	r, err := build(context.Background())
	if err != nil {
		panic(err)
	}
	out, err := r.Invoke(context.Background(), diagnostics{Values: map[string]int{"cpu": 0, "memory": 4}})
	if err != nil {
		panic(err)
	}
	fmt.Println(out.Total)
}
