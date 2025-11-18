package planning

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDecompositionStrategy tests the decomposition strategy
func TestDecompositionStrategy(t *testing.T) {
	strategy := &DecompositionStrategy{}

	assert.Equal(t, "decomposition", strategy.Name())

	tests := []struct {
		name        string
		plan        *Plan
		constraints PlanConstraints
		wantErr     bool
		validate    func(*testing.T, *Plan)
	}{
		{
			name: "decompose complex steps",
			plan: &Plan{
				ID:   "plan_1",
				Goal: "Test goal",
				Steps: []*Step{
					{
						ID:          "step_1",
						Name:        "Complex Analysis Step",
						Description: "This is a very long description that exceeds 100 characters and should trigger decomposition into sub-steps for better planning",
						Type:        StepTypeAnalysis,
						Priority:    1,
					},
				},
				Dependencies: make(map[string][]string),
			},
			constraints: PlanConstraints{},
			wantErr:     false,
			validate: func(t *testing.T, p *Plan) {
				assert.Greater(t, len(p.Steps), 1) // Should have decomposed into multiple steps
				assert.Equal(t, PlanStatusReady, p.Status)
			},
		},
		{
			name: "simple steps unchanged",
			plan: &Plan{
				ID:   "plan_2",
				Goal: "Test goal",
				Steps: []*Step{
					{
						ID:          "step_1",
						Name:        "Simple Step",
						Description: "Short description",
						Type:        StepTypeAction,
						Priority:    1,
					},
				},
				Dependencies: make(map[string][]string),
			},
			constraints: PlanConstraints{},
			wantErr:     false,
			validate: func(t *testing.T, p *Plan) {
				assert.Equal(t, 1, len(p.Steps))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := strategy.Apply(ctx, tt.plan, tt.constraints)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
				if tt.validate != nil {
					tt.validate(t, result)
				}
			}
		})
	}
}

// TestBackwardChainingStrategy tests the backward chaining strategy
func TestBackwardChainingStrategy(t *testing.T) {
	strategy := &BackwardChainingStrategy{}

	assert.Equal(t, "backward_chaining", strategy.Name())

	plan := &Plan{
		ID:           "plan_1",
		Goal:         "Achieve final goal",
		Steps:        []*Step{},
		Dependencies: make(map[string][]string),
		Context:      make(map[string]interface{}),
	}

	ctx := context.Background()
	result, err := strategy.Apply(ctx, plan, PlanConstraints{})

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Greater(t, len(result.Steps), 0) // Should generate steps
	assert.Equal(t, PlanStatusReady, result.Status)

	// Verify goal step is at the end
	lastStep := result.Steps[len(result.Steps)-1]
	assert.Equal(t, "goal", lastStep.ID)
	assert.Equal(t, StepTypeValidation, lastStep.Type)

	// Verify dependency chain
	assert.NotEmpty(t, result.Dependencies)
}

// TestHierarchicalStrategy tests the hierarchical strategy
func TestHierarchicalStrategy(t *testing.T) {
	strategy := &HierarchicalStrategy{}

	assert.Equal(t, "hierarchical", strategy.Name())

	plan := &Plan{
		ID:           "plan_1",
		Goal:         "Complex multi-phase project",
		Steps:        []*Step{},
		Dependencies: make(map[string][]string),
	}

	ctx := context.Background()
	result, err := strategy.Apply(ctx, plan, PlanConstraints{})

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Greater(t, len(result.Steps), 3) // Should have phases + sub-steps
	assert.Equal(t, PlanStatusReady, result.Status)

	// Verify steps are sorted by priority
	for i := 1; i < len(result.Steps); i++ {
		assert.GreaterOrEqual(t, result.Steps[i].Priority, result.Steps[i-1].Priority)
	}

	// Verify dependencies are set
	assert.NotEmpty(t, result.Dependencies)
}

// TestNoOpStrategy tests the no-op strategy
func TestNoOpStrategy(t *testing.T) {
	strategy := &NoOpStrategy{}

	assert.Equal(t, "noop", strategy.Name())

	plan := &Plan{
		ID:     "plan_1",
		Goal:   "Test goal",
		Steps:  []*Step{{ID: "step_1"}},
		Status: PlanStatusDraft,
	}

	ctx := context.Background()
	result, err := strategy.Apply(ctx, plan, PlanConstraints{})

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, PlanStatusReady, result.Status)
	assert.Equal(t, len(plan.Steps), len(result.Steps)) // No changes to steps
}

