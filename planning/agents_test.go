package planning

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
)

// TestNewPlanningAgent tests planning agent creation
func TestNewPlanningAgent(t *testing.T) {
	llmClient := &MockLLMClient{}
	mem := &MockMemoryManager{}
	planner := NewSmartPlanner(llmClient, mem)

	logger := &MockLogger{}
	executor := NewAgentExecutor(logger)

	agent := NewPlanningAgent(planner, executor)

	assert.NotNil(t, agent)
	assert.Equal(t, "planning_agent", agent.Name())
	assert.NotEmpty(t, agent.Description())
	assert.Contains(t, agent.Capabilities(), "planning")
}

// TestPlanningAgent_Execute tests planning agent execution
func TestPlanningAgent_Execute(t *testing.T) {
	llmClient := &MockLLMClient{}
	mem := &MockMemoryManager{}
	planner := NewSmartPlanner(llmClient, mem)

	logger := &MockLogger{}
	executor := NewAgentExecutor(logger)
	executor.registry.RegisterAgent("action_agent", &MockAgent{name: "action_agent"})
	executor.registry.RegisterAgent("analysis_agent", &MockAgent{name: "analysis_agent"})
	executor.registry.RegisterAgent("validation_agent", &MockAgent{name: "validation_agent"})

	agent := NewPlanningAgent(planner, executor)

	tests := []struct {
		name     string
		input    *core.AgentInput
		wantErr  bool
		validate func(*testing.T, *core.AgentOutput)
	}{
		{
			name: "create plan only",
			input: &core.AgentInput{
				Task: "Create a plan",
				Context: map[string]interface{}{
					"goal": "Build a web application",
				},
			},
			wantErr: false,
			validate: func(t *testing.T, output *core.AgentOutput) {
				assert.NotNil(t, output.Result)
				plan, ok := output.Result.(*Plan)
				assert.True(t, ok)
				assert.NotEmpty(t, plan.ID)
				assert.Equal(t, "Build a web application", plan.Goal)
			},
		},
		{
			name: "create and execute plan",
			input: &core.AgentInput{
				Task: "Create and execute plan",
				Context: map[string]interface{}{
					"goal":    "Test goal",
					"execute": true,
				},
			},
			wantErr: false,
			validate: func(t *testing.T, output *core.AgentOutput) {
				result, ok := output.Result.(map[string]interface{})
				assert.True(t, ok)
				assert.Contains(t, result, "plan")
				assert.Contains(t, result, "result")
			},
		},
		{
			name: "execute existing plan",
			input: &core.AgentInput{
				Task: "Execute plan",
				Context: map[string]interface{}{
					"goal": "Test goal",
					"plan": &Plan{
						ID:   "plan_123",
						Goal: "Test goal",
						Steps: []*Step{
							{ID: "step_1", Type: StepTypeAction, Status: StepStatusPending},
						},
						Dependencies: make(map[string][]string),
						Status:       PlanStatusReady,
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, output *core.AgentOutput) {
				result, ok := output.Result.(*PlanResult)
				assert.True(t, ok)
				assert.NotNil(t, result)
			},
		},
		{
			name: "missing goal",
			input: &core.AgentInput{
				Task:    "Create plan",
				Context: map[string]interface{}{},
			},
			wantErr:  true,
			validate: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			output, err := agent.Execute(ctx, tt.input)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, output)
				if tt.validate != nil {
					tt.validate(t, output)
				}
			}
		})
	}
}

// TestNewTaskDecompositionAgent tests task decomposition agent creation
func TestNewTaskDecompositionAgent(t *testing.T) {
	llmClient := &MockLLMClient{}
	mem := &MockMemoryManager{}
	planner := NewSmartPlanner(llmClient, mem)

	agent := NewTaskDecompositionAgent(planner)

	assert.NotNil(t, agent)
	assert.Equal(t, "task_decomposition", agent.Name())
	assert.Contains(t, agent.Capabilities(), "decomposition")
}

