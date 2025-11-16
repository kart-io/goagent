// Package planning provides task decomposition and strategy planning capabilities for agents.
package planning

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// Plan represents a structured plan for achieving a goal
type Plan struct {
	ID           string                 `json:"id"`
	Goal         string                 `json:"goal"`
	Strategy     string                 `json:"strategy"`
	Steps        []*Step                `json:"steps"`
	Dependencies map[string][]string    `json:"dependencies"` // step ID -> dependent step IDs
	Context      map[string]interface{} `json:"context"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	Status       PlanStatus             `json:"status"`
	Metrics      *PlanMetrics           `json:"metrics,omitempty"`
}

// Step represents a single step in a plan
type Step struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	Description       string                 `json:"description"`
	Type              StepType               `json:"type"`
	Agent             string                 `json:"agent,omitempty"` // Agent to execute this step
	Parameters        map[string]interface{} `json:"parameters,omitempty"`
	Expected          *ExpectedOutcome       `json:"expected,omitempty"`
	Priority          int                    `json:"priority"`
	EstimatedDuration time.Duration          `json:"estimated_duration,omitempty"`
	Status            StepStatus             `json:"status"`
	Result            *StepResult            `json:"result,omitempty"`
}

// ExpectedOutcome defines what we expect from a step
type ExpectedOutcome struct {
	Description string                 `json:"description"`
	Criteria    []string               `json:"criteria"`
	Metrics     map[string]interface{} `json:"metrics,omitempty"`
}

// StepResult contains the execution result of a step
type StepResult struct {
	Success   bool                   `json:"success"`
	Output    interface{}            `json:"output"`
	Error     string                 `json:"error,omitempty"`
	Duration  time.Duration          `json:"duration"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// StepType defines the type of a planning step
type StepType string

const (
	StepTypeAnalysis     StepType = "analysis"
	StepTypeDecision     StepType = "decision"
	StepTypeAction       StepType = "action"
	StepTypeValidation   StepType = "validation"
	StepTypeOptimization StepType = "optimization"
)

// PlanStatus represents the status of a plan
type PlanStatus string

const (
	PlanStatusDraft     PlanStatus = "draft"
	PlanStatusReady     PlanStatus = "ready"
	PlanStatusExecuting PlanStatus = "executing"
	PlanStatusCompleted PlanStatus = "completed"
	PlanStatusFailed    PlanStatus = "failed"
	PlanStatusCancelled PlanStatus = "cancelled"
)

// StepStatus represents the status of a step
type StepStatus string

const (
	StepStatusPending   StepStatus = "pending"
	StepStatusReady     StepStatus = "ready"
	StepStatusExecuting StepStatus = "executing"
	StepStatusCompleted StepStatus = "completed"
	StepStatusFailed    StepStatus = "failed"
	StepStatusSkipped   StepStatus = "skipped"
)

// PlanMetrics tracks plan execution metrics
type PlanMetrics struct {
	TotalSteps     int           `json:"total_steps"`
	CompletedSteps int           `json:"completed_steps"`
	FailedSteps    int           `json:"failed_steps"`
	SkippedSteps   int           `json:"skipped_steps"`
	TotalDuration  time.Duration `json:"total_duration"`
	StartTime      time.Time     `json:"start_time"`
	EndTime        time.Time     `json:"end_time,omitempty"`
	SuccessRate    float64       `json:"success_rate"`
}

// Planner interface for creating and managing plans
type Planner interface {
	// CreatePlan creates a plan for achieving a goal
	CreatePlan(ctx context.Context, goal string, constraints PlanConstraints) (*Plan, error)

	// RefinePlan refines an existing plan based on feedback
	RefinePlan(ctx context.Context, plan *Plan, feedback string) (*Plan, error)

	// DecomposePlan breaks down a plan into more detailed steps
	DecomposePlan(ctx context.Context, plan *Plan, step *Step) ([]*Step, error)

	// OptimizePlan optimizes a plan for efficiency
	OptimizePlan(ctx context.Context, plan *Plan) (*Plan, error)

	// ValidatePlan validates that a plan is feasible
	ValidatePlan(ctx context.Context, plan *Plan) (bool, []string, error)
}

// PlanConstraints defines constraints for plan creation
type PlanConstraints struct {
	MaxSteps       int                    `json:"max_steps,omitempty"`
	MaxDuration    time.Duration          `json:"max_duration,omitempty"`
	RequiredSteps  []string               `json:"required_steps,omitempty"`
	ForbiddenSteps []string               `json:"forbidden_steps,omitempty"`
	Resources      map[string]interface{} `json:"resources,omitempty"`
	Priority       int                    `json:"priority,omitempty"`
}

// SmartPlanner uses LLM and memory to create intelligent plans
type SmartPlanner struct {
	llm        llm.Client
	memory     interfaces.MemoryManager
	strategies map[string]PlanStrategy
	validators []PlanValidator
	optimizer  PlanOptimizer
	mu         sync.RWMutex

	// Configuration
	maxPlanDepth int
	maxRetries   int
	timeout      time.Duration
}

// NewSmartPlanner creates a new smart planner
func NewSmartPlanner(llmClient llm.Client, mem interfaces.MemoryManager, opts ...PlannerOption) *SmartPlanner {
	p := &SmartPlanner{
		llm:          llmClient,
		memory:       mem,
		strategies:   make(map[string]PlanStrategy),
		validators:   []PlanValidator{},
		maxPlanDepth: 5,
		maxRetries:   3,
		timeout:      5 * time.Minute,
	}

	// Apply options
	for _, opt := range opts {
		opt(p)
	}

	// Register default strategies
	p.RegisterStrategy("decomposition", &DecompositionStrategy{})
	p.RegisterStrategy("backward_chaining", &BackwardChainingStrategy{})
	p.RegisterStrategy("hierarchical", &HierarchicalStrategy{})

	// Add default validators
	p.AddValidator(&DependencyValidator{})
	p.AddValidator(&ResourceValidator{})
	p.AddValidator(&TimeValidator{})

	return p
}

// PlannerOption configures the planner
type PlannerOption func(*SmartPlanner)

// WithMaxDepth sets the maximum planning depth
func WithMaxDepth(depth int) PlannerOption {
	return func(p *SmartPlanner) {
		p.maxPlanDepth = depth
	}
}

// WithTimeout sets the planning timeout
func WithTimeout(timeout time.Duration) PlannerOption {
	return func(p *SmartPlanner) {
		p.timeout = timeout
	}
}

// WithOptimizer sets the plan optimizer
func WithOptimizer(optimizer PlanOptimizer) PlannerOption {
	return func(p *SmartPlanner) {
		p.optimizer = optimizer
	}
}

// CreatePlan creates a plan for achieving a goal
func (p *SmartPlanner) CreatePlan(ctx context.Context, goal string, constraints PlanConstraints) (*Plan, error) {
	// Retrieve relevant past plans from memory
	similarPlans, err := p.retrieveSimilarPlans(ctx, goal)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve similar plans: %w", err)
	}

	// Generate plan using LLM with context from similar plans
	prompt := p.buildPlanPrompt(goal, constraints, similarPlans)

	response, err := p.llm.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("LLM failed to generate plan: %w", err)
	}

	// Parse and structure the plan
	plan := p.parsePlan(response.Content, goal)

	// Apply planning strategy
	strategy := p.selectStrategy(goal, constraints)
	plan, err = strategy.Apply(ctx, plan, constraints)
	if err != nil {
		return nil, fmt.Errorf("strategy application failed: %w", err)
	}

	// Validate the plan
	valid, issues, err := p.ValidatePlan(ctx, plan)
	if err != nil {
		return nil, fmt.Errorf("plan validation failed: %w", err)
	}

	if !valid && len(issues) > 0 {
		// Try to refine the plan based on validation issues
		refinedPlan, err := p.RefinePlan(ctx, plan, strings.Join(issues, "; "))
		if err == nil {
			plan = refinedPlan
		}
	}

	// Optimize if optimizer is available
	if p.optimizer != nil {
		optimizedPlan, err := p.optimizer.Optimize(ctx, plan)
		if err == nil {
			plan = optimizedPlan
		}
	}

	// Store the plan in memory
	p.storePlan(ctx, plan)

	return plan, nil
}

