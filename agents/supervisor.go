package agents

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

// SupervisorAgent coordinates multiple sub-agents to handle complex tasks
type SupervisorAgent struct {
	*core.BaseAgent
	llm              llm.Client
	SubAgents        map[string]core.Agent
	Router           AgentRouter
	Orchestrator     *TaskOrchestrator
	ResultAggregator *ResultAggregator
	config           *SupervisorConfig
	metrics          *SupervisorMetrics
	mu               sync.RWMutex
}

// SupervisorConfig configures the supervisor agent
type SupervisorConfig struct {
	// MaxConcurrentAgents limits concurrent sub-agent executions
	MaxConcurrentAgents int

	// Timeout for sub-agent executions
	SubAgentTimeout time.Duration

	// RetryPolicy for failed sub-agent tasks
	RetryPolicy *tools.RetryPolicy

	// EnableCaching enables result caching
	EnableCaching bool

	// CacheTTL for cached results
	CacheTTL time.Duration

	// EnableMetrics enables metrics collection
	EnableMetrics bool

	// RoutingStrategy defines how to route tasks
	RoutingStrategy RoutingStrategy

	// AggregationStrategy defines how to aggregate results
	AggregationStrategy AggregationStrategy
}

// DefaultSupervisorConfig returns default configuration
func DefaultSupervisorConfig() *SupervisorConfig {
	return &SupervisorConfig{
		MaxConcurrentAgents: 5,
		SubAgentTimeout:     30 * time.Second,
		RetryPolicy: &tools.RetryPolicy{
			MaxRetries:   3,
			InitialDelay: 1 * time.Second,
			MaxDelay:     10 * time.Second,
			Multiplier:   2.0,
		},
		EnableCaching:       true,
		CacheTTL:            5 * time.Minute,
		EnableMetrics:       true,
		RoutingStrategy:     StrategyLLMBased,
		AggregationStrategy: StrategyMerge,
	}
}

// RoutingStrategy defines how tasks are routed to sub-agents
type RoutingStrategy string

const (
	StrategyLLMBased   RoutingStrategy = "llm"        // Use LLM to decide
	StrategyRuleBased  RoutingStrategy = "rules"      // Use predefined rules
	StrategyRoundRobin RoutingStrategy = "round"      // Round-robin distribution
	StrategyCapability RoutingStrategy = "capability" // Based on agent capabilities
)

// AggregationStrategy defines how results are aggregated
type AggregationStrategy string

const (
	StrategyMerge     AggregationStrategy = "merge"     // Merge all results
	StrategyBest      AggregationStrategy = "best"      // Select best result
	StrategyConsensus AggregationStrategy = "consensus" // Majority vote
	StrategyHierarchy AggregationStrategy = "hierarchy" // Hierarchical aggregation
)

// AgentRouter decides which sub-agent to use for a task
type AgentRouter interface {
	Route(ctx context.Context, task Task, agents map[string]core.Agent) (string, error)
	GetCapabilities(agentName string) []string
	UpdateRouting(agentName string, performance float64)
}

// Task represents a task to be executed by an agent
type Task struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Input       interface{}            `json:"input"`
	Priority    int                    `json:"priority"`
	Deadline    *time.Time             `json:"deadline,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// TaskResult represents the result of a task execution
type TaskResult struct {
	TaskID      string                 `json:"task_id"`
	AgentName   string                 `json:"agent_name"`
	Output      interface{}            `json:"output"`
	Error       error                  `json:"-"`
	ErrorString string                 `json:"error,omitempty"`
	StartTime   time.Time              `json:"start_time"`
	EndTime     time.Time              `json:"end_time"`
	Duration    time.Duration          `json:"duration"`
	Confidence  float64                `json:"confidence"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// NewSupervisorAgent creates a new supervisor agent
func NewSupervisorAgent(llm llm.Client, config *SupervisorConfig) *SupervisorAgent {
	if config == nil {
		config = DefaultSupervisorConfig()
	}

	supervisor := &SupervisorAgent{
		BaseAgent:        &core.BaseAgent{},
		llm:              llm,
		SubAgents:        make(map[string]core.Agent),
		Orchestrator:     NewTaskOrchestrator(config.MaxConcurrentAgents),
		ResultAggregator: NewResultAggregator(config.AggregationStrategy),
		config:           config,
		metrics:          NewSupervisorMetrics(),
	}

	// Initialize router based on strategy
	switch config.RoutingStrategy {
	case StrategyLLMBased:
		supervisor.Router = NewLLMRouter(llm)
	case StrategyRuleBased:
		supervisor.Router = NewRuleBasedRouter()
	case StrategyRoundRobin:
		supervisor.Router = NewRoundRobinRouter()
	case StrategyCapability:
		supervisor.Router = NewCapabilityRouter()
	default:
		supervisor.Router = NewLLMRouter(llm)
	}

	return supervisor
}

