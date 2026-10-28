package react

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/parsers"
	"github.com/kart-io/goagent/performance"
)

// ReActAgent ReAct (Reasoning + Acting) Agent
//
// 实现 LangChain 的 ReAct 模式，通过思考-行动-观察循环解决问题:
// 1. Thought: 分析当前情况
// 2. Action: 决定使用哪个工具
// 3. Observation: 执行工具并观察结果
// 4. 循环直到得出最终答案
type ReActAgent struct {
	*agentcore.BaseAgent
	llm             llm.Client
	tools           []interfaces.Tool
	toolsByName     map[string]interfaces.Tool
	parser          *parsers.ReActOutputParser
	maxSteps        int
	maxParseRetries int
	stopPattern     []string
	promptPrefix    string
	promptSuffix    string
	formatInstr     string
}

// ReActConfig ReAct Agent 配置
type ReActConfig struct {
	Name            string            // Agent 名称
	Description     string            // Agent 描述
	LLM             llm.Client        // LLM 客户端
	Tools           []interfaces.Tool // 可用工具列表
	MaxSteps        int               // 最大步数
	MaxParseRetries int               // Optional format repairs within MaxSteps; zero disables repairs.
	StopPattern     []string          // 停止模式
	PromptPrefix    string            // Prompt 前缀
	PromptSuffix    string            // Prompt 后缀
	FormatInstr     string            // 格式说明
}

// NewReActAgent 创建 ReAct Agent
func NewReActAgent(config ReActConfig) *ReActAgent {
	if config.MaxSteps <= 0 {
		config.MaxSteps = 10
	}

	if len(config.StopPattern) == 0 {
		config.StopPattern = []string{parsers.MarkerFinalAnswer}
	}

	if config.PromptPrefix == "" {
		config.PromptPrefix = defaultReActPromptPrefix
	}

	if config.PromptSuffix == "" {
		config.PromptSuffix = defaultReActPromptSuffix
	}

	if config.FormatInstr == "" {
		config.FormatInstr = defaultReActFormatInstructions
	}

	// 构建工具映射
	toolsByName := make(map[string]interfaces.Tool)
	for _, tool := range config.Tools {
		toolsByName[tool.Name()] = tool
	}

	capabilities := []string{"reasoning", "tool_calling", "multi_step"}

	parser := parsers.NewReActOutputParser()

	agent := &ReActAgent{
		BaseAgent:       agentcore.NewBaseAgent(config.Name, config.Description, capabilities),
		llm:             config.LLM,
		tools:           config.Tools,
		toolsByName:     toolsByName,
		parser:          parser,
		maxSteps:        config.MaxSteps,
		maxParseRetries: config.MaxParseRetries,
		stopPattern:     config.StopPattern,
		promptPrefix:    config.PromptPrefix,
		promptSuffix:    config.PromptSuffix,
		formatInstr:     config.FormatInstr,
	}

	return agent
}

// Invoke 执行 ReAct Agent（含完整回调）
func (r *ReActAgent) Invoke(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	startTime := time.Now()

	// 触发开始回调
	if err := r.triggerOnStart(ctx, input); err != nil {
		return nil, err
	}

	// 执行核心逻辑
	output, err := r.executeCore(ctx, input, startTime, true, nil)

	// 触发完成回调
	if err == nil {
		if cbErr := r.triggerOnFinish(ctx, output); cbErr != nil {
			return nil, cbErr
		}
	}

	return output, err
}

// InvokeFast 快速执行（绕过回调）
//
// 用于热路径优化，直接执行核心逻辑，跳过所有回调
// 性能提升：避免回调遍历和接口调用开销
//
// 使用场景：
// - Chain 内部调用
// - 嵌套 Agent 调用
// - 高频循环场景
// - 不需要监控和追踪的内部调用
//
// 注意：此方法不会触发任何回调（OnStart/OnFinish/OnLLMStart/OnToolStart 等）
//
//go:inline
func (r *ReActAgent) InvokeFast(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	return r.executeCore(ctx, input, time.Now(), false, nil)
}

