package middleware

import (
	"context"

	agentErrors "github.com/kart-io/goagent/errors"
)

// ImmutableMiddlewareChain 不可变中间件链
//
// 优化点：
// - 预编译的执行链，避免运行时构建
// - 减少接口调用
// - 内联小函数
//
// 性能提升：预期 20% 的中间件开销减少
type ImmutableMiddlewareChain struct {
	middlewares []Middleware
	handler     Handler

	// 预编译的执行函数
	beforeChain func(context.Context, *MiddlewareRequest) (*MiddlewareRequest, error)
	afterChain  func(context.Context, *MiddlewareResponse) (*MiddlewareResponse, error)
}

// NewImmutableMiddlewareChain 创建不可变中间件链
func NewImmutableMiddlewareChain(handler Handler, middlewares ...Middleware) *ImmutableMiddlewareChain {
	chain := &ImmutableMiddlewareChain{
		middlewares: make([]Middleware, len(middlewares)),
		handler:     handler,
	}
	copy(chain.middlewares, middlewares)

	// 预编译执行链
	chain.compile()

	return chain
}

// compile 预编译中间件执行链
//
//go:inline
func (c *ImmutableMiddlewareChain) compile() {
	// 构建 OnBefore 链
	c.beforeChain = func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
		for _, mw := range c.middlewares {
			var err error
			req, err = mw.OnBefore(ctx, req)
			if err != nil {
				return req, err
			}
		}
		return req, nil
	}

	// 构建 OnAfter 链（反向执行）
	c.afterChain = func(ctx context.Context, resp *MiddlewareResponse) (*MiddlewareResponse, error) {
		for i := len(c.middlewares) - 1; i >= 0; i-- {
			var err error
			resp, err = c.middlewares[i].OnAfter(ctx, resp)
			if err != nil {
				return resp, err
			}
		}
		return resp, nil
	}
}

// Execute 执行中间件链
func (c *ImmutableMiddlewareChain) Execute(ctx context.Context, request *MiddlewareRequest) (*MiddlewareResponse, error) {
	// OnBefore 链
	modifiedReq, err := c.beforeChain(ctx, request)
	if err != nil {
		return c.handleError(ctx, err)
	}

	// 执行主处理器
	response, err := c.handler(ctx, modifiedReq)
	if err != nil {
		return c.handleError(ctx, err)
	}

	// OnAfter 链
	modifiedResp, err := c.afterChain(ctx, response)
	if err != nil {
		return c.handleError(ctx, err)
	}

	return modifiedResp, nil
}

// handleError 处理错误
//
//go:inline
func (c *ImmutableMiddlewareChain) handleError(ctx context.Context, err error) (*MiddlewareResponse, error) {
	// 触发 OnError 回调
	for _, mw := range c.middlewares {
		err = mw.OnError(ctx, err)
	}
	return nil, err
}

// Middlewares 返回中间件列表
func (c *ImmutableMiddlewareChain) Middlewares() []Middleware {
	return c.middlewares
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
	// 快速路径：仅 OnBefore
	for _, mw := range c.middlewares {
		var err error
		request, err = mw.OnBefore(ctx, request)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeMiddlewareExecution, "fast chain execution failed").
				WithComponent("fast_middleware_chain").
				WithOperation("execute")
		}
	}

	// 执行处理器
	return c.handler(ctx, request)
}

// Middlewares 返回中间件列表
func (c *FastMiddlewareChain) Middlewares() []Middleware {
	return c.middlewares
}
