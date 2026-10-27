package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/workflow"
)

type devopsState struct {
	ModelReview    *modelEvidence `json:"model_review,omitempty"`
	IncidentID     string         `json:"incident_id"`
	Service        string         `json:"service"`
	Healthy        bool           `json:"healthy"`
	ErrorPercent   int            `json:"error_percent"`
	HealthChecked  bool           `json:"health_checked"`
	MetricsChecked bool           `json:"metrics_checked"`
	Outcome        string         `json:"outcome"`
	Receipt        string         `json:"receipt,omitempty"`
}

const restartSchema = `{"type":"object","properties":{"id":{"type":"string","minLength":1},"service":{"type":"string","enum":["demo-api"]}},"required":["id","service"],"additionalProperties":false}`

func devopsGraph(ctx context.Context, store durable.Store, backend *demoBackend) (*workflow.DurableGraph[devopsState], error) {
	g := workflow.NewGraph[devopsState](workflow.Config{MaxConcurrency: 2})
	err := g.AddParallel("inspect", workflow.ParallelConfig[devopsState]{
		Branches: []workflow.Branch[devopsState]{
			{Name: "health", Run: func(ctx context.Context, s devopsState) (devopsState, error) {
				if strings.TrimSpace(s.IncidentID) == "" || s.Service != "demo-api" {
					return s, fmt.Errorf("incident ID and allowlisted demo-api service required")
				}
				s.HealthChecked = true
				return s, ctx.Err()
			}},
			{Name: "metrics", Run: func(ctx context.Context, s devopsState) (devopsState, error) {
				if s.ErrorPercent < 0 || s.ErrorPercent > 100 {
					return s, fmt.Errorf("error_percent must be between 0 and 100")
				}
				s.MetricsChecked = true
				return s, ctx.Err()
			}},
		},
		Clone: func(s devopsState) devopsState { return s },
		Merge: func(_ context.Context, s devopsState, results []devopsState) (devopsState, error) {
			s.HealthChecked, s.MetricsChecked = results[0].HealthChecked, results[1].MetricsChecked
			return s, nil
		},
	})
	if err != nil {
		return nil, err
	}
	if err = g.AddNode("diagnose", func(ctx context.Context, s devopsState) (devopsState, error) {
		s.Receipt = ""
		s.ModelReview = nil
		switch {
		case s.Healthy && s.ErrorPercent < 5:
			s.Outcome = "healthy"
		case !s.Healthy && s.ErrorPercent >= 5:
			s.Outcome = "restart_required"
		default:
			s.Outcome = "escalated"
		}
		if backend.model != nil {
			review, err := backend.model.judge(ctx, "devops", map[string]any{"service": s.Service, "healthy": s.Healthy, "error_percent": s.ErrorPercent})
			if err != nil {
				return s, err
			}
			s.ModelReview = review
			expected := map[string]string{"healthy": "observe", "restart_required": "restart", "escalated": "escalate"}[s.Outcome]
			// Model disagreement cannot broaden the deterministic restart policy.
			if review.Decision != expected {
				s.Outcome = "escalated"
			}
		}
		return s, nil
	}); err != nil {
		return nil, err
	}
	d, err := dispatcher(backend, "restart_service", restartSchema)
	if err != nil {
		return nil, err
	}
	if err = addEffect(g, d, "restart", "restart_service", func(s devopsState) map[string]any {
		return map[string]any{"id": s.IncidentID, "service": s.Service}
	}, func(s devopsState, receipt string) devopsState { s.Receipt = receipt; return s }); err != nil {
		return nil, err
	}
	if err = g.AddNode("verify", func(ctx context.Context, s devopsState) (devopsState, error) {
		// The fake service's only state transition is its committed restart record.
		if _, err := backend.store.Load(ctx, effectID("restart_service", s.IncidentID)); err != nil {
			return s, err
		}
		s.Healthy, s.ErrorPercent, s.Outcome = true, 0, "recovered"
		return s, nil
	}); err != nil {
		return nil, err
	}
	if err = g.AddBranch("diagnose", func(_ context.Context, s devopsState) (string, error) {
		if s.Outcome == "restart_required" {
			return "restart", nil
		}
		return workflow.END, nil
	}, "restart", workflow.END); err != nil {
		return nil, err
	}
	if err = edges(g, [2]string{workflow.START, "inspect"}, [2]string{"inspect", "diagnose"}, [2]string{"restart", "verify"}, [2]string{"verify", workflow.END}); err != nil {
		return nil, err
	}
	return g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: backend.version("devops-demo-v1"), ApprovalNodes: []string{"restart"}})
}
