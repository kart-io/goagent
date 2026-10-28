package tools

import (
	"context"
	"errors"
	"strings"
)

// ArgumentError 表示模型生成的参数不合法。Message 会原样交给模型，不得包含内部细节；
// Err 只用于 errors.Is/As，不展示给模型。
type ArgumentError struct {
	Message string
	Err     error
}

func (e *ArgumentError) Error() string { return e.Message }
func (e *ArgumentError) Unwrap() error { return e.Err }

// argumentErrorMessage 判断 err 能否交还模型改正。上下文结束、嵌套深度、Dispatcher 重入和
// 等待宿主继续都属于宿主侧问题，无论如何包装都不可恢复；Join 只有每个分支都是参数错误才可恢复。
func argumentErrorMessage(err error) (string, bool) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrNestedExecutionLimit) || errors.Is(err, ErrDispatcherReentry) || errors.Is(err, ErrToolWorkflowWaiting) {
		return "", false
	}
	switch e := err.(type) {
	case *ArgumentError:
		return e.Message, true
	case interface{ Unwrap() []error }:
		var messages []string
		for _, branch := range e.Unwrap() {
			m, ok := argumentErrorMessage(branch)
			if !ok {
				return "", false
			}
			messages = append(messages, m)
		}
		return strings.Join(messages, "; "), len(messages) > 0
	case interface{ Unwrap() error }:
		return argumentErrorMessage(e.Unwrap())
	}
	return "", false
}
