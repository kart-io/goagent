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

// Retryable 报告失败是否可能因重试而消失：429、5xx，以及连接层错误。
// 其他 4xx、取消与超时、协议或解码错误都不可重试。
func (e *ProviderError) Retryable() bool {
	switch {
	case e.StatusCode == 429 || (e.StatusCode >= 500 && e.StatusCode <= 599):
		return true
	case e.StatusCode != 0 || errors.Is(e.Err, context.Canceled) || errors.Is(e.Err, context.DeadlineExceeded):
		return false
	}
	var netErr net.Error
	return errors.As(e.Err, &netErr) || errors.Is(e.Err, io.ErrUnexpectedEOF) || errors.Is(e.Err, syscall.ECONNRESET)
}
