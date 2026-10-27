package main

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/workflow"
)

type articleState struct {
	ModelReview *modelEvidence `json:"model_review,omitempty"`
	ArticleID   string         `json:"article_id"`
	Title       string         `json:"title"`
	Body        string         `json:"body"`
	Safety      string         `json:"safety"`
	Quality     string         `json:"quality"`
	Decision    string         `json:"decision"`
	Receipt     string         `json:"receipt,omitempty"`
}

const publishSchema = `{"type":"object","properties":{"id":{"type":"string","minLength":1},"title":{"type":"string","minLength":1},"body":{"type":"string","minLength":1}},"required":["id","title","body"],"additionalProperties":false}`

func articleGraph(ctx context.Context, store durable.Store, backend *demoBackend) (*workflow.DurableGraph[articleState], error) {
	g := workflow.NewGraph[articleState](workflow.Config{MaxConcurrency: 2})
	err := g.AddParallel("review", workflow.ParallelConfig[articleState]{
		Branches: []workflow.Branch[articleState]{
			{Name: "content_rules", Run: func(ctx context.Context, s articleState) (articleState, error) {
				if strings.TrimSpace(s.ArticleID) == "" || strings.TrimSpace(s.Title) == "" || strings.TrimSpace(s.Body) == "" {
					return s, fmt.Errorf("article ID, title and body required")
				}
				// Deliberately explicit test markers, NOT a production moderation model.
				text := strings.ToUpper(s.Title + "\n" + s.Body)
				s.Safety = "pass"
				if strings.Contains(text, "DEMO_BLOCK") {
					s.Safety = "block"
				} else if strings.Contains(text, "DEMO_REVIEW") {
					s.Safety = "review"
				}
				s.ModelReview = nil
				if backend.model != nil {
					review, err := backend.model.judge(ctx, "article-review", map[string]string{"title": s.Title, "body": s.Body})
					if err != nil {
						return s, err
					}
					s.ModelReview = review
					if review.Decision == "reject" {
						s.Safety = "block"
					} else if review.Decision == "review" && s.Safety != "block" {
						s.Safety = "review"
					}
				}
				return s, ctx.Err()
			}},
			{Name: "quality_rules", Run: func(ctx context.Context, s articleState) (articleState, error) {
				s.Quality = "pass"
				if utf8.RuneCountInString(strings.TrimSpace(s.Body)) < 80 {
					s.Quality = "review"
				}
				return s, ctx.Err()
			}},
		}, Clone: func(s articleState) articleState { return s },
		Merge: func(_ context.Context, s articleState, results []articleState) (articleState, error) {
			s.Safety, s.Quality = results[0].Safety, results[1].Quality
			s.ModelReview = results[0].ModelReview
			s.Receipt = ""
			switch {
			case s.Safety == "block":
				s.Decision = "rejected"
			case s.Safety == "review" || s.Quality == "review":
				s.Decision = "manual_review"
			default:
				s.Decision = "accepted"
			}
			return s, nil
		},
	})
	if err != nil {
		return nil, err
	}
	d, err := dispatcher(backend, "publish_article", publishSchema)
	if err != nil {
		return nil, err
	}
	for _, node := range []string{"publish", "publish_reviewed"} {
		if err = addEffect(g, d, node, "publish_article", func(s articleState) map[string]any {
			return map[string]any{"id": s.ArticleID, "title": s.Title, "body": s.Body}
		}, func(s articleState, receipt string) articleState {
			s.Receipt, s.Decision = receipt, "published"
			return s
		}); err != nil {
			return nil, err
		}
	}
	if err = g.AddBranch("review", func(_ context.Context, s articleState) (string, error) {
		switch s.Decision {
		case "rejected":
			return workflow.END, nil
		case "manual_review":
			return "publish_reviewed", nil
		default:
			return "publish", nil
		}
	}, workflow.END, "publish_reviewed", "publish"); err != nil {
		return nil, err
	}
	if err = edges(g, [2]string{workflow.START, "review"}, [2]string{"publish", workflow.END}, [2]string{"publish_reviewed", workflow.END}); err != nil {
		return nil, err
	}
	return g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: backend.version("article-demo-v1"), ApprovalNodes: []string{"publish_reviewed"}})
}
