package core

import (
	"context"
	"time"

	"github.com/kart-io/goagent/interfaces"
)

// SimpleAgent is a type alias for the canonical Agent interface.
//
// For new code that doesn't need generic typing, use interfaces.Agent directly.
// This alias provides a migration path for existing code.
type SimpleAgent = interfaces.Agent

// Agent 定义通用 AI Agent 接口
//
// Agent 是一个 Runnable[*AgentInput, *AgentOutput]，具有推理能力的智能体，能够：
// - 接收输入并进行处理（通过 Runnable.Invoke）
// - 调用工具获取额外信息
// - 使用 LLM 进行推理
// - 返回结构化输出
// - 支持流式处理、批量执行、管道连接等 Runnable 特性
//
// Note: This is a generic version of the Agent interface.
// For cross-package compatibility, consider using interfaces.Agent.
type Agent interface {
	// 继承 Runnable 接口，Agent 是一个可执行的组件
	Runnable[*AgentInput, *AgentOutput]

	// Agent 特有方法
	// Name 返回 Agent 的名称
	Name() string

	// Description 返回 Agent 的描述
	Description() string

	// Capabilities 返回 Agent 的能力列表
	Capabilities() []string
}

// AgentInput Agent 输入
type AgentInput struct {
	// 任务描述
	Task        string                 `json:"task"`        // 任务描述
	Instruction string                 `json:"instruction"` // 具体指令
	Context     map[string]interface{} `json:"context"`     // 上下文信息

	// 执行选项
	Options AgentOptions `json:"options"` // 执行选项

	// 元数据
	SessionID string    `json:"session_id"` // 会话 ID
	Timestamp time.Time `json:"timestamp"`  // 时间戳
}

// AgentOutput Agent 输出
type AgentOutput struct {
	// 执行结果
	Result  interface{} `json:"result"`  // 结果数据
	Status  string      `json:"status"`  // 状态: "success", "failed", "partial"
	Message string      `json:"message"` // 结果消息

	// 推理过程
	ReasoningSteps []ReasoningStep `json:"reasoning_steps"` // 推理步骤
	ToolCalls      []ToolCall      `json:"tool_calls"`      // 工具调用记录

	// 元数据
	Latency   time.Duration          `json:"latency"`   // 执行延迟
	Timestamp time.Time              `json:"timestamp"` // 时间戳
	Metadata  map[string]interface{} `json:"metadata"`  // 额外元数据
}

// AgentOptions Agent 执行选项
type AgentOptions struct {
	// LLM 配置
	Temperature float64 `json:"temperature,omitempty"` // LLM 温度参数
	MaxTokens   int     `json:"max_tokens,omitempty"`  // 最大 token 数
	Model       string  `json:"model,omitempty"`       // LLM 模型

	// 工具配置
	EnableTools  bool     `json:"enable_tools,omitempty"`   // 是否启用工具
	AllowedTools []string `json:"allowed_tools,omitempty"`  // 允许的工具列表
	MaxToolCalls int      `json:"max_tool_calls,omitempty"` // 最大工具调用次数

	// 记忆配置
	EnableMemory     bool `json:"enable_memory,omitempty"`      // 是否启用记忆
	LoadHistory      bool `json:"load_history,omitempty"`       // 是否加载历史
	SaveToMemory     bool `json:"save_to_memory,omitempty"`     // 是否保存到记忆
	MaxHistoryLength int  `json:"max_history_length,omitempty"` // 最大历史长度

	// 超时配置
	Timeout time.Duration `json:"timeout,omitempty"` // 超时时间
}

// ReasoningStep 推理步骤
type ReasoningStep struct {
	Step        int           `json:"step"`        // 步骤编号
	Action      string        `json:"action"`      // 执行的操作
	Description string        `json:"description"` // 操作描述
	Result      string        `json:"result"`      // 操作结果
	Duration    time.Duration `json:"duration"`    // 耗时
	Success     bool          `json:"success"`     // 是否成功
	Error       string        `json:"error"`       // 错误信息
}

// ToolCall 工具调用记录
type ToolCall struct {
	ToolName string                 `json:"tool_name"` // 工具名称
	Input    map[string]interface{} `json:"input"`     // 输入参数
	Output   interface{}            `json:"output"`    // 输出结果
	Duration time.Duration          `json:"duration"`  // 耗时
	Success  bool                   `json:"success"`   // 是否成功
	Error    string                 `json:"error"`     // 错误信息
}

