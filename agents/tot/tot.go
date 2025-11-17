package tot

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// ToTAgent implements Tree-of-Thought reasoning pattern.
//
// Tree-of-Thought (ToT) extends Chain-of-Thought by exploring multiple reasoning
// paths simultaneously, evaluating each path, and selecting the best one.
// This agent:
// - Maintains a search tree of thoughts
// - Evaluates and scores each thought
// - Uses search strategies (BFS, DFS, beam search)
// - Backtracks when needed
// - Selects optimal reasoning path
type ToTAgent struct {
	*agentcore.BaseAgent
	llm         llm.Client
	tools       []interfaces.Tool
	toolsByName map[string]interfaces.Tool
	config      ToTConfig
}

// ToTConfig configuration for Tree-of-Thought agent
type ToTConfig struct {
	Name        string            // Agent name
	Description string            // Agent description
	LLM         llm.Client        // LLM client
	Tools       []interfaces.Tool // Available tools (optional)

	// Tree search parameters
	MaxDepth        int // Maximum depth of the thought tree
	BranchingFactor int // Number of thoughts to generate at each node
	BeamWidth       int // Width for beam search (0 = use all branches)

	// Search strategy
	SearchStrategy interfaces.ReasoningStrategy // Search strategy to use

	// Evaluation settings
	EvaluationMethod string  // How to evaluate thoughts ("llm", "heuristic", "hybrid")
	PruneThreshold   float64 // Threshold for pruning low-score branches

	// Prompts
	ThoughtGenerationPrompt string // Prompt for generating thoughts
	EvaluationPrompt        string // Prompt for evaluating thoughts
	SolutionCheckPrompt     string // Prompt for checking if solution is found
}

// ThoughtNode represents a node in the thought tree
type ThoughtNode struct {
	ID         string
	Thought    string
	Score      float64
	Depth      int
	Parent     *ThoughtNode
	Children   []*ThoughtNode
	State      map[string]interface{} // Problem state at this node
	IsSolution bool
	ToolCalls  []agentcore.ToolCall
}

// NewToTAgent creates a new Tree-of-Thought agent
func NewToTAgent(config ToTConfig) *ToTAgent {
	if config.MaxDepth <= 0 {
		config.MaxDepth = 5
	}
	if config.BranchingFactor <= 0 {
		config.BranchingFactor = 3
	}
	if config.SearchStrategy == "" {
		config.SearchStrategy = interfaces.StrategyBeamSearch
	}
	if config.EvaluationMethod == "" {
		config.EvaluationMethod = "llm"
	}
	if config.PruneThreshold == 0 {
		config.PruneThreshold = 0.3
	}

	// Build tools map
	toolsByName := make(map[string]interfaces.Tool)
	for _, tool := range config.Tools {
		toolsByName[tool.Name()] = tool
	}

	capabilities := []string{"tree_of_thought", "search", "backtracking", "multi_path"}
	if len(config.Tools) > 0 {
		capabilities = append(capabilities, "tool_calling")
	}

	return &ToTAgent{
		BaseAgent:   agentcore.NewBaseAgent(config.Name, config.Description, capabilities),
		llm:         config.LLM,
		tools:       config.Tools,
		toolsByName: toolsByName,
		config:      config,
	}
}