// executeCore 核心执行逻辑
//
// withCallbacks 参数控制是否触发回调：
//   - true: 触发 LLM 和 Tool 回调（用于 Invoke）
//   - false: 跳过所有回调（用于 InvokeFast）
func (r *ReActAgent) executeCore(ctx context.Context, input *agentcore.AgentInput, startTime time.Time, withCallbacks bool, emit func(*agentcore.AgentOutput) bool) (*agentcore.AgentOutput, error) {
	if input == nil {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "agent input is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// 构建初始 prompt
	prompt := r.buildPrompt(input)

	// 初始化输出
	output := &agentcore.AgentOutput{
		Steps:     make([]agentcore.AgentStep, 0),
		ToolCalls: make([]agentcore.AgentToolCall, 0),
		Metadata:  make(map[string]interface{}),
		TokenUsage: &interfaces.TokenUsage{
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		},
	}

	// ReAct 循环
	var finalAnswer string
	scratchpad := ""
	parseRetries := 0

	for step := 0; step < r.maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return r.handleErrorFast(output, step, "Execution canceled", err, startTime)
		}
		// 构建当前输入
		currentPrompt := prompt + scratchpad

		// 调用 LLM
		llmStart := time.Now()

		// 可选：触发 LLM 开始回调
		if withCallbacks {
			if err := r.triggerOnLLMStart(ctx, []string{currentPrompt}); err != nil {
				return nil, err
			}
		}

		// 使用 LLM Chat 接口
		messages := []llm.Message{
			llm.UserMessage(currentPrompt),
		}

		llmResp, err := r.llm.Chat(ctx, messages)
		if err != nil {
			if withCallbacks {
				err = r.triggerOnLLMError(ctx, err)
			}
			return r.handleErrorFast(output, step, "LLM call failed", err, startTime)
		}

		if llmResp == nil {
			return r.handleErrorFast(output, step, "LLM call failed", fmt.Errorf("nil LLM response"), startTime)
		}
		if err := ctx.Err(); err != nil {
			return r.handleErrorFast(output, step, "Execution canceled", err, startTime)
		}

		// Collect token usage
		if llmResp.Usage != nil {
			output.TokenUsage.Add(llmResp.Usage)
		}

		llmOutput := llmResp.Content

		// 可选：触发 LLM 结束回调
		if withCallbacks {
			if err := r.triggerOnLLMEnd(ctx, llmOutput, llmResp.TokensUsed); err != nil {
				return nil, err
			}
		}

		// 解析 LLM 输出
		parsed, err := r.parser.Parse(ctx, llmOutput)
		if err != nil {
			if parseRetries < r.maxParseRetries && step+1 < r.maxSteps {
				parseRetries++
				output.Metadata["parse_retries"] = parseRetries
				scratchpad += "\nPrevious model output (format rejected, no tool executed):\n" + llmOutput + "\nReturn either Final Answer: followed by the complete answer in the requested format, or Action: <tool> and Action Input: <JSON>. Do not repeat completed tool calls.\n"
				continue
			}
			return r.handleErrorFast(output, step, "Failed to parse LLM output", err, startTime)
		}

		// 检查是否得到最终答案
		if parsed.FinalAnswer != "" {
			finalAnswer = parsed.FinalAnswer
			output.Steps = append(output.Steps, agentcore.AgentStep{
				Step:        step + 1,
				Action:      "Final Answer",
				Description: "Reached final conclusion",
				Result:      parsed.FinalAnswer,
				Duration:    time.Since(llmStart),
				Success:     true,
			})
			break
		}

		// 提取思考和行动
		thought := parsed.Thought
		action := parsed.Action
		actionInput := parsed.ActionInput

		if action == "" {
			return r.handleErrorFast(output, step, "No action specified", agentErrors.New(agentErrors.CodeParserFailed, "empty action").
				WithComponent("react_agent").
				WithOperation("executeCore"), startTime)
		}

		// 记录思考步骤
		output.Steps = append(output.Steps, agentcore.AgentStep{
			Step:        step + 1,
			Action:      parsers.FieldThought,
			Description: thought,
			Result:      "",
			Duration:    time.Since(llmStart),
			Success:     true,
		})

		if emit != nil {
			snapshot := r.createStepOutput(output, step, fmt.Sprintf("Thought: %s", thought), startTime)
			snapshot.Status = interfaces.StatusInProgress
			snapshot.Metadata["step_type"] = "thought"
			if !emit(snapshot) {
				return output, nil
			}
		}
		// Do not invoke tools after the consumer cancels or stops iteration.
		if err := ctx.Err(); err != nil {
			return r.handleErrorFast(output, step, "Execution canceled", err, startTime)
		}

		// 执行工具
		toolStart := time.Now()
		var observation interface{}
		var toolErr error

		if withCallbacks {
			observation, toolErr = r.executeTool(ctx, action, actionInput)
		} else {
			observation, toolErr = r.executeToolFast(ctx, action, actionInput)
		}

		// 记录工具调用
		toolCall := agentcore.AgentToolCall{
			ToolName: action,
			Input:    actionInput,
			Output:   observation,
			Duration: time.Since(toolStart),
			Success:  toolErr == nil,
		}
		if toolErr != nil {
			toolCall.Error = toolErr.Error()
			observation = fmt.Sprintf("Error: %v", toolErr)
		}
		output.ToolCalls = append(output.ToolCalls, toolCall)

		// 记录行动步骤
		output.Steps = append(output.Steps, agentcore.AgentStep{
			Step:        step + 1,
			Action:      parsers.FieldAction,
			Description: fmt.Sprintf("Tool: %s", action),
			Result:      fmt.Sprintf("%v", observation),
			Duration:    time.Since(toolStart),
			Success:     toolErr == nil,
			Error:       toolCall.Error,
		})

		if emit != nil {
			snapshot := r.createStepOutput(output, step, fmt.Sprintf("Action: %s", action), startTime)
			snapshot.Status = interfaces.StatusInProgress
			snapshot.Metadata["step_type"] = "action"
			snapshot.Metadata["tool_name"] = action
			snapshot.Metadata["observation"] = observation
			if !emit(snapshot) {
				return output, nil
			}
		}

		// 更新 scratchpad
		scratchpad += fmt.Sprintf("\n%s %s\n%s %s\n%s %v\n%s %v\n",
			parsers.MarkerThought, thought,
			parsers.MarkerAction, action,
			parsers.MarkerActionInput, actionInput,
			parsers.MarkerObservation, observation)

		// 检查是否达到停止条件
		if r.shouldStop(llmOutput) {
			break
		}
	}

	// 构建最终输出
	if finalAnswer != "" {
		output.Status = interfaces.StatusSuccess
		output.Result = finalAnswer
		output.Message = "Task completed successfully"
	} else {
		output.Status = interfaces.StatusPartial
		output.Result = scratchpad
		output.Message = fmt.Sprintf("Reached max steps (%d) without final answer", r.maxSteps)
	}

	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)
	output.Metadata["steps"] = len(output.Steps)
	output.Metadata["tool_calls"] = len(output.ToolCalls)
	if emit != nil {
		currentStep := 0
		if len(output.Steps) > 0 {
			currentStep = output.Steps[len(output.Steps)-1].Step
		}
		output.Metadata["current_step"] = currentStep
		output.Metadata["max_steps"] = r.maxSteps
		output.Metadata["total_reasoning_steps"] = len(output.Steps)
		output.Metadata["total_tool_calls"] = len(output.ToolCalls)
	}

	return output, nil
}

