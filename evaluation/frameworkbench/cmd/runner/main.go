package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	bench "github.com/kart-io/goagent-frameworkbench"
	"os"
	"runtime"
	"time"
)

func main() {
	framework := flag.String("framework", "goagent", "framework")
	scenario := flag.String("scenario", "graph_8", "scenario")
	samples := flag.Int("samples", 400, "samples")
	warmup := flag.Int("warmup", 50, "warmup")
	flag.Parse()
	if *samples < 1 || *warmup < 0 {
		fail(fmt.Errorf("invalid sample counts"))
	}
	r, err := bench.NewRunner(*framework, *scenario)
	if err != nil {
		fail(err)
	}
	durations := make([]int64, 0, *samples)
	firsts := []int64{}
	var result bench.Outcome
	for i := 0; i < *warmup+*samples; i++ {
		start := time.Now()
		result, err = r.Run(context.Background())
		elapsed := time.Since(start).Nanoseconds()
		if err != nil {
			fail(err)
		}
		if err = bench.Validate(*scenario, result); err != nil {
			fail(err)
		}
		if i >= *warmup {
			durations = append(durations, elapsed)
			if result.FirstNS > 0 {
				firsts = append(firsts, result.FirstNS)
			}
		}
	}
	if err = json.NewEncoder(os.Stdout).Encode(map[string]any{"framework": *framework, "scenario": *scenario, "runtime": runtime.Version(), "durations_ns": durations, "first_ns": firsts, "contract": result}); err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
