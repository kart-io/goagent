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
	if errors.As(err, &apiErr) && apiErr == nil {
		// 上游对流式请求回了非 SSE 内容时，go-openai 包装的是空 *APIError，其 Error() 不可调用。
		e.Err = errors.New("上游返回的不是 SSE 事件流")
	} else if errors.As(err, &apiErr) {
		e.StatusCode = apiErr.HTTPStatusCode
		e.QuotaExhausted = quotaExhausted(apiErr)
	} else if errors.As(err, &reqErr) {
		e.StatusCode = reqErr.HTTPStatusCode
	}
	return e
}

// quotaMarkers 是 OpenAI 兼容服务表示额度耗尽的错误消息片段：MiniMax 的套餐用量上限（错误码 2056）。
var quotaMarkers = []string{"用量上限", "(2056)"}

// quotaExhausted 识别 OpenAI 兼容服务的额度耗尽。OpenAI 以 insufficient_quota 标在 code 或 type 上；
// 消息里的 "exceeded your current quota" 不能作依据，Gemini 的按分钟限流也这样写。
func quotaExhausted(e *openai.APIError) bool {
	if code, _ := e.Code.(string); code == "insufficient_quota" || e.Type == "insufficient_quota" {
		return true
	}
	for _, m := range quotaMarkers {
		if strings.Contains(e.Message, m) {
			return true
		}
	}
	return false
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
