package middleware

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
	loggerCore "github.com/kart-io/logger/core"
)

// mockLogger 用于测试的模拟日志器
type mockLogger struct {
	infoLogs  []logEntry
	errorLogs []logEntry
}

type logEntry struct {
	message string
	fields  map[string]interface{}
}

func newMockLogger() *mockLogger {
	return &mockLogger{
		infoLogs:  make([]logEntry, 0),
		errorLogs: make([]logEntry, 0),
	}
}

func (m *mockLogger) Info(args ...interface{})  {}
func (m *mockLogger) Error(args ...interface{}) {}

func (m *mockLogger) Infow(msg string, keysAndValues ...interface{}) {
	entry := logEntry{
		message: msg,
		fields:  parseFields(keysAndValues),
	}
	m.infoLogs = append(m.infoLogs, entry)
}

func (m *mockLogger) Errorw(msg string, keysAndValues ...interface{}) {
	entry := logEntry{
		message: msg,
		fields:  parseFields(keysAndValues),
	}
	m.errorLogs = append(m.errorLogs, entry)
}

func (m *mockLogger) Debug(args ...interface{})                       {}
func (m *mockLogger) Warn(args ...interface{})                        {}
func (m *mockLogger) Fatal(args ...interface{})                       {}
func (m *mockLogger) Debugf(template string, args ...interface{})     {}
func (m *mockLogger) Infof(template string, args ...interface{})      {}
func (m *mockLogger) Warnf(template string, args ...interface{})      {}
func (m *mockLogger) Errorf(template string, args ...interface{})     {}
func (m *mockLogger) Fatalf(template string, args ...interface{})     {}
func (m *mockLogger) Debugw(msg string, keysAndValues ...interface{}) {}
func (m *mockLogger) Warnw(msg string, keysAndValues ...interface{})  {}
func (m *mockLogger) Fatalw(msg string, keysAndValues ...interface{}) {}
func (m *mockLogger) With(keyValues ...interface{}) loggerCore.Logger { return m }
func (m *mockLogger) WithCtx(ctx context.Context, keyValues ...interface{}) loggerCore.Logger {
	return m
}
func (m *mockLogger) WithCallerSkip(skip int) loggerCore.Logger { return m }
func (m *mockLogger) SetLevel(level loggerCore.Level)           {}
func (m *mockLogger) Flush() error                              { return nil }

// parseFields 将键值对数组转换为 map
func parseFields(keysAndValues []interface{}) map[string]interface{} {
	fields := make(map[string]interface{})
	for i := 0; i < len(keysAndValues)-1; i += 2 {
		key, ok := keysAndValues[i].(string)
		if ok {
			fields[key] = keysAndValues[i+1]
		}
	}
	return fields
}

// TestLoggingMiddleware_Basic 测试基本日志功能
func TestLoggingMiddleware_Basic(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()
	logger := newMockLogger()

	middleware := NewLoggingMiddleware(WithLogger(logger))

	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		// 模拟一些处理时间
		time.Sleep(10 * time.Millisecond)
		return &interfaces.ToolOutput{
			Result:  "test result",
			Success: true,
		}, nil
	}

	wrapped := Chain(tool, invoker, middleware)
	input := &interfaces.ToolInput{
		Args:     map[string]interface{}{"key": "value"},
		CallerID: "test_caller",
		TraceID:  "trace_123",
	}

	output, err := wrapped(ctx, input)
	require.NoError(t, err)
	assert.True(t, output.Success)

	// 验证日志记录
	assert.Len(t, logger.infoLogs, 2, "Should have 2 info logs (before and after)")

	// 验证 before 日志
	beforeLog := logger.infoLogs[0]
	assert.Equal(t, "Tool invocation started", beforeLog.message)
	assert.Equal(t, "test_tool", beforeLog.fields["tool"])
	assert.Equal(t, "test_caller", beforeLog.fields["caller_id"])
	assert.Equal(t, "trace_123", beforeLog.fields["trace_id"])
	assert.Contains(t, beforeLog.fields["args"], "key")

	// 验证 after 日志
	afterLog := logger.infoLogs[1]
	assert.Equal(t, "Tool invocation completed", afterLog.message)
	assert.Equal(t, "test_tool", afterLog.fields["tool"])
	assert.True(t, afterLog.fields["success"].(bool))
	assert.Greater(t, afterLog.fields["duration_ms"], int64(0))
	assert.Contains(t, afterLog.fields["result"], "test result")

	// 验证 Metadata 中的 execution_time
	assert.NotNil(t, output.Metadata)
	assert.Contains(t, output.Metadata, "execution_time")
	assert.Greater(t, output.Metadata["execution_time"].(int64), int64(0))
}

// TestLoggingMiddleware_WithoutInputLogging 测试禁用输入日志
func TestLoggingMiddleware_WithoutInputLogging(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()
	logger := newMockLogger()

	middleware := NewLoggingMiddleware(
		WithLogger(logger),
		WithoutInputLogging(),
	)

	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{
			Result:  "result",
			Success: true,
		}, nil
	}

	wrapped := Chain(tool, invoker, middleware)
	input := &interfaces.ToolInput{
		Args: map[string]interface{}{"secret": "password123"},
	}

	_, err := wrapped(ctx, input)
	require.NoError(t, err)

	// 验证输入参数未被记录
	beforeLog := logger.infoLogs[0]
	assert.NotContains(t, beforeLog.fields, "args", "Args should not be logged")
}

