package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/workflow"
)

type replyState struct {
	ModelReview *modelEvidence `json:"model_review,omitempty"`
	MessageID   string         `json:"message_id"`
	Recipient   string         `json:"recipient"`
	Message     string         `json:"message"`
	Answer      string         `json:"answer"`
	Source      string         `json:"source"`
	Outcome     string         `json:"outcome"`
	Receipt     string         `json:"receipt,omitempty"`
}

const replySchema = `{"type":"object","properties":{"id":{"type":"string","minLength":1},"recipient":{"type":"string","enum":["demo-user"]},"message":{"type":"string","minLength":1},"answer":{"type":"string","minLength":1},"source":{"type":"string","minLength":1}},"required":["id","recipient","message","answer","source"],"additionalProperties":false}`

func replyGraph(ctx context.Context, store durable.Store, backend *demoBackend) (*workflow.DurableGraph[replyState], error) {
	g := workflow.NewGraph[replyState]()
	if err := g.AddNode("retrieve", func(ctx context.Context, s replyState) (replyState, error) {
		if strings.TrimSpace(s.MessageID) == "" || strings.TrimSpace(s.Message) == "" || s.Recipient != "demo-user" {
			return s, fmt.Errorf("message ID, message and allowlisted demo-user recipient required")
		}
		// Exact FAQ matching intentionally fails closed for unsupported questions.
		s.Answer, s.Source, s.Receipt = "", "", ""
		s.ModelReview = nil
		s.Outcome = "human_handoff"
		switch strings.TrimSpace(s.Message) {
		case "营业时间是什么？":
			s.Source = "demo-faq-v1/hours"
		case "如何重置密码？":
			s.Source = "demo-faq-v1/password"
		}
		if backend.model != nil {
			review, err := backend.model.judge(ctx, "auto-reply", map[string]string{"message": s.Message})
			if err != nil {
				return s, err
			}
			s.ModelReview, s.Source = review, review.Source
		}
		if s.Source != "" {
			s.Answer, _ = faqAnswer(s.Source)
			s.Outcome = "ready"
		}
		return s, ctx.Err()
	}); err != nil {
		return nil, err
	}
	d, err := dispatcher(backend, "send_reply", replySchema)
	if err != nil {
		return nil, err
	}
	if err = addEffect(g, d, "send", "send_reply", func(s replyState) map[string]any {
		return map[string]any{"id": s.MessageID, "recipient": s.Recipient, "message": s.Message, "answer": s.Answer, "source": s.Source}
	}, func(s replyState, receipt string) replyState { s.Receipt, s.Outcome = receipt, "replied"; return s }); err != nil {
		return nil, err
	}
	if err = g.AddBranch("retrieve", func(_ context.Context, s replyState) (string, error) {
		if s.Outcome == "ready" {
			return "send", nil
		}
		return workflow.END, nil
	}, "send", workflow.END); err != nil {
		return nil, err
	}
	if err = edges(g, [2]string{workflow.START, "retrieve"}, [2]string{"send", workflow.END}); err != nil {
		return nil, err
	}
	return g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: backend.version("reply-demo-v1")})
}
