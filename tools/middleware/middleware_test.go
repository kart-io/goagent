package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
)

// mockTool 是用于测试的模拟工具
type mockTool struct {
	name        string
	description string
	argsSchema  string
}

func (m *mockTool) Name() string {
	return m.name
}

func (m *mockTool) Description() string {
	return m.description
}

func (m *mockTool) ArgsSchema() string {
	return m.argsSchema
}

func (m *mockTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return &interfaces.ToolOutput{
		Result:  "success",
		Success: true,
	}, nil
}

// newMockTool 创建测试用的模拟工具
func newMockTool() *mockTool {
	return &mockTool{
		name:        "test_tool",
		description: "A test tool",
		argsSchema:  `{"type": "object"}`,
	}
}

// TestBaseToolMiddleware 测试基础中间件的默认行为
func TestBaseToolMiddleware(t *testing.T) {
	middleware := NewBaseToolMiddleware("test")
	ctx := context.Background()
	tool := newMockTool()

	t.Run("Name", func(t *testing.T) {
		assert.Equal(t, "test", middleware.Name())
	})

	t.Run("OnBeforeInvoke_NoOp", func(t *testing.T) {
		input := &interfaces.ToolInput{
			Args: map[string]interface{}{"key": "value"},
		}
		result, err := middleware.OnBeforeInvoke(ctx, tool, input)
		require.NoError(t, err)
		assert.Equal(t, input, result)
	})

	t.Run("OnAfterInvoke_NoOp", func(t *testing.T) {
		output := &interfaces.ToolOutput{
			Result:  "test",
			Success: true,
		}
		result, err := middleware.OnAfterInvoke(ctx, tool, output)
		require.NoError(t, err)
		assert.Equal(t, output, result)
	})

	t.Run("OnError_PassThrough", func(t *testing.T) {
		originalErr := errors.New("test error")
		result := middleware.OnError(ctx, tool, originalErr)
		assert.Equal(t, originalErr, result)
	})
}

// TestChain_NoMiddleware 测试无中间件时的链式调用
func TestChain_NoMiddleware(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()

	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{
			Result:  "base result",
			Success: true,
		}, nil
	}

	wrapped := Chain(tool, invoker)
	input := &interfaces.ToolInput{Args: map[string]interface{}{}}

	output, err := wrapped(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, "base result", output.Result)
}

// TestChain_SingleMiddleware 测试单个中间件的链式调用
func TestChain_SingleMiddleware(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()

	// 创建追踪中间件
	var executionOrder []string
	trackingMiddleware := &trackingMiddleware{
		BaseToolMiddleware: NewBaseToolMiddleware("tracking"),
		order:              &executionOrder,
	}

	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		executionOrder = append(executionOrder, "invoke")
		return &interfaces.ToolOutput{
			Result:  "result",
			Success: true,
		}, nil
	}

	wrapped := Chain(tool, invoker, trackingMiddleware)
	input := &interfaces.ToolInput{Args: map[string]interface{}{}}

	output, err := wrapped(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, "result", output.Result)

	// 验证执行顺序：before -> invoke -> after
	assert.Equal(t, []string{"before", "invoke", "after"}, executionOrder)
}

// TestChain_MultipleMiddleware 测试多个中间件的洋葱模型
func TestChain_MultipleMiddleware(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()

	var executionOrder []string

	middleware1 := &trackingMiddleware{
		BaseToolMiddleware: NewBaseToolMiddleware("mw1"),
		order:              &executionOrder,
		prefix:             "mw1",
	}
	middleware2 := &trackingMiddleware{
		BaseToolMiddleware: NewBaseToolMiddleware("mw2"),
		order:              &executionOrder,
		prefix:             "mw2",
	}
	middleware3 := &trackingMiddleware{
		BaseToolMiddleware: NewBaseToolMiddleware("mw3"),
		order:              &executionOrder,
		prefix:             "mw3",
	}

	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		executionOrder = append(executionOrder, "invoke")
		return &interfaces.ToolOutput{
			Result:  "result",
			Success: true,
		}, nil
	}

	// 按顺序应用中间件
	wrapped := Chain(tool, invoker, middleware1, middleware2, middleware3)
	input := &interfaces.ToolInput{Args: map[string]interface{}{}}

	output, err := wrapped(ctx, input)
	require.NoError(t, err)
	assert.True(t, output.Success)

	// 验证洋葱模型执行顺序
	expected := []string{
		"mw1:before", "mw2:before", "mw3:before",
		"invoke",
		"mw3:after", "mw2:after", "mw1:after",
	}
	assert.Equal(t, expected, executionOrder)
}

