package react

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/parsers"
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
	llm          llm.Client
	tools        []interfaces.Tool
	toolsByName  map[string]interfaces.Tool
	parser       *parsers.ReActOutputParser
	maxSteps     int
	stopPattern  []string
	promptPrefix string
	promptSuffix string
	formatInstr  string
}

// ReActConfig ReAct Agent 配置
type ReActConfig struct {
	Name         string            // Agent 名称
	Description  string            // Agent 描述
	LLM          llm.Client        // LLM 客户端
	Tools        []interfaces.Tool // 可用工具列表
	MaxSteps     int               // 最大步数
	StopPattern  []string          // 停止模式
	PromptPrefix string            // Prompt 前缀
	PromptSuffix string            // Prompt 后缀
	FormatInstr  string            // 格式说明
}

// NewReActAgent 创建 ReAct Agent
func NewReActAgent(config ReActConfig) *ReActAgent {
	if config.MaxSteps <= 0 {
		config.MaxSteps = 10
	}

	if len(config.StopPattern) == 0 {
		config.StopPattern = []string{"Final Answer:"}
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
		BaseAgent:    agentcore.NewBaseAgent(config.Name, config.Description, capabilities),
		llm:          config.LLM,
		tools:        config.Tools,
		toolsByName:  toolsByName,
		parser:       parser,
		maxSteps:     config.MaxSteps,
		stopPattern:  config.StopPattern,
		promptPrefix: config.PromptPrefix,
		promptSuffix: config.PromptSuffix,
		formatInstr:  config.FormatInstr,
	}

	return agent
}

