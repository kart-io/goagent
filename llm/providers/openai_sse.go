package providers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync/atomic"

	"github.com/sashabaranov/go-openai"

	agentErrors "github.com/kart-io/goagent/errors"
)

// 上游（代理或不支持流的兼容服务）对流式请求回了 JSON 等非 SSE 内容时，go-openai 只报 io.EOF，
// 与连接中断无法区分；nonSSEDoer 嗅探响应体首个非空行，流式循环据此报告真实原因。
type nonSSEKey struct{}

func withNonSSESlot(ctx context.Context) (context.Context, *atomic.Bool) {
	slot := new(atomic.Bool)
	return context.WithValue(ctx, nonSSEKey{}, slot), slot
}

type nonSSEDoer struct{ next openai.HTTPDoer }

func (d nonSSEDoer) Do(req *http.Request) (*http.Response, error) {
	resp, err := d.next.Do(req)
	slot, ok := req.Context().Value(nonSSEKey{}).(*atomic.Bool)
	if err != nil || !ok || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, err
	}
	resp.Body = &sseSniffBody{ReadCloser: resp.Body, nonSSE: slot}
	return resp, nil
}

// sseLinePrefixes 是 SSE 行的合法开头：注释冒号或字段名（字段名后可不带冒号）。
var sseLinePrefixes = [][]byte{[]byte(":"), []byte("data"), []byte("event"), []byte("id"), []byte("retry")}

// sseSniffLimit 足以判断最长的字段名，首行超过它即可下结论，不缓存整行。
const sseSniffLimit = 16

type sseSniffBody struct {
	io.ReadCloser
	nonSSE  *atomic.Bool
	line    []byte
	decided bool
}

func (b *sseSniffBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	for _, c := range p[:n] {
		if b.decided {
			break
		}
		b.feed(c)
	}
	if err != nil && !b.decided {
		b.decide()
	}
	return n, err
}

func (b *sseSniffBody) feed(c byte) {
	if c == '\n' || c == '\r' {
		if len(bytes.TrimSpace(b.line)) > 0 {
			b.decide()
		}
		b.line = b.line[:0]
		return
	}
	b.line = append(b.line, c)
	if len(bytes.TrimLeft(b.line, " \t")) >= sseSniffLimit {
		b.decide()
	}
}

// decide 只在首个非空行上判定一次；空响应体不算非 SSE，按连接中断处理。
func (b *sseSniffBody) decide() {
	b.decided = true
	line := bytes.TrimPrefix(bytes.TrimLeft(b.line, " \t"), []byte("\xef\xbb\xbf"))
	if len(line) == 0 {
		return
	}
	for _, prefix := range sseLinePrefixes {
		if bytes.HasPrefix(line, prefix) {
			return
		}
	}
	b.nonSSE.Store(true)
}

func newNonSSEStreamError() error {
	return agentErrors.New(agentErrors.CodeLLMResponse, "上游返回的不是 SSE 事件流").WithComponent("llm").WithOperation("decode_stream")
}
