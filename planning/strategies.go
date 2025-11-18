package planning

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// PlanStrategy defines a strategy for plan creation and refinement
type PlanStrategy interface {
	// Apply applies the strategy to a plan
	Apply(ctx context.Context, plan *Plan, constraints PlanConstraints) (*Plan, error)

	// Name returns the strategy name
	Name() string
}

// PlanValidator validates plans for feasibility
type PlanValidator interface {
	// Validate checks if a plan is valid
	Validate(ctx context.Context, plan *Plan) (bool, []string, error)
}

// PlanOptimizer optimizes plans for efficiency
type PlanOptimizer interface {
	// Optimize optimizes a plan
	Optimize(ctx context.Context, plan *Plan) (*Plan, error)
}

// DecompositionStrategy breaks down complex goals into simpler steps
type DecompositionStrategy struct{}

func (s *DecompositionStrategy) Name() string {
	return StrategyDecomposition
}

func (s *DecompositionStrategy) Apply(ctx context.Context, plan *Plan, constraints PlanConstraints) (*Plan, error) {
	// Decompose each high-level step into more detailed sub-steps
	var decomposedSteps []*Step

	for _, step := range plan.Steps {
		if s.isComplexStep(step) {
			// Break down complex steps
			subSteps := s.decomposeStep(step)
			decomposedSteps = append(decomposedSteps, subSteps...)
		} else {
			decomposedSteps = append(decomposedSteps, step)
		}
	}

	plan.Steps = decomposedSteps
	plan.Status = PlanStatusReady

	// Update dependencies
	s.updateDependencies(plan)

	return plan, nil
}

func (s *DecompositionStrategy) isComplexStep(step *Step) bool {
	// Check if step needs decomposition
	return len(step.Description) > 100 || step.Type == StepTypeAnalysis
}

func (s *DecompositionStrategy) decomposeStep(step *Step) []*Step {
	// Create sub-steps for complex step
	var subSteps []*Step

	// Example decomposition
	if step.Type == StepTypeAnalysis {
		subSteps = append(subSteps, &Step{
			ID:                fmt.Sprintf("%s_1", step.ID),
			Name:              fmt.Sprintf("Prepare %s", step.Name),
			Description:       fmt.Sprintf("Prepare data and context for %s", step.Name),
			Type:              StepTypeAction,
			Priority:          step.Priority,
			Status:            StatusPending,
			EstimatedDuration: 5 * time.Minute,
		})

		subSteps = append(subSteps, &Step{
			ID:                fmt.Sprintf("%s_2", step.ID),
			Name:              fmt.Sprintf("Execute %s", step.Name),
			Description:       step.Description,
			Type:              step.Type,
			Priority:          step.Priority,
			Status:            StatusPending,
			EstimatedDuration: step.EstimatedDuration,
		})

		subSteps = append(subSteps, &Step{
			ID:                fmt.Sprintf("%s_3", step.ID),
			Name:              fmt.Sprintf("Verify %s", step.Name),
			Description:       fmt.Sprintf("Verify results of %s", step.Name),
			Type:              StepTypeValidation,
			Priority:          step.Priority,
			Status:            StatusPending,
			EstimatedDuration: 2 * time.Minute,
		})
	} else {
		// Return original step if no decomposition needed
		subSteps = append(subSteps, step)
	}

	return subSteps
}

func (s *DecompositionStrategy) updateDependencies(plan *Plan) {
	// Update step dependencies based on priority and type
	for i, step := range plan.Steps {
		if i > 0 {
			// Simple sequential dependency
			prevStep := plan.Steps[i-1]
			if plan.Dependencies[step.ID] == nil {
				plan.Dependencies[step.ID] = []string{}
			}
			plan.Dependencies[step.ID] = append(plan.Dependencies[step.ID], prevStep.ID)
		}
	}
}

// BackwardChainingStrategy works backward from goal to current state
type BackwardChainingStrategy struct{}

func (s *BackwardChainingStrategy) Name() string {
	return StrategyBackwardChaining
}

