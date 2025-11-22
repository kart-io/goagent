package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/logger/core"
)

// InstrumentedAgent 带可观测性的 Agent 包装器
// 自动记录指标、日志和追踪
type InstrumentedAgent struct {
	agent       agentcore.Agent
	serviceName string
	logger      core.Logger
}

// NewInstrumentedAgent 创建带可观测性的 Agent
func NewInstrumentedAgent(agent agentcore.Agent, serviceName string, logger core.Logger) *InstrumentedAgent {
	return &InstrumentedAgent{
		agent:       agent,
		serviceName: serviceName,
		logger:      logger.With("agent", agent.Name()),
	}
}

// Invoke 执行 Agent 并自动记录可观测性数据
func (i *InstrumentedAgent) Invoke(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	start := time.Now()
	agentName := i.agent.Name()

	// 记录并发执行
	IncrementConcurrentExecutions()
	defer DecrementConcurrentExecutions()

	// 启动追踪 span
	ctx, span := StartAgentSpan(ctx, agentName,
		attribute.String("service", i.serviceName),
		attribute.String("task", input.Task),
		attribute.String("session_id", input.SessionID),
	)
	defer span.End()

	// 记录开始日志
	i.logger.Info("Agent execution started",
		"agent", agentName,
		"service", i.serviceName,
		"task", input.Task,
		"session_id", input.SessionID)

	// 执行 Agent
	output, err := i.agent.Invoke(ctx, input)
	duration := time.Since(start)

	// 记录结果
	status := "success"
	if err != nil {
		status = "error"
		RecordError(span, err)
		RecordAgentError(agentName, i.serviceName, "execution_error")

		i.logger.Error("Agent execution failed",
			"agent", agentName,
			"service", i.serviceName,
			"duration", duration,
			"error", err)
	} else {
		i.logger.Info("Agent execution completed",
			"agent", agentName,
			"service", i.serviceName,
			"duration", duration,
			"status", output.Status)

		// 记录工具调用
		for _, toolCall := range output.ToolCalls {
			toolStatus := "success"
			if !toolCall.Success {
				toolStatus = "error"
				RecordToolError(toolCall.ToolName, agentName, "tool_error")
			}
			RecordToolCall(toolCall.ToolName, agentName, toolStatus, toolCall.Duration)

			// 添加工具调用事件到 span
			AddEvent(span, "tool_call",
				attribute.String("tool", toolCall.ToolName),
				attribute.Bool("success", toolCall.Success),
				attribute.Float64("duration_ms", float64(toolCall.Duration.Milliseconds())),
			)
		}
	}

	// 记录执行指标
	RecordAgentExecution(agentName, i.serviceName, status, duration)

	// 添加 span 属性
	AddAttributes(span,
		attribute.String("status", status),
		attribute.Float64("duration_ms", float64(duration.Milliseconds())),
		attribute.Int("tool_calls", len(output.ToolCalls)),
		attribute.Int("reasoning_steps", len(output.ReasoningSteps)),
	)

	return output, err
}

// Name 返回 Agent 名称
func (i *InstrumentedAgent) Name() string {
	return i.agent.Name()
}

// Description 返回 Agent 描述
func (i *InstrumentedAgent) Description() string {
	return i.agent.Description()
}

// Capabilities 返回 Agent 能力
func (i *InstrumentedAgent) Capabilities() []string {
	return i.agent.Capabilities()
}

