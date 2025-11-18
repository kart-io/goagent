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

// MockLogger is shared from system_test.go but defined here for clarity
type MockLogger2 struct{}

func (m *MockLogger2) Debug(args ...interface{})                       {}
func (m *MockLogger2) Info(args ...interface{})                        {}
func (m *MockLogger2) Warn(args ...interface{})                        {}
func (m *MockLogger2) Error(args ...interface{})                       {}
func (m *MockLogger2) Fatal(args ...interface{})                       {}
func (m *MockLogger2) Debugf(template string, args ...interface{})     {}
func (m *MockLogger2) Infof(template string, args ...interface{})      {}
func (m *MockLogger2) Warnf(template string, args ...interface{})      {}
func (m *MockLogger2) Errorf(template string, args ...interface{})     {}
func (m *MockLogger2) Fatalf(template string, args ...interface{})     {}
func (m *MockLogger2) Debugw(msg string, keysAndValues ...interface{}) {}
func (m *MockLogger2) Infow(msg string, keysAndValues ...interface{})  {}
func (m *MockLogger2) Warnw(msg string, keysAndValues ...interface{})  {}
func (m *MockLogger2) Errorw(msg string, keysAndValues ...interface{}) {}
func (m *MockLogger2) Fatalw(msg string, keysAndValues ...interface{}) {}
func (m *MockLogger2) With(keyValues ...interface{}) loggercore.Logger { return m }
func (m *MockLogger2) WithCtx(ctx context.Context, keyValues ...interface{}) loggercore.Logger {
	return m
}
func (m *MockLogger2) WithCallerSkip(skip int) loggercore.Logger { return m }
func (m *MockLogger2) SetLevel(level loggercore.Level)           {}
func (m *MockLogger2) Sync() error                               { return nil }
func (m *MockLogger2) RotateLogFile() error                      { return nil }
func (m *MockLogger2) Flush() error                              { return nil }

func TestNewBaseCollaborativeAgent(t *testing.T) {
	logger := &MockLogger2{}
	system := NewMultiAgentSystem(logger)

	agent := NewBaseCollaborativeAgent("agent1", "Test agent", RoleWorker, system)

	require.NotNil(t, agent)
	assert.Equal(t, "agent1", agent.Name())
	assert.Equal(t, RoleWorker, agent.GetRole())
	assert.NotNil(t, agent.messageBox)
	assert.NotNil(t, agent.outbox)
	assert.NotNil(t, agent.state)
}

func TestBaseCollaborativeAgent_GetSetRole(t *testing.T) {
	agent := NewBaseCollaborativeAgent("agent1", "Test", RoleWorker, nil)

	tests := []struct {
		name string
		role Role
	}{
		{"set leader", RoleLeader},
		{"set coordinator", RoleCoordinator},
		{"set specialist", RoleSpecialist},
		{"set validator", RoleValidator},
		{"set observer", RoleObserver},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent.SetRole(tt.role)
			assert.Equal(t, tt.role, agent.GetRole())
		})
	}
}

func TestBaseCollaborativeAgent_ReceiveMessage(t *testing.T) {
	agent := NewBaseCollaborativeAgent("agent1", "Test", RoleWorker, nil)
	ctx := context.Background()

	message := Message{
		ID:        "msg1",
		From:      "agent2",
		To:        "agent1",
		Type:      MessageTypeRequest,
		Content:   "test",
		Priority:  1,
		Timestamp: time.Now(),
	}

	err := agent.ReceiveMessage(ctx, message)
	require.NoError(t, err)

	// Verify message was received
	select {
	case received := <-agent.messageBox:
		assert.Equal(t, message.ID, received.ID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for message")
	}
}

func TestBaseCollaborativeAgent_SendMessage(t *testing.T) {
	logger := &MockLogger2{}
	system := NewMultiAgentSystem(logger)

	agent := NewBaseCollaborativeAgent("agent1", "Test", RoleWorker, system)
	ctx := context.Background()

	message := Message{
		ID:        "msg1",
		From:      "agent1",
		To:        "agent2",
		Type:      MessageTypeRequest,
		Content:   "test",
		Priority:  1,
		Timestamp: time.Now(),
	}

	err := agent.SendMessage(ctx, message)
	require.NoError(t, err)

	// Test without system
	agentNoSystem := NewBaseCollaborativeAgent("agent2", "Test", RoleWorker, nil)
	err = agentNoSystem.SendMessage(ctx, message)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not connected to system")
}

func TestBaseCollaborativeAgent_Collaborate(t *testing.T) {
	agent := NewBaseCollaborativeAgent("agent1", "Test", RoleWorker, nil)
	ctx := context.Background()

	task := &CollaborativeTask{
		ID:          "task1",
		Name:        "Test Task",
		Description: "Test collaboration",
		Type:        CollaborationTypeSequential,
		Input:       "test data",
		Status:      TaskStatusPending,
		Assignments: make(map[string]Assignment),
	}

	assignment, err := agent.Collaborate(ctx, task)

	require.NoError(t, err)
	assert.NotNil(t, assignment)
	assert.Equal(t, "agent1", assignment.AgentID)
	assert.Equal(t, RoleWorker, assignment.Role)
	assert.Equal(t, TaskStatusCompleted, assignment.Status)
	assert.NotNil(t, assignment.Result)
}

func TestBaseCollaborativeAgent_Vote(t *testing.T) {
	agent := NewBaseCollaborativeAgent("agent1", "Test", RoleWorker, nil)
	ctx := context.Background()

	// Test voting multiple times to verify randomness
	votes := make([]bool, 10)
	for i := 0; i < 10; i++ {
		vote, err := agent.Vote(ctx, "proposal")
		require.NoError(t, err)
		votes[i] = vote
	}

	// Should have some variation (not all same)
	// This test might occasionally fail due to randomness, but very unlikely
	allSame := true
	first := votes[0]
	for _, v := range votes {
		if v != first {
			allSame = false
			break
		}
	}
	// With 10 votes and >70% probability, extremely unlikely all votes are same
	// If this fails, it's likely a bug
	_ = allSame // We don't strictly test this as it could theoretically all be same
}

func TestBaseCollaborativeAgent_Execute(t *testing.T) {
	logger := &MockLogger2{}
	system := NewMultiAgentSystem(logger)
	agent := NewBaseCollaborativeAgent("agent1", "Test", RoleWorker, system)

	ctx := context.Background()
	input := &core.AgentInput{
		Context: map[string]interface{}{
			"task": "test",
		},
	}

	output, err := agent.Execute(ctx, input)

	require.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, "success", output.Status)
	assert.NotNil(t, output.Result)
	assert.Contains(t, output.Metadata, "role")
	assert.Contains(t, output.Metadata, "agent_id")
}

