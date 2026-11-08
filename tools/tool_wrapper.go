package tools

import (
	"context"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools/middleware"
)

// MiddlewareTool wraps a tool with middleware support.
//
// It implements the Tool interface and applies middleware to tool invocations.
// This allows adding cross-cutting concerns like logging, caching, and rate limiting.
//
// 使用示例:
//
//	tool := NewCalculatorTool()
//	wrappedTool := tools.WithMiddleware(tool,
//	    middleware.Logging(),
//	    middleware.Caching(middleware.WithTTL(5*time.Minute)),
//	    middleware.RateLimit(middleware.WithQPS(10)),
//	)
type MiddlewareTool struct {
	tool    interfaces.Tool
	invoker middleware.ToolInvoker
}

// WithMiddleware 按传入顺序装配函数式中间件，第一个位于最外层。
// 需要读取对象统计时，调用方保留对象并显式传入其 Wrap 方法。
func WithMiddleware(tool interfaces.Tool, middlewares ...middleware.ToolMiddlewareFunc) interfaces.Tool {
	if len(middlewares) == 0 {
		return tool
	}

	// 基础调用器：直接调用工具
	baseInvoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return tool.Invoke(ctx, input)
	}

	// 应用所有中间件
	var invoker middleware.ToolInvoker = baseInvoker

	// 从后向前应用（洋葱模型），这样第一个中间件在最外层
	for i := len(middlewares) - 1; i >= 0; i-- {
		mw := middlewares[i]
		if mw == nil {
			panic(agentErrors.New(agentErrors.CodeInvalidConfig, "工具中间件不能为空").WithComponent("tool_middleware").WithOperation("assemble"))
		}
		invoker = mw(tool, invoker)
		if invoker == nil {
			panic(agentErrors.New(agentErrors.CodeInvalidConfig, "工具中间件必须返回调用器").WithComponent("tool_middleware").WithOperation("assemble"))
		}
	}

	return &MiddlewareTool{
		tool:    tool,
		invoker: invoker,
	}
}

// Name 返回工具名称
func (w *MiddlewareTool) Name() string {
	return w.tool.Name()
}

// Description 返回工具描述
func (w *MiddlewareTool) Description() string {
	return w.tool.Description()
}

// ArgsSchema 返回参数模式
func (w *MiddlewareTool) ArgsSchema() string {
	return w.tool.ArgsSchema()
}

// Invoke 执行工具调用（通过中间件）
func (w *MiddlewareTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return w.invoker(ctx, input)
}

// Unwrap 返回被包装的原始工具
//
// 这允许访问原始工具的方法或检查工具类型
func (w *MiddlewareTool) Unwrap() interfaces.Tool {
	return w.tool
}
