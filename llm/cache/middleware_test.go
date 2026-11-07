package cache

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	shared "github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/llm"
)

type countingStream struct {
	calls  int
	answer string
	err    error
}

func (c *countingStream) CompleteStream(context.Context, *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	c.calls++
	out := make(chan *llm.StreamChunk, 2)
	if c.err != nil {
		out <- &llm.StreamChunk{Delta: "部分", Done: true, Error: c.err}
	} else {
		out <- &llm.StreamChunk{Delta: c.answer}
		out <- &llm.StreamChunk{Content: c.answer, FinishReason: "stop", Done: true, Usage: &llm.Usage{TotalTokens: 4}}
	}
	close(out)
	return out, nil
}

func (c *countingStream) Provider() shared.Provider { return shared.ProviderCustom }

func newCached(t *testing.T, upstream llm.Client) *CachedLLMClient {
	t.Helper()
	client, err := NewCachedLLMClient(&CachedLLMClientConfig{Client: upstream, EmbeddingProvider: NewMockEmbeddingProvider(16)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestCachedLLMClientServesHitsWithoutUpstream(t *testing.T) {
	upstream := &countingStream{answer: "答案"}
	client := newCached(t, upstream)
	req := &llm.CompletionRequest{Messages: []llm.Message{llm.UserMessage("同一个问题")}}

	first, err := llm.Complete(context.Background(), client, req)
	require.NoError(t, err)
	require.Equal(t, "答案", first.Content)
	require.Equal(t, "stop", first.FinishReason)

	second, err := llm.Complete(context.Background(), client, req)
	require.NoError(t, err)
	require.Equal(t, "答案", second.Content)
	require.Equal(t, "cache_hit", second.FinishReason)
	require.Equal(t, 1, upstream.calls, "命中缓存时不请求上游")
}

func TestCachedLLMClientDoesNotCacheFailedStreams(t *testing.T) {
	sentinel := errors.New("流中断")
	upstream := &countingStream{err: sentinel}
	client := newCached(t, upstream)
	req := &llm.CompletionRequest{Messages: []llm.Message{llm.UserMessage("问题")}}
	for range 2 {
		_, err := llm.Complete(context.Background(), client, req)
		require.ErrorIs(t, err, sentinel)
	}
	require.Equal(t, 2, upstream.calls, "失败的回答不能进入缓存")
}
