package builder

import (
	"fmt"
	"time"

	"github.com/kart-io/goagent/core/middleware"
	agentErrors "github.com/kart-io/goagent/errors"
)

// 预设配置方法
// 本文件包含 AgentBuilder 的常用场景预设配置方法

// ConfigureForRAG 添加常用的 RAG (检索增强生成) 组件
func (b *AgentBuilder[C, S]) ConfigureForRAG() *AgentBuilder[C, S] {
	// 添加常用的 RAG 中间件
	b.WithMiddleware(
		middleware.NewCacheMiddleware(5*time.Minute),
		middleware.NewDynamicPromptMiddleware(func(req *middleware.MiddlewareRequest) string {
			// 从检索添加上下文
			return fmt.Sprintf("Use the following context to answer: %v", req.Input)
		}),
	)

	// 设置合适的配置
	b.WithMaxTokens(3000)
	b.WithTemperature(0.3) // 较低温度用于事实性响应

	return b
}

// ConfigureForChatbot 添加常用的聊天机器人组件
func (b *AgentBuilder[C, S]) ConfigureForChatbot() *AgentBuilder[C, S] {
	// 添加聊天机器人中间件
	b.WithMiddleware(
		middleware.NewRateLimiterMiddleware(20, time.Minute),
		middleware.NewValidationMiddleware(
			func(req *middleware.MiddlewareRequest) error {
				// 验证输入长度
				if len(fmt.Sprintf("%v", req.Input)) > 1000 {
					return agentErrors.NewInvalidInputError("chatbot", "message", "message too long")
				}
				return nil
			},
		),
	)

	// 启用流式响应以获得更好的用户体验
	b.WithStreamingEnabled(true)
	b.WithTemperature(0.8) // 更高温度用于创造性

	return b
}

// ConfigureForAnalysis 添加用于数据分析任务的组件
func (b *AgentBuilder[C, S]) ConfigureForAnalysis() *AgentBuilder[C, S] {
	// 添加分析中间件
	b.WithMiddleware(
		middleware.NewTimingMiddleware(),
		middleware.NewTransformMiddleware(
			nil, // 不转换输入
			func(output interface{}) (interface{}, error) {
				// 将输出格式化为结构化数据
				return map[string]interface{}{
					"analysis":  output,
					"timestamp": time.Now(),
				}, nil
			},
		),
	)

	// 配置准确性
	b.WithTemperature(0.1)  // 非常低的温度用于一致性
	b.WithMaxIterations(20) // 更多迭代用于复杂分析

	return b
}