// Stream 流式执行 Agent 并自动记录可观测性数据
func (i *InstrumentedAgent) Stream(ctx context.Context, input *agentcore.AgentInput) (<-chan agentcore.StreamChunk[*agentcore.AgentOutput], error) {
	start := time.Now()
	agentName := i.agent.Name()

	// 记录并发执行
	IncrementConcurrentExecutions()

	// 启动追踪 span
	ctx, span := StartAgentSpan(ctx, agentName,
		attribute.String("operation", "stream"),
		attribute.String("service", i.serviceName),
		attribute.String("task", input.Task),
		attribute.String("session_id", input.SessionID),
	)

	// 记录开始日志
	i.logger.Info("Agent stream started",
		"agent", agentName,
		"service", i.serviceName,
		"task", input.Task,
		"session_id", input.SessionID)

	// 执行流式操作
	streamChan, err := i.agent.Stream(ctx, input)
	if err != nil {
		DecrementConcurrentExecutions()
		duration := time.Since(start)

		RecordError(span, err)
		RecordAgentError(agentName, i.serviceName, "stream_error")
		RecordAgentExecution(agentName, i.serviceName, "error", duration)

		AddAttributes(span,
			attribute.String("status", "error"),
			attribute.Float64("duration_ms", float64(duration.Milliseconds())),
		)
		span.End()

		i.logger.Error("Agent stream failed",
			"agent", agentName,
			"service", i.serviceName,
			"duration", duration,
			"error", err)

		return nil, err
	}

	// 创建包装的输出通道以跟踪流式处理
	wrappedChan := make(chan agentcore.StreamChunk[*agentcore.AgentOutput])

	go func() {
		defer close(wrappedChan)
		defer span.End()
		defer DecrementConcurrentExecutions()

		chunkCount := 0
		var lastOutput *agentcore.AgentOutput
		var streamErr error

		for chunk := range streamChan {
			chunkCount++
			wrappedChan <- chunk

			if chunk.Error != nil {
				streamErr = chunk.Error
				AddEvent(span, "stream_error",
					attribute.Int("chunk_number", chunkCount),
					attribute.String("error", chunk.Error.Error()),
				)
			} else if chunk.Data != nil {
				lastOutput = chunk.Data
				AddEvent(span, "stream_chunk",
					attribute.Int("chunk_number", chunkCount),
					attribute.Bool("done", chunk.Done),
				)
			}

			if chunk.Done {
				break
			}
		}

		duration := time.Since(start)
		status := "success"

		// 记录最终状态
		if streamErr != nil {
			status = "error"
			RecordError(span, streamErr)
			RecordAgentError(agentName, i.serviceName, "stream_execution_error")

			i.logger.Error("Agent stream completed with error",
				"agent", agentName,
				"service", i.serviceName,
				"duration", duration,
				"chunks", chunkCount,
				"error", streamErr)
		} else {
			i.logger.Info("Agent stream completed",
				"agent", agentName,
				"service", i.serviceName,
				"duration", duration,
				"chunks", chunkCount)

			// 记录工具调用（如果有）
			if lastOutput != nil {
				for _, toolCall := range lastOutput.ToolCalls {
					toolStatus := "success"
					if !toolCall.Success {
						toolStatus = "error"
						RecordToolError(toolCall.ToolName, agentName, "tool_error")
					}
					RecordToolCall(toolCall.ToolName, agentName, toolStatus, toolCall.Duration)

					AddEvent(span, "tool_call",
						attribute.String("tool", toolCall.ToolName),
						attribute.Bool("success", toolCall.Success),
						attribute.Float64("duration_ms", float64(toolCall.Duration.Milliseconds())),
					)
				}
			}
		}

		// 记录执行指标
		RecordAgentExecution(agentName, i.serviceName, status, duration)

		// 添加 span 属性
		AddAttributes(span,
			attribute.String("status", status),
			attribute.Float64("duration_ms", float64(duration.Milliseconds())),
			attribute.Int("chunk_count", chunkCount),
		)

		if lastOutput != nil {
			AddAttributes(span,
				attribute.Int("tool_calls", len(lastOutput.ToolCalls)),
				attribute.Int("reasoning_steps", len(lastOutput.ReasoningSteps)),
			)
		}
	}()

	return wrappedChan, nil
}