// Invoke executes the Tree-of-Thought reasoning
func (t *ToTAgent) Invoke(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	startTime := time.Now()

	// Trigger start callback
	if err := t.triggerOnStart(ctx, input); err != nil {
		return nil, err
	}

	// Initialize output
	output := &agentcore.AgentOutput{
		ReasoningSteps: make([]agentcore.ReasoningStep, 0),
		ToolCalls:      make([]agentcore.ToolCall, 0),
		Metadata:       make(map[string]interface{}),
	}

	// Create root node
	root := &ThoughtNode{
		ID:      "root",
		Thought: input.Task,
		Score:   1.0,
		Depth:   0,
		State:   make(map[string]interface{}),
	}

	// Execute tree search based on strategy
	var solution *ThoughtNode
	var err error

	switch t.config.SearchStrategy {
	case interfaces.StrategyDepthFirst:
		solution, err = t.depthFirstSearch(ctx, root, input, output)
	case interfaces.StrategyBreadthFirst:
		solution, err = t.breadthFirstSearch(ctx, root, input, output)
	case interfaces.StrategyBeamSearch:
		solution, err = t.beamSearch(ctx, root, input, output)
	case interfaces.StrategyMonteCarlo:
		solution, err = t.monteCarloSearch(ctx, root, input, output)
	default:
		solution, err = t.beamSearch(ctx, root, input, output)
	}

	if err != nil {
		return t.handleError(ctx, output, "Tree search failed", err, startTime)
	}

	// Build final answer from solution path
	if solution != nil {
		path := t.getPathToRoot(solution)
		finalAnswer := t.buildAnswerFromPath(path)

		output.Status = "success"
		output.Result = finalAnswer
		output.Message = "Tree-of-Thought reasoning completed successfully"

		// Add path to metadata
		output.Metadata["solution_path"] = t.pathToStrings(path)
		output.Metadata["total_nodes_explored"] = t.countNodes(root)
		output.Metadata["solution_depth"] = solution.Depth
	} else {
		output.Status = "failed"
		output.Message = "No solution found within depth limit"
		output.Result = "Unable to find a solution through tree search"
	}

	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)

	// Trigger finish callback
	if err := t.triggerOnFinish(ctx, output); err != nil {
		return nil, err
	}

	return output, nil
}

// beamSearch performs beam search on the thought tree
func (t *ToTAgent) beamSearch(ctx context.Context, root *ThoughtNode, input *agentcore.AgentInput, output *agentcore.AgentOutput) (*ThoughtNode, error) {
	beamWidth := t.config.BeamWidth
	if beamWidth <= 0 {
		beamWidth = t.config.BranchingFactor
	}

	// Current beam (frontier nodes)
	beam := []*ThoughtNode{root}

	for depth := 0; depth < t.config.MaxDepth && len(beam) > 0; depth++ {
		nextBeam := make([]*ThoughtNode, 0)

		// Expand all nodes in current beam
		for _, node := range beam {
			// Check if current node is a solution
			if t.isSolution(ctx, node, input) {
				node.IsSolution = true
				return node, nil
			}

			// Generate children thoughts
			children := t.generateThoughts(ctx, node, input, output)

			// Evaluate each child
			for _, child := range children {
				child.Score = t.evaluateThought(ctx, child, input)

				// Prune low-score thoughts
				if child.Score >= t.config.PruneThreshold {
					nextBeam = append(nextBeam, child)

					// Record reasoning step
					output.ReasoningSteps = append(output.ReasoningSteps, agentcore.ReasoningStep{
						Step:        len(output.ReasoningSteps) + 1,
						Action:      fmt.Sprintf("Thought (depth=%d)", depth+1),
						Description: child.Thought,
						Result:      fmt.Sprintf("Score: %.2f", child.Score),
						Duration:    time.Millisecond * 100, // Approximate
						Success:     true,
					})
				}
			}
		}

		// Select top-k nodes for next beam
		if len(nextBeam) > beamWidth {
			sort.Slice(nextBeam, func(i, j int) bool {
				return nextBeam[i].Score > nextBeam[j].Score
			})
			nextBeam = nextBeam[:beamWidth]
		}

		beam = nextBeam
	}

	// Return best node if no solution found
	if len(beam) > 0 {
		return beam[0], nil
	}

	return nil, agentErrors.New(agentErrors.CodeAgentExecution, "no valid paths found").
		WithComponent("tot_agent").
		WithOperation("beamSearch")
}

