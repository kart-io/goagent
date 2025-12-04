package metacot

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

// MetaCoTAgent implements Meta Chain-of-Thought and Self-Ask reasoning patterns.
//
// Meta-CoT / Self-Ask enables the agent to:
// - Generate follow-up questions to clarify understanding
// - Decompose complex questions into simpler sub-questions
// - Verify its own reasoning through self-critique
// - Recursively answer sub-questions to build up to the final answer
// - Maintain awareness of what it knows and doesn't know
type MetaCoTAgent struct {
	*agentcore.BaseAgent
	llm         llm.Client
	tools       []interfaces.Tool
	toolsByName map[string]interfaces.Tool
	config      MetaCoTConfig
	parser      *base.DefaultParser
}

// MetaCoTConfig configuration for Meta-CoT / Self-Ask agent
type MetaCoTConfig struct {
	Name        string            // Agent name
	Description string            // Agent description
	LLM         llm.Client        // LLM client
	Tools       []interfaces.Tool // Available tools (optional)

	// Self-questioning settings
	MaxQuestions    int  // Maximum number of follow-up questions
	MaxDepth        int  // Maximum recursion depth for sub-questions
	AutoDecompose   bool // Automatically decompose complex questions
	RequireEvidence bool // Require evidence for answers
	SelfCritique    bool // Enable self-critique of answers

	// Question generation strategy
	QuestionStrategy string // "focused", "broad", "critical"

	// Verification settings
	VerifyAnswers       bool    // Verify answers through additional questioning
	ConfidenceThreshold float64 // Minimum confidence for accepting an answer
}

// Question represents a question in the self-ask process
type Question struct {
	ID           string
	Text         string
	Type         string // "main", "followup", "verification", "decomposed"
	ParentID     string
	Answer       string
	Confidence   float64
	Evidence     []string
	SubQuestions []*Question
	Status       string // "pending", "answered", "verified"
}

// NewMetaCoTAgent creates a new Meta-CoT / Self-Ask agent
func NewMetaCoTAgent(config MetaCoTConfig) *MetaCoTAgent {
	if config.MaxQuestions <= 0 {
		config.MaxQuestions = 5
	}
	if config.MaxDepth <= 0 {
		config.MaxDepth = 3
	}
	if config.QuestionStrategy == "" {
		config.QuestionStrategy = "focused"
	}
	if config.ConfidenceThreshold == 0 {
		config.ConfidenceThreshold = 0.7
	}

	// Build tools map
	toolsByName := make(map[string]interfaces.Tool)
	for _, tool := range config.Tools {
		toolsByName[tool.Name()] = tool
	}

	capabilities := []string{"meta_reasoning", "self_ask", "question_decomposition", "self_critique"}
	if len(config.Tools) > 0 {
		capabilities = append(capabilities, "tool_calling")
	}

	return &MetaCoTAgent{
		BaseAgent:   agentcore.NewBaseAgent(config.Name, config.Description, capabilities),
		llm:         config.LLM,
		tools:       config.Tools,
		toolsByName: toolsByName,
		config:      config,
		parser:      base.GetDefaultParser(),
	}
}

