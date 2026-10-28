package tools

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"

	"github.com/kart-io/goagent/interfaces"
)

// ArgumentError 表示模型生成的参数不合法。Message 会原样交给模型，不得包含内部细节；
// Err 只用于 errors.Is/As，不展示给模型。
type ArgumentError struct {
	Message string
	Err     error
}

func (e *ArgumentError) Error() string { return e.Message }
func (e *ArgumentError) Unwrap() error { return e.Err }

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

// Settle 把 err 链上的参数错误节点登记到 ctx 中最近的 settledSet，并原样返回 err。
// 自己调用子 Agent 或内层工具的包装器应以传入的 ctx 登记其返回的错误，外层模型就不会把内层参数错误当作自己的。
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
		errors.Is(err, ErrNestedExecutionLimit) || errors.Is(err, ErrDispatcherReentry) || errors.Is(err, ErrToolWorkflowWaiting) {
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

// ArgumentErrorResult 判断 out 是否为 Dispatcher 交还模型的参数错误结果，并返回其中的 message。
func ArgumentErrorResult(out *interfaces.ToolOutput) (string, bool) {
	if out == nil {
		return "", false
	}
	return argumentResultMessage(out)
}
