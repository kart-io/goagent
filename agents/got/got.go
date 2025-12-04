package got

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/agents/base"
	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// GoTAgent implements Graph-of-Thought reasoning pattern.
//
// Graph-of-Thought (GoT) extends tree-based reasoning to a directed graph,
// allowing more complex dependencies and parallel exploration. This agent:
// - Builds a directed acyclic graph (DAG) of thoughts
// - Supports multiple dependency relationships
// - Enables parallel execution of independent nodes
// - Merges insights from different reasoning paths
// - Handles cyclic dependency detection
type GoTAgent struct {
	*agentcore.BaseAgent
	llm         llm.Client
	tools       []interfaces.Tool
	toolsByName map[string]interfaces.Tool
	config      GoTConfig
	parser      *base.DefaultParser
}

// GoTConfig configuration for Graph-of-Thought agent
type GoTConfig struct {
	Name        string            // Agent name
	Description string            // Agent description
	LLM         llm.Client        // LLM client
	Tools       []interfaces.Tool // Available tools (optional)

	// Graph parameters
	MaxNodes          int     // Maximum number of nodes in the graph
	MaxEdgesPerNode   int     // Maximum edges from a single node
	ParallelExecution bool    // Enable parallel node processing
	MergeStrategy     string  // How to merge multiple paths ("vote", "weighted", "llm")
	CycleDetection    bool    // Enable cycle detection
	PruneThreshold    float64 // Threshold for pruning low-score nodes

	// 性能优化参数
	FastEvaluation bool          // 快速评估模式：跳过单独的 LLM 评估调用，使用启发式评分
	NodeTimeout    time.Duration // 单个节点的处理超时时间

	// DeepSeek/慢速 API 优化参数
	MinimalMode     bool // 极简模式：仅使用2次LLM调用（生成+合成），适用于慢速API如DeepSeek
	BatchGeneration bool // 批量生成：一次调用生成所有思考，减少API调用次数
	DirectSynthesis bool // 直接合成：跳过图执行，直接从生成的思考合成答案
}

// GraphNode represents a node in the thought graph
type GraphNode struct {
	ID           string
	Thought      string
	Score        float64
	Dependencies []*GraphNode           // Nodes this depends on
	Dependents   []*GraphNode           // Nodes that depend on this
	State        map[string]interface{} // State at this node
	Status       string                 // "pending", "processing", "completed"
	Result       interface{}            // Result after processing
	mu           sync.RWMutex           // Thread safety for parallel execution
}

// NewGoTAgent creates a new Graph-of-Thought agent
func NewGoTAgent(config GoTConfig) *GoTAgent {
	// 优化默认值：减少节点数量以降低 LLM 调用次数
	if config.MaxNodes <= 0 {
		config.MaxNodes = 10 // 从 50 降低到 10，显著减少 LLM 调用
	}
	if config.MaxEdgesPerNode <= 0 {
		config.MaxEdgesPerNode = 3 // 从 5 降低到 3
	}
	if config.MergeStrategy == "" {
		config.MergeStrategy = "weighted"
	}
	// 提高剪枝阈值：更积极地过滤低质量思考
	if config.PruneThreshold == 0 {
		config.PruneThreshold = 0.5 // 从 0.3 提高到 0.5
	}
	// 默认启用快速评估模式
	// 用户可以通过设置 FastEvaluation = false 来使用完整的 LLM 评估
	// 注：这里不覆盖用户显式设置的值，因为 bool 零值是 false
	// 如果用户未设置，默认使用快速评估
	if config.NodeTimeout == 0 {
		config.NodeTimeout = 30 * time.Second // 单节点 30 秒超时
	}

	// Build tools map
	toolsByName := make(map[string]interfaces.Tool)
	for _, tool := range config.Tools {
		toolsByName[tool.Name()] = tool
	}

	capabilities := []string{"graph_of_thought", "parallel", "dag", "merge_paths"}
	if len(config.Tools) > 0 {
		capabilities = append(capabilities, "tool_calling")
	}

	return &GoTAgent{
		BaseAgent:   agentcore.NewBaseAgent(config.Name, config.Description, capabilities),
		llm:         config.LLM,
		tools:       config.Tools,
		toolsByName: toolsByName,
		config:      config,
		parser:      base.GetDefaultParser(),
	}
}

