package providers_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/stretchr/testify/require"
)

type completer interface {
	Complete(context.Context, *llm.CompletionRequest) (*llm.CompletionResponse, error)
}

func completeProviders(t *testing.T, endpoint string) map[string]completer {
	t.Helper()
	opts := []llm.ClientOption{llm.WithAPIKey("test"), llm.WithBaseURL(endpoint), llm.WithModel("m"), llm.WithTimeout(3 * time.Second)}
	clients := map[string]completer{}
	add := func(name string, c completer, err error) {
		require.NoError(t, err, name)
		clients[name] = c
	}
	openai, err := providers.NewOpenAIWithOptions(opts...)
	add("openai", openai, err)
	deepseek, err := providers.NewDeepSeekWithOptions(opts...)
	add("deepseek", deepseek, err)
	anthropic, err := providers.NewAnthropicWithOptions(opts...)
	add("anthropic", anthropic, err)
	cohere, err := providers.NewCohereWithOptions(opts...)
	add("cohere", cohere, err)
	huggingface, err := providers.NewHuggingFaceWithOptions(opts...)
	add("huggingface", huggingface, err)
	ollama, err := providers.NewOllamaWithOptions(opts...)
	add("ollama", ollama, err)
	kimi, err := providers.NewKimiWithOptions(opts...)
	add("kimi", kimi, err)
	siliconflow, err := providers.NewSiliconFlowWithOptions(opts...)
	add("siliconflow", siliconflow, err)
	return clients
}

func TestCompleteErrorsAreClassified(t *testing.T) {
	t.Setenv("GO_TEST_MODE", "true")
	for _, tc := range []struct {
		status     int
		reason     llm.ErrorReason
		retryable  bool
		retryAfter time.Duration
	}{
		{http.StatusTooManyRequests, llm.ReasonRateLimit, true, 2 * time.Second},
		{http.StatusServiceUnavailable, llm.ReasonServer, true, 0},
		{http.StatusBadRequest, llm.ReasonBadRequest, false, 0},
		{http.StatusUnauthorized, llm.ReasonAuth, false, 0},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tc.retryAfter > 0 {
				w.Header().Set("Retry-After", fmt.Sprint(int(tc.retryAfter/time.Second)))
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(tc.status)
			fmt.Fprint(w, `{"error":{"message":"upstream says no"},"message":"upstream says no"}`)
		}))
		for name, client := range completeProviders(t, server.URL) {
			t.Run(fmt.Sprintf("%s/%d", name, tc.status), func(t *testing.T) {
				_, err := client.Complete(context.Background(), &llm.CompletionRequest{Messages: []llm.Message{llm.UserMessage("q")}})
				require.Error(t, err)
				class := llm.ClassifyError(err)
				require.Equal(t, tc.reason, class.Reason, "%v", err)
				require.Equal(t, tc.retryable, class.Retryable, "%v", err)
				requireProviderError(t, err, tc.status, tc.retryAfter, tc.retryable)
			})
		}
		server.Close()
	}
}

func TestCompleteConnectionRefusedIsRetryable(t *testing.T) {
	t.Setenv("GO_TEST_MODE", "true")
	server := httptest.NewServer(http.NotFoundHandler())
	endpoint := server.URL
	server.Close()
	for name, client := range completeProviders(t, endpoint) {
		t.Run(name, func(t *testing.T) {
			_, err := client.Complete(context.Background(), &llm.CompletionRequest{Messages: []llm.Message{llm.UserMessage("q")}})
			require.Error(t, err)
			class := llm.ClassifyError(err)
			require.Equal(t, llm.ReasonNetwork, class.Reason, "%v", err)
			require.True(t, class.Retryable, "%v", err)
		})
	}
}
