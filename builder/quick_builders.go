package builder

import (
	"context"
	"fmt"
	"time"

	shared "github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/middleware"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// 快速构建函数
// 本文件包含快速创建预配置 Agent 的便捷函数

// quickBuilder 给预设 Agent 显式带上限额默认值改为调用方所有之前的取值，预设行为不变。
func quickBuilder(llmClient llm.Client) *SimpleAgentBuilder {
	return NewSimpleBuilder(llmClient).
		WithMaxIterations(shared.DefaultAgentMaxIterations).
		WithTimeout(shared.DefaultAgentExecutionTimeout)
}

// presetRetrieval 是 RAGAgent 沿用的检索篇数与字数上限。
var presetRetrieval = RetrievalConfig{MaxDocuments: 4, MaxContextRunes: 12000}

// QuickAgent creates a simple agent with minimal configuration.
//
// Like every quick constructor in this file it keeps the limits presets had
// before limits became caller-owned: shared.DefaultAgentMaxIterations rounds
// (unless the preset documents another value) and a
// shared.DefaultAgentExecutionTimeout turn timeout. Use NewSimpleBuilder to
// choose limits yourself.
func QuickAgent(llmClient llm.Client, systemPrompt string) (*SimpleAgent, error) {
	return quickBuilder(llmClient).
		WithSystemPrompt(systemPrompt).
		WithState(core.NewAgentState()).
		Build()
}

// RAGAgent creates a retrieval-grounded agent. It accepts a DocumentRetriever,
// a retriever exposing Retrieve, or a VectorStore (top 4 results).
// A nil retriever preserves the legacy prompt-only preset.
func RAGAgent(llmClient llm.Client, retriever interface{}) (*SimpleAgent, error) {
	b := quickBuilder(llmClient).
		WithSystemPrompt("You are a helpful assistant. Answer questions based on the provided context.").
		ConfigureForRAG().
		WithState(core.NewAgentState()).
		WithMetadata("type", "rag")
	if retriever == nil {
		return b.Build()
	}
	if isNilRetriever(retriever) {
		return nil, agentErrors.NewInvalidConfigError("builder", "retriever", "retriever must not be a typed nil")
	}
	switch r := retriever.(type) {
	case interfaces.DocumentRetriever:
		b.WithRetriever(r, presetRetrieval)
	case interface {
		Retrieve(context.Context, string) ([]*interfaces.Document, error)
	}:
		b.WithRetriever(documentRetrieverFunc(r.Retrieve), presetRetrieval)
	case interfaces.VectorStore:
		b.WithRetriever(documentRetrieverFunc(func(ctx context.Context, query string) ([]*interfaces.Document, error) {
			return r.SimilaritySearch(ctx, query, 4)
		}), presetRetrieval)
	default:
		return nil, agentErrors.NewInvalidConfigError("builder", "retriever", "expected DocumentRetriever, Retrieve method, or VectorStore")
	}
	return b.Build()
}

// ChatAgent creates a preconfigured chatbot agent.
func ChatAgent(llmClient llm.Client, userName string) (*SimpleAgent, error) {
	state := core.NewAgentState()
	state.Set("user_name", userName)

	return quickBuilder(llmClient).
		WithSystemPrompt(fmt.Sprintf("You are a helpful assistant chatting with %s.", userName)).
		WithState(state).
		ConfigureForChatbot().
		WithMetadata("type", "chatbot").
		Build()
}

// AnalysisAgent creates a preconfigured data analysis agent.
//
// This agent is optimized for:
//   - Data analysis and report generation
//   - Consistent, factual output
//   - Structured data transformation
//   - Extended reasoning iterations
//
// Configuration:
//   - Temperature: 0.1 (very low for consistency)
//   - MaxIterations: 20 (more iterations for complex analysis)
//   - Middleware: Timing, Transform (for structured output)
func AnalysisAgent(llmClient llm.Client, dataSource interface{}) (*SimpleAgent, error) {
	state := core.NewAgentState()
	if dataSource != nil {
		state.Set("data_source", dataSource)
	}

	return quickBuilder(llmClient).
		WithSystemPrompt("You are a data analysis expert. Analyze data thoroughly and provide structured, accurate insights.").
		WithState(state).
		ConfigureForAnalysis().
		WithMetadata("type", "analysis").
		WithMetadata("data_source_type", fmt.Sprintf("%T", dataSource)).
		Build()
}

// WorkflowAgent creates a preconfigured workflow orchestration agent.
//
// This agent is optimized for:
//   - Multi-step workflow execution
//   - Task orchestration and coordination
//   - Error handling and validation
//
// Configuration:
//   - MaxIterations: 15 (balanced workflow steps)
//   - Middleware: Logging, CircuitBreaker, Validation
func WorkflowAgent(llmClient llm.Client, workflows map[string]interface{}) (*SimpleAgent, error) {
	state := core.NewAgentState()
	if workflows != nil {
		state.Set("workflows", workflows)
		state.Set("workflow_status", "initialized")
	}

	return quickBuilder(llmClient).
		WithSystemPrompt("You are a workflow orchestrator. Execute tasks systematically, validate results, and handle errors gracefully.").
		WithState(state).
		WithMaxIterations(15).
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

// MonitoringAgent creates a preconfigured monitoring agent.
//
// This agent is optimized for:
//   - Continuous system monitoring
//   - Anomaly detection
//   - Alert generation
//   - Periodic health checks
//
// Configuration:
//   - MaxIterations: 100 (long-running monitoring)
//   - Temperature: 0.3
//   - Middleware: RateLimiter (60 checks per minute, prevents overload), Timing
func MonitoringAgent(llmClient llm.Client, checkInterval time.Duration) (*SimpleAgent, error) {
	state := core.NewAgentState()
	state.Set("check_interval", checkInterval)
	state.Set("last_check", time.Now())
	state.Set("monitoring_status", "active")

	return quickBuilder(llmClient).
		WithSystemPrompt("You are a system monitoring expert. Observe metrics, detect anomalies, and alert on issues promptly.").
		WithState(state).
		WithMaxIterations(100). // 长时间运行监控
		WithTemperature(0.3).   // 平衡的模式识别
		WithMiddleware(
			middleware.NewRateLimiterMiddleware(60, time.Minute), // 限制为每分钟 60 次检查
			middleware.NewTimingMiddleware(),
		).
		WithMetadata("type", "monitoring").
		WithMetadata("check_interval", checkInterval.String()).
		Build()
}

// ResearchAgent creates a preconfigured research and information-gathering
// agent.
//
// This agent is optimized for:
//   - Gathering information from multiple sources
//   - Research report generation
//   - Source synthesis and citation
//   - Fact checking and verification
//
// Configuration:
//   - MaxTokens: 4000 (larger context for comprehensive reports)
//   - Temperature: 0.5 (balances creativity and accuracy)
//   - MaxIterations: 15
//   - Middleware: Timing
func ResearchAgent(llmClient llm.Client, sources []string) (*SimpleAgent, error) {
	state := core.NewAgentState()
	if len(sources) > 0 {
		state.Set("sources", sources)
		state.Set("sources_count", len(sources))
	}
	state.Set("research_status", "initialized")

	return quickBuilder(llmClient).
		WithSystemPrompt("You are a research expert. Gather information from multiple sources, synthesize findings, and provide comprehensive, well-cited reports.").
		WithState(state).
		WithMaxTokens(4000).
		WithTemperature(0.5).
		WithMaxIterations(15).
		WithMiddleware(
			middleware.NewTimingMiddleware(),
		).
		WithMetadata("type", "research").
		WithMetadata("sources_count", len(sources)).
		Build()
}
