package planning

import (
	"context"
	"sync"
	"time"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	loggercore "github.com/kart-io/logger/core"
)

// PlanExecutor executes plans using agents
type PlanExecutor interface {
	// Execute executes a plan
	Execute(ctx context.Context, plan *Plan) (*PlanResult, error)

	// ExecuteStep executes a single step
	ExecuteStep(ctx context.Context, step *Step) (*StepResult, error)

	// Pause pauses plan execution
	Pause(planID string) error

	// Resume resumes plan execution
	Resume(planID string) error

	// Cancel cancels plan execution
	Cancel(planID string) error

	// GetStatus gets the current status of a plan
	GetStatus(planID string) (*PlanStatus, error)
}

// PlanResult contains the result of executing a plan
type PlanResult struct {
	PlanID         string                 `json:"plan_id"`
	Success        bool                   `json:"success"`
	CompletedSteps int                    `json:"completed_steps"`
	FailedSteps    int                    `json:"failed_steps"`
	SkippedSteps   int                    `json:"skipped_steps"`
	TotalDuration  time.Duration          `json:"total_duration"`
	StepResults    map[string]*StepResult `json:"step_results"`
	Error          error                  `json:"error,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// AgentExecutor executes plans using agents
type AgentExecutor struct {
	registry   *AgentRegistry
	logger     loggercore.Logger
	executions map[string]*ExecutionState
	mu         sync.RWMutex

	// Configuration
	maxConcurrency int
	timeout        time.Duration
	retryPolicy    RetryPolicy
}

// ExecutionState tracks the state of a plan execution
type ExecutionState struct {
	Plan        *Plan
	Status      PlanStatus
	StartTime   time.Time
	EndTime     time.Time
	StepResults map[string]*StepResult
	CurrentStep string
	PauseChan   chan struct{}
	CancelChan  chan struct{}
	mu          sync.RWMutex
}

// AgentRegistry manages available agents
type AgentRegistry struct {
	agents map[string]core.Agent
	mu     sync.RWMutex
}

// RetryPolicy defines retry behavior for failed steps
type RetryPolicy struct {
	MaxRetries    int           `json:"max_retries"`
	RetryDelay    time.Duration `json:"retry_delay"`
	BackoffFactor float64       `json:"backoff_factor"`
}

// NewAgentExecutor creates a new agent-based plan executor
func NewAgentExecutor(logger loggercore.Logger, opts ...ExecutorOption) *AgentExecutor {
	e := &AgentExecutor{
		registry:       NewAgentRegistry(),
		logger:         logger,
		executions:     make(map[string]*ExecutionState),
		maxConcurrency: 5,
		timeout:        30 * time.Minute,
		retryPolicy: RetryPolicy{
			MaxRetries:    3,
			RetryDelay:    1 * time.Second,
			BackoffFactor: 2.0,
		},
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// ExecutorOption configures the executor
type ExecutorOption func(*AgentExecutor)

// WithMaxConcurrency sets the maximum concurrent step executions
func WithMaxConcurrency(max int) ExecutorOption {
	return func(e *AgentExecutor) {
		e.maxConcurrency = max
	}
}

// WithRetryPolicy sets the retry policy
func WithRetryPolicy(policy RetryPolicy) ExecutorOption {
	return func(e *AgentExecutor) {
		e.retryPolicy = policy
	}
}

// Execute executes a plan
func (e *AgentExecutor) Execute(ctx context.Context, plan *Plan) (*PlanResult, error) {
	e.logger.Info("Starting plan execution",
		"plan_id", plan.ID,
		"goal", plan.Goal)

	// Create execution state
	state := &ExecutionState{
		Plan:        plan,
		Status:      PlanStatusExecuting,
		StartTime:   time.Now(),
		StepResults: make(map[string]*StepResult),
		PauseChan:   make(chan struct{}),
		CancelChan:  make(chan struct{}),
	}

	e.mu.Lock()
	e.executions[plan.ID] = state
	e.mu.Unlock()

	// Update plan status
	plan.Status = PlanStatusExecuting
	plan.Metrics = &PlanMetrics{
		TotalSteps: len(plan.Steps),
		StartTime:  state.StartTime,
	}

	// Execute plan
	result := e.executePlan(ctx, state)

	// Update final status
	state.mu.Lock()
	state.EndTime = time.Now()
	state.Status = PlanStatusCompleted
	if !result.Success {
		state.Status = PlanStatusFailed
	}
	state.mu.Unlock()

	// Update plan metrics
	plan.Metrics.EndTime = state.EndTime
	plan.Metrics.TotalDuration = state.EndTime.Sub(state.StartTime)
	plan.Metrics.CompletedSteps = result.CompletedSteps
	plan.Metrics.FailedSteps = result.FailedSteps
	plan.Metrics.SkippedSteps = result.SkippedSteps
	if plan.Metrics.TotalSteps > 0 {
		plan.Metrics.SuccessRate = float64(result.CompletedSteps) / float64(plan.Metrics.TotalSteps)
	}

	e.logger.Info("Plan execution completed",
		"plan_id", plan.ID,
		"success", result.Success,
		"duration", result.TotalDuration)

	return result, result.Error
}

// executePlan executes all steps in a plan
func (e *AgentExecutor) executePlan(ctx context.Context, state *ExecutionState) *PlanResult {
	result := &PlanResult{
		PlanID:      state.Plan.ID,
		StepResults: make(map[string]*StepResult),
		Metadata:    make(map[string]interface{}),
	}

	// Build execution order based on dependencies
	executionOrder := e.buildExecutionOrder(state.Plan)

	// Execute steps in order
	for _, steps := range executionOrder {
		// Execute parallel steps in this level
		if err := e.executeParallelSteps(ctx, state, steps, result); err != nil {
			result.Error = err
			result.Success = false
			return result
		}

		// Check for pause or cancel
		select {
		case <-state.PauseChan:
			e.handlePause(ctx, state)
		case <-state.CancelChan:
			result.Error = agentErrors.New(agentErrors.CodeContextCanceled, "execution cancelled").
				WithComponent("agent_executor").
				WithOperation("execute_plan").
				WithContext("plan_id", state.Plan.ID)
			result.Success = false
			return result
		default:
			// Continue execution
		}
	}

	// Calculate final result
	result.Success = result.FailedSteps == 0
	result.TotalDuration = time.Since(state.StartTime)

	return result
}

// buildExecutionOrder builds the execution order based on dependencies
func (e *AgentExecutor) buildExecutionOrder(plan *Plan) [][]*Step {
	// Calculate levels using topological sort
	levels := make(map[int][]*Step)
	level := make(map[string]int)

	// Calculate level for each step
	for _, step := range plan.Steps {
		e.calculateStepLevel(step, plan, level, 0)
	}

	// Group steps by level
	maxLevel := 0
	for _, step := range plan.Steps {
		l := level[step.ID]
		if l > maxLevel {
			maxLevel = l
		}
		levels[l] = append(levels[l], step)
	}

	// Build execution order
	var executionOrder [][]*Step
	for i := 0; i <= maxLevel; i++ {
		if steps, ok := levels[i]; ok {
			executionOrder = append(executionOrder, steps)
		}
	}

	return executionOrder
}

// calculateStepLevel calculates the execution level of a step
func (e *AgentExecutor) calculateStepLevel(step *Step, plan *Plan, level map[string]int, currentLevel int) int {
	if l, exists := level[step.ID]; exists {
		return l
	}

	maxDepLevel := currentLevel
	if deps, ok := plan.Dependencies[step.ID]; ok {
		for _, depID := range deps {
			// Find the dependency step
			for _, s := range plan.Steps {
				if s.ID == depID {
					depLevel := e.calculateStepLevel(s, plan, level, currentLevel+1)
					if depLevel > maxDepLevel {
						maxDepLevel = depLevel
					}
					break
				}
			}
		}
	}

	level[step.ID] = maxDepLevel
	return maxDepLevel
}

// executeParallelSteps executes steps that can run in parallel
func (e *AgentExecutor) executeParallelSteps(ctx context.Context, state *ExecutionState, steps []*Step, result *PlanResult) error {
	if len(steps) == 0 {
		return nil
	}

	// Use semaphore to limit concurrency
	sem := make(chan struct{}, e.maxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstError error

	for _, step := range steps {
		// Check if step should be skipped
		if e.shouldSkipStep(step, state) {
			result.SkippedSteps++
			continue
		}

		wg.Add(1)
		go func(s *Step) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Execute step with retry
			stepResult := e.executeStepWithRetry(ctx, state, s)

			// Update results
			mu.Lock()
			result.StepResults[s.ID] = stepResult
			state.StepResults[s.ID] = stepResult

			if stepResult.Success {
				result.CompletedSteps++
			} else {
				result.FailedSteps++
				if firstError == nil {
					firstError = agentErrors.New(agentErrors.CodeAgentExecution, "step execution failed").
						WithComponent("agent_executor").
						WithOperation("execute_step").
						WithContext("step_id", s.ID).
						WithContext("error", stepResult.Error)
				}
			}
			mu.Unlock()

			// Update step status
			if stepResult.Success {
				s.Status = StepStatusCompleted
			} else {
				s.Status = StepStatusFailed
			}
		}(step)
	}

	wg.Wait()

	return firstError
}

// shouldSkipStep determines if a step should be skipped
func (e *AgentExecutor) shouldSkipStep(step *Step, state *ExecutionState) bool {
	// Skip if dependencies failed
	if deps, ok := state.Plan.Dependencies[step.ID]; ok {
		for _, depID := range deps {
			if result, ok := state.StepResults[depID]; ok && !result.Success {
				e.logger.Info("Skipping step due to failed dependency",
					"step", step.ID,
					"failed_dep", depID)
				return true
			}
		}
	}

	return false
}

// executeStepWithRetry executes a step with retry logic
func (e *AgentExecutor) executeStepWithRetry(ctx context.Context, state *ExecutionState, step *Step) *StepResult {
	var lastResult *StepResult
	retryDelay := e.retryPolicy.RetryDelay

	for attempt := 0; attempt <= e.retryPolicy.MaxRetries; attempt++ {
		if attempt > 0 {
			e.logger.Info("Retrying step execution",
				"step", step.ID,
				"attempt", attempt)
			time.Sleep(retryDelay)
			retryDelay = time.Duration(float64(retryDelay) * e.retryPolicy.BackoffFactor)
		}

		// Update current step
		state.mu.Lock()
		state.CurrentStep = step.ID
		state.mu.Unlock()

		// Execute step
		lastResult = e.executeStep(ctx, step)

		if lastResult.Success {
			return lastResult
		}

		// Check if error is retryable
		if !e.isRetryableError(lastResult.Error) {
			break
		}
	}

	return lastResult
}

// executeStep executes a single step
func (e *AgentExecutor) executeStep(ctx context.Context, step *Step) *StepResult {
	startTime := time.Now()

	e.logger.Info("Executing step",
		"step_id", step.ID,
		"name", step.Name,
		"type", string(step.Type))

	// Mark step as executing
	step.Status = StepStatusExecuting

	// Select agent based on step type and parameters
	agent := e.selectAgent(step)
	if agent == nil {
		return &StepResult{
			Success: false,
			Error: agentErrors.New(agentErrors.CodeAgentNotFound, "no agent available for step type").
				WithComponent("agent_executor").
				WithOperation("select_agent").
				WithContext("step_type", string(step.Type)).Error(),
			Duration:  time.Since(startTime),
			Timestamp: time.Now(),
		}
	}

	// Prepare input for agent
	input := &core.AgentInput{
		Task:        step.Name,
		Instruction: step.Description,
		Context: map[string]interface{}{
			"step":        step,
			"description": step.Description,
			"parameters":  step.Parameters,
		},
		Options: core.DefaultAgentOptions(),
	}

	// Execute with timeout
	execCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	// Execute agent
	output, err := agent.Invoke(execCtx, input)

	// Build result
	result := &StepResult{
		Success:   err == nil,
		Duration:  time.Since(startTime),
		Timestamp: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	if err != nil {
		result.Error = err.Error()
	} else if output != nil {
		result.Output = output.Result
		result.Metadata = output.Metadata
	}

	// Validate against expected outcome
	if step.Expected != nil && result.Success {
		result.Success = e.validateOutcome(result, step.Expected)
	}

	step.Result = result

	e.logger.Info("Step execution completed",
		"step_id", step.ID,
		"success", result.Success,
		"duration", result.Duration)

	return result
}

// ExecuteStep executes a single step (implements interface)
func (e *AgentExecutor) ExecuteStep(ctx context.Context, step *Step) (*StepResult, error) {
	result := e.executeStep(ctx, step)
	if !result.Success {
		return result, agentErrors.New(agentErrors.CodeAgentExecution, result.Error).
			WithComponent("agent_executor").
			WithOperation("execute_step").
			WithContext("step_id", step.ID)
	}
	return result, nil
}

// selectAgent selects an appropriate agent for a step
func (e *AgentExecutor) selectAgent(step *Step) core.Agent {
	// First check if agent is explicitly specified
	if step.Agent != "" {
		return e.registry.GetAgent(step.Agent)
	}

	// Select based on step type
	switch step.Type {
	case StepTypeAnalysis:
		return e.registry.GetAgent("analysis_agent")
	case StepTypeDecision:
		return e.registry.GetAgent("decision_agent")
	case StepTypeAction:
		return e.registry.GetAgent("action_agent")
	case StepTypeValidation:
		return e.registry.GetAgent("validation_agent")
	case StepTypeOptimization:
		return e.registry.GetAgent("optimization_agent")
	default:
		return e.registry.GetAgent("default_agent")
	}
}

// validateOutcome validates step result against expected outcome
func (e *AgentExecutor) validateOutcome(result *StepResult, expected *ExpectedOutcome) bool {
	// Simple validation - can be made more sophisticated
	if result.Output == nil {
		return false
	}

	// Check criteria
	for _, criterion := range expected.Criteria {
		// This would be more sophisticated in production
		if !e.checkCriterion(result, criterion) {
			return false
		}
	}

	return true
}

// checkCriterion checks if a result meets a criterion
func (e *AgentExecutor) checkCriterion(result *StepResult, criterion string) bool {
	// Simplified criterion checking
	// In production, this would parse and evaluate complex criteria
	return true
}

// isRetryableError determines if an error is retryable
func (e *AgentExecutor) isRetryableError(errorMsg string) bool {
	// Define retryable error patterns
	retryablePatterns := []string{
		"timeout",
		"temporary",
		"unavailable",
		"connection",
	}

	for _, pattern := range retryablePatterns {
		if contains(errorMsg, pattern) {
			return true
		}
	}

	return false
}

// handlePause handles plan pause
func (e *AgentExecutor) handlePause(ctx context.Context, state *ExecutionState) {
	e.logger.Info("Plan execution paused", "plan_id", state.Plan.ID)

	state.mu.Lock()
	state.Status = PlanStatusExecuting // Keep as executing, but paused
	state.mu.Unlock()

	// Wait for resume or cancel
	select {
	case <-state.PauseChan: // Resume
		e.logger.Info("Plan execution resumed", "plan_id", state.Plan.ID)
	case <-state.CancelChan: // Cancel
		// Will be handled in main execution loop
	case <-ctx.Done():
		// Context cancelled
	}
}

// Pause pauses plan execution
func (e *AgentExecutor) Pause(planID string) error {
	e.mu.RLock()
	state, exists := e.executions[planID]
	e.mu.RUnlock()

	if !exists {
		return agentErrors.New(agentErrors.CodePlanNotFound, "plan not found").
			WithComponent("agent_executor").
			WithOperation("pause").
			WithContext("plan_id", planID)
	}

	select {
	case state.PauseChan <- struct{}{}:
		return nil
	default:
		return agentErrors.New(agentErrors.CodeAgentExecution, "plan is not running").
			WithComponent("agent_executor").
			WithOperation("pause").
			WithContext("plan_id", planID)
	}
}

// Resume resumes plan execution
func (e *AgentExecutor) Resume(planID string) error {
	e.mu.RLock()
	state, exists := e.executions[planID]
	e.mu.RUnlock()

	if !exists {
		return agentErrors.New(agentErrors.CodePlanNotFound, "plan not found").
			WithComponent("agent_executor").
			WithOperation("resume").
			WithContext("plan_id", planID)
	}

	select {
	case state.PauseChan <- struct{}{}:
		return nil
	default:
		return agentErrors.New(agentErrors.CodeAgentExecution, "plan is not paused").
			WithComponent("agent_executor").
			WithOperation("resume").
			WithContext("plan_id", planID)
	}
}

// Cancel cancels plan execution
func (e *AgentExecutor) Cancel(planID string) error {
	e.mu.RLock()
	state, exists := e.executions[planID]
	e.mu.RUnlock()

	if !exists {
		return agentErrors.New(agentErrors.CodePlanNotFound, "plan not found").
			WithComponent("agent_executor").
			WithOperation("cancel").
			WithContext("plan_id", planID)
	}

	select {
	case state.CancelChan <- struct{}{}:
		return nil
	default:
		return agentErrors.New(agentErrors.CodeAgentExecution, "plan is not running").
			WithComponent("agent_executor").
			WithOperation("cancel").
			WithContext("plan_id", planID)
	}
}

// GetStatus gets the current status of a plan
func (e *AgentExecutor) GetStatus(planID string) (*PlanStatus, error) {
	e.mu.RLock()
	state, exists := e.executions[planID]
	e.mu.RUnlock()

	if !exists {
		return nil, agentErrors.New(agentErrors.CodePlanNotFound, "plan not found").
			WithComponent("agent_executor").
			WithOperation("get_status").
			WithContext("plan_id", planID)
	}

	state.mu.RLock()
	status := state.Status
	state.mu.RUnlock()

	return &status, nil
}

// NewAgentRegistry creates a new agent registry
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		agents: make(map[string]core.Agent),
	}
}

// RegisterAgent registers an agent
func (r *AgentRegistry) RegisterAgent(name string, agent core.Agent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[name] = agent
}

// GetAgent gets an agent by name
func (r *AgentRegistry) GetAgent(name string) core.Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agents[name]
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 &&
		(s == substr || len(s) > len(substr) &&
			(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr))
}
