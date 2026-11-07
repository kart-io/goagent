package providers_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func Test提供方真实HTTP保留错误码与恢复信息(t *testing.T) {
	t.Setenv("GO_TEST_MODE", "")
	for _, name := range []string{"openai", "gemini"} {
		for _, mode := range []string{"complete", "embed"} {
			for _, tc := range []struct {
				status int
				code   agentErrors.ErrorCode
				reason llm.ErrorReason
				retry  bool
			}{
				{429, agentErrors.CodeLLMRateLimit, llm.ReasonRateLimit, true},
				{401, agentErrors.CodeInvalidConfig, llm.ReasonAuth, false},
				{403, agentErrors.CodeAccessDenied, llm.ReasonAuth, false},
				{503, agentErrors.CodeLLMResponse, llm.ReasonServer, true},
			} {
				t.Run(fmt.Sprintf("%s/%s/%d", name, mode, tc.status), func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						w.Header().Set("Retry-After", "2")
						w.WriteHeader(tc.status)
						fmt.Fprintf(w, `{"error":{"code":%d,"message":"请求失败","status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"2s"}]}}`, tc.status)
					}))
					defer server.Close()
					opts := []llm.ClientOption{llm.WithAPIKey("test"), llm.WithBaseURL(server.URL)}
					var client llm.Client
					var err error
					if name == "openai" {
						client, err = providers.NewOpenAIWithOptions(opts...)
					} else {
						client, err = providers.NewGeminiWithOptions(opts...)
					}
					require.NoError(t, err)
					if mode == "embed" {
						_, err = client.(interface {
							Embed(context.Context, string) ([]float32, error)
						}).Embed(context.Background(), "q")
					} else {
						_, err = llm.Complete(context.Background(), client, &llm.CompletionRequest{Messages: []llm.Message{llm.UserMessage("q")}})
					}
					require.Error(t, err)
					require.Equal(t, tc.code, agentErrors.GetCode(err))
					classification := llm.ClassifyError(err)
					require.Equal(t, tc.reason, classification.Reason)
					require.Equal(t, tc.retry, common.IsRetryable(err))
					var pe *llm.ProviderError
					require.ErrorAs(t, err, &pe)
					require.Equal(t, tc.status, pe.StatusCode)
					if tc.status == 429 || tc.status == 503 {
						require.Equal(t, 2*time.Second, pe.RetryAfter)
					}
					if name == "openai" {
						var sdk *openai.APIError
						require.ErrorAs(t, err, &sdk)
					} else {
						var sdk genai.APIError
						require.ErrorAs(t, err, &sdk)
					}
				})
			}
		}
	}
}

func TestDeepSeekEmbed证书失败只请求一次(t *testing.T) {
	t.Setenv("GO_TEST_MODE", "")
	var connections atomic.Int32
	server := httptest.NewUnstartedServer(http.NotFoundHandler())
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.StartTLS()
	defer server.Close()
	client, err := providers.NewDeepSeekWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL))
	require.NoError(t, err)
	_, err = client.Embed(context.Background(), "q")
	require.Error(t, err)
	require.Equal(t, int32(1), connections.Load())
	require.False(t, common.IsRetryable(err))
	var pe *llm.ProviderError
	require.True(t, errors.As(err, &pe))
}

func Test提供方真实请求超时与取消保留原因(t *testing.T) {
	t.Setenv("GO_TEST_MODE", "")
	for _, name := range []string{"openai", "gemini"} {
		for _, mode := range []string{"complete", "embed"} {
			for _, canceled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/取消=%t", name, mode, canceled), func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
					defer cancel()
					release := make(chan struct{})
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if canceled {
							cancel()
						}
						select {
						case <-r.Context().Done():
						case <-release:
						}
					}))
					defer server.Close()
					defer close(release)
					opts := []llm.ClientOption{llm.WithAPIKey("test"), llm.WithBaseURL(server.URL)}
					var client llm.Client
					var err error
					if name == "openai" {
						client, err = providers.NewOpenAIWithOptions(opts...)
					} else {
						client, err = providers.NewGeminiWithOptions(opts...)
					}
					require.NoError(t, err)
					if mode == "embed" {
						_, err = client.(interface {
							Embed(context.Context, string) ([]float32, error)
						}).Embed(ctx, "q")
					} else {
						_, err = llm.Complete(ctx, client, &llm.CompletionRequest{Messages: []llm.Message{llm.UserMessage("q")}})
					}
					expectedCause := context.DeadlineExceeded
					expectedCode := agentErrors.CodeLLMTimeout
					if canceled {
						expectedCause, expectedCode = context.Canceled, agentErrors.CodeContextCanceled
					}
					require.ErrorIs(t, err, expectedCause)
					require.Equal(t, expectedCode, agentErrors.GetCode(err))
					require.Equal(t, llm.ReasonCanceled, llm.ClassifyError(err).Reason)
					require.False(t, common.IsRetryable(err))
				})
			}
		}
	}
}
