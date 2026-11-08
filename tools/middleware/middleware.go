package middleware

import (
	"context"

	"github.com/kart-io/goagent/interfaces"
)

// ToolInvoker 是一次工具调用；中间件须保留结果与错误的组合语义。
type ToolInvoker func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error)

// ToolMiddlewareFunc 接收下一层调用器并返回包装器。
// 装配按逆序进行，因此列表中的第一个函数处于最外层；每次调用的临时状态须留在调用闭包内。
type ToolMiddlewareFunc func(interfaces.Tool, ToolInvoker) ToolInvoker