// RefinePlan refines an existing plan based on feedback
func (p *SmartPlanner) RefinePlan(ctx context.Context, plan *Plan, feedback string) (*Plan, error) {
	prompt := fmt.Sprintf(`Refine the following plan based on the feedback:

Original Plan:
Goal: %s
Strategy: %s
Steps: %d

Feedback: %s

Please provide an improved plan that addresses the feedback while maintaining the original goal.`,
		plan.Goal, plan.Strategy, len(plan.Steps), feedback)

	response, err := p.llm.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to refine plan: %w", err)
	}

	refinedPlan := p.parsePlan(response.Content, plan.Goal)

	// Preserve context and metrics from original plan
	refinedPlan.Context = plan.Context
	refinedPlan.ID = plan.ID
	refinedPlan.UpdatedAt = time.Now()

	return refinedPlan, nil
}

// DecomposePlan breaks down a plan step into more detailed sub-steps
func (p *SmartPlanner) DecomposePlan(ctx context.Context, plan *Plan, step *Step) ([]*Step, error) {
	prompt := fmt.Sprintf(`Decompose the following step into smaller, actionable sub-steps:

Step: %s
Description: %s
Type: %s
Context: %s

Provide 3-7 detailed sub-steps that fully accomplish the original step.`,
		step.Name, step.Description, step.Type, plan.Goal)

	response, err := p.llm.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to decompose step: %w", err)
	}

	subSteps := p.parseSteps(response.Content)

	// Set proper relationships
	for i, subStep := range subSteps {
		subStep.ID = fmt.Sprintf("%s.%d", step.ID, i+1)
		subStep.Priority = step.Priority
	}

	return subSteps, nil
}

