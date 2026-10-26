package retrieval

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

type contractVector struct{ interfaces.VectorStore }

func (*contractVector) SimilaritySearch(context.Context, string, int) ([]*interfaces.Document, error) {
	return []*interfaces.Document{{ID: "refund", PageContent: "refund policy", Score: 1}}, nil
}

type contractModel struct{}

func (*contractModel) Complete(context.Context, *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{Content: "refund"}, nil
}
func (m *contractModel) Chat(ctx context.Context, msgs []llm.Message) (*llm.CompletionResponse, error) {
	return m.Complete(ctx, &llm.CompletionRequest{Messages: msgs})
}
func (*contractModel) Provider() constants.Provider { return constants.ProviderCustom }
func (*contractModel) IsAvailable() bool            { return true }

func TestConcreteRetrieverRunnableContract(t *testing.T) {
	ctx := context.Background()
	config := DefaultRetrieverConfig()
	keyword := NewKeywordRetriever([]*interfaces.Document{{ID: "refund", PageContent: "refund policy"}}, config)
	vector := NewVectorStoreRetriever(&contractVector{}, config)
	for name, r := range map[string]Retriever{
		"keyword":     keyword,
		"vector":      vector,
		"hybrid":      NewHybridRetriever(vector, keyword, .5, .5, config),
		"ensemble":    NewEnsembleRetriever([]Retriever{vector, keyword}, []float64{.5, .5}, config),
		"multi_query": NewMultiQueryRetriever(keyword, &contractModel{}, 1, config),
		"reranking":   NewRerankingRetriever(keyword, NewBaseReranker("identity"), 4, config),
	} {
		t.Run(name, func(t *testing.T) {
			want, err := r.GetRelevantDocuments(ctx, "refund")
			require.NoError(t, err)
			require.NotEmpty(t, want)
			got, err := r.Invoke(ctx, "refund")
			require.NoError(t, err)
			require.Equal(t, want, got)
			chunks, err := r.Stream(ctx, "refund")
			require.NoError(t, err)
			count := 0
			for chunk := range chunks {
				count++
				require.True(t, chunk.Done)
				require.NoError(t, chunk.Error)
				require.Equal(t, want, chunk.Data)
			}
			require.Equal(t, 1, count)
			batch, err := r.Batch(ctx, []string{"refund", "refund"})
			require.NoError(t, err)
			require.Equal(t, [][]*interfaces.Document{want, want}, batch)
			for _, clone := range []core.Runnable[string, []*interfaces.Document]{r.WithCallbacks(&core.BaseCallback{}), r.WithConfig(core.RunnableConfig{MaxConcurrency: 1})} {
				got, err = clone.Invoke(ctx, "refund")
				require.NoError(t, err)
				require.Equal(t, want, got)
			}
			pipe := r.Pipe(core.NewRunnableFunc(func(_ context.Context, docs []*interfaces.Document) (any, error) { return docs[0].ID, nil }))
			value, err := pipe.Invoke(ctx, "refund")
			require.NoError(t, err)
			require.Equal(t, "refund", value)
		})
	}
}

type failingEndCallback struct {
	core.BaseCallback
	err error
}

func (c *failingEndCallback) OnEnd(context.Context, interface{}) error { return c.err }

func TestRetrieverErrorsAndCancellation(t *testing.T) {
	sentinel := errors.New("backend unavailable")
	r := NewFuncRetriever(func(context.Context, string) ([]*interfaces.Document, error) { return nil, sentinel })
	_, err := r.Invoke(context.Background(), "query")
	require.ErrorIs(t, err, sentinel)
	chunks, err := r.Stream(context.Background(), "query")
	require.NoError(t, err)
	chunk := <-chunks
	require.True(t, chunk.Done)
	require.ErrorIs(t, chunk.Error, sentinel)
	_, err = r.Batch(context.Background(), []string{"a", "b"})
	require.ErrorIs(t, err, sentinel)
	pipe := r.Pipe(core.NewRunnableFunc(func(context.Context, []*interfaces.Document) (any, error) {
		t.Error("downstream called after failure")
		return nil, nil
	}))
	_, err = pipe.Invoke(context.Background(), "query")
	require.ErrorIs(t, err, sentinel)
	chunks2, err := pipe.Stream(context.Background(), "query")
	require.NoError(t, err)
	c := <-chunks2
	require.True(t, c.Done)
	require.ErrorIs(t, c.Error, sentinel)
	good := NewFuncRetriever(func(context.Context, string) ([]*interfaces.Document, error) { return []*interfaces.Document{}, nil })
	_, err = good.WithCallbacks(&failingEndCallback{err: sentinel}).Invoke(context.Background(), "query")
	require.ErrorIs(t, err, sentinel)
	_, err = NewBaseRetriever().Invoke(context.Background(), "query")
	require.Error(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = good.Invoke(ctx, "query")
	require.ErrorIs(t, err, context.Canceled)
	_, err = good.Stream(ctx, "query")
	require.ErrorIs(t, err, context.Canceled)
	for _, combined := range []Retriever{NewEnsembleRetriever([]Retriever{r}, []float64{1}, DefaultRetrieverConfig()), NewMultiQueryRetriever(r, &contractModel{}, 1, DefaultRetrieverConfig())} {
		_, err := combined.Invoke(context.Background(), "query")
		require.ErrorIs(t, err, sentinel)
	}
}

func TestRetrieverBatchBoundsConcurrencyAndCancels(t *testing.T) {
	var active, peak atomic.Int32
	started := make(chan struct{}, 2)
	r := NewFuncRetriever(func(ctx context.Context, query string) ([]*interfaces.Document, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}).WithConfig(core.RunnableConfig{MaxConcurrency: 2})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := r.Batch(ctx, make([]string, 1000)); done <- err }()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("batch did not cancel")
	}
	require.LessOrEqual(t, peak.Load(), int32(2))
}

func TestHybridFailsFastAndCancelsSibling(t *testing.T) {
	sentinel := errors.New("backend failure")
	canceled := make(chan struct{})
	started := make(chan struct{})
	slow := NewFuncRetriever(func(ctx context.Context, _ string) ([]*interfaces.Document, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	})
	bad := NewFuncRetriever(func(context.Context, string) ([]*interfaces.Document, error) {
		<-started
		return nil, sentinel
	})
	hybrid := NewHybridRetriever(slow, bad, 0.5, 0.5, RetrieverConfig{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := hybrid.Invoke(ctx, "q")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected backend error, got %v", err)
	}
	select {
	case <-canceled:
	case <-ctx.Done():
		t.Fatal("sibling not canceled")
	}
}
func TestHybridUsesNormalizedScores(t *testing.T) {
	h := NewHybridRetriever(nil, nil, 0.75, 0.25, RetrieverConfig{})
	docs := h.weightedSumFusion(
		[]*interfaces.Document{{ID: "a", Score: 0.9}, {ID: "b", Score: 0.1}},
		[]*interfaces.Document{{ID: "a", Score: 1}, {ID: "b", Score: 100}},
	)
	scores := map[string]float64{}
	for _, d := range docs {
		scores[d.ID] = d.Score
	}
	if scores["a"] != 0.75 || scores["b"] != 0.25 {
		t.Fatalf("unnormalized scores: %v", scores)
	}
}