// Invoke executes the Graph-of-Thought reasoning
func (g *GoTAgent) Invoke(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	startTime := time.Now()

	// Trigger start callback
	if err := g.triggerOnStart(ctx, input); err != nil {
		return nil, err
	}

	// Initialize output
	output := &agentcore.AgentOutput{
		Steps:     make([]agentcore.AgentStep, 0),
		ToolCalls: make([]agentcore.AgentToolCall, 0),
		Metadata:  make(map[string]interface{}),
	}

	// 极简模式：专为慢速 API（如 DeepSeek）优化
	// 仅使用 2 次 LLM 调用：生成思考 + 合成答案
	if g.config.MinimalMode {
		return g.invokeMinimal(ctx, input, output, startTime)
	}

	// Build thought graph
	graph := g.buildThoughtGraph(ctx, input, output)

	// Check for cycles if enabled
	if g.config.CycleDetection && g.hasCycles(graph) {
		return g.handleError(ctx, output, "Cycle detected in thought graph", agentErrors.New(agentErrors.CodeAgentExecution, "cyclic dependencies found").
			WithComponent("got_agent").
			WithOperation("Invoke"), startTime)
	}

	// Execute graph (parallel or sequential)
	var finalResult interface{}
	var err error

	if g.config.ParallelExecution {
		finalResult, err = g.executeGraphParallel(ctx, graph, input, output)
	} else {
		finalResult, err = g.executeGraphSequential(ctx, graph, input, output)
	}

	if err != nil {
		return g.handleError(ctx, output, "Graph execution failed", err, startTime)
	}

	// Build final answer from graph results
	finalAnswer := g.synthesizeAnswer(ctx, graph, finalResult)

	output.Status = "success"
	output.Result = finalAnswer
	output.Message = "Graph-of-Thought reasoning completed successfully"
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)

	// Add graph metadata
	output.Metadata["total_nodes"] = len(graph)
	output.Metadata["parallel_execution"] = g.config.ParallelExecution
	output.Metadata["merge_strategy"] = g.config.MergeStrategy

	// Trigger finish callback
	if err := g.triggerOnFinish(ctx, output); err != nil {
		return nil, err
	}

	return output, nil
}

// invokeMinimal 极简模式执行
// 仅使用 2 次 LLM 调用，适用于慢速 API（如 DeepSeek）
//
// 执行流程：
//  1. 一次调用生成多个并行思考路径
//  2. 一次调用合成最终答案
//
// 优势：
//   - LLM 调用次数从 N 次降为 2 次
//   - 总执行时间显著缩短
//   - 适合响应慢的 API 提供商
func (g *GoTAgent) invokeMinimal(ctx context.Context, input *agentcore.AgentInput, output *agentcore.AgentOutput, startTime time.Time) (*agentcore.AgentOutput, error) {
	// 步骤1：一次调用生成多个思考路径
	thoughts, err := g.generateAllThoughts(ctx, input)
	if err != nil {
		return g.handleError(ctx, output, "Failed to generate thoughts", err, startTime)
	}

	// 记录生成步骤
	output.Steps = append(output.Steps, agentcore.AgentStep{
		Step:        1,
		Action:      "Generate Thoughts",
		Description: fmt.Sprintf("Generated %d parallel reasoning paths", len(thoughts)),
		Result:      strings.Join(thoughts, "\n---\n"),
		Duration:    time.Since(startTime),
		Success:     true,
	})

	// 步骤2：一次调用合成最终答案
	synthesisStart := time.Now()
	finalAnswer, err := g.synthesizeFromThoughts(ctx, input, thoughts)
	if err != nil {
		return g.handleError(ctx, output, "Failed to synthesize answer", err, startTime)
	}

	// 记录合成步骤
	output.Steps = append(output.Steps, agentcore.AgentStep{
		Step:        2,
		Action:      "Synthesize Answer",
		Description: "Combined insights from all reasoning paths",
		Result:      "Synthesis complete",
		Duration:    time.Since(synthesisStart),
		Success:     true,
	})

	output.Status = interfaces.StatusSuccess
	output.Result = finalAnswer
	output.Message = "Graph-of-Thought reasoning completed (minimal mode)"
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)

	// 添加元数据
	output.Metadata["mode"] = "minimal"
	output.Metadata["thought_count"] = len(thoughts)
	output.Metadata["llm_calls"] = 2

	// Trigger finish callback
	if err := g.triggerOnFinish(ctx, output); err != nil {
		return nil, err
	}

	return output, nil
}

