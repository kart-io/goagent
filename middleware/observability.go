package middleware

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	coremiddleware "github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/observability"
)

// ObservabilityMiddleware 可观测性中间件
// 集成 OpenTelemetry 追踪和指标
type ObservabilityMiddleware struct {
	*coremiddleware.BaseMiddleware
	tracer  *observability.AgentTracer
	metrics *observability.AgentMetrics
}

// NewObservabilityMiddleware 创建可观测性中间件
func NewObservabilityMiddleware(provider *observability.TelemetryProvider) (*ObservabilityMiddleware, error) {
	tracer := observability.NewAgentTracer(provider, "agent-middleware")
	metrics, err := observability.NewAgentMetrics(provider, "agent-middleware")
	if err != nil {
		return nil, err
	}

	return &ObservabilityMiddleware{
		BaseMiddleware: coremiddleware.NewBaseMiddleware("observability"),
		tracer:         tracer,
		metrics:        metrics,
	}, nil
}

// OnBefore 请求前处理 - 启动 span 和记录指标
func (m *ObservabilityMiddleware) OnBefore(ctx context.Context, request *coremiddleware.MiddlewareRequest) (*coremiddleware.MiddlewareRequest, error) {
	// 启动 span
	agentName := "unknown"
	if name, ok := request.Metadata["agent.name"].(string); ok {
		agentName = name
	}

	ctx, span := m.tracer.StartAgentSpan(ctx, agentName,
		attribute.String("request.type", "agent_execution"),
		attribute.Int64("request.timestamp", request.Timestamp.Unix()),
	)

	// 将 span 存储到 metadata 中供后续使用
	request.Metadata["_otel_span"] = span
	request.Metadata["_otel_ctx"] = ctx

	// 增加活跃计数
	m.metrics.IncrementActiveAgents(ctx, 1)

	return request, nil
}

// OnAfter 请求后处理 - 记录结果和指标
func (m *ObservabilityMiddleware) OnAfter(ctx context.Context, response *coremiddleware.MiddlewareResponse) (*coremiddleware.MiddlewareResponse, error) {
	// 从 metadata 获取 span
	var span interface{}
	var spanCtx context.Context
	if response.Metadata != nil {
		span = response.Metadata["_otel_span"]
		if ctx, ok := response.Metadata["_otel_ctx"].(context.Context); ok {
			spanCtx = ctx
		}
	}

	// 记录成功
	if span != nil && spanCtx != nil {
		m.tracer.SetStatus(spanCtx, codes.Ok, "execution completed")

		// 添加输出大小属性
		m.tracer.SetAttributes(spanCtx,
			attribute.Float64("response.duration_seconds", response.Duration.Seconds()),
		)
	}

	// 记录指标
	agentName := "unknown"
	agentType := "default"
	if response.Metadata != nil {
		if name, ok := response.Metadata["agent.name"].(string); ok {
			agentName = name
		}
		if typ, ok := response.Metadata["agent.type"].(string); ok {
			agentType = typ
		}
	}

	m.metrics.RecordAgentExecution(ctx, agentName, agentType, response.Duration.Seconds(), response.Error == nil)

	// 减少活跃计数
	m.metrics.IncrementActiveAgents(ctx, -1)

	// 清理 metadata
	if response.Metadata != nil {
		delete(response.Metadata, "_otel_span")
		delete(response.Metadata, "_otel_ctx")
	}

	return response, nil
}

// OnError 错误处理 - 记录错误到 span 和指标
func (m *ObservabilityMiddleware) OnError(ctx context.Context, err error) error {
	// 记录错误到 tracer
	m.tracer.RecordError(ctx, err)

	// 记录错误指标
	errorType := "UnknownError"
	if err != nil {
		errorType = err.Error()
	}
	m.metrics.RecordError(ctx, errorType)

	// 减少活跃计数
	m.metrics.IncrementActiveAgents(ctx, -1)

	return err
}

// RecordToolCall 记录工具调用 (辅助方法)
func (m *ObservabilityMiddleware) RecordToolCall(ctx context.Context, toolName string, duration time.Duration, err error) {
	// 启动工具 span
	_, span := m.tracer.StartToolSpan(ctx, toolName)
	defer span.End()

	if err != nil {
		m.tracer.RecordError(ctx, err)
		m.metrics.RecordToolCall(ctx, toolName, duration.Seconds(), false)
	} else {
		m.tracer.SetStatus(ctx, codes.Ok, "tool call completed")
		m.metrics.RecordToolCall(ctx, toolName, duration.Seconds(), true)
	}
}

// RecordLLMCall 记录 LLM 调用 (辅助方法)
func (m *ObservabilityMiddleware) RecordLLMCall(ctx context.Context, model, provider string, tokens int, duration time.Duration, err error) {
	// 启动 LLM span
	_, span := m.tracer.StartLLMSpan(ctx, model,
		attribute.String("llm.provider", provider),
		attribute.Int("llm.tokens", tokens),
	)
	defer span.End()

	if err != nil {
		m.tracer.RecordError(ctx, err)
		m.metrics.RecordLLMCall(ctx, model, provider, tokens, duration.Seconds(), false)
	} else {
		m.tracer.SetStatus(ctx, codes.Ok, "llm call completed")
		m.metrics.RecordLLMCall(ctx, model, provider, tokens, duration.Seconds(), true)
	}
}
