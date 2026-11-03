package providers

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// streamStallError 是流式响应超过空闲或总时限。实现 net.Error 且 Timeout 为 true，ProviderError 据此判为可重试。
type streamStallError struct{ msg string }

func (e streamStallError) Error() string { return e.msg }
func (streamStallError) Timeout() bool   { return true }
func (streamStallError) Temporary() bool { return true }

// streamWatchdog 在流式响应停住或拖得过久时取消请求的 ctx，原因记为 streamStallError。
// 服务端过载时首包很快，之后可能几分钟不发数据或一点点地发，响应头超时管不到。
type streamWatchdog struct {
	ctx        context.Context
	cancel     context.CancelCauseFunc
	idle       time.Duration
	idleTimer  *time.Timer
	totalTimer *time.Timer
}

func newStreamWatchdog(parent context.Context, idle, total time.Duration) *streamWatchdog {
	ctx, cancel := context.WithCancelCause(parent)
	w := &streamWatchdog{ctx: ctx, cancel: cancel, idle: idle}
	if idle > 0 {
		w.idleTimer = time.AfterFunc(idle, func() { cancel(streamStallError{fmt.Sprintf("流式响应 %s 内没有新数据", idle)}) })
	}
	if total > 0 {
		w.totalTimer = time.AfterFunc(total, func() { cancel(streamStallError{fmt.Sprintf("流式响应超过 %s 仍未结束", total)}) })
	}
	return w
}

// touch 在收到数据后重新计算空闲时限。
func (w *streamWatchdog) touch() {
	if w.idleTimer != nil {
		w.idleTimer.Reset(w.idle)
	}
}

// stalled 返回看门狗中断请求的原因；调用方自己的 ctx 先结束时返回 nil，由调用方报告它的错误。
func (w *streamWatchdog) stalled() error {
	var stall streamStallError
	if cause := context.Cause(w.ctx); errors.As(cause, &stall) {
		return cause
	}
	return nil
}

func (w *streamWatchdog) stop() {
	if w.idleTimer != nil {
		w.idleTimer.Stop()
	}
	if w.totalTimer != nil {
		w.totalTimer.Stop()
	}
	w.cancel(nil)
}
