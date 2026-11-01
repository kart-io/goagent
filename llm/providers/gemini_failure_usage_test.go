package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/usage"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

func TestGeminiStreamFailurePreservesUsage(t *testing.T) {
	for _, mode := range []string{"max_tokens", "safety", "blocked", "broken_after_usage", "cancel_after_usage", "partial_frames_failure", "unknown_failure"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				switch mode {
				case "max_tokens":
					fmt.Fprint(w, `data: {"candidates":[{"finishReason":"MAX_TOKENS"}],"usageMetadata":{"totalTokenCount":19,"cachedContentTokenCount":4}}`+"\n\n")
				case "safety":
					fmt.Fprint(w, `data: {"candidates":[{"finishReason":"SAFETY"}],"usageMetadata":{"totalTokenCount":19,"cachedContentTokenCount":4}}`+"\n\n")
				case "blocked":
					fmt.Fprint(w, `data: {"promptFeedback":{"blockReason":"SAFETY"},"usageMetadata":{"totalTokenCount":19,"cachedContentTokenCount":4}}`+"\n\n")
				case "partial_frames_failure":
					fmt.Fprint(w, `data: {"usageMetadata":{"cachedContentTokenCount":4}}`+"\n\n")
					fmt.Fprint(w, `data: {"candidates":[{"finishReason":"MAX_TOKENS"}],"usageMetadata":{"totalTokenCount":19}}`+"\n\n")
				case "unknown_failure":
					fmt.Fprint(w, `data: {"candidates":[{"finishReason":"MAX_TOKENS"}]}`+"\n\n")
				default:
					fmt.Fprint(w, `data: {"usageMetadata":{"totalTokenCount":19,"cachedContentTokenCount":4}}`+"\n\n")
					if mode == "cancel_after_usage" {
						fmt.Fprint(w, `data: {"candidates":[{"content":{"parts":[{"text":"ready"}]}}]}`+"\n\n")
						w.(http.Flusher).Flush()
						<-r.Context().Done()
					} else {
						fmt.Fprint(w, "data: invalid-json\n\n")
					}
				}
			}))
			defer server.Close()
			p, err := NewGeminiWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL))
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			chunks, err := p.ChatStream(ctx, []agentllm.Message{agentllm.UserMessage("question")})
			require.NoError(t, err)
			var terminal *agentllm.StreamChunk
			for c := range chunks {
				if mode == "cancel_after_usage" && c.Delta == "ready" {
					cancel()
				}
				if c.Done {
					terminal = c
				}
			}
			require.NotNil(t, terminal, "failure must deliver terminal evidence")
			require.Error(t, terminal.Error)
			require.Empty(t, terminal.ToolCalls)
			if mode == "unknown_failure" {
				require.Nil(t, terminal.Usage)
				return
			}
			require.NotNil(t, terminal.Usage)
			require.Equal(t, 19, terminal.Usage.TotalTokens)
			require.Equal(t, 4, terminal.Usage.CachedTokens)
			require.Equal(t, usage.Total|usage.Cached, terminal.Usage.Fields())
		})
	}
}
