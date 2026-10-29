package providers_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/stretchr/testify/require"
)

func requireProviderError(t *testing.T, err error, status int, retryAfter time.Duration, retryable bool) {
	t.Helper()
	var pe *llm.ProviderError
	require.True(t, errors.As(err, &pe), "%v", err)
	require.Equal(t, status, pe.StatusCode)
	require.Equal(t, retryAfter, pe.RetryAfter)
	require.Equal(t, retryable, pe.Retryable())
}

func TestOpenAICompatibleStreamsExposeProviderError(t *testing.T) {
	for _, name := range []string{"openai", "deepseek"} {
		for _, tc := range []struct {
			status     int
			header     string
			retryAfter time.Duration
			retryable  bool
		}{
			{429, "2", 2 * time.Second, true},
			{429, "", 0, true},
			{503, "1", time.Second, true},
			{400, "", 0, false},
		} {
			t.Run(fmt.Sprintf("%s/%d/%q", name, tc.status, tc.header), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if tc.header != "" {
						w.Header().Set("Retry-After", tc.header)
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.status)
					fmt.Fprint(w, `{"error":{"message":"upstream says no","type":"x"}}`)
				}))
				defer server.Close()
				_, err := contractProvider(t, name, server.URL).CompleteToolsStream(context.Background(), &llm.CompletionRequest{Messages: []llm.Message{llm.UserMessage("q")}}, nil)
				require.Error(t, err)
				requireProviderError(t, err, tc.status, tc.retryAfter, tc.retryable)
			})
		}
	}
}

func TestOpenAIStreamBrokenMidwayIsUnstatusedProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n")
		w.(http.Flusher).Flush()
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()
	stream, err := contractProvider(t, "openai", server.URL).CompleteStream(context.Background(), &llm.CompletionRequest{Messages: []llm.Message{llm.UserMessage("q")}})
	require.NoError(t, err)
	var last *llm.StreamChunk
	for chunk := range stream {
		last = chunk
	}
	require.NotNil(t, last.Error)
	var pe *llm.ProviderError
	require.True(t, errors.As(last.Error, &pe), "%v", last.Error)
	require.Zero(t, pe.StatusCode)
	require.True(t, pe.Retryable(), "%v", last.Error)
}

func TestGeminiStreamExposesProviderErrorAndRetryDelay(t *testing.T) {
	for _, tc := range []struct {
		status     int
		body       string
		retryAfter time.Duration
		retryable  bool
	}{
		{429, `{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"3s"}]}}`, 3 * time.Second, true},
		{503, `{"error":{"code":503,"message":"overloaded","status":"UNAVAILABLE"}}`, 0, true},
		{400, `{"error":{"code":400,"message":"bad","status":"INVALID_ARGUMENT"}}`, 0, false},
	} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			p, err := providers.NewGeminiWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL))
			require.NoError(t, err)
			_, err = p.CompleteToolsStream(context.Background(), &llm.CompletionRequest{Messages: []llm.Message{llm.UserMessage("q")}}, nil)
			require.Error(t, err)
			requireProviderError(t, err, tc.status, tc.retryAfter, tc.retryable)
		})
	}
}
