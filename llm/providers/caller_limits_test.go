package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
)

func writeCompletion(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
}

func complete(p *OpenAIProvider) error {
	stream, err := p.CompleteStream(context.Background(), &agentllm.CompletionRequest{Messages: []agentllm.Message{{Role: "user", Content: "q"}}})
	if err != nil {
		return err
	}
	for chunk := range stream {
		if chunk.Error != nil {
			return chunk.Error
		}
	}
	return nil
}

func TestMaxTokensNotSentByDefaultOpenAICompatible(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []agentllm.ClientOption
		want any
	}{
		{"默认不发送", nil, nil},
		{"显式 0 不发送", []agentllm.ClientOption{agentllm.WithMaxTokens(0)}, nil},
		{"显式设置时发送", []agentllm.ClientOption{agentllm.WithMaxTokens(300)}, float64(300)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				require.NoError(t, json.Unmarshal(raw, &body))
				writeCompletion(w)
			}))
			defer server.Close()
			p, err := NewOllamaWithOptions(append([]agentllm.ClientOption{agentllm.WithBaseURL(server.URL + "/v1")}, tc.opts...)...)
			require.NoError(t, err)
			require.NoError(t, complete(p))
			require.Equal(t, tc.want, body["max_tokens"], "旧默认每次都发送 2000")
		})
	}
}

func TestAnthropicMaxTokensRequiredDefault(t *testing.T) {
	req := &agentllm.CompletionRequest{Messages: []agentllm.Message{{Role: "user", Content: "q"}}}
	for _, tc := range []struct {
		name string
		opts []agentllm.ClientOption
		want int64
	}{
		{"未设置时沿用 2000", nil, 2000},
		{"0 表示默认", []agentllm.ClientOption{agentllm.WithMaxTokens(0)}, 2000},
		{"显式值", []agentllm.ClientOption{agentllm.WithMaxTokens(500)}, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewAnthropicWithOptions(append([]agentllm.ClientOption{agentllm.WithAPIKey("k")}, tc.opts...)...)
			require.NoError(t, err)
			params, err := p.params(req, nil)
			require.NoError(t, err)
			require.Equal(t, tc.want, params.MaxTokens)
		})
	}
	require.EqualValues(t, 2000, constants.DefaultClientMaxTokens)
	_, err := NewAnthropicWithOptions(agentllm.WithAPIKey("k"), agentllm.WithMaxTokens(-1))
	require.Error(t, err)
}

func TestLLMTimeoutZeroIsUnlimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1500 * time.Millisecond)
		writeCompletion(w)
	}))
	defer server.Close()
	call := func(opts ...agentllm.ClientOption) error {
		p, err := NewOllamaWithOptions(append([]agentllm.ClientOption{agentllm.WithBaseURL(server.URL + "/v1")}, opts...)...)
		require.NoError(t, err)
		return complete(p)
	}
	require.NoError(t, call(), "未设置超时")
	require.NoError(t, call(agentllm.WithTimeout(0)), "旧实现把 0 回落到 30s")
	require.Error(t, call(agentllm.WithTimeout(time.Second)), "显式超时仍生效")

	base := common.NewBaseProvider()
	require.Zero(t, base.GetTimeout())
	ctx, cancel := base.WithTimeout(context.Background())
	defer cancel()
	_, has := ctx.Deadline()
	require.False(t, has, "Gemini 等以 ctx 计时的 provider 不得立即到期")
}
