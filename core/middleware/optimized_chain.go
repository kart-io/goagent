package middleware

import (
	"context"
)

// ImmutableMiddlewareChain holds a fixed set of middleware and shares the
// execution protocol with the mutable chain.
type ImmutableMiddlewareChain struct {
	middlewares []Middleware
	handler     Handler
}

// NewImmutableMiddlewareChain creates an immutable middleware chain.
func NewImmutableMiddlewareChain(handler Handler, middlewares ...Middleware) *ImmutableMiddlewareChain {
	chain := &ImmutableMiddlewareChain{
		middlewares: make([]Middleware, len(middlewares)),
		handler:     handler,
	}
	copy(chain.middlewares, middlewares)

	return chain
}

// Execute runs the middleware chain.
func (c *ImmutableMiddlewareChain) Execute(ctx context.Context, request *MiddlewareRequest) (*MiddlewareResponse, error) {
	return executeMiddleware(ctx, request, c.handler, c.middlewares, true)
}

// Middlewares returns the list of middleware.
func (c *ImmutableMiddlewareChain) Middlewares() []Middleware {
	return append([]Middleware(nil), c.middlewares...)
}

// FastMiddlewareChain is a fast middleware chain (without OnAfter support).
//
// It is for scenarios that only need OnBefore, further reducing overhead.
type FastMiddlewareChain struct {
	middlewares []Middleware
	handler     Handler
}

// NewFastMiddlewareChain creates a fast middleware chain.
func NewFastMiddlewareChain(handler Handler, middlewares ...Middleware) *FastMiddlewareChain {
	return &FastMiddlewareChain{
		middlewares: append([]Middleware(nil), middlewares...),
		handler:     handler,
	}
}

// Execute runs the chain (OnBefore only).
//
//go:inline
func (c *FastMiddlewareChain) Execute(ctx context.Context, request *MiddlewareRequest) (*MiddlewareResponse, error) {
	return executeMiddleware(ctx, request, c.handler, c.middlewares, false)
}

// Middlewares returns the list of middleware.
func (c *FastMiddlewareChain) Middlewares() []Middleware {
	return append([]Middleware(nil), c.middlewares...)
}