func (s *BackwardChainingStrategy) Apply(ctx context.Context, plan *Plan, constraints PlanConstraints) (*Plan, error) {
	// Start from the goal and work backward to determine required steps

	// Identify the final goal state
	goalStep := &Step{
		ID:          "goal",
		Name:        "Achieve Goal",
		Description: plan.Goal,
		Type:        StepTypeValidation,
		Priority:    100,
		Status:      StatusPending,
	}

	// Work backward to identify prerequisites
	prerequisites := s.identifyPrerequisites(goalStep, plan.Context)

	// Build the plan from prerequisites
	steps := make([]*Step, 0, len(prerequisites)+1)
	for i, prereq := range prerequisites {
		steps = append(steps, &Step{
			ID:          fmt.Sprintf("step_%d", i+1),
			Name:        prereq.Name,
			Description: prereq.Description,
			Type:        prereq.Type,
			Priority:    i + 1,
			Status:      StatusPending,
		})
	}

	// Add the goal step at the end
	steps = append(steps, goalStep)

	plan.Steps = steps
	plan.Status = PlanStatusReady

	// Build dependency chain
	for i := len(steps) - 1; i > 0; i-- {
		current := steps[i]
		previous := steps[i-1]
		if plan.Dependencies[current.ID] == nil {
			plan.Dependencies[current.ID] = []string{}
		}
		plan.Dependencies[current.ID] = append(plan.Dependencies[current.ID], previous.ID)
	}

	return plan, nil
}

func (s *BackwardChainingStrategy) identifyPrerequisites(goalStep *Step, context map[string]interface{}) []*Step {
	// Identify what needs to be done before achieving the goal
	prerequisites := []*Step{
		{
			Name:        "Gather Required Resources",
			Description: "Collect all necessary resources and information",
			Type:        StepTypeAnalysis,
		},
		{
			Name:        "Prepare Environment",
			Description: "Set up the environment for achieving the goal",
			Type:        StepTypeAction,
		},
		{
			Name:        "Execute Main Action",
			Description: "Perform the primary action to achieve the goal",
			Type:        StepTypeAction,
		},
	}

	return prerequisites
}

// HierarchicalStrategy creates multi-level plans
type HierarchicalStrategy struct {
	maxLevels int //nolint:unused // Will be used for depth control
}

func (s *HierarchicalStrategy) Name() string {
	return "hierarchical"
}

func (s *HierarchicalStrategy) Apply(ctx context.Context, plan *Plan, constraints PlanConstraints) (*Plan, error) {
	// Create a hierarchical plan with multiple levels of abstraction

	// Top-level phases
	phases := s.identifyPhases(plan.Goal)

	allSteps := make([]*Step, 0, len(phases)*3) // Preallocate: phases + sub-steps
	for phaseIdx, phase := range phases {
		// Create phase step
		phaseStep := &Step{
			ID:          fmt.Sprintf("phase_%d", phaseIdx+1),
			Name:        phase.Name,
			Description: phase.Description,
			Type:        StepTypeAction,
			Priority:    (phaseIdx + 1) * 10,
			Status:      StepStatusPending,
		}
		allSteps = append(allSteps, phaseStep)

		// Create sub-steps for each phase
		subSteps := s.createPhaseSteps(phase, phaseIdx+1)
		for _, subStep := range subSteps {
			subStep.Priority = phaseStep.Priority + subStep.Priority
			if plan.Dependencies[subStep.ID] == nil {
				plan.Dependencies[subStep.ID] = []string{}
			}
			plan.Dependencies[subStep.ID] = append(plan.Dependencies[subStep.ID], phaseStep.ID)
			allSteps = append(allSteps, subStep)
		}
	}

	// Sort steps by priority
	sort.Slice(allSteps, func(i, j int) bool {
		return allSteps[i].Priority < allSteps[j].Priority
	})

	plan.Steps = allSteps
	plan.Status = PlanStatusReady

	return plan, nil
}

type Phase struct {
	Name        string
	Description string
}

func (s *HierarchicalStrategy) identifyPhases(goal string) []Phase {
	// Identify high-level phases for achieving the goal
	return []Phase{
		{
			Name:        "Initialization",
			Description: "Initialize and prepare the system",
		},
		{
			Name:        "Execution",
			Description: "Execute the main operations",
		},
		{
			Name:        "Finalization",
			Description: "Finalize and validate results",
		},
	}
}