// InvokeFast 快速执行 Meta-CoT / Self-Ask 推理（绕过回调）
//
// 用于热路径优化，跳过回调直接执行
// 性能提升：避免回调遍历开销
//
// 注意：此方法不会触发任何回调（OnStart/OnFinish等）
//
//go:inline
func (m *MetaCoTAgent) InvokeFast(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	startTime := time.Now()

	// Initialize output
	output := &agentcore.AgentOutput{
		Steps:     make([]agentcore.AgentStep, 0),
		ToolCalls: make([]agentcore.AgentToolCall, 0),
		Metadata:  make(map[string]interface{}),
	}

	// Create main question
	mainQuestion := &Question{
		ID:     "main",
		Text:   input.Task,
		Type:   "main",
		Status: "pending",
	}

	// Phase 1: Question decomposition (if needed)
	if m.shouldDecompose(mainQuestion.Text) {
		subQuestions := m.decomposeQuestion(ctx, mainQuestion, output)
		mainQuestion.SubQuestions = subQuestions

		output.Steps = append(output.Steps, agentcore.AgentStep{
			Step:        1,
			Action:      "Decompose Question",
			Description: fmt.Sprintf("Decomposed into %d sub-questions", len(subQuestions)),
			Result:      m.formatQuestions(subQuestions),
			Duration:    time.Since(startTime),
			Success:     true,
		})
	}

	// Phase 2: Self-ask process
	questionTree := m.buildQuestionTree(mainQuestion)
	err := m.processSelfAsk(ctx, questionTree, 0, output)
	if err != nil {
		output.Status = interfaces.StatusFailed
		output.Message = "Self-ask process failed: " + err.Error()
		output.Timestamp = time.Now()
		output.Latency = time.Since(startTime)
		return output, err
	}

	// Phase 3: Synthesize final answer
	finalAnswer := m.synthesizeAnswer(ctx, questionTree, output)

	// Phase 4: Self-critique (if enabled)
	if m.config.SelfCritique {
		critiqueStart := time.Now()
		critique := m.selfCritique(ctx, input.Task, finalAnswer)

		output.Steps = append(output.Steps, agentcore.AgentStep{
			Step:        len(output.Steps) + 1,
			Action:      "Self-Critique",
			Description: "Critically evaluate the answer",
			Result:      critique,
			Duration:    time.Since(critiqueStart),
			Success:     true,
		})

		// Refine answer if critique suggests improvements
		if m.needsRefinement(critique) {
			finalAnswer = m.refineAnswer(ctx, finalAnswer, critique)
		}
	}

	// Set final output
	output.Status = interfaces.StatusSuccess
	output.Result = finalAnswer
	output.Message = "Meta-CoT / Self-Ask reasoning completed"
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)

	// Add metadata
	output.Metadata["total_questions"] = m.countQuestions(questionTree)
	output.Metadata["max_depth"] = m.getMaxDepth(questionTree)
	output.Metadata["self_critique"] = m.config.SelfCritique

	return output, nil
}

// Invoke executes the Meta-CoT / Self-Ask reasoning
func (m *MetaCoTAgent) Invoke(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	startTime := time.Now()

	// Trigger start callback
	if err := m.triggerOnStart(ctx, input); err != nil {
		return nil, err
	}

	// Initialize output
	output := &agentcore.AgentOutput{
		Steps:     make([]agentcore.AgentStep, 0),
		ToolCalls: make([]agentcore.AgentToolCall, 0),
		Metadata:  make(map[string]interface{}),
	}

	// Create main question
	mainQuestion := &Question{
		ID:     "main",
		Text:   input.Task,
		Type:   "main",
		Status: "pending",
	}

	// Phase 1: Question decomposition (if needed)
	if m.shouldDecompose(mainQuestion.Text) {
		subQuestions := m.decomposeQuestion(ctx, mainQuestion, output)
		mainQuestion.SubQuestions = subQuestions

		output.Steps = append(output.Steps, agentcore.AgentStep{
			Step:        1,
			Action:      "Decompose Question",
			Description: fmt.Sprintf("Decomposed into %d sub-questions", len(subQuestions)),
			Result:      m.formatQuestions(subQuestions),
			Duration:    time.Since(startTime),
			Success:     true,
		})
	}

	// Phase 2: Self-ask process
	questionTree := m.buildQuestionTree(mainQuestion)
	err := m.processSelfAsk(ctx, questionTree, 0, output)
	if err != nil {
		return m.handleError(ctx, output, "Self-ask process failed", err, startTime)
	}

	// Phase 3: Synthesize final answer
	finalAnswer := m.synthesizeAnswer(ctx, questionTree, output)

	// Phase 4: Self-critique (if enabled)
	if m.config.SelfCritique {
		critiqueStart := time.Now()
		critique := m.selfCritique(ctx, input.Task, finalAnswer)

		output.Steps = append(output.Steps, agentcore.AgentStep{
			Step:        len(output.Steps) + 1,
			Action:      "Self-Critique",
			Description: "Critically evaluate the answer",
			Result:      critique,
			Duration:    time.Since(critiqueStart),
			Success:     true,
		})

		// Refine answer if critique suggests improvements
		if m.needsRefinement(critique) {
			finalAnswer = m.refineAnswer(ctx, finalAnswer, critique)
		}
	}

	// Set final output
	output.Status = "success"
	output.Result = finalAnswer
	output.Message = "Meta-CoT / Self-Ask reasoning completed"
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)

	// Add metadata
	output.Metadata["total_questions"] = m.countQuestions(questionTree)
	output.Metadata["max_depth"] = m.getMaxDepth(questionTree)
	output.Metadata["self_critique"] = m.config.SelfCritique

	// Trigger finish callback
	if err := m.triggerOnFinish(ctx, output); err != nil {
		return nil, err
	}

	return output, nil
}

