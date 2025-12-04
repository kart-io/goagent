package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestDefaultTimeoutConfig 测试默认超时配置
func TestDefaultTimeoutConfig(t *testing.T) {
	config := DefaultTimeoutConfig()

	// 验证所有超时值都已设置且为正值
	assert.Greater(t, config.LLMTimeout, time.Duration(0), "LLM 超时应该大于 0")
	assert.Greater(t, config.ToolTimeout, time.Duration(0), "工具超时应该大于 0")
	assert.Greater(t, config.DBConnectionTimeout, time.Duration(0), "数据库连接超时应该大于 0")
	assert.Greater(t, config.DBOperationTimeout, time.Duration(0), "数据库操作超时应该大于 0")
	assert.Greater(t, config.HTTPTimeout, time.Duration(0), "HTTP 超时应该大于 0")
	assert.Greater(t, config.PoolAcquireTimeout, time.Duration(0), "池获取超时应该大于 0")
	assert.Greater(t, config.BatchTimeout, time.Duration(0), "批量执行超时应该大于 0")
	assert.Greater(t, config.AgentExecutionTimeout, time.Duration(0), "Agent 执行超时应该大于 0")

	// 验证具体的默认值
	assert.Equal(t, 60*time.Second, config.LLMTimeout, "LLM 超时默认值应该是 60 秒")
	assert.Equal(t, 30*time.Second, config.ToolTimeout, "工具超时默认值应该是 30 秒")
	assert.Equal(t, 5*time.Second, config.DBConnectionTimeout, "数据库连接超时默认值应该是 5 秒")
	assert.Equal(t, 10*time.Second, config.DBOperationTimeout, "数据库操作超时默认值应该是 10 秒")
	assert.Equal(t, 30*time.Second, config.HTTPTimeout, "HTTP 超时默认值应该是 30 秒")
	assert.Equal(t, 10*time.Second, config.PoolAcquireTimeout, "池获取超时默认值应该是 10 秒")
	assert.Equal(t, 5*time.Minute, config.BatchTimeout, "批量执行超时默认值应该是 5 分钟")
	assert.Equal(t, 5*time.Minute, config.AgentExecutionTimeout, "Agent 执行超时默认值应该是 5 分钟")
}

// TestTimeoutConstants 测试超时常量
func TestTimeoutConstants(t *testing.T) {
	// 验证常量与默认配置一致
	config := DefaultTimeoutConfig()

	assert.Equal(t, DefaultLLMTimeout, config.LLMTimeout, "LLM 超时常量应该与默认配置一致")
	assert.Equal(t, DefaultToolTimeout, config.ToolTimeout, "工具超时常量应该与默认配置一致")
	assert.Equal(t, DefaultDBConnectionTimeout, config.DBConnectionTimeout, "数据库连接超时常量应该与默认配置一致")
	assert.Equal(t, DefaultDBOperationTimeout, config.DBOperationTimeout, "数据库操作超时常量应该与默认配置一致")
	assert.Equal(t, DefaultHTTPTimeout, config.HTTPTimeout, "HTTP 超时常量应该与默认配置一致")
	assert.Equal(t, DefaultPoolAcquireTimeout, config.PoolAcquireTimeout, "池获取超时常量应该与默认配置一致")
	assert.Equal(t, DefaultBatchTimeout, config.BatchTimeout, "批量执行超时常量应该与默认配置一致")
	assert.Equal(t, DefaultAgentExecutionTimeout, config.AgentExecutionTimeout, "Agent 执行超时常量应该与默认配置一致")
}

// TestWithLLMTimeout 测试设置 LLM 超时
func TestWithLLMTimeout(t *testing.T) {
	config := DefaultTimeoutConfig()

	// 测试设置有效超时
	newTimeout := 120 * time.Second
	result := config.WithLLMTimeout(newTimeout)
	assert.Equal(t, newTimeout, config.LLMTimeout, "应该更新 LLM 超时")
	assert.Equal(t, config, result, "应该返回配置本身以支持链式调用")

	// 测试设置无效超时（0 或负数）
	originalTimeout := config.LLMTimeout
	config.WithLLMTimeout(0)
	assert.Equal(t, originalTimeout, config.LLMTimeout, "不应该接受 0 超时")

	config.WithLLMTimeout(-1 * time.Second)
	assert.Equal(t, originalTimeout, config.LLMTimeout, "不应该接受负数超时")
}

// TestWithToolTimeout 测试设置工具超时
func TestWithToolTimeout(t *testing.T) {
	config := DefaultTimeoutConfig()

	// 测试设置有效超时
	newTimeout := 45 * time.Second
	result := config.WithToolTimeout(newTimeout)
	assert.Equal(t, newTimeout, config.ToolTimeout, "应该更新工具超时")
	assert.Equal(t, config, result, "应该返回配置本身以支持链式调用")

	// 测试设置无效超时
	originalTimeout := config.ToolTimeout
	config.WithToolTimeout(0)
	assert.Equal(t, originalTimeout, config.ToolTimeout, "不应该接受 0 超时")
}

