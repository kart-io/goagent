package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/evaluation/einocompare"
)

func main() {
	primary := core.NewStreamRunnable(core.StreamModeFinal, func(ctx context.Context, in string, emit func(core.StreamChunk[string]) error) error {
		return emit(core.StreamChunk[string]{Done: true, Data: strings.ToUpper(in)})
	})
	fallback := core.NewStreamRunnable(core.StreamModeFinal, func(ctx context.Context, in string, emit func(core.StreamChunk[string]) error) error {
		return emit(core.StreamChunk[string]{Done: true, Data: "alternate:" + in})
	})
	workflow, err := einocompare.BuildWorkflow(context.Background(), primary, fallback)
	if err != nil {
		log.Fatal(err)
	}
	for _, input := range []string{"hello", "fallback:question"} {
		output, err := workflow.Invoke(context.Background(), input)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(output)
	}
}