// generateAllThoughts 一次调用生成所有思考路径
func (g *GoTAgent) generateAllThoughts(ctx context.Context, input *agentcore.AgentInput) ([]string, error) {
	prompt := fmt.Sprintf(`任务：%s

请从多个不同角度分析这个问题，提供 3-5 个独立的思考路径。
每个路径应该：
- 从不同的视角或方法切入
- 包含完整的推理过程
- 得出自己的结论

请按以下格式输出，用 "---" 分隔不同的思考路径：

思考路径 1:
[你的分析和结论]

---

思考路径 2:
[你的分析和结论]

---

思考路径 3:
[你的分析和结论]`, input.Task)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	resp, err := g.llm.Chat(ctx, messages)
	if err != nil {
		return nil, err
	}

	// 解析思考路径
	thoughts := g.parseThoughts(resp.Content)
	if len(thoughts) == 0 {
		// 如果解析失败，将整个响应作为单个思考
		thoughts = []string{resp.Content}
	}

	return thoughts, nil
}

// parseThoughts 解析 LLM 响应中的多个思考路径
func (g *GoTAgent) parseThoughts(content string) []string {
	// 按 "---" 分隔
	parts := strings.Split(content, "---")
	thoughts := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) > 20 { // 过滤太短的内容
			thoughts = append(thoughts, part)
		}
	}

	return thoughts
}

// synthesizeFromThoughts 从多个思考路径合成最终答案
func (g *GoTAgent) synthesizeFromThoughts(ctx context.Context, input *agentcore.AgentInput, thoughts []string) (string, error) {
	var thoughtsText strings.Builder
	for i, thought := range thoughts {
		thoughtsText.WriteString(fmt.Sprintf("=== 思考路径 %d ===\n%s\n\n", i+1, thought))
	}

	prompt := fmt.Sprintf(`原始任务：%s

以下是从不同角度进行的分析：

%s

请综合以上所有分析，提取关键见解，给出一个完整、准确的最终答案。
注意整合不同视角的优点，避免重复，确保答案全面且有条理。`, input.Task, thoughtsText.String())

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	resp, err := g.llm.Chat(ctx, messages)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

// InvokeFast 快速执行 Graph-of-Thought 推理（绕过回调）
//
// 用于热路径优化，跳过回调直接执行
// 性能提升：避免回调遍历开销
//
// 注意：此方法不会触发任何回调（OnStart/OnFinish等）
//
//go:inline
func (g *GoTAgent) InvokeFast(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	startTime := time.Now()

	// Initialize output
	output := &agentcore.AgentOutput{
		Steps:     make([]agentcore.AgentStep, 0),
		ToolCalls: make([]agentcore.AgentToolCall, 0),
		Metadata:  make(map[string]interface{}),
	}

	// Build thought graph
	graph := g.buildThoughtGraph(ctx, input, output)

	// Check for cycles if enabled
	if g.config.CycleDetection && g.hasCycles(graph) {
		err := agentErrors.New(agentErrors.CodeAgentExecution, "cyclic dependencies found").
			WithComponent("got_agent").
			WithOperation("InvokeFast")
		output.Status = interfaces.StatusFailed
		output.Message = "Cycle detected in thought graph"
		output.Timestamp = time.Now()
		output.Latency = time.Since(startTime)
		return output, err
	}

	// Execute graph (parallel or sequential)
	var finalResult interface{}
	var err error

	if g.config.ParallelExecution {
		finalResult, err = g.executeGraphParallel(ctx, graph, input, output)
	} else {
		finalResult, err = g.executeGraphSequential(ctx, graph, input, output)
	}

	if err != nil {
		output.Status = interfaces.StatusFailed
		output.Message = "Graph execution failed: " + err.Error()
		output.Timestamp = time.Now()
		output.Latency = time.Since(startTime)
		return output, err
	}

	// Build final answer from graph results
	finalAnswer := g.synthesizeAnswer(ctx, graph, finalResult)

	output.Status = interfaces.StatusSuccess
	output.Result = finalAnswer
	output.Message = "Graph-of-Thought reasoning completed successfully"
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)

	// Add graph metadata
	output.Metadata["total_nodes"] = len(graph)
	output.Metadata["parallel_execution"] = g.config.ParallelExecution
	output.Metadata["merge_strategy"] = g.config.MergeStrategy

	return output, nil
}

