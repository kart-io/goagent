package builder

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/checkpoint"
	"github.com/kart-io/goagent/core/execution"
	"github.com/kart-io/goagent/core/middleware"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/store"
	"github.com/kart-io/goagent/store/memory"
)

// AgentBuilder 提供用于构建 Agent 的 fluent API
//
// 受 LangChain 的 create_agent 函数启发,它集成了:
//   - LLM 客户端配置
//   - 工具注册
//   - 状态管理
//   - 运行时上下文
//   - Store 和 Checkpointer
//   - 中间件栈
//   - 系统提示词
type AgentBuilder[C any, S core.State] struct {
	// 核心组件
	llmClient    llm.Client
	tools        []interfaces.Tool
	systemPrompt string

	// Phase 1 组件
	state        S
	store        store.Store
	checkpointer checkpoint.Checkpointer
	context      C

	// Phase 2 组件
	middlewares []middleware.Middleware

	// 配置
	config *AgentConfig

	// 回调
	callbacks []core.Callback

	// 错误处理
	errorHandler func(error) error

	// 元数据
	metadata map[string]interface{}
}

// NewAgentBuilder 创建一个新的 Agent 构建器
func NewAgentBuilder[C any, S core.State](llmClient llm.Client) *AgentBuilder[C, S] {
	return &AgentBuilder[C, S]{
		llmClient:   llmClient,
		tools:       []interfaces.Tool{},
		middlewares: []middleware.Middleware{},
		callbacks:   []core.Callback{},
		config:      DefaultAgentConfig(),
		metadata:    make(map[string]interface{}),
	}
}

// Build 构建最终的 Agent
func (b *AgentBuilder[C, S]) Build() (*ConfigurableAgent[C, S], error) {
	// 验证必需组件
	if b.llmClient == nil {
		return nil, agentErrors.NewInvalidConfigError("builder", "llm_client", "LLM client is required")
	}

	// 如果未提供则设置默认值
	var zero S
	if reflect.DeepEqual(b.state, zero) {
		// 尝试创建默认状态,如果 S 是 *core.AgentState
		if _, ok := any(zero).(*core.AgentState); ok {
			b.state = any(core.NewAgentState()).(S)
		} else {
			return nil, agentErrors.NewInvalidConfigError("builder", "state", "state is required")
		}
	}

	if b.store == nil {
		b.store = memory.New()
	}

	if b.checkpointer == nil {
		b.checkpointer = checkpoint.NewInMemorySaver()
	}

	// 创建运行时
	runtime := execution.NewRuntime(
		b.context,
		b.state,
		b.store,
		b.checkpointer,
		b.config.SessionID,
	)

	// 构建中间件链
	handler := b.createHandler(runtime)
	chain := middleware.NewMiddlewareChain(handler)

	// 如果 verbose,添加默认中间件
	if b.config.Verbose {
		chain.Use(middleware.NewLoggingMiddleware(nil))
		chain.Use(middleware.NewTimingMiddleware())
	}

	// 添加用户指定的中间件
	chain.Use(b.middlewares...)

	// 创建 Agent
	agent := &ConfigurableAgent[C, S]{
		llmClient:    b.llmClient,
		tools:        b.tools,
		systemPrompt: b.systemPrompt,
		runtime:      runtime,
		chain:        chain,
		config:       b.config,
		callbacks:    b.callbacks,
		errorHandler: b.errorHandler,
		metadata:     b.metadata,
	}

	// 如果需要则初始化
	if err := agent.Initialize(context.Background()); err != nil {
		return nil, agentErrors.NewAgentInitializationError("configurable_agent", err)
	}

	return agent, nil
}

