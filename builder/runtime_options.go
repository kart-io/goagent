package builder

import (
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
)

// Runtime 组件配置方法
// 本文件包含 AgentBuilder 的运行时组件配置方法

// WithMemory 设置对话记忆管理器
//
// MemoryManager 用于管理多轮对话的历史记录，使 Agent 能够"记住"之前的对话内容。
// 每次执行时，Agent 会从 MemoryManager 加载历史对话，并在执行后保存新的对话。
//
// 使用示例：
//
//	memMgr := memory.NewInMemoryManager(memory.DefaultConfig())
//	agent, err := builder.NewSimpleBuilder(llmClient).
//	    WithSystemPrompt("你是一个助手").
//	    WithMemory(memMgr).
//	    WithSessionID("user-session-123").
//	    Build()
func (b *AgentBuilder[S]) WithMemory(memMgr interfaces.MemoryManager) *AgentBuilder[S] {
	b.memoryManager = memMgr
	return b
}

// WithMiddleware 添加中间件到链中
func (b *AgentBuilder[S]) WithMiddleware(mw ...middleware.Middleware) *AgentBuilder[S] {
	b.middlewares = append(b.middlewares, mw...)
	return b
}
