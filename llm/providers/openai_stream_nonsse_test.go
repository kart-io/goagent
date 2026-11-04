package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	agentllm "github.com/kart-io/goagent/llm"
)

// 上游（代理或不支持流的兼容服务）对流式请求返回普通 JSON 时，要报告错误而不是 panic。
func TestOpenAIStreamRejectsNonSSEBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"id":"x","choices":[{"message":{"role":"assistant","content":"好"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	p, err := NewOpenAIWithOptions(agentllm.WithAPIKey("k"), agentllm.WithBaseURL(server.URL), agentllm.WithTimeout(time.Second))
	require.NoError(t, err)
	_, err = agentllm.Complete(context.Background(), p, &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("q")}})
	require.Error(t, err)
	require.ErrorContains(t, err, "SSE")
}
