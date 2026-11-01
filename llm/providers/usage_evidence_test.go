package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kart-io/goagent/core/usage"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

func TestStreamUsageEvidenceProtocol(t *testing.T) {
	for _, provider := range []string{"openai", "deepseek", "gemini"} {
		for _, partial := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/partial=%v", provider, partial), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					if provider == "gemini" {
						if partial {
							fmt.Fprint(w, `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"totalTokenCount":19}}`+"\n\n")
						} else {
							fmt.Fprint(w, `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":19,"cachedContentTokenCount":4}}`+"\n\n")
						}
					} else {
						fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`+"\n\n")
						if partial {
							fmt.Fprint(w, `data: {"choices":[],"usage":{"total_tokens":19}}`+"\n\n")
						} else {
							fmt.Fprint(w, `data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":19,"prompt_tokens_details":{"cached_tokens":4}}}`+"\n\n")
						}
						fmt.Fprint(w, "data: [DONE]\n\n")
					}
				}))
				defer server.Close()
				opts := []agentllm.ClientOption{agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL)}
				var client agentllm.StreamClient
				var err error
				switch provider {
				case "openai":
					client, err = NewOpenAIWithOptions(opts...)
				case "deepseek":
					client, err = NewDeepSeekWithOptions(opts...)
				case "gemini":
					client, err = NewGeminiWithOptions(opts...)
				}
				require.NoError(t, err)
				chunks, err := client.ChatStream(context.Background(), []agentllm.Message{agentllm.UserMessage("question")})
				require.NoError(t, err)
				var last *agentllm.StreamChunk
				for c := range chunks {
					last = c
				}
				require.NotNil(t, last)
				require.NoError(t, last.Error)
				require.NotNil(t, last.Usage)
				require.Equal(t, 19, last.Usage.TotalTokens)
				if partial {
					require.Equal(t, usage.Total, last.Usage.Fields())
				} else {
					require.Equal(t, usage.BasicFields|usage.Cached, last.Usage.Fields())
					require.Equal(t, 4, last.Usage.CachedTokens)
				}
			})
		}
	}
}