// processSelfAsk processes the self-ask questioning recursively
func (m *MetaCoTAgent) processSelfAsk(ctx context.Context, question *Question, depth int, output *agentcore.AgentOutput) error {
	// Check depth limit
	if depth >= m.config.MaxDepth {
		return m.answerDirectly(ctx, question, output)
	}

	// Generate follow-up questions
	followupQuestions := m.generateFollowupQuestions(ctx, question, output)

	// Process each follow-up question
	for _, fq := range followupQuestions {
		// Check if we need to search for information
		if m.needsExternalInfo(fq) && len(m.tools) > 0 {
			m.searchForAnswer(ctx, fq, output)
		} else {
			// Recursively process sub-question
			if err := m.processSelfAsk(ctx, fq, depth+1, output); err != nil {
				return err
			}
		}

		// Record step
		output.Steps = append(output.Steps, agentcore.AgentStep{
			Step:        len(output.Steps) + 1,
			Action:      fmt.Sprintf("Self-Ask (depth=%d)", depth),
			Description: fq.Text,
			Result:      fq.Answer,
			Duration:    time.Millisecond * 100, // Approximate
			Success:     fq.Status == "answered",
		})
	}

	// Answer the current question using follow-up answers
	return m.answerWithContext(ctx, question, followupQuestions, output)
}

// generateFollowupQuestions generates follow-up questions
func (m *MetaCoTAgent) generateFollowupQuestions(ctx context.Context, question *Question, output *agentcore.AgentOutput) []*Question {
	prompt := m.buildFollowupPrompt(question)

	messages := []llm.Message{
		llm.SystemMessage("You are an expert at asking clarifying and follow-up questions to better understand and solve problems."),
		llm.UserMessage(prompt),
	}

	llmResp, err := m.llm.Chat(ctx, messages)
	if err != nil {
		return nil
	}

	// Parse follow-up questions
	questions := m.parseFollowupQuestions(llmResp.Content, question.ID)

	// Limit number of questions
	if len(questions) > m.config.MaxQuestions {
		questions = questions[:m.config.MaxQuestions]
	}

	return questions
}

// buildFollowupPrompt builds prompt for generating follow-up questions
func (m *MetaCoTAgent) buildFollowupPrompt(question *Question) string {
	strategy := ""
	switch m.config.QuestionStrategy {
	case "focused":
		strategy = "Generate focused questions that directly help answer the main question."
	case "broad":
		strategy = "Generate broad questions that explore different aspects of the problem."
	case "critical":
		strategy = "Generate critical questions that challenge assumptions and verify facts."
	default:
		strategy = "Generate helpful follow-up questions."
	}

	return fmt.Sprintf(`Given this question: "%s"

%s

What follow-up questions would help answer this? Generate 2-3 questions.
If the question can be answered directly without follow-ups, respond with "DIRECT_ANSWER".

Format each question on a new line starting with "Q: "`, question.Text, strategy)
}

