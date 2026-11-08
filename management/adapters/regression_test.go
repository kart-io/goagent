package adapters

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/workflow"
	"testing"
)

func TestReviewGraphLostConfirmationAtNextGate(t *testing.T) {
	ctx := context.Background()
	store, e := durable.NewFileStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	g := workflow.NewGraph[int]()
	for _, n := range []string{"a", "b"} {
		if e = g.AddNode(n, func(_ context.Context, s int) (int, error) { return s + 1, nil }); e != nil {
			t.Fatal(e)
		}
	}
	for _, x := range [][2]string{{constants.WorkflowStart, "a"}, {"a", "b"}, {"b", constants.WorkflowEnd}} {
		if e = g.AddEdge(x[0], x[1]); e != nil {
			t.Fatal(e)
		}
	}
	compiled, e := g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v1", ApprovalNodes: []string{"a", "b"}}, workflow.WithResumeHandler(func(_ context.Context, _ string, s *workflow.GraphSnapshot[int], _ json.RawMessage) (int, error) {
		return s.State, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	first, _ := compiled.Run(ctx, "task", 0, "")
	answer := mg.ApprovalAnswer{ApprovalID: "ap", Decision: "approve", IntentHash: "intent"}
	raw, _ := json.Marshal(answer)
	second, _ := compiled.Resume(ctx, "task", workflow.ResumeRequest{Key: first.Key, Input: raw})
	if second == nil || !second.Waiting || second.Key == first.Key {
		t.Fatal("did not advance to next gate")
	}
	adapter := graph[int]{g: compiled, initial: func(json.RawMessage) (int, error) { return 0, nil }, r: mg.Resources{ApprovalReader: func(context.Context, string) (mg.Approval, error) {
		return mg.Approval{TaskID: "task", Decision: "approve", State: "approved", Wait: mg.WaitPoint{Key: first.Key}}, nil
	}}}
	_, e = adapter.Run(ctx, mg.RunSpec{Task: mg.Task{Meta: mg.Meta{ID: "task"}, RunID: "task", Input: []byte(`0`)}, ResumeInputs: map[string]string{first.Key: string(raw)}})
	if e != nil {
		t.Fatalf("old decision accepted but management confirmation lost; expected current wait, got %v", e)
	}
}

func TestReviewGraphInputValidation(t *testing.T) {
	ctx := context.Background()
	store, e := durable.NewFileStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	g := workflow.NewGraph[int]()
	g.AddNode("ask", func(ctx context.Context, s int) (int, error) {
		_, e := durable.AwaitInput(ctx, "amount", json.RawMessage(`{}`), func(raw json.RawMessage) error { return fmt.Errorf("invalid amount") })
		return s, e
	})
	g.AddEdge(constants.WorkflowStart, "ask")
	g.AddEdge("ask", constants.WorkflowEnd)
	compiled, e := g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v1"})
	if e != nil {
		t.Fatal(e)
	}
	adapter := graph[int]{g: compiled, initial: func(json.RawMessage) (int, error) { return 0, nil }}
	spec := mg.RunSpec{Task: mg.Task{Meta: mg.Meta{ID: "task"}, RunID: "task", Input: []byte(`0`)}}
	first, e := adapter.Run(ctx, spec)
	if e != nil {
		t.Fatal(e)
	}
	spec.ResumeInputs = map[string]string{first.Waits[0].Key: `-1`}
	result, e := adapter.Run(ctx, spec)
	if e != nil || result.Outcome != "waiting" || len(result.Waits) != 1 || !result.Waits[0].Rejected {
		t.Fatalf("validation must preserve rejected wait, got outcome=%q waits=%+v err=%v", result.Outcome, result.Waits, e)
	}
}