// TestTaskDecompositionAgent_Execute tests task decomposition
func TestTaskDecompositionAgent_Execute(t *testing.T) {
	llmClient := &MockLLMClient{}
	mem := &MockMemoryManager{}
	planner := NewSmartPlanner(llmClient, mem)

	agent := NewTaskDecompositionAgent(planner)

	tests := []struct {
		name     string
		input    *core.AgentInput
		wantErr  bool
		validate func(*testing.T, *core.AgentOutput)
	}{
		{
			name: "decompose complex task",
			input: &core.AgentInput{
				Task: "Decompose this task",
				Context: map[string]interface{}{
					"task": "Build a complete e-commerce system",
				},
			},
			wantErr: false,
			validate: func(t *testing.T, output *core.AgentOutput) {
				subtasks, ok := output.Result.([]map[string]interface{})
				assert.True(t, ok)
				assert.NotEmpty(t, subtasks)

				// Verify subtask structure
				for _, subtask := range subtasks {
					assert.Contains(t, subtask, "id")
					assert.Contains(t, subtask, "name")
					assert.Contains(t, subtask, "description")
				}
			},
		},
		{
			name: "missing task",
			input: &core.AgentInput{
				Task:    "Decompose",
				Context: map[string]interface{}{},
			},
			wantErr:  true,
			validate: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			output, err := agent.Execute(ctx, tt.input)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, output)
				if tt.validate != nil {
					tt.validate(t, output)
				}
			}
		})
	}
}

// TestNewStrategyAgent tests strategy agent creation
func TestNewStrategyAgent(t *testing.T) {
	agent := NewStrategyAgent()

	assert.NotNil(t, agent)
	assert.Equal(t, "strategy_agent", agent.Name())
	assert.Contains(t, agent.Capabilities(), "strategy")

	// Should have default strategies registered
	assert.Contains(t, agent.strategies, "decomposition")
	assert.Contains(t, agent.strategies, "backward_chaining")
	assert.Contains(t, agent.strategies, "hierarchical")
}

// TestStrategyAgent_Execute tests strategy selection and application
func TestStrategyAgent_Execute(t *testing.T) {
	agent := NewStrategyAgent()

	plan := &Plan{
		ID:   "plan_123",
		Goal: "Test goal",
		Steps: []*Step{
			{ID: "step_1", Type: StepTypeAction},
		},
		Dependencies: make(map[string][]string),
	}

	tests := []struct {
		name     string
		input    *core.AgentInput
		wantErr  bool
		validate func(*testing.T, *core.AgentOutput)
	}{
		{
			name: "apply decomposition strategy",
			input: &core.AgentInput{
				Task: "Apply strategy",
				Context: map[string]interface{}{
					"plan":     plan,
					"strategy": "decomposition",
				},
			},
			wantErr: false,
			validate: func(t *testing.T, output *core.AgentOutput) {
				result, ok := output.Result.(*Plan)
				assert.True(t, ok)
				assert.Equal(t, PlanStatusReady, result.Status)
			},
		},
		{
			name: "apply backward chaining strategy",
			input: &core.AgentInput{
				Task: "Apply strategy",
				Context: map[string]interface{}{
					"plan":     plan,
					"strategy": "backward_chaining",
				},
			},
			wantErr: false,
			validate: func(t *testing.T, output *core.AgentOutput) {
				result, ok := output.Result.(*Plan)
				assert.True(t, ok)
				assert.NotNil(t, result)
			},
		},
		{
			name: "default strategy",
			input: &core.AgentInput{
				Task: "Apply strategy",
				Context: map[string]interface{}{
					"plan": plan,
				},
			},
			wantErr: false,
			validate: func(t *testing.T, output *core.AgentOutput) {
				assert.NotNil(t, output.Result)
			},
		},
		{
			name: "non-existent strategy",
			input: &core.AgentInput{
				Task: "Apply strategy",
				Context: map[string]interface{}{
					"plan":     plan,
					"strategy": "nonexistent",
				},
			},
			wantErr:  true,
			validate: nil,
		},
		{
			name: "missing plan",
			input: &core.AgentInput{
				Task:    "Apply strategy",
				Context: map[string]interface{}{},
			},
			wantErr:  true,
			validate: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			output, err := agent.Execute(ctx, tt.input)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, output)
				if tt.validate != nil {
					tt.validate(t, output)
				}
			}
		})
	}
}

