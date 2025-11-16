package planning

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kart-io/goagent/core"
)

// PlanningAgent is an agent that creates and executes plans
type PlanningAgent struct {
	*core.BaseAgent
	planner  Planner
	executor PlanExecutor
}

// NewPlanningAgent creates a new planning agent
func NewPlanningAgent(planner Planner, executor PlanExecutor) *PlanningAgent {
	agent := &PlanningAgent{
		BaseAgent: core.NewBaseAgent("planning_agent", "Creates and executes plans to achieve goals", []string{"planning", "execution"}),
		planner:   planner,
		executor:  executor,
	}
	return agent
}

// Execute implements the Agent interface
func (a *PlanningAgent) Execute(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	// Extract goal from input
	goal, ok := input.Context["goal"].(string)
	if !ok {
		return nil, fmt.Errorf("goal not provided in input")
	}

	// Extract constraints if provided
	var constraints PlanConstraints
	if c, ok := input.Context["constraints"]; ok {
		if constraintData, err := json.Marshal(c); err == nil {
			_ = json.Unmarshal(constraintData, &constraints)
		}
	}

	// Check if we should execute an existing plan
	if planData, ok := input.Context["plan"]; ok {
		if plan, ok := planData.(*Plan); ok {
			// Execute existing plan
			result, err := a.executor.Execute(ctx, plan)
			if err != nil {
				return nil, fmt.Errorf("failed to execute plan: %w", err)
			}

			return &core.AgentOutput{
				Result: result,
				Metadata: map[string]interface{}{
					"plan_id":        plan.ID,
					"execution_time": result.TotalDuration,
				},
			}, nil
		}
	}

	// Create a new plan
	plan, err := a.planner.CreatePlan(ctx, goal, constraints)
	if err != nil {
		return nil, fmt.Errorf("failed to create plan: %w", err)
	}

	// Execute the plan if requested
	if execute, ok := input.Context["execute"].(bool); ok && execute {
		result, err := a.executor.Execute(ctx, plan)
		if err != nil {
			return nil, fmt.Errorf("failed to execute plan: %w", err)
		}

		return &core.AgentOutput{
			Result: map[string]interface{}{
				"plan":   plan,
				"result": result,
			},
			Metadata: map[string]interface{}{
				"plan_id":        plan.ID,
				"execution_time": result.TotalDuration,
			},
		}, nil
	}

	// Return just the plan
	return &core.AgentOutput{
		Result: plan,
		Metadata: map[string]interface{}{
			"plan_id":     plan.ID,
			"total_steps": len(plan.Steps),
		},
	}, nil
}

// TaskDecompositionAgent decomposes complex tasks into subtasks
type TaskDecompositionAgent struct {
	*core.BaseAgent
	planner Planner
}

// NewTaskDecompositionAgent creates a new task decomposition agent
func NewTaskDecompositionAgent(planner Planner) *TaskDecompositionAgent {
	return &TaskDecompositionAgent{
		BaseAgent: core.NewBaseAgent("task_decomposition", "Decomposes complex tasks into subtasks", []string{"decomposition"}),
		planner:   planner,
	}
}

// Execute decomposes a task into subtasks
func (a *TaskDecompositionAgent) Execute(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	// Extract task description
	task, ok := input.Context["task"].(string)
	if !ok {
		return nil, fmt.Errorf("task not provided in input")
	}

	// Create a simple plan for the task
	plan, err := a.planner.CreatePlan(ctx, task, PlanConstraints{
		MaxSteps: 10, // Reasonable limit for decomposition
	})
	if err != nil {
		return nil, fmt.Errorf("failed to decompose task: %w", err)
	}

	// Extract just the steps as subtasks
	subtasks := make([]map[string]interface{}, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		subtasks = append(subtasks, map[string]interface{}{
			"id":                 step.ID,
			"name":               step.Name,
			"description":        step.Description,
			"type":               step.Type,
			"priority":           step.Priority,
			"estimated_duration": step.EstimatedDuration,
		})
	}

	return &core.AgentOutput{
		Result: subtasks,
		Metadata: map[string]interface{}{
			"task":         task,
			"num_subtasks": len(subtasks),
		},
	}, nil
}

// StrategyAgent selects and applies planning strategies
type StrategyAgent struct {
	*core.BaseAgent
	strategies map[string]PlanStrategy
}

// NewStrategyAgent creates a new strategy agent
func NewStrategyAgent() *StrategyAgent {
	agent := &StrategyAgent{
		BaseAgent:  core.NewBaseAgent("strategy_agent", "Selects and applies planning strategies", []string{"strategy"}),
		strategies: make(map[string]PlanStrategy),
	}

	// Register default strategies
	agent.RegisterStrategy("decomposition", &DecompositionStrategy{})
	agent.RegisterStrategy("backward_chaining", &BackwardChainingStrategy{})
	agent.RegisterStrategy("hierarchical", &HierarchicalStrategy{})

	return agent
}

// RegisterStrategy registers a new strategy
func (a *StrategyAgent) RegisterStrategy(name string, strategy PlanStrategy) {
	a.strategies[name] = strategy
}

