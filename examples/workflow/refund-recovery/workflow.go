package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/workflow"
	"github.com/kart-io/goagent/workflow/refund"
)

// refundState is the entire persisted business state. All orders are synthetic.
type refundState = refund.Request

func newRunner(ctx context.Context, store durable.Store, ledger *refundLedger, approvalTTL time.Duration, afterRefund func()) (*durable.Runner, error) {
	g := workflow.NewGraph[refundState]()
	if err := g.AddNode("lookup_order", func(ctx context.Context, state refundState) (refundState, error) {
		order, err := ledger.order(ctx, state.OrderID)
		if err != nil {
			return state, err
		}
		state.PaidCents, state.DeliveryDays = order.PaidCents, order.DeliveryDays
		state.Currency, state.OrderVersion = order.Currency, order.Version
		return state, nil
	}); err != nil {
		return nil, err
	}
	if err := g.AddNode("check_policy", func(ctx context.Context, state refundState) (refundState, error) {
		if state.AmountCents <= 0 || state.AmountCents > state.PaidCents || state.DeliveryDays > 7 {
			return state, fmt.Errorf("refund does not satisfy the synthetic policy")
		}
		state.PreparedAt = ledger.now().UTC().Format(time.RFC3339Nano)
		state.ValidUntil = ledger.now().Add(approvalTTL).UTC().Format(time.RFC3339Nano)
		return state, ctx.Err()
	}); err != nil {
		return nil, err
	}
	for _, edge := range [][2]string{{workflow.START, "lookup_order"}, {"lookup_order", "check_policy"}, {"check_policy", workflow.END}} {
		if err := g.AddEdge(edge[0], edge[1]); err != nil {
			return nil, err
		}
	}
	prepared, err := g.Compile(ctx)
	if err != nil {
		return nil, err
	}
	return &durable.Runner{Store: store, Version: "refund-demo-v2", Steps: []durable.Step{
		{Name: "prepare_refund", Run: func(ctx context.Context, in durable.StepInput) (json.RawMessage, error) {
			var state refundState
			if err := json.Unmarshal(in.Data, &state); err != nil {
				return nil, err
			}
			if existing, found, err := ledger.existingRequest(ctx, state); err != nil {
				return nil, err
			} else if found {
				return json.Marshal(existing)
			}
			state, err := prepared.Invoke(ctx, state)
			if err != nil {
				return nil, err
			}
			state, err = ledger.register(ctx, state)
			if err != nil {
				return nil, err
			}
			return json.Marshal(state)
		}},
		{Name: "issue_refund", RequireApproval: true, Run: func(ctx context.Context, in durable.StepInput) (json.RawMessage, error) {
			var state refundState
			if err := json.Unmarshal(in.Data, &state); err != nil {
				return nil, err
			}
			receipt, err := ledger.refund(ctx, in.Key, state)
			if err != nil {
				return nil, err
			}
			// The ledger has committed; the runner has not yet saved its result.
			// The CLI can exit here to exercise the ambiguous-effect crash window.
			if afterRefund != nil {
				afterRefund()
			}
			state.ReceiptID = receipt
			return json.Marshal(state)
		}},
	}}, nil
}