// TestLoggingMiddleware_WithoutOutputLogging 测试禁用输出日志
func TestLoggingMiddleware_WithoutOutputLogging(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()
	logger := newMockLogger()

	middleware := NewLoggingMiddleware(
		WithLogger(logger),
		WithoutOutputLogging(),
	)

	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{
			Result:  "sensitive result",
			Success: true,
		}, nil
	}

	wrapped := Chain(tool, invoker, middleware)
	input := &interfaces.ToolInput{Args: map[string]interface{}{}}

	_, err := wrapped(ctx, input)
	require.NoError(t, err)

	// 验证输出结果未被记录
	afterLog := logger.infoLogs[1]
	assert.NotContains(t, afterLog.fields, "result", "Result should not be logged")
}

// TestLoggingMiddleware_MaxArgBytes 测试参数截断
func TestLoggingMiddleware_MaxArgBytes(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()
	logger := newMockLogger()

	middleware := NewLoggingMiddleware(
		WithLogger(logger),
		WithMaxArgBytes(50), // 设置很小的限制
	)

	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{
			Result:  "result",
			Success: true,
		}, nil
	}

	wrapped := Chain(tool, invoker, middleware)
	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"long_key": strings.Repeat("a", 200), // 很长的值
		},
	}

	_, err := wrapped(ctx, input)
	require.NoError(t, err)

	// 验证参数被截断
	beforeLog := logger.infoLogs[0]
	argsStr, ok := beforeLog.fields["args"].(string)
	require.True(t, ok)
	assert.Contains(t, argsStr, "truncated", "Long args should be truncated")
	// 截断后的字符串应该是 maxArgBytes + len("...(truncated)")
	assert.LessOrEqual(t, len(argsStr), 50+len("...(truncated)")+5, "Truncated string should be limited")
}

// TestLoggingMiddleware_ErrorLogging 测试错误日志
func TestLoggingMiddleware_ErrorLogging(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()
	logger := newMockLogger()

	middleware := NewLoggingMiddleware(WithLogger(logger))

	testErr := errors.New("test error")
	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return nil, testErr
	}

	wrapped := Chain(tool, invoker, middleware)
	input := &interfaces.ToolInput{Args: map[string]interface{}{}}

	_, err := wrapped(ctx, input)
	require.Error(t, err)

	// 验证错误日志
	assert.Len(t, logger.infoLogs, 1, "Should have 1 info log (before)")
	assert.Len(t, logger.errorLogs, 1, "Should have 1 error log")

	errorLog := logger.errorLogs[0]
	assert.Equal(t, "Tool invocation error", errorLog.message)
	assert.Equal(t, "test_tool", errorLog.fields["tool"])
	assert.Equal(t, "test error", errorLog.fields["error"])
}

// TestLoggingMiddleware_FailedOutput 测试失败的输出日志
func TestLoggingMiddleware_FailedOutput(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()
	logger := newMockLogger()

	middleware := NewLoggingMiddleware(WithLogger(logger))

	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{
			Result:  nil,
			Success: false,
			Error:   "execution failed",
		}, nil
	}

	wrapped := Chain(tool, invoker, middleware)
	input := &interfaces.ToolInput{Args: map[string]interface{}{}}

	output, err := wrapped(ctx, input)
	require.NoError(t, err)
	assert.False(t, output.Success)

	// 验证使用 Error 级别记录失败
	assert.Len(t, logger.errorLogs, 1, "Failed output should be logged as error")

	errorLog := logger.errorLogs[0]
	assert.Equal(t, "Tool invocation failed", errorLog.message)
	assert.False(t, errorLog.fields["success"].(bool))
	assert.Equal(t, "execution failed", errorLog.fields["error"])
}

// TestLoggingMiddleware_StringResult 测试字符串结果的序列化
func TestLoggingMiddleware_StringResult(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()
	logger := newMockLogger()

	middleware := NewLoggingMiddleware(WithLogger(logger))

	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{
			Result:  "simple string result",
			Success: true,
		}, nil
	}

	wrapped := Chain(tool, invoker, middleware)
	input := &interfaces.ToolInput{Args: map[string]interface{}{}}

	_, err := wrapped(ctx, input)
	require.NoError(t, err)

	afterLog := logger.infoLogs[1]
	assert.Equal(t, "simple string result", afterLog.fields["result"])
}

// TestLoggingMiddleware_ComplexResult 测试复杂结果的序列化
func TestLoggingMiddleware_ComplexResult(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()
	logger := newMockLogger()

	middleware := NewLoggingMiddleware(WithLogger(logger))

	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{
			Result: map[string]interface{}{
				"count": 42,
				"items": []string{"a", "b", "c"},
			},
			Success: true,
		}, nil
	}

	wrapped := Chain(tool, invoker, middleware)
	input := &interfaces.ToolInput{Args: map[string]interface{}{}}

	_, err := wrapped(ctx, input)
	require.NoError(t, err)

	afterLog := logger.infoLogs[1]
	resultStr, ok := afterLog.fields["result"].(string)
	require.True(t, ok)
	assert.Contains(t, resultStr, "count")
	assert.Contains(t, resultStr, "42")
}

// TestLoggingMiddleware_NilOutput 测试 nil 输出的处理
func TestLoggingMiddleware_NilOutput(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()
	logger := newMockLogger()

	middleware := NewLoggingMiddleware(WithLogger(logger))

	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{
			Result:  nil,
			Success: true,
		}, nil
	}

	wrapped := Chain(tool, invoker, middleware)
	input := &interfaces.ToolInput{Args: map[string]interface{}{}}

	_, err := wrapped(ctx, input)
	require.NoError(t, err)

	afterLog := logger.infoLogs[1]
	assert.NotContains(t, afterLog.fields, "result", "Nil result should not be logged")
}