// Execute selects and applies a strategy to a plan
func (a *StrategyAgent) Execute(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	// Extract plan
	planData, ok := input.Context["plan"]
	if !ok {
		return nil, fmt.Errorf("plan not provided in input")
	}

	plan, ok := planData.(*Plan)
	if !ok {
		// Try to unmarshal if it's JSON data
		if planBytes, err := json.Marshal(planData); err == nil {
			plan = &Plan{}
			if err := json.Unmarshal(planBytes, plan); err != nil {
				return nil, fmt.Errorf("invalid plan data")
			}
		} else {
			return nil, fmt.Errorf("invalid plan data")
		}
	}

	// Extract strategy name
	strategyName := "decomposition" // default
	if s, ok := input.Context["strategy"].(string); ok {
		strategyName = s
	}

	// Get strategy
	strategy, exists := a.strategies[strategyName]
	if !exists {
		return nil, fmt.Errorf("strategy %s not found", strategyName)
	}

	// Extract constraints
	var constraints PlanConstraints
	if c, ok := input.Context["constraints"]; ok {
		if constraintData, err := json.Marshal(c); err == nil {
			_ = json.Unmarshal(constraintData, &constraints)
		}
	}

	// Apply strategy
	refinedPlan, err := strategy.Apply(ctx, plan, constraints)
	if err != nil {
		return nil, fmt.Errorf("failed to apply strategy: %w", err)
	}

	return &core.AgentOutput{
		Result: refinedPlan,
		Metadata: map[string]interface{}{
			"strategy":    strategyName,
			"total_steps": len(refinedPlan.Steps),
		},
	}, nil
}

// OptimizationAgent optimizes plans for efficiency
type OptimizationAgent struct {
	*core.BaseAgent
	optimizer PlanOptimizer
}

// NewOptimizationAgent creates a new optimization agent
func NewOptimizationAgent(optimizer PlanOptimizer) *OptimizationAgent {
	if optimizer == nil {
		optimizer = &DefaultOptimizer{}
	}
	return &OptimizationAgent{
		BaseAgent: core.NewBaseAgent("optimization_agent", "Optimizes plans for efficiency", []string{"optimization"}),
		optimizer: optimizer,
	}
}

// Execute optimizes a plan
func (a *OptimizationAgent) Execute(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	// Extract plan
	planData, ok := input.Context["plan"]
	if !ok {
		return nil, fmt.Errorf("plan not provided in input")
	}

	plan, ok := planData.(*Plan)
	if !ok {
		// Try to unmarshal if it's JSON data
		if planBytes, err := json.Marshal(planData); err == nil {
			plan = &Plan{}
			if err := json.Unmarshal(planBytes, plan); err != nil {
				return nil, fmt.Errorf("invalid plan data")
			}
		} else {
			return nil, fmt.Errorf("invalid plan data")
		}
	}

	// Optimize plan
	optimizedPlan, err := a.optimizer.Optimize(ctx, plan)
	if err != nil {
		return nil, fmt.Errorf("failed to optimize plan: %w", err)
	}

	// Calculate optimization metrics
	originalSteps := len(plan.Steps)
	optimizedSteps := len(optimizedPlan.Steps)
	reduction := float64(originalSteps-optimizedSteps) / float64(originalSteps) * 100

	// Count parallelizable steps
	parallelSteps := 0
	for _, step := range optimizedPlan.Steps {
		if parallel, ok := step.Parameters["parallel"].(bool); ok && parallel {
			parallelSteps++
		}
	}

	return &core.AgentOutput{
		Result: optimizedPlan,
		Metadata: map[string]interface{}{
			"original_steps":  originalSteps,
			"optimized_steps": optimizedSteps,
			"reduction_pct":   reduction,
			"parallel_steps":  parallelSteps,
		},
	}, nil
}

// ValidationAgent validates plans for feasibility
type ValidationAgent struct {
	*core.BaseAgent
	validators []PlanValidator
}

// NewValidationAgent creates a new validation agent
func NewValidationAgent() *ValidationAgent {
	agent := &ValidationAgent{
		BaseAgent:  core.NewBaseAgent("validation_agent", "Validates plans for feasibility", []string{"validation"}),
		validators: []PlanValidator{},
	}

	// Add default validators
	agent.AddValidator(&DependencyValidator{})
	agent.AddValidator(&ResourceValidator{})
	agent.AddValidator(&TimeValidator{})

	return agent
}

// AddValidator adds a validator
func (a *ValidationAgent) AddValidator(validator PlanValidator) {
	a.validators = append(a.validators, validator)
}

// Execute validates a plan
func (a *ValidationAgent) Execute(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	// Extract plan
	planData, ok := input.Context["plan"]
	if !ok {
		return nil, fmt.Errorf("plan not provided in input")
	}

	plan, ok := planData.(*Plan)
	if !ok {
		// Try to unmarshal if it's JSON data
		if planBytes, err := json.Marshal(planData); err == nil {
			plan = &Plan{}
			if err := json.Unmarshal(planBytes, plan); err != nil {
				return nil, fmt.Errorf("invalid plan data")
			}
		} else {
			return nil, fmt.Errorf("invalid plan data")
		}
	}

	// Run all validators
	var allIssues []string
	valid := true

	for _, validator := range a.validators {
		v, issues, err := validator.Validate(ctx, plan)
		if err != nil {
			return nil, fmt.Errorf("validator %T failed: %w", validator, err)
		}
		if !v {
			valid = false
			allIssues = append(allIssues, issues...)
		}
	}

	return &core.AgentOutput{
		Result: map[string]interface{}{
			"valid":  valid,
			"issues": allIssues,
		},
		Metadata: map[string]interface{}{
			"plan_id":         plan.ID,
			"validator_count": len(a.validators),
			"issue_count":     len(allIssues),
		},
	}, nil
}
