package core

import "time"

// TimeoutConfig 统一的超时配置
// 管理所有类型的超时设置，确保项目中超时处理的一致性
type TimeoutConfig struct {
	// LLM 相关超时
	// LLMTimeout LLM 调用超时时间（包括推理、生成等）
	LLMTimeout time.Duration

	// 工具执行超时
	// ToolTimeout 工具执行超时时间
	ToolTimeout time.Duration

	// 数据库相关超时
	// DBConnectionTimeout 数据库连接建立超时时间
	DBConnectionTimeout time.Duration
	// DBOperationTimeout 数据库操作（读写查询）超时时间
	DBOperationTimeout time.Duration

	// HTTP 相关超时
	// HTTPTimeout HTTP 请求超时时间
	HTTPTimeout time.Duration

	// 资源池相关超时
	// PoolAcquireTimeout 从池中获取资源的超时时间
	PoolAcquireTimeout time.Duration

	// 批量执行超时
	// BatchTimeout 批量执行操作的超时时间
	BatchTimeout time.Duration

	// Agent 执行超时
	// AgentExecutionTimeout Agent 执行超时时间（包括完整推理循环）
	AgentExecutionTimeout time.Duration
}

// DefaultTimeoutConfig 返回默认超时配置
// 基于项目最佳实践和性能测试结果设定合理的默认值
func DefaultTimeoutConfig() *TimeoutConfig {
	return &TimeoutConfig{
		// LLM 调用：60秒
		// 理由：大多数 LLM 调用在 10-30 秒内完成，60 秒足够处理复杂推理
		LLMTimeout: 60 * time.Second,

		// 工具执行：30秒
		// 理由：工具执行通常较快，30 秒可以处理大多数场景（文件操作、API 调用等）
		ToolTimeout: 30 * time.Second,

		// 数据库连接：5秒
		// 理由：快速失败原则，连接建立应该很快，超时说明网络或服务有问题
		DBConnectionTimeout: 5 * time.Second,

		// 数据库操作：10秒
		// 理由：允许执行较复杂的查询，但避免长时间阻塞
		DBOperationTimeout: 10 * time.Second,

		// HTTP 请求：30秒
		// 理由：符合 HTTP 标准超时，平衡网络延迟和响应时间
		HTTPTimeout: 30 * time.Second,

		// 池资源获取：10秒
		// 理由：等待资源可用的合理时间，避免无限等待
		PoolAcquireTimeout: 10 * time.Second,

		// 批量执行：5分钟
		// 理由：批量操作涉及多个子任务，需要较长时间
		BatchTimeout: 5 * time.Minute,

		// Agent 执行：5分钟
		// 理由：Agent 可能执行多轮推理和工具调用，需要足够时间
		AgentExecutionTimeout: 5 * time.Minute,
	}
}

// 超时常量定义
// 为常见场景提供预定义的超时值
const (
	// 默认 LLM 调用超时
	DefaultLLMTimeout = 60 * time.Second

	// 默认工具执行超时
	DefaultToolTimeout = 30 * time.Second

	// 默认数据库连接超时
	DefaultDBConnectionTimeout = 5 * time.Second

	// 默认数据库操作超时
	DefaultDBOperationTimeout = 10 * time.Second

	// 默认 HTTP 请求超时
	DefaultHTTPTimeout = 30 * time.Second

	// 默认池获取超时
	DefaultPoolAcquireTimeout = 10 * time.Second

	// 默认批量执行超时
	DefaultBatchTimeout = 5 * time.Minute

	// 默认 Agent 执行超时
	DefaultAgentExecutionTimeout = 5 * time.Minute
)

// WithLLMTimeout 设置 LLM 调用超时
func (c *TimeoutConfig) WithLLMTimeout(timeout time.Duration) *TimeoutConfig {
	if timeout > 0 {
		c.LLMTimeout = timeout
	}
	return c
}

// WithToolTimeout 设置工具执行超时
func (c *TimeoutConfig) WithToolTimeout(timeout time.Duration) *TimeoutConfig {
	if timeout > 0 {
		c.ToolTimeout = timeout
	}
	return c
}

// WithDBConnectionTimeout 设置数据库连接超时
func (c *TimeoutConfig) WithDBConnectionTimeout(timeout time.Duration) *TimeoutConfig {
	if timeout > 0 {
		c.DBConnectionTimeout = timeout
	}
	return c
}

// WithDBOperationTimeout 设置数据库操作超时
func (c *TimeoutConfig) WithDBOperationTimeout(timeout time.Duration) *TimeoutConfig {
	if timeout > 0 {
		c.DBOperationTimeout = timeout
	}
	return c
}

// WithHTTPTimeout 设置 HTTP 请求超时
func (c *TimeoutConfig) WithHTTPTimeout(timeout time.Duration) *TimeoutConfig {
	if timeout > 0 {
		c.HTTPTimeout = timeout
	}
	return c
}

// WithPoolAcquireTimeout 设置池获取超时
func (c *TimeoutConfig) WithPoolAcquireTimeout(timeout time.Duration) *TimeoutConfig {
	if timeout > 0 {
		c.PoolAcquireTimeout = timeout
	}
	return c
}

// WithBatchTimeout 设置批量执行超时
func (c *TimeoutConfig) WithBatchTimeout(timeout time.Duration) *TimeoutConfig {
	if timeout > 0 {
		c.BatchTimeout = timeout
	}
	return c
}

// WithAgentExecutionTimeout 设置 Agent 执行超时
func (c *TimeoutConfig) WithAgentExecutionTimeout(timeout time.Duration) *TimeoutConfig {
	if timeout > 0 {
		c.AgentExecutionTimeout = timeout
	}
	return c
}
