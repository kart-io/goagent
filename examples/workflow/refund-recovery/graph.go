package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

// One tool effect per durable node; the dispatcher supplies that node's stable key.
type refundTool struct {
	ledger *refundLedger
	after  func()
}

func (*refundTool) Name() string { return "issue_refund" }
func (*refundTool) Description() string {
	return "Record an already reviewed and approved synthetic refund"
}
func (*refundTool) ArgsSchema() string {
	return `{"type":"object","properties":{"request_id":{"type":"string","minLength":1}},"required":["request_id"],"additionalProperties":false}`
}
func (t *refundTool) Invoke(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	id, ok := in.Args["request_id"].(string)
	if !ok {
		return nil, fmt.Errorf("request_id must be a string")
	}
	req, _, err := readRequest(ctx, t.ledger.db, id)
	if err != nil {
		return nil, err
	}
	receipt, err := t.ledger.refund(ctx, in.ExecutionKey, req)
	if err != nil {
		return nil, err
	}
	if t.after != nil {
		t.after()
	}
	return &interfaces.ToolOutput{Success: true, Result: receipt}, nil
}

func newRefundGraph(ctx context.Context, store durable.Store, ledger *refundLedger, ttl time.Duration, after func()) (*workflow.DurableGraph[refundState], error) {
	g := workflow.NewGraph[refundState](workflow.Config{MaxConcurrency: 2})
	if err := g.AddNode("restore_request", func(ctx context.Context, s refundState) (refundState, error) {
		existing, found, err := ledger.existingRequest(ctx, s)
		if err != nil {
			return s, err
		}
		if found {
			return existing, nil
		}
		return s, nil
	}); err != nil {
		return nil, err
	}
	if err := g.AddParallel("prepare", workflow.ParallelConfig[refundState]{
		Branches: []workflow.Branch[refundState]{
			{Name: "lookup_order", Run: func(ctx context.Context, s refundState) (refundState, error) {
				order, err := ledger.order(ctx, s.OrderID)
				if err != nil {
					return s, err
				}
				s.PaidCents, s.Currency, s.OrderVersion, s.DeliveryDays = order.PaidCents, order.Currency, order.Version, order.DeliveryDays
				return s, nil
			}},
			{Name: "validate_request", Run: func(ctx context.Context, s refundState) (refundState, error) {
				if s.RequestID == "" || s.OrderID == "" || s.AmountCents <= 0 {
					return s, fmt.Errorf("invalid refund intent")
				}
				return s, ctx.Err()
			}},
		}, Clone: func(s refundState) refundState { return s },
		Merge: func(_ context.Context, _ refundState, out []refundState) (refundState, error) { return out[0], nil },
	}); err != nil {
		return nil, err
	}
	if err := g.AddNode("register_refund", func(ctx context.Context, s refundState) (refundState, error) {
		s.PreparedAt = ledger.now().UTC().Format(time.RFC3339Nano)
		s.ValidUntil = ledger.now().Add(ttl).UTC().Format(time.RFC3339Nano)
		return ledger.register(ctx, s)
	}); err != nil {
		return nil, err
	}
	registry := tools.NewRegistry()
	if err := registry.Register(&refundTool{ledger: ledger, after: after}); err != nil {
		return nil, err
	}
	dispatcher, err := tools.NewDispatcher(registry, tools.DispatchConfig{MaxConcurrency: 1})
	if err != nil {
		return nil, err
	}
	node, err := workflow.Adapt(dispatcher.AsRunnable(), func(s refundState) (tools.ToolRequest, error) {
		return tools.ToolRequest{Name: "issue_refund", Input: &interfaces.ToolInput{Args: map[string]any{"request_id": s.RequestID}}}, nil
	}, func(s refundState, out *interfaces.ToolOutput) (refundState, error) {
		receipt, ok := out.Result.(string)
		if !ok || receipt == "" {
			return s, fmt.Errorf("missing refund receipt")
		}
		s.ReceiptID = receipt
		return s, nil
	})
	if err != nil {
		return nil, err
	}
	if err = g.AddRunnable("issue_refund", node); err != nil {
		return nil, err
	}
	if err = g.AddBranch("restore_request", func(_ context.Context, s refundState) (string, error) {
		if s.PreparedAt != "" {
			return "issue_refund", nil
		}
		return "prepare", nil
	}, "issue_refund", "prepare"); err != nil {
		return nil, err
	}
	for _, edge := range [][2]string{{workflow.START, "restore_request"}, {"prepare", "register_refund"}, {"register_refund", "issue_refund"}, {"issue_refund", workflow.END}} {
		if err = g.AddEdge(edge[0], edge[1]); err != nil {
			return nil, err
		}
	}
	return g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "refund-graph-v1", ApprovalNodes: []string{"issue_refund"}})
}

func runGraphCLI(ctx context.Context, store durable.Store, ledger *refundLedger, id string, initial refundState, key, actor string, ttl time.Duration, after func(), output io.Writer) error {
	run, err := newRefundGraph(ctx, store, ledger, ttl, after)
	if err != nil {
		return err
	}
	if key != "" {
		snapshot, err := run.Snapshot(ctx, id)
		if err != nil {
			return err
		}
		if !snapshot.Waiting || snapshot.Key != key {
			return fmt.Errorf("approval does not match waiting node")
		}
		if err = ledger.approve(ctx, snapshot.State, actor); err != nil {
			return err
		}
	}
	snapshot, err := run.Run(ctx, id, initial, key)
	status := "completed"
	if errors.Is(err, durable.ErrApprovalRequired) {
		status = "approval_required"
	} else if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(struct {
		Status   string                               `json:"status"`
		Snapshot *workflow.GraphSnapshot[refundState] `json:"snapshot"`
	}{status, snapshot})
}