// TestWithDBConnectionTimeout 测试设置数据库连接超时
func TestWithDBConnectionTimeout(t *testing.T) {
	config := DefaultTimeoutConfig()

	newTimeout := 10 * time.Second
	result := config.WithDBConnectionTimeout(newTimeout)
	assert.Equal(t, newTimeout, config.DBConnectionTimeout, "应该更新数据库连接超时")
	assert.Equal(t, config, result, "应该返回配置本身以支持链式调用")
}

// TestWithDBOperationTimeout 测试设置数据库操作超时
func TestWithDBOperationTimeout(t *testing.T) {
	config := DefaultTimeoutConfig()

	newTimeout := 20 * time.Second
	result := config.WithDBOperationTimeout(newTimeout)
	assert.Equal(t, newTimeout, config.DBOperationTimeout, "应该更新数据库操作超时")
	assert.Equal(t, config, result, "应该返回配置本身以支持链式调用")
}

// TestWithHTTPTimeout 测试设置 HTTP 超时
func TestWithHTTPTimeout(t *testing.T) {
	config := DefaultTimeoutConfig()

	newTimeout := 60 * time.Second
	result := config.WithHTTPTimeout(newTimeout)
	assert.Equal(t, newTimeout, config.HTTPTimeout, "应该更新 HTTP 超时")
	assert.Equal(t, config, result, "应该返回配置本身以支持链式调用")
}

// TestWithPoolAcquireTimeout 测试设置池获取超时
func TestWithPoolAcquireTimeout(t *testing.T) {
	config := DefaultTimeoutConfig()

	newTimeout := 15 * time.Second
	result := config.WithPoolAcquireTimeout(newTimeout)
	assert.Equal(t, newTimeout, config.PoolAcquireTimeout, "应该更新池获取超时")
	assert.Equal(t, config, result, "应该返回配置本身以支持链式调用")
}

// TestWithBatchTimeout 测试设置批量执行超时
func TestWithBatchTimeout(t *testing.T) {
	config := DefaultTimeoutConfig()

	newTimeout := 10 * time.Minute
	result := config.WithBatchTimeout(newTimeout)
	assert.Equal(t, newTimeout, config.BatchTimeout, "应该更新批量执行超时")
	assert.Equal(t, config, result, "应该返回配置本身以支持链式调用")
}

// TestWithAgentExecutionTimeout 测试设置 Agent 执行超时
func TestWithAgentExecutionTimeout(t *testing.T) {
	config := DefaultTimeoutConfig()

	newTimeout := 10 * time.Minute
	result := config.WithAgentExecutionTimeout(newTimeout)
	assert.Equal(t, newTimeout, config.AgentExecutionTimeout, "应该更新 Agent 执行超时")
	assert.Equal(t, config, result, "应该返回配置本身以支持链式调用")
}

// TestChainedConfiguration 测试链式配置
func TestChainedConfiguration(t *testing.T) {
	config := DefaultTimeoutConfig().
		WithLLMTimeout(90 * time.Second).
		WithToolTimeout(45 * time.Second).
		WithHTTPTimeout(60 * time.Second)

	assert.Equal(t, 90*time.Second, config.LLMTimeout, "链式调用应该正确设置 LLM 超时")
	assert.Equal(t, 45*time.Second, config.ToolTimeout, "链式调用应该正确设置工具超时")
	assert.Equal(t, 60*time.Second, config.HTTPTimeout, "链式调用应该正确设置 HTTP 超时")
	// 未设置的应该保持默认值
	assert.Equal(t, 5*time.Second, config.DBConnectionTimeout, "未设置的超时应该保持默认值")
}

// TestTimeoutReasonableness 测试超时值的合理性
func TestTimeoutReasonableness(t *testing.T) {
	config := DefaultTimeoutConfig()

	// 数据库连接超时应该比操作超时短（快速失败）
	assert.Less(t, config.DBConnectionTimeout, config.DBOperationTimeout,
		"数据库连接超时应该比操作超时短")

	// 工具超时应该比 Agent 执行超时短（工具是子任务）
	assert.Less(t, config.ToolTimeout, config.AgentExecutionTimeout,
		"工具超时应该比 Agent 执行超时短")

	// 批量执行和 Agent 执行超时应该是最长的
	assert.GreaterOrEqual(t, config.BatchTimeout, config.LLMTimeout,
		"批量执行超时应该不小于单次 LLM 调用超时")
	assert.GreaterOrEqual(t, config.AgentExecutionTimeout, config.LLMTimeout,
		"Agent 执行超时应该不小于单次 LLM 调用超时")
}