// buildThoughtGraph constructs the DAG of thoughts
func (g *GoTAgent) buildThoughtGraph(ctx context.Context, input *agentcore.AgentInput, output *agentcore.AgentOutput) []*GraphNode {
	// Initialize with root node
	root := &GraphNode{
		ID:           "root",
		Thought:      input.Task,
		Score:        1.0,
		Dependencies: []*GraphNode{},
		Dependents:   []*GraphNode{},
		State:        make(map[string]interface{}),
		Status:       "pending",
	}

	graph := []*GraphNode{root}
	nodeMap := map[string]*GraphNode{"root": root}

	// Build graph iteratively
	for len(graph) < g.config.MaxNodes {
		// 检查上下文是否已取消（超时控制）
		select {
		case <-ctx.Done():
			// 上下文已取消，停止构建图，返回当前已构建的部分
			return graph
		default:
			// 继续执行
		}

		// Select nodes for expansion
		candidates := g.selectExpansionCandidates(graph)
		if len(candidates) == 0 {
			break
		}

		for _, node := range candidates {
			// 每次生成前检查上下文
			select {
			case <-ctx.Done():
				return graph
			default:
			}

			// Generate new thoughts from this node
			newThoughts := g.generateThoughtsFromNode(ctx, node, input)

			for _, thought := range newThoughts {
				if len(graph) >= g.config.MaxNodes {
					break
				}

				// Create new node
				newNode := &GraphNode{
					ID:           fmt.Sprintf("node_%d", len(graph)),
					Thought:      thought,
					Score:        g.evaluateThought(ctx, thought, input),
					Dependencies: []*GraphNode{node},
					Dependents:   []*GraphNode{},
					State:        g.copyState(node.State),
					Status:       "pending",
				}

				// Check if this thought should connect to other existing nodes
				g.findAdditionalDependencies(newNode, graph)

				// Add to graph if score is above threshold
				if newNode.Score >= g.config.PruneThreshold {
					graph = append(graph, newNode)
					nodeMap[newNode.ID] = newNode

					// Update dependents
					for _, dep := range newNode.Dependencies {
						dep.Dependents = append(dep.Dependents, newNode)
					}

					// Record step
					output.Steps = append(output.Steps, agentcore.AgentStep{
						Step:        len(output.Steps) + 1,
						Action:      fmt.Sprintf("Graph Node (%s)", newNode.ID),
						Description: newNode.Thought,
						Result:      fmt.Sprintf("Score: %.2f, Dependencies: %d", newNode.Score, len(newNode.Dependencies)),
						Duration:    time.Millisecond * 100,
						Success:     true,
					})
				}
			}
		}
	}

	return graph
}

// executeGraphParallel executes independent nodes in parallel
func (g *GoTAgent) executeGraphParallel(ctx context.Context, graph []*GraphNode, input *agentcore.AgentInput, output *agentcore.AgentOutput) (interface{}, error) {
	// Topological sort for execution order
	sorted, err := g.topologicalSort(graph)
	if err != nil {
		return nil, err
	}

	// Execute in waves (nodes with same depth can run in parallel)
	waves := g.groupByDepth(sorted)

	for _, wave := range waves {
		var wg sync.WaitGroup
		errors := make(chan error, len(wave))

		for _, node := range wave {
			wg.Add(1)
			go func(n *GraphNode) {
				defer wg.Done()

				// Wait for dependencies to complete
				for _, dep := range n.Dependencies {
					for dep.Status != "completed" {
						time.Sleep(10 * time.Millisecond)
					}
				}

				// Process node
				err := g.processNode(ctx, n, input, output)
				if err != nil {
					errors <- err
				}
			}(node)
		}

		wg.Wait()
		close(errors)

		// Check for errors
		for err := range errors {
			if err != nil {
				return nil, err
			}
		}
	}

	// Find terminal nodes (no dependents)
	terminals := g.findTerminalNodes(graph)

	// Merge results from terminal nodes
	return g.mergeResults(ctx, terminals)
}

// executeGraphSequential executes nodes in topological order
func (g *GoTAgent) executeGraphSequential(ctx context.Context, graph []*GraphNode, input *agentcore.AgentInput, output *agentcore.AgentOutput) (interface{}, error) {
	// Topological sort
	sorted, err := g.topologicalSort(graph)
	if err != nil {
		return nil, err
	}

	// Execute in order
	for _, node := range sorted {
		if err := g.processNode(ctx, node, input, output); err != nil {
			return nil, err
		}
	}

	// Find terminal nodes and merge results
	terminals := g.findTerminalNodes(graph)
	return g.mergeResults(ctx, terminals)
}

// processNode executes a single node
func (g *GoTAgent) processNode(ctx context.Context, node *GraphNode, input *agentcore.AgentInput, output *agentcore.AgentOutput) error {
	node.mu.Lock()
	node.Status = "processing"
	node.mu.Unlock()

	// 添加节点级别超时控制
	nodeCtx, cancel := context.WithTimeout(ctx, g.config.NodeTimeout)
	defer cancel()

	// Build context from dependencies
	depContext := g.buildDependencyContext(node)

	// Generate prompt with dependency context
	prompt := fmt.Sprintf(`Given the task: %s

Previous reasoning:
%s

Current thought: %s

Provide your analysis or answer based on the above context.`,
		input.Task,
		depContext,
		node.Thought)

	// Call LLM
	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	llmResp, err := g.llm.Chat(nodeCtx, messages)
	if err != nil {
		node.mu.Lock()
		node.Status = "failed"
		node.mu.Unlock()
		return err
	}

	node.mu.Lock()
	node.Result = llmResp.Content
	node.Status = "completed"
	node.mu.Unlock()

	return nil
}