// TestStrategyAgent_RegisterStrategy tests custom strategy registration
func TestStrategyAgent_RegisterStrategy(t *testing.T) {
	agent := NewStrategyAgent()

	customStrategy := &NoOpStrategy{}
	agent.RegisterStrategy("custom", customStrategy)

	assert.Contains(t, agent.strategies, "custom")
	assert.Equal(t, customStrategy, agent.strategies["custom"])
}

// TestNewOptimizationAgent tests optimization agent creation
func TestNewOptimizationAgent(t *testing.T) {
	t.Run("with optimizer", func(t *testing.T) {
		optimizer := &DefaultOptimizer{}
		agent := NewOptimizationAgent(optimizer)

		assert.NotNil(t, agent)
		assert.Equal(t, "optimization_agent", agent.Name())
		assert.Equal(t, optimizer, agent.optimizer)
	})

	t.Run("without optimizer", func(t *testing.T) {
		agent := NewOptimizationAgent(nil)

		assert.NotNil(t, agent)
		assert.NotNil(t, agent.optimizer) // Should use default
	})
}

// TestOptimizationAgent_Execute tests plan optimization
func TestOptimizationAgent_Execute(t *testing.T) {
	optimizer := &DefaultOptimizer{}
	agent := NewOptimizationAgent(optimizer)

	plan := &Plan{
		ID:   "plan_123",
		Goal: "Test goal",
		Steps: []*Step{
			{ID: "step_1", Name: "Step 1", Type: StepTypeAction, Description: "Desc 1", Priority: 2, Parameters: make(map[string]interface{})},
			{ID: "step_2", Name: "Step 1", Type: StepTypeAction, Description: "Desc 1", Priority: 3, Parameters: make(map[string]interface{})}, // Duplicate
			{ID: "step_3", Name: "Step 3", Type: StepTypeAction, Description: "Desc 3", Priority: 1, Parameters: make(map[string]interface{})},
		},
		Dependencies: make(map[string][]string),
	}

	tests := []struct {
		name     string
		input    *core.AgentInput
		wantErr  bool
		validate func(*testing.T, *core.AgentOutput)
	}{
		{
			name: "optimize plan",
			input: &core.AgentInput{
				Task: "Optimize plan",
				Context: map[string]interface{}{
					"plan": plan,
				},
			},
			wantErr: false,
			validate: func(t *testing.T, output *core.AgentOutput) {
				result, ok := output.Result.(*Plan)
				assert.True(t, ok)
				assert.NotNil(t, result)

				// Check metadata
				assert.Contains(t, output.Metadata, "original_steps")
				assert.Contains(t, output.Metadata, "optimized_steps")
				assert.Contains(t, output.Metadata, "reduction_pct")
			},
		},
		{
			name: "missing plan",
			input: &core.AgentInput{
				Task:    "Optimize plan",
				Context: map[string]interface{}{},
			},
			wantErr:  true,
			validate: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			output, err := agent.Execute(ctx, tt.input)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, output)
				if tt.validate != nil {
					tt.validate(t, output)
				}
			}
		})
	}
}

// TestNewValidationAgent tests validation agent creation
func TestNewValidationAgent(t *testing.T) {
	agent := NewValidationAgent()

	assert.NotNil(t, agent)
	assert.Equal(t, "validation_agent", agent.Name())
	assert.Contains(t, agent.Capabilities(), "validation")

	// Should have default validators
	assert.Equal(t, 3, len(agent.validators))
}

