package frameworkbench

import (
	"context"
	"strings"
	"testing"
)

func TestFrameworkContracts(t *testing.T) {
	for _, framework := range []string{"goagent", "eino"} {
		for _, scenario := range []string{"graph_8", "branch_loop_8", "agent_tool", "stream_64"} {
			t.Run(framework+"/"+scenario, func(t *testing.T) {
				runner, err := NewRunner(framework, scenario)
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 3; i++ {
					got, err := runner.Run(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					want := Outcome{Answer: "8"}
					if scenario == "agent_tool" {
						want = Outcome{Answer: "30", ModelCalls: 2, ToolCalls: 1}
					}
					if scenario == "stream_64" {
						want = Outcome{Answer: strings.Repeat("x", 64), ModelCalls: 1, Chunks: 64}
					}
					if got.Answer != want.Answer || got.ModelCalls != want.ModelCalls || got.ToolCalls != want.ToolCalls || got.Chunks != want.Chunks {
						t.Fatalf("got %+v want %+v", got, want)
					}
				}
			})
		}
	}
}

func TestInvalidRunner(t *testing.T) {
	if _, err := NewRunner("unknown", "graph_8"); err == nil {
		t.Fatal("unknown framework accepted")
	}
	if _, err := NewRunner("goagent", "unknown"); err == nil {
		t.Fatal("unknown scenario accepted")
	}
}

func BenchmarkFramework(b *testing.B) {
	for _, name := range []string{"goagent", "eino"} {
		for _, scenario := range []string{"graph_8", "branch_loop_8", "agent_tool", "stream_64"} {
			b.Run(name+"/"+scenario, func(b *testing.B) {
				runner, err := NewRunner(name, scenario)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := runner.Run(context.Background()); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func TestValidationRejectsSkippedWork(t *testing.T) {
	for _, result := range []Outcome{{Answer: "30", ModelCalls: 1, ToolCalls: 1}, {Answer: "30", ModelCalls: 2}, {Answer: "wrong", ModelCalls: 2, ToolCalls: 1}} {
		if Validate("agent_tool", result) == nil {
			t.Fatalf("accepted skipped/wrong work: %+v", result)
		}
	}
	if Validate("stream_64", Outcome{Answer: strings.Repeat("x", 64), ModelCalls: 1, Chunks: 63}) == nil {
		t.Fatal("accepted missing delta")
	}
}