// depthFirstSearch performs DFS on the thought tree
func (t *ToTAgent) depthFirstSearch(ctx context.Context, node *ThoughtNode, input *agentcore.AgentInput, output *agentcore.AgentOutput) (*ThoughtNode, error) {
	// Check if current node is a solution
	if t.isSolution(ctx, node, input) {
		node.IsSolution = true
		return node, nil
	}

	// Check depth limit
	if node.Depth >= t.config.MaxDepth {
		return nil, nil
	}

	// Generate and explore children
	children := t.generateThoughts(ctx, node, input, output)
	for _, child := range children {
		child.Score = t.evaluateThought(ctx, child, input)

		// Skip low-score branches
		if child.Score < t.config.PruneThreshold {
			continue
		}

		// Record step
		output.ReasoningSteps = append(output.ReasoningSteps, agentcore.ReasoningStep{
			Step:        len(output.ReasoningSteps) + 1,
			Action:      fmt.Sprintf("Explore (DFS, depth=%d)", child.Depth),
			Description: child.Thought,
			Result:      fmt.Sprintf("Score: %.2f", child.Score),
			Duration:    time.Millisecond * 100,
			Success:     true,
		})

		// Recursive DFS
		solution, err := t.depthFirstSearch(ctx, child, input, output)
		if err != nil {
			return nil, err
		}
		if solution != nil {
			return solution, nil
		}
	}

	return nil, nil
}

// breadthFirstSearch performs BFS on the thought tree
func (t *ToTAgent) breadthFirstSearch(ctx context.Context, root *ThoughtNode, input *agentcore.AgentInput, output *agentcore.AgentOutput) (*ThoughtNode, error) {
	queue := []*ThoughtNode{root}
	visited := make(map[string]bool)

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]

		if visited[node.ID] {
			continue
		}
		visited[node.ID] = true

		// Check if solution
		if t.isSolution(ctx, node, input) {
			node.IsSolution = true
			return node, nil
		}

		// Check depth limit
		if node.Depth >= t.config.MaxDepth {
			continue
		}

		// Generate children
		children := t.generateThoughts(ctx, node, input, output)
		for _, child := range children {
			child.Score = t.evaluateThought(ctx, child, input)

			if child.Score >= t.config.PruneThreshold {
				queue = append(queue, child)

				// Record step
				output.ReasoningSteps = append(output.ReasoningSteps, agentcore.ReasoningStep{
					Step:        len(output.ReasoningSteps) + 1,
					Action:      fmt.Sprintf("Explore (BFS, depth=%d)", child.Depth),
					Description: child.Thought,
					Result:      fmt.Sprintf("Score: %.2f", child.Score),
					Duration:    time.Millisecond * 100,
					Success:     true,
				})
			}
		}
	}

	return nil, agentErrors.New(agentErrors.CodeAgentExecution, "no solution found").
		WithComponent("tot_agent").
		WithOperation("breadthFirstSearch")
}

// monteCarloSearch performs Monte Carlo Tree Search
func (t *ToTAgent) monteCarloSearch(ctx context.Context, root *ThoughtNode, input *agentcore.AgentInput, output *agentcore.AgentOutput) (*ThoughtNode, error) {
	iterations := 100 // Number of MCTS iterations

	for i := 0; i < iterations; i++ {
		// Selection: select promising node
		node := t.selectNode(root)

		// Expansion: expand if not fully expanded
		if node.Depth < t.config.MaxDepth && !t.isSolution(ctx, node, input) {
			children := t.generateThoughts(ctx, node, input, output)
			if len(children) > 0 {
				node = children[0] // Select first child for simulation
			}
		}

		// Simulation: simulate to terminal state
		score := t.simulate(ctx, node, input)

		// Backpropagation: update scores
		t.backpropagate(node, score)

		// Check if we found a solution
		if t.isSolution(ctx, node, input) {
			node.IsSolution = true
			return node, nil
		}
	}

	// Return best path
	return t.getBestPath(root), nil
}