// parseFollowupQuestions parses follow-up questions from response
func (m *MetaCoTAgent) parseFollowupQuestions(response string, parentID string) []*Question {
	// 使用解析器解析问题
	parsedQuestions := m.parser.ParseQuestions(response, parentID)
	if parsedQuestions == nil {
		return nil
	}

	// 转换为 Question 结构
	questions := make([]*Question, 0, len(parsedQuestions))
	for _, pq := range parsedQuestions {
		questions = append(questions, &Question{
			ID:       pq.ID,
			Text:     pq.Text,
			Type:     pq.Type,
			ParentID: pq.ParentID,
			Status:   "pending",
		})
	}

	// 限制问题数量
	if len(questions) > m.config.MaxQuestions {
		questions = questions[:m.config.MaxQuestions]
	}

	return questions
}

// answerDirectly answers a question directly without follow-ups
func (m *MetaCoTAgent) answerDirectly(ctx context.Context, question *Question, output *agentcore.AgentOutput) error {
	prompt := fmt.Sprintf("Answer this question directly and concisely: %s", question.Text)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	llmResp, err := m.llm.Chat(ctx, messages)
	if err != nil {
		return err
	}

	question.Answer = llmResp.Content
	question.Status = "answered"
	question.Confidence = m.estimateConfidence(llmResp.Content)

	return nil
}

// answerWithContext answers using follow-up question context
func (m *MetaCoTAgent) answerWithContext(ctx context.Context, question *Question, followups []*Question, output *agentcore.AgentOutput) error {
	// Build context from follow-up answers
	var context strings.Builder
	context.WriteString("Based on the following information:\n")

	for _, fq := range followups {
		if fq.Answer != "" {
			context.WriteString(fmt.Sprintf("- %s: %s\n", fq.Text, fq.Answer))
		}
	}

	prompt := fmt.Sprintf(`%s

Now answer the original question: %s`, context.String(), question.Text)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	llmResp, err := m.llm.Chat(ctx, messages)
	if err != nil {
		return err
	}

	question.Answer = llmResp.Content
	question.Status = "answered"
	question.Confidence = m.estimateConfidence(llmResp.Content)

	// Collect evidence
	for _, fq := range followups {
		if fq.Answer != "" {
			question.Evidence = append(question.Evidence, fmt.Sprintf("%s: %s", fq.Text, fq.Answer))
		}
	}

	return nil
}

// decomposeQuestion decomposes a complex question into sub-questions
func (m *MetaCoTAgent) decomposeQuestion(ctx context.Context, question *Question, output *agentcore.AgentOutput) []*Question {
	prompt := fmt.Sprintf(`Decompose this complex question into simpler sub-questions that, when answered together, will provide the complete answer:

Question: %s

Generate 2-4 sub-questions. Format each on a new line starting with "Q: "`, question.Text)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	llmResp, err := m.llm.Chat(ctx, messages)
	if err != nil {
		return nil
	}

	// Parse sub-questions
	subQuestions := m.parseFollowupQuestions(llmResp.Content, question.ID)
	for _, sq := range subQuestions {
		sq.Type = "decomposed"
	}

	return subQuestions
}

// selfCritique critiques the generated answer
func (m *MetaCoTAgent) selfCritique(ctx context.Context, originalQuestion string, answer string) string {
	prompt := fmt.Sprintf(`Critically evaluate this answer to the question:

Question: %s
Answer: %s

Consider:
1. Is the answer complete and accurate?
2. Are there any logical flaws or inconsistencies?
3. What assumptions were made?
4. What could be improved?

Provide a brief critique.`, originalQuestion, answer)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	llmResp, err := m.llm.Chat(ctx, messages)
	if err != nil {
		return "Unable to generate critique"
	}

	return llmResp.Content
}

// refineAnswer refines the answer based on critique
func (m *MetaCoTAgent) refineAnswer(ctx context.Context, answer string, critique string) string {
	prompt := fmt.Sprintf(`Given this answer and critique, provide an improved answer:

Original Answer: %s
Critique: %s

Improved Answer:`, answer, critique)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	llmResp, err := m.llm.Chat(ctx, messages)
	if err != nil {
		return answer // Return original if refinement fails
	}

	return llmResp.Content
}