// AddSubAgent adds a sub-agent to the supervisor
func (s *SupervisorAgent) AddSubAgent(name string, agent core.Agent) *SupervisorAgent {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.SubAgents[name] = agent
	return s
}

// RemoveSubAgent removes a sub-agent
func (s *SupervisorAgent) RemoveSubAgent(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.SubAgents, name)
}

// Run executes a complex task by coordinating sub-agents
func (s *SupervisorAgent) Run(ctx context.Context, input interface{}) (*core.AgentOutput, error) {
	// Start metrics collection
	startTime := time.Now()
	s.metrics.IncrementTotalTasks()

	// Parse input into tasks
	tasks, err := s.parseTasks(ctx, input)
	if err != nil {
		s.metrics.IncrementFailedTasks()
		return nil, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "failed to parse tasks").
			WithComponent("supervisor_agent").
			WithOperation("Run")
	}

	// Create execution plan
	plan := s.Orchestrator.CreateExecutionPlan(tasks)

	// Execute tasks
	results := s.executePlan(ctx, plan)

	// Aggregate results
	finalResult := s.ResultAggregator.Aggregate(results)

	// Update metrics
	s.metrics.UpdateExecutionTime(time.Since(startTime))
	s.metrics.IncrementSuccessfulTasks()

	return &core.AgentOutput{
		Result: finalResult,
		Metadata: map[string]interface{}{
			"tasks_executed": len(tasks),
			"agents_used":    s.getUsedAgents(results),
			"execution_time": time.Since(startTime),
			"execution_plan": plan,
		},
	}, nil
}

// parseTasks converts input into a list of tasks
func (s *SupervisorAgent) parseTasks(ctx context.Context, input interface{}) ([]Task, error) {
	// Use LLM to decompose complex input into tasks
	prompt := fmt.Sprintf(`
		Decompose the following request into individual tasks:
		%v

		Return tasks as a structured list with type, description, and priority.
		Available agent types: %s
	`, input, s.getAgentTypes())

	response, err := s.llm.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.SystemMessage("You are a task decomposition expert."),
			llm.UserMessage(prompt),
		},
	})
	if err != nil {
		return nil, err
	}

	// Parse response into tasks (simplified for demo)
	return s.parseTaskResponse(response.Content), nil
}

// parseTaskResponse parses LLM response into tasks
func (s *SupervisorAgent) parseTaskResponse(response string) []Task {
	tasks := []Task{}

	// Simple parsing logic (in production, use structured output)
	lines := strings.Split(response, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			tasks = append(tasks, Task{
				ID:          fmt.Sprintf("task_%d", i),
				Type:        "general",
				Description: line,
				Priority:    len(lines) - i, // Higher priority for earlier tasks
			})
		}
	}

	return tasks
}

// executePlan executes the task execution plan
func (s *SupervisorAgent) executePlan(ctx context.Context, plan *ExecutionPlan) []TaskResult {
	// Count total tasks
	totalTasks := 0
	for _, stage := range plan.Stages {
		totalTasks += len(stage.Tasks)
	}

	results := make([]TaskResult, 0, totalTasks)
	resultsChan := make(chan TaskResult, totalTasks)

	// Execute tasks according to plan
	var wg sync.WaitGroup
	sem := make(chan struct{}, s.config.MaxConcurrentAgents)

	for _, stage := range plan.Stages {
		for _, task := range stage.Tasks {
			wg.Add(1)
			go func(t Task) {
				defer wg.Done()

				// Acquire semaphore
				sem <- struct{}{}
				defer func() { <-sem }()

				// Execute task
				result := s.executeTask(ctx, t)
				resultsChan <- result
			}(task)
		}

		// Wait for stage to complete if sequential
		if stage.Sequential {
			wg.Wait()
		}
	}

	// Wait for all tasks to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results
	for result := range resultsChan {
		results = append(results, result)
	}

	return results
}

