package cot

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/kart-io/goagent/agents/base"
	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// 预编译正则表达式，避免每次调用都重新编译
var digitOnlyRegex = regexp.MustCompile(`^\d+$`)

// CoTAgent implements Chain-of-Thought reasoning pattern.
//
// Chain-of-Thought (CoT) prompts the model to break down complex problems
// into intermediate reasoning steps, making the problem-solving process
// more transparent and accurate. This agent:
// - Encourages step-by-step reasoning
// - Shows intermediate calculations and logic
// - Improves accuracy on complex tasks
// - Provides interpretable reasoning traces
type CoTAgent struct {
	*base.BaseReasoningAgent
	config CoTConfig
}

// CoTConfig configuration for Chain-of-Thought agent
type CoTConfig struct {
	Name        string            // Agent name
	Description string            // Agent description
	LLM         llm.Client        // LLM client
	Tools       []interfaces.Tool // Available tools (optional)
	MaxSteps    int               // Maximum reasoning steps

	// CoT-specific settings
	ShowStepNumbers      bool   // Show step numbers in reasoning
	RequireJustification bool   // Require justification for each step
	FinalAnswerFormat    string // Format for final answer
	ExampleFormat        string // Example CoT format to show model

	// Prompting strategy
	ZeroShot        bool         // Use zero-shot CoT ("Let's think step by step")
	FewShot         bool         // Use few-shot CoT with examples
	FewShotExamples []CoTExample // Examples for few-shot learning
}

// CoTExample represents an example for few-shot Chain-of-Thought
type CoTExample struct {
	Question string
	Steps    []string
	Answer   string
}

// CoTStrategy CoT推理策略实现
type CoTStrategy struct {
	config CoTConfig
}

// NewCoTAgent creates a new Chain-of-Thought agent
func NewCoTAgent(config CoTConfig) *CoTAgent {
	if config.MaxSteps <= 0 {
		config.MaxSteps = 10
	}

	if config.FinalAnswerFormat == "" {
		config.FinalAnswerFormat = "Therefore, the final answer is:"
	}

	capabilities := []string{"chain_of_thought", "step_by_step", "reasoning"}
	if len(config.Tools) > 0 {
		capabilities = append(capabilities, "tool_calling")
	}

	strategy := &CoTStrategy{config: config}

	baseAgent := base.NewBaseReasoningAgent(
		config.Name,
		config.Description,
		capabilities,
		config.LLM,
		config.Tools,
		strategy,
	)

	return &CoTAgent{
		BaseReasoningAgent: baseAgent,
		config:             config,
	}
}