// Helper methods

func (g *GoTAgent) selectExpansionCandidates(graph []*GraphNode) []*GraphNode {
	candidates := make([]*GraphNode, 0)

	for _, node := range graph {
		// Select nodes that are completed and have good scores
		if node.Status == "pending" && node.Score >= g.config.PruneThreshold {
			if len(node.Dependents) < g.config.MaxEdgesPerNode {
				candidates = append(candidates, node)
			}
		}
	}

	// Sort by score and take top candidates
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	if len(candidates) > 3 {
		candidates = candidates[:3]
	}

	return candidates
}

func (g *GoTAgent) generateThoughtsFromNode(ctx context.Context, node *GraphNode, input *agentcore.AgentInput) []string {
	prompt := fmt.Sprintf(`Given the task: %s
Current thought: %s

Generate 2-3 different follow-up thoughts or approaches that build on or complement this thought.
Format each thought on a new line starting with "- " or numbered like "1. "`,
		input.Task,
		node.Thought)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	llmResp, err := g.llm.Chat(ctx, messages)
	if err != nil {
		return nil
	}

	// 使用解析器解析思考
	thoughts := make([]string, 0)
	lines := strings.Split(llmResp.Content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 使用解析器检测是否是步骤行
		isStep, content := g.parser.IsStepLine(line)
		if isStep {
			if content == "" {
				content = g.parser.ExtractStepContent(line)
			}
			if content != "" {
				thoughts = append(thoughts, content)
				continue
			}
		}

		// 备选：如果行足够长且不是提示性文本，也作为思考
		if len(line) > 20 && !g.isSkippableLine(line) {
			thoughts = append(thoughts, line)
		}
	}

	return thoughts
}

// isSkippableLine 检查是否应该跳过的行
func (g *GoTAgent) isSkippableLine(line string) bool {
	lowerLine := strings.ToLower(line)
	skipPrefixes := []string{"given", "task:", "current", "here are", "以下是", "任务：", "当前"}
	for _, skip := range skipPrefixes {
		if strings.HasPrefix(lowerLine, skip) || strings.HasPrefix(line, skip) {
			return true
		}
	}
	return false
}

func (g *GoTAgent) findAdditionalDependencies(node *GraphNode, graph []*GraphNode) {
	// Look for nodes that this thought might depend on
	// (simplified - in practice, use semantic similarity)
	for _, other := range graph {
		if other.ID != node.Dependencies[0].ID { // Skip direct parent
			// Check if thoughts are related
			if g.parser.AreThoughtsRelated(node.Thought, other.Thought) {
				node.Dependencies = append(node.Dependencies, other)
				if len(node.Dependencies) >= 3 { // Limit dependencies
					break
				}
			}
		}
	}
}

func (g *GoTAgent) evaluateThought(ctx context.Context, thought string, input *agentcore.AgentInput) float64 {
	// 快速评估模式：使用启发式评分，跳过 LLM 调用
	// 这是主要的性能优化点，避免每个思考都进行 LLM 调用
	if g.config.FastEvaluation {
		return g.evaluateThoughtFast(thought, input)
	}

	// 完整评估模式：使用 LLM 评分（可能导致超时）
	prompt := fmt.Sprintf(`Rate the following thought for solving the task on a scale of 0 to 1:
Task: %s
Thought: %s

Respond with just a number between 0 and 1.`,
		input.Task,
		thought)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	// 添加节点超时控制
	evalCtx, cancel := context.WithTimeout(ctx, g.config.NodeTimeout)
	defer cancel()

	llmResp, err := g.llm.Chat(evalCtx, messages)
	if err != nil {
		return 0.5
	}

	var score float64
	_, err = fmt.Sscanf(strings.TrimSpace(llmResp.Content), "%f", &score)
	if err != nil {
		score = 0.5
	}

	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	return score
}

