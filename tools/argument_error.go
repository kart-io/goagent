package tools

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// ArgumentError reports that model-generated arguments are invalid. Message is
// passed to the model verbatim and must not contain internal details; Err is
// only used by errors.Is/As and is never shown to the model.
type ArgumentError struct {
	Message string
	Err     error
}

func (e *ArgumentError) Error() string { return e.Message }
func (e *ArgumentError) Unwrap() error { return e.Err }

// ErrorCode lets argument errors without an underlying cause keep their
// validation classification at the host call boundary.
func (e *ArgumentError) ErrorCode() agentErrors.ErrorCode {
	if e == nil {
		return agentErrors.CodeInternal
	}
	return agentErrors.CodeToolValidation
}

// settledSet 记录内层 Dispatcher 或组合工具已定性的参数错误节点。外层模型路径在调用工具前把它放进 ctx，
// 内层返回错误时登记，错误值本身不变；外层只把未登记的节点当作本层模型的参数错误。
type settledSet struct {
	mu    sync.Mutex
	nodes []error
}

type settledKey struct{}

func withSettledSet(ctx context.Context) (context.Context, *settledSet) {
	s := &settledSet{}
	return context.WithValue(ctx, settledKey{}, s), s
}

// Settle registers the argument error nodes in err's chain with the nearest
// settledSet in ctx and returns err unchanged. Wrappers that call a sub-agent
// or an inner tool themselves should register the returned errors with the
// ctx they received, so the outer model does not treat the inner argument
// errors as its own.
func Settle(ctx context.Context, err error) error {
	if ctx == nil || err == nil {
		return err
	}
	if s, _ := ctx.Value(settledKey{}).(*settledSet); s != nil {
		s.mu.Lock()
		s.add(err)
		s.mu.Unlock()
	}
	return err
}

func (s *settledSet) add(err error) {
	if _, ok := argumentNode(err); ok && reflect.TypeOf(err).Comparable() {
		s.nodes = append(s.nodes, err)
	}
	switch e := err.(type) {
	case interface{ Unwrap() []error }:
		for _, branch := range e.Unwrap() {
			if branch != nil {
				s.add(branch)
			}
		}
	case interface{ Unwrap() error }:
		if next := e.Unwrap(); next != nil {
			s.add(next)
		}
	}
}

func (s *settledSet) has(err error) bool {
	if s == nil || !reflect.TypeOf(err).Comparable() {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, node := range s.nodes {
		if node == err {
			return true
		}
	}
	return false
}

// argumentNode 只看当前这一层，不穿透包装。
func argumentNode(err error) (string, bool) {
	if m, ok := err.(interface{ ModelArgumentMessage() (string, bool) }); ok {
		if message, ok := m.ModelArgumentMessage(); ok {
			return message, true
		}
	}
	if e, ok := err.(*ArgumentError); ok {
		return e.Message, true
	}
	return "", false
}

// argumentErrorMessage 判断 err 能否交还模型改正。上下文结束、嵌套深度、Dispatcher 重入和
// 等待宿主继续都属于宿主侧问题，无论如何包装都不可恢复；登记在 settled 中的节点属于内层，同样不可恢复；
// Join 只有每个分支都是参数错误才可恢复。
func argumentErrorMessage(err error, settled *settledSet) (string, bool) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, agentErrors.ErrNestedExecutionLimit) || errors.Is(err, agentErrors.ErrDispatcherReentry) || errors.Is(err, agentErrors.ErrToolWorkflowWaiting) {
		return "", false
	}
	if message, ok := argumentNode(err); ok {
		if settled.has(err) {
			return "", false
		}
		return message, true
	}
	switch e := err.(type) {
	case interface{ Unwrap() []error }:
		var messages []string
		for _, branch := range e.Unwrap() {
			m, ok := argumentErrorMessage(branch, settled)
			if !ok {
				return "", false
			}
			messages = append(messages, m)
		}
		return strings.Join(messages, "; "), len(messages) > 0
	case interface{ Unwrap() error }:
		return argumentErrorMessage(e.Unwrap(), settled)
	}
	return "", false
}

// ArgumentErrorResult reports whether out is an argument error result that the
// Dispatcher returned to the model, and returns its message.
func ArgumentErrorResult(out *interfaces.ToolOutput) (string, bool) {
	if out == nil {
		return "", false
	}
	return argumentResultMessage(out)
}