// TestValidationAgent_Execute tests plan validation
func TestValidationAgent_Execute(t *testing.T) {
	agent := NewValidationAgent()

	tests := []struct {
		name     string
		input    *core.AgentInput
		wantErr  bool
		validate func(*testing.T, *core.AgentOutput)
	}{
		{
			name: "valid plan",
			input: &core.AgentInput{
				Task: "Validate plan",
				Context: map[string]interface{}{
					"plan": &Plan{
						ID:   "plan_123",
						Goal: "Test goal",
						Steps: []*Step{
							{ID: "step_1"},
							{ID: "step_2"},
						},
						Dependencies: map[string][]string{
							"step_2": {"step_1"},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, output *core.AgentOutput) {
				result, ok := output.Result.(map[string]interface{})
				assert.True(t, ok)
				assert.Contains(t, result, "valid")
				assert.Contains(t, result, "issues")

				valid := result["valid"].(bool)
				assert.True(t, valid)
			},
		},
		{
			name: "invalid plan with circular dependency",
			input: &core.AgentInput{
				Task: "Validate plan",
				Context: map[string]interface{}{
					"plan": &Plan{
						ID:   "plan_123",
						Goal: "Test goal",
						Steps: []*Step{
							{ID: "step_1"},
							{ID: "step_2"},
						},
						Dependencies: map[string][]string{
							"step_1": {"step_2"},
							"step_2": {"step_1"},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, output *core.AgentOutput) {
				result, ok := output.Result.(map[string]interface{})
				assert.True(t, ok)

				valid := result["valid"].(bool)
				assert.False(t, valid)

				issues := result["issues"].([]string)
				assert.NotEmpty(t, issues)
			},
		},
		{
			name: "missing plan",
			input: &core.AgentInput{
				Task:    "Validate plan",
				Context: map[string]interface{}{},
			},
			wantErr:  true,
			validate: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			output, err := agent.Execute(ctx, tt.input)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, output)
				if tt.validate != nil {
					tt.validate(t, output)
				}
			}
		})
	}
}

// TestValidationAgent_AddValidator tests adding custom validators
func TestValidationAgent_AddValidator(t *testing.T) {
	agent := NewValidationAgent()
	initialCount := len(agent.validators)

	customValidator := &TimeValidator{}
	agent.AddValidator(customValidator)

	assert.Equal(t, initialCount+1, len(agent.validators))
}

// Benchmark tests
func BenchmarkPlanningAgent_Execute(b *testing.B) {
	llmClient := &MockLLMClient{}
	mem := &MockMemoryManager{}
	planner := NewSmartPlanner(llmClient, mem)

	logger := &MockLogger{}
	executor := NewAgentExecutor(logger)
	executor.registry.RegisterAgent("action_agent", &MockAgent{name: "action_agent"})

	agent := NewPlanningAgent(planner, executor)

	input := &core.AgentInput{
		Task: "Create plan",
		Context: map[string]interface{}{
			"goal": "Build a web application",
		},
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = agent.Execute(ctx, input)
	}
}

func BenchmarkTaskDecompositionAgent_Execute(b *testing.B) {
	llmClient := &MockLLMClient{}
	mem := &MockMemoryManager{}
	planner := NewSmartPlanner(llmClient, mem)

	agent := NewTaskDecompositionAgent(planner)

	input := &core.AgentInput{
		Task: "Decompose task",
		Context: map[string]interface{}{
			"task": "Build an e-commerce system",
		},
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = agent.Execute(ctx, input)
	}
}

func BenchmarkOptimizationAgent_Execute(b *testing.B) {
	optimizer := &DefaultOptimizer{}
	agent := NewOptimizationAgent(optimizer)

	plan := &Plan{
		ID:   "plan_123",
		Goal: "Test goal",
		Steps: []*Step{
			{ID: "step_1", Name: "Step 1", Priority: 2, Parameters: make(map[string]interface{})},
			{ID: "step_2", Name: "Step 2", Priority: 3, Parameters: make(map[string]interface{})},
			{ID: "step_3", Name: "Step 3", Priority: 1, Parameters: make(map[string]interface{})},
		},
		Dependencies: make(map[string][]string),
	}

	input := &core.AgentInput{
		Task: "Optimize plan",
		Context: map[string]interface{}{
			"plan": plan,
		},
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = agent.Execute(ctx, input)
	}
}

func BenchmarkValidationAgent_Execute(b *testing.B) {
	agent := NewValidationAgent()

	plan := &Plan{
		ID:   "plan_123",
		Goal: "Test goal",
		Steps: []*Step{
			{ID: "step_1"},
			{ID: "step_2"},
			{ID: "step_3"},
		},
		Dependencies: map[string][]string{
			"step_2": {"step_1"},
			"step_3": {"step_2"},
		},
	}

	input := &core.AgentInput{
		Task: "Validate plan",
		Context: map[string]interface{}{
			"plan": plan,
		},
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = agent.Execute(ctx, input)
	}
}