// generateThoughts generates child thoughts for a node
func (t *ToTAgent) generateThoughts(ctx context.Context, parent *ThoughtNode, input *agentcore.AgentInput, output *agentcore.AgentOutput) []*ThoughtNode {
	prompt := t.buildThoughtGenerationPrompt(parent, input)

	messages := []llm.Message{
		llm.SystemMessage("You are generating possible next steps in a reasoning tree. Generate diverse and logical continuations."),
		llm.UserMessage(prompt),
	}

	llmResp, err := t.llm.Chat(ctx, messages)
	if err != nil {
		return nil
	}

	// Parse generated thoughts
	thoughts := t.parseGeneratedThoughts(llmResp.Content)

	children := make([]*ThoughtNode, 0, len(thoughts))
	for i, thought := range thoughts {
		child := &ThoughtNode{
			ID:      fmt.Sprintf("%s_%d_%d", parent.ID, parent.Depth+1, i),
			Thought: thought,
			Depth:   parent.Depth + 1,
			Parent:  parent,
			State:   t.copyState(parent.State),
		}

		// Check if tools are needed
		if t.needsTools(thought) {
			toolResults := t.executeToolsForThought(ctx, thought, output)
			child.ToolCalls = toolResults
		}

		children = append(children, child)
		parent.Children = append(parent.Children, child)
	}

	return children
}

// evaluateThought evaluates the quality/promise of a thought
func (t *ToTAgent) evaluateThought(ctx context.Context, node *ThoughtNode, input *agentcore.AgentInput) float64 {
	switch t.config.EvaluationMethod {
	case "llm":
		return t.evaluateWithLLM(ctx, node, input)
	case "heuristic":
		return t.evaluateWithHeuristic(node, input)
	case "hybrid":
		llmScore := t.evaluateWithLLM(ctx, node, input)
		heuristicScore := t.evaluateWithHeuristic(node, input)
		return (llmScore + heuristicScore) / 2
	default:
		return t.evaluateWithLLM(ctx, node, input)
	}
}

// evaluateWithLLM uses the LLM to score a thought
func (t *ToTAgent) evaluateWithLLM(ctx context.Context, node *ThoughtNode, input *agentcore.AgentInput) float64 {
	prompt := fmt.Sprintf(`Evaluate this reasoning step for solving the problem:
Problem: %s
Current thought: %s
Previous context: %s

Rate this thought from 0 to 1 based on:
1. Logical correctness
2. Progress toward solution
3. Clarity and coherence

Respond with just a number between 0 and 1.`,
		input.Task,
		node.Thought,
		t.getContext(node))

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	llmResp, err := t.llm.Chat(ctx, messages)
	if err != nil {
		return 0.5 // Default score on error
	}

	// Parse score from response
	var score float64
	_, err = fmt.Sscanf(strings.TrimSpace(llmResp.Content), "%f", &score)
	if err != nil {
		score = 0.5 // Default score on parse error
	}

	// Ensure score is in valid range
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	return score
}

// evaluateWithHeuristic uses heuristics to score a thought
func (t *ToTAgent) evaluateWithHeuristic(node *ThoughtNode, input *agentcore.AgentInput) float64 {
	score := 0.5 // Base score

	// Penalize very short thoughts
	if len(node.Thought) < 20 {
		score -= 0.2
	}

	// Reward detailed thoughts
	if len(node.Thought) > 100 {
		score += 0.1
	}

	// Reward if contains key problem terms
	problemWords := strings.Fields(strings.ToLower(input.Task))
	thoughtWords := strings.Fields(strings.ToLower(node.Thought))
	matches := 0
	for _, pw := range problemWords {
		for _, tw := range thoughtWords {
			if pw == tw {
				matches++
				break
			}
		}
	}
	score += float64(matches) / float64(len(problemWords)) * 0.2

	// Penalize repetition
	if node.Parent != nil && strings.Contains(node.Thought, node.Parent.Thought) {
		score -= 0.3
	}

	// Ensure valid range
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	return score
}

// isSolution checks if a node represents a solution
func (t *ToTAgent) isSolution(ctx context.Context, node *ThoughtNode, input *agentcore.AgentInput) bool {
	prompt := fmt.Sprintf(`Given this problem: %s

And this reasoning path:
%s

Current thought: %s

Has a complete solution been reached? Answer only 'yes' or 'no'.`,
		input.Task,
		t.getContext(node),
		node.Thought)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	llmResp, err := t.llm.Chat(ctx, messages)
	if err != nil {
		return false
	}

	response := strings.ToLower(strings.TrimSpace(llmResp.Content))
	return strings.Contains(response, "yes")
}

// Helper methods