// executeTask executes a single task
func (s *SupervisorAgent) executeTask(ctx context.Context, task Task) TaskResult {
	startTime := time.Now()
	result := TaskResult{
		TaskID:    task.ID,
		StartTime: startTime,
	}

	// Create timeout context
	taskCtx, cancel := context.WithTimeout(ctx, s.config.SubAgentTimeout)
	defer cancel()

	// Route task to appropriate agent
	agentName, err := s.Router.Route(taskCtx, task, s.SubAgents)
	if err != nil {
		result.Error = err
		result.ErrorString = err.Error()
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(startTime)
		return result
	}

	result.AgentName = agentName

	// Get the selected agent
	s.mu.RLock()
	agent, exists := s.SubAgents[agentName]
	s.mu.RUnlock()

	if !exists {
		result.Error = agentErrors.New(agentErrors.CodeAgentNotFound, "agent not found").
			WithComponent("supervisor_agent").
			WithOperation("executeTask").
			WithContext("agent_name", agentName)
		result.ErrorString = result.Error.Error()
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(startTime)
		return result
	}

	// Execute task with retry
	var output interface{}
	var execErr error

	for attempt := 0; attempt <= s.config.RetryPolicy.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := s.config.RetryPolicy.InitialDelay
			for i := 1; i < attempt; i++ {
				delay = time.Duration(float64(delay) * s.config.RetryPolicy.Multiplier)
				if delay > s.config.RetryPolicy.MaxDelay {
					delay = s.config.RetryPolicy.MaxDelay
					break
				}
			}
			time.Sleep(delay)
		}

		// Create agent input
		agentInput := &core.AgentInput{
			Task:        task.Description,
			Instruction: fmt.Sprintf("Execute %s task", task.Type),
			Context:     map[string]interface{}{"task": task},
			SessionID:   fmt.Sprintf("%s-%s", task.ID, agentName),
			Timestamp:   time.Now(),
		}
		agentOutput, err := agent.Invoke(taskCtx, agentInput)
		if err == nil {
			output = agentOutput.Result
			break
		}

		execErr = err

		// Check if error is retryable
		if !s.isRetryableError(err) {
			break
		}
	}

	result.Output = output
	result.Error = execErr
	if execErr != nil {
		result.ErrorString = execErr.Error()
	}
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(startTime)

	// Update router with performance feedback
	if execErr == nil {
		result.Confidence = 0.9 // High confidence for success
		s.Router.UpdateRouting(agentName, 1.0)
	} else {
		result.Confidence = 0.1 // Low confidence for failure
		s.Router.UpdateRouting(agentName, 0.0)
	}

	return result
}

// isRetryableError checks if an error is retryable
func (s *SupervisorAgent) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	for _, retryable := range s.config.RetryPolicy.RetryableErrors {
		if strings.Contains(errStr, retryable) {
			return true
		}
	}

	return false
}

// getAgentTypes returns a list of available agent types
func (s *SupervisorAgent) getAgentTypes() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	types := []string{}
	for name := range s.SubAgents {
		types = append(types, name)
	}

	return strings.Join(types, ", ")
}

// getUsedAgents extracts the list of agents used in results
func (s *SupervisorAgent) getUsedAgents(results []TaskResult) []string {
	agentSet := make(map[string]bool)
	for _, result := range results {
		if result.AgentName != "" {
			agentSet[result.AgentName] = true
		}
	}

	agents := []string{}
	for agent := range agentSet {
		agents = append(agents, agent)
	}

	return agents
}

// GetMetrics returns supervisor metrics
func (s *SupervisorAgent) GetMetrics() map[string]interface{} {
	return s.metrics.GetSnapshot()
}

// TaskOrchestrator creates execution plans for tasks
type TaskOrchestrator struct {
	maxConcurrent int
}

// NewTaskOrchestrator creates a new task orchestrator
func NewTaskOrchestrator(maxConcurrent int) *TaskOrchestrator {
	return &TaskOrchestrator{
		maxConcurrent: maxConcurrent,
	}
}

// ExecutionPlan represents a plan for executing tasks
type ExecutionPlan struct {
	Stages []ExecutionStage `json:"stages"`
}

// ExecutionStage represents a stage in the execution plan
type ExecutionStage struct {
	ID         string `json:"id"`
	Tasks      []Task `json:"tasks"`
	Sequential bool   `json:"sequential"`
}

// CreateExecutionPlan creates an execution plan for tasks
func (o *TaskOrchestrator) CreateExecutionPlan(tasks []Task) *ExecutionPlan {
	// Sort tasks by priority
	sortedTasks := make([]Task, len(tasks))
	copy(sortedTasks, tasks)

	// Group tasks into stages based on dependencies and priority
	stages := []ExecutionStage{}

	// Simple strategy: group by priority levels
	currentPriority := -1
	var currentStage *ExecutionStage

	for _, task := range sortedTasks {
		if task.Priority != currentPriority {
			if currentStage != nil {
				stages = append(stages, *currentStage)
			}
			currentStage = &ExecutionStage{
				ID:         fmt.Sprintf("stage_%d", len(stages)),
				Tasks:      []Task{},
				Sequential: false,
			}
			currentPriority = task.Priority
		}
		currentStage.Tasks = append(currentStage.Tasks, task)
	}

	if currentStage != nil && len(currentStage.Tasks) > 0 {
		stages = append(stages, *currentStage)
	}

	return &ExecutionPlan{
		Stages: stages,
	}
}