// synthesizeAnswer synthesizes the final answer from the question tree
func (m *MetaCoTAgent) synthesizeAnswer(ctx context.Context, questionTree *Question, output *agentcore.AgentOutput) string {
	// If main question has sub-questions, synthesize from them
	if len(questionTree.SubQuestions) > 0 {
		var subAnswers strings.Builder
		subAnswers.WriteString("Based on the following sub-question answers:\n\n")

		for i, sq := range questionTree.SubQuestions {
			subAnswers.WriteString(fmt.Sprintf("%d. %s\n   Answer: %s\n\n", i+1, sq.Text, sq.Answer))
		}

		prompt := fmt.Sprintf(`%s

Synthesize a complete answer to: %s`, subAnswers.String(), questionTree.Text)

		messages := []llm.Message{
			llm.UserMessage(prompt),
		}

		llmResp, err := m.llm.Chat(ctx, messages)
		if err != nil {
			return questionTree.Answer
		}

		return llmResp.Content
	}

	return questionTree.Answer
}

// Helper methods

func (m *MetaCoTAgent) shouldDecompose(question string) bool {
	if !m.config.AutoDecompose {
		return false
	}
	// 使用解析器判断是否应该分解
	return m.parser.ShouldDecompose(question)
}

func (m *MetaCoTAgent) needsExternalInfo(question *Question) bool {
	// 使用解析器判断是否需要外部信息
	return m.parser.NeedsExternalInfo(question.Text)
}

func (m *MetaCoTAgent) searchForAnswer(ctx context.Context, question *Question, output *agentcore.AgentOutput) {
	// Use search tool if available
	if searchTool, exists := m.toolsByName["search"]; exists {
		toolInput := &interfaces.ToolInput{
			Args: map[string]interface{}{
				"query": question.Text,
			},
			Context: ctx,
		}

		result, err := searchTool.Invoke(ctx, toolInput)
		if err == nil && result.Success {
			question.Answer = fmt.Sprintf("%v", result.Result)
			question.Status = "answered"
			question.Evidence = append(question.Evidence, "Search result")

			// Record tool call
			output.ToolCalls = append(output.ToolCalls, agentcore.AgentToolCall{
				ToolName: "search",
				Input:    toolInput.Args,
				Output:   result.Result,
				Success:  true,
			})
		}
	}

	// Fallback to direct answer if search fails
	if question.Status != "answered" {
		_ = m.answerDirectly(ctx, question, output)
	}
}

func (m *MetaCoTAgent) buildQuestionTree(mainQuestion *Question) *Question {
	// Build a tree structure from questions and sub-questions
	// This is simplified - in practice, handle more complex relationships
	return mainQuestion
}

func (m *MetaCoTAgent) estimateConfidence(answer string) float64 {
	// 使用解析器估算置信度
	return m.parser.EstimateConfidence(answer)
}

func (m *MetaCoTAgent) needsRefinement(critique string) bool {
	// 使用解析器判断是否需要改进
	return m.parser.NeedsRefinement(critique)
}

func (m *MetaCoTAgent) formatQuestions(questions []*Question) string {
	var formatted strings.Builder

	for i, q := range questions {
		formatted.WriteString(fmt.Sprintf("%d. %s\n", i+1, q.Text))
	}

	return formatted.String()
}

func (m *MetaCoTAgent) countQuestions(root *Question) int {
	count := 1
	for _, sq := range root.SubQuestions {
		count += m.countQuestions(sq)
	}
	return count
}

func (m *MetaCoTAgent) getMaxDepth(root *Question) int {
	if len(root.SubQuestions) == 0 {
		return 0
	}

	maxChildDepth := 0
	for _, sq := range root.SubQuestions {
		depth := m.getMaxDepth(sq)
		if depth > maxChildDepth {
			maxChildDepth = depth
		}
	}

	return maxChildDepth + 1
}

