package main

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/flow"
)

func main() {
	run, err := flow.AsRunnable(func(context.Context, string) (flow.Source[string], error) {
		return flow.Map(flow.Source[string](func(_ context.Context, y func(string) error) error {
			for _, s := range []string{"", "订单", "通过"} {
				if err := y(s); err != nil {
					return err
				}
			}
			return nil
		}), func(_ context.Context, s string) (string, error) { return s, nil }), nil
	}, flow.Fold[string, string]{Init: func() string { return "" }, Add: func(_ context.Context, a, b string) (string, error) { return a + b, nil }}, func(s string) (*core.StreamEvent, error) { return &core.StreamEvent{Type: "preview", Delta: s}, nil }, flow.Limits[string]{MaxBytes: 100, Size: func(s string) int64 { return int64(len(s)) }})
	if err != nil {
		panic(err)
	}
	stream, err := run.Stream(context.Background(), "")
	if err != nil {
		panic(err)
	}
	for chunk := range stream {
		if chunk.Error != nil {
			panic(chunk.Error)
		}
		if chunk.Event != nil {
			fmt.Printf("preview: %q\n", chunk.Event.Delta)
		}
		if chunk.Done {
			fmt.Println("final:", chunk.Data)
		}
	}
}
