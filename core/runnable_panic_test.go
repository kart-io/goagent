package core

import (
	"context"
	"strings"
	"testing"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Panic Recovery Tests
// =============================================================================

// TestRunnableFunc_PanicRecovery 测试 RunnableFunc 的 panic 恢复
func TestRunnableFunc_PanicRecovery(t *testing.T) {
	t.Run("Nil pointer panic", func(t *testing.T) {
		// 创建会触发 nil pointer panic 的函数
		panicFunc := NewRunnableFunc(func(ctx context.Context, input string) (string, error) {
			var ptr *string
			return *ptr, nil // 这会触发 panic
		})

		output, err := panicFunc.Invoke(context.Background(), "test")

		// 验证 panic 被捕获并转换为错误
		require.Error(t, err)
		assert.Empty(t, output)

		// 验证错误类型
		agentErr, ok := err.(*agentErrors.AgentError)
		require.True(t, ok, "error should be AgentError")
		assert.Equal(t, agentErrors.CodeInternal, agentErr.Code)

		// 验证错误信息包含 panic 信息
		assert.Contains(t, err.Error(), "panic recovered")
		assert.Contains(t, err.Error(), "runtime error")
	})

	t.Run("Index out of bounds panic", func(t *testing.T) {
		panicFunc := NewRunnableFunc(func(ctx context.Context, input []int) (int, error) {
			return input[10], nil // 越界访问
		})

		output, err := panicFunc.Invoke(context.Background(), []int{1, 2, 3})

		require.Error(t, err)
		assert.Zero(t, output)
		assert.Contains(t, err.Error(), "panic recovered")
		assert.Contains(t, err.Error(), "index out of range")
	})

	t.Run("String panic", func(t *testing.T) {
		panicFunc := NewRunnableFunc(func(ctx context.Context, input string) (string, error) {
			panic("custom panic message")
		})

		output, err := panicFunc.Invoke(context.Background(), "test")

		require.Error(t, err)
		assert.Empty(t, output)
		assert.Contains(t, err.Error(), "panic recovered")
		assert.Contains(t, err.Error(), "custom panic message")
	})

	t.Run("Stack trace captured", func(t *testing.T) {
		panicFunc := NewRunnableFunc(func(ctx context.Context, input string) (string, error) {
			panic("test panic for stack trace")
		})

		_, err := panicFunc.Invoke(context.Background(), "test")

		require.Error(t, err)

		// 验证 AgentError 包含堆栈信息
		agentErr, ok := err.(*agentErrors.AgentError)
		require.True(t, ok)

		// 检查上下文中是否包含 stack_trace
		stackTrace, hasStack := agentErr.Context["stack_trace"]
		assert.True(t, hasStack, "error should contain stack_trace in Context")

		stackStr, ok := stackTrace.(string)
		require.True(t, ok)
		assert.NotEmpty(t, stackStr)
		assert.Contains(t, stackStr, "runnable_panic_test.go") // 应包含当前测试文件名
	})
}

// TestRunnablePipe_PanicRecovery 测试 RunnablePipe 的 panic 恢复
func TestRunnablePipe_PanicRecovery(t *testing.T) {
	t.Run("First runnable panics", func(t *testing.T) {
		// 第一个 Runnable 会 panic
		first := NewRunnableFunc(func(ctx context.Context, input string) (int, error) {
			panic("first runnable panic")
		})

		// 第二个 Runnable 正常
		second := NewRunnableFunc(func(ctx context.Context, input int) (string, error) {
			return "success", nil
		})

		pipe := NewRunnablePipe(first, second)

		output, err := pipe.Invoke(context.Background(), "test")

		require.Error(t, err)
		assert.Empty(t, output)
		assert.Contains(t, err.Error(), "panic recovered")
		assert.Contains(t, err.Error(), "first runnable panic")
		assert.Contains(t, err.Error(), "first runnable failed")
	})

	t.Run("Second runnable panics", func(t *testing.T) {
		// 第一个 Runnable 正常
		first := NewRunnableFunc(func(ctx context.Context, input string) (int, error) {
			return 42, nil
		})

		// 第二个 Runnable 会 panic
		second := NewRunnableFunc(func(ctx context.Context, input int) (string, error) {
			panic("second runnable panic")
		})

		pipe := NewRunnablePipe(first, second)

		output, err := pipe.Invoke(context.Background(), "test")

		require.Error(t, err)
		assert.Empty(t, output)
		assert.Contains(t, err.Error(), "panic recovered")
		assert.Contains(t, err.Error(), "second runnable panic")
		assert.Contains(t, err.Error(), "second runnable failed")
	})

	t.Run("Pipe Stream with panic", func(t *testing.T) {
		// 第一个 Runnable 在 Invoke 时会 panic
		first := NewRunnableFunc(func(ctx context.Context, input string) (int, error) {
			var ptr *int
			return *ptr, nil // nil pointer panic
		})

		second := NewRunnableFunc(func(ctx context.Context, input int) (string, error) {
			return "success", nil
		})

		pipe := NewRunnablePipe(first, second)

		stream, err := pipe.Stream(context.Background(), "test")
		require.NoError(t, err)

		// 从流中读取错误
		chunk := <-stream
		assert.Empty(t, chunk.Data) // For string type, zero value is ""
		require.Error(t, chunk.Error)
		assert.Contains(t, chunk.Error.Error(), "panic recovered")
		assert.Contains(t, chunk.Error.Error(), "first runnable failed")
	})
}

// TestRunnableSequence_PanicRecovery 测试 RunnableSequence 的 panic 恢复
func TestRunnableSequence_PanicRecovery(t *testing.T) {
	t.Run("First runnable panics", func(t *testing.T) {
		r1 := NewRunnableFunc(func(ctx context.Context, input any) (any, error) {
			panic("first panic")
		})
		r2 := NewRunnableFunc(func(ctx context.Context, input any) (any, error) {
			return "success", nil
		})

		seq := NewRunnableSequence(r1, r2)

		output, err := seq.Invoke(context.Background(), "test")

		require.Error(t, err)
		assert.Nil(t, output)
		assert.Contains(t, err.Error(), "panic recovered")
		assert.Contains(t, err.Error(), "first panic")
	})

	t.Run("Middle runnable panics", func(t *testing.T) {
		r1 := NewRunnableFunc(func(ctx context.Context, input any) (any, error) {
			return "step1", nil
		})
		r2 := NewRunnableFunc(func(ctx context.Context, input any) (any, error) {
			panic("middle panic")
		})
		r3 := NewRunnableFunc(func(ctx context.Context, input any) (any, error) {
			return "step3", nil
		})

		seq := NewRunnableSequence(r1, r2, r3)

		output, err := seq.Invoke(context.Background(), "test")

		require.Error(t, err)
		assert.Nil(t, output)
		assert.Contains(t, err.Error(), "panic recovered")
		assert.Contains(t, err.Error(), "middle panic")

		// 验证错误上下文包含索引信息
		agentErr, ok := err.(*agentErrors.AgentError)
		require.True(t, ok)
		assert.Equal(t, 1, agentErr.Context["index"]) // r2 的索引是 1
	})

	t.Run("Last runnable panics", func(t *testing.T) {
		r1 := NewRunnableFunc(func(ctx context.Context, input any) (any, error) {
			return "step1", nil
		})
		r2 := NewRunnableFunc(func(ctx context.Context, input any) (any, error) {
			return "step2", nil
		})
		r3 := NewRunnableFunc(func(ctx context.Context, input any) (any, error) {
			panic("last panic")
		})

		seq := NewRunnableSequence(r1, r2, r3)

		output, err := seq.Invoke(context.Background(), "test")

		require.Error(t, err)
		assert.Nil(t, output)
		assert.Contains(t, err.Error(), "panic recovered")
		assert.Contains(t, err.Error(), "last panic")
	})

	t.Run("Sequence Stream with panic", func(t *testing.T) {
		r1 := NewRunnableFunc(func(ctx context.Context, input any) (any, error) {
			return "step1", nil
		})
		r2 := NewRunnableFunc(func(ctx context.Context, input any) (any, error) {
			panic("stream panic")
		})

		seq := NewRunnableSequence(r1, r2)

		stream, err := seq.Stream(context.Background(), "test")
		require.NoError(t, err)

		// 从流中读取错误
		chunk := <-stream
		assert.Nil(t, chunk.Data)
		require.Error(t, chunk.Error)
		assert.Contains(t, chunk.Error.Error(), "panic recovered")
		assert.Contains(t, chunk.Error.Error(), "stream panic")
	})
}

// TestPanicToError 测试 panicToError 函数
func TestPanicToError(t *testing.T) {
	ctx := context.Background()

	t.Run("String panic value", func(t *testing.T) {
		err := panicToError(ctx, "test_component", "test_operation", "test panic")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "panic recovered")
		assert.Contains(t, err.Error(), "test panic")

		agentErr, ok := err.(*agentErrors.AgentError)
		require.True(t, ok)
		assert.Equal(t, agentErrors.CodeInternal, agentErr.Code)

		// 检查上下文
		assert.Equal(t, "test panic", agentErr.Context["panic_value"])
		assert.NotEmpty(t, agentErr.Context["stack_trace"])
	})

	t.Run("Error panic value", func(t *testing.T) {
		originalErr := agentErrors.New(agentErrors.CodeAgentValidation, "original error")
		err := panicToError(ctx, "comp", "op", originalErr)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "panic recovered")

		agentErr, ok := err.(*agentErrors.AgentError)
		require.True(t, ok)

		panicValue := agentErr.Context["panic_value"]
		assert.Equal(t, originalErr, panicValue)
	})

	t.Run("Struct panic value", func(t *testing.T) {
		type CustomError struct {
			Code    int
			Message string
		}

		customErr := CustomError{Code: 500, Message: "custom error"}
		err := panicToError(ctx, "comp", "op", customErr)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "panic recovered")

		agentErr, ok := err.(*agentErrors.AgentError)
		require.True(t, ok)

		assert.Equal(t, customErr, agentErr.Context["panic_value"])
	})
}

