package providers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
	"google.golang.org/genai"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
)

type retryAfterKey struct{}

// withRetryAfterSlot 让 retryAfterDoer 把本次请求的 Retry-After 写回调用方；go-openai 的错误类型不带响应头。
func withRetryAfterSlot(ctx context.Context) (context.Context, *time.Duration) {
	slot := new(time.Duration)
	return context.WithValue(ctx, retryAfterKey{}, slot), slot
}

type retryAfterDoer struct{ next openai.HTTPDoer }

func (d retryAfterDoer) Do(req *http.Request) (*http.Response, error) {
	resp, err := d.next.Do(req)
	if err == nil && resp != nil && (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable) {
		if slot, ok := req.Context().Value(retryAfterKey{}).(*time.Duration); ok {
			*slot = common.RetryAfterDuration(resp.Header.Get("Retry-After"), time.Now())
		}
	}
	return resp, err
}

func openAIProviderError(err error, retryAfter time.Duration) *agentllm.ProviderError {
	e := &agentllm.ProviderError{RetryAfter: retryAfter, Err: err}
	var apiErr *openai.APIError
	var reqErr *openai.RequestError
	if errors.As(err, &apiErr) {
		e.StatusCode = apiErr.HTTPStatusCode
	} else if errors.As(err, &reqErr) {
		e.StatusCode = reqErr.HTTPStatusCode
	}
	return e
}

func geminiProviderError(err error) *agentllm.ProviderError {
	e := &agentllm.ProviderError{Err: err}
	var apiErr genai.APIError
	var apiErrPtr *genai.APIError
	if errors.As(err, &apiErr) {
		e.StatusCode, e.RetryAfter = apiErr.Code, geminiRetryDelay(apiErr.Details)
	} else if errors.As(err, &apiErrPtr) && apiErrPtr != nil {
		e.StatusCode, e.RetryAfter = apiErrPtr.Code, geminiRetryDelay(apiErrPtr.Details)
	}
	return e
}

// geminiRetryDelay 读取 google.rpc.RetryInfo 的 retryDelay（如 "30s"）。
func geminiRetryDelay(details []map[string]any) time.Duration {
	for _, d := range details {
		if kind, _ := d["@type"].(string); !strings.HasSuffix(kind, "google.rpc.RetryInfo") {
			continue
		}
		if delay, _ := d["retryDelay"].(string); delay != "" {
			if v, err := time.ParseDuration(delay); err == nil && v > 0 {
				return v
			}
		}
	}
	return 0
}