func (t *ToTAgent) buildThoughtGenerationPrompt(parent *ThoughtNode, input *agentcore.AgentInput) string {
	context := t.getContext(parent)

	prompt := fmt.Sprintf(`Problem: %s

Current reasoning path:
%s

Current state: %s

Generate %d different possible next steps in the reasoning process.
Each step should:
1. Be logically connected to the current state
2. Make progress toward solving the problem
3. Be distinct from other steps

Format your response as:
Step 1: [thought]
Step 2: [thought]
...`,
		input.Task,
		context,
		parent.Thought,
		t.config.BranchingFactor)

	return prompt
}

func (t *ToTAgent) parseGeneratedThoughts(response string) []string {
	thoughts := make([]string, 0)
	lines := strings.Split(response, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Step ") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) > 1 {
				thought := strings.TrimSpace(parts[1])
				if thought != "" {
					thoughts = append(thoughts, thought)
				}
			}
		}
	}

	// If no structured format, split by numbered list
	if len(thoughts) == 0 {
		for _, line := range lines {
			if matched := strings.TrimSpace(line); matched != "" && len(matched) > 10 {
				thoughts = append(thoughts, matched)
				if len(thoughts) >= t.config.BranchingFactor {
					break
				}
			}
		}
	}

	return thoughts
}

func (t *ToTAgent) getContext(node *ThoughtNode) string {
	path := t.getPathToRoot(node)
	context := make([]string, 0, len(path))

	for i := len(path) - 1; i >= 0; i-- {
		if path[i].ID != "root" {
			context = append(context, fmt.Sprintf("→ %s", path[i].Thought))
		}
	}

	return strings.Join(context, "\n")
}

func (t *ToTAgent) getPathToRoot(node *ThoughtNode) []*ThoughtNode {
	path := make([]*ThoughtNode, 0)
	current := node

	for current != nil {
		path = append(path, current)
		current = current.Parent
	}

	return path
}

func (t *ToTAgent) pathToStrings(path []*ThoughtNode) []string {
	result := make([]string, 0, len(path))
	for i := len(path) - 1; i >= 0; i-- {
		if path[i].ID != "root" {
			result = append(result, path[i].Thought)
		}
	}
	return result
}

func (t *ToTAgent) buildAnswerFromPath(path []*ThoughtNode) string {
	steps := t.pathToStrings(path)

	var answer strings.Builder
	answer.WriteString("Based on tree-of-thought reasoning:\n\n")

	for i, step := range steps {
		answer.WriteString(fmt.Sprintf("Step %d: %s\n", i+1, step))
	}

	if len(path) > 0 && path[0].IsSolution {
		answer.WriteString(fmt.Sprintf("\nFinal Answer: %s", path[0].Thought))
	}

	return answer.String()
}

func (t *ToTAgent) countNodes(root *ThoughtNode) int {
	count := 1
	for _, child := range root.Children {
		count += t.countNodes(child)
	}
	return count
}

func (t *ToTAgent) copyState(state map[string]interface{}) map[string]interface{} {
	newState := make(map[string]interface{})
	for k, v := range state {
		newState[k] = v
	}
	return newState
}

func (t *ToTAgent) needsTools(thought string) bool {
	toolKeywords := []string{"calculate", "compute", "search", "look up", "find", "verify"}
	thoughtLower := strings.ToLower(thought)

	for _, keyword := range toolKeywords {
		if strings.Contains(thoughtLower, keyword) {
			return true
		}
	}
	return false
}

func (t *ToTAgent) executeToolsForThought(ctx context.Context, thought string, output *agentcore.AgentOutput) []agentcore.ToolCall {
	// This is simplified - in practice, you'd parse the thought to determine which tools to use
	toolCalls := make([]agentcore.ToolCall, 0)

	// Example: if thought mentions calculation, use calculator tool
	if strings.Contains(strings.ToLower(thought), "calculate") {
		if calc, exists := t.toolsByName["calculator"]; exists {
			input := &interfaces.ToolInput{
				Args: map[string]interface{}{
					"expression": thought,
				},
				Context: ctx,
			}

			result, err := calc.Invoke(ctx, input)
			toolCall := agentcore.ToolCall{
				ToolName: "calculator",
				Input:    input.Args,
				Success:  err == nil,
			}

			if err != nil {
				toolCall.Error = err.Error()
			} else {
				toolCall.Output = result.Result
			}

			toolCalls = append(toolCalls, toolCall)
			output.ToolCalls = append(output.ToolCalls, toolCall)
		}
	}

	return toolCalls
}

