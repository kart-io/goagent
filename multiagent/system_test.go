package multiagent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	loggercore "github.com/kart-io/logger/core"
)

// MockLogger implements a basic logger for testing
type MockLogger struct{}

func (m *MockLogger) Debug(args ...interface{})                       {}
func (m *MockLogger) Info(args ...interface{})                        {}
func (m *MockLogger) Warn(args ...interface{})                        {}
func (m *MockLogger) Error(args ...interface{})                       {}
func (m *MockLogger) Fatal(args ...interface{})                       {}
func (m *MockLogger) Debugf(template string, args ...interface{})     {}
func (m *MockLogger) Infof(template string, args ...interface{})      {}
func (m *MockLogger) Warnf(template string, args ...interface{})      {}
func (m *MockLogger) Errorf(template string, args ...interface{})     {}
func (m *MockLogger) Fatalf(template string, args ...interface{})     {}
func (m *MockLogger) Debugw(msg string, keysAndValues ...interface{}) {}
func (m *MockLogger) Infow(msg string, keysAndValues ...interface{})  {}
func (m *MockLogger) Warnw(msg string, keysAndValues ...interface{})  {}
func (m *MockLogger) Errorw(msg string, keysAndValues ...interface{}) {}
func (m *MockLogger) Fatalw(msg string, keysAndValues ...interface{}) {}
func (m *MockLogger) With(keyValues ...interface{}) loggercore.Logger { return m }
func (m *MockLogger) WithCtx(ctx context.Context, keyValues ...interface{}) loggercore.Logger {
	return m
}
func (m *MockLogger) WithCallerSkip(skip int) loggercore.Logger { return m }
func (m *MockLogger) SetLevel(level loggercore.Level)           {}
func (m *MockLogger) Sync() error                               { return nil }
func (m *MockLogger) RotateLogFile() error                      { return nil }
func (m *MockLogger) Flush() error                              { return nil }

// Ensure MockLogger implements loggercore.Logger
var _ loggercore.Logger = (*MockLogger)(nil)

// MockCollaborativeAgent for testing
type MockCollaborativeAgent struct {
	id           string
	role         Role
	voteResponse bool
	voteError    error
	collabResult *Assignment
	collabError  error
}

func (m *MockCollaborativeAgent) Name() string        { return m.id }
func (m *MockCollaborativeAgent) Description() string { return "Mock agent" }
func (m *MockCollaborativeAgent) Capabilities() []string {
	return []string{}
}

func (m *MockCollaborativeAgent) GetRole() Role {
	return m.role
}

func (m *MockCollaborativeAgent) SetRole(role Role) {
	m.role = role
}

func (m *MockCollaborativeAgent) ReceiveMessage(ctx context.Context, message Message) error {
	return nil
}

func (m *MockCollaborativeAgent) SendMessage(ctx context.Context, message Message) error {
	return nil
}

func (m *MockCollaborativeAgent) Collaborate(ctx context.Context, task *CollaborativeTask) (*Assignment, error) {
	if m.collabError != nil {
		return nil, m.collabError
	}

	if m.collabResult != nil {
		return m.collabResult, nil
	}

	return &Assignment{
		AgentID:   m.id,
		Role:      m.role,
		Subtask:   task.Input,
		Status:    TaskStatusCompleted,
		Result:    map[string]interface{}{"result": "completed"},
		StartTime: time.Now(),
		EndTime:   time.Now(),
	}, nil
}

func (m *MockCollaborativeAgent) Vote(ctx context.Context, proposal interface{}) (bool, error) {
	return m.voteResponse, m.voteError
}

func (m *MockCollaborativeAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	return &core.AgentOutput{Result: "success"}, nil
}

func (m *MockCollaborativeAgent) Stream(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[*core.AgentOutput], error) {
	ch := make(chan core.StreamChunk[*core.AgentOutput])
	close(ch)
	return ch, nil
}

func (m *MockCollaborativeAgent) Batch(ctx context.Context, inputs []*core.AgentInput) ([]*core.AgentOutput, error) {
	return nil, nil
}

func (m *MockCollaborativeAgent) Pipe(other core.Runnable[*core.AgentOutput, any]) core.Runnable[*core.AgentInput, any] {
	// Return a mock runnable that satisfies the type
	return &mockRunnable{}
}

func (m *MockCollaborativeAgent) WithCallbacks(callbacks ...core.Callback) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return m
}

func (m *MockCollaborativeAgent) WithConfig(config core.RunnableConfig) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return m
}

// mockRunnable is a simple mock that satisfies Runnable[*AgentInput, any]
type mockRunnable struct{}