// createHandler 创建主执行处理器
func (b *AgentBuilder[C, S]) createHandler(runtime *execution.Runtime[C, S]) middleware.Handler {
	return func(ctx context.Context, request *middleware.MiddlewareRequest) (*middleware.MiddlewareResponse, error) {
		// 提取输入
		inputStr := fmt.Sprintf("%v", request.Input)

		// 创建 LLM 请求
		llmReq := &llm.CompletionRequest{
			Messages: []llm.Message{
				{
					Role:    "system",
					Content: b.systemPrompt,
				},
				{
					Role:    "user",
					Content: inputStr,
				},
			},
			MaxTokens:   b.config.MaxTokens,
			Temperature: b.config.Temperature,
		}

		// 调用 LLM
		response, err := b.llmClient.Complete(ctx, llmReq)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeLLMRequest, "LLM completion error")
		}

		// 触发 OnLLMEnd 回调
		if len(b.callbacks) > 0 {
			for _, cb := range b.callbacks {
				if err := cb.OnLLMEnd(ctx, response.Content, response.TokensUsed); err != nil {
					// 记录错误但不失败请求
					fmt.Fprintf(os.Stderr, "Callback OnLLMEnd error: %v\n", err)
				}
			}
		}

		// 如果需要则更新状态
		if request.State != nil {
			request.State.Set("last_response", response.Content)
			request.State.Set("last_timestamp", time.Now())
		}

		// 如果启用自动保存则保存检查点
		if b.config.EnableAutoSave && runtime.Checkpointer != nil {
			if err := runtime.SaveState(ctx); err != nil {
				// 记录错误但不失败请求
				// 状态保存很重要但对响应不是关键的
				fmt.Fprintf(os.Stderr, "Failed to auto-save state: %v\n", err)
			}
		}

		// 创建响应
		return &middleware.MiddlewareResponse{
			Output:   response.Content,
			State:    request.State,
			Metadata: request.Metadata,
		}, nil
	}
}

// ConfigurableAgent 是具有完整配置的已构建 Agent
type ConfigurableAgent[C any, S core.State] struct {
	llmClient    llm.Client
	tools        []interfaces.Tool
	systemPrompt string
	runtime      *execution.Runtime[C, S]
	chain        *middleware.MiddlewareChain
	config       *AgentConfig
	callbacks    []core.Callback
	errorHandler func(error) error
	metadata     map[string]interface{}
	mu           sync.RWMutex
}

// Initialize 准备 Agent 执行
func (a *ConfigurableAgent[C, S]) Initialize(ctx context.Context) error {
	// 如果存在则加载先前状态
	if a.runtime.Checkpointer != nil {
		if exists, _ := a.runtime.Checkpointer.Exists(ctx, a.config.SessionID); exists {
			state, err := a.runtime.Checkpointer.Load(ctx, a.config.SessionID)
			if err == nil {
				// 更新运行时状态
				a.runtime.State = state.(S)
			}
		}
	}

	// 通知回调
	for _, cb := range a.callbacks {
		if err := cb.OnStart(ctx, a.metadata); err != nil {
			return err
		}
	}

	return nil
}

// Execute 使用给定输入运行 Agent
func (a *ConfigurableAgent[C, S]) Execute(ctx context.Context, input interface{}) (*AgentOutput, error) {
	// 如果配置了超时则应用
	if a.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.config.Timeout)
		defer cancel()
	}

	// 创建请求
	request := &middleware.MiddlewareRequest{
		Input:     input,
		State:     a.runtime.State,
		Runtime:   a.runtime,
		Metadata:  make(map[string]interface{}),
		Headers:   make(map[string]string),
		Timestamp: time.Now(),
	}

	// 添加元数据
	for k, v := range a.metadata {
		request.Metadata[k] = v
	}

	// 通过中间件链执行
	response, err := a.chain.Execute(ctx, request)
	if err != nil {
		// 处理错误
		if a.errorHandler != nil {
			err = a.errorHandler(err)
		}

		// 通知回调
		for _, cb := range a.callbacks {
			if err := cb.OnError(ctx, err); err != nil {
				// 记录回调错误但不覆盖原始错误
				fmt.Fprintf(os.Stderr, "Callback OnError failed: %v\n", err)
			}
		}

		return nil, err
	}

	// 创建输出
	output := &AgentOutput{
		Result:    response.Output,
		State:     response.State,
		Metadata:  response.Metadata,
		Duration:  response.Duration,
		Timestamp: time.Now(),
	}

	// 通知回调
	for _, cb := range a.callbacks {
		if err := cb.OnEnd(ctx, output); err != nil {
			return output, err
		}
	}

	return output, nil
}

