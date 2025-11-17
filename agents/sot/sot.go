package sot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// SoTAgent implements Skeleton-of-Thought reasoning pattern.
//
// Skeleton-of-Thought (SoT) first generates a high-level skeleton of the solution,
// then elaborates each point in parallel, significantly speeding up reasoning.
// This agent:
// - Creates a skeleton outline first
// - Elaborates skeleton points in parallel
// - Manages dependencies between points
// - Aggregates results efficiently
// - Optimizes for latency through parallelism
type SoTAgent struct {
	*agentcore.BaseAgent
	llm         llm.Client
	tools       []interfaces.Tool
	toolsByName map[string]interfaces.Tool
	config      SoTConfig
}

// SoTConfig configuration for Skeleton-of-Thought agent
type SoTConfig struct {
	Name        string            // Agent name
	Description string            // Agent description
	LLM         llm.Client        // LLM client
	Tools       []interfaces.Tool // Available tools (optional)

	// Skeleton generation settings
	MaxSkeletonPoints int  // Maximum number of skeleton points
	MinSkeletonPoints int  // Minimum number of skeleton points
	AutoDecompose     bool // Automatically decompose complex points

	// Parallel execution settings
	MaxConcurrency     int           // Maximum concurrent elaborations
	ElaborationTimeout time.Duration // Timeout for each elaboration
	BatchSize          int           // Batch size for parallel processing

	// Aggregation settings
	AggregationStrategy string // How to combine results ("sequential", "hierarchical", "weighted")
	DependencyAware     bool   // Consider dependencies between points
}

// SkeletonPoint represents a point in the skeleton structure
type SkeletonPoint struct {
	ID           string
	Title        string                 // Short title of the point
	Description  string                 // What needs to be elaborated
	Dependencies []string               // IDs of points this depends on
	Priority     int                    // Priority for execution order
	Status       string                 // "pending", "processing", "completed"
	Elaboration  string                 // Detailed elaboration result
	SubPoints    []*SkeletonPoint       // Sub-points for hierarchical structure
	Metadata     map[string]interface{} // Additional metadata
	mu           sync.RWMutex           // Thread safety
}

// NewSoTAgent creates a new Skeleton-of-Thought agent
func NewSoTAgent(config SoTConfig) *SoTAgent {
	if config.MaxSkeletonPoints <= 0 {
		config.MaxSkeletonPoints = 10
	}
	if config.MinSkeletonPoints <= 0 {
		config.MinSkeletonPoints = 3
	}
	if config.MaxConcurrency <= 0 {
		config.MaxConcurrency = 5
	}
	if config.ElaborationTimeout <= 0 {
		config.ElaborationTimeout = 30 * time.Second
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 3
	}
	if config.AggregationStrategy == "" {
		config.AggregationStrategy = "sequential"
	}

	// Build tools map
	toolsByName := make(map[string]interfaces.Tool)
	for _, tool := range config.Tools {
		toolsByName[tool.Name()] = tool
	}

	capabilities := []string{"skeleton_of_thought", "parallel_reasoning", "decomposition"}
	if len(config.Tools) > 0 {
		capabilities = append(capabilities, "tool_calling")
	}

	return &SoTAgent{
		BaseAgent:   agentcore.NewBaseAgent(config.Name, config.Description, capabilities),
		llm:         config.LLM,
		tools:       config.Tools,
		toolsByName: toolsByName,
		config:      config,
	}
}