func (mr *mockRunnable) Invoke(ctx context.Context, input *core.AgentInput) (any, error) {
	return nil, nil
}

func (mr *mockRunnable) Stream(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[any], error) {
	ch := make(chan core.StreamChunk[any])
	close(ch)
	return ch, nil
}

func (mr *mockRunnable) Batch(ctx context.Context, inputs []*core.AgentInput) ([]any, error) {
	return nil, nil
}

func (mr *mockRunnable) Pipe(next core.Runnable[any, any]) core.Runnable[*core.AgentInput, any] {
	return mr
}

func (mr *mockRunnable) WithCallbacks(callbacks ...core.Callback) core.Runnable[*core.AgentInput, any] {
	return mr
}

func (mr *mockRunnable) WithConfig(config core.RunnableConfig) core.Runnable[*core.AgentInput, any] {
	return mr
}

func TestNewMultiAgentSystem(t *testing.T) {
	tests := []struct {
		name string
		opts []SystemOption
		want func(*MultiAgentSystem) bool
	}{
		{
			name: "default system",
			opts: nil,
			want: func(s *MultiAgentSystem) bool {
				return s.maxAgents == 100 &&
					s.messageBufferSize == 1000 &&
					s.timeout == 30*time.Second
			},
		},
		{
			name: "with max agents",
			opts: []SystemOption{WithMaxAgents(50)},
			want: func(s *MultiAgentSystem) bool {
				return s.maxAgents == 50
			},
		},
		{
			name: "with timeout",
			opts: []SystemOption{WithTimeout(1 * time.Minute)},
			want: func(s *MultiAgentSystem) bool {
				return s.timeout == 1*time.Minute
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &MockLogger{}
			system := NewMultiAgentSystem(logger, tt.opts...)

			assert.NotNil(t, system)
			assert.NotNil(t, system.agents)
			assert.NotNil(t, system.teams)
			assert.NotNil(t, system.tasks)
			assert.True(t, tt.want(system))
		})
	}
}