// Execute 实现ReasoningStrategy接口
func (s *CoTStrategy) Execute(
	ctx context.Context,
	input *agentcore.AgentInput,
	llmClient llm.Client,
	tools []interfaces.Tool,
	toolsByName map[string]interfaces.Tool,
	output *agentcore.AgentOutput,
) (result interface{}, err error) {
	// 构建CoT prompt
	prompt := s.buildCoTPrompt(input)

	// 初始化推理步骤
	reasoningSteps := make([]string, 0)

	// 调用LLM
	messages := []llm.Message{
		llm.SystemMessage(s.getSystemPrompt()),
		llm.UserMessage(prompt),
	}

	llmResp, err := llmClient.Chat(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	// 收集token使用量
	if llmResp.Usage != nil {
		output.TokenUsage.Add(llmResp.Usage)
	}

	// 解析CoT响应
	response := llmResp.Content
	steps, finalAnswer := s.parseCoTResponse(response)

	// 记录推理步骤
	for i, step := range steps {
		output.Steps = append(output.Steps, agentcore.AgentStep{
			Step:        i + 1,
			Action:      "Reasoning",
			Description: fmt.Sprintf("Step %d", i+1),
			Result:      step,
			Duration:    time.Millisecond * 100,
			Success:     true,
		})
		reasoningSteps = append(reasoningSteps, step)
	}

	// 如果工具可用且需要，执行工具
	if len(tools) > 0 {
		toolResults := s.executeToolsIfNeeded(ctx, steps, toolsByName, output)
		if len(toolResults) > 0 {
			// 使用工具结果重新推理
			toolContext := s.formatToolResults(toolResults)
			messages = append(messages, llm.AssistantMessage(response))
			messages = append(messages, llm.UserMessage(toolContext))

			llmResp2, err := llmClient.Chat(ctx, messages)
			if err == nil {
				// 收集第二次LLM调用的token使用量
				if llmResp2.Usage != nil {
					output.TokenUsage.Add(llmResp2.Usage)
				}

				response = llmResp2.Content
				additionalSteps, newAnswer := s.parseCoTResponse(response)
				if newAnswer != "" {
					finalAnswer = newAnswer
				}
				for i, step := range additionalSteps {
					output.Steps = append(output.Steps, agentcore.AgentStep{
						Step:        len(steps) + i + 1,
						Action:      "Reasoning with Tools",
						Description: fmt.Sprintf("Step %d (with tools)", len(steps)+i+1),
						Result:      step,
						Duration:    time.Millisecond * 100,
						Success:     true,
					})
				}
			}
		}
	}

	// 设置元数据
	output.Metadata["total_steps"] = len(output.Steps)
	output.Metadata["reasoning_trace"] = reasoningSteps

	return finalAnswer, nil
}

// ExecuteWithGenerator 实现Generator模式执行（可选）
func (s *CoTStrategy) ExecuteWithGenerator(
	ctx context.Context,
	input *agentcore.AgentInput,
	llmClient llm.Client,
	tools []interfaces.Tool,
	toolsByName map[string]interfaces.Tool,
	output *agentcore.AgentOutput,
	yield func(*agentcore.AgentOutput, error) bool,
	startTime time.Time,
) (result interface{}, err error) {
	// 构建CoT prompt
	prompt := s.buildCoTPrompt(input)

	// 调用LLM
	messages := []llm.Message{
		llm.SystemMessage(s.getSystemPrompt()),
		llm.UserMessage(prompt),
	}

	llmResp, err := llmClient.Chat(ctx, messages)
	if err != nil {
		return nil, err
	}

	// 收集token使用量
	if llmResp.Usage != nil {
		output.TokenUsage.Add(llmResp.Usage)
	}

	// 解析CoT响应
	response := llmResp.Content
	steps, finalAnswer := s.parseCoTResponse(response)

	// 记录推理步骤
	for i, step := range steps {
		output.Steps = append(output.Steps, agentcore.AgentStep{
			Step:        i + 1,
			Action:      "Reasoning",
			Description: fmt.Sprintf("Step %d", i+1),
			Result:      step,
			Duration:    time.Since(startTime) / time.Duration(len(steps)),
			Success:     true,
		})
	}

	// Yield初始推理完成
	stepOutput := createStepOutput(output, "Initial reasoning completed", startTime)
	stepOutput.Status = interfaces.StatusInProgress
	stepOutput.Metadata["step_type"] = "initial_reasoning"
	stepOutput.Metadata["total_reasoning_steps"] = len(steps)
	if finalAnswer != "" {
		stepOutput.Metadata["has_final_answer"] = true
	}
	if !yield(stepOutput, nil) {
		return finalAnswer, nil // 早期终止
	}

	// 如果工具可用且需要，执行工具
	if len(tools) > 0 {
		toolResults := s.executeToolsIfNeeded(ctx, steps, toolsByName, output)
		if len(toolResults) > 0 {
			// Yield工具执行完成
			toolOutput := createStepOutput(output, "Tools executed", startTime)
			toolOutput.Status = interfaces.StatusInProgress
			toolOutput.Metadata["step_type"] = "tool_execution"
			toolOutput.Metadata["tools_used"] = len(output.ToolCalls)
			if !yield(toolOutput, nil) {
				return finalAnswer, nil
			}

			// 使用工具结果重新推理
			toolContext := s.formatToolResults(toolResults)
			messages = append(messages, llm.AssistantMessage(response))
			messages = append(messages, llm.UserMessage(toolContext))

			llmResp2, err := llmClient.Chat(ctx, messages)
			if err == nil {
				if llmResp2.Usage != nil {
					output.TokenUsage.Add(llmResp2.Usage)
				}

				response = llmResp2.Content
				additionalSteps, newAnswer := s.parseCoTResponse(response)
				if newAnswer != "" {
					finalAnswer = newAnswer
				}

				// 记录额外推理步骤
				for i, step := range additionalSteps {
					output.Steps = append(output.Steps, agentcore.AgentStep{
						Step:        len(steps) + i + 1,
						Action:      "Reasoning with Tools",
						Description: fmt.Sprintf("Step %d (with tools)", len(steps)+i+1),
						Result:      step,
						Duration:    time.Since(startTime) / time.Duration(len(steps)+len(additionalSteps)),
						Success:     true,
					})
				}

				// Yield工具推理完成
				finalReasoningOutput := createStepOutput(output, "Reasoning with tools completed", startTime)
				finalReasoningOutput.Status = interfaces.StatusInProgress
				finalReasoningOutput.Metadata["step_type"] = "reasoning_with_tools"
				finalReasoningOutput.Metadata["additional_steps"] = len(additionalSteps)
				if !yield(finalReasoningOutput, nil) {
					return finalAnswer, nil
				}
			}
		}
	}

	// Yield最终输出
	finalOutput := createStepOutput(output, "Reasoning completed", startTime)
	finalOutput.Status = interfaces.StatusSuccess
	finalOutput.Result = finalAnswer
	finalOutput.Timestamp = time.Now()
	finalOutput.Latency = time.Since(startTime)
	finalOutput.Metadata["step_type"] = "final"
	finalOutput.Metadata["total_steps"] = len(output.Steps)
	finalOutput.Metadata["reasoning_trace"] = steps
	if !yield(finalOutput, nil) {
		return finalAnswer, nil
	}

	return finalAnswer, nil
}

// 辅助方法

func (s *CoTStrategy) buildCoTPrompt(input *agentcore.AgentInput) string {
	var prompt strings.Builder

	// 添加few-shot示例
	if s.config.FewShot && len(s.config.FewShotExamples) > 0 {
		prompt.WriteString("Here are some examples of step-by-step reasoning:\n\n")
		for _, example := range s.config.FewShotExamples {
			prompt.WriteString(fmt.Sprintf("Question: %s\n", example.Question))
			prompt.WriteString("Let's think step by step:\n")
			for i, step := range example.Steps {
				if s.config.ShowStepNumbers {
					prompt.WriteString(fmt.Sprintf("Step %d: %s\n", i+1, step))
				} else {
					prompt.WriteString(fmt.Sprintf("- %s\n", step))
				}
			}
			prompt.WriteString(fmt.Sprintf("%s %s\n\n", s.config.FinalAnswerFormat, example.Answer))
		}
		prompt.WriteString("Now, let's solve this problem:\n\n")
	}

	// 添加实际问题
	prompt.WriteString(fmt.Sprintf("Question: %s\n\n", input.Task))

	// 添加CoT触发器
	if s.config.ZeroShot || !s.config.FewShot {
		prompt.WriteString("Let's think step by step:\n")
	}

	// 添加格式说明
	if s.config.ExampleFormat != "" {
		prompt.WriteString(fmt.Sprintf("\nPlease follow this format:\n%s\n", s.config.ExampleFormat))
	}

	// 添加要求展示工作过程的说明
	if s.config.RequireJustification {
		prompt.WriteString("\nFor each step, provide clear justification and show all work.\n")
	}

	// 添加最终答案格式提醒
	prompt.WriteString(fmt.Sprintf("\nEnd with: %s [your final answer]\n", s.config.FinalAnswerFormat))

	return prompt.String()
}

func (s *CoTStrategy) getSystemPrompt() string {
	prompt := `You are an expert problem solver that uses Chain-of-Thought reasoning.
Break down complex problems into clear, logical steps.
Show your work and reasoning at each step.
Be systematic and thorough in your analysis.`

	if s.config.ShowStepNumbers {
		prompt += "\nNumber each step clearly."
	}

	if s.config.RequireJustification {
		prompt += "\nProvide justification for each reasoning step."
	}

	return prompt
}

func (s *CoTStrategy) parseCoTResponse(response string) ([]string, string) {
	lines := strings.Split(response, "\n")
	steps := make([]string, 0)
	finalAnswer := ""

	currentStep := strings.Builder{}
	inStep := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 检查最终答案
		if strings.Contains(line, s.config.FinalAnswerFormat) {
			// 保存当前步骤
			if currentStep.Len() > 0 {
				steps = append(steps, strings.TrimSpace(currentStep.String()))
			}
			parts := strings.SplitN(line, s.config.FinalAnswerFormat, 2)
			if len(parts) > 1 {
				finalAnswer = strings.TrimSpace(parts[1])
			}
			break
		}

		// 检测步骤头部
		lowerLine := strings.ToLower(line)
		isStepHeader := strings.Contains(lowerLine, "step") &&
			(strings.Contains(line, ":") || strings.HasPrefix(line, "**") || strings.HasPrefix(line, "-"))

		if isStepHeader {
			// 保存前一步骤
			if currentStep.Len() > 0 {
				steps = append(steps, strings.TrimSpace(currentStep.String()))
				currentStep.Reset()
			}
			inStep = true

			// 提取步骤标题和内容
			cleanLine := strings.TrimPrefix(line, "**")
			cleanLine = strings.TrimSuffix(cleanLine, "**")
			cleanLine = strings.TrimPrefix(cleanLine, "- ")
			currentStep.WriteString(cleanLine)
			currentStep.WriteString(" ")
		} else if inStep {
			// 跳过空行和LaTeX分隔符
			if line == "\\[" || line == "\\]" {
				continue
			}
			// 跳过纯数字行
			if digitOnlyRegex.MatchString(line) {
				continue
			}
			// 跳过纯LaTeX公式
			if strings.HasPrefix(line, "\\frac") || strings.HasPrefix(line, "\\quad") ||
				strings.HasPrefix(line, "\\text") {
				continue
			}
			// 跳过"Question:"和"Let's"行
			if strings.HasPrefix(lowerLine, "question:") ||
				strings.HasPrefix(lowerLine, "let's") {
				continue
			}

			// 收集当前步骤内容
			currentStep.WriteString(line)
			currentStep.WriteString(" ")
		}
	}

	// 保存最后一步
	if currentStep.Len() > 0 {
		steps = append(steps, strings.TrimSpace(currentStep.String()))
	}

	// 如果没有找到结构化步骤，尝试备用解析
	if len(steps) == 0 && finalAnswer == "" {
		paragraphs := strings.Split(response, "\n\n")
		for _, para := range paragraphs {
			para = strings.TrimSpace(para)
			if para != "" && !strings.HasPrefix(strings.ToLower(para), "question") &&
				!strings.HasPrefix(strings.ToLower(para), "let's") {
				steps = append(steps, para)
			}
		}

		// 最后一段可能是答案
		if len(steps) > 0 {
			lastStep := steps[len(steps)-1]
			if strings.Contains(strings.ToLower(lastStep), "answer") ||
				strings.Contains(strings.ToLower(lastStep), "conclusion") {
				finalAnswer = lastStep
				steps = steps[:len(steps)-1]
			}
		}
	}

	return steps, finalAnswer
}