// Stream executes Meta-CoT with streaming
func (m *MetaCoTAgent) Stream(ctx context.Context, input *agentcore.AgentInput) (<-chan agentcore.StreamChunk[*agentcore.AgentOutput], error) {
	outChan := make(chan agentcore.StreamChunk[*agentcore.AgentOutput])

	go func() {
		defer close(outChan)

		output, err := m.Invoke(ctx, input)
		outChan <- agentcore.StreamChunk[*agentcore.AgentOutput]{
			Data:  output,
			Error: err,
			Done:  true,
		}
	}()

	return outChan, nil
}

// RunGenerator 使用 Generator 模式执行 Meta-CoT / Self-Ask（实验性功能）
//
// 相比 Stream，RunGenerator 提供零分配的流式执行，在每个主要阶段后 yield 中间结果：
//   - 问题分解后 yield
//   - 每个自问-自答步骤后 yield
//   - 答案合成后 yield
//   - 自我批评后 yield（如果启用）
//   - 最终输出
//
// 性能优势：
//   - 零内存分配（无 channel、goroutine 开销）
//   - 支持早期终止（用户可以在任意步骤 break）
//   - 更低延迟（无 channel 发送/接收开销）
//
// 使用示例：
//
//	for output, err := range agent.RunGenerator(ctx, input) {
//	    if err != nil {
//	        log.Error("step failed", err)
//	        continue
//	    }
//	    stepType := output.Metadata["step_type"].(string)
//	    if stepType == "question_decomposed" {
//	        fmt.Printf("分解了 %d 个子问题\n", output.Metadata["sub_questions_count"])
//	    }
//	    if output.Status == interfaces.StatusSuccess {
//	        break  // 完成
//	    }
//	}
//
// 注意：此方法不触发 Agent 级别的回调（OnStart/OnFinish）
func (m *MetaCoTAgent) RunGenerator(ctx context.Context, input *agentcore.AgentInput) agentcore.Generator[*agentcore.AgentOutput] {
	return func(yield func(*agentcore.AgentOutput, error) bool) {
		startTime := time.Now()

		// Initialize accumulated output
		accumulated := &agentcore.AgentOutput{
			Steps:     make([]agentcore.AgentStep, 0),
			ToolCalls: make([]agentcore.AgentToolCall, 0),
			Metadata:  make(map[string]interface{}),
		}

		// Create main question
		mainQuestion := &Question{
			ID:     "main",
			Text:   input.Task,
			Type:   "main",
			Status: "pending",
		}

		// Phase 1: Question decomposition (if needed)
		if m.shouldDecompose(mainQuestion.Text) {
			decompositionStart := time.Now()
			subQuestions := m.decomposeQuestion(ctx, mainQuestion, accumulated)
			mainQuestion.SubQuestions = subQuestions

			accumulated.Steps = append(accumulated.Steps, agentcore.AgentStep{
				Step:        1,
				Action:      "Decompose Question",
				Description: fmt.Sprintf("Decomposed into %d sub-questions", len(subQuestions)),
				Result:      m.formatQuestions(subQuestions),
				Duration:    time.Since(decompositionStart),
				Success:     true,
			})

			// Yield after question decomposition
			decompositionOutput := m.createStepOutput(accumulated, "Question decomposed", startTime)
			decompositionOutput.Status = interfaces.StatusInProgress
			decompositionOutput.Metadata["step_type"] = "question_decomposed"
			decompositionOutput.Metadata["sub_questions_count"] = len(subQuestions)
			decompositionOutput.Metadata["sub_questions"] = m.formatQuestions(subQuestions)
			if !yield(decompositionOutput, nil) {
				return // Early termination
			}
		}

		// Phase 2: Self-ask process
		questionTree := m.buildQuestionTree(mainQuestion)
		err := m.processSelfAskGenerator(ctx, questionTree, 0, accumulated, yield, startTime)
		if err != nil {
			errorOutput := m.createStepOutput(accumulated, "Self-ask process failed", startTime)
			errorOutput.Status = interfaces.StatusFailed
			if !yield(errorOutput, err) {
				return
			}
			return
		}

		// Phase 3: Synthesize final answer
		synthesisStart := time.Now()
		finalAnswer := m.synthesizeAnswer(ctx, questionTree, accumulated)

		accumulated.Steps = append(accumulated.Steps, agentcore.AgentStep{
			Step:        len(accumulated.Steps) + 1,
			Action:      "Synthesize Answer",
			Description: "Combine all sub-question answers",
			Result:      "Answer synthesis complete",
			Duration:    time.Since(synthesisStart),
			Success:     true,
		})

		// Yield after answer synthesis
		synthesisOutput := m.createStepOutput(accumulated, "Answer synthesized", startTime)
		synthesisOutput.Status = interfaces.StatusInProgress
		synthesisOutput.Metadata["step_type"] = "answer_synthesized"
		synthesisOutput.Metadata["answer_preview"] = m.truncateText(finalAnswer, 100)
		if !yield(synthesisOutput, nil) {
			return
		}

		// Phase 4: Self-critique (if enabled)
		if m.config.SelfCritique {
			critiqueStart := time.Now()
			critique := m.selfCritique(ctx, input.Task, finalAnswer)

			accumulated.Steps = append(accumulated.Steps, agentcore.AgentStep{
				Step:        len(accumulated.Steps) + 1,
				Action:      "Self-Critique",
				Description: "Critically evaluate the answer",
				Result:      critique,
				Duration:    time.Since(critiqueStart),
				Success:     true,
			})

			// Yield after self-critique
			critiqueOutput := m.createStepOutput(accumulated, "Self-critique completed", startTime)
			critiqueOutput.Status = interfaces.StatusInProgress
			critiqueOutput.Metadata["step_type"] = "self_critique_completed"
			critiqueOutput.Metadata["critique"] = critique
			critiqueOutput.Metadata["needs_refinement"] = m.needsRefinement(critique)
			if !yield(critiqueOutput, nil) {
				return
			}

			// Refine answer if critique suggests improvements
			if m.needsRefinement(critique) {
				refinementStart := time.Now()
				finalAnswer = m.refineAnswer(ctx, finalAnswer, critique)

				accumulated.Steps = append(accumulated.Steps, agentcore.AgentStep{
					Step:        len(accumulated.Steps) + 1,
					Action:      "Refine Answer",
					Description: "Improve answer based on critique",
					Result:      "Answer refinement complete",
					Duration:    time.Since(refinementStart),
					Success:     true,
				})

				// Yield after refinement
				refinementOutput := m.createStepOutput(accumulated, "Answer refined", startTime)
				refinementOutput.Status = interfaces.StatusInProgress
				refinementOutput.Metadata["step_type"] = "answer_refined"
				refinementOutput.Metadata["refined_answer_preview"] = m.truncateText(finalAnswer, 100)
				if !yield(refinementOutput, nil) {
					return
				}
			}
		}

		// Yield final output
		finalOutput := m.createStepOutput(accumulated, "Meta-CoT / Self-Ask reasoning completed", startTime)
		finalOutput.Status = interfaces.StatusSuccess
		finalOutput.Result = finalAnswer
		finalOutput.Metadata["step_type"] = "final"
		finalOutput.Metadata["total_questions"] = m.countQuestions(questionTree)
		finalOutput.Metadata["max_depth"] = m.getMaxDepth(questionTree)
		finalOutput.Metadata["self_critique"] = m.config.SelfCritique
		finalOutput.Metadata["total_duration_ms"] = time.Since(startTime).Milliseconds()
		yield(finalOutput, nil)
	}
}