func TestMultiAgentSystem_RegisterAgent(t *testing.T) {
	logger := &MockLogger{}
	system := NewMultiAgentSystem(logger, WithMaxAgents(3))

	tests := []struct {
		name    string
		id      string
		agent   CollaborativeAgent
		wantErr bool
		errMsg  string
	}{
		{
			name:    "register valid agent",
			id:      "agent1",
			agent:   &MockCollaborativeAgent{id: "agent1", role: RoleWorker},
			wantErr: false,
		},
		{
			name:    "register another agent",
			id:      "agent2",
			agent:   &MockCollaborativeAgent{id: "agent2", role: RoleLeader},
			wantErr: false,
		},
		{
			name:    "duplicate agent",
			id:      "agent1",
			agent:   &MockCollaborativeAgent{id: "agent1", role: RoleWorker},
			wantErr: true,
			errMsg:  "already registered",
		},
		{
			name:    "exceed max agents",
			id:      "agent3",
			agent:   &MockCollaborativeAgent{id: "agent3", role: RoleWorker},
			wantErr: false, // This should succeed now since maxAgents=3
		},
		{
			name:    "exceed max agents for real",
			id:      "agent4",
			agent:   &MockCollaborativeAgent{id: "agent4", role: RoleWorker},
			wantErr: true,
			errMsg:  "maximum number of agents",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := system.RegisterAgent(tt.id, tt.agent)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMultiAgentSystem_UnregisterAgent(t *testing.T) {
	logger := &MockLogger{}
	system := NewMultiAgentSystem(logger)

	agent := &MockCollaborativeAgent{id: "agent1", role: RoleWorker}
	err := system.RegisterAgent("agent1", agent)
	require.NoError(t, err)

	tests := []struct {
		name    string
		id      string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "unregister existing agent",
			id:      "agent1",
			wantErr: false,
		},
		{
			name:    "unregister non-existent agent",
			id:      "agent999",
			wantErr: true,
			errMsg:  "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := system.UnregisterAgent(tt.id)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMultiAgentSystem_CreateTeam(t *testing.T) {
	logger := &MockLogger{}
	system := NewMultiAgentSystem(logger)

	// Register agents first
	agent1 := &MockCollaborativeAgent{id: "agent1", role: RoleLeader}
	agent2 := &MockCollaborativeAgent{id: "agent2", role: RoleWorker}
	agent3 := &MockCollaborativeAgent{id: "agent3", role: RoleWorker}

	require.NoError(t, system.RegisterAgent("agent1", agent1))
	require.NoError(t, system.RegisterAgent("agent2", agent2))
	require.NoError(t, system.RegisterAgent("agent3", agent3))

	tests := []struct {
		name    string
		team    *Team
		wantErr bool
		errMsg  string
	}{
		{
			name: "create valid team",
			team: &Team{
				ID:      "team1",
				Name:    "Team Alpha",
				Leader:  "agent1",
				Members: []string{"agent1", "agent2", "agent3"},
				Purpose: "Testing",
			},
			wantErr: false,
		},
		{
			name: "duplicate team",
			team: &Team{
				ID:      "team1",
				Name:    "Team Beta",
				Leader:  "agent1",
				Members: []string{"agent1"},
			},
			wantErr: true,
			errMsg:  "already exists",
		},
		{
			name: "missing member",
			team: &Team{
				ID:      "team2",
				Name:    "Team Gamma",
				Members: []string{"agent999"},
			},
			wantErr: true,
			errMsg:  "not found",
		},
		{
			name: "missing leader",
			team: &Team{
				ID:      "team3",
				Name:    "Team Delta",
				Leader:  "agent999",
				Members: []string{"agent1"},
			},
			wantErr: true,
			errMsg:  "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := system.CreateTeam(tt.team)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMultiAgentSystem_ExecuteTask_Parallel(t *testing.T) {
	logger := &MockLogger{}
	system := NewMultiAgentSystem(logger)

	agent1 := &MockCollaborativeAgent{id: "agent1", role: RoleWorker}
	agent2 := &MockCollaborativeAgent{id: "agent2", role: RoleWorker}

	require.NoError(t, system.RegisterAgent("agent1", agent1))
	require.NoError(t, system.RegisterAgent("agent2", agent2))

	task := &CollaborativeTask{
		ID:          "task1",
		Name:        "Parallel Task",
		Description: "Test parallel execution",
		Type:        CollaborationTypeParallel,
		Input:       "test data",
		Assignments: make(map[string]Assignment),
	}

	ctx := context.Background()
	result, err := system.ExecuteTask(ctx, task)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, TaskStatusCompleted, result.Status)
	assert.NotEmpty(t, result.Results)
}

func TestMultiAgentSystem_ExecuteTask_Sequential(t *testing.T) {
	logger := &MockLogger{}
	system := NewMultiAgentSystem(logger)

	agent1 := &MockCollaborativeAgent{id: "agent1", role: RoleWorker}
	agent2 := &MockCollaborativeAgent{id: "agent2", role: RoleWorker}

	require.NoError(t, system.RegisterAgent("agent1", agent1))
	require.NoError(t, system.RegisterAgent("agent2", agent2))

	task := &CollaborativeTask{
		ID:          "task2",
		Name:        "Sequential Task",
		Description: "Test sequential execution",
		Type:        CollaborationTypeSequential,
		Input:       "test data",
		Assignments: make(map[string]Assignment),
	}

	ctx := context.Background()
	result, err := system.ExecuteTask(ctx, task)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, TaskStatusCompleted, result.Status)
	assert.NotNil(t, result.Output)
}

func TestMultiAgentSystem_ExecuteTask_Hierarchical(t *testing.T) {
	logger := &MockLogger{}
	system := NewMultiAgentSystem(logger)

	// Leader creates a plan with worker assignments
	leader := &MockCollaborativeAgent{
		id:   "leader",
		role: RoleLeader,
		collabResult: &Assignment{
			AgentID: "leader",
			Role:    RoleLeader,
			Result: map[string]interface{}{
				"worker1": "subtask1",
				"worker2": "subtask2",
			},
			Status:    TaskStatusCompleted,
			StartTime: time.Now(),
			EndTime:   time.Now(),
		},
	}

	worker1 := &MockCollaborativeAgent{id: "worker1", role: RoleWorker}
	worker2 := &MockCollaborativeAgent{id: "worker2", role: RoleWorker}

	require.NoError(t, system.RegisterAgent("leader", leader))
	require.NoError(t, system.RegisterAgent("worker1", worker1))
	require.NoError(t, system.RegisterAgent("worker2", worker2))

	task := &CollaborativeTask{
		ID:          "task3",
		Name:        "Hierarchical Task",
		Description: "Test hierarchical execution",
		Type:        CollaborationTypeHierarchical,
		Input:       "test data",
		Assignments: make(map[string]Assignment),
	}

	ctx := context.Background()
	result, err := system.ExecuteTask(ctx, task)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.Output)
}

func TestMultiAgentSystem_ExecuteTask_Consensus(t *testing.T) {
	logger := &MockLogger{}
	system := NewMultiAgentSystem(logger)

	agent1 := &MockCollaborativeAgent{id: "agent1", role: RoleWorker, voteResponse: true}
	agent2 := &MockCollaborativeAgent{id: "agent2", role: RoleWorker, voteResponse: true}
	agent3 := &MockCollaborativeAgent{id: "agent3", role: RoleWorker, voteResponse: false}

	require.NoError(t, system.RegisterAgent("agent1", agent1))
	require.NoError(t, system.RegisterAgent("agent2", agent2))
	require.NoError(t, system.RegisterAgent("agent3", agent3))

	tests := []struct {
		name    string
		task    *CollaborativeTask
		wantErr bool
	}{
		{
			name: "consensus reached",
			task: &CollaborativeTask{
				ID:          "consensus1",
				Name:        "Consensus Task",
				Type:        CollaborationTypeConsensus,
				Input:       "proposal",
				Assignments: make(map[string]Assignment),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := system.ExecuteTask(ctx, tt.task)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
				assert.NotNil(t, result.Output)

				output, ok := result.Output.(map[string]interface{})
				assert.True(t, ok)
				assert.Contains(t, output, "consensus_reached")
			}
		})
	}
}

func TestMultiAgentSystem_ExecuteTask_Pipeline(t *testing.T) {
	logger := &MockLogger{}
	system := NewMultiAgentSystem(logger)

	agent1 := &MockCollaborativeAgent{id: "agent1", role: RoleWorker}
	agent2 := &MockCollaborativeAgent{id: "agent2", role: RoleWorker}
	agent3 := &MockCollaborativeAgent{id: "agent3", role: RoleWorker}

	require.NoError(t, system.RegisterAgent("agent1", agent1))
	require.NoError(t, system.RegisterAgent("agent2", agent2))
	require.NoError(t, system.RegisterAgent("agent3", agent3))

	pipeline := []interface{}{"stage1", "stage2", "stage3"}

	task := &CollaborativeTask{
		ID:          "pipeline1",
		Name:        "Pipeline Task",
		Type:        CollaborationTypePipeline,
		Input:       pipeline,
		Assignments: make(map[string]Assignment),
	}

	ctx := context.Background()
	result, err := system.ExecuteTask(ctx, task)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, TaskStatusCompleted, result.Status)
	assert.NotNil(t, result.Output)
}

func TestMultiAgentSystem_ExecuteTask_Errors(t *testing.T) {
	logger := &MockLogger{}
	system := NewMultiAgentSystem(logger)

	tests := []struct {
		name    string
		task    *CollaborativeTask
		wantErr bool
		errMsg  string
	}{
		{
			name: "unknown collaboration type",
			task: &CollaborativeTask{
				ID:          "unknown",
				Name:        "Unknown Task",
				Type:        CollaborationType("unknown"),
				Assignments: make(map[string]Assignment),
			},
			wantErr: true,
			errMsg:  "unknown collaboration type",
		},
		{
			name: "no agents for parallel",
			task: &CollaborativeTask{
				ID:          "no_agents",
				Name:        "No Agents",
				Type:        CollaborationTypeParallel,
				Assignments: make(map[string]Assignment),
			},
			wantErr: true,
			errMsg:  "no available agents",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := system.ExecuteTask(ctx, tt.task)

			if tt.wantErr {
				require.Error(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, TaskStatusFailed, result.Status)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMultiAgentSystem_SendMessage(t *testing.T) {
	logger := &MockLogger{}
	system := NewMultiAgentSystem(logger)

	message := Message{
		ID:        "msg1",
		From:      "agent1",
		To:        "agent2",
		Type:      MessageTypeRequest,
		Content:   "test",
		Priority:  1,
		Timestamp: time.Now(),
	}

	err := system.SendMessage(message)
	assert.NoError(t, err)
}

// Benchmark tests
func BenchmarkMultiAgentSystem_RegisterAgent(b *testing.B) {
	logger := &MockLogger{}
	system := NewMultiAgentSystem(logger, WithMaxAgents(10000))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		agent := &MockCollaborativeAgent{
			id:   string(rune(i)),
			role: RoleWorker,
		}
		_ = system.RegisterAgent(string(rune(i)), agent)
	}
}

func BenchmarkMultiAgentSystem_ExecuteParallelTask(b *testing.B) {
	logger := &MockLogger{}
	system := NewMultiAgentSystem(logger)

	for i := 0; i < 10; i++ {
		agent := &MockCollaborativeAgent{
			id:   string(rune(i)),
			role: RoleWorker,
		}
		_ = system.RegisterAgent(string(rune(i)), agent)
	}

	task := &CollaborativeTask{
		ID:          "bench",
		Name:        "Benchmark Task",
		Type:        CollaborationTypeParallel,
		Input:       "data",
		Assignments: make(map[string]Assignment),
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = system.ExecuteTask(ctx, task)
	}
}
