package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// Rerank scores bounded candidates in one model request. Invalid or incomplete
// scores are errors: retrieval order is never silently substituted for model output.
// Candidate indices, rather than document IDs, also support duplicate/empty IDs.
func (l *LLMReranker) Rerank(ctx context.Context, query string, docs []*interfaces.Document) ([]*interfaces.Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return docs, nil
	}
	if l.Client == nil {
		return nil, fmt.Errorf("LLM reranker requires a client; use NewLLMRerankerWithClient")
	}
	limit, runes := l.MaxCandidates, l.MaxDocumentRunes
	if limit <= 0 {
		limit = 20
	}
	if runes <= 0 {
		runes = 4000
	}
	if len(docs) > limit {
		return nil, fmt.Errorf("rerank candidates %d exceed limit %d", len(docs), limit)
	}
	candidates := make([]string, len(docs))
	for i, doc := range docs {
		if doc == nil {
			return nil, fmt.Errorf("nil rerank candidate %d", i)
		}
		text := []rune(doc.PageContent)
		if len(text) > runes {
			text = text[:runes]
		}
		candidates[i] = string(text)
	}
	payload, err := json.Marshal(struct {
		Query      string   `json:"query"`
		Candidates []string `json:"candidates"`
	}{query, candidates})
	if err != nil {
		return nil, err
	}
	response, err := l.Client.Chat(ctx, []llm.Message{{Role: "system", Content: l.Prompt}, {Role: "user", Content: string(payload)}})
	if err != nil {
		return nil, fmt.Errorf("rerank model: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("rerank model returned nil response")
	}
	var result struct {
		Scores []struct {
			Index *int     `json:"index"`
			Score *float64 `json:"score"`
		} `json:"scores"`
	}
	// Accept a single JSON code fence, a common provider formatting choice.
	content := strings.TrimSpace(response.Content)
	if strings.HasPrefix(content, "```json\n") && strings.HasSuffix(content, "```") {
		content = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(content, "```json\n"), "```"))
	}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("invalid rerank JSON: %w", err)
	}
	if len(result.Scores) != len(docs) {
		return nil, fmt.Errorf("rerank response must score every candidate")
	}
	scored := make([]*interfaces.Document, len(docs))
	seen := make(map[int]bool, len(docs))
	for _, item := range result.Scores {
		if item.Index == nil || item.Score == nil {
			return nil, fmt.Errorf("rerank index and score are required")
		}
		i, score := *item.Index, *item.Score
		if i < 0 || i >= len(docs) || seen[i] || math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > 1 {
			return nil, fmt.Errorf("invalid or duplicate rerank score at index %d", i)
		}
		seen[i] = true
		scored[i] = docs[i].Clone()
		scored[i].Score = score
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
	if l.TopN > 0 && len(scored) > l.TopN {
		scored = scored[:l.TopN]
	}
	return scored, nil
}