func (s *HierarchicalStrategy) createPhaseSteps(phase Phase, phaseNum int) []*Step {
	// Create detailed steps for each phase
	var steps []*Step

	switch phase.Name {
	case "Initialization":
		steps = append(steps, &Step{
			ID:          fmt.Sprintf("step_%d_1", phaseNum),
			Name:        "Check Prerequisites",
			Description: "Verify all prerequisites are met",
			Type:        StepTypeValidation,
			Priority:    1,
			Status:      StatusPending,
		})
		steps = append(steps, &Step{
			ID:          fmt.Sprintf("step_%d_2", phaseNum),
			Name:        "Load Configuration",
			Description: "Load required configuration",
			Type:        StepTypeAction,
			Priority:    2,
			Status:      StatusPending,
		})
	case "Execution":
		steps = append(steps, &Step{
			ID:          fmt.Sprintf("step_%d_1", phaseNum),
			Name:        "Process Data",
			Description: "Process input data",
			Type:        StepTypeAction,
			Priority:    1,
			Status:      StatusPending,
		})
		steps = append(steps, &Step{
			ID:          fmt.Sprintf("step_%d_2", phaseNum),
			Name:        "Apply Transformations",
			Description: "Apply necessary transformations",
			Type:        StepTypeAction,
			Priority:    2,
			Status:      StatusPending,
		})
	case "Finalization":
		steps = append(steps, &Step{
			ID:          fmt.Sprintf("step_%d_1", phaseNum),
			Name:        "Validate Output",
			Description: "Validate the output meets requirements",
			Type:        StepTypeValidation,
			Priority:    1,
			Status:      StatusPending,
		})
		steps = append(steps, &Step{
			ID:          fmt.Sprintf("step_%d_2", phaseNum),
			Name:        "Generate Report",
			Description: "Generate execution report",
			Type:        StepTypeAction,
			Priority:    2,
			Status:      StatusPending,
		})
	}

	return steps
}

// NoOpStrategy does nothing (used as fallback)
type NoOpStrategy struct{}

func (s *NoOpStrategy) Name() string {
	return "noop"
}

func (s *NoOpStrategy) Apply(ctx context.Context, plan *Plan, constraints PlanConstraints) (*Plan, error) {
	plan.Status = PlanStatusReady
	return plan, nil
}

// DependencyValidator validates step dependencies
type DependencyValidator struct{}

func (v *DependencyValidator) Validate(ctx context.Context, plan *Plan) (bool, []string, error) {
	var issues []string

	// Check for circular dependencies
	if hasCycle := v.hasCyclicDependency(plan); hasCycle {
		issues = append(issues, "Plan has circular dependencies")
	}

	// Check that all referenced dependencies exist
	stepIDs := make(map[string]bool)
	for _, step := range plan.Steps {
		stepIDs[step.ID] = true
	}

	for stepID, deps := range plan.Dependencies {
		if !stepIDs[stepID] {
			issues = append(issues, fmt.Sprintf("Dependency references non-existent step: %s", stepID))
		}
		for _, dep := range deps {
			if !stepIDs[dep] {
				issues = append(issues, fmt.Sprintf("Step %s depends on non-existent step: %s", stepID, dep))
			}
		}
	}

	return len(issues) == 0, issues, nil
}

func (v *DependencyValidator) hasCyclicDependency(plan *Plan) bool {
	// Simple cycle detection using DFS
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var hasCycle func(node string) bool
	hasCycle = func(node string) bool {
		visited[node] = true
		recStack[node] = true

		if deps, ok := plan.Dependencies[node]; ok {
			for _, dep := range deps {
				if !visited[dep] {
					if hasCycle(dep) {
						return true
					}
				} else if recStack[dep] {
					return true
				}
			}
		}

		recStack[node] = false
		return false
	}

	for _, step := range plan.Steps {
		if !visited[step.ID] {
			if hasCycle(step.ID) {
				return true
			}
		}
	}

	return false
}

// ResourceValidator validates resource constraints
type ResourceValidator struct{}

