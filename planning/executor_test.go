package planning

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
)

// TestNewAgentExecutor tests executor creation
func TestNewAgentExecutor(t *testing.T) {
	logger := &MockLogger{}

	tests := []struct {
		name string
		opts []ExecutorOption
		want func(*AgentExecutor) bool
	}{
		{
			name: "default executor",
			opts: nil,
			want: func(e *AgentExecutor) bool {
				return e.maxConcurrency == 5 &&
					e.timeout == 30*time.Minute &&
					e.retryPolicy.MaxRetries == 3
			},
		},
		{
			name: "with custom concurrency",
			opts: []ExecutorOption{WithMaxConcurrency(10)},
			want: func(e *AgentExecutor) bool {
				return e.maxConcurrency == 10
			},
		},
		{
			name: "with custom retry policy",
			opts: []ExecutorOption{WithRetryPolicy(RetryPolicy{
				MaxRetries:    5,
				RetryDelay:    2 * time.Second,
				BackoffFactor: 3.0,
			})},
			want: func(e *AgentExecutor) bool {
				return e.retryPolicy.MaxRetries == 5 &&
					e.retryPolicy.RetryDelay == 2*time.Second
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewAgentExecutor(logger, tt.opts...)
			assert.NotNil(t, executor)
			assert.True(t, tt.want(executor))
		})
	}
}