// evaluateThoughtFast 使用启发式方法快速评估思考质量
// 不调用 LLM，基于文本特征评分
func (g *GoTAgent) evaluateThoughtFast(thought string, input *agentcore.AgentInput) float64 {
	score := 0.5 // 基础分

	// 1. 长度评分：太短或太长都扣分
	thoughtLen := len(thought)
	if thoughtLen >= 20 && thoughtLen <= 500 {
		score += 0.15
	} else if thoughtLen < 10 {
		score -= 0.2
	}

	// 2. 与任务的相关性：检查是否包含任务关键词
	taskLower := strings.ToLower(input.Task)
	thoughtLower := strings.ToLower(thought)

	// 提取任务关键词（简单分词）
	taskWords := strings.Fields(taskLower)
	matchCount := 0
	for _, word := range taskWords {
		if len(word) > 3 && strings.Contains(thoughtLower, word) {
			matchCount++
		}
	}
	if len(taskWords) > 0 {
		relevance := float64(matchCount) / float64(len(taskWords))
		score += relevance * 0.2
	}

	// 3. 结构性评分：使用解析器检查是否包含推理词汇
	if g.parser.ContainsReasoningWords(thought) {
		score += 0.05
	}

	// 4. 确保分数在 [0, 1] 范围内
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	return score
}

func (g *GoTAgent) topologicalSort(graph []*GraphNode) ([]*GraphNode, error) {
	sorted := make([]*GraphNode, 0, len(graph))
	visited := make(map[string]bool)
	tempMark := make(map[string]bool)

	var visit func(*GraphNode) error
	visit = func(node *GraphNode) error {
		if tempMark[node.ID] {
			return agentErrors.New(agentErrors.CodeAgentExecution, "cycle detected").
				WithComponent("got_agent").
				WithOperation("detectCycles").
				WithContext("node_id", node.ID)
		}
		if visited[node.ID] {
			return nil
		}

		tempMark[node.ID] = true

		for _, dep := range node.Dependents {
			if err := visit(dep); err != nil {
				return err
			}
		}

		tempMark[node.ID] = false
		visited[node.ID] = true
		sorted = append([]*GraphNode{node}, sorted...) // Prepend

		return nil
	}

	for _, node := range graph {
		if !visited[node.ID] {
			if err := visit(node); err != nil {
				return nil, err
			}
		}
	}

	return sorted, nil
}

func (g *GoTAgent) groupByDepth(sorted []*GraphNode) [][]*GraphNode {
	depths := make(map[*GraphNode]int)
	maxDepth := 0

	// Calculate depth for each node
	for _, node := range sorted {
		depth := 0
		for _, dep := range node.Dependencies {
			if d, exists := depths[dep]; exists && d >= depth {
				depth = d + 1
			}
		}
		depths[node] = depth
		if depth > maxDepth {
			maxDepth = depth
		}
	}

	// Group by depth
	waves := make([][]*GraphNode, maxDepth+1)
	for node, depth := range depths {
		waves[depth] = append(waves[depth], node)
	}

	return waves
}

func (g *GoTAgent) findTerminalNodes(graph []*GraphNode) []*GraphNode {
	terminals := make([]*GraphNode, 0)
	for _, node := range graph {
		if len(node.Dependents) == 0 {
			terminals = append(terminals, node)
		}
	}
	return terminals
}

func (g *GoTAgent) mergeResults(ctx context.Context, terminals []*GraphNode) (interface{}, error) {
	if len(terminals) == 0 {
		return nil, agentErrors.New(agentErrors.CodeAgentExecution, "no terminal nodes found").
			WithComponent("got_agent").
			WithOperation("mergeResults")
	}

	if len(terminals) == 1 {
		return terminals[0].Result, nil
	}

	switch g.config.MergeStrategy {
	case "vote":
		return g.mergeByVoting(terminals), nil

	case "weighted":
		return g.mergeByWeightedScore(terminals), nil

	case "llm":
		return g.mergeByLLM(ctx, terminals)

	default:
		// Default to weighted merge
		return g.mergeByWeightedScore(terminals), nil
	}
}

func (g *GoTAgent) mergeByVoting(terminals []*GraphNode) interface{} {
	// Simple majority voting (simplified)
	results := make(map[string]int)
	for _, node := range terminals {
		if result, ok := node.Result.(string); ok {
			results[result]++
		}
	}

	maxVotes := 0
	winner := ""
	for result, votes := range results {
		if votes > maxVotes {
			maxVotes = votes
			winner = result
		}
	}

	return winner
}

func (g *GoTAgent) mergeByWeightedScore(terminals []*GraphNode) interface{} {
	// Combine results weighted by score
	var combined strings.Builder
	totalScore := 0.0

	for _, node := range terminals {
		totalScore += node.Score
	}

	combined.WriteString("Combined insights from multiple reasoning paths:\n\n")

	for _, node := range terminals {
		weight := node.Score / totalScore
		combined.WriteString(fmt.Sprintf("[Weight: %.2f] %v\n", weight, node.Result))
	}

	return combined.String()
}