// Invoke executes the Skeleton-of-Thought reasoning
func (s *SoTAgent) Invoke(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	startTime := time.Now()

	// Trigger start callback
	if err := s.triggerOnStart(ctx, input); err != nil {
		return nil, err
	}

	// Initialize output
	output := &agentcore.AgentOutput{
		ReasoningSteps: make([]agentcore.ReasoningStep, 0),
		ToolCalls:      make([]agentcore.ToolCall, 0),
		Metadata:       make(map[string]interface{}),
	}

	// Phase 1: Generate skeleton
	skeletonStart := time.Now()
	skeleton, err := s.generateSkeleton(ctx, input)
	if err != nil {
		return s.handleError(ctx, output, "Skeleton generation failed", err, startTime)
	}

	// Record skeleton generation
	output.ReasoningSteps = append(output.ReasoningSteps, agentcore.ReasoningStep{
		Step:        1,
		Action:      "Generate Skeleton",
		Description: fmt.Sprintf("Created %d skeleton points", len(skeleton)),
		Result:      s.formatSkeleton(skeleton),
		Duration:    time.Since(skeletonStart),
		Success:     true,
	})

	// Phase 2: Elaborate skeleton points in parallel
	elaborationStart := time.Now()
	err = s.elaborateSkeletonParallel(ctx, skeleton, input, output)
	if err != nil {
		return s.handleError(ctx, output, "Skeleton elaboration failed", err, startTime)
	}

	// Record elaboration phase
	output.ReasoningSteps = append(output.ReasoningSteps, agentcore.ReasoningStep{
		Step:        2,
		Action:      "Parallel Elaboration",
		Description: fmt.Sprintf("Elaborated %d points in parallel", len(skeleton)),
		Result:      "All points successfully elaborated",
		Duration:    time.Since(elaborationStart),
		Success:     true,
	})

	// Phase 3: Aggregate results
	aggregationStart := time.Now()
	finalAnswer := s.aggregateResults(ctx, skeleton, input)

	// Record aggregation
	output.ReasoningSteps = append(output.ReasoningSteps, agentcore.ReasoningStep{
		Step:        3,
		Action:      "Aggregate Results",
		Description: "Combined elaborated points into final answer",
		Result:      "Aggregation complete",
		Duration:    time.Since(aggregationStart),
		Success:     true,
	})

	// Set final output
	output.Status = "success"
	output.Result = finalAnswer
	output.Message = "Skeleton-of-Thought reasoning completed"
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)

	// Add metadata
	output.Metadata["skeleton_points"] = len(skeleton)
	output.Metadata["parallel_concurrency"] = s.config.MaxConcurrency
	output.Metadata["aggregation_strategy"] = s.config.AggregationStrategy
	output.Metadata["total_duration_ms"] = output.Latency.Milliseconds()

	// Trigger finish callback
	if err := s.triggerOnFinish(ctx, output); err != nil {
		return nil, err
	}

	return output, nil
}

// generateSkeleton creates the initial skeleton structure
func (s *SoTAgent) generateSkeleton(ctx context.Context, input *agentcore.AgentInput) ([]*SkeletonPoint, error) {
	prompt := fmt.Sprintf(`Break down the following task into a skeleton outline with %d-%d key points:

Task: %s

Create a structured outline where each point:
1. Has a clear, concise title
2. Can be elaborated independently (or note dependencies)
3. Contributes to solving the overall task

Format your response as:
1. [Title]: Brief description
2. [Title]: Brief description
...

If a point depends on another, add "Depends on: X" at the end.`,
		s.config.MinSkeletonPoints,
		s.config.MaxSkeletonPoints,
		input.Task)

	messages := []llm.Message{
		llm.SystemMessage("You are an expert at decomposing complex problems into structured outlines."),
		llm.UserMessage(prompt),
	}

	llmResp, err := s.llm.Chat(ctx, messages)
	if err != nil {
		return nil, err
	}

	// Parse skeleton from response
	skeleton := s.parseSkeleton(llmResp.Content)

	// Auto-decompose complex points if enabled
	if s.config.AutoDecompose {
		skeleton = s.decomposeComplexPoints(ctx, skeleton, input)
	}

	return skeleton, nil
}

// parseSkeleton parses the LLM response into skeleton points
func (s *SoTAgent) parseSkeleton(response string) []*SkeletonPoint {
	skeleton := make([]*SkeletonPoint, 0)
	lines := strings.Split(response, "\n")

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse numbered points (1. [Title]: Description)
		if matched := parseNumberedLine(line); matched != nil {
			point := &SkeletonPoint{
				ID:          fmt.Sprintf("point_%d", len(skeleton)+1),
				Title:       matched["title"],
				Description: matched["description"],
				Priority:    i,
				Status:      "pending",
				Metadata:    make(map[string]interface{}),
			}

			// Check for dependencies
			if deps := matched["dependencies"]; deps != "" {
				point.Dependencies = parseDependencies(deps)
			}

			skeleton = append(skeleton, point)
		}
	}

	// If parsing failed, create a simple skeleton
	if len(skeleton) == 0 {
		skeleton = s.createDefaultSkeleton(response)
	}

	return skeleton
}

