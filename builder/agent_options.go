package builder

import "github.com/kart-io/goagent/core"

// Agent 核心配置方法
// 本文件包含 AgentBuilder 的核心 Agent 配置方法
//
// API 分层说明：
// - [Simple]: 日常 API，覆盖 80% 使用场景（5-8 个方法）
// - [Core]: 标准 API，覆盖 95% 使用场景（15-20 个方法）
// - [Advanced]: 完整 API，覆盖所有场景（30+ 个方法）

// WithSystemPrompt 设置系统提示词
//
// [Simple] 最常用的配置方法，定义 Agent 的角色和行为。
//
// 示例：
//
//	agent, _ := builder.NewSimpleBuilder(llm).
//	    WithSystemPrompt("你是一个友好的助手").
//	    Build()
func (b *AgentBuilder[C, S]) WithSystemPrompt(prompt string) *AgentBuilder[C, S] {
	b.systemPrompt = prompt
	return b
}

// WithState 设置 Agent 状态
//
// [Advanced] 高级配置，用于自定义状态类型（需要泛型知识）。
// 大多数情况下使用默认的 *core.AgentState 即可。
func (b *AgentBuilder[C, S]) WithState(state S) *AgentBuilder[C, S] {
	b.state = state
	return b
}

// WithContext 设置应用上下文
//
// [Advanced] 高级配置，用于自定义上下文类型（需要泛型知识）。
// 大多数情况下使用默认的 any 即可。
func (b *AgentBuilder[C, S]) WithContext(context C) *AgentBuilder[C, S] {
	b.context = context
	return b
}

// WithCallbacks 添加回调函数用于监控
//
// [Core] 标准配置，用于监控 Agent 执行过程（日志、指标、调试）。
//
// 示例：
//
//	agent, _ := builder.NewSimpleBuilder(llm).
//	    WithSystemPrompt("...").
//	    WithCallbacks(core.NewStdoutCallback(true)).
//	    Build()
func (b *AgentBuilder[C, S]) WithCallbacks(callbacks ...core.Callback) *AgentBuilder[C, S] {
	b.callbacks = append(b.callbacks, callbacks...)
	return b
}

// WithErrorHandler 设置自定义错误处理函数
//
// [Core] 标准配置，用于自定义错误处理逻辑（例如重试、降级）。
func (b *AgentBuilder[C, S]) WithErrorHandler(handler func(error) error) *AgentBuilder[C, S] {
	b.errorHandler = handler
	return b
}

// WithMetadata 添加元数据到 Agent
//
// [Advanced] 高级配置，用于存储自定义键值对数据。
func (b *AgentBuilder[C, S]) WithMetadata(key string, value interface{}) *AgentBuilder[C, S] {
	b.metadata[key] = value
	return b
}

// WithTelemetry 添加 OpenTelemetry 支持
//
// [Advanced] 高级配置，用于集成 OpenTelemetry 分布式追踪。
func (b *AgentBuilder[C, S]) WithTelemetry(provider interface{}) *AgentBuilder[C, S] {
	b.metadata["telemetry_provider"] = provider
	return b
}

// WithCommunicator 添加通信器
//
// [Advanced] 高级配置，用于 Agent 间通信（多 Agent 系统）。
func (b *AgentBuilder[C, S]) WithCommunicator(communicator interface{}) *AgentBuilder[C, S] {
	b.metadata["communicator"] = communicator
	return b
}