// ResultAggregator aggregates results from multiple agents
type ResultAggregator struct {
	strategy AggregationStrategy
}

// NewResultAggregator creates a new result aggregator
func NewResultAggregator(strategy AggregationStrategy) *ResultAggregator {
	return &ResultAggregator{
		strategy: strategy,
	}
}

// Aggregate combines multiple task results
func (a *ResultAggregator) Aggregate(results []TaskResult) interface{} {
	switch a.strategy {
	case StrategyMerge:
		return a.mergeResults(results)
	case StrategyBest:
		return a.selectBest(results)
	case StrategyConsensus:
		return a.findConsensus(results)
	case StrategyHierarchy:
		return a.hierarchicalAggregate(results)
	default:
		return a.mergeResults(results)
	}
}

// mergeResults combines all results into a single output
func (a *ResultAggregator) mergeResults(results []TaskResult) interface{} {
	merged := map[string]interface{}{
		"results":    []interface{}{},
		"errors":     []string{},
		"confidence": 0.0,
	}

	totalConfidence := 0.0
	successCount := 0

	for _, result := range results {
		if result.Error == nil {
			merged["results"] = append(merged["results"].([]interface{}), result.Output)
			totalConfidence += result.Confidence
			successCount++
		} else {
			merged["errors"] = append(merged["errors"].([]string), result.ErrorString)
		}
	}

	if successCount > 0 {
		merged["confidence"] = totalConfidence / float64(successCount)
	}

	return merged
}

// selectBest selects the best result based on confidence
func (a *ResultAggregator) selectBest(results []TaskResult) interface{} {
	var best *TaskResult
	maxConfidence := 0.0

	for i, result := range results {
		if result.Error == nil && result.Confidence > maxConfidence {
			best = &results[i]
			maxConfidence = result.Confidence
		}
	}

	if best != nil {
		return best.Output
	}

	return nil
}

// findConsensus finds consensus among results
func (a *ResultAggregator) findConsensus(results []TaskResult) interface{} {
	// Count occurrences of each result
	resultCounts := make(map[string]int)
	resultMap := make(map[string]interface{})

	for _, result := range results {
		if result.Error == nil {
			key := fmt.Sprintf("%v", result.Output)
			resultCounts[key]++
			resultMap[key] = result.Output
		}
	}

	// Find the most common result
	maxCount := 0
	var consensus string

	for key, count := range resultCounts {
		if count > maxCount {
			maxCount = count
			consensus = key
		}
	}

	if consensus != "" {
		return resultMap[consensus]
	}

	return nil
}

// hierarchicalAggregate performs hierarchical aggregation
func (a *ResultAggregator) hierarchicalAggregate(results []TaskResult) interface{} {
	// Group results by agent type
	grouped := make(map[string][]TaskResult)

	for _, result := range results {
		grouped[result.AgentName] = append(grouped[result.AgentName], result)
	}

	// Aggregate within groups first
	groupAggregates := make(map[string]interface{})
	for agentName, groupResults := range grouped {
		groupAggregates[agentName] = a.mergeResults(groupResults)
	}

	return groupAggregates
}

// SupervisorMetrics tracks metrics for the supervisor
type SupervisorMetrics struct {
	totalTasks      int64
	successfulTasks int64
	failedTasks     int64
	totalTime       time.Duration
	averageTime     time.Duration
	mu              sync.RWMutex
}

// NewSupervisorMetrics creates new metrics tracker
func NewSupervisorMetrics() *SupervisorMetrics {
	return &SupervisorMetrics{}
}

// IncrementTotalTasks increments total tasks counter
func (m *SupervisorMetrics) IncrementTotalTasks() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalTasks++
}

// IncrementSuccessfulTasks increments successful tasks counter
func (m *SupervisorMetrics) IncrementSuccessfulTasks() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.successfulTasks++
}

// IncrementFailedTasks increments failed tasks counter
func (m *SupervisorMetrics) IncrementFailedTasks() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failedTasks++
}

// UpdateExecutionTime updates execution time metrics
func (m *SupervisorMetrics) UpdateExecutionTime(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalTime += duration
	if m.totalTasks > 0 {
		m.averageTime = m.totalTime / time.Duration(m.totalTasks)
	}
}

// GetSnapshot returns a snapshot of metrics
func (m *SupervisorMetrics) GetSnapshot() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"total_tasks":      m.totalTasks,
		"successful_tasks": m.successfulTasks,
		"failed_tasks":     m.failedTasks,
		"total_time":       m.totalTime,
		"average_time":     m.averageTime,
		"success_rate":     float64(m.successfulTasks) / float64(m.totalTasks),
	}
}
