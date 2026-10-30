package llm

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"syscall"
	"time"
)

// ProviderError 携带模型服务失败中可用于判定重试的信息，由 Provider 构造；
// 调用方据此判断，不依赖任何 SDK 的错误类型。Error 文本与 Err 相同。
type ProviderError struct {
	// StatusCode 是 HTTP 状态码；0 表示没有得到响应，或流在响应开始后中断。
	StatusCode int
	// RetryAfter 是服务端建议的等待时长，0 表示未给出。
	RetryAfter time.Duration
	Err        error
}

func (e *ProviderError) Error() string { return e.Err.Error() }
func (e *ProviderError) Unwrap() error { return e.Err }

// Retryable 报告失败是否可能因重试而消失：429、5xx，以及暂时性的连接错误（网络超时、连接被拒绝或重置、
// 连接意外关闭、DNS 暂时失败）。其他 4xx、取消与上下文超时、证书与 TLS 错误、域名不存在、协议或解码错误都不可重试。
func (e *ProviderError) Retryable() bool {
	switch {
	case e.StatusCode == 429 || (e.StatusCode >= 500 && e.StatusCode <= 599):
		return true
	case e.StatusCode != 0 || errors.Is(e.Err, context.Canceled) || errors.Is(e.Err, context.DeadlineExceeded):
		return false
	}
	// 只按白名单判断：*url.Error 总是实现 net.Error，不能据此认定为连接层的暂时性错误。
	var dnsErr *net.DNSError
	if errors.As(e.Err, &dnsErr) {
		return !dnsErr.IsNotFound && (dnsErr.IsTimeout || dnsErr.IsTemporary)
	}
	var netErr net.Error
	if errors.As(e.Err, &netErr) && netErr.Timeout() {
		return true
	}
	for _, transient := range []error{syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.ECONNABORTED, syscall.EPIPE, io.ErrUnexpectedEOF, io.EOF} {
		if errors.Is(e.Err, transient) {
			return true
		}
	}
	return false
}

// ErrorReason 是模型请求失败的分类。
type ErrorReason string

const (
	ReasonRateLimit     ErrorReason = "rate_limit"
	ReasonServer        ErrorReason = "server"
	ReasonNetwork       ErrorReason = "network"
	ReasonContextLength ErrorReason = "context_length"
	ReasonAuth          ErrorReason = "auth"
	ReasonNotFound      ErrorReason = "not_found"
	ReasonBadRequest    ErrorReason = "bad_request"
	ReasonCanceled      ErrorReason = "canceled"
	ReasonUnknown       ErrorReason = "unknown"
)

// ClassifiedError 是模型请求失败的分类与恢复提示。提示只供恢复策略参考，是否重试、压缩或回退由调用方决定。
type ClassifiedError struct {
	Reason ErrorReason
	// Retryable 表示原样重试可能成功（与 ProviderError.Retryable 一致）。
	Retryable bool
	// ShouldCompress 表示请求超出上下文窗口，缩短消息后重试可能成功。
	ShouldCompress bool
	// ShouldFallback 表示换一个模型或提供方可能成功（限流、服务端错误、鉴权失败、模型不存在）。
	ShouldFallback bool
}

// contextLengthMarkers 是各提供方表示超出上下文窗口的错误片段（小写）。
var contextLengthMarkers = []string{
	"context_length_exceeded", "maximum context length", "context length", "context window",
	"prompt is too long", "too many tokens", "input is too long", "reduce the length",
}

// ClassifyError 对模型请求的错误分类，复用 ProviderError 的状态码与暂时性判断。
func ClassifyError(err error) ClassifiedError {
	switch {
	case err == nil:
		return ClassifiedError{Reason: ReasonUnknown}
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return ClassifiedError{Reason: ReasonCanceled}
	}
	text := strings.ToLower(err.Error())
	for _, marker := range contextLengthMarkers {
		if strings.Contains(text, marker) {
			return ClassifiedError{Reason: ReasonContextLength, ShouldCompress: true}
		}
	}
	var pe *ProviderError
	if !errors.As(err, &pe) {
		return ClassifiedError{Reason: ReasonUnknown}
	}
	c := ClassifiedError{Retryable: pe.Retryable()}
	switch {
	case pe.StatusCode == 429:
		c.Reason, c.ShouldFallback = ReasonRateLimit, true
	case pe.StatusCode >= 500 && pe.StatusCode <= 599:
		c.Reason, c.ShouldFallback = ReasonServer, true
	case pe.StatusCode == 401 || pe.StatusCode == 403:
		c.Reason, c.ShouldFallback = ReasonAuth, true
	case pe.StatusCode == 404:
		c.Reason, c.ShouldFallback = ReasonNotFound, true
	case pe.StatusCode == 413:
		c.Reason, c.ShouldCompress = ReasonContextLength, true
	case pe.StatusCode >= 400 && pe.StatusCode <= 499:
		c.Reason = ReasonBadRequest
	case c.Retryable:
		c.Reason = ReasonNetwork
	default:
		c.Reason = ReasonUnknown
	}
	return c
}
