package tools

import (
	"context"

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

// WithMiddleware 为工具应用中间件
//
// 支持两种中间件类型：
//   - ToolMiddleware: 基于接口的中间件（旧接口，已废弃）
//   - ToolMiddlewareFunc: 基于函数的中间件（推荐）
//
// 参数:
//   - tool: 要包装的工具
//   - middlewares: 中间件列表，按顺序应用
//
// 返回:
//   - interfaces.Tool: 包装后的工具
//
// 使用示例:
//
//	// 使用函数式中间件（推荐）
//	wrappedTool := WithMiddleware(tool,
//	    middleware.Logging(),
//	    middleware.Caching(),
//	)
//
//	// 使用接口式中间件（旧接口）
//	wrappedTool := WithMiddleware(tool,
//	    middleware.NewLoggingMiddleware(),
//	    middleware.NewCachingMiddleware(),
//	)
func WithMiddleware(tool interfaces.Tool, middlewares ...interface{}) interfaces.Tool {
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

		switch m := mw.(type) {
		case middleware.ToolMiddlewareFunc:
			// 函数式中间件（推荐）
			invoker = m(tool, invoker)

		case middleware.ToolMiddleware:
			// 接口式中间件（旧接口，通过 Chain 转换）
			invoker = middleware.Chain(tool, invoker, m)

		case func(interfaces.Tool, middleware.ToolInvoker) middleware.ToolInvoker:
			// 直接的函数类型
			invoker = m(tool, invoker)

		default:
			// 忽略不支持的类型
			continue
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
