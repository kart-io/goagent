package middleware

import (
	"context"
)

// ImmutableMiddlewareChain 持有固定中间件集合，与可变链共用执行协议。
type ImmutableMiddlewareChain struct {
	middlewares []Middleware
	handler     Handler
}

// NewImmutableMiddlewareChain 创建不可变中间件链
func NewImmutableMiddlewareChain(handler Handler, middlewares ...Middleware) *ImmutableMiddlewareChain {
	chain := &ImmutableMiddlewareChain{
		middlewares: make([]Middleware, len(middlewares)),
		handler:     handler,
	}
	copy(chain.middlewares, middlewares)

	return chain
}

// Execute 执行中间件链
func (c *ImmutableMiddlewareChain) Execute(ctx context.Context, request *MiddlewareRequest) (*MiddlewareResponse, error) {
	return executeMiddleware(ctx, request, c.handler, c.middlewares, true)
}

// Middlewares 返回中间件列表
func (c *ImmutableMiddlewareChain) Middlewares() []Middleware {
	return append([]Middleware(nil), c.middlewares...)
}

// FastMiddlewareChain 快速中间件链（无 OnAfter 支持）
//
// 用于只需要 OnBefore 的场景，进一步减少开销
type FastMiddlewareChain struct {
	middlewares []Middleware
	handler     Handler
}

// NewFastMiddlewareChain 创建快速中间件链
func NewFastMiddlewareChain(handler Handler, middlewares ...Middleware) *FastMiddlewareChain {
	return &FastMiddlewareChain{
		middlewares: append([]Middleware(nil), middlewares...),
		handler:     handler,
	}
}

// Execute 执行（仅 OnBefore）
//
//go:inline
func (c *FastMiddlewareChain) Execute(ctx context.Context, request *MiddlewareRequest) (*MiddlewareResponse, error) {
	return executeMiddleware(ctx, request, c.handler, c.middlewares, false)
}

// Middlewares 返回中间件列表
func (c *FastMiddlewareChain) Middlewares() []Middleware {
	return append([]Middleware(nil), c.middlewares...)
}
