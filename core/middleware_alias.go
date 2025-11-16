package core

import (
	"github.com/kart-io/goagent/core/middleware"
)

// Type aliases for backward compatibility
// These types have been moved to the middleware package

type (
	Middleware         = middleware.Middleware
	BaseMiddleware     = middleware.BaseMiddleware
	MiddlewareRequest  = middleware.MiddlewareRequest
	MiddlewareResponse = middleware.MiddlewareResponse
	Handler            = middleware.Handler
	MiddlewareChain    = middleware.MiddlewareChain
)

// Constructor functions
var (
	NewBaseMiddleware           = middleware.NewBaseMiddleware
	NewMiddlewareChain          = middleware.NewMiddlewareChain
	NewLoggingMiddleware        = middleware.NewLoggingMiddleware
	NewTimingMiddleware         = middleware.NewTimingMiddleware
	NewCacheMiddleware          = middleware.NewCacheMiddleware
	NewRetryMiddleware          = middleware.NewRetryMiddleware
	NewTransformMiddleware      = middleware.NewTransformMiddleware
	NewCircuitBreakerMiddleware = middleware.NewCircuitBreakerMiddleware
	NewRateLimiterMiddleware    = middleware.NewRateLimiterMiddleware
	NewValidationMiddleware     = middleware.NewValidationMiddleware
	NewAuthenticationMiddleware = middleware.NewAuthenticationMiddleware
	NewDynamicPromptMiddleware  = middleware.NewDynamicPromptMiddleware
	NewToolSelectorMiddleware   = middleware.NewToolSelectorMiddleware
)