// elaborateSkeletonParallel elaborates all skeleton points in parallel
func (s *SoTAgent) elaborateSkeletonParallel(ctx context.Context, skeleton []*SkeletonPoint, input *agentcore.AgentInput, output *agentcore.AgentOutput) error {
	// Group points by dependency level
	levels := s.groupByDependencyLevel(skeleton)

	// Process each level
	for levelIdx, level := range levels {
		// Use semaphore for concurrency control
		sem := make(chan struct{}, s.config.MaxConcurrency)
		var wg sync.WaitGroup
		errors := make(chan error, len(level))

		for _, point := range level {
			wg.Add(1)
			sem <- struct{}{} // Acquire semaphore

			go func(p *SkeletonPoint) {
				defer wg.Done()
				defer func() { <-sem }() // Release semaphore

				// Create timeout context for this elaboration
				elaborateCtx, cancel := context.WithTimeout(ctx, s.config.ElaborationTimeout)
				defer cancel()

				// Elaborate the point
				err := s.elaboratePoint(elaborateCtx, p, skeleton, input)
				if err != nil {
					errors <- agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "failed to elaborate point").
						WithComponent("sot_agent").
						WithOperation("elaborateSkeletonParallel").
						WithContext("point_id", p.ID)
				}

				// Record elaboration step
				output.ReasoningSteps = append(output.ReasoningSteps, agentcore.ReasoningStep{
					Step:        len(output.ReasoningSteps) + 1,
					Action:      fmt.Sprintf("Elaborate (Level %d)", levelIdx+1),
					Description: p.Title,
					Result:      s.truncateText(p.Elaboration, 100),
					Duration:    s.config.ElaborationTimeout,
					Success:     err == nil,
				})
			}(point)
		}

		wg.Wait()
		close(errors)

		// Check for errors
		for err := range errors {
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// elaboratePoint elaborates a single skeleton point
func (s *SoTAgent) elaboratePoint(ctx context.Context, point *SkeletonPoint, skeleton []*SkeletonPoint, input *agentcore.AgentInput) error {
	point.mu.Lock()
	point.Status = "processing"
	point.mu.Unlock()

	// Build context from dependencies
	depContext := s.buildDependencyContext(point, skeleton)

	prompt := fmt.Sprintf(`Elaborate on the following point for the task:

Task: %s

Point: %s
Description: %s

%s

Provide a detailed elaboration that fully addresses this point.`,
		input.Task,
		point.Title,
		point.Description,
		depContext)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	llmResp, err := s.llm.Chat(ctx, messages)
	if err != nil {
		point.mu.Lock()
		point.Status = "failed"
		point.mu.Unlock()
		return err
	}

	point.mu.Lock()
	point.Elaboration = llmResp.Content
	point.Status = "completed"
	point.mu.Unlock()

	return nil
}

// aggregateResults combines all elaborated points into final answer
func (s *SoTAgent) aggregateResults(ctx context.Context, skeleton []*SkeletonPoint, input *agentcore.AgentInput) string {
	switch s.config.AggregationStrategy {
	case "hierarchical":
		return s.aggregateHierarchical(skeleton)

	case "weighted":
		return s.aggregateWeighted(ctx, skeleton, input)

	case "sequential":
	default:
		return s.aggregateSequential(skeleton)
	}

	return s.aggregateSequential(skeleton)
}

// aggregateSequential combines results in sequential order
func (s *SoTAgent) aggregateSequential(skeleton []*SkeletonPoint) string {
	var result strings.Builder

	result.WriteString("Based on skeleton-of-thought analysis:\n\n")

	for i, point := range skeleton {
		point.mu.RLock()
		result.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, point.Title))
		result.WriteString(fmt.Sprintf("   %s\n\n", point.Elaboration))
		point.mu.RUnlock()
	}

	return result.String()
}