// Invoke 执行 ReAct Agent
func (r *ReActAgent) Invoke(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	startTime := time.Now()

	// 触发开始回调
	if err := r.triggerOnStart(ctx, input); err != nil {
		return nil, err
	}

	// 构建初始 prompt
	prompt := r.buildPrompt(input)

	// 初始化输出
	output := &agentcore.AgentOutput{
		ReasoningSteps: make([]agentcore.ReasoningStep, 0),
		ToolCalls:      make([]agentcore.ToolCall, 0),
		Metadata:       make(map[string]interface{}),
	}

	// ReAct 循环
	var finalAnswer string
	scratchpad := ""

	for step := 0; step < r.maxSteps; step++ {
		// 构建当前输入
		currentPrompt := prompt + scratchpad

		// 调用 LLM
		llmStart := time.Now()
		if err := r.triggerOnLLMStart(ctx, []string{currentPrompt}); err != nil {
			return nil, err
		}

		// 使用 LLM Chat 接口
		messages := []llm.Message{
			llm.UserMessage(currentPrompt),
		}

		llmResp, err := r.llm.Chat(ctx, messages)
		if err != nil {
			_ = r.triggerOnLLMError(ctx, err)
			return r.handleError(ctx, output, step, "LLM call failed", err, startTime)
		}

		llmOutput := llmResp.Content

		if err := r.triggerOnLLMEnd(ctx, llmOutput, llmResp.TokensUsed); err != nil {
			return nil, err
		}

		// 解析 LLM 输出
		parsed, err := r.parser.Parse(ctx, llmOutput)
		if err != nil {
			return r.handleError(ctx, output, step, "Failed to parse LLM output", err, startTime)
		}

		// 检查是否得到最终答案
		if parsed.FinalAnswer != "" {
			finalAnswer = parsed.FinalAnswer
			output.ReasoningSteps = append(output.ReasoningSteps, agentcore.ReasoningStep{
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
			return r.handleError(ctx, output, step, "No action specified", fmt.Errorf("empty action"), startTime)
		}

		// 记录思考步骤
		output.ReasoningSteps = append(output.ReasoningSteps, agentcore.ReasoningStep{
			Step:        step + 1,
			Action:      "Thought",
			Description: thought,
			Result:      "",
			Duration:    time.Since(llmStart),
			Success:     true,
		})

		// 执行工具
		toolStart := time.Now()
		observation, toolErr := r.executeTool(ctx, action, actionInput)

		// 记录工具调用
		toolCall := agentcore.ToolCall{
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
		output.ReasoningSteps = append(output.ReasoningSteps, agentcore.ReasoningStep{
			Step:        step + 1,
			Action:      "Action",
			Description: fmt.Sprintf("Tool: %s", action),
			Result:      fmt.Sprintf("%v", observation),
			Duration:    time.Since(toolStart),
			Success:     toolErr == nil,
			Error:       toolCall.Error,
		})

		// 更新 scratchpad
		scratchpad += fmt.Sprintf("\nThought: %s\nAction: %s\nAction Input: %v\nObservation: %v\n",
			thought, action, actionInput, observation)

		// 检查是否达到停止条件
		if r.shouldStop(llmOutput) {
			break
		}
	}

	// 构建最终输出
	if finalAnswer != "" {
		output.Status = "success"
		output.Result = finalAnswer
		output.Message = "Task completed successfully"
	} else {
		output.Status = "partial"
		output.Result = scratchpad
		output.Message = fmt.Sprintf("Reached max steps (%d) without final answer", r.maxSteps)
	}

	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)
	output.Metadata["steps"] = len(output.ReasoningSteps)
	output.Metadata["tool_calls"] = len(output.ToolCalls)

	// 触发完成回调
	if err := r.triggerOnFinish(ctx, output); err != nil {
		return nil, err
	}

	return output, nil
}

// Stream 流式执行 ReAct Agent
func (r *ReActAgent) Stream(ctx context.Context, input *agentcore.AgentInput) (<-chan agentcore.StreamChunk[*agentcore.AgentOutput], error) {
	outChan := make(chan agentcore.StreamChunk[*agentcore.AgentOutput])

	go func() {
		defer close(outChan)

		// 直接调用 Invoke 并将结果包装成流
		output, err := r.Invoke(ctx, input)
		outChan <- agentcore.StreamChunk[*agentcore.AgentOutput]{
			Data:  output,
			Error: err,
			Done:  true,
		}
	}()

	return outChan, nil
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

// executeTool 执行工具
func (r *ReActAgent) executeTool(ctx context.Context, toolName string, input map[string]interface{}) (interface{}, error) {
	tool, ok := r.toolsByName[toolName]
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", toolName)
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
		_ = r.triggerOnToolError(ctx, toolName, err)
		return nil, err
	}

	if err := r.triggerOnToolEnd(ctx, toolName, output.Result); err != nil {
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

// handleError 处理错误
func (r *ReActAgent) handleError(ctx context.Context, output *agentcore.AgentOutput, step int, message string, err error, startTime time.Time) (*agentcore.AgentOutput, error) {
	output.Status = "failed"
	output.Message = message
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)
	output.ReasoningSteps = append(output.ReasoningSteps, agentcore.ReasoningStep{
		Step:    step + 1,
		Action:  "Error",
		Success: false,
		Error:   err.Error(),
	})

	_ = r.triggerOnError(ctx, err)
	return output, err
}

// 回调触发辅助方法
func (r *ReActAgent) triggerOnStart(ctx context.Context, input *agentcore.AgentInput) error {
	config := r.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnStart(ctx, input); err != nil {
			return err
		}
	}
	return nil
}

func (r *ReActAgent) triggerOnFinish(ctx context.Context, output *agentcore.AgentOutput) error {
	config := r.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnAgentFinish(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (r *ReActAgent) triggerOnError(ctx context.Context, err error) error {
	config := r.GetConfig()
	for _, cb := range config.Callbacks {
		if cbErr := cb.OnError(ctx, err); cbErr != nil {
			return cbErr
		}
	}
	return nil
}

func (r *ReActAgent) triggerOnLLMStart(ctx context.Context, prompts []string) error {
	config := r.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnLLMStart(ctx, prompts, ""); err != nil {
			return err
		}
	}
	return nil
}

func (r *ReActAgent) triggerOnLLMEnd(ctx context.Context, output string, tokenUsage int) error {
	config := r.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnLLMEnd(ctx, output, tokenUsage); err != nil {
			return err
		}
	}
	return nil
}

func (r *ReActAgent) triggerOnLLMError(ctx context.Context, err error) error {
	config := r.GetConfig()
	for _, cb := range config.Callbacks {
		if cbErr := cb.OnLLMError(ctx, err); cbErr != nil {
			return cbErr
		}
	}
	return nil
}

func (r *ReActAgent) triggerOnToolStart(ctx context.Context, toolName string, input interface{}) error {
	config := r.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnToolStart(ctx, toolName, input); err != nil {
			return err
		}
	}
	return nil
}

func (r *ReActAgent) triggerOnToolEnd(ctx context.Context, toolName string, output interface{}) error {
	config := r.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnToolEnd(ctx, toolName, output); err != nil {
			return err
		}
	}
	return nil
}

func (r *ReActAgent) triggerOnToolError(ctx context.Context, toolName string, err error) error {
	config := r.GetConfig()
	for _, cb := range config.Callbacks {
		if cbErr := cb.OnToolError(ctx, toolName, err); cbErr != nil {
			return cbErr
		}
	}
	return nil
}

// 默认 ReAct Prompts
const defaultReActPromptPrefix = `Answer the following questions as best you can. You have access to the following tools:

{tools}

Use the following format:

{format_instructions}

Begin!`

const defaultReActPromptSuffix = `Question: {input}
Thought:`

const defaultReActFormatInstructions = `Thought: you should always think about what to do
Action: the action to take, should be one of [{tool_names}]
Action Input: the input to the action
Observation: the result of the action
... (this Thought/Action/Action Input/Observation can repeat N times)
Thought: I now know the final answer
Final Answer: the final answer to the original input question`
