package main

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/workflow"
)

type order struct {
	ID       string
	Amount   int
	Discount *int
	Total    int
}

func adapter(run core.Runnable[int, int]) (*core.StreamRunnable[order, order], error) {
	return workflow.Adapt(run, func(s order) (int, error) {
		if s.ID == "" || s.Discount == nil {
			return 0, fmt.Errorf("order ID and discount required")
		}
		return s.Amount - *s.Discount, nil
	}, func(s order, total int) (order, error) { s.Total = total; return s, nil })
}
func main() {
	run := core.NewStreamRunnable(core.StreamModeFinal, func(_ context.Context, n int, emit func(core.StreamChunk[int]) error) error {
		return emit(core.StreamChunk[int]{Done: true, Data: n})
	})
	mapped, err := adapter(run)
	if err != nil {
		panic(err)
	}
	zero := 0
	out, err := mapped.Invoke(context.Background(), order{ID: "42", Discount: &zero})
	if err != nil {
		panic(err)
	}
	fmt.Println(out.Total)
}