// processSelfAskGenerator processes the self-ask questioning recursively with generator support
func (m *MetaCoTAgent) processSelfAskGenerator(ctx context.Context, question *Question, depth int, accumulated *agentcore.AgentOutput, yield func(*agentcore.AgentOutput, error) bool, startTime time.Time) error {
	// Check depth limit
	if depth >= m.config.MaxDepth {
		return m.answerDirectly(ctx, question, accumulated)
	}

	// Generate follow-up questions
	followupQuestions := m.generateFollowupQuestions(ctx, question, accumulated)

	// Process each follow-up question
	for i, fq := range followupQuestions {
		// Check if we need to search for information
		if m.needsExternalInfo(fq) && len(m.tools) > 0 {
			m.searchForAnswer(ctx, fq, accumulated)
		} else {
			// Recursively process sub-question
			if err := m.processSelfAskGenerator(ctx, fq, depth+1, accumulated, yield, startTime); err != nil {
				return err
			}
		}

		// Record step
		accumulated.Steps = append(accumulated.Steps, agentcore.AgentStep{
			Step:        len(accumulated.Steps) + 1,
			Action:      fmt.Sprintf("Self-Ask (depth=%d)", depth),
			Description: fq.Text,
			Result:      fq.Answer,
			Duration:    time.Millisecond * 100, // Approximate
			Success:     fq.Status == "answered",
		})

		// Yield after each follow-up question is answered
		followupOutput := m.createStepOutput(accumulated, fmt.Sprintf("Follow-up question %d answered", i+1), startTime)
		followupOutput.Status = interfaces.StatusInProgress
		followupOutput.Metadata["step_type"] = "followup_answered"
		followupOutput.Metadata["depth"] = depth
		followupOutput.Metadata["question"] = fq.Text
		followupOutput.Metadata["answer"] = fq.Answer
		followupOutput.Metadata["question_index"] = i + 1
		followupOutput.Metadata["total_followups"] = len(followupQuestions)
		if !yield(followupOutput, nil) {
			return fmt.Errorf("early termination")
		}
	}

	// Answer the current question using follow-up answers
	return m.answerWithContext(ctx, question, followupQuestions, accumulated)
}

