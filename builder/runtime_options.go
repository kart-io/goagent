package builder

import (
	"github.com/kart-io/goagent/core/checkpoint"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/store"
)

// Runtime 组件配置方法
// 本文件包含 AgentBuilder 的运行时组件配置方法

// WithStore 设置长期存储
func (b *AgentBuilder[C, S]) WithStore(st store.Store) *AgentBuilder[C, S] {
	b.store = st
	return b
}

// WithMemory 设置对话记忆管理器
//
// MemoryManager 用于管理多轮对话的历史记录，使 Agent 能够"记住"之前的对话内容。
// 每次执行时，Agent 会从 MemoryManager 加载历史对话，并在执行后保存新的对话。
//
// 注意：WithStore 是键值存储，用于保存任意数据；
// WithMemory 是专门的对话记忆管理，用于实现多轮对话能力。
//
// 使用示例：
//
//	memMgr := memory.NewInMemoryManager(memory.DefaultConfig())
//	agent, err := builder.NewSimpleBuilder(llmClient).
//	    WithSystemPrompt("你是一个助手").
//	    WithMemory(memMgr).
//	    WithSessionID("user-session-123").
//	    Build()
func (b *AgentBuilder[C, S]) WithMemory(memMgr interfaces.MemoryManager) *AgentBuilder[C, S] {
	b.memoryManager = memMgr
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