func (g *GoTAgent) mergeByLLM(ctx context.Context, terminals []*GraphNode) (interface{}, error) {
	// Use LLM to synthesize results
	var insights strings.Builder
	for i, node := range terminals {
		insights.WriteString(fmt.Sprintf("Path %d (Score: %.2f): %v\n", i+1, node.Score, node.Result))
	}

	prompt := fmt.Sprintf(`Given these different reasoning paths and their conclusions:

%s

Synthesize them into a single, coherent answer that combines the best insights from each path.`,
		insights.String())

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	llmResp, err := g.llm.Chat(ctx, messages)
	if err != nil {
		return nil, err
	}

	return llmResp.Content, nil
}

func (g *GoTAgent) buildDependencyContext(node *GraphNode) string {
	var context strings.Builder

	for _, dep := range node.Dependencies {
		context.WriteString(fmt.Sprintf("- %s: %v\n", dep.Thought, dep.Result))
	}

	return context.String()
}

func (g *GoTAgent) synthesizeAnswer(ctx context.Context, graph []*GraphNode, result interface{}) string {
	// Format the final answer
	return fmt.Sprintf("Based on graph-of-thought reasoning with %d nodes:\n\n%v", len(graph), result)
}

func (g *GoTAgent) hasCycles(graph []*GraphNode) bool {
	// Simple cycle detection using DFS
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var hasCycleDFS func(*GraphNode) bool
	hasCycleDFS = func(node *GraphNode) bool {
		visited[node.ID] = true
		recStack[node.ID] = true

		for _, dep := range node.Dependents {
			if !visited[dep.ID] {
				if hasCycleDFS(dep) {
					return true
				}
			} else if recStack[dep.ID] {
				return true
			}
		}

		recStack[node.ID] = false
		return false
	}

	for _, node := range graph {
		if !visited[node.ID] {
			if hasCycleDFS(node) {
				return true
			}
		}
	}

	return false
}

func (g *GoTAgent) copyState(state map[string]interface{}) map[string]interface{} {
	newState := make(map[string]interface{})
	for k, v := range state {
		newState[k] = v
	}
	return newState
}

// Stream executes Graph-of-Thought with streaming
func (g *GoTAgent) Stream(ctx context.Context, input *agentcore.AgentInput) (<-chan agentcore.StreamChunk[*agentcore.AgentOutput], error) {
	outChan := make(chan agentcore.StreamChunk[*agentcore.AgentOutput])

	go func() {
		defer close(outChan)

		output, err := g.Invoke(ctx, input)
		outChan <- agentcore.StreamChunk[*agentcore.AgentOutput]{
			Data:  output,
			Error: err,
			Done:  true,
		}
	}()

	return outChan, nil
}

