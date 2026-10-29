package llm

import (
	"context"
	"errors"
	"io"
	"net"
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
