package executor

import (
	"context"
	"fmt"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// Memory 定义记忆系统接口（简化版，适配 memory.Manager）
type Memory interface {
	// SaveContext 保存上下文
	SaveContext(ctx context.Context, sessionID string, input, output map[string]interface{}) error

	// LoadHistory 加载历史
	LoadHistory(ctx context.Context, sessionID string) ([]map[string]interface{}, error)

	// Clear 清除记忆
	Clear(ctx context.Context, sessionID string) error
}

// AgentExecutor Agent 执行器
//
// 提供高级的执行逻辑，包括:
// - 记忆管理
// - 对话历史
// - 工具管理
// - 错误处理和重试
// - 早停机制
type AgentExecutor struct {
	agent               agentcore.Agent
	tools               []interfaces.Tool
	memory              Memory
	maxIterations       int
	maxExecutionTime    time.Duration
	earlyStoppingMethod string
	handleParsingErrors bool
	returnIntermSteps   bool
	verbose             bool
}

// ExecutorConfig 执行器配置
type ExecutorConfig struct {
	Agent               agentcore.Agent   // Agent 实例
	Tools               []interfaces.Tool // 可用工具
	Memory              Memory            // 记忆系统
	MaxIterations       int               // 最大迭代次数
	MaxExecutionTime    time.Duration     // 最大执行时间
	EarlyStoppingMethod string            // 早停方法: "force", "generate"
	HandleParsingErrors bool              // 是否处理解析错误
	ReturnIntermSteps   bool              // 是否返回中间步骤
	Verbose             bool              // 是否详细输出
}

// NewAgentExecutor 创建 Agent 执行器
func NewAgentExecutor(config ExecutorConfig) *AgentExecutor {
	if config.MaxIterations == 0 {
		config.MaxIterations = 15
	}

	if config.MaxExecutionTime == 0 {
		config.MaxExecutionTime = 5 * time.Minute
	}

	if config.EarlyStoppingMethod == "" {
		config.EarlyStoppingMethod = "force"
	}

	return &AgentExecutor{
		agent:               config.Agent,
		tools:               config.Tools,
		memory:              config.Memory,
		maxIterations:       config.MaxIterations,
		maxExecutionTime:    config.MaxExecutionTime,
		earlyStoppingMethod: config.EarlyStoppingMethod,
		handleParsingErrors: config.HandleParsingErrors,
		returnIntermSteps:   config.ReturnIntermSteps,
		verbose:             config.Verbose,
	}
}

// Run 执行 Agent
func (e *AgentExecutor) Run(ctx context.Context, input string) (string, error) {
	agentInput := &agentcore.AgentInput{
		Task:      input,
		Timestamp: time.Now(),
	}

	output, err := e.Execute(ctx, agentInput)
	if err != nil {
		return "", err
	}

	if result, ok := output.Result.(string); ok {
		return result, nil
	}

	return fmt.Sprintf("%v", output.Result), nil
}

// Execute 执行 Agent 输入
func (e *AgentExecutor) Execute(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	startTime := time.Now()

	// 应用执行超时
	if e.maxExecutionTime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.maxExecutionTime)
		defer cancel()
	}

	// 加载记忆
	if e.memory != nil {
		history, err := e.memory.LoadHistory(ctx, input.SessionID)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to load memory").
				WithComponent("executor_agent").
				WithOperation("Execute").
				WithContext("session_id", input.SessionID)
		}

		if input.Context == nil {
			input.Context = make(map[string]interface{})
		}
		input.Context["history"] = history
	}

	// 执行 Agent（使用快速路径优化内部调用）
	output, err := agentcore.TryInvokeFast(ctx, e.agent, input)
	if err != nil {
		return nil, err
	}

	// 检查是否超时
	if ctx.Err() == context.DeadlineExceeded {
		return nil, agentErrors.New(agentErrors.CodeContextTimeout, "execution timeout").
			WithComponent("executor_agent").
			WithOperation("Execute").
			WithContext("timeout", e.maxExecutionTime)
	}

	// 检查是否超过最大迭代次数
	if len(output.ReasoningSteps) > e.maxIterations {
		if e.earlyStoppingMethod == "force" {
			output.Status = "partial"
			output.Message = fmt.Sprintf("Reached max iterations (%d)", e.maxIterations)
		} else {
			// "generate" - 让 Agent 生成最终答案
			output.Message = "Generating final answer after max iterations"
		}
	}

	// 保存到记忆
	if e.memory != nil {
		memoryInput := map[string]interface{}{
			"input":  input.Task,
			"output": output.Result,
		}
		if err := e.memory.SaveContext(ctx, input.SessionID, memoryInput, output.Metadata); err != nil {
			// 记录错误但不中断执行
			if e.verbose {
				fmt.Printf("Warning: failed to save to memory: %v\n", err)
			}
		}
	}

	// 记录执行时间
	output.Latency = time.Since(startTime)

	return output, nil
}