// ExecuteWithTools 使用工具执行能力运行 Agent
func (a *ConfigurableAgent[C, S]) ExecuteWithTools(ctx context.Context, input interface{}) (*AgentOutput, error) {
	iterations := 0
	var lastOutput *AgentOutput

	for iterations < a.config.MaxIterations {
		// 执行一步
		output, err := a.Execute(ctx, input)
		if err != nil {
			return nil, err
		}

		lastOutput = output

		// 检查是否需要使用工具
		toolCalls := a.extractToolCalls(output.Result)
		if len(toolCalls) == 0 {
			// 不需要工具,返回结果
			return output, nil
		}

		// 执行工具
		toolResults := make([]interface{}, 0, len(toolCalls))
		for _, call := range toolCalls {
			result, err := a.executeToolCall(ctx, call)
			if err != nil {
				return nil, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "tool execution failed")
			}
			toolResults = append(toolResults, result)
		}

		// 使用工具结果更新输入用于下一次迭代
		input = map[string]interface{}{
			"previous_output": output.Result,
			"tool_results":    toolResults,
		}

		iterations++
	}

	// 达到最大迭代次数
	return lastOutput, agentErrors.New(agentErrors.CodeAgentExecution, "max iterations reached").
		WithContext("max_iterations", a.config.MaxIterations)
}

// extractToolCalls 从 LLM 输出中提取工具调用
func (a *ConfigurableAgent[C, S]) extractToolCalls(output interface{}) []ToolCall {
	// 简化的工具调用提取
	// 在生产中,使用适当的解析
	return []ToolCall{}
}

// executeToolCall 执行单个工具调用
func (a *ConfigurableAgent[C, S]) executeToolCall(ctx context.Context, call ToolCall) (interface{}, error) {
	// 查找工具
	for _, tool := range a.tools {
		if tool.Name() == call.Name {
			// 创建工具输入
			toolInput := &interfaces.ToolInput{
				Args:    call.Input,
				Context: ctx,
			}

			// 执行工具
			output, err := tool.Invoke(ctx, toolInput)
			if err != nil {
				return nil, err
			}
			return output.Result, nil
		}
	}
	return nil, agentErrors.NewToolNotFoundError(call.Name)
}

// GetState 返回当前状态
func (a *ConfigurableAgent[C, S]) GetState() S {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.runtime.State
}

// GetMetrics 返回 Agent 指标
func (a *ConfigurableAgent[C, S]) GetMetrics() map[string]interface{} {
	metrics := make(map[string]interface{})

	// 添加基本指标
	metrics["session_id"] = a.config.SessionID
	metrics["tools_count"] = len(a.tools)

	// 如果可用则添加状态大小
	if state, ok := any(a.runtime.State).(*core.AgentState); ok {
		metrics["state_size"] = state.Size()
	}

	return metrics
}

// Shutdown 优雅地关闭 Agent
func (a *ConfigurableAgent[C, S]) Shutdown(ctx context.Context) error {
	// 保存最终状态
	if a.runtime.Checkpointer != nil {
		if err := a.runtime.SaveState(ctx); err != nil {
			return agentErrors.NewStateCheckpointError(a.config.SessionID, "save_final", err)
		}
	}

	// 通知回调
	for _, cb := range a.callbacks {
		if shutdown, ok := cb.(interface{ OnShutdown(context.Context) error }); ok {
			if err := shutdown.OnShutdown(ctx); err != nil {
				return err
			}
		}
	}

	return nil
}

// ToolCall 表示工具调用请求
type ToolCall struct {
	Name  string
	Input map[string]interface{}
}

// AgentOutput 表示 Agent 执行结果
type AgentOutput struct {
	Result    interface{}
	State     core.State
	Metadata  map[string]interface{}
	Duration  time.Duration
	Timestamp time.Time
}
