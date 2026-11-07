package tools

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

// 超时后的迟到结果不能被当成成功，执行器也不能遗留工具协程。
func TestToolExecutor_Timeout(t *testing.T) {
	for _, lateSuccess := range []bool{false, true} {
		name := "协作返回超时"
		if lateSuccess {
			name = "拒绝超时后的成功结果"
		}
		t.Run(name, func(t *testing.T) {
			exited := make(chan struct{})
			tool := NewBaseTool("slow", "等待执行期限", `{}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				defer close(exited)
				<-ctx.Done()
				if lateSuccess {
					return &interfaces.ToolOutput{Success: true, Result: "迟到结果"}, nil
				}
				return nil, ctx.Err()
			})
			executor := NewToolExecutor(WithMaxConcurrency(1), WithTimeout(20*time.Millisecond), WithRetryPolicy(&RetryPolicy{}))
			results, err := executor.ExecuteParallel(context.Background(), []*ToolCallRequest{{ID: "slow", Tool: tool}})
			require.Error(t, err)
			require.Len(t, results, 1)
			require.ErrorIs(t, results[0].Error, context.DeadlineExceeded)
			require.Nil(t, results[0].Output)
			select {
			case <-exited:
			default:
				t.Fatal("执行返回时工具仍未退出")
			}
		})
	}
}

func TestToolExecutorCanceledContextDoesNotInvoke(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	tool := NewBaseTool("effect", "记录调用", `{}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls.Add(1)
		return &interfaces.ToolOutput{Success: true}, nil
	})
	executor := NewToolExecutor()
	results, err := executor.ExecuteSequential(ctx, []*ToolCallRequest{{ID: "effect", Tool: tool}})
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, results, 1)
	require.ErrorIs(t, results[0].Error, context.Canceled)
	require.Zero(t, calls.Load())
}

func TestToolExecutorCancellationInterruptsRetryDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	tool := NewBaseTool("retry", "首轮取消后禁止重试", `{}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls.Add(1)
		cancel()
		return nil, errors.New("可重试错误")
	})
	executor := NewToolExecutor(WithRetryPolicy(&RetryPolicy{MaxRetries: 2, InitialDelay: 10 * time.Second, MaxDelay: 10 * time.Second, Multiplier: 1}))
	done := make(chan error, 1)
	go func() {
		_, err := executor.ExecuteSequential(ctx, []*ToolCallRequest{{ID: "retry", Tool: tool}})
		done <- err
	}()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
		require.EqualValues(t, 1, calls.Load())
	case <-time.After(time.Second):
		t.Fatal("父上下文取消后仍阻塞于重试退避")
	}
}

func TestToolExecutorCancellationWhileWaitingForRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempted := make(chan struct{}, 1)
	tool := NewBaseTool("retry", "等待退避时取消", `{}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		attempted <- struct{}{}
		return nil, errors.New("临时错误")
	})
	executor := NewToolExecutor(WithRetryPolicy(&RetryPolicy{MaxRetries: 1, InitialDelay: 10 * time.Second, MaxDelay: 10 * time.Second, Multiplier: 1}))
	done := make(chan error, 1)
	go func() {
		_, err := executor.ExecuteSequential(ctx, []*ToolCallRequest{{ID: "retry", Tool: tool}})
		done <- err
	}()
	<-attempted
	select {
	case err := <-done:
		t.Fatalf("退避前意外结束：%v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("重试退避未响应取消")
	}
	require.Empty(t, attempted, "取消后不得再次调用工具")
}

func TestToolExecutorLateSuccessDoesNotRepeatEffects(t *testing.T) {
	var effects atomic.Int32
	tool := NewBaseTool("effect", "迟到成功不得自动重试", `{}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		effects.Add(1)
		<-ctx.Done()
		return &interfaces.ToolOutput{Success: true, Result: "效果已发生"}, nil
	})
	executor := NewToolExecutor(WithTimeout(5 * time.Millisecond))
	results, err := executor.ExecuteSequential(context.Background(), []*ToolCallRequest{{ID: "effect", Tool: tool}})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Len(t, results, 1)
	require.Nil(t, results[0].Output)
	require.EqualValues(t, 1, effects.Load(), "迟到成功已有副作用，不能作为普通超时自动重试")
}
