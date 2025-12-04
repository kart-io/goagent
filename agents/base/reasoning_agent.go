package base

import (
	"context"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// ReasoningStrategy 定义推理策略接口
//
// 所有推理Agent（CoT, ToT, ReAct等）必须实现此接口，
// 提供具体的推理逻辑实现。
type ReasoningStrategy interface {
	// Execute 执行推理逻辑，返回最终结果
	//
	// 参数：
	//   - ctx: 上下文
	//   - input: Agent输入
	//   - llmClient: LLM客户端
	//   - tools: 可用工具列表
	//   - toolsByName: 工具名称映射
	//   - output: 输出对象（策略可以向其添加ReasoningSteps和ToolCalls）
	//
	// 返回：
	//   - result: 推理结果（通常是string或结构化数据）
	//   - err: 执行错误
	Execute(
		ctx context.Context,
		input *agentcore.AgentInput,
		llmClient llm.Client,
		tools []interfaces.Tool,
		toolsByName map[string]interfaces.Tool,
		output *agentcore.AgentOutput,
	) (result interface{}, err error)

	// ExecuteWithGenerator 使用Generator模式执行推理（可选）
	//
	// 此方法支持流式返回中间结果，在每个主要步骤后通过yield返回。
	//
	// 参数：
	//   - ctx: 上下文
	//   - input: Agent输入
	//   - llmClient: LLM客户端
	//   - tools: 可用工具列表
	//   - toolsByName: 工具名称映射
	//   - output: 累积输出对象
	//   - yield: yield函数，用于返回中间结果
	//   - startTime: 开始时间（用于计算延迟）
	//
	// 返回：
	//   - result: 最终推理结果
	//   - err: 执行错误
	//
	// 如果策略不支持Generator模式，可以返回未实现错误。
	ExecuteWithGenerator(
		ctx context.Context,
		input *agentcore.AgentInput,
		llmClient llm.Client,
		tools []interfaces.Tool,
		toolsByName map[string]interfaces.Tool,
		output *agentcore.AgentOutput,
		yield func(*agentcore.AgentOutput, error) bool,
		startTime time.Time,
	) (result interface{}, err error)
}

// BaseReasoningAgent 推理Agent基类
//
// 提供所有推理Agent的通用功能：
//   - 回调触发
//   - Stream实现
//   - RunGenerator框架
//   - 错误处理
//   - 工具管理
//   - Token使用量追踪
//
// 具体推理逻辑通过ReasoningStrategy接口注入。
type BaseReasoningAgent struct {
	*agentcore.BaseAgent
	llm         llm.Client
	tools       []interfaces.Tool
	toolsByName map[string]interfaces.Tool
	strategy    ReasoningStrategy
}

// NewBaseReasoningAgent 创建推理Agent基类实例
func NewBaseReasoningAgent(
	name string,
	description string,
	capabilities []string,
	llmClient llm.Client,
	tools []interfaces.Tool,
	strategy ReasoningStrategy,
) *BaseReasoningAgent {
	// 构建工具映射
	toolsByName := make(map[string]interfaces.Tool)
	for _, tool := range tools {
		toolsByName[tool.Name()] = tool
	}

	return &BaseReasoningAgent{
		BaseAgent:   agentcore.NewBaseAgent(name, description, capabilities),
		llm:         llmClient,
		tools:       tools,
		toolsByName: toolsByName,
		strategy:    strategy,
	}
}

// Invoke 执行推理Agent（含完整回调）
func (b *BaseReasoningAgent) Invoke(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	startTime := time.Now()

	// 触发开始回调
	if err := b.triggerOnStart(ctx, input); err != nil {
		return nil, err
	}

	// 初始化输出
	output := b.initOutput()

	// 执行策略（唯一差异化部分）
	result, err := b.strategy.Execute(ctx, input, b.llm, b.tools, b.toolsByName, output)
	if err != nil {
		return b.handleError(ctx, output, "Strategy execution failed", err, startTime)
	}

	// 设置结果
	output.Result = result
	output.Status = interfaces.StatusSuccess
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)

	// 触发完成回调
	if err := b.triggerOnFinish(ctx, output); err != nil {
		return nil, err
	}

	return output, nil
}

// InvokeFast 快速执行推理Agent（绕过回调）
//
// 用于热路径优化，直接执行策略逻辑，跳过回调
// 性能提升：避免回调遍历开销
//
// 注意：此方法不会触发任何回调（OnStart/OnFinish等）
// 仅在性能关键路径且不需要额外处理时使用
//
//go:inline
func (b *BaseReasoningAgent) InvokeFast(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	startTime := time.Now()

	// 初始化输出
	output := b.initOutput()

	// 直接执行策略（跳过回调）
	result, err := b.strategy.Execute(ctx, input, b.llm, b.tools, b.toolsByName, output)
	if err != nil {
		output.Status = interfaces.StatusFailed
		output.Message = "Strategy execution failed: " + err.Error()
		output.Timestamp = time.Now()
		output.Latency = time.Since(startTime)
		return output, err
	}

	// 设置结果
	output.Result = result
	output.Status = interfaces.StatusSuccess
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)

	return output, nil
}

// Stream 流式执行推理Agent
func (b *BaseReasoningAgent) Stream(ctx context.Context, input *agentcore.AgentInput) (<-chan agentcore.StreamChunk[*agentcore.AgentOutput], error) {
	outChan := make(chan agentcore.StreamChunk[*agentcore.AgentOutput])

	go func() {
		defer close(outChan)

		output, err := b.Invoke(ctx, input)
		outChan <- agentcore.StreamChunk[*agentcore.AgentOutput]{
			Data:  output,
			Error: err,
			Done:  true,
		}
	}()

	return outChan, nil
}