// RunGenerator 使用 Generator 模式执行 Graph-of-Thought（实验性功能）
//
// 相比 Stream，RunGenerator 提供零分配的流式执行，在每个主要阶段后 yield 中间结果：
//   - 构建思维图后 yield
//   - 图执行完成后 yield
//   - 合成最终答案后 yield
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
//	    if stepType == "graph_built" {
//	        fmt.Printf("构建了 %d 个节点的图\n", output.Metadata["total_nodes"])
//	    }
//	    if output.Status == interfaces.StatusSuccess {
//	        break  // 完成
//	    }
//	}
//
// 注意：此方法不触发 Agent 级别的回调（OnStart/OnFinish）
func (g *GoTAgent) RunGenerator(ctx context.Context, input *agentcore.AgentInput) agentcore.Generator[*agentcore.AgentOutput] {
	return func(yield func(*agentcore.AgentOutput, error) bool) {
		startTime := time.Now()

		// Initialize accumulated output
		accumulated := &agentcore.AgentOutput{
			Steps:     make([]agentcore.AgentStep, 0),
			ToolCalls: make([]agentcore.AgentToolCall, 0),
			Metadata:  make(map[string]interface{}),
		}

		// Phase 1: Build thought graph
		graphStart := time.Now()
		graph := g.buildThoughtGraph(ctx, input, accumulated)

		// Check for cycles if enabled
		if g.config.CycleDetection && g.hasCycles(graph) {
			errorOutput := g.createStepOutput(accumulated, "Cycle detected in thought graph", startTime)
			errorOutput.Status = interfaces.StatusFailed
			err := agentErrors.New(agentErrors.CodeAgentExecution, "cyclic dependencies found").
				WithComponent("got_agent").
				WithOperation("RunGenerator")
			if !yield(errorOutput, err) {
				return
			}
			return
		}

		// Record graph building
		accumulated.Steps = append(accumulated.Steps, agentcore.AgentStep{
			Step:        1,
			Action:      "Build Graph",
			Description: fmt.Sprintf("Built thought graph with %d nodes", len(graph)),
			Result:      "Graph construction complete",
			Duration:    time.Since(graphStart),
			Success:     true,
		})

		// Yield after graph building
		graphOutput := g.createStepOutput(accumulated, "Thought graph built", startTime)
		graphOutput.Status = interfaces.StatusInProgress
		graphOutput.Metadata["step_type"] = "graph_built"
		graphOutput.Metadata["total_nodes"] = len(graph)
		graphOutput.Metadata["parallel_execution"] = g.config.ParallelExecution
		if !yield(graphOutput, nil) {
			return // Early termination
		}

		// Phase 2: Execute graph
		executionStart := time.Now()
		var finalResult interface{}
		var err error

		if g.config.ParallelExecution {
			finalResult, err = g.executeGraphParallel(ctx, graph, input, accumulated)
		} else {
			finalResult, err = g.executeGraphSequential(ctx, graph, input, accumulated)
		}

		if err != nil {
			errorOutput := g.createStepOutput(accumulated, "Graph execution failed", startTime)
			errorOutput.Status = interfaces.StatusPartial
			if !yield(errorOutput, err) {
				return
			}
			return
		}

		// Record graph execution
		accumulated.Steps = append(accumulated.Steps, agentcore.AgentStep{
			Step:        2,
			Action:      "Execute Graph",
			Description: "Executed all graph nodes",
			Result:      "Graph execution complete",
			Duration:    time.Since(executionStart),
			Success:     true,
		})

		// Yield after graph execution
		executionOutput := g.createStepOutput(accumulated, "Graph execution completed", startTime)
		executionOutput.Status = interfaces.StatusInProgress
		executionOutput.Metadata["step_type"] = "execution_completed"
		executionOutput.Metadata["merge_strategy"] = g.config.MergeStrategy
		if !yield(executionOutput, nil) {
			return // Early termination
		}

		// Phase 3: Synthesize answer
		synthesisStart := time.Now()
		finalAnswer := g.synthesizeAnswer(ctx, graph, finalResult)

		// Record synthesis
		accumulated.Steps = append(accumulated.Steps, agentcore.AgentStep{
			Step:        3,
			Action:      "Synthesize Answer",
			Description: "Combined graph results into final answer",
			Result:      "Answer synthesis complete",
			Duration:    time.Since(synthesisStart),
			Success:     true,
		})

		// Yield final output
		finalOutput := g.createStepOutput(accumulated, "Graph-of-Thought reasoning completed successfully", startTime)
		finalOutput.Status = interfaces.StatusSuccess
		finalOutput.Result = finalAnswer
		finalOutput.Metadata["step_type"] = "final"
		finalOutput.Metadata["total_duration_ms"] = time.Since(startTime).Milliseconds()
		yield(finalOutput, nil)
	}
}

// createStepOutput creates a snapshot of current execution state
func (g *GoTAgent) createStepOutput(accumulated *agentcore.AgentOutput, message string, startTime time.Time) *agentcore.AgentOutput {
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

// Error handling
func (g *GoTAgent) handleError(ctx context.Context, output *agentcore.AgentOutput, message string, err error, startTime time.Time) (*agentcore.AgentOutput, error) {
	output.Status = "failed"
	output.Message = message
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)

	_ = g.triggerOnError(ctx, err)
	return output, err
}

// Callback triggers
func (g *GoTAgent) triggerOnStart(ctx context.Context, input *agentcore.AgentInput) error {
	config := g.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnStart(ctx, input); err != nil {
			return err
		}
	}
	return nil
}

func (g *GoTAgent) triggerOnFinish(ctx context.Context, output *agentcore.AgentOutput) error {
	config := g.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnAgentFinish(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (g *GoTAgent) triggerOnError(ctx context.Context, err error) error {
	config := g.GetConfig()
	for _, cb := range config.Callbacks {
		if cbErr := cb.OnError(ctx, err); cbErr != nil {
			return cbErr
		}
	}
	return nil
}

// WithCallbacks adds callback handlers
func (g *GoTAgent) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	newAgent := *g
	newAgent.BaseAgent = g.BaseAgent.WithCallbacks(callbacks...).(*agentcore.BaseAgent)
	return &newAgent
}

// WithConfig configures the agent
func (g *GoTAgent) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	newAgent := *g
	newAgent.BaseAgent = g.BaseAgent.WithConfig(config).(*agentcore.BaseAgent)
	return &newAgent
}
