package planning

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

// TestNewSmartPlanner tests the creation of a SmartPlanner
func TestNewSmartPlanner(t *testing.T) {
	tests := []struct {
		name string
		opts []PlannerOption
		want func(*SmartPlanner) bool
	}{
		{
			name: "default planner",
			opts: nil,
			want: func(p *SmartPlanner) bool {
				return p.maxPlanDepth == 5 &&
					p.maxRetries == 3 &&
					p.timeout == 5*time.Minute &&
					len(p.strategies) == 3 && // 3 default strategies
					len(p.validators) == 3 // 3 default validators
			},
		},
		{
			name: "with custom depth",
			opts: []PlannerOption{WithMaxDepth(10)},
			want: func(p *SmartPlanner) bool {
				return p.maxPlanDepth == 10
			},
		},
		{
			name: "with custom timeout",
			opts: []PlannerOption{WithTimeout(10 * time.Minute)},
			want: func(p *SmartPlanner) bool {
				return p.timeout == 10*time.Minute
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			llmClient := &MockLLMClient{}
			mem := &MockMemoryManager{}
			planner := NewSmartPlanner(llmClient, mem, tt.opts...)

			assert.NotNil(t, planner)
			assert.True(t, tt.want(planner))
		})
	}
}

// TestSmartPlanner_CreatePlan tests plan creation
func TestSmartPlanner_CreatePlan(t *testing.T) {
	tests := []struct {
		name        string
		goal        string
		constraints PlanConstraints
		llmResponse string
		wantErr     bool
		validate    func(*testing.T, *Plan)
	}{
		{
			name: "successful plan creation",
			goal: "Build a web application",
			constraints: PlanConstraints{
				MaxSteps: 5,
			},
			llmResponse: "Strategy: decomposition\n\nStep 1: Design\nStep 2: Develop\nStep 3: Test",
			wantErr:     false,
			validate: func(t *testing.T, p *Plan) {
				assert.NotEmpty(t, p.ID)
				assert.Equal(t, "Build a web application", p.Goal)
				assert.NotEmpty(t, p.Steps)
				assert.Equal(t, PlanStatusReady, p.Status)
			},
		},
		{
			name:        "llm error",
			goal:        "Test goal",
			llmResponse: "",
			wantErr:     true,
			validate:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			llmClient := &MockLLMClient{}
			if tt.wantErr {
				llmClient.CompleteFn = func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
					return nil, agentErrors.New(agentErrors.CodeLLMRequest, "LLM error")
				}
			} else {
				llmClient.CompleteFn = func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
					return &llm.CompletionResponse{
						Content: tt.llmResponse,
					}, nil
				}
			}

			mem := &MockMemoryManager{}
			planner := NewSmartPlanner(llmClient, mem)

			ctx := context.Background()
			plan, err := planner.CreatePlan(ctx, tt.goal, tt.constraints)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, plan)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, plan)
				if tt.validate != nil {
					tt.validate(t, plan)
				}
			}
		})
	}
}

// TestSmartPlanner_RefinePlan tests plan refinement
func TestSmartPlanner_RefinePlan(t *testing.T) {
	llmClient := &MockLLMClient{
		CompleteFn: func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return &llm.CompletionResponse{
				Content: "Refined plan content",
			}, nil
		},
	}

	mem := &MockMemoryManager{}
	planner := NewSmartPlanner(llmClient, mem)

	originalPlan := &Plan{
		ID:       "plan_123",
		Goal:     "Test goal",
		Strategy: "decomposition",
		Steps: []*Step{
			{ID: "step_1", Name: "Step 1"},
		},
		Context:   make(map[string]interface{}),
		CreatedAt: time.Now(),
	}

	ctx := context.Background()
	refined, err := planner.RefinePlan(ctx, originalPlan, "Need more detail")

	require.NoError(t, err)
	assert.NotNil(t, refined)
	assert.Equal(t, originalPlan.ID, refined.ID)
	assert.Equal(t, originalPlan.Goal, refined.Goal)
}