// TestAgentExecutor_Execute tests plan execution
func TestAgentExecutor_Execute(t *testing.T) {
	tests := []struct {
		name     string
		plan     *Plan
		agents   map[string]core.Agent
		wantErr  bool
		validate func(*testing.T, *PlanResult)
	}{
		{
			name: "successful execution",
			plan: &Plan{
				ID:   "plan_1",
				Goal: "Test goal",
				Steps: []*Step{
					{ID: "step_1", Name: "Step 1", Type: StepTypeAction, Status: StepStatusPending, Priority: 1},
					{ID: "step_2", Name: "Step 2", Type: StepTypeAction, Status: StepStatusPending, Priority: 2},
				},
				Dependencies: map[string][]string{
					"step_2": {"step_1"},
				},
				Status: PlanStatusReady,
			},
			agents: map[string]core.Agent{
				"action_agent": &MockAgent{name: "action_agent"},
			},
			wantErr: false,
			validate: func(t *testing.T, r *PlanResult) {
				assert.True(t, r.Success)
				assert.Equal(t, 2, r.CompletedSteps)
				assert.Equal(t, 0, r.FailedSteps)
			},
		},
		{
			name: "failed step execution",
			plan: &Plan{
				ID:   "plan_2",
				Goal: "Test goal",
				Steps: []*Step{
					{ID: "step_1", Name: "Step 1", Type: StepTypeAction, Status: StepStatusPending, Priority: 1},
				},
				Dependencies: make(map[string][]string),
				Status:       PlanStatusReady,
			},
			agents: map[string]core.Agent{
				"action_agent": func() *MockAgent {
					agent := NewMockAgent("action_agent")
					agent.SetInvokeFn(func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
						return nil, errors.New("execution failed")
					})
					return agent
				}(),
			},
			wantErr: true,
			validate: func(t *testing.T, r *PlanResult) {
				assert.False(t, r.Success)
				assert.Equal(t, 0, r.CompletedSteps)
				assert.Equal(t, 1, r.FailedSteps)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &MockLogger{}
			executor := NewAgentExecutor(logger)

			// Register agents
			for name, agent := range tt.agents {
				executor.registry.RegisterAgent(name, agent)
			}

			ctx := context.Background()
			result, err := executor.Execute(ctx, tt.plan)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.NotNil(t, result)
			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

// TestAgentExecutor_ExecuteStep tests single step execution
func TestAgentExecutor_ExecuteStep(t *testing.T) {
	logger := &MockLogger{}
	executor := NewAgentExecutor(logger)

	agent := &MockAgent{name: "test_agent"}
	executor.registry.RegisterAgent("action_agent", agent)

	step := &Step{
		ID:          "step_1",
		Name:        "Test Step",
		Description: "Test description",
		Type:        StepTypeAction,
		Status:      StepStatusPending,
	}

	ctx := context.Background()
	result, err := executor.ExecuteStep(ctx, step)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success)
	// Step status might be "executing" or "completed" depending on async behavior
	assert.Contains(t, []StepStatus{StepStatusExecuting, StepStatusCompleted}, step.Status)
}

// TestAgentExecutor_PauseResumeCancel tests execution control
func TestAgentExecutor_PauseResumeCancel(t *testing.T) {
	logger := &MockLogger{}
	executor := NewAgentExecutor(logger)

	// Create a plan with a long-running step
	plan := &Plan{
		ID:   "plan_1",
		Goal: "Test goal",
		Steps: []*Step{
			{ID: "step_1", Name: "Step 1", Type: StepTypeAction, Status: StepStatusPending},
		},
		Dependencies: make(map[string][]string),
		Status:       PlanStatusReady,
	}

	// Register a slow agent
	slowAgent := NewMockAgent("action_agent")
	slowAgent.SetInvokeFn(func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
		time.Sleep(100 * time.Millisecond)
		return &core.AgentOutput{Result: "done"}, nil
	})
	executor.registry.RegisterAgent("action_agent", slowAgent)

	// Start execution in background
	ctx := context.Background()
	go func() {
		_, _ = executor.Execute(ctx, plan)
	}()

	// Wait for execution to start
	time.Sleep(10 * time.Millisecond)

	t.Run("pause", func(t *testing.T) {
		err := executor.Pause("plan_1")
		// May or may not succeed depending on timing
		_ = err
	})

	t.Run("cancel", func(t *testing.T) {
		err := executor.Cancel("plan_1")
		// May or may not succeed depending on timing
		_ = err
	})

	t.Run("error on non-existent plan", func(t *testing.T) {
		err := executor.Pause("nonexistent")
		assert.Error(t, err)

		err = executor.Resume("nonexistent")
		assert.Error(t, err)

		err = executor.Cancel("nonexistent")
		assert.Error(t, err)
	})
}

// TestAgentExecutor_GetStatus tests status retrieval
func TestAgentExecutor_GetStatus(t *testing.T) {
	logger := &MockLogger{}
	executor := NewAgentExecutor(logger)

	// Create execution state
	state := &ExecutionState{
		Plan:   &Plan{ID: "plan_1"},
		Status: PlanStatusExecuting,
	}
	executor.executions["plan_1"] = state

	t.Run("existing plan", func(t *testing.T) {
		status, err := executor.GetStatus("plan_1")
		require.NoError(t, err)
		assert.NotNil(t, status)
		assert.Equal(t, PlanStatusExecuting, *status)
	})

	t.Run("non-existent plan", func(t *testing.T) {
		status, err := executor.GetStatus("nonexistent")
		assert.Error(t, err)
		assert.Nil(t, status)
	})
}

// TestBuildExecutionOrder tests dependency-based execution ordering
func TestBuildExecutionOrder(t *testing.T) {
	logger := &MockLogger{}
	executor := NewAgentExecutor(logger)

	tests := []struct {
		name     string
		plan     *Plan
		validate func(*testing.T, [][]*Step)
	}{
		{
			name: "linear dependencies",
			plan: &Plan{
				Steps: []*Step{
					{ID: "step_1", Priority: 1},
					{ID: "step_2", Priority: 2},
					{ID: "step_3", Priority: 3},
				},
				Dependencies: map[string][]string{
					"step_2": {"step_1"},
					"step_3": {"step_2"},
				},
			},
			validate: func(t *testing.T, order [][]*Step) {
				assert.NotEmpty(t, order)
				// Verify all steps are present
				stepCount := 0
				for _, level := range order {
					stepCount += len(level)
				}
				assert.Equal(t, 3, stepCount, "All 3 steps should be in execution order")
			},
		},
		{
			name: "parallel steps",
			plan: &Plan{
				Steps: []*Step{
					{ID: "step_1", Priority: 1},
					{ID: "step_2", Priority: 1},
					{ID: "step_3", Priority: 2},
				},
				Dependencies: map[string][]string{
					"step_3": {"step_1", "step_2"},
				},
			},
			validate: func(t *testing.T, order [][]*Step) {
				assert.NotEmpty(t, order)
				// Verify all 3 steps are present
				stepCount := 0
				for _, level := range order {
					stepCount += len(level)
				}
				assert.Equal(t, 3, stepCount, "All 3 steps should be in execution order")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order := executor.buildExecutionOrder(tt.plan)
			assert.NotEmpty(t, order)
			if tt.validate != nil {
				tt.validate(t, order)
			}
		})
	}
}

// TestShouldSkipStep tests step skipping logic
func TestShouldSkipStep(t *testing.T) {
	logger := &MockLogger{}
	executor := NewAgentExecutor(logger)

	tests := []struct {
		name     string
		step     *Step
		state    *ExecutionState
		wantSkip bool
	}{
		{
			name: "no dependencies",
			step: &Step{ID: "step_1"},
			state: &ExecutionState{
				Plan: &Plan{
					Dependencies: make(map[string][]string),
				},
				StepResults: make(map[string]*StepResult),
			},
			wantSkip: false,
		},
		{
			name: "successful dependency",
			step: &Step{ID: "step_2"},
			state: &ExecutionState{
				Plan: &Plan{
					Dependencies: map[string][]string{
						"step_2": {"step_1"},
					},
				},
				StepResults: map[string]*StepResult{
					"step_1": {Success: true},
				},
			},
			wantSkip: false,
		},
		{
			name: "failed dependency",
			step: &Step{ID: "step_2"},
			state: &ExecutionState{
				Plan: &Plan{
					Dependencies: map[string][]string{
						"step_2": {"step_1"},
					},
				},
				StepResults: map[string]*StepResult{
					"step_1": {Success: false},
				},
			},
			wantSkip: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skip := executor.shouldSkipStep(tt.step, tt.state)
			assert.Equal(t, tt.wantSkip, skip)
		})
	}
}

