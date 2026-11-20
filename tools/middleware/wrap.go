package middleware

import (
	"context"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// WrappedTool is a tool with middleware assembled by Wrap.
//
// Example:
//
//	wrapped := middleware.Wrap(tool,
//	    middleware.Logging(),
//	    middleware.Caching(middleware.WithTTL(5*time.Minute)),
//	    middleware.RateLimit(middleware.WithQPS(10)),
//	)
type WrappedTool struct {
	tool    interfaces.Tool
	invoker ToolInvoker
}

// Wrap assembles functional middleware in the given order, with the first one
// outermost. When object statistics are needed, the caller keeps the object
// and passes its Wrap method explicitly.
func Wrap(tool interfaces.Tool, middlewares ...ToolMiddlewareFunc) interfaces.Tool {
	if len(middlewares) == 0 {
		return tool
	}

	// 基础调用器：直接调用工具
	baseInvoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return tool.Invoke(ctx, input)
	}

	// 应用所有中间件
	var invoker ToolInvoker = baseInvoker

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

	return &WrappedTool{
		tool:    tool,
		invoker: invoker,
	}
}

// Name returns the tool name.
func (w *WrappedTool) Name() string {
	return w.tool.Name()
}

// Description returns the tool description.
func (w *WrappedTool) Description() string {
	return w.tool.Description()
}

// ArgsSchema returns the argument schema.
func (w *WrappedTool) ArgsSchema() string {
	return w.tool.ArgsSchema()
}

// Invoke executes the tool call (through the middleware).
func (w *WrappedTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return w.invoker(ctx, input)
}

// SupportsConcurrentCalls implements interfaces.ConcurrentCallTool by
// forwarding the wrapped tool's declaration; undeclared tools stay serial.
func (w *WrappedTool) SupportsConcurrentCalls() bool {
	c, ok := w.tool.(interfaces.ConcurrentCallTool)
	return ok && c.SupportsConcurrentCalls()
}

// Unwrap returns the original wrapped tool.
//
// This allows access to the original tool's methods or checking its type.
func (w *WrappedTool) Unwrap() interfaces.Tool {
	return w.tool
}
