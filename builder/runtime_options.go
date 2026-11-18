package builder

import (
	"github.com/kart-io/goagent/core/middleware"
)

// Runtime 组件配置方法
// 本文件包含 AgentBuilder 的运行时组件配置方法

// WithMiddleware adds middleware to the chain.
func (b *AgentBuilder[S]) WithMiddleware(mw ...middleware.Middleware) *AgentBuilder[S] {
	b.middlewares = append(b.middlewares, mw...)
	return b
}
