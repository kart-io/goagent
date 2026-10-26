package retrieval

import (
	"context"
	"errors"
	"testing"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

type scoringModel struct {
	contractModel
	content string
	err     error
	calls   int
}

func (m *scoringModel) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	m.calls++
	if len(messages) != 2 {
		return nil, errors.New("expected system and candidate messages")
	}
	return &llm.CompletionResponse{Content: m.content}, m.err
}
func TestLLMRerankerUsesModelAndRejectsInvalidScores(t *testing.T) {
	docs := []*interfaces.Document{{ID: "a", PageContent: "退款", Score: 9}, {ID: "b", PageContent: "运输"}}
	m := &scoringModel{content: `{"scores":[{"index":1,"score":0.9},{"index":0,"score":0.1}]}`}
	r := NewLLMRerankerWithClient(m, 1)
	got, err := r.Rerank(context.Background(), "退款", docs)
	if err != nil || len(got) != 1 || got[0].ID != "b" || m.calls != 1 || docs[0].Score != 9 {
		t.Fatalf("got %v err %v calls %d", got, err, m.calls)
	}
	for _, content := range []string{`garbage`, `{"scores":[]}`, `{"scores":[{"index":0,"score":0.1},{"index":0,"score":0.2}]}`, `{"scores":[{"index":0,"score":-1},{"index":1,"score":0.2}]}`, `{"scores":[{"index":0},{"index":1,"score":0.2}]}`} {
		m.content = content
		if _, err := r.Rerank(context.Background(), "q", docs); err == nil {
			t.Fatalf("accepted %s", content)
		}
	}
	sentinel := errors.New("upstream")
	m.err = sentinel
	if _, err := r.Rerank(context.Background(), "q", docs); !errors.Is(err, sentinel) {
		t.Fatalf("lost error: %v", err)
	}
	if _, err := NewLLMReranker(1).Rerank(context.Background(), "q", docs); err == nil {
		t.Fatal("missing client accepted")
	}
	r.MaxCandidates = 1
	if _, err := r.Rerank(context.Background(), "q", docs); err == nil {
		t.Fatal("candidate limit ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Rerank(ctx, "q", docs); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
func TestChineseKeywordRetrieval(t *testing.T) {
	docs := []*interfaces.Document{{ID: "refund", PageContent: "退款政策允许在购买后三十天内退货。"}, {ID: "delivery", PageContent: "标准配送需要三至五个工作日。"}}
	r := NewKeywordRetriever(docs, RetrieverConfig{TopK: 1})
	for query, want := range map[string]string{"退款政策": "refund", "配送": "delivery", "退": "refund", "refund退款": "refund"} {
		got, err := r.Invoke(context.Background(), query)
		if err != nil || len(got) != 1 || got[0].ID != want {
			t.Fatalf("%s: got %v err %v", query, got, err)
		}
	}
	got, err := r.Invoke(context.Background(), " ")
	if err != nil || len(got) != 0 {
		t.Fatalf("empty query: %v %v", got, err)
	}
}