// TestSmartPlanner_DecomposePlan tests step decomposition
func TestSmartPlanner_DecomposePlan(t *testing.T) {
	llmClient := &MockLLMClient{
		CompleteFn: func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return &llm.CompletionResponse{
				Content: "Sub-step 1: Prepare\nSub-step 2: Execute\nSub-step 3: Verify",
			}, nil
		},
	}

	mem := &MockMemoryManager{}
	planner := NewSmartPlanner(llmClient, mem)

	plan := &Plan{
		ID:   "plan_123",
		Goal: "Test goal",
	}

	step := &Step{
		ID:          "step_1",
		Name:        "Complex Step",
		Description: "A complex step that needs decomposition",
		Type:        StepTypeAnalysis,
	}

	ctx := context.Background()
	subSteps, err := planner.DecomposePlan(ctx, plan, step)

	require.NoError(t, err)
	assert.NotEmpty(t, subSteps)

	// Verify sub-step IDs are properly formatted
	for _, subStep := range subSteps {
		assert.Contains(t, subStep.ID, "step_1.")
	}
}

// TestSmartPlanner_ValidatePlan tests plan validation
func TestSmartPlanner_ValidatePlan(t *testing.T) {
	tests := []struct {
		name       string
		plan       *Plan
		wantValid  bool
		wantIssues int
	}{
		{
			name: "valid plan",
			plan: &Plan{
				ID:   "plan_123",
				Goal: "Test goal",
				Steps: []*Step{
					{ID: "step_1", Name: "Step 1"},
					{ID: "step_2", Name: "Step 2"},
				},
				Dependencies: map[string][]string{
					"step_2": {"step_1"},
				},
			},
			wantValid:  true,
			wantIssues: 0,
		},
		{
			name: "circular dependency",
			plan: &Plan{
				ID:   "plan_123",
				Goal: "Test goal",
				Steps: []*Step{
					{ID: "step_1", Name: "Step 1"},
					{ID: "step_2", Name: "Step 2"},
				},
				Dependencies: map[string][]string{
					"step_1": {"step_2"},
					"step_2": {"step_1"},
				},
			},
			wantValid:  false,
			wantIssues: 1, // At least one issue
		},
		{
			name: "missing dependency",
			plan: &Plan{
				ID:   "plan_123",
				Goal: "Test goal",
				Steps: []*Step{
					{ID: "step_1", Name: "Step 1"},
				},
				Dependencies: map[string][]string{
					"step_1": {"step_999"}, // Non-existent step
				},
			},
			wantValid:  false,
			wantIssues: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			llmClient := &MockLLMClient{}
			mem := &MockMemoryManager{}
			planner := NewSmartPlanner(llmClient, mem)

			ctx := context.Background()
			valid, issues, err := planner.ValidatePlan(ctx, tt.plan)

			require.NoError(t, err)
			assert.Equal(t, tt.wantValid, valid)
			if tt.wantIssues > 0 {
				assert.GreaterOrEqual(t, len(issues), tt.wantIssues)
			}
		})
	}
}

// TestSmartPlanner_RegisterStrategy tests strategy registration
func TestSmartPlanner_RegisterStrategy(t *testing.T) {
	llmClient := &MockLLMClient{}
	mem := &MockMemoryManager{}
	planner := NewSmartPlanner(llmClient, mem)

	customStrategy := &NoOpStrategy{}
	planner.RegisterStrategy("custom", customStrategy)

	planner.mu.RLock()
	strategy, exists := planner.strategies["custom"]
	planner.mu.RUnlock()

	assert.True(t, exists)
	assert.Equal(t, customStrategy, strategy)
}

// TestSmartPlanner_AddValidator tests validator addition
func TestSmartPlanner_AddValidator(t *testing.T) {
	llmClient := &MockLLMClient{}
	mem := &MockMemoryManager{}
	planner := NewSmartPlanner(llmClient, mem)

	initialCount := len(planner.validators)

	customValidator := &DependencyValidator{}
	planner.AddValidator(customValidator)

	assert.Equal(t, initialCount+1, len(planner.validators))
}

// TestParsePlan tests plan parsing
func TestParsePlan(t *testing.T) {
	llmClient := &MockLLMClient{}
	mem := &MockMemoryManager{}
	planner := NewSmartPlanner(llmClient, mem)

	content := `Strategy: decomposition

Step 1: Analyze requirements
Step 2: Design system
Step 3: Implement features`

	goal := "Build a system"
	plan := planner.parsePlan(content, goal)

	assert.NotNil(t, plan)
	assert.Equal(t, goal, plan.Goal)
	assert.NotEmpty(t, plan.ID)
	assert.Equal(t, PlanStatusDraft, plan.Status)
	assert.NotEmpty(t, plan.Steps) // Should have default steps
}

