package providers

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
	"google.golang.org/genai"

	agentllm "github.com/kart-io/goagent/llm"
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
			*slot = parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		}
	}
	return resp, err
}

// parseRetryAfter 解析秒数或 HTTP 日期；缺失、无法解析或已过期都返回 0，即视为没有给出。
// 不用 common.ParseRetryAfter：它对无法解析的值回退为 60 秒。
func parseRetryAfter(header string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(header), 10, 64); err == nil {
		return time.Duration(min(max(seconds, 0), math.MaxInt64/int64(time.Second))) * time.Second
	}
	if at, err := http.ParseTime(header); err == nil {
		return max(at.Sub(now), 0)
	}
	return 0
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
