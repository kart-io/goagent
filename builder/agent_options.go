package builder

import "github.com/kart-io/goagent/core"

// Agent 核心配置方法
// 本文件包含 AgentBuilder 的核心 Agent 配置方法
//
// API 分层说明：
// - [Simple]: 日常 API，覆盖 80% 使用场景（5-8 个方法）
// - [Core]: 标准 API，覆盖 95% 使用场景（15-20 个方法）
// - [Advanced]: 完整 API，覆盖所有场景（30+ 个方法）

// WithSystemPrompt sets the system prompt.
//
// [Simple] The most commonly used option; it defines the agent's role and
// behavior.
//
// Example:
//
//	agent, _ := builder.NewSimpleBuilder(llm).
//	    WithSystemPrompt("You are a friendly assistant").
//	    Build()
func (b *AgentBuilder[S]) WithSystemPrompt(prompt string) *AgentBuilder[S] {
	b.systemPrompt = prompt
	return b
}

// WithState sets the agent state.
//
// [Advanced] Used for custom state types (requires familiarity with generics).
// In most cases the default *core.AgentState is sufficient.
func (b *AgentBuilder[S]) WithState(state S) *AgentBuilder[S] {
	b.state = state
	return b
}

// WithCallbacks adds callbacks for monitoring.
//
// [Core] Standard option for observing agent execution (logging, metrics,
// debugging).
//
// Example:
//
//	agent, _ := builder.NewSimpleBuilder(llm).
//	    WithSystemPrompt("...").
//	    WithCallbacks(core.NewStdoutCallback(true)).
//	    Build()
func (b *AgentBuilder[S]) WithCallbacks(callbacks ...core.Callback) *AgentBuilder[S] {
	b.callbacks = append(b.callbacks, callbacks...)
	return b
}

// WithErrorHandler sets a custom error handler.
//
// [Core] Standard option for custom error handling logic (for example, retry or
// fallback).
func (b *AgentBuilder[S]) WithErrorHandler(handler func(error) error) *AgentBuilder[S] {
	b.errorHandler = handler
	return b
}

// WithMetadata adds metadata to the agent.
//
// [Advanced] Used to store custom key-value data.
func (b *AgentBuilder[S]) WithMetadata(key string, value interface{}) *AgentBuilder[S] {
	b.metadata[key] = value
	return b
}