// TestBuildPlanPrompt tests prompt building
func TestBuildPlanPrompt(t *testing.T) {
	llmClient := &MockLLMClient{}
	mem := &MockMemoryManager{}
	planner := NewSmartPlanner(llmClient, mem)

	goal := "Build a web application"
	constraints := PlanConstraints{
		MaxSteps:    5,
		MaxDuration: 1 * time.Hour,
	}

	similarPlans := []*Plan{
		{
			Goal:     "Similar project",
			Strategy: "decomposition",
			Steps:    []*Step{{}, {}},
			Metrics:  &PlanMetrics{SuccessRate: 0.95},
		},
	}

	prompt := planner.buildPlanPrompt(goal, constraints, similarPlans)

	assert.Contains(t, prompt, goal)
	assert.Contains(t, prompt, "Maximum steps: 5")
	assert.Contains(t, prompt, "Similar project")
}

// TestSelectStrategy tests strategy selection
func TestSelectStrategy(t *testing.T) {
	llmClient := &MockLLMClient{}
	mem := &MockMemoryManager{}
	planner := NewSmartPlanner(llmClient, mem)

	tests := []struct {
		name        string
		goal        string
		constraints PlanConstraints
		wantName    string
	}{
		{
			name: "backward chaining for small steps",
			goal: "Simple task",
			constraints: PlanConstraints{
				MaxSteps: 3,
			},
			wantName: "backward_chaining",
		},
		{
			name: "hierarchical for complex goals",
			goal: "Complex multi-stage project",
			constraints: PlanConstraints{
				MaxSteps: 10,
			},
			wantName: "hierarchical",
		},
		{
			name: "decomposition as default",
			goal: "Regular task",
			constraints: PlanConstraints{
				MaxSteps: 7,
			},
			wantName: "decomposition",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strategy := planner.selectStrategy(tt.goal, tt.constraints)
			assert.NotNil(t, strategy)
			assert.Equal(t, tt.wantName, strategy.Name())
		})
	}
}

// TestDefaultOptimization tests the default optimization
func TestDefaultOptimization(t *testing.T) {
	llmClient := &MockLLMClient{}
	mem := &MockMemoryManager{}
	planner := NewSmartPlanner(llmClient, mem)

	plan := &Plan{
		ID:   "plan_123",
		Goal: "Test goal",
		Steps: []*Step{
			{ID: "step_1", Name: "Step 1", Type: StepTypeAnalysis, Priority: 1},
			{ID: "step_2", Name: "Step 1", Type: StepTypeAnalysis, Priority: 2}, // Duplicate
			{ID: "step_3", Name: "Step 3", Type: StepTypeAction, Priority: 3},
		},
		Dependencies: make(map[string][]string),
	}

	ctx := context.Background()
	optimized, err := planner.defaultOptimization(ctx, plan)

	require.NoError(t, err)
	assert.NotNil(t, optimized)
	assert.LessOrEqual(t, len(optimized.Steps), len(plan.Steps)) // Should remove duplicates
}

// Benchmark tests
func BenchmarkSmartPlanner_CreatePlan(b *testing.B) {
	llmClient := &MockLLMClient{}
	mem := &MockMemoryManager{}
	planner := NewSmartPlanner(llmClient, mem)

	ctx := context.Background()
	constraints := PlanConstraints{MaxSteps: 5}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = planner.CreatePlan(ctx, "Test goal", constraints)
	}
}

func BenchmarkSmartPlanner_ValidatePlan(b *testing.B) {
	llmClient := &MockLLMClient{}
	mem := &MockMemoryManager{}
	planner := NewSmartPlanner(llmClient, mem)

	plan := &Plan{
		ID:   "plan_123",
		Goal: "Test goal",
		Steps: []*Step{
			{ID: "step_1", Name: "Step 1"},
			{ID: "step_2", Name: "Step 2"},
			{ID: "step_3", Name: "Step 3"},
		},
		Dependencies: map[string][]string{
			"step_2": {"step_1"},
			"step_3": {"step_2"},
		},
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = planner.ValidatePlan(ctx, plan)
	}
}