// OptimizePlan optimizes a plan for efficiency
func (p *SmartPlanner) OptimizePlan(ctx context.Context, plan *Plan) (*Plan, error) {
	if p.optimizer == nil {
		// Use default optimization
		return p.defaultOptimization(ctx, plan)
	}

	return p.optimizer.Optimize(ctx, plan)
}

// ValidatePlan validates that a plan is feasible
func (p *SmartPlanner) ValidatePlan(ctx context.Context, plan *Plan) (bool, []string, error) {
	var issues []string
	valid := true

	for _, validator := range p.validators {
		v, i, err := validator.Validate(ctx, plan)
		if err != nil {
			return false, nil, fmt.Errorf("validator %T failed: %w", validator, err)
		}
		if !v {
			valid = false
			issues = append(issues, i...)
		}
	}

	return valid, issues, nil
}

// RegisterStrategy registers a planning strategy
func (p *SmartPlanner) RegisterStrategy(name string, strategy PlanStrategy) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.strategies[name] = strategy
}

// AddValidator adds a plan validator
func (p *SmartPlanner) AddValidator(validator PlanValidator) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.validators = append(p.validators, validator)
}

// Helper methods

func (p *SmartPlanner) retrieveSimilarPlans(ctx context.Context, goal string) ([]*Plan, error) {
	// Search memory for similar cases
	cases, err := p.memory.SearchSimilarCases(ctx, goal, 5)
	if err != nil {
		return nil, err
	}

	plans := make([]*Plan, 0, len(cases))
	for _, c := range cases {
		// Create a simplified plan from the case
		plan := &Plan{
			ID:       c.ID,
			Goal:     c.Problem,
			Strategy: c.Solution,
			Status:   PlanStatusCompleted,
		}
		plans = append(plans, plan)
	}

	return plans, nil
}