// BaseAgent 提供 Agent 的基础实现
//
// BaseAgent 实现了 Agent 接口，包括完整的 Runnable 接口支持
// 具体的执行逻辑需要通过组合或继承来实现
type BaseAgent struct {
	*BaseRunnable[*AgentInput, *AgentOutput]
	name         string
	description  string
	capabilities []string
}

// NewBaseAgent 创建基础 Agent
func NewBaseAgent(name, description string, capabilities []string) *BaseAgent {
	return &BaseAgent{
		BaseRunnable: NewBaseRunnable[*AgentInput, *AgentOutput](),
		name:         name,
		description:  description,
		capabilities: capabilities,
	}
}

// Name 返回 Agent 名称
func (a *BaseAgent) Name() string {
	return a.name
}

// Description 返回 Agent 描述
func (a *BaseAgent) Description() string {
	return a.description
}

// Capabilities 返回 Agent 能力列表
func (a *BaseAgent) Capabilities() []string {
	return a.capabilities
}

// Invoke 执行 Agent
// 这是 Runnable 接口的核心方法，需要由具体 Agent 实现
func (a *BaseAgent) Invoke(ctx context.Context, input *AgentInput) (*AgentOutput, error) {
	// 触发回调
	startTime := time.Now()
	if err := a.triggerOnStart(ctx, input); err != nil {
		return nil, err
	}

	// 默认实现返回错误，提示需要重写
	output := &AgentOutput{
		Status:    "failed",
		Message:   "Invoke method must be implemented by concrete agent",
		Timestamp: time.Now(),
		Latency:   time.Since(startTime),
	}

	// 触发回调
	_ = a.triggerOnFinish(ctx, output)

	return output, ErrNotImplemented
}

// Stream 流式执行 Agent
// 默认实现将 Invoke 的结果包装成单个流块
func (a *BaseAgent) Stream(ctx context.Context, input *AgentInput) (<-chan StreamChunk[*AgentOutput], error) {
	outChan := make(chan StreamChunk[*AgentOutput], 1)

	go func() {
		defer close(outChan)

		output, err := a.Invoke(ctx, input)
		outChan <- StreamChunk[*AgentOutput]{
			Data:  output,
			Error: err,
			Done:  true,
		}
	}()

	return outChan, nil
}

// Batch 批量执行 Agent
// 使用 BaseRunnable 的默认批处理实现
func (a *BaseAgent) Batch(ctx context.Context, inputs []*AgentInput) ([]*AgentOutput, error) {
	return a.BaseRunnable.Batch(ctx, inputs, a.Invoke)
}

// Pipe 连接到另一个 Runnable
// 将当前 Agent 的输出连接到下一个 Runnable 的输入
func (a *BaseAgent) Pipe(next Runnable[*AgentOutput, any]) Runnable[*AgentInput, any] {
	return NewRunnablePipe[*AgentInput, *AgentOutput, any](a, next)
}

// WithCallbacks 添加回调处理器
// 返回一个新的 Agent 实例，包含指定的回调
func (a *BaseAgent) WithCallbacks(callbacks ...Callback) Runnable[*AgentInput, *AgentOutput] {
	newAgent := *a
	newAgent.BaseRunnable = a.BaseRunnable.WithCallbacks(callbacks...)
	return &newAgent
}

// WithConfig 配置 Agent
// 返回一个新的 Agent 实例，使用指定的配置
func (a *BaseAgent) WithConfig(config RunnableConfig) Runnable[*AgentInput, *AgentOutput] {
	newAgent := *a
	newAgent.BaseRunnable = a.BaseRunnable.WithConfig(config)
	return &newAgent
}

// triggerOnStart 触发开始回调
func (a *BaseAgent) triggerOnStart(ctx context.Context, input *AgentInput) error {
	config := a.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnStart(ctx, input); err != nil {
			return err
		}
	}
	return nil
}

