package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/require"

	agentllm "github.com/kart-io/goagent/llm"
)

const sseStopChunk = `data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"好"},"finish_reason":"stop"}]}` + "\n\n"

// 上游（代理或不支持流的兼容服务）对流式请求返回非 SSE 内容时，要报告真实原因，而不是当作连接中断。
func TestOpenAIStreamRejectsNonSSEBody(t *testing.T) {
	longJSON := "{\n" + strings.Repeat("  \"k\": 1,\n", 400) + "  \"end\": 1\n}\n"
	cases := []struct {
		name        string
		contentType string
		body        string
		wantErr     string
		wantNoSSE   bool
	}{
		{name: "JSON 响应", contentType: "application/json", body: `{"id":"x","choices":[{"message":{"role":"assistant","content":"好"},"finish_reason":"stop"}]}` + "\n", wantErr: "SSE"},
		{name: "前导空行后的 JSON", contentType: "application/json", body: "\n\r\n  {\"id\":\"x\"}\n", wantErr: "SSE"},
		{name: "HTML 错误页", contentType: "text/html", body: "<html><body>502 Bad Gateway</body></html>\n", wantErr: "SSE"},
		{name: "超过空消息上限的多行 JSON", contentType: "application/json", body: longJSON, wantErr: "SSE"},
		{name: "空响应体按中断处理", contentType: "text/event-stream", body: "", wantErr: "unexpected EOF", wantNoSSE: true},
		{name: "截断的 SSE 按中断处理", contentType: "text/event-stream", body: `data: {"id":"x","choices":[{"index":0,"delta":{"content":"半"}}]}` + "\n\n", wantErr: "unexpected EOF", wantNoSSE: true},
		{name: "注释开头的 SSE", contentType: "text/event-stream", body: ": ping\n\n" + sseStopChunk + "data: [DONE]\n\n"},
		{name: "event 字段开头的 SSE", contentType: "text/event-stream", body: "event: message\n" + sseStopChunk + "data: [DONE]\n\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			p, err := NewOpenAIWithOptions(agentllm.WithAPIKey("k"), agentllm.WithBaseURL(server.URL), agentllm.WithTimeout(time.Second))
			require.NoError(t, err)
			resp, err := agentllm.Complete(context.Background(), p, &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("q")}})
			if tc.wantErr == "" {
				require.NoError(t, err)
				require.Equal(t, "好", resp.Content)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
			if tc.wantNoSSE {
				require.NotContains(t, err.Error(), "SSE")
			}
		})
	}
}

// 首行被拆成多次读取、或比判定长度更长时，结论都只取决于首个非空行的开头。
func TestSSESniffBodyDecidesOnFirstNonBlankLine(t *testing.T) {
	cases := []struct {
		body   string
		nonSSE bool
	}{
		{body: "data: {}\n\n", nonSSE: false},
		{body: "\r\n\n   retry: 100\n", nonSSE: false},
		{body: "\xef\xbb\xbfid: 1\n", nonSSE: false},
		{body: "data", nonSSE: false},
		{body: "   \n\t\n", nonSSE: false},
		{body: "", nonSSE: false},
		{body: `{"error":"boom"}`, nonSSE: true},
		{body: "\n" + strings.Repeat("x", 100) + "\ndata: {}\n", nonSSE: true},
		{body: "data: {}\n{not sse}\n", nonSSE: false},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q", tc.body), func(t *testing.T) {
			slot := new(atomic.Bool)
			body := &sseSniffBody{ReadCloser: io.NopCloser(iotest.OneByteReader(strings.NewReader(tc.body))), nonSSE: slot}
			data, err := io.ReadAll(body)
			require.NoError(t, err)
			require.Equal(t, tc.body, string(data))
			require.Equal(t, tc.nonSSE, slot.Load())
		})
	}
}
