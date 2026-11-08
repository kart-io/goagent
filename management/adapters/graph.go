package adapters

import (
	"context"
	"errors"
	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/workflow"
	"os"
)

// GraphFactory 的 BuildGraph 必须为所选版本配置明确的批准/拒绝分支及 ResumeHandler。
type GraphFactory[T any] struct {
	Check      func(context.Context, mg.Version) error
	BuildGraph func(context.Context, mg.Version, mg.Resources) (*workflow.DurableGraph[T], func(context.Context) error, error)
	Initial    func(json.RawMessage) (T, error)
}

func (f GraphFactory[T]) Validate(ctx context.Context, v mg.Version) (mg.Capabilities, error) {
	if f.Check == nil || f.BuildGraph == nil || f.Initial == nil {
		return mg.Capabilities{}, agentErrors.ErrManagementInvalid
	}
	return mg.Capabilities{Approval: true, Input: true}, f.Check(ctx, v)
}
func (f GraphFactory[T]) Build(ctx context.Context, v mg.Version, r mg.Resources) (mg.Binding, error) {
	g, cleanup, err := f.BuildGraph(ctx, v, r)
	return mg.Binding{Runtime: &graph[T]{g: g, initial: f.Initial, r: r}, Cleanup: cleanup}, err
}

type graph[T any] struct {
	g       *workflow.DurableGraph[T]
	initial func(json.RawMessage) (T, error)
	r       mg.Resources
}

func (g *graph[T]) result(snap *workflow.GraphSnapshot[T]) (mg.RunResult, error) {
	if snap.Done {
		raw, err := json.Marshal(snap.State)
		return mg.RunResult{Outcome: "completed", Output: raw}, err
	}
	out := mg.RunResult{Outcome: "waiting"}
	if len(snap.Interrupts) > 0 {
		for _, p := range snap.Interrupts {
			info := p.Info
			intent := hash(info)
			out.Waits = append(out.Waits, mg.WaitPoint{ID: hash([]byte(p.Key)), Kind: "input", Key: p.Key, InterruptID: p.ID, IntentHash: intent, Info: info, Rejected: p.Rejected, Reason: p.Reason})
		}
	} else {
		info, _ := json.Marshal(map[string]any{"node": snap.Node})
		out.Waits = []mg.WaitPoint{{ID: hash([]byte(snap.Key)), Kind: "approval", Key: snap.Key, InterruptID: "graph/" + snap.Key, IntentHash: hash(info), Info: info}}
	}
	return out, nil
}
func (g *graph[T]) Run(ctx context.Context, spec mg.RunSpec) (mg.RunResult, error) {
	initial, err := g.initial(spec.Task.Input)
	if err != nil {
		return mg.RunResult{}, err
	}
	saved, err := g.g.Snapshot(ctx, spec.Task.RunID)
	var snap *workflow.GraphSnapshot[T]
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return mg.RunResult{}, err
	}
	if saved != nil && saved.Waiting {
		input, key := "", ""
		if len(saved.Interrupts) == 0 {
			if v, ok := spec.ResumeInputs[saved.Key]; ok {
				key, input = saved.Key, v
			}
		} else {
			for _, point := range saved.Interrupts {
				if v, ok := spec.ResumeInputs[point.Key]; ok {
					key, input = point.Key, v
					break
				}
			}
		}
		if key != "" {
			if len(saved.Interrupts) == 0 {
				var answer mg.ApprovalAnswer
				if json.Unmarshal([]byte(input), &answer) != nil {
					return mg.RunResult{}, agentErrors.ErrManagementInvalid
				}
				ap, e := g.r.ApprovalReader(ctx, answer.ApprovalID)
				if e != nil {
					return mg.RunResult{}, e
				}
				if ap.TaskID != spec.Task.ID || ap.Decision != answer.Decision || ap.Wait.Key != key || (ap.State != "approved" && ap.State != "rejected") {
					return mg.RunResult{}, agentErrors.ErrManagementForbidden
				}
			}
			snap, err = g.g.Resume(ctx, spec.Task.RunID, workflow.ResumeRequest{Key: key, Input: json.RawMessage(input)})
		} else {
			snap = saved
		}
	} else {
		snap, err = g.g.Run(ctx, spec.Task.RunID, initial, "")
	}
	var rejected *workflow.ResumeRejectedError
	if errors.As(err, &rejected) {
		current, e := g.g.Snapshot(ctx, spec.Task.RunID)
		if e != nil {
			return mg.RunResult{}, e
		}
		result, e := g.result(current)
		for i := range result.Waits {
			result.Waits[i].Rejected = true
			if result.Waits[i].Reason == "" {
				result.Waits[i].Reason = "validation_rejected"
			}
		}
		return result, e
	}
	if err != nil && !errors.Is(err, agentErrors.ErrApprovalRequired) {
		return mg.RunResult{}, err
	}
	if snap == nil {
		return mg.RunResult{}, agentErrors.ErrManagementInvalid
	}
	return g.result(snap)
}
func (g *graph[T]) Inspect(ctx context.Context, spec mg.RunSpec) (mg.RunEvidence, error) {
	snap, err := g.g.Snapshot(ctx, spec.Task.RunID)
	if errors.Is(err, os.ErrNotExist) {
		return mg.RunEvidence{State: "absent"}, nil
	}
	if err != nil {
		return mg.RunEvidence{}, err
	}
	if snap.Done {
		result, e := g.result(snap)
		return mg.RunEvidence{State: "completed", Result: result}, e
	}
	return mg.RunEvidence{State: "waiting"}, nil
}