// triggerOnFinish 触发完成回调
func (a *BaseAgent) triggerOnFinish(ctx context.Context, output *AgentOutput) error {
	config := a.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnAgentFinish(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

// triggerOnAction 触发操作回调
//
//nolint:unused // Reserved for future agent action tracking
func (a *BaseAgent) triggerOnAction(ctx context.Context, action *AgentAction) error {
	config := a.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnAgentAction(ctx, action); err != nil {
			return err
		}
	}
	return nil
}

// DefaultAgentOptions 返回默认的 Agent 选项
func DefaultAgentOptions() AgentOptions {
	return AgentOptions{
		Temperature:      0.7,
		MaxTokens:        2000,
		EnableTools:      true,
		MaxToolCalls:     5,
		EnableMemory:     false,
		LoadHistory:      false,
		SaveToMemory:     false,
		MaxHistoryLength: 10,
		Timeout:          60 * time.Second,
	}
}

// AgentExecutor 执行 Agent 的辅助结构
//
// 提供额外的执行逻辑，如重试、超时控制等
type AgentExecutor struct {
	agent       Agent
	maxRetries  int
	timeout     time.Duration
	stopOnError bool
}

// NewAgentExecutor 创建 Agent 执行器
func NewAgentExecutor(agent Agent, options ...ExecutorOption) *AgentExecutor {
	executor := &AgentExecutor{
		agent:       agent,
		maxRetries:  0,
		timeout:     0,
		stopOnError: true,
	}

	for _, opt := range options {
		opt(executor)
	}

	return executor
}

// ExecutorOption 执行器选项函数
type ExecutorOption func(*AgentExecutor)

// WithMaxRetries 设置最大重试次数
func WithMaxRetries(maxRetries int) ExecutorOption {
	return func(e *AgentExecutor) {
		e.maxRetries = maxRetries
	}
}

// WithTimeout 设置超时时间
func WithTimeout(timeout time.Duration) ExecutorOption {
	return func(e *AgentExecutor) {
		e.timeout = timeout
	}
}

// WithStopOnError 设置是否在错误时停止
func WithStopOnError(stop bool) ExecutorOption {
	return func(e *AgentExecutor) {
		e.stopOnError = stop
	}
}

// Execute 执行 Agent，支持重试和超时
func (e *AgentExecutor) Execute(ctx context.Context, input *AgentInput) (*AgentOutput, error) {
	// 应用超时
	if e.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.timeout)
		defer cancel()
	}

	var lastErr error
	attempts := e.maxRetries + 1 // 第一次尝试 + 重试次数

	for i := 0; i < attempts; i++ {
		output, err := e.agent.Invoke(ctx, input)
		if err == nil {
			return output, nil
		}

		lastErr = err

		// 如果设置了在错误时停止，且不是最后一次尝试，则不重试
		if e.stopOnError && i < attempts-1 {
			return output, err
		}

		// 检查上下文是否已取消
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	return nil, lastErr
}

// ChainableAgent 可链式调用的 Agent
//
// 允许将多个 Agent 串联起来，前一个的输出作为后一个的输入
type ChainableAgent struct {
	*BaseAgent
	agents []Agent
}

// NewChainableAgent 创建可链式调用的 Agent
func NewChainableAgent(name, description string, agents ...Agent) *ChainableAgent {
	capabilities := []string{"chaining"}
	for _, agent := range agents {
		capabilities = append(capabilities, agent.Capabilities()...)
	}

	return &ChainableAgent{
		BaseAgent: NewBaseAgent(name, description, capabilities),
		agents:    agents,
	}
}

// Invoke 顺序调用所有 Agent
func (c *ChainableAgent) Invoke(ctx context.Context, input *AgentInput) (*AgentOutput, error) {
	if len(c.agents) == 0 {
		return &AgentOutput{
			Status:    "success",
			Message:   "No agents in chain",
			Timestamp: time.Now(),
		}, nil
	}

	currentInput := input
	var finalOutput *AgentOutput

	for i, agent := range c.agents {
		output, err := agent.Invoke(ctx, currentInput)
		if err != nil {
			return nil, err
		}

		finalOutput = output

		// 如果不是最后一个 agent，准备下一个的输入
		if i < len(c.agents)-1 {
			// 将当前输出转换为下一个的输入
			currentInput = &AgentInput{
				Task:        currentInput.Task,
				Instruction: currentInput.Instruction,
				Context:     output.Metadata,
				Options:     currentInput.Options,
				SessionID:   currentInput.SessionID,
				Timestamp:   time.Now(),
			}
		}
	}

	return finalOutput, nil
}
