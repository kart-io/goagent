// human-in-the-loop demonstrates a persisted review gate without model calls.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kart-io/goagent/codec/json"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/workflow"
)

type reviewState struct {
	Text     string `json:"text"`
	Decision string `json:"decision,omitempty"`
	Reviewer string `json:"reviewer,omitempty"`
	Status   string `json:"status"`
}

type humanResponse struct {
	Decision string `json:"decision"`
	Text     string `json:"text,omitempty"`
}

type reviewerKey struct{}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("human-in-the-loop", flag.ContinueOnError)
	dir := flags.String("dir", "", "checkpoint directory (required)")
	id := flags.String("run", "review-1", "stable run ID")
	action := flags.String("action", "start", "start, status, or resume")
	key := flags.String("key", "", "waiting checkpoint key")
	decision := flags.String("decision", "", "approve or reject")
	text := flags.String("text", "", "draft text, or optional human revision")
	reviewer := flags.String("reviewer", "", "demo-reviewer for the local example")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dir == "" || flags.NArg() != 0 {
		return fmt.Errorf("provide -dir and no positional arguments")
	}
	store, err := durable.NewFileStore(*dir)
	if err != nil {
		return err
	}
	g := workflow.NewGraph[reviewState]()
	if err := g.AddNode("review", func(_ context.Context, in reviewState) (reviewState, error) { return in, nil }); err != nil {
		return err
	}
	if err := g.AddNode("accept", func(_ context.Context, in reviewState) (reviewState, error) { in.Status = "accepted"; return in, nil }); err != nil {
		return err
	}
	if err := g.AddNode("reject", func(_ context.Context, in reviewState) (reviewState, error) { in.Status = "rejected"; return in, nil }); err != nil {
		return err
	}
	for _, edge := range [][2]string{{workflow.START, "review"}, {"accept", workflow.END}, {"reject", workflow.END}} {
		if err := g.AddEdge(edge[0], edge[1]); err != nil {
			return err
		}
	}
	if err := g.AddBranch("review", func(_ context.Context, in reviewState) (string, error) {
		if in.Decision == "approve" {
			return "accept", nil
		}
		return "reject", nil
	}, "accept", "reject"); err != nil {
		return err
	}
	graph, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "human-review-v1", ApprovalNodes: []string{"review"}},
		workflow.WithResumeHandler(func(ctx context.Context, _ string, snap *workflow.GraphSnapshot[reviewState], raw json.RawMessage) (reviewState, error) {
			var response humanResponse
			if err := json.Unmarshal(raw, &response); err != nil {
				return reviewState{}, err
			}
			if response.Decision != "approve" && response.Decision != "reject" {
				return reviewState{}, fmt.Errorf("decision must be approve or reject")
			}
			state := snap.State
			state.Decision = response.Decision
			if strings.TrimSpace(response.Text) != "" {
				state.Text = response.Text
			}
			state.Reviewer, _ = ctx.Value(reviewerKey{}).(string)
			return state, nil
		}), workflow.WithBeforeApprove(func(ctx context.Context, _ string, snap *workflow.GraphSnapshot[reviewState]) error {
			// Demonstration only: production hosts authenticate the principal
			// before binding it to context; a client-supplied flag is not identity.
			if ctx.Value(reviewerKey{}) != "demo-reviewer" {
				return fmt.Errorf("reviewer is not authorized")
			}
			if snap.State.Decision != "approve" && snap.State.Decision != "reject" {
				return fmt.Errorf("human decision is required")
			}
			return nil
		}))
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, reviewerKey{}, *reviewer)
	var snapshot *workflow.GraphSnapshot[reviewState]
	switch *action {
	case "start":
		initial := reviewState{Text: *text, Status: "draft"}
		snapshot, err = graph.Run(ctx, *id, initial, "")
	case "status":
		snapshot, err = graph.Snapshot(ctx, *id)
	case "resume":
		var raw []byte
		raw, err = json.Marshal(humanResponse{Decision: *decision, Text: *text})
		if err == nil {
			snapshot, err = graph.Resume(ctx, *id, workflow.ResumeRequest{Key: *key, Input: raw})
		}
	default:
		return fmt.Errorf("unknown action %q", *action)
	}
	if err != nil && (err != durable.ErrApprovalRequired || snapshot == nil || !snapshot.Waiting) {
		return err
	}
	if snapshot == nil {
		return errors.New("missing checkpoint")
	}
	return json.Encode(output, snapshot, json.Pretty)
}
