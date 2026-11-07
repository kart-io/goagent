package errors_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/stretchr/testify/require"
)

// 固定错误应在保持身份匹配的同时，向统一分类入口提供错误码。
func TestFrameworkSentinelCodeAndIdentity(t *testing.T) {
	wrapped := fmt.Errorf("工具注册失败: %w", agentErrors.ErrSubstituteUnknown)
	require.Equal(t, agentErrors.CodeToolNotFound, agentErrors.GetCode(wrapped))
	require.ErrorIs(t, wrapped, agentErrors.ErrSubstituteUnknown)
	require.False(t, errors.Is(agentErrors.ErrToolKeyNotFound, agentErrors.ErrSessionNotFound), "同类错误仍有独立身份")
	require.False(t, errors.Is(agentErrors.New(agentErrors.CodeToolNotFound, "其他工具错误"), agentErrors.ErrSubstituteUnknown))
}

// 锁等待等控制信号和标准上下文错误均须保留在合并错误中。
func TestCodedSentinelKeepsJoinedContext(t *testing.T) {
	err := errors.Join(agentErrors.ErrDispatcherBusy, context.DeadlineExceeded)
	require.Equal(t, agentErrors.CodeOverloaded, agentErrors.GetCode(err))
	require.ErrorIs(t, err, agentErrors.ErrDispatcherBusy)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// 多错误链按从左到右的遍历顺序取码，外层业务包装优先。
func TestUnifiedErrorCodeFollowsChainOrder(t *testing.T) {
	business := agentErrors.New(agentErrors.CodeInvalidInput, "参数不合法")
	require.Equal(t, agentErrors.CodeToolNotFound, agentErrors.GetCode(errors.Join(agentErrors.ErrSubstituteUnknown, business)))
	require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(errors.Join(business, agentErrors.ErrSubstituteUnknown)))
	outer := agentErrors.Wrap(agentErrors.ErrSubstituteUnknown, agentErrors.CodeAgentExecution, "代理执行失败")
	require.Equal(t, agentErrors.CodeAgentExecution, agentErrors.GetCode(outer))
	require.ErrorIs(t, outer, agentErrors.ErrSubstituteUnknown)
	var signal *agentErrors.SentinelError
	require.ErrorAs(t, fmt.Errorf("包装: %w", agentErrors.ErrSubstituteUnknown), &signal)
	require.Equal(t, agentErrors.CodeToolNotFound, signal.ErrorCode())
	require.Empty(t, agentErrors.GetCode(nil))
	var nilBusiness *agentErrors.AgentError
	require.Equal(t, agentErrors.CodeInternal, agentErrors.GetCode(nilBusiness))
}

// 为重试或耗时追加信息时，只能创建包装，不能降级分类或污染共享哨兵。
func TestAnnotatingSentinelPreservesCodeAndIdentity(t *testing.T) {
	original := agentErrors.ErrLockUnavailable
	annotated := agentErrors.ErrorWithRetry(original, 2, 3)
	require.Equal(t, agentErrors.CodeLockUnavailable, agentErrors.GetCode(annotated))
	require.ErrorIs(t, annotated, original)
	require.Empty(t, agentErrors.GetContext(original))
	require.Equal(t, 2, agentErrors.GetContext(annotated)["retry_attempt"])
}

// 自定义 As 向调用方投影 AgentError 是标准错误协议的一部分。
type projectedBusinessError struct{}

func (projectedBusinessError) Error() string { return "业务错误投影" }
func (projectedBusinessError) As(target any) bool {
	if p, ok := target.(**agentErrors.AgentError); ok {
		*p = agentErrors.New(agentErrors.CodeInvalidInput, "投影参数不合法")
		return true
	}
	return false
}
func TestErrorCodeSupportsCustomAsProjection(t *testing.T) {
	projected := projectedBusinessError{}
	require.True(t, agentErrors.IsAgentError(projected))
	require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(projected))
	require.Equal(t, agentErrors.CodeToolNotFound, agentErrors.GetCode(errors.Join(agentErrors.ErrSubstituteUnknown, projected)))
	require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(errors.Join(projected, agentErrors.ErrSubstituteUnknown)))
}

// 转发 As 的包装不能跨过合并链左侧的哨兵分类。
type delegatedClassification struct{ inner error }

func (e delegatedClassification) Error() string      { return e.inner.Error() }
func (e delegatedClassification) Unwrap() error      { return e.inner }
func (e delegatedClassification) As(target any) bool { return errors.As(e.inner, target) }
func TestDelegatedAsKeepsFirstErrorClassification(t *testing.T) {
	inner := errors.Join(agentErrors.ErrSubstituteUnknown, agentErrors.New(agentErrors.CodeInvalidInput, "输入错误"))
	require.Equal(t, agentErrors.CodeToolNotFound, agentErrors.GetCode(delegatedClassification{inner}))
}