// Stream emits reasoning and tool-result snapshots followed by one terminal chunk.
// This is step streaming, not model token streaming. Cancel ctx when abandoning
// the channel so the producer and any context-aware tool/model can stop.
func (r *ReActAgent) Stream(ctx context.Context, input *agentcore.AgentInput) (<-chan agentcore.StreamChunk[*agentcore.AgentOutput], error) {
	if input == nil {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "agent input is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	outChan := make(chan agentcore.StreamChunk[*agentcore.AgentOutput])
	go func() {
		defer close(outChan)
		send := func(chunk agentcore.StreamChunk[*agentcore.AgentOutput]) bool {
			if ctx.Err() != nil {
				return false
			}
			select {
			case outChan <- chunk:
				return true
			case <-ctx.Done():
				return false
			}
		}
		startTime := time.Now()
		if err := r.triggerOnStart(ctx, input); err != nil {
			send(agentcore.StreamChunk[*agentcore.AgentOutput]{Error: err, Done: true})
			return
		}
		output, err := r.executeCore(ctx, input, startTime, true, func(snapshot *agentcore.AgentOutput) bool {
			return send(agentcore.StreamChunk[*agentcore.AgentOutput]{Data: snapshot})
		})
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = r.triggerOnFinish(ctx, output)
		}
		send(agentcore.StreamChunk[*agentcore.AgentOutput]{Data: output, Error: err, Done: true})
	}()
	return outChan, nil
}