func (v *ResourceValidator) Validate(ctx context.Context, plan *Plan) (bool, []string, error) {
	var issues []string

	// Check resource requirements
	totalDuration := time.Duration(0)
	for _, step := range plan.Steps {
		if step.EstimatedDuration > 0 {
			totalDuration += step.EstimatedDuration
		}
	}

	// Check against constraints if they exist in context
	if maxDuration, ok := plan.Context["max_duration"].(time.Duration); ok {
		if totalDuration > maxDuration {
			issues = append(issues, fmt.Sprintf("Plan duration (%s) exceeds maximum (%s)", totalDuration, maxDuration))
		}
	}

	return len(issues) == 0, issues, nil
}

// TimeValidator validates timing constraints
type TimeValidator struct{}

func (v *TimeValidator) Validate(ctx context.Context, plan *Plan) (bool, []string, error) {
	var issues []string

	// Validate that steps can be executed within time constraints
	for _, step := range plan.Steps {
		if step.EstimatedDuration < 0 {
			issues = append(issues, fmt.Sprintf("Step %s has invalid duration: %s", step.ID, step.EstimatedDuration))
		}
	}

	return len(issues) == 0, issues, nil
}

// DefaultOptimizer provides basic plan optimization
type DefaultOptimizer struct{}

func (o *DefaultOptimizer) Optimize(ctx context.Context, plan *Plan) (*Plan, error) {
	optimized := *plan

	// Parallelize independent steps
	o.parallelizeSteps(&optimized)

	// Remove redundant steps
	o.removeRedundantSteps(&optimized)

	// Optimize step ordering
	o.optimizeOrdering(&optimized)

	return &optimized, nil
}

func (o *DefaultOptimizer) parallelizeSteps(plan *Plan) {
	// Identify steps that can run in parallel
	// Steps with no dependencies on each other can run concurrently

	// Group steps by dependency level
	levels := make(map[int][]*Step)
	level := make(map[string]int)

	// Calculate levels using topological sort
	for _, step := range plan.Steps {
		o.calculateLevel(step, plan, level, 0)
	}

	// Group steps by level
	for _, step := range plan.Steps {
		l := level[step.ID]
		levels[l] = append(levels[l], step)
	}

	// Mark parallel steps
	for _, stepsAtLevel := range levels {
		if len(stepsAtLevel) > 1 {
			// These steps can run in parallel
			for _, step := range stepsAtLevel {
				if step.Parameters == nil {
					step.Parameters = make(map[string]interface{})
				}
				step.Parameters["parallel"] = true
			}
		}
	}
}

func (o *DefaultOptimizer) calculateLevel(step *Step, plan *Plan, level map[string]int, currentLevel int) int {
	if l, exists := level[step.ID]; exists {
		return l
	}

	maxDepLevel := currentLevel
	if deps, ok := plan.Dependencies[step.ID]; ok {
		for _, depID := range deps {
			// Find the dependency step
			for _, s := range plan.Steps {
				if s.ID == depID {
					depLevel := o.calculateLevel(s, plan, level, currentLevel+1)
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

func (o *DefaultOptimizer) removeRedundantSteps(plan *Plan) {
	// Remove duplicate or unnecessary steps
	seen := make(map[string]bool)
	var uniqueSteps []*Step

	for _, step := range plan.Steps {
		key := fmt.Sprintf("%s:%s:%s", step.Name, step.Type, step.Description)
		if !seen[key] {
			seen[key] = true
			uniqueSteps = append(uniqueSteps, step)
		}
	}

	plan.Steps = uniqueSteps
}

func (o *DefaultOptimizer) optimizeOrdering(plan *Plan) {
	// Optimize step ordering based on priorities and dependencies
	sort.Slice(plan.Steps, func(i, j int) bool {
		// First sort by priority
		if plan.Steps[i].Priority != plan.Steps[j].Priority {
			return plan.Steps[i].Priority < plan.Steps[j].Priority
		}

		// Then by estimated duration (shorter first)
		if plan.Steps[i].EstimatedDuration != plan.Steps[j].EstimatedDuration {
			return plan.Steps[i].EstimatedDuration < plan.Steps[j].EstimatedDuration
		}

		// Finally by ID for stability
		return plan.Steps[i].ID < plan.Steps[j].ID
	})
}