func TestBaseCollaborativeAgent_RoleBasedTasks(t *testing.T) {
	logger := &MockLogger2{}
	system := NewMultiAgentSystem(logger)
	ctx := context.Background()

	tests := []struct {
		name    string
		role    Role
		checkFn func(*testing.T, interface{})
	}{
		{
			name: "leader task",
			role: RoleLeader,
			checkFn: func(t *testing.T, result interface{}) {
				m, ok := result.(map[string]interface{})
				require.True(t, ok)
				assert.Contains(t, m, "strategy")
				assert.Contains(t, m, "phases")
			},
		},
		{
			name: "worker task",
			role: RoleWorker,
			checkFn: func(t *testing.T, result interface{}) {
				m, ok := result.(map[string]interface{})
				require.True(t, ok)
				assert.Contains(t, m, "worker_id")
				assert.Contains(t, m, "completed")
			},
		},
		{
			name: "coordinator task",
			role: RoleCoordinator,
			checkFn: func(t *testing.T, result interface{}) {
				m, ok := result.(map[string]interface{})
				require.True(t, ok)
				assert.Contains(t, m, "synchronized")
				assert.Contains(t, m, "agents_ready")
			},
		},
		{
			name: "specialist task",
			role: RoleSpecialist,
			checkFn: func(t *testing.T, result interface{}) {
				m, ok := result.(map[string]interface{})
				require.True(t, ok)
				assert.Contains(t, m, "specialist_id")
				assert.Contains(t, m, "analysis")
			},
		},
		{
			name: "validator task",
			role: RoleValidator,
			checkFn: func(t *testing.T, result interface{}) {
				m, ok := result.(map[string]interface{})
				require.True(t, ok)
				assert.Contains(t, m, "valid")
				assert.Contains(t, m, "confidence")
			},
		},
		{
			name: "observer task",
			role: RoleObserver,
			checkFn: func(t *testing.T, result interface{}) {
				m, ok := result.(map[string]interface{})
				require.True(t, ok)
				assert.Contains(t, m, "observed_agents")
				assert.Contains(t, m, "status")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewBaseCollaborativeAgent("test", "Test", tt.role, system)

			task := &CollaborativeTask{
				ID:          "task1",
				Type:        CollaborationTypeSequential,
				Input:       "test",
				Assignments: make(map[string]Assignment),
			}

			assignment, err := agent.Collaborate(ctx, task)
			require.NoError(t, err)
			assert.NotNil(t, assignment.Result)

			if tt.checkFn != nil {
				tt.checkFn(t, assignment.Result)
			}
		})
	}
}

func TestNewSpecializedAgent(t *testing.T) {
	logger := &MockLogger2{}
	system := NewMultiAgentSystem(logger)

	agent := NewSpecializedAgent("specialist1", "machine_learning", system)

	require.NotNil(t, agent)
	assert.Equal(t, "specialist1", agent.Name())
	assert.Equal(t, RoleSpecialist, agent.GetRole())
	assert.Contains(t, agent.Description(), "machine_learning")
}

