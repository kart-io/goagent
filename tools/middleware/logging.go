package middleware

import (
	"context"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/utils/json"
	loggerCore "github.com/kart-io/logger/core"
	"github.com/kart-io/logger/engines/slog"
	"github.com/kart-io/logger/option"
)

// LoggingMiddleware 提供工具调用的日志记录功能。
//
// 它记录：
//   - 工具名称和描述
//   - 输入参数（可配置是否记录）
//   - 输出结果（可配置是否记录）
//   - 执行耗时
//   - 错误信息
//
// 使用示例:
//
//	logger := core.GetLogger("tools")
//	loggingMW := NewLoggingMiddleware(
//	    WithLogger(logger),
//	    WithoutInputLogging(),  // 不记录输入（保护敏感数据）
//	)
//	wrappedTool := tools.WithMiddleware(myTool, loggingMW)
type LoggingMiddleware struct {
	*BaseToolMiddleware
	logger      loggerCore.Logger
	logInput    bool
	logOutput   bool
	maxArgBytes int // 最大参数字节数，避免日志过大
}

// LoggingOption 定义日志中间件的配置选项
type LoggingOption func(*LoggingMiddleware)

// WithLogger 设置自定义日志器
//
// 如果不设置，将使用默认的 factory.GetLogger("tools")
func WithLogger(logger loggerCore.Logger) LoggingOption {
	return func(m *LoggingMiddleware) {
		m.logger = logger
	}
}

// WithoutInputLogging 禁用输入参数的日志记录
//
// 用于保护敏感数据，如密码、API密钥等
func WithoutInputLogging() LoggingOption {
	return func(m *LoggingMiddleware) {
		m.logInput = false
	}
}

// WithoutOutputLogging 禁用输出结果的日志记录
//
// 用于减少日志量或保护敏感输出
func WithoutOutputLogging() LoggingOption {
	return func(m *LoggingMiddleware) {
		m.logOutput = false
	}
}

// WithMaxArgBytes 设置参数序列化的最大字节数
//
// 超过此限制的参数将被截断，默认值为 1024 字节
func WithMaxArgBytes(maxBytes int) LoggingOption {
	return func(m *LoggingMiddleware) {
		if maxBytes > 0 {
			m.maxArgBytes = maxBytes
		}
	}
}

// NewLoggingMiddleware 创建一个新的日志中间件
//
// 参数:
//   - opts: 配置选项
//
// 返回:
//   - *LoggingMiddleware: 日志中间件实例
func NewLoggingMiddleware(opts ...LoggingOption) *LoggingMiddleware {
	// 创建默认的 slog logger
	defaultLogger, err := slog.NewSlogLogger(&option.LogOption{
		Engine:      "slog",
		Level:       "INFO",
		Format:      "json",
		OutputPaths: []string{"stdout"},
		Development: false,
	})
	if err != nil {
		// 如果创建失败，使用 nil，后续调用会跳过日志
		defaultLogger = nil
	}

	middleware := &LoggingMiddleware{
		BaseToolMiddleware: NewBaseToolMiddleware("logging"),
		logger:             defaultLogger,
		logInput:           true,
		logOutput:          true,
		maxArgBytes:        1024, // 默认 1KB
	}

	for _, opt := range opts {
		opt(middleware)
	}

	return middleware
}

// OnBeforeInvoke 在工具调用前记录日志
func (m *LoggingMiddleware) OnBeforeInvoke(ctx context.Context, tool interfaces.Tool, input *interfaces.ToolInput) (*interfaces.ToolInput, error) {
	if m.logger == nil {
		return input, nil // 如果没有 logger，跳过日志
	}

	fields := []interface{}{
		"tool", tool.Name(),
		"caller_id", input.CallerID,
		"trace_id", input.TraceID,
	}

	// 记录输入参数（如果启用）
	if m.logInput && input.Args != nil {
		argsStr := m.serializeArgs(input.Args)
		fields = append(fields, "args", argsStr)
	}

	m.logger.Infow("Tool invocation started", fields...)

	// 将开始时间存储在 Args 中（用于传递给 OnAfterInvoke）
	// 这是安全的，因为 Chain 会将 input 传递到下一个中间件
	if input.Args == nil {
		input.Args = make(map[string]interface{})
	}
	input.Args["__logging_start_time"] = time.Now()

	return input, nil
}

// OnAfterInvoke 在工具调用后记录日志
func (m *LoggingMiddleware) OnAfterInvoke(ctx context.Context, tool interfaces.Tool, output *interfaces.ToolOutput) (*interfaces.ToolOutput, error) {
	if m.logger == nil {
		return output, nil // 如果没有 logger，跳过日志
	}

	// 计算执行耗时
	var duration time.Duration
	if output.Metadata != nil {
		if startTime, ok := output.Metadata["__logging_start_time"].(time.Time); ok {
			duration = time.Since(startTime)
			// 清理临时元数据
			delete(output.Metadata, "__logging_start_time")
		}
	}

	fields := []interface{}{
		"tool", tool.Name(),
		"success", output.Success,
		"duration_ms", duration.Milliseconds(),
	}

	// 将耗时存储到 Metadata
	if output.Metadata == nil {
		output.Metadata = make(map[string]interface{})
	}
	output.Metadata["execution_time"] = duration.Milliseconds()

	// 记录输出结果（如果启用）
	if m.logOutput && output.Result != nil {
		resultStr := m.serializeResult(output.Result)
		fields = append(fields, "result", resultStr)
	}

	// 根据成功/失败使用不同的日志级别
	if output.Success {
		m.logger.Infow("Tool invocation completed", fields...)
	} else {
		fields = append(fields, "error", output.Error)
		m.logger.Errorw("Tool invocation failed", fields...)
	}

	return output, nil
}

// OnError 在工具执行出错时记录日志
func (m *LoggingMiddleware) OnError(ctx context.Context, tool interfaces.Tool, err error) error {
	if m.logger != nil {
		m.logger.Errorw("Tool invocation error",
			"tool", tool.Name(),
			"error", err.Error(),
		)
	}
	return err
}

// serializeArgs 序列化参数为字符串，限制大小
func (m *LoggingMiddleware) serializeArgs(args map[string]interface{}) string {
	// 过滤掉内部元数据
	filtered := make(map[string]interface{})
	for k, v := range args {
		// 跳过以 __ 开头的内部键
		if len(k) < 2 || k[:2] != "__" {
			filtered[k] = v
		}
	}

	data, err := json.Marshal(filtered)
	if err != nil {
		return "<serialization error>"
	}

	// 限制大小
	if len(data) > m.maxArgBytes {
		return string(data[:m.maxArgBytes]) + "...(truncated)"
	}

	return string(data)
}

// serializeResult 序列化结果为字符串，限制大小
func (m *LoggingMiddleware) serializeResult(result interface{}) string {
	// 对于字符串类型，直接使用
	if str, ok := result.(string); ok {
		if len(str) > m.maxArgBytes {
			return str[:m.maxArgBytes] + "...(truncated)"
		}
		return str
	}

	// 其他类型尝试 JSON 序列化
	data, err := json.Marshal(result)
	if err != nil {
		return "<serialization error>"
	}

	if len(data) > m.maxArgBytes {
		return string(data[:m.maxArgBytes]) + "...(truncated)"
	}

	return string(data)
}
