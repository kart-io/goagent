package builder

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/checkpoint"
	"github.com/kart-io/goagent/core/execution"
	"github.com/kart-io/goagent/core/middleware"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
	"github.com/kart-io/goagent/store"
	"github.com/kart-io/goagent/store/memory"
	"github.com/kart-io/goagent/utils/json"
)

// commonToolCallingClient 定义内部接口，兼容 common.ToolCallResponse 返回类型
// 这是因为 llm.ToolCallingClient 要求返回 *llm.ToolCallResponse
// 但 DeepSeek/OpenAI 等实际返回的是 *common.ToolCallResponse
type commonToolCallingClient interface {
	GenerateWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (*common.ToolCallResponse, error)
}

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
			Output:     response.Content,
			State:      request.State,
			Metadata:   request.Metadata,
			TokenUsage: response.Usage,
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
		Result:     response.Output,
		State:      response.State,
		Metadata:   response.Metadata,
		Duration:   response.Duration,
		Timestamp:  time.Now(),
		TokenUsage: response.TokenUsage,
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
	// 检查 LLM 客户端是否支持工具调用
	// 优先检查 commonToolCallingClient（实际实现使用的接口）
	toolCaller, ok := a.llmClient.(commonToolCallingClient)
	if !ok {
		// 也尝试检查标准的 llm.ToolCallingClient
		if stdToolCaller, stdOk := a.llmClient.(llm.ToolCallingClient); stdOk {
			// 如果实现了标准接口，包装成 commonToolCallingClient
			toolCaller = &stdToolCallerWrapper{client: stdToolCaller}
		} else {
			// 不支持工具调用，回退到普通执行
			return a.Execute(ctx, input)
		}
	}

	// 如果没有工具配置，直接执行
	if len(a.tools) == 0 {
		return a.Execute(ctx, input)
	}

	// 如果配置了超时则应用
	if a.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.config.Timeout)
		defer cancel()
	}

	startTime := time.Now()
	iterations := 0
	var totalTokenUsage *interfaces.TokenUsage

	// 构建初始提示
	prompt := a.buildPromptWithSystemMessage(input)

	for iterations < a.config.MaxIterations {
		// 使用工具调用接口
		response, err := toolCaller.GenerateWithTools(ctx, prompt, a.tools)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeLLMRequest, "tool-enabled LLM request failed")
		}

		// 累计 token 使用
		if response.Usage != nil {
			if totalTokenUsage == nil {
				totalTokenUsage = &interfaces.TokenUsage{}
			}
			totalTokenUsage.Add(response.Usage)
		}

		// 如果没有工具调用，返回结果
		if len(response.ToolCalls) == 0 {
			return &AgentOutput{
				Result:     response.Content,
				State:      a.runtime.State,
				Metadata:   make(map[string]interface{}),
				Duration:   time.Since(startTime),
				Timestamp:  time.Now(),
				TokenUsage: totalTokenUsage,
			}, nil
		}

		// 执行工具调用
		toolResults := make([]string, 0, len(response.ToolCalls))
		for _, tc := range response.ToolCalls {
			call := ToolCall{}

			// common.ToolCall 可能有两种格式：
			// 1. 直接的 Name 和 Arguments 字段
			// 2. Function 嵌套结构
			if tc.Name != "" {
				call.Name = tc.Name
				call.Input = tc.Arguments
			} else if tc.Function != nil {
				call.Name = tc.Function.Name
				// 解析 JSON 参数
				if tc.Function.Arguments != "" {
					var args map[string]interface{}
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err == nil {
						call.Input = args
					}
				}
			}

			if call.Name == "" {
				continue // 跳过无效的工具调用
			}

			result, err := a.executeToolCall(ctx, call)
			if err != nil {
				return nil, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "tool execution failed")
			}

			// 格式化工具结果
			resultStr, _ := json.Marshal(result)
			toolResults = append(toolResults, fmt.Sprintf("工具 %s 执行结果: %s", call.Name, string(resultStr)))
		}

		// 构建下一轮的提示，包含工具结果
		prompt = fmt.Sprintf("%s\n\n工具调用结果:\n%s\n\n请根据以上工具执行结果，给出最终回答。",
			prompt, strings.Join(toolResults, "\n"))

		iterations++
	}

	// 达到最大迭代次数
	return &AgentOutput{
			Result:     "达到最大迭代次数，无法完成任务",
			State:      a.runtime.State,
			Metadata:   make(map[string]interface{}),
			Duration:   time.Since(startTime),
			Timestamp:  time.Now(),
			TokenUsage: totalTokenUsage,
		}, agentErrors.New(agentErrors.CodeAgentExecution, "max iterations reached").
			WithContext("max_iterations", a.config.MaxIterations)
}

// buildPromptWithSystemMessage 构建包含系统消息的提示
func (a *ConfigurableAgent[C, S]) buildPromptWithSystemMessage(input interface{}) string {
	inputStr := fmt.Sprintf("%v", input)
	if a.systemPrompt != "" {
		return fmt.Sprintf("系统指令: %s\n\n用户请求: %s", a.systemPrompt, inputStr)
	}
	return inputStr
}