// RunGenerator emits the same step snapshots and terminal result as Stream,
// without a channel or producer goroutine. Breaking iteration stops execution.
// It triggers LLM and tool callbacks, but not agent-level start/finish callbacks.
func (r *ReActAgent) RunGenerator(ctx context.Context, input *agentcore.AgentInput) agentcore.Generator[*agentcore.AgentOutput] {
	return func(yield func(*agentcore.AgentOutput, error) bool) {
		stopped := false
		output, err := r.executeCore(ctx, input, time.Now(), true, func(snapshot *agentcore.AgentOutput) bool {
			if !yield(snapshot, nil) {
				stopped = true
				return false
			}
			return true
		})
		if !stopped {
			yield(output, err)
		}
	}
}

// createStepOutput 创建步骤输出（辅助函数）
func (r *ReActAgent) createStepOutput(accumulated *agentcore.AgentOutput, currentStep int, message string, startTime time.Time) *agentcore.AgentOutput {
	// 创建当前步骤的输出快照
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

	// 复制推理步骤和工具调用
	copy(stepOutput.Steps, accumulated.Steps)
	copy(stepOutput.ToolCalls, accumulated.ToolCalls)

	// 复制元数据
	for k, v := range accumulated.Metadata {
		stepOutput.Metadata[k] = v
	}

	// 添加当前步骤信息
	stepOutput.Metadata["current_step"] = currentStep + 1
	stepOutput.Metadata["max_steps"] = r.maxSteps
	stepOutput.Metadata["total_reasoning_steps"] = len(stepOutput.Steps)
	stepOutput.Metadata["total_tool_calls"] = len(stepOutput.ToolCalls)

	return stepOutput
}

// WithCallbacks 添加回调处理器
func (r *ReActAgent) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	newAgent := *r
	newAgent.BaseAgent = r.BaseAgent.WithCallbacks(callbacks...).(*agentcore.BaseAgent)
	return &newAgent
}

// WithConfig 配置 Agent
func (r *ReActAgent) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	newAgent := *r
	newAgent.BaseAgent = r.BaseAgent.WithConfig(config).(*agentcore.BaseAgent)
	return &newAgent
}

// executeTool 执行工具（含回调）
func (r *ReActAgent) executeTool(ctx context.Context, toolName string, input map[string]interface{}) (interface{}, error) {
	tool, ok := r.toolsByName[toolName]
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeToolNotFound, "tool not found").
			WithComponent("react_agent").
			WithOperation("executeTool").
			WithContext("tool_name", toolName)
	}

	// 触发工具回调
	if err := r.triggerOnToolStart(ctx, toolName, input); err != nil {
		return nil, err
	}

	// 执行工具
	toolInput := &interfaces.ToolInput{
		Args:    input,
		Context: ctx,
	}

	output, err := tool.Invoke(ctx, toolInput)
	if err != nil {
		err = r.triggerOnToolError(ctx, toolName, err)
		return nil, err
	}

	if err := r.triggerOnToolEnd(ctx, toolName, output.Result); err != nil {
		return nil, err
	}

	return output.Result, nil
}

// executeToolFast 快速执行工具（无回调）
//
// 用于热路径优化，跳过工具回调
//
//go:inline
func (r *ReActAgent) executeToolFast(ctx context.Context, toolName string, input map[string]interface{}) (interface{}, error) {
	tool, ok := r.toolsByName[toolName]
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeToolNotFound, "tool not found").
			WithComponent("react_agent").
			WithOperation("executeToolFast").
			WithContext("tool_name", toolName)
	}

	// 直接执行工具，无回调
	toolInput := &interfaces.ToolInput{
		Args:    input,
		Context: ctx,
	}

	output, err := tool.Invoke(ctx, toolInput)
	if err != nil {
		return nil, err
	}

	return output.Result, nil
}

