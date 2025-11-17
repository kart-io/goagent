package got

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

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
	if config.MaxNodes <= 0 {
		config.MaxNodes = 50
	}
	if config.MaxEdgesPerNode <= 0 {
		config.MaxEdgesPerNode = 5
	}
	if config.MergeStrategy == "" {
		config.MergeStrategy = "weighted"
	}
	if config.PruneThreshold == 0 {
		config.PruneThreshold = 0.3
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
		ReasoningSteps: make([]agentcore.ReasoningStep, 0),
		ToolCalls:      make([]agentcore.ToolCall, 0),
		Metadata:       make(map[string]interface{}),
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
		// Select nodes for expansion
		candidates := g.selectExpansionCandidates(graph)
		if len(candidates) == 0 {
			break
		}

		for _, node := range candidates {
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
					output.ReasoningSteps = append(output.ReasoningSteps, agentcore.ReasoningStep{
						Step:        len(output.ReasoningSteps) + 1,
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

	llmResp, err := g.llm.Chat(ctx, messages)
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
Format each thought on a new line starting with "- "`,
		input.Task,
		node.Thought)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	llmResp, err := g.llm.Chat(ctx, messages)
	if err != nil {
		return nil
	}

	// Parse thoughts
	thoughts := make([]string, 0)
	lines := strings.Split(llmResp.Content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			thought := strings.TrimPrefix(line, "- ")
			if thought != "" {
				thoughts = append(thoughts, thought)
			}
		}
	}

	return thoughts
}

func (g *GoTAgent) findAdditionalDependencies(node *GraphNode, graph []*GraphNode) {
	// Look for nodes that this thought might depend on
	// (simplified - in practice, use semantic similarity)
	for _, other := range graph {
		if other.ID != node.Dependencies[0].ID { // Skip direct parent
			// Check if thoughts are related
			if g.areThoughtsRelated(node.Thought, other.Thought) {
				node.Dependencies = append(node.Dependencies, other)
				if len(node.Dependencies) >= 3 { // Limit dependencies
					break
				}
			}
		}
	}
}

func (g *GoTAgent) areThoughtsRelated(thought1, thought2 string) bool {
	// Simplified relatedness check
	t1Lower := strings.ToLower(thought1)
	t2Lower := strings.ToLower(thought2)

	// Check for common key terms
	commonTerms := []string{"therefore", "because", "result", "conclusion", "analysis"}
	for _, term := range commonTerms {
		if strings.Contains(t1Lower, term) && strings.Contains(t2Lower, term) {
			return true
		}
	}

	return false
}

func (g *GoTAgent) evaluateThought(ctx context.Context, thought string, input *agentcore.AgentInput) float64 {
	prompt := fmt.Sprintf(`Rate the following thought for solving the task on a scale of 0 to 1:
Task: %s
Thought: %s

Respond with just a number between 0 and 1.`,
		input.Task,
		thought)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	llmResp, err := g.llm.Chat(ctx, messages)
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