func (p *SmartPlanner) buildPlanPrompt(goal string, constraints PlanConstraints, similarPlans []*Plan) string {
	prompt := fmt.Sprintf("Create a detailed plan to achieve the following goal:\n\nGoal: %s\n\n", goal)

	if constraints.MaxSteps > 0 {
		prompt += fmt.Sprintf("Maximum steps: %d\n", constraints.MaxSteps)
	}
	if constraints.MaxDuration > 0 {
		prompt += fmt.Sprintf("Maximum duration: %s\n", constraints.MaxDuration)
	}

	if len(similarPlans) > 0 {
		prompt += "\nSimilar successful plans for reference:\n"
		for _, plan := range similarPlans {
			prompt += fmt.Sprintf("- %s (Strategy: %s, Steps: %d, Success Rate: %.2f%%)\n",
				plan.Goal, plan.Strategy, len(plan.Steps), plan.Metrics.SuccessRate*100)
		}
	}

	prompt += "\nProvide a structured plan with:\n"
	prompt += "1. Overall strategy\n"
	prompt += "2. Detailed steps with clear descriptions\n"
	prompt += "3. Dependencies between steps\n"
	prompt += "4. Expected outcomes for each step\n"

	return prompt
}

func (p *SmartPlanner) parsePlan(content string, goal string) *Plan {
	// This is a simplified parser - in production, use structured output from LLM
	plan := &Plan{
		ID:           fmt.Sprintf("plan_%d", time.Now().Unix()),
		Goal:         goal,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Status:       PlanStatusDraft,
		Context:      make(map[string]interface{}),
		Dependencies: make(map[string][]string),
	}

	// Parse strategy and steps from LLM response
	// This would be more sophisticated in production
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Strategy:") {
			plan.Strategy = strings.TrimPrefix(line, "Strategy:")
			plan.Strategy = strings.TrimSpace(plan.Strategy)
		}
		// Parse steps...
	}

	// Generate sample steps for demonstration
	plan.Steps = []*Step{
		{
			ID:          "step_1",
			Name:        "Analyze Current State",
			Description: "Analyze the current situation and gather context",
			Type:        StepTypeAnalysis,
			Priority:    1,
			Status:      StepStatusPending,
		},
		{
			ID:          "step_2",
			Name:        "Plan Execution",
			Description: "Execute the main action to achieve the goal",
			Type:        StepTypeAction,
			Priority:    2,
			Status:      StepStatusPending,
		},
		{
			ID:          "step_3",
			Name:        "Validate Results",
			Description: "Validate that the goal has been achieved",
			Type:        StepTypeValidation,
			Priority:    3,
			Status:      StepStatusPending,
		},
	}

	return plan
}

func (p *SmartPlanner) parseSteps(content string) []*Step {
	// Simplified step parsing
	var steps []*Step

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			steps = append(steps, &Step{
				ID:          fmt.Sprintf("substep_%d", i+1),
				Name:        fmt.Sprintf("Sub-step %d", i+1),
				Description: line,
				Type:        StepTypeAction,
				Status:      StepStatusPending,
			})
		}
	}

	return steps
}

func (p *SmartPlanner) selectStrategy(goal string, constraints PlanConstraints) PlanStrategy {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Simple strategy selection - could be more sophisticated
	if constraints.MaxSteps > 0 && constraints.MaxSteps < 5 {
		if strategy, ok := p.strategies["backward_chaining"]; ok {
			return strategy
		}
	}

	if strings.Contains(strings.ToLower(goal), "complex") || strings.Contains(strings.ToLower(goal), "multi") {
		if strategy, ok := p.strategies["hierarchical"]; ok {
			return strategy
		}
	}

	// Default to decomposition
	if strategy, ok := p.strategies["decomposition"]; ok {
		return strategy
	}

	// Fallback to a no-op strategy
	return &NoOpStrategy{}
}

func (p *SmartPlanner) storePlan(ctx context.Context, plan *Plan) {
	// Store plan in memory for future reference
	key := fmt.Sprintf("plan:%s", plan.ID)
	_ = p.memory.Store(ctx, key, plan)
}

func (p *SmartPlanner) defaultOptimization(ctx context.Context, plan *Plan) (*Plan, error) {
	// Simple optimization: remove redundant steps and optimize ordering
	optimized := *plan

	// Remove duplicate steps
	seen := make(map[string]bool)
	var uniqueSteps []*Step
	for _, step := range plan.Steps {
		key := fmt.Sprintf("%s:%s", step.Name, step.Type)
		if !seen[key] {
			seen[key] = true
			uniqueSteps = append(uniqueSteps, step)
		}
	}
	optimized.Steps = uniqueSteps

	// Sort by priority
	// This would be more sophisticated in production

	return &optimized, nil
}
