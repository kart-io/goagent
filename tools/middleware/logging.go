package middleware

import (
	"context"
	"time"

	"github.com/kart-io/goagent/codec/json"

	loggerCore "github.com/kart-io/logger/core"
	"github.com/kart-io/logger/engines/slog"
	"github.com/kart-io/logger/option"

	"github.com/kart-io/goagent/interfaces"
)

// LoggingMiddleware logs tool calls.
//
// It records:
//   - tool name and description
//   - input arguments (not logged by default; enable with WithInputLogging)
//   - output results (not logged by default; enable with WithOutputLogging)
//   - execution duration
//   - error information
//
// Arguments and results often contain user data and credentials, so by
// default only the call itself is logged.
//
// Example:
//
//	logger := core.GetLogger("tools")
//	loggingMW := NewLoggingMiddleware(
//	    WithLogger(logger),
//	    WithOutputLogging(), // log results; enable only when they contain no sensitive data
//	)
//	wrappedTool := middleware.Wrap(myTool, loggingMW.Wrap)
type LoggingMiddleware struct {
	logger      loggerCore.Logger
	logInput    bool
	logOutput   bool
	maxArgBytes int // 最大参数字节数，避免日志过大
}

// LoggingOption is a configuration option for the logging middleware.
type LoggingOption func(*LoggingMiddleware)

// WithLogger sets a custom logger.
//
// When unset, a JSON slog logger writing to stdout is used; passing nil
// explicitly disables logging.
func WithLogger(logger loggerCore.Logger) LoggingOption {
	return func(m *LoggingMiddleware) {
		m.logger = logger
	}
}

// WithInputLogging logs input arguments (except internal keys starting with
// __); do not enable it when arguments may contain passwords, API keys, etc.
func WithInputLogging() LoggingOption {
	return func(m *LoggingMiddleware) {
		m.logInput = true
	}
}

// WithOutputLogging logs output results; do not enable it when results may
// contain sensitive data.
func WithOutputLogging() LoggingOption {
	return func(m *LoggingMiddleware) {
		m.logOutput = true
	}
}

// WithMaxArgBytes sets the maximum number of bytes for serialized arguments.
//
// Arguments exceeding this limit are truncated; the default is 1024 bytes.
func WithMaxArgBytes(maxBytes int) LoggingOption {
	return func(m *LoggingMiddleware) {
		if maxBytes > 0 {
			m.maxArgBytes = maxBytes
		}
	}
}

// NewLoggingMiddleware creates a new logging middleware.
//
// Parameters:
//   - opts: configuration options
//
// Returns:
//   - *LoggingMiddleware: the logging middleware instance
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
		logger:      defaultLogger,
		maxArgBytes: 1024, // 默认 1KB
	}

	for _, opt := range opts {
		opt(middleware)
	}

	return middleware
}

// Wrap times each call independently and does not pass internal state through
// business arguments or metadata. Logging does not change execution errors or
// their partial results; empty input and output are passed through unchanged.
func (m *LoggingMiddleware) Wrap(tool interfaces.Tool, next ToolInvoker) ToolInvoker {
	return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		if m.logger == nil {
			return next(ctx, input)
		}
		started := time.Now()
		fields := []interface{}{"tool", tool.Name()}
		if input != nil {
			fields = append(fields, "caller_id", input.CallerID, "trace_id", input.TraceID)
			if m.logInput && input.Args != nil {
				fields = append(fields, "args", m.serializeArgs(input.Args))
			}
		}
		m.logger.Infow("Tool invocation started", fields...)

		output, err := next(ctx, input)
		duration := time.Since(started).Milliseconds()
		if err != nil {
			m.logger.Errorw("Tool invocation error", "tool", tool.Name(), "error", err.Error(), "duration_ms", duration)
			return output, err
		}
		fields = []interface{}{"tool", tool.Name(), "duration_ms", duration}
		if output == nil {
			m.logger.Infow("Tool invocation completed", fields...)
			return nil, nil
		}
		fields = append(fields, "success", output.Success)
		if output.Metadata == nil {
			output.Metadata = make(map[string]interface{})
		}
		output.Metadata["execution_time"] = duration
		if m.logOutput && output.Result != nil {
			fields = append(fields, "result", m.serializeResult(output.Result))
		}
		if output.Success {
			m.logger.Infow("Tool invocation completed", fields...)
		} else {
			fields = append(fields, "error", output.Error)
			m.logger.Errorw("Tool invocation failed", fields...)
		}
		return output, nil
	}
}

// Logging returns a logging middleware function ready for assembly.
func Logging(opts ...LoggingOption) ToolMiddlewareFunc {
	return NewLoggingMiddleware(opts...).Wrap
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