// TestChain_ErrorInBefore 测试前置处理中的错误处理
func TestChain_ErrorInBefore(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()

	middleware := &errorMiddleware{
		BaseToolMiddleware: NewBaseToolMiddleware("error"),
		failBefore:         true,
	}

	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		t.Fatal("invoker should not be called")
		return nil, nil
	}

	wrapped := Chain(tool, invoker, middleware)
	input := &interfaces.ToolInput{Args: map[string]interface{}{}}

	_, err := wrapped(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "before error")
}

// TestChain_ErrorInInvoke 测试调用时的错误处理
func TestChain_ErrorInInvoke(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()

	var errorHandled bool
	middleware := &errorMiddleware{
		BaseToolMiddleware: NewBaseToolMiddleware("error"),
		onErrorFunc: func(ctx context.Context, tool interfaces.Tool, err error) error {
			errorHandled = true
			return err
		},
	}

	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return nil, errors.New("invoke error")
	}

	wrapped := Chain(tool, invoker, middleware)
	input := &interfaces.ToolInput{Args: map[string]interface{}{}}

	_, err := wrapped(ctx, input)
	require.Error(t, err)
	assert.True(t, errorHandled, "OnError should be called")
	assert.Contains(t, err.Error(), "invoke error")
}

// TestChain_ErrorInAfter 测试后置处理中的错误处理
func TestChain_ErrorInAfter(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()

	middleware := &errorMiddleware{
		BaseToolMiddleware: NewBaseToolMiddleware("error"),
		failAfter:          true,
	}

	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{
			Result:  "success",
			Success: true,
		}, nil
	}

	wrapped := Chain(tool, invoker, middleware)
	input := &interfaces.ToolInput{Args: map[string]interface{}{}}

	_, err := wrapped(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after error")
}

// trackingMiddleware 用于追踪执行顺序的中间件
type trackingMiddleware struct {
	*BaseToolMiddleware
	order  *[]string
	prefix string
}

func (m *trackingMiddleware) OnBeforeInvoke(ctx context.Context, tool interfaces.Tool, input *interfaces.ToolInput) (*interfaces.ToolInput, error) {
	if m.prefix != "" {
		*m.order = append(*m.order, m.prefix+":before")
	} else {
		*m.order = append(*m.order, "before")
	}
	return input, nil
}

func (m *trackingMiddleware) OnAfterInvoke(ctx context.Context, tool interfaces.Tool, output *interfaces.ToolOutput) (*interfaces.ToolOutput, error) {
	if m.prefix != "" {
		*m.order = append(*m.order, m.prefix+":after")
	} else {
		*m.order = append(*m.order, "after")
	}
	return output, nil
}

// errorMiddleware 用于测试错误处理的中间件
type errorMiddleware struct {
	*BaseToolMiddleware
	failBefore  bool
	failAfter   bool
	onErrorFunc func(context.Context, interfaces.Tool, error) error
}

func (m *errorMiddleware) OnBeforeInvoke(ctx context.Context, tool interfaces.Tool, input *interfaces.ToolInput) (*interfaces.ToolInput, error) {
	if m.failBefore {
		return nil, errors.New("before error")
	}
	return input, nil
}

func (m *errorMiddleware) OnAfterInvoke(ctx context.Context, tool interfaces.Tool, output *interfaces.ToolOutput) (*interfaces.ToolOutput, error) {
	if m.failAfter {
		return nil, errors.New("after error")
	}
	return output, nil
}

func (m *errorMiddleware) OnError(ctx context.Context, tool interfaces.Tool, err error) error {
	if m.onErrorFunc != nil {
		return m.onErrorFunc(ctx, tool, err)
	}
	return err
}