// TestDependencyValidator tests dependency validation
func TestDependencyValidator(t *testing.T) {
	validator := &DependencyValidator{}

	tests := []struct {
		name       string
		plan       *Plan
		wantValid  bool
		wantIssues int
	}{
		{
			name: "valid dependencies",
			plan: &Plan{
				Steps: []*Step{
					{ID: "step_1"},
					{ID: "step_2"},
					{ID: "step_3"},
				},
				Dependencies: map[string][]string{
					"step_2": {"step_1"},
					"step_3": {"step_1", "step_2"},
				},
			},
			wantValid:  true,
			wantIssues: 0,
		},
		{
			name: "circular dependency",
			plan: &Plan{
				Steps: []*Step{
					{ID: "step_1"},
					{ID: "step_2"},
				},
				Dependencies: map[string][]string{
					"step_1": {"step_2"},
					"step_2": {"step_1"},
				},
			},
			wantValid:  false,
			wantIssues: 1,
		},
		{
			name: "missing step in dependency",
			plan: &Plan{
				Steps: []*Step{
					{ID: "step_1"},
				},
				Dependencies: map[string][]string{
					"step_1": {"step_999"},
				},
			},
			wantValid:  false,
			wantIssues: 1,
		},
		{
			name: "dependency on non-existent step",
			plan: &Plan{
				Steps: []*Step{
					{ID: "step_1"},
					{ID: "step_2"},
				},
				Dependencies: map[string][]string{
					"step_999": {"step_1"},
				},
			},
			wantValid:  false,
			wantIssues: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			valid, issues, err := validator.Validate(ctx, tt.plan)

			require.NoError(t, err)
			assert.Equal(t, tt.wantValid, valid)
			if tt.wantIssues > 0 {
				assert.GreaterOrEqual(t, len(issues), tt.wantIssues)
			} else {
				assert.Empty(t, issues)
			}
		})
	}
}

// TestResourceValidator tests resource validation
func TestResourceValidator(t *testing.T) {
	validator := &ResourceValidator{}

	tests := []struct {
		name       string
		plan       *Plan
		wantValid  bool
		wantIssues int
	}{
		{
			name: "within resource limits",
			plan: &Plan{
				Steps: []*Step{
					{ID: "step_1", EstimatedDuration: 10 * time.Minute},
					{ID: "step_2", EstimatedDuration: 15 * time.Minute},
				},
				Context: map[string]interface{}{
					"max_duration": 30 * time.Minute,
				},
			},
			wantValid:  true,
			wantIssues: 0,
		},
		{
			name: "exceeds duration limit",
			plan: &Plan{
				Steps: []*Step{
					{ID: "step_1", EstimatedDuration: 20 * time.Minute},
					{ID: "step_2", EstimatedDuration: 25 * time.Minute},
				},
				Context: map[string]interface{}{
					"max_duration": 30 * time.Minute,
				},
			},
			wantValid:  false,
			wantIssues: 1,
		},
		{
			name: "no duration constraints",
			plan: &Plan{
				Steps: []*Step{
					{ID: "step_1", EstimatedDuration: 100 * time.Minute},
				},
				Context: make(map[string]interface{}),
			},
			wantValid:  true,
			wantIssues: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			valid, issues, err := validator.Validate(ctx, tt.plan)

			require.NoError(t, err)
			assert.Equal(t, tt.wantValid, valid)
			if tt.wantIssues > 0 {
				assert.GreaterOrEqual(t, len(issues), tt.wantIssues)
			} else {
				assert.Empty(t, issues)
			}
		})
	}
}

// TestTimeValidator tests time validation
func TestTimeValidator(t *testing.T) {
	validator := &TimeValidator{}

	tests := []struct {
		name       string
		plan       *Plan
		wantValid  bool
		wantIssues int
	}{
		{
			name: "valid durations",
			plan: &Plan{
				Steps: []*Step{
					{ID: "step_1", EstimatedDuration: 10 * time.Minute},
					{ID: "step_2", EstimatedDuration: 0}, // Zero is valid
				},
			},
			wantValid:  true,
			wantIssues: 0,
		},
		{
			name: "negative duration",
			plan: &Plan{
				Steps: []*Step{
					{ID: "step_1", EstimatedDuration: -10 * time.Minute},
				},
			},
			wantValid:  false,
			wantIssues: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			valid, issues, err := validator.Validate(ctx, tt.plan)

			require.NoError(t, err)
			assert.Equal(t, tt.wantValid, valid)
			if tt.wantIssues > 0 {
				assert.GreaterOrEqual(t, len(issues), tt.wantIssues)
			} else {
				assert.Empty(t, issues)
			}
		})
	}
}

