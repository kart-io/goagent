package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

func TestMiniMaxRegionConfig(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "")
	t.Setenv("MINIMAX_CN_API_KEY", "cn-test-key")
	t.Setenv("MINIMAX_BASE_URL", "")
	key, base, err := minimaxConfig()
	require.NoError(t, err)
	require.Equal(t, "cn-test-key", key)
	require.Equal(t, "https://api.minimax.cn/v1", base)
	t.Setenv("MINIMAX_API_KEY", "global-test-key")
	key, base, err = minimaxConfig()
	require.NoError(t, err)
	require.Equal(t, "global-test-key", key)
	require.Equal(t, "https://api.minimax.io/v1", base)
	for _, url := range []string{"file:///tmp/key", "https://user:password@host/v1", "https://host/v1?key=secret", "https://host/v1#fragment"} {
		t.Setenv("MINIMAX_BASE_URL", url)
		_, _, err = minimaxConfig()
		require.Error(t, err)
	}
	t.Setenv("MINIMAX_BASE_URL", "")
	t.Setenv("MINIMAX_API_KEY", "")
	t.Setenv("MINIMAX_CN_API_KEY", "")
	_, err = newModelJudge("minimax", "MiniMax-M3.1-Flash")
	require.ErrorContains(t, err, "required")
}

func TestMiniMaxCLIProtocolAndReplay(t *testing.T) {
	var calls atomic.Int32
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer cn-test-key" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"MiniMax-M3.1-Flash","choices":[{"message":{"content":"{\"decision\":\"reply\",\"reason\":\"FAQ match\",\"source\":\"demo-faq-v1/hours\"}","reasoning_content":"private-reasoning"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":24,"total_tokens":36}}`)
	}))
	defer server.Close()
	t.Setenv("MINIMAX_API_KEY", "")
	t.Setenv("MINIMAX_CN_API_KEY", "cn-test-key")
	t.Setenv("MINIMAX_BASE_URL", server.URL+"/v1/")
	t.Setenv("OPENAI_API_KEY", "unrelated-key")
	t.Setenv("OPENAI_BASE_URL", "https://unused.invalid")
	args := []string{"-live", "-provider", "minimax", "-model", "MiniMax-M3.1-Flash", "-case", "auto-reply", "-dir", t.TempDir()}
	var output bytes.Buffer
	require.NoError(t, runCLI(context.Background(), args, &output))
	require.NotContains(t, output.String(), "private-reasoning")
	var first struct {
		Live     bool `json:"live_model"`
		Calls    int  `json:"model_calls_this_invocation"`
		Snapshot struct {
			State replyState
			Done  bool
		}
	}
	require.NoError(t, json.Unmarshal(output.Bytes(), &first))
	require.True(t, first.Live && first.Snapshot.Done)
	require.Equal(t, 1, first.Calls)
	require.Equal(t, "minimax", first.Snapshot.State.ModelReview.Provider)
	require.Equal(t, 36, first.Snapshot.State.ModelReview.Usage.TotalTokens)
	request := <-requests
	require.Equal(t, "MiniMax-M3.1-Flash", request["model"])
	require.Equal(t, true, request["reasoning_split"])
	require.Equal(t, "low", request["reasoning_effort"])
	require.EqualValues(t, modelMaxTokens, request["max_completion_tokens"])
	require.NotContains(t, request, "tools")
	output.Reset()
	require.NoError(t, runCLI(context.Background(), args, &output))
	var repeat struct {
		Calls int `json:"model_calls_this_invocation"`
	}
	require.NoError(t, json.Unmarshal(output.Bytes(), &repeat))
	require.Zero(t, repeat.Calls)
	require.EqualValues(t, 1, calls.Load())
}

func TestMiniMaxErrorsFailWithoutRetries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"rate_limit", 429, `{"error":"echoed-secret"}`},
		{"server_error", 500, `private-reasoning`},
		{"redirect", 307, ""},
		{"api_error", 200, `{"base_resp":{"status_code":1004}}`},
		{"openai_error", 200, `{"error":{"message":"echoed-secret"}}`},
		{"empty_choices", 200, `{"choices":[]}`},
		{"invalid_json", 200, `not JSON`},
		{"oversized", 200, strings.Repeat("x", (2<<20)+1)},
		{"truncated", 200, `{"choices":[{"message":{"content":""},"finish_reason":"length"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Location", "/unexpected-redirect")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			t.Setenv("MINIMAX_API_KEY", "test-key")
			t.Setenv("MINIMAX_BASE_URL", server.URL+"/v1")
			m, err := newModelJudge("minimax", "MiniMax-M3.1-Flash")
			require.NoError(t, err)
			_, err = m.judge(context.Background(), "auto-reply", "question")
			require.Error(t, err)
			require.NotContains(t, err.Error(), "echoed-secret")
			require.NotContains(t, err.Error(), "private-reasoning")
			require.EqualValues(t, 1, calls.Load())
		})
	}
}

func TestMiniMaxCanceledRequestAndRedaction(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "global-test-secret")
	t.Setenv("MINIMAX_CN_API_KEY", "cn-test-secret")
	t.Setenv("MINIMAX_BASE_URL", "https://unused.invalid/v1")
	client, err := newMiniMaxCompletion()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Complete(ctx, &llm.CompletionRequest{Model: "MiniMax-M3.1-Flash", MaxTokens: 50, Messages: []llm.Message{{Role: "user", Content: "test"}}})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, "[REDACTED] [REDACTED]", redactModelError("global-test-secret cn-test-secret"))
}
