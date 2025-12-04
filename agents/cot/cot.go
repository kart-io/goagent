package cot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kart-io/goagent/agents/base"
	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

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
	parser *base.DefaultParser
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

	strategy := &CoTStrategy{
		config: config,
		parser: base.GetDefaultParser(),
	}

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
	// 使用解析器解析步骤
	steps := s.parser.ParseSteps(response)

	// 使用解析器解析答案
	finalAnswer := s.parser.ParseAnswer(response)

	// 如果配置了自定义最终答案格式，也检查它
	if finalAnswer == "" && s.config.FinalAnswerFormat != "" {
		lines := strings.Split(response, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.Contains(line, s.config.FinalAnswerFormat) {
				parts := strings.SplitN(line, s.config.FinalAnswerFormat, 2)
				if len(parts) > 1 {
					finalAnswer = strings.TrimSpace(parts[1])
					break
				}
			}
		}
	}

	// 如果没有找到结构化步骤，尝试备用解析
	if len(steps) == 0 && finalAnswer == "" {
		paragraphs := strings.Split(response, "\n\n")
		for _, para := range paragraphs {
			para = strings.TrimSpace(para)
			if para != "" && !s.isSkippableParagraph(para) {
				steps = append(steps, para)
			}
		}

		// 最后一段可能是答案
		if len(steps) > 0 {
			lastStep := steps[len(steps)-1]
			if s.isAnswerParagraph(lastStep) {
				finalAnswer = lastStep
				steps = steps[:len(steps)-1]
			}
		}
	}

	// 如果仍然没有找到最终答案，但有步骤，使用最后一步作为答案
	if finalAnswer == "" && len(steps) > 0 {
		finalAnswer = steps[len(steps)-1]
	}

	// 如果完全没有解析出结果，返回原始响应作为答案
	if finalAnswer == "" && len(steps) == 0 {
		finalAnswer = response
	}

	return steps, finalAnswer
}

// isStepHeader 检测是否是步骤头部（委托给解析器）
func (s *CoTStrategy) isStepHeader(line string) bool {
	isStep, _ := s.parser.IsStepLine(line)
	return isStep
}

// isAnswerLine 检测是否是答案行（委托给解析器）
func (s *CoTStrategy) isAnswerLine(line string) bool {
	// 首先检查配置的自定义格式
	if s.config.FinalAnswerFormat != "" && strings.Contains(line, s.config.FinalAnswerFormat) {
		return true
	}
	return s.parser.IsAnswerLine(line)
}

// extractAnswer 从答案行中提取答案内容（委托给解析器）
func (s *CoTStrategy) extractAnswer(line string) string {
	// 首先尝试从配置的格式中提取
	if s.config.FinalAnswerFormat != "" && strings.Contains(line, s.config.FinalAnswerFormat) {
		parts := strings.SplitN(line, s.config.FinalAnswerFormat, 2)
		if len(parts) > 1 {
			return strings.TrimSpace(parts[1])
		}
	}
	return s.parser.ExtractAnswerContent(line)
}

// isSkippableParagraph 检测是否应该跳过的段落
func (s *CoTStrategy) isSkippableParagraph(para string) bool {
	lowerPara := strings.ToLower(para)

	// 英文
	if strings.HasPrefix(lowerPara, "question") || strings.HasPrefix(lowerPara, "let's") {
		return true
	}

	// 中文
	if strings.HasPrefix(para, "问题") || strings.HasPrefix(para, "让我们") {
		return true
	}

	return false
}

// isAnswerParagraph 检测段落是否包含答案
func (s *CoTStrategy) isAnswerParagraph(para string) bool {
	lowerPara := strings.ToLower(para)

	// 英文
	if strings.Contains(lowerPara, "answer") || strings.Contains(lowerPara, "conclusion") ||
		strings.Contains(lowerPara, "therefore") || strings.Contains(lowerPara, "thus") {
		return true
	}

	// 中文
	if strings.Contains(para, "答案") || strings.Contains(para, "结论") ||
		strings.Contains(para, "因此") || strings.Contains(para, "所以") ||
		strings.Contains(para, "综上") || strings.Contains(para, "总结") {
		return true
	}

	return false
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

	// 复制slices
	copy(stepOutput.Steps, accumulated.Steps)
	copy(stepOutput.ToolCalls, accumulated.ToolCalls)

	// 复制metadata
	for k, v := range accumulated.Metadata {
		stepOutput.Metadata[k] = v
	}

	return stepOutput
}