// TestDefaultOptimizer tests the default optimizer
func TestDefaultOptimizer(t *testing.T) {
	optimizer := &DefaultOptimizer{}

	tests := []struct {
		name     string
		plan     *Plan
		validate func(*testing.T, *Plan, *Plan)
	}{
		{
			name: "remove duplicate steps",
			plan: &Plan{
				ID:   "plan_1",
				Goal: "Test goal",
				Steps: []*Step{
					{ID: "step_1", Name: "Same Step", Type: StepTypeAction, Description: "Desc 1"},
					{ID: "step_2", Name: "Same Step", Type: StepTypeAction, Description: "Desc 1"},
					{ID: "step_3", Name: "Different Step", Type: StepTypeAction, Description: "Desc 2"},
				},
				Dependencies: make(map[string][]string),
			},
			validate: func(t *testing.T, original, optimized *Plan) {
				assert.Less(t, len(optimized.Steps), len(original.Steps))
			},
		},
		{
			name: "optimize ordering by priority",
			plan: &Plan{
				ID:   "plan_1",
				Goal: "Test goal",
				Steps: []*Step{
					{ID: "step_1", Name: "Step 1", Priority: 3},
					{ID: "step_2", Name: "Step 2", Priority: 1},
					{ID: "step_3", Name: "Step 3", Priority: 2},
				},
				Dependencies: make(map[string][]string),
			},
			validate: func(t *testing.T, original, optimized *Plan) {
				// Verify steps are sorted by priority
				for i := 1; i < len(optimized.Steps); i++ {
					assert.LessOrEqual(t, optimized.Steps[i-1].Priority, optimized.Steps[i].Priority)
				}
			},
		},
		{
			name: "parallelize independent steps",
			plan: &Plan{
				ID:   "plan_1",
				Goal: "Test goal",
				Steps: []*Step{
					{ID: "step_1", Name: "Step 1", Priority: 1, Parameters: make(map[string]interface{})},
					{ID: "step_2", Name: "Step 2", Priority: 1, Parameters: make(map[string]interface{})},
					{ID: "step_3", Name: "Step 3", Priority: 2, Parameters: make(map[string]interface{})},
				},
				Dependencies: map[string][]string{
					"step_3": {"step_1", "step_2"}, // step_1 and step_2 are independent
				},
			},
			validate: func(t *testing.T, original, optimized *Plan) {
				// Check that some steps are marked as parallel
				parallelCount := 0
				for _, step := range optimized.Steps {
					if parallel, ok := step.Parameters["parallel"].(bool); ok && parallel {
						parallelCount++
					}
				}
				assert.Greater(t, parallelCount, 0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			optimized, err := optimizer.Optimize(ctx, tt.plan)

			require.NoError(t, err)
			assert.NotNil(t, optimized)
			if tt.validate != nil {
				tt.validate(t, tt.plan, optimized)
			}
		})
	}
}

// TestIsComplexStep tests complexity detection
func TestIsComplexStep(t *testing.T) {
	strategy := &DecompositionStrategy{}

	tests := []struct {
		name string
		step *Step
		want bool
	}{
		{
			name: "long description is complex",
			step: &Step{
				Description: "This is a very long description that exceeds the 100 character threshold and should be considered complex enough to require decomposition",
			},
			want: true,
		},
		{
			name: "analysis type is complex",
			step: &Step{
				Description: "Short",
				Type:        StepTypeAnalysis,
			},
			want: true,
		},
		{
			name: "simple action step",
			step: &Step{
				Description: "Short description",
				Type:        StepTypeAction,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strategy.isComplexStep(tt.step)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Benchmark tests
func BenchmarkDecompositionStrategy_Apply(b *testing.B) {
	strategy := &DecompositionStrategy{}
	plan := &Plan{
		ID:   "plan_1",
		Goal: "Test goal",
		Steps: []*Step{
			{
				ID:          "step_1",
				Name:        "Complex Step",
				Description: "This is a long description that will trigger decomposition for benchmarking purposes",
				Type:        StepTypeAnalysis,
			},
		},
		Dependencies: make(map[string][]string),
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = strategy.Apply(ctx, plan, PlanConstraints{})
	}
}

func BenchmarkDependencyValidator_Validate(b *testing.B) {
	validator := &DependencyValidator{}
	plan := &Plan{
		Steps: []*Step{
			{ID: "step_1"},
			{ID: "step_2"},
			{ID: "step_3"},
			{ID: "step_4"},
			{ID: "step_5"},
		},
		Dependencies: map[string][]string{
			"step_2": {"step_1"},
			"step_3": {"step_1", "step_2"},
			"step_4": {"step_2"},
			"step_5": {"step_3", "step_4"},
		},
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = validator.Validate(ctx, plan)
	}
}

func BenchmarkDefaultOptimizer_Optimize(b *testing.B) {
	optimizer := &DefaultOptimizer{}
	plan := &Plan{
		ID:   "plan_1",
		Goal: "Test goal",
		Steps: []*Step{
			{ID: "step_1", Name: "Step 1", Priority: 5, Parameters: make(map[string]interface{})},
			{ID: "step_2", Name: "Step 2", Priority: 2, Parameters: make(map[string]interface{})},
			{ID: "step_3", Name: "Step 3", Priority: 8, Parameters: make(map[string]interface{})},
			{ID: "step_4", Name: "Step 1", Type: StepTypeAction, Description: "Desc"},
			{ID: "step_5", Name: "Step 5", Priority: 1, Parameters: make(map[string]interface{})},
		},
		Dependencies: map[string][]string{
			"step_2": {"step_5"},
			"step_3": {"step_1"},
		},
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = optimizer.Optimize(ctx, plan)
	}
}