// RunGenerator 使用Generator模式执行推理（实验性功能）
//
// 相比Stream，RunGenerator提供零分配的流式执行，在每个主要步骤后yield中间结果。
//
// 性能优势：
//   - 零内存分配（无channel、goroutine开销）
//   - 支持早期终止（用户可以在任意步骤break）
//   - 更低延迟（无channel发送/接收开销）
//
// 使用示例：
//
//	for output, err := range agent.RunGenerator(ctx, input) {
//	    if err != nil {
//	        log.Error("step failed", err)
//	        continue
//	    }
//	    if output.Status == interfaces.StatusSuccess {
//	        break  // 完成
//	    }
//	}
//
// 注意：此方法不触发Agent级别的回调（OnStart/OnFinish）
func (b *BaseReasoningAgent) RunGenerator(ctx context.Context, input *agentcore.AgentInput) agentcore.Generator[*agentcore.AgentOutput] {
	return func(yield func(*agentcore.AgentOutput, error) bool) {
		startTime := time.Now()

		// 初始化累积输出
		accumulated := b.initOutput()

		// 执行策略的Generator版本
		// 策略负责yield所有中间结果和最终结果
		_, err := b.strategy.ExecuteWithGenerator(ctx, input, b.llm, b.tools, b.toolsByName, accumulated, yield, startTime)

		if err != nil {
			errorOutput := b.createStepOutput(accumulated, "Strategy execution failed", startTime)
			errorOutput.Status = interfaces.StatusFailed
			yield(errorOutput, err) // 最后一次yield，即使返回false也不会panic
			return
		}
	}
}

// WithCallbacks 添加回调处理器
func (b *BaseReasoningAgent) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	newAgent := *b
	newAgent.BaseAgent = b.BaseAgent.WithCallbacks(callbacks...).(*agentcore.BaseAgent)
	return &newAgent
}

// WithConfig 配置Agent
func (b *BaseReasoningAgent) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	newAgent := *b
	newAgent.BaseAgent = b.BaseAgent.WithConfig(config).(*agentcore.BaseAgent)
	return &newAgent
}

// GetLLM 获取LLM客户端（供策略使用）
func (b *BaseReasoningAgent) GetLLM() llm.Client {
	return b.llm
}

// GetTools 获取工具列表（供策略使用）
func (b *BaseReasoningAgent) GetTools() []interfaces.Tool {
	return b.tools
}

// GetToolByName 根据名称获取工具（供策略使用）
func (b *BaseReasoningAgent) GetToolByName(name string) (interfaces.Tool, bool) {
	tool, exists := b.toolsByName[name]
	return tool, exists
}

// initOutput 初始化输出对象
func (b *BaseReasoningAgent) initOutput() *agentcore.AgentOutput {
	return &agentcore.AgentOutput{
		Steps:     make([]agentcore.AgentStep, 0),
		ToolCalls: make([]agentcore.AgentToolCall, 0),
		Metadata:  make(map[string]interface{}),
		TokenUsage: &interfaces.TokenUsage{
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		},
	}
}

// createStepOutput 创建步骤输出（用于Generator）
func (b *BaseReasoningAgent) createStepOutput(accumulated *agentcore.AgentOutput, message string, startTime time.Time) *agentcore.AgentOutput {
	stepOutput := &agentcore.AgentOutput{
		Steps:     make([]agentcore.AgentStep, len(accumulated.Steps)),
		ToolCalls: make([]agentcore.AgentToolCall, len(accumulated.ToolCalls)),
		Metadata:  make(map[string]interface{}),
		TokenUsage: &interfaces.TokenUsage{
			PromptTokens:     accumulated.TokenUsage.PromptTokens,
			CompletionTokens: accumulated.TokenUsage.CompletionTokens,
			TotalTokens:      accumulated.TokenUsage.TotalTokens,
			CachedTokens:     accumulated.TokenUsage.CachedTokens,
		},
		Timestamp: time.Now(),
		Latency:   time.Since(startTime),
		Message:   message,
	}

	// 复制slice
	copy(stepOutput.Steps, accumulated.Steps)
	copy(stepOutput.ToolCalls, accumulated.ToolCalls)

	// 复制metadata
	for k, v := range accumulated.Metadata {
		stepOutput.Metadata[k] = v
	}

	return stepOutput
}

// handleError 处理错误
func (b *BaseReasoningAgent) handleError(ctx context.Context, output *agentcore.AgentOutput, message string, err error, startTime time.Time) (*agentcore.AgentOutput, error) {
	output.Status = interfaces.StatusFailed
	output.Message = message
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)

	_ = b.triggerOnError(ctx, err)
	return output, err
}

// 回调触发方法

func (b *BaseReasoningAgent) triggerOnStart(ctx context.Context, input *agentcore.AgentInput) error {
	config := b.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnStart(ctx, input); err != nil {
			return err
		}
	}
	return nil
}

func (b *BaseReasoningAgent) triggerOnFinish(ctx context.Context, output *agentcore.AgentOutput) error {
	config := b.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnAgentFinish(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (b *BaseReasoningAgent) triggerOnError(ctx context.Context, err error) error {
	config := b.GetConfig()
	for _, cb := range config.Callbacks {
		if cbErr := cb.OnError(ctx, err); cbErr != nil {
			return cbErr
		}
	}
	return nil
}
