package builder

import (
	"fmt"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/middleware"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

// 快速构建函数
// 本文件包含快速创建预配置 Agent 的便捷函数

// QuickAgent 创建一个简单的 Agent,使用最小配置
func QuickAgent(llmClient llm.Client, systemPrompt string) (*SimpleAgent, error) {
	return NewSimpleBuilder(llmClient).
		WithSystemPrompt(systemPrompt).
		WithState(core.NewAgentState()).
		Build()
}

// RAGAgent 创建一个预配置的 RAG Agent
func RAGAgent(llmClient llm.Client, retriever interface{}) (*SimpleAgent, error) {
	return NewSimpleBuilder(llmClient).
		WithSystemPrompt("You are a helpful assistant. Answer questions based on the provided context.").
		ConfigureForRAG().
		WithState(core.NewAgentState()).
		WithMetadata("type", "rag").
		Build()
}

// ChatAgent 创建一个预配置的聊天机器人 Agent
func ChatAgent(llmClient llm.Client, userName string) (*SimpleAgent, error) {
	state := core.NewAgentState()
	state.Set("user_name", userName)

	return NewSimpleBuilder(llmClient).
		WithSystemPrompt(fmt.Sprintf("You are a helpful assistant chatting with %s.", userName)).
		WithState(state).
		ConfigureForChatbot().
		WithMetadata("type", "chatbot").
		Build()
}

// AnalysisAgent 创建一个预配置的数据分析 Agent
//
// 此 Agent 优化用于:
//   - 数据分析和报告生成
//   - 一致的、事实性的输出
//   - 结构化数据转换
//   - 扩展的推理迭代
//
// 配置:
//   - Temperature: 0.1 (非常低用于一致性)
//   - MaxIterations: 20 (更多迭代用于复杂分析)
//   - Middleware: Timing, Transform (用于结构化输出)
func AnalysisAgent(llmClient llm.Client, dataSource interface{}) (*SimpleAgent, error) {
	state := core.NewAgentState()
	if dataSource != nil {
		state.Set("data_source", dataSource)
	}

	return NewSimpleBuilder(llmClient).
		WithSystemPrompt("You are a data analysis expert. Analyze data thoroughly and provide structured, accurate insights.").
		WithState(state).
		ConfigureForAnalysis().
		WithMetadata("type", "analysis").
		WithMetadata("data_source_type", fmt.Sprintf("%T", dataSource)).
		Build()
}

// WorkflowAgent 创建一个预配置的工作流编排 Agent
//
// 此 Agent 优化用于:
//   - 多步骤工作流执行
//   - 任务编排和协调
//   - 错误处理和验证
//   - 跨步骤的状态持久化
//
// 配置:
//   - MaxIterations: 15 (平衡的工作流步骤)
//   - EnableAutoSave: true (跨步骤持久化状态)
//   - Middleware: Logging, CircuitBreaker, Validation
func WorkflowAgent(llmClient llm.Client, workflows map[string]interface{}) (*SimpleAgent, error) {
	state := core.NewAgentState()
	if workflows != nil {
		state.Set("workflows", workflows)
		state.Set("workflow_status", "initialized")
	}

	return NewSimpleBuilder(llmClient).
		WithSystemPrompt("You are a workflow orchestrator. Execute tasks systematically, validate results, and handle errors gracefully.").
		WithState(state).
		WithMaxIterations(15).
		WithAutoSaveEnabled(true).
		WithMiddleware(
			middleware.NewLoggingMiddleware(nil),
			middleware.NewCircuitBreakerMiddleware(5, 30*time.Second),
			middleware.NewValidationMiddleware(func(req *middleware.MiddlewareRequest) error {
				// 基本验证
				if req.Input == nil {
					return agentErrors.NewInvalidInputError("workflow", "input", "workflow input cannot be nil")
				}
				return nil
			}),
		).
		WithMetadata("type", "workflow").
		Build()
}

// MonitoringAgent 创建一个预配置的监控 Agent
//
// 此 Agent 优化用于:
//   - 持续系统监控
//   - 异常检测
//   - 警报生成
//   - 定期健康检查
//
// 配置:
//   - 持续操作模式
//   - 速率限制以防止过载
//   - 缓存用于高效监控
//   - 警报中间件用于通知
func MonitoringAgent(llmClient llm.Client, checkInterval time.Duration) (*SimpleAgent, error) {
	state := core.NewAgentState()
	state.Set("check_interval", checkInterval)
	state.Set("last_check", time.Now())
	state.Set("monitoring_status", "active")

	return NewSimpleBuilder(llmClient).
		WithSystemPrompt("You are a system monitoring expert. Observe metrics, detect anomalies, and alert on issues promptly.").
		WithState(state).
		WithMaxIterations(100). // 长时间运行监控
		WithTemperature(0.3).   // 平衡的模式识别
		WithMiddleware(
			middleware.NewRateLimiterMiddleware(60, time.Minute), // 限制为每分钟 60 次检查
			middleware.NewCacheMiddleware(5*time.Minute),         // 缓存最近的检查
			middleware.NewTimingMiddleware(),
		).
		WithMetadata("type", "monitoring").
		WithMetadata("check_interval", checkInterval.String()).
		Build()
}

// ResearchAgent 创建一个预配置的研究和信息收集 Agent
//
// 此 Agent 优化用于:
//   - 从多个来源收集信息
//   - 研究报告生成
//   - 来源综合和引用
//   - 事实核查和验证
//
// 配置:
//   - MaxTokens: 4000 (更大上下文用于综合报告)
//   - Temperature: 0.5 (平衡创造性和准确性)
//   - Middleware: ToolSelector (用于搜索/抓取), Cache
func ResearchAgent(llmClient llm.Client, sources []string) (*SimpleAgent, error) {
	state := core.NewAgentState()
	if len(sources) > 0 {
		state.Set("sources", sources)
		state.Set("sources_count", len(sources))
	}
	state.Set("research_status", "initialized")

	return NewSimpleBuilder(llmClient).
		WithSystemPrompt("You are a research expert. Gather information from multiple sources, synthesize findings, and provide comprehensive, well-cited reports.").
		WithState(state).
		WithMaxTokens(4000).
		WithTemperature(0.5).
		WithMaxIterations(15).
		WithMiddleware(
			middleware.NewCacheMiddleware(10*time.Minute), // 缓存研究结果
			middleware.NewTimingMiddleware(),
		).
		WithMetadata("type", "research").
		WithMetadata("sources_count", len(sources)).
		Build()
}