// createStepOutput creates a snapshot of current execution state
func (m *MetaCoTAgent) createStepOutput(accumulated *agentcore.AgentOutput, message string, startTime time.Time) *agentcore.AgentOutput {
	stepOutput := &agentcore.AgentOutput{
		Steps:     make([]agentcore.AgentStep, len(accumulated.Steps)),
		ToolCalls: make([]agentcore.AgentToolCall, len(accumulated.ToolCalls)),
		Metadata:  make(map[string]interface{}),
		Timestamp: time.Now(),
		Latency:   time.Since(startTime),
		Message:   message,
	}

	// Copy slices
	copy(stepOutput.Steps, accumulated.Steps)
	copy(stepOutput.ToolCalls, accumulated.ToolCalls)

	// Copy existing metadata
	for k, v := range accumulated.Metadata {
		stepOutput.Metadata[k] = v
	}

	return stepOutput
}

// truncateText truncates text to maxLen characters
func (m *MetaCoTAgent) truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

// Error handling
func (m *MetaCoTAgent) handleError(ctx context.Context, output *agentcore.AgentOutput, message string, err error, startTime time.Time) (*agentcore.AgentOutput, error) {
	output.Status = "failed"
	output.Message = message
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)

	_ = m.triggerOnError(ctx, err)
	return output, err
}

// Callback triggers
func (m *MetaCoTAgent) triggerOnStart(ctx context.Context, input *agentcore.AgentInput) error {
	config := m.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnStart(ctx, input); err != nil {
			return err
		}
	}
	return nil
}

func (m *MetaCoTAgent) triggerOnFinish(ctx context.Context, output *agentcore.AgentOutput) error {
	config := m.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnAgentFinish(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (m *MetaCoTAgent) triggerOnError(ctx context.Context, err error) error {
	config := m.GetConfig()
	for _, cb := range config.Callbacks {
		if cbErr := cb.OnError(ctx, err); cbErr != nil {
			return cbErr
		}
	}
	return nil
}

// WithCallbacks adds callback handlers
func (m *MetaCoTAgent) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	newAgent := *m
	newAgent.BaseAgent = m.BaseAgent.WithCallbacks(callbacks...).(*agentcore.BaseAgent)
	return &newAgent
}

// WithConfig configures the agent
func (m *MetaCoTAgent) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	newAgent := *m
	newAgent.BaseAgent = m.BaseAgent.WithConfig(config).(*agentcore.BaseAgent)
	return &newAgent
}