func (s *CoTStrategy) executeToolsIfNeeded(ctx context.Context, steps []string, toolsByName map[string]interfaces.Tool, output *agentcore.AgentOutput) map[string]interface{} {
	toolResults := make(map[string]interface{})

	for _, step := range steps {
		// 检查步骤是否提到需要工具
		if strings.Contains(step, "USE_TOOL:") {
			parts := strings.SplitN(step, "USE_TOOL:", 2)
			if len(parts) > 1 {
				toolRequest := strings.TrimSpace(parts[1])
				// 解析工具名称和输入
				toolParts := strings.SplitN(toolRequest, " ", 2)
				toolName := toolParts[0]

				var toolInput map[string]interface{}
				if len(toolParts) > 1 {
					toolInput = map[string]interface{}{
						"query": toolParts[1],
					}
				}

				// 执行工具
				if tool, exists := toolsByName[toolName]; exists {
					toolIn := &interfaces.ToolInput{
						Args:    toolInput,
						Context: ctx,
					}

					startTime := time.Now()
					result, err := tool.Invoke(ctx, toolIn)

					toolCall := agentcore.AgentToolCall{
						ToolName: toolName,
						Input:    toolInput,
						Duration: time.Since(startTime),
						Success:  err == nil,
					}

					if err != nil {
						toolCall.Error = err.Error()
					} else {
						toolCall.Output = result.Result
						toolResults[toolName] = result.Result
					}

					output.ToolCalls = append(output.ToolCalls, toolCall)
				}
			}
		}
	}

	return toolResults
}

func (s *CoTStrategy) formatToolResults(results map[string]interface{}) string {
	if len(results) == 0 {
		return ""
	}

	var formatted strings.Builder
	formatted.WriteString("Tool execution results:\n")
	for toolName, result := range results {
		formatted.WriteString(fmt.Sprintf("- %s: %v\n", toolName, result))
	}
	formatted.WriteString("\nPlease continue your reasoning with these results.")

	return formatted.String()
}

// createStepOutput 创建步骤输出快照
func createStepOutput(accumulated *agentcore.AgentOutput, message string, startTime time.Time) *agentcore.AgentOutput {
	stepOutput := &agentcore.AgentOutput{
		Steps: make([]agentcore.AgentStep, len(accumulated.Steps)),
		ToolCalls:      make([]agentcore.AgentToolCall, len(accumulated.ToolCalls)),
		Metadata:       make(map[string]interface{}),
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

	// 复制slices
	copy(stepOutput.Steps, accumulated.Steps)
	copy(stepOutput.ToolCalls, accumulated.ToolCalls)

	// 复制metadata
	for k, v := range accumulated.Metadata {
		stepOutput.Metadata[k] = v
	}

	return stepOutput
}
