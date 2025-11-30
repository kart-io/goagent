package builder

import "github.com/kart-io/goagent/core"

// Agent 核心配置方法
// 本文件包含 AgentBuilder 的核心 Agent 配置方法

// WithSystemPrompt 设置系统提示词
func (b *AgentBuilder[C, S]) WithSystemPrompt(prompt string) *AgentBuilder[C, S] {
	b.systemPrompt = prompt
	return b
}

// WithState 设置 Agent 状态
func (b *AgentBuilder[C, S]) WithState(state S) *AgentBuilder[C, S] {
	b.state = state
	return b
}

// WithContext 设置应用上下文
func (b *AgentBuilder[C, S]) WithContext(context C) *AgentBuilder[C, S] {
	b.context = context
	return b
}

// WithCallbacks 添加回调函数用于监控
func (b *AgentBuilder[C, S]) WithCallbacks(callbacks ...core.Callback) *AgentBuilder[C, S] {
	b.callbacks = append(b.callbacks, callbacks...)
	return b
}

// WithErrorHandler 设置自定义错误处理函数
func (b *AgentBuilder[C, S]) WithErrorHandler(handler func(error) error) *AgentBuilder[C, S] {
	b.errorHandler = handler
	return b
}

// WithMetadata 添加元数据到 Agent
func (b *AgentBuilder[C, S]) WithMetadata(key string, value interface{}) *AgentBuilder[C, S] {
	b.metadata[key] = value
	return b
}

// WithTelemetry 添加 OpenTelemetry 支持
func (b *AgentBuilder[C, S]) WithTelemetry(provider interface{}) *AgentBuilder[C, S] {
	b.metadata["telemetry_provider"] = provider
	return b
}

// WithCommunicator 添加通信器
func (b *AgentBuilder[C, S]) WithCommunicator(communicator interface{}) *AgentBuilder[C, S] {
	b.metadata["communicator"] = communicator
	return b
}