// aggregateHierarchical combines results considering sub-points
func (s *SoTAgent) aggregateHierarchical(skeleton []*SkeletonPoint) string {
	var result strings.Builder

	result.WriteString("Hierarchical analysis:\n\n")

	var writePoint func(point *SkeletonPoint, level int)
	writePoint = func(point *SkeletonPoint, level int) {
		indent := strings.Repeat("  ", level)
		point.mu.RLock()
		result.WriteString(fmt.Sprintf("%s• %s\n", indent, point.Title))
		result.WriteString(fmt.Sprintf("%s  %s\n", indent, point.Elaboration))

		for _, subPoint := range point.SubPoints {
			writePoint(subPoint, level+1)
		}
		point.mu.RUnlock()
	}

	for _, point := range skeleton {
		writePoint(point, 0)
		result.WriteString("\n")
	}

	return result.String()
}

// aggregateWeighted combines results with importance weighting
func (s *SoTAgent) aggregateWeighted(ctx context.Context, skeleton []*SkeletonPoint, input *agentcore.AgentInput) string {
	// Use LLM to synthesize with weights
	var elaborations strings.Builder
	for _, point := range skeleton {
		point.mu.RLock()
		elaborations.WriteString(fmt.Sprintf("- %s: %s\n", point.Title, point.Elaboration))
		point.mu.RUnlock()
	}

	prompt := fmt.Sprintf(`Given these elaborated points for the task "%s":

%s

Synthesize them into a coherent answer, giving appropriate weight to each point based on its importance.`,
		input.Task,
		elaborations.String())

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	llmResp, err := s.llm.Chat(ctx, messages)
	if err != nil {
		// Fallback to sequential
		return s.aggregateSequential(skeleton)
	}

	return llmResp.Content
}

// Helper methods

func (s *SoTAgent) groupByDependencyLevel(skeleton []*SkeletonPoint) [][]*SkeletonPoint {
	levels := make([][]*SkeletonPoint, 0)
	processed := make(map[string]bool)

	for len(processed) < len(skeleton) {
		level := make([]*SkeletonPoint, 0)

		for _, point := range skeleton {
			if processed[point.ID] {
				continue
			}

			// Check if all dependencies are processed
			canProcess := true
			for _, depID := range point.Dependencies {
				if !processed[depID] {
					canProcess = false
					break
				}
			}

			if canProcess {
				level = append(level, point)
			}
		}

		// Add level and mark as processed
		if len(level) > 0 {
			levels = append(levels, level)
			for _, point := range level {
				processed[point.ID] = true
			}
		} else {
			// Break if we can't make progress (circular dependency)
			break
		}
	}

	return levels
}

func (s *SoTAgent) buildDependencyContext(point *SkeletonPoint, skeleton []*SkeletonPoint) string {
	if len(point.Dependencies) == 0 {
		return ""
	}

	var context strings.Builder
	context.WriteString("Context from dependencies:\n")

	for _, depID := range point.Dependencies {
		for _, dep := range skeleton {
			if dep.ID == depID {
				dep.mu.RLock()
				if dep.Status == "completed" {
					context.WriteString(fmt.Sprintf("- %s: %s\n", dep.Title, dep.Elaboration))
				}
				dep.mu.RUnlock()
				break
			}
		}
	}

	return context.String()
}

func (s *SoTAgent) formatSkeleton(skeleton []*SkeletonPoint) string {
	var formatted strings.Builder

	for i, point := range skeleton {
		formatted.WriteString(fmt.Sprintf("%d. %s", i+1, point.Title))
		if len(point.Dependencies) > 0 {
			formatted.WriteString(fmt.Sprintf(" (depends on: %s)", strings.Join(point.Dependencies, ", ")))
		}
		formatted.WriteString("\n")
	}

	return formatted.String()
}

func (s *SoTAgent) decomposeComplexPoints(ctx context.Context, skeleton []*SkeletonPoint, input *agentcore.AgentInput) []*SkeletonPoint {
	// Simplified - in practice, check complexity and decompose if needed
	return skeleton
}