// buildPrompt 构建 prompt
func (r *ReActAgent) buildPrompt(input *agentcore.AgentInput) string {
	// 构建工具描述
	toolDescriptions := make([]string, 0, len(r.tools))
	for _, tool := range r.tools {
		toolDescriptions = append(toolDescriptions, fmt.Sprintf("- %s: %s", tool.Name(), tool.Description()))
	}

	// 替换占位符
	prompt := r.promptPrefix
	prompt = strings.ReplaceAll(prompt, "{tools}", strings.Join(toolDescriptions, "\n"))
	prompt = strings.ReplaceAll(prompt, "{tool_names}", r.getToolNames())
	prompt = strings.ReplaceAll(prompt, "{format_instructions}", r.formatInstr)

	// 添加任务
	prompt += "\n\n" + r.promptSuffix
	prompt = strings.ReplaceAll(prompt, "{input}", input.Task)
	if input.Instruction != "" {
		prompt = strings.ReplaceAll(prompt, "{instruction}", input.Instruction)
	}

	return prompt
}

// getToolNames 获取工具名称列表
func (r *ReActAgent) getToolNames() string {
	names := make([]string, 0, len(r.tools))
	for _, tool := range r.tools {
		names = append(names, tool.Name())
	}
	return strings.Join(names, ", ")
}

// shouldStop 检查是否应该停止
func (r *ReActAgent) shouldStop(output string) bool {
	for _, pattern := range r.stopPattern {
		if strings.Contains(output, pattern) {
			return true
		}
	}
	return false
}

// handleErrorFast 快速处理错误（无回调）
//
// 用于热路径优化，跳过错误回调
//
//go:inline
func (r *ReActAgent) handleErrorFast(output *agentcore.AgentOutput, step int, message string, err error, startTime time.Time) (*agentcore.AgentOutput, error) {
	output.Status = "failed"
	output.Message = message
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)
	output.Steps = append(output.Steps, agentcore.AgentStep{
		Step:    step + 1,
		Action:  "Error",
		Success: false,
		Error:   err.Error(),
	})

	return output, err
}

// 回调触发辅助方法
func (r *ReActAgent) triggerOnStart(ctx context.Context, input *agentcore.AgentInput) error {
	_, err := agentcore.NotifyCallbacks(ctx, "react_agent", "on_start", r.GetConfig().Callbacks, func(cb agentcore.Callback) error {
		return cb.OnStart(ctx, input)
	})
	return err
}

func (r *ReActAgent) triggerOnFinish(ctx context.Context, output *agentcore.AgentOutput) error {
	_, err := agentcore.NotifyCallbacks(ctx, "react_agent", "on_agent_finish", r.GetConfig().Callbacks, func(cb agentcore.Callback) error {
		return cb.OnAgentFinish(ctx, output)
	})
	return err
}

// triggerOnError 触发错误回调，返回应交给调用方的错误：回调自身 panic 时为 *PanicError，否则为原错误 err
//
//nolint:unused // Reserved for potential future use in error handling paths
func (r *ReActAgent) triggerOnError(ctx context.Context, err error) error {
	if panicked, panicErr := agentcore.NotifyCallbacks(ctx, "react_agent", "on_error", r.GetConfig().Callbacks, func(cb agentcore.Callback) error {
		return cb.OnError(ctx, err)
	}); panicked {
		return panicErr
	}
	return err
}

func (r *ReActAgent) triggerOnLLMStart(ctx context.Context, prompts []string) error {
	_, err := agentcore.NotifyCallbacks(ctx, "react_agent", "on_llm_start", r.GetConfig().Callbacks, func(cb agentcore.Callback) error {
		return cb.OnLLMStart(ctx, prompts, "")
	})
	return err
}

func (r *ReActAgent) triggerOnLLMEnd(ctx context.Context, output string, tokenUsage int) error {
	_, err := agentcore.NotifyCallbacks(ctx, "react_agent", "on_llm_end", r.GetConfig().Callbacks, func(cb agentcore.Callback) error {
		return cb.OnLLMEnd(ctx, output, tokenUsage)
	})
	return err
}

