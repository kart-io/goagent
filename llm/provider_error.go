package llm

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"syscall"
	"time"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
)

// ProviderError carries the information from a model service failure that is
// used to decide on retries; it is constructed by the Provider. Callers rely on
// it rather than on any SDK error type. Its Error text is the same as Err's.
type ProviderError struct {
	// StatusCode is the HTTP status code; 0 means no response was received, or the
	// stream broke after the response started.
	StatusCode int
	// RetryAfter is the wait duration suggested by the server; 0 means none was
	// given.
	RetryAfter time.Duration
	// QuotaExhausted reports that the account quota or plan usage is exhausted
	// (such as OpenAI's insufficient_quota). Servers often return it as 429, but it
	// requires a top-up or quota recovery, so short-term retries will not succeed.
	QuotaExhausted bool
	Err            error
}

func (e *ProviderError) Error() string { return e.Err.Error() }
func (e *ProviderError) Unwrap() error { return e.Err }

// ErrorCode maps failures known at the provider boundary to the framework's
// existing categories without changing the original cause chain.
func (e *ProviderError) ErrorCode() agentErrors.ErrorCode {
	if e == nil {
		return agentErrors.CodeInternal
	}
	switch {
	case e.StatusCode == 429:
		return agentErrors.CodeLLMRateLimit
	case e.StatusCode == 401:
		return agentErrors.CodeInvalidConfig
	case e.StatusCode == 403:
		return agentErrors.CodeAccessDenied
	case e.StatusCode == 400 || e.StatusCode == 413 || e.StatusCode == 422:
		return agentErrors.CodeInvalidInput
	case e.StatusCode != 0:
		return agentErrors.CodeLLMResponse
	case errors.Is(e.Err, context.Canceled):
		return agentErrors.CodeContextCanceled
	case errors.Is(e.Err, context.DeadlineExceeded):
		return agentErrors.CodeLLMTimeout
	}
	var coded agentErrors.CodedError
	if errors.As(e.Err, &coded) {
		return coded.ErrorCode()
	}
	var netErr net.Error
	if errors.As(e.Err, &netErr) && netErr.Timeout() {
		return agentErrors.CodeLLMTimeout
	}
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(e.Err, &syntaxErr) || errors.As(e.Err, &typeErr) {
		return agentErrors.CodeLLMResponse
	}
	return agentErrors.CodeLLMRequest
}

// Retryable reports whether the failure may go away on retry: 429, 5xx, and
// transient connection errors (network timeouts, connection refused or reset,
// unexpected connection close, temporary DNS failures). Exhausted quota, other
// 4xx, cancellation and context deadlines, certificate and TLS errors,
// nonexistent domains, and protocol or decoding errors are not retryable.
func (e *ProviderError) Retryable() bool {
	switch {
	case e.QuotaExhausted:
		return false
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

// ErrorReason classifies a model request failure.
type ErrorReason string

const (
	ReasonRateLimit     ErrorReason = "rate_limit"
	ReasonQuota         ErrorReason = "quota"
	ReasonServer        ErrorReason = "server"
	ReasonNetwork       ErrorReason = "network"
	ReasonContextLength ErrorReason = "context_length"
	ReasonAuth          ErrorReason = "auth"
	ReasonNotFound      ErrorReason = "not_found"
	ReasonBadRequest    ErrorReason = "bad_request"
	ReasonCanceled      ErrorReason = "canceled"
	ReasonUnknown       ErrorReason = "unknown"
)

// ClassifiedError is the classification and recovery hints for a model request
// failure. The hints are only advisory for recovery strategies; the caller
// decides whether to retry, compress, or fall back.
type ClassifiedError struct {
	Reason ErrorReason
	// Retryable reports that retrying as is may succeed (consistent with
	// ProviderError.Retryable).
	Retryable bool
	// ShouldCompress reports that the request exceeded the context window and
	// retrying with shorter messages may succeed.
	ShouldCompress bool
	// ShouldFallback reports that switching to another model or provider may
	// succeed (rate limiting, exhausted quota, server errors, authentication
	// failures, model not found).
	ShouldFallback bool
}

// contextLengthMarkers 是各提供方表示超出上下文窗口的错误片段（小写）。
var contextLengthMarkers = []string{
	"context_length_exceeded", "maximum context length", "context length", "context window",
	"prompt is too long", "too many tokens", "input is too long", "reduce the length",
}

// ClassifyError classifies a model request error, reusing ProviderError's
// status code and transience checks.
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
	case pe.QuotaExhausted:
		c.Reason, c.ShouldFallback = ReasonQuota, true
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