func (s *SoTAgent) createDefaultSkeleton(response string) []*SkeletonPoint {
	// Create a simple 3-point skeleton as fallback
	return []*SkeletonPoint{
		{
			ID:          "point_1",
			Title:       "Analysis",
			Description: "Analyze the problem",
			Priority:    0,
			Status:      "pending",
			Metadata:    make(map[string]interface{}),
		},
		{
			ID:           "point_2",
			Title:        "Solution",
			Description:  "Develop the solution",
			Priority:     1,
			Status:       "pending",
			Dependencies: []string{"point_1"},
			Metadata:     make(map[string]interface{}),
		},
		{
			ID:           "point_3",
			Title:        "Conclusion",
			Description:  "Summarize findings",
			Priority:     2,
			Status:       "pending",
			Dependencies: []string{"point_2"},
			Metadata:     make(map[string]interface{}),
		},
	}
}

func (s *SoTAgent) truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

// Utility functions

func parseNumberedLine(line string) map[string]string {
	// Parse lines like "1. [Title]: Description"
	// This is simplified - use proper regex in production
	result := make(map[string]string)

	// Remove numbering
	parts := strings.SplitN(line, ".", 2)
	if len(parts) < 2 {
		return nil
	}

	content := strings.TrimSpace(parts[1])

	// Extract title and description
	if strings.Contains(content, ":") {
		titleDesc := strings.SplitN(content, ":", 2)
		title := strings.TrimSpace(titleDesc[0])
		title = strings.Trim(title, "[]")

		desc := ""
		if len(titleDesc) > 1 {
			desc = strings.TrimSpace(titleDesc[1])
		}

		result["title"] = title
		result["description"] = desc

		// Check for dependencies
		if strings.Contains(desc, "Depends on:") {
			depParts := strings.SplitN(desc, "Depends on:", 2)
			result["description"] = strings.TrimSpace(depParts[0])
			result["dependencies"] = strings.TrimSpace(depParts[1])
		}
	} else {
		result["title"] = content
		result["description"] = content
	}

	return result
}

func parseDependencies(deps string) []string {
	// Parse dependency string like "1, 2" or "point_1, point_2"
	dependencies := make([]string, 0)
	parts := strings.Split(deps, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			// Convert number to point ID if needed
			if _, err := fmt.Sscanf(part, "%d", new(int)); err == nil {
				dependencies = append(dependencies, fmt.Sprintf("point_%s", part))
			} else {
				dependencies = append(dependencies, part)
			}
		}
	}

	return dependencies
}

// Stream executes Skeleton-of-Thought with streaming
func (s *SoTAgent) Stream(ctx context.Context, input *agentcore.AgentInput) (<-chan agentcore.StreamChunk[*agentcore.AgentOutput], error) {
	outChan := make(chan agentcore.StreamChunk[*agentcore.AgentOutput])

	go func() {
		defer close(outChan)

		output, err := s.Invoke(ctx, input)
		outChan <- agentcore.StreamChunk[*agentcore.AgentOutput]{
			Data:  output,
			Error: err,
			Done:  true,
		}
	}()

	return outChan, nil
}

// Error handling
func (s *SoTAgent) handleError(ctx context.Context, output *agentcore.AgentOutput, message string, err error, startTime time.Time) (*agentcore.AgentOutput, error) {
	output.Status = "failed"
	output.Message = message
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)

	_ = s.triggerOnError(ctx, err)
	return output, err
}

// Callback triggers
func (s *SoTAgent) triggerOnStart(ctx context.Context, input *agentcore.AgentInput) error {
	config := s.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnStart(ctx, input); err != nil {
			return err
		}
	}
	return nil
}

func (s *SoTAgent) triggerOnFinish(ctx context.Context, output *agentcore.AgentOutput) error {
	config := s.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnAgentFinish(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (s *SoTAgent) triggerOnError(ctx context.Context, err error) error {
	config := s.GetConfig()
	for _, cb := range config.Callbacks {
		if cbErr := cb.OnError(ctx, err); cbErr != nil {
			return cbErr
		}
	}
	return nil
}

// WithCallbacks adds callback handlers
func (s *SoTAgent) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	newAgent := *s
	newAgent.BaseAgent = s.BaseAgent.WithCallbacks(callbacks...).(*agentcore.BaseAgent)
	return &newAgent
}

// WithConfig configures the agent
func (s *SoTAgent) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	newAgent := *s
	newAgent.BaseAgent = s.BaseAgent.WithConfig(config).(*agentcore.BaseAgent)
	return &newAgent
}