// TestSafeInvoke 测试 safeInvoke 函数
func TestSafeInvoke(t *testing.T) {
	t.Run("Normal function execution", func(t *testing.T) {
		fn := func(ctx context.Context, input string) (string, error) {
			return "result: " + input, nil
		}

		output, err := safeInvoke(fn, context.Background(), "test")

		require.NoError(t, err)
		assert.Equal(t, "result: test", output)
	})

	t.Run("Function returns error", func(t *testing.T) {
		fn := func(ctx context.Context, input string) (string, error) {
			return "", agentErrors.New(agentErrors.CodeAgentValidation, "validation failed")
		}

		output, err := safeInvoke(fn, context.Background(), "test")

		require.Error(t, err)
		assert.Empty(t, output)
		assert.Contains(t, err.Error(), "validation failed")
	})

	t.Run("Function panics", func(t *testing.T) {
		fn := func(ctx context.Context, input int) (int, error) {
			panic("test panic")
		}

		output, err := safeInvoke(fn, context.Background(), 42)

		require.Error(t, err)
		assert.Zero(t, output)
		assert.Contains(t, err.Error(), "panic recovered")
		assert.Contains(t, err.Error(), "test panic")
	})
}

// TestPanicRecovery_RealWorldScenario 真实场景测试
func TestPanicRecovery_RealWorldScenario(t *testing.T) {
	t.Run("Third-party tool panics", func(t *testing.T) {
		// 模拟第三方 Tool 的实现（可能有 bug 导致 panic）
		thirdPartyTool := NewRunnableFunc(func(ctx context.Context, input map[string]string) (map[string]interface{}, error) {
			// 假设第三方代码有 bug
			var config *map[string]string
			_ = (*config)["key"] // nil pointer dereference
			return nil, nil
		})

		// Agent 使用该 Tool
		input := map[string]string{"query": "test"}
		result, err := thirdPartyTool.Invoke(context.Background(), input)

		// 验证 Agent 系统没有崩溃，而是收到错误
		require.Error(t, err)
		assert.Nil(t, result)

		agentErr, ok := err.(*agentErrors.AgentError)
		require.True(t, ok)
		assert.Equal(t, agentErrors.CodeInternal, agentErr.Code)

		// 系统可以继续运行
		t.Log("Agent system continues to run after third-party tool panic")
	})

	t.Run("Multiple tools, one panics", func(t *testing.T) {
		// Tool 1: 正常工作
		tool1 := NewRunnableFunc(func(ctx context.Context, input string) (string, error) {
			return "tool1 result", nil
		})

		// Tool 2: 会 panic
		tool2 := NewRunnableFunc(func(ctx context.Context, input string) (string, error) {
			panic("tool2 has a bug")
		})

		// Tool 3: 正常工作
		tool3 := NewRunnableFunc(func(ctx context.Context, input string) (string, error) {
			return "tool3 result", nil
		})

		// 依次执行所有 tools
		result1, err1 := tool1.Invoke(context.Background(), "test")
		assert.NoError(t, err1)
		assert.Equal(t, "tool1 result", result1)

		result2, err2 := tool2.Invoke(context.Background(), "test")
		assert.Error(t, err2)
		assert.Empty(t, result2)
		assert.Contains(t, err2.Error(), "panic recovered")

		// Tool 3 仍然可以正常执行
		result3, err3 := tool3.Invoke(context.Background(), "test")
		assert.NoError(t, err3)
		assert.Equal(t, "tool3 result", result3)
	})
}

// TestPanicRecovery_ErrorDetails 测试 panic 恢复的错误详情
func TestPanicRecovery_ErrorDetails(t *testing.T) {
	panicFunc := NewRunnableFunc(func(ctx context.Context, input string) (string, error) {
		panic("detailed panic test")
	})

	_, err := panicFunc.Invoke(context.Background(), "test")

	require.Error(t, err)

	agentErr, ok := err.(*agentErrors.AgentError)
	require.True(t, ok)

	// 验证组件信息（使用接口化后的新值）
	assert.Equal(t, "runnable", agentErr.Component)

	// 验证操作信息（使用接口化后的新值）
	assert.Equal(t, "invoke", agentErr.Operation)

	// 验证详细信息
	assert.Contains(t, agentErr.Context, "panic_value")
	assert.Contains(t, agentErr.Context, "stack_trace")

	// 验证堆栈包含有用信息
	stackTrace := agentErr.Context["stack_trace"].(string)
	assert.Contains(t, stackTrace, "goroutine")
	assert.True(t, strings.Contains(stackTrace, "panic") || strings.Contains(stackTrace, "runnable"))
}