// MCTS helper methods

func (t *ToTAgent) selectNode(root *ThoughtNode) *ThoughtNode {
	current := root

	for len(current.Children) > 0 {
		// UCB1 formula for selection
		bestChild := current.Children[0]
		bestScore := t.ucb1Score(bestChild, current)

		for _, child := range current.Children[1:] {
			score := t.ucb1Score(child, current)
			if score > bestScore {
				bestScore = score
				bestChild = child
			}
		}

		current = bestChild
	}

	return current
}

func (t *ToTAgent) ucb1Score(child, parent *ThoughtNode) float64 {
	if child.Score == 0 {
		return math.Inf(1) // Unexplored nodes have infinite score
	}

	exploitation := child.Score
	exploration := math.Sqrt(2 * math.Log(float64(t.countNodes(parent))) / float64(t.countNodes(child)))

	return exploitation + exploration
}

func (t *ToTAgent) simulate(ctx context.Context, node *ThoughtNode, input *agentcore.AgentInput) float64 {
	// Simple simulation: use LLM to estimate quality
	return t.evaluateThought(ctx, node, input)
}

func (t *ToTAgent) backpropagate(node *ThoughtNode, score float64) {
	current := node
	for current != nil {
		current.Score = (current.Score + score) / 2 // Running average
		current = current.Parent
	}
}

func (t *ToTAgent) getBestPath(root *ThoughtNode) *ThoughtNode {
	if len(root.Children) == 0 {
		return root
	}

	bestChild := root.Children[0]
	for _, child := range root.Children[1:] {
		if child.Score > bestChild.Score {
			bestChild = child
		}
	}

	return t.getBestPath(bestChild)
}

// Stream executes Tree-of-Thought with streaming
func (t *ToTAgent) Stream(ctx context.Context, input *agentcore.AgentInput) (<-chan agentcore.StreamChunk[*agentcore.AgentOutput], error) {
	outChan := make(chan agentcore.StreamChunk[*agentcore.AgentOutput])

	go func() {
		defer close(outChan)

		output, err := t.Invoke(ctx, input)
		outChan <- agentcore.StreamChunk[*agentcore.AgentOutput]{
			Data:  output,
			Error: err,
			Done:  true,
		}
	}()

	return outChan, nil
}

// Error handling
func (t *ToTAgent) handleError(ctx context.Context, output *agentcore.AgentOutput, message string, err error, startTime time.Time) (*agentcore.AgentOutput, error) {
	output.Status = "failed"
	output.Message = message
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)

	_ = t.triggerOnError(ctx, err)
	return output, err
}

// Callback triggers
func (t *ToTAgent) triggerOnStart(ctx context.Context, input *agentcore.AgentInput) error {
	config := t.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnStart(ctx, input); err != nil {
			return err
		}
	}
	return nil
}

func (t *ToTAgent) triggerOnFinish(ctx context.Context, output *agentcore.AgentOutput) error {
	config := t.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnAgentFinish(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (t *ToTAgent) triggerOnError(ctx context.Context, err error) error {
	config := t.GetConfig()
	for _, cb := range config.Callbacks {
		if cbErr := cb.OnError(ctx, err); cbErr != nil {
			return cbErr
		}
	}
	return nil
}

// WithCallbacks adds callback handlers
func (t *ToTAgent) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	newAgent := *t
	newAgent.BaseAgent = t.BaseAgent.WithCallbacks(callbacks...).(*agentcore.BaseAgent)
	return &newAgent
}

// WithConfig configures the agent
func (t *ToTAgent) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	newAgent := *t
	newAgent.BaseAgent = t.BaseAgent.WithConfig(config).(*agentcore.BaseAgent)
	return &newAgent
}
