package builder

import (
	"github.com/kart-io/goagent/core/checkpoint"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/store"
)

// Runtime 组件配置方法
// 本文件包含 AgentBuilder 的运行时组件配置方法

// WithStore 设置长期存储
func (b *AgentBuilder[C, S]) WithStore(st store.Store) *AgentBuilder[C, S] {
	b.store = st
	return b
}

// WithCheckpointer 设置会话检查点器
func (b *AgentBuilder[C, S]) WithCheckpointer(checkpointer checkpoint.Checkpointer) *AgentBuilder[C, S] {
	b.checkpointer = checkpointer
	return b
}

// WithMiddleware 添加中间件到链中
func (b *AgentBuilder[C, S]) WithMiddleware(mw ...middleware.Middleware) *AgentBuilder[C, S] {
	b.middlewares = append(b.middlewares, mw...)
	return b
}