// triggerOnLLMError 触发错误回调，返回应交给调用方的错误：回调自身 panic 时为 *PanicError，否则为原错误 err
func (r *ReActAgent) triggerOnLLMError(ctx context.Context, err error) error {
	if panicked, panicErr := agentcore.NotifyCallbacks(ctx, "react_agent", "on_llm_error", r.GetConfig().Callbacks, func(cb agentcore.Callback) error {
		return cb.OnLLMError(ctx, err)
	}); panicked {
		return panicErr
	}
	return err
}

func (r *ReActAgent) triggerOnToolStart(ctx context.Context, toolName string, input interface{}) error {
	_, err := agentcore.NotifyCallbacks(ctx, "react_agent", "on_tool_start", r.GetConfig().Callbacks, func(cb agentcore.Callback) error {
		return cb.OnToolStart(ctx, toolName, input)
	})
	return err
}

func (r *ReActAgent) triggerOnToolEnd(ctx context.Context, toolName string, output interface{}) error {
	_, err := agentcore.NotifyCallbacks(ctx, "react_agent", "on_tool_end", r.GetConfig().Callbacks, func(cb agentcore.Callback) error {
		return cb.OnToolEnd(ctx, toolName, output)
	})
	return err
}

// triggerOnToolError 触发错误回调，返回应交给调用方的错误：回调自身 panic 时为 *PanicError，否则为原错误 err
func (r *ReActAgent) triggerOnToolError(ctx context.Context, toolName string, err error) error {
	if panicked, panicErr := agentcore.NotifyCallbacks(ctx, "react_agent", "on_tool_error", r.GetConfig().Callbacks, func(cb agentcore.Callback) error {
		return cb.OnToolError(ctx, toolName, err)
	}); panicked {
		return panicErr
	}
	return err
}

// 默认 ReAct Prompts
const defaultReActPromptPrefix = `Answer the following questions as best you can. You have access to the following tools:

{tools}

Use the following format:

{format_instructions}

Begin!`

const defaultReActPromptSuffix = `Question: {input}
` + parsers.MarkerThought

const defaultReActFormatInstructions = parsers.MarkerThought + ` you should always think about what to do
` + parsers.MarkerAction + ` the action to take, should be one of [{tool_names}]
` + parsers.MarkerActionInput + ` the input to the action
` + parsers.MarkerObservation + ` the result of the action
... (this Thought/Action/Action Input/Observation can repeat N times)
` + parsers.MarkerThought + ` I now know the final answer
` + parsers.MarkerFinalAnswer + ` the final answer to the original input question`

// NewCachedReActAgent creates a ReAct agent with caching enabled
// This wraps a ReActAgent with performance.CachedAgent for automatic result caching.
// Cached ReAct agents are ideal for scenarios with repeated reasoning patterns or queries.
//
// Example:
//
//	config := react.ReActConfig{
//	    Name:        "cached-react",
//	    Description: "ReAct agent with caching",
//	    LLM:         llmClient,
//	    Tools:       tools,
//	    MaxSteps:    10,
//	}
//	cachedAgent := react.NewCachedReActAgent(config, nil)
//
// Or with custom cache config:
//
//	cacheConfig := &performance.CacheConfig{
//	    TTL:     5 * time.Minute,
//	    MaxSize: 500,
//	}
//	cachedAgent := react.NewCachedReActAgent(config, cacheConfig)
func NewCachedReActAgent(config ReActConfig, cacheConfig *performance.CacheConfig) agentcore.Agent {
	agent := NewReActAgent(config)

	// Use provided cache config or create default
	if cacheConfig == nil {
		defaultConfig := performance.DefaultCacheConfig()
		cacheConfig = &defaultConfig
		// ReAct reasoning patterns can be cached for moderate durations
		cacheConfig.TTL = 5 * time.Minute
		cacheConfig.MaxSize = 500
	}

	return performance.NewCachedAgent(agent, *cacheConfig)
}

// StreamMode 标记 ReAct 输出推理步骤，而非模型逐 Token 增量。
func (r *ReActAgent) StreamMode() agentcore.StreamMode { return agentcore.StreamModeSteps }
