// Run without API keys: go run ./examples/workflow [-stream].
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/workflow"
)

type state struct {
	Question string
	Sources  []string
	Answer   string
	Attempts int
}

func main() {
	streaming := flag.Bool("stream", false, "print workflow node and branch events")
	flag.Parse()
	runnable := build()
	for _, question := range []string{"退款规则", "quick:问候"} {
		input := state{Question: question}
		if *streaming {
			if err := showStream(context.Background(), runnable, input); err != nil {
				log.Fatal(err)
			}
		} else {
			out, err := runnable.Invoke(context.Background(), input)
			if err != nil {
				log.Fatal(err)
			}
			show(out)
		}
	}
}

func build() *core.StreamRunnable[state, state] {
	graph := workflow.NewGraph[state](workflow.Config{MaxSteps: 12, MaxConcurrency: 2})
	must(graph.AddNode("validate", func(_ context.Context, s state) (state, error) {
		s.Question = strings.TrimSpace(s.Question)
		if s.Question == "" {
			return s, fmt.Errorf("question is empty")
		}
		return s, nil
	}))
	source := func(name, text string) workflow.Branch[state] {
		return workflow.Branch[state]{Name: name, Run: func(ctx context.Context, s state) (state, error) {
			if err := ctx.Err(); err != nil {
				return s, err
			}
			s.Sources = []string{text}
			return s, nil
		}}
	}
	must(graph.AddParallel("retrieve", workflow.ParallelConfig[state]{
		Branches: []workflow.Branch[state]{source("handbook", "退款期限为 30 天"), source("faq", "退款需提供订单号")},
		Clone:    func(s state) state { s.Sources = append([]string(nil), s.Sources...); return s },
		Merge: func(_ context.Context, s state, branches []state) (state, error) {
			for _, branch := range branches {
				s.Sources = append(s.Sources, branch.Sources...)
			}
			return s, nil
		},
	}))
	must(graph.AddNode("draft", func(_ context.Context, s state) (state, error) {
		s.Attempts++
		s.Answer = strings.Join(s.Sources, "；")
		if s.Attempts == 2 {
			s.Answer += "。请先准备订单号。"
		}
		return s, nil
	}))
	must(graph.AddNode("review", func(_ context.Context, s state) (state, error) { return s, nil }))
	must(graph.AddNode("fast", func(_ context.Context, s state) (state, error) {
		s.Answer = "快速处理：" + strings.TrimPrefix(s.Question, "quick:")
		return s, nil
	}))
	must(graph.AddEdge(workflow.START, "validate"))
	must(graph.AddBranch("validate", func(_ context.Context, s state) (string, error) {
		if strings.HasPrefix(s.Question, "quick:") {
			return "fast", nil
		}
		return "retrieve", nil
	}, "fast", "retrieve"))
	must(graph.AddEdge("retrieve", "draft"))
	must(graph.AddEdge("draft", "review"))
	must(graph.AddBranch("review", func(_ context.Context, s state) (string, error) {
		if s.Attempts < 2 {
			return "draft", nil
		}
		return workflow.END, nil
	}, "draft", workflow.END))
	must(graph.AddEdge("fast", workflow.END))
	compiled, err := graph.Compile(context.Background())
	must(err)
	return compiled
}

func showStream(ctx context.Context, r *core.StreamRunnable[state, state], in state) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	chunks, err := r.Stream(ctx, in)
	if err != nil {
		return err
	}
	for chunk := range chunks {
		if chunk.Error != nil {
			return chunk.Error
		}
		if chunk.Event != nil {
			fmt.Printf("%s %s\n", chunk.Event.Type, chunk.Event.Step)
		}
		if chunk.Done {
			show(chunk.Data)
		}
	}
	return ctx.Err()
}
func show(s state) {
	fmt.Printf("%s -> %s (attempts=%d sources=%d)\n", s.Question, s.Answer, s.Attempts, len(s.Sources))
}
func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