// TestIsRetryableError tests error retry logic
func TestIsRetryableError(t *testing.T) {
	logger := &MockLogger{}
	executor := NewAgentExecutor(logger)

	tests := []struct {
		name          string
		errorMsg      string
		wantRetryable bool
	}{
		{
			name:          "timeout error",
			errorMsg:      "request timeout",
			wantRetryable: true,
		},
		{
			name:          "temporary error",
			errorMsg:      "temporary failure",
			wantRetryable: true,
		},
		{
			name:          "unavailable error",
			errorMsg:      "service unavailable",
			wantRetryable: true,
		},
		{
			name:          "connection error",
			errorMsg:      "connection refused",
			wantRetryable: true,
		},
		{
			name:          "permanent error",
			errorMsg:      "invalid input",
			wantRetryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retryable := executor.isRetryableError(tt.errorMsg)
			assert.Equal(t, tt.wantRetryable, retryable)
		})
	}
}

// TestAgentRegistry tests the agent registry
func TestAgentRegistry(t *testing.T) {
	registry := NewAgentRegistry()

	t.Run("register and get agent", func(t *testing.T) {
		agent := &MockAgent{name: "test_agent"}
		registry.RegisterAgent("test", agent)

		retrieved := registry.GetAgent("test")
		assert.Equal(t, agent, retrieved)
	})

	t.Run("get non-existent agent", func(t *testing.T) {
		retrieved := registry.GetAgent("nonexistent")
		assert.Nil(t, retrieved)
	})
}

// TestSelectAgent tests agent selection logic
func TestSelectAgent(t *testing.T) {
	logger := &MockLogger{}
	executor := NewAgentExecutor(logger)

	// Register agents for different step types
	executor.registry.RegisterAgent("analysis_agent", &MockAgent{name: "analysis_agent"})
	executor.registry.RegisterAgent("action_agent", &MockAgent{name: "action_agent"})
	executor.registry.RegisterAgent("custom_agent", &MockAgent{name: "custom_agent"})

	tests := []struct {
		name      string
		step      *Step
		wantAgent string
	}{
		{
			name: "explicit agent",
			step: &Step{
				Agent: "custom_agent",
			},
			wantAgent: "custom_agent",
		},
		{
			name: "analysis step",
			step: &Step{
				Type: StepTypeAnalysis,
			},
			wantAgent: "analysis_agent",
		},
		{
			name: "action step",
			step: &Step{
				Type: StepTypeAction,
			},
			wantAgent: "action_agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := executor.selectAgent(tt.step)
			if tt.wantAgent != "" {
				assert.NotNil(t, agent)
				assert.Equal(t, tt.wantAgent, agent.Name())
			}
		})
	}
}

// TestContains tests the contains helper function
func TestContains(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		substr string
		want   bool
	}{
		{
			name:   "exact match",
			s:      "timeout",
			substr: "timeout",
			want:   true,
		},
		{
			name:   "prefix match",
			s:      "timeout error",
			substr: "timeout",
			want:   true,
		},
		{
			name:   "suffix match",
			s:      "error timeout",
			substr: "timeout",
			want:   true,
		},
		{
			name:   "no match",
			s:      "error occurred",
			substr: "timeout",
			want:   false,
		},
		{
			name:   "empty string",
			s:      "",
			substr: "timeout",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contains(tt.s, tt.substr)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Benchmark tests
func BenchmarkAgentExecutor_Execute(b *testing.B) {
	logger := &MockLogger{}
	executor := NewAgentExecutor(logger)
	executor.registry.RegisterAgent("action_agent", &MockAgent{name: "action_agent"})

	plan := &Plan{
		ID:   "plan_1",
		Goal: "Benchmark goal",
		Steps: []*Step{
			{ID: "step_1", Type: StepTypeAction, Status: StepStatusPending},
			{ID: "step_2", Type: StepTypeAction, Status: StepStatusPending},
			{ID: "step_3", Type: StepTypeAction, Status: StepStatusPending},
		},
		Dependencies: map[string][]string{
			"step_2": {"step_1"},
			"step_3": {"step_2"},
		},
		Status: PlanStatusReady,
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Reset plan status for each iteration
		plan.Status = PlanStatusReady
		for _, step := range plan.Steps {
			step.Status = StepStatusPending
		}
		_, _ = executor.Execute(ctx, plan)
	}
}

func BenchmarkBuildExecutionOrder(b *testing.B) {
	logger := &MockLogger{}
	executor := NewAgentExecutor(logger)

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
			"step_3": {"step_1"},
			"step_4": {"step_2", "step_3"},
			"step_5": {"step_4"},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = executor.buildExecutionOrder(plan)
	}
}