// ExecuteWithCallbacks 使用回调执行
func (e *AgentExecutor) ExecuteWithCallbacks(
	ctx context.Context,
	input *agentcore.AgentInput,
	callbacks ...agentcore.Callback,
) (*agentcore.AgentOutput, error) {
	// 添加回调到 Agent
	agent := e.agent.WithCallbacks(callbacks...).(agentcore.Agent)

	// 临时替换 agent
	originalAgent := e.agent
	e.agent = agent
	defer func() { e.agent = originalAgent }()

	return e.Execute(ctx, input)
}

// Stream 流式执行
func (e *AgentExecutor) Stream(
	ctx context.Context,
	input *agentcore.AgentInput,
) (<-chan agentcore.StreamChunk[*agentcore.AgentOutput], error) {
	// 应用超时
	if e.maxExecutionTime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.maxExecutionTime)
		defer cancel()
	}

	// 加载记忆
	if e.memory != nil {
		history, err := e.memory.LoadHistory(ctx, input.SessionID)
		if err != nil {
			outChan := make(chan agentcore.StreamChunk[*agentcore.AgentOutput], 1)
			outChan <- agentcore.StreamChunk[*agentcore.AgentOutput]{
				Error: agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to load memory").
					WithComponent("executor_agent").
					WithOperation("Stream").
					WithContext("session_id", input.SessionID),
				Done: true,
			}
			close(outChan)
			return outChan, nil
		}

		if input.Context == nil {
			input.Context = make(map[string]interface{})
		}
		input.Context["history"] = history
	}

	// 流式执行
	return e.agent.Stream(ctx, input)
}

// Batch 批量执行
func (e *AgentExecutor) Batch(
	ctx context.Context,
	inputs []*agentcore.AgentInput,
) ([]*agentcore.AgentOutput, error) {
	outputs := make([]*agentcore.AgentOutput, len(inputs))

	for i, input := range inputs {
		output, err := e.Execute(ctx, input)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "failed to execute input").
				WithComponent("executor_agent").
				WithOperation("Batch").
				WithContext("input_index", i)
		}
		outputs[i] = output
	}

	return outputs, nil
}

// GetTools 获取可用工具
func (e *AgentExecutor) GetTools() []interfaces.Tool {
	return e.tools
}

// GetMemory 获取记忆系统
func (e *AgentExecutor) GetMemory() Memory {
	return e.memory
}

// SetMemory 设置记忆系统
func (e *AgentExecutor) SetMemory(mem Memory) {
	e.memory = mem
}

// SetVerbose 设置详细输出
func (e *AgentExecutor) SetVerbose(verbose bool) {
	e.verbose = verbose
}

// ConversationChain 对话链
//
// 专门用于对话场景的执行器
type ConversationChain struct {
	*AgentExecutor
	conversationMemory Memory
}

// NewConversationChain 创建对话链
func NewConversationChain(agent agentcore.Agent, mem Memory) *ConversationChain {
	executor := NewAgentExecutor(ExecutorConfig{
		Agent:            agent,
		Memory:           mem,
		MaxIterations:    10,
		MaxExecutionTime: 2 * time.Minute,
		Verbose:          false,
	})

	return &ConversationChain{
		AgentExecutor:      executor,
		conversationMemory: mem,
	}
}

// Chat 进行对话
func (c *ConversationChain) Chat(ctx context.Context, message string, sessionID string) (string, error) {
	input := &agentcore.AgentInput{
		Task:      message,
		SessionID: sessionID,
		Timestamp: time.Now(),
	}

	output, err := c.Execute(ctx, input)
	if err != nil {
		return "", err
	}

	if result, ok := output.Result.(string); ok {
		return result, nil
	}

	return fmt.Sprintf("%v", output.Result), nil
}

// ClearMemory 清除对话记忆
func (c *ConversationChain) ClearMemory(ctx context.Context, sessionID string) error {
	if c.conversationMemory != nil {
		return c.conversationMemory.Clear(ctx, sessionID)
	}
	return nil
}

// GetHistory 获取对话历史
func (c *ConversationChain) GetHistory(ctx context.Context, sessionID string) ([]map[string]interface{}, error) {
	if c.conversationMemory != nil {
		return c.conversationMemory.LoadHistory(ctx, sessionID)
	}
	return nil, nil
}