// extractToolCalls 从 LLM 输出中提取工具调用
func (a *ConfigurableAgent[C, S]) extractToolCalls(output interface{}) []ToolCall {
	// 尝试从输出中提取工具调用
	switch v := output.(type) {
	case string:
		// 尝试从 JSON 字符串中解析工具调用
		return a.parseToolCallsFromJSON(v)
	case map[string]interface{}:
		// 尝试从 map 中提取工具调用
		if toolCalls, ok := v["tool_calls"].([]interface{}); ok {
			return a.convertToolCalls(toolCalls)
		}
	case *llm.ToolCallResponse:
		// 直接从 LLM 工具调用响应中提取
		return a.convertLLMToolCalls(v.ToolCalls)
	}
	return nil
}

// parseToolCallsFromJSON 从 JSON 字符串中解析工具调用
func (a *ConfigurableAgent[C, S]) parseToolCallsFromJSON(content string) []ToolCall {
	// 尝试解析整个内容为 JSON
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return nil
	}

	if toolCalls, ok := data["tool_calls"].([]interface{}); ok {
		return a.convertToolCalls(toolCalls)
	}
	return nil
}

// convertToolCalls 将通用工具调用数据转换为 ToolCall 切片
func (a *ConfigurableAgent[C, S]) convertToolCalls(toolCalls []interface{}) []ToolCall {
	var result []ToolCall
	for _, tc := range toolCalls {
		if tcMap, ok := tc.(map[string]interface{}); ok {
			call := ToolCall{}
			if name, ok := tcMap["name"].(string); ok {
				call.Name = name
			}
			if input, ok := tcMap["input"].(map[string]interface{}); ok {
				call.Input = input
			} else if args, ok := tcMap["arguments"].(map[string]interface{}); ok {
				call.Input = args
			}
			if call.Name != "" {
				result = append(result, call)
			}
		}
	}
	return result
}

// convertLLMToolCalls 将 LLM ToolCall 转换为 builder ToolCall
func (a *ConfigurableAgent[C, S]) convertLLMToolCalls(llmCalls []llm.ToolCall) []ToolCall {
	var result []ToolCall
	for _, tc := range llmCalls {
		call := ToolCall{
			Name: tc.Function.Name,
		}
		// 解析 JSON 参数
		if tc.Function.Arguments != "" {
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err == nil {
				call.Input = args
			}
		}
		if call.Name != "" {
			result = append(result, call)
		}
	}
	return result
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
	Result     interface{}
	State      core.State
	Metadata   map[string]interface{}
	Duration   time.Duration
	Timestamp  time.Time
	TokenUsage *interfaces.TokenUsage
}

// ==============================
// 简化 API（推荐使用）
// ==============================

// SimpleAgentBuilder 是 AgentBuilder 的简化版本类型别名
//
// 使用最常见的类型参数组合：
//   - Context: any (通用上下文)
//   - State: *core.AgentState (标准状态实现)
//
// 这个类型别名消除了 95% 以上使用场景中的泛型复杂度。
// 如果需要自定义 Context 或 State，请使用原始的 NewAgentBuilder[C, S] 函数。
//
// 使用示例：
//
//	builder := builder.NewSimpleBuilder(llmClient)
//	agent, err := builder.
//	    WithSystemPrompt("你是一个助手").
//	    Build()
type SimpleAgentBuilder = AgentBuilder[any, *core.AgentState]

// SimpleAgent 是 ConfigurableAgent 的简化版本类型别名
//
// 使用最常见的类型参数组合，与 SimpleAgentBuilder 匹配。
type SimpleAgent = ConfigurableAgent[any, *core.AgentState]

// NewSimpleBuilder 创建一个简化的 Agent 构建器
//
// 这是推荐的构建器创建方式，适用于大多数使用场景。
// 相比 NewAgentBuilder[any, *core.AgentState](client)，
// 此函数无需显式指定泛型参数，使用更简洁。
//
// 参数：
//   - llmClient: LLM 客户端实例
//
// 返回：
//   - *SimpleAgentBuilder: 简化的构建器实例
//
// 使用示例：
//
//	client := providers.NewOpenAIClient(apiKey)
//	builder := builder.NewSimpleBuilder(client)
//	agent, err := builder.
//	    WithSystemPrompt("你是一个助手").
//	    WithTools(calculatorTool, searchTool).
//	    Build()
func NewSimpleBuilder(llmClient llm.Client) *SimpleAgentBuilder {
	return NewAgentBuilder[any, *core.AgentState](llmClient)
}

// stdToolCallerWrapper 将 llm.ToolCallingClient 包装成 commonToolCallingClient
type stdToolCallerWrapper struct {
	client llm.ToolCallingClient
}

// GenerateWithTools 实现 commonToolCallingClient 接口
func (w *stdToolCallerWrapper) GenerateWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (*common.ToolCallResponse, error) {
	resp, err := w.client.GenerateWithTools(ctx, prompt, tools)
	if err != nil {
		return nil, err
	}

	// 转换 llm.ToolCallResponse 到 common.ToolCallResponse
	result := &common.ToolCallResponse{
		Content: resp.Content,
		Usage:   resp.Usage,
	}

	for _, tc := range resp.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, common.ToolCall{
			ID: tc.ID,
			Function: &struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}

	return result, nil
}