// Batch 批量执行 Agent 并自动记录可观测性数据
func (i *InstrumentedAgent) Batch(ctx context.Context, inputs []*agentcore.AgentInput) ([]*agentcore.AgentOutput, error) {
	start := time.Now()
	agentName := i.agent.Name()
	batchSize := len(inputs)

	// 记录并发执行
	IncrementConcurrentExecutions()
	defer DecrementConcurrentExecutions()

	// 启动追踪 span
	ctx, span := StartAgentSpan(ctx, agentName,
		attribute.String("operation", "batch"),
		attribute.String("service", i.serviceName),
		attribute.Int("batch_size", batchSize),
	)
	defer span.End()

	// 记录开始日志
	i.logger.Info("Agent batch execution started",
		"agent", agentName,
		"service", i.serviceName,
		"batch_size", batchSize)

	// 执行批量操作
	outputs, err := i.agent.Batch(ctx, inputs)
	duration := time.Since(start)

	// 记录结果
	status := "success"
	outputCount := len(outputs)

	if err != nil {
		status = "error"
		RecordError(span, err)
		RecordAgentError(agentName, i.serviceName, "batch_error")

		i.logger.Error("Agent batch execution failed",
			"agent", agentName,
			"service", i.serviceName,
			"batch_size", batchSize,
			"output_count", outputCount,
			"duration", duration,
			"error", err)
	} else {
		i.logger.Info("Agent batch execution completed",
			"agent", agentName,
			"service", i.serviceName,
			"batch_size", batchSize,
			"output_count", outputCount,
			"duration", duration)

		// 记录工具调用统计
		totalToolCalls := 0
		successfulToolCalls := 0
		for _, output := range outputs {
			if output != nil {
				for _, toolCall := range output.ToolCalls {
					totalToolCalls++
					toolStatus := "success"
					if !toolCall.Success {
						toolStatus = "error"
						RecordToolError(toolCall.ToolName, agentName, "tool_error")
					} else {
						successfulToolCalls++
					}
					RecordToolCall(toolCall.ToolName, agentName, toolStatus, toolCall.Duration)
				}
			}
		}

		// 添加批量工具调用事件
		if totalToolCalls > 0 {
			AddEvent(span, "batch_tool_calls",
				attribute.Int("total_tool_calls", totalToolCalls),
				attribute.Int("successful_tool_calls", successfulToolCalls),
				attribute.Int("failed_tool_calls", totalToolCalls-successfulToolCalls),
			)
		}
	}

	// 记录执行指标
	RecordAgentExecution(agentName, i.serviceName, status, duration)

	// 添加 span 属性
	AddAttributes(span,
		attribute.String("status", status),
		attribute.Float64("duration_ms", float64(duration.Milliseconds())),
		attribute.Int("batch_size", batchSize),
		attribute.Int("output_count", outputCount),
	)

	// 统计总的推理步骤和工具调用
	totalReasoningSteps := 0
	totalToolCalls := 0
	for _, output := range outputs {
		if output != nil {
			totalReasoningSteps += len(output.ReasoningSteps)
			totalToolCalls += len(output.ToolCalls)
		}
	}

	AddAttributes(span,
		attribute.Int("total_tool_calls", totalToolCalls),
		attribute.Int("total_reasoning_steps", totalReasoningSteps),
	)

	return outputs, err
}

// Pipe 连接到另一个 Runnable（委托给内部 agent）
func (i *InstrumentedAgent) Pipe(next agentcore.Runnable[*agentcore.AgentOutput, any]) agentcore.Runnable[*agentcore.AgentInput, any] {
	return i.agent.Pipe(next)
}

// WithCallbacks 添加回调处理器（委托给内部 agent）
func (i *InstrumentedAgent) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	return i.agent.WithCallbacks(callbacks...)
}

// WithConfig 配置 Agent（委托给内部 agent）
func (i *InstrumentedAgent) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	return i.agent.WithConfig(config)
}

// WrapAgent 包装 Agent 以添加可观测性
func WrapAgent(agent agentcore.Agent, serviceName string, logger core.Logger) agentcore.Agent {
	return NewInstrumentedAgent(agent, serviceName, logger)
}