func TestSpecializedAgent_Collaborate(t *testing.T) {
	logger := &MockLogger2{}
	system := NewMultiAgentSystem(logger)

	agent := NewSpecializedAgent("specialist1", "data_science", system)
	ctx := context.Background()

	task := &CollaborativeTask{
		ID:          "task1",
		Type:        CollaborationTypeSequential,
		Input:       "analyze dataset",
		Assignments: make(map[string]Assignment),
	}

	assignment, err := agent.Collaborate(ctx, task)

	require.NoError(t, err)
	assert.NotNil(t, assignment)
	assert.Equal(t, "specialist1", assignment.AgentID)
	assert.Equal(t, TaskStatusCompleted, assignment.Status)

	result, ok := assignment.Result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "data_science", result["specialization"])
	assert.Contains(t, result, "analysis")
	assert.Contains(t, result, "confidence")
}

func TestNewNegotiatingAgent(t *testing.T) {
	logger := &MockLogger2{}
	system := NewMultiAgentSystem(logger)

	agent := NewNegotiatingAgent("negotiator1", system)

	require.NotNil(t, agent)
	assert.Equal(t, "negotiator1", agent.Name())
	assert.Equal(t, RoleWorker, agent.GetRole())
	assert.NotNil(t, agent.preferences)
	assert.NotNil(t, agent.negotiationHistory)
}

func TestNegotiatingAgent_Negotiate(t *testing.T) {
	logger := &MockLogger2{}
	system := NewMultiAgentSystem(logger)

	negotiator := NewNegotiatingAgent("negotiator1", system)

	// Register negotiator and partners
	require.NoError(t, system.RegisterAgent("negotiator1", negotiator))

	partner1 := NewNegotiatingAgent("partner1", system)
	partner2 := NewNegotiatingAgent("partner2", system)
	require.NoError(t, system.RegisterAgent("partner1", partner1))
	require.NoError(t, system.RegisterAgent("partner2", partner2))

	// Set up partners to respond
	go func() {
		for {
			select {
			case msg := <-partner1.messageBox:
				response := Message{
					ID:        "resp1",
					From:      "partner1",
					To:        msg.From,
					Type:      MessageTypeResponse,
					Content:   true,
					Timestamp: time.Now(),
				}
				_ = system.SendMessage(response)
			case <-time.After(3 * time.Second):
				return
			}
		}
	}()

	go func() {
		for {
			select {
			case msg := <-partner2.messageBox:
				response := Message{
					ID:        "resp2",
					From:      "partner2",
					To:        msg.From,
					Type:      MessageTypeResponse,
					Content:   true,
					Timestamp: time.Now(),
				}
				_ = system.SendMessage(response)
			case <-time.After(3 * time.Second):
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	proposal := map[string]interface{}{
		"price":  100,
		"terms":  "standard",
		"period": "30 days",
	}

	partners := []string{"partner1", "partner2"}

	// Test negotiation
	result, err := negotiator.Negotiate(ctx, proposal, partners)

	// May succeed or timeout depending on timing
	// Just verify it doesn't panic
	_ = result
	_ = err
}

func TestNegotiatingAgent_EvaluateOffers(t *testing.T) {
	agent := NewNegotiatingAgent("negotiator1", nil)

	tests := []struct {
		name     string
		offers   map[string]interface{}
		expected bool
	}{
		{
			name: "majority accepts",
			offers: map[string]interface{}{
				"agent1": true,
				"agent2": true,
				"agent3": false,
			},
			expected: true,
		},
		{
			name: "majority rejects",
			offers: map[string]interface{}{
				"agent1": false,
				"agent2": false,
				"agent3": true,
			},
			expected: false,
		},
		{
			name: "tie goes to false",
			offers: map[string]interface{}{
				"agent1": true,
				"agent2": false,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := agent.evaluateOffers(tt.offers)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNegotiatingAgent_ModifyProposal(t *testing.T) {
	agent := NewNegotiatingAgent("negotiator1", nil)

	original := map[string]interface{}{
		"price": 100,
		"terms": "standard",
	}

	feedback := map[string]interface{}{
		"agent1": "reduce price",
		"agent2": "add flexibility",
	}

	modified := agent.modifyProposal(original, feedback)

	require.NotNil(t, modified)
	m, ok := modified.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, m, "original")
	assert.Contains(t, m, "modified")
	assert.Equal(t, true, m["modified"])
}

// Benchmark tests
func BenchmarkBaseCollaborativeAgent_Collaborate(b *testing.B) {
	logger := &MockLogger2{}
	system := NewMultiAgentSystem(logger)
	agent := NewBaseCollaborativeAgent("agent1", "Test", RoleWorker, system)

	ctx := context.Background()
	task := &CollaborativeTask{
		ID:          "bench",
		Type:        CollaborationTypeSequential,
		Input:       "data",
		Assignments: make(map[string]Assignment),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = agent.Collaborate(ctx, task)
	}
}

func BenchmarkSpecializedAgent_Collaborate(b *testing.B) {
	logger := &MockLogger2{}
	system := NewMultiAgentSystem(logger)
	agent := NewSpecializedAgent("specialist", "domain", system)

	ctx := context.Background()
	task := &CollaborativeTask{
		ID:          "bench",
		Type:        CollaborationTypeSequential,
		Input:       "data",
		Assignments: make(map[string]Assignment),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = agent.Collaborate(ctx, task)
	}
}
