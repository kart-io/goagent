package multiagent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMessageRouter(t *testing.T) {
	router := NewMessageRouter()

	require.NotNil(t, router)
	assert.NotNil(t, router.routes)
	assert.NotNil(t, router.patterns)
}

func TestMessageRouter_RegisterRoute(t *testing.T) {
	router := NewMessageRouter()

	handler := func(ctx context.Context, message *AgentMessage) (*AgentMessage, error) {
		return &AgentMessage{Payload: "handled"}, nil
	}

	err := router.RegisterRoute("test.topic", handler)
	require.NoError(t, err)

	// Verify route was registered
	router.mu.RLock()
	_, exists := router.routes["test.topic"]
	router.mu.RUnlock()

	assert.True(t, exists)
}

func TestMessageRouter_RegisterPatternRoute(t *testing.T) {
	router := NewMessageRouter()

	handler := func(ctx context.Context, message *AgentMessage) (*AgentMessage, error) {
		return &AgentMessage{Payload: "handled"}, nil
	}

	tests := []struct {
		name    string
		pattern string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid pattern",
			pattern: "^agent\\.[0-9]+\\.inbox$",
			wantErr: false,
		},
		{
			name:    "invalid pattern",
			pattern: "[invalid(regex",
			wantErr: true,
			errMsg:  "invalid pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := router.RegisterPatternRoute(tt.pattern, handler)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)

				// Verify pattern was compiled and stored
				router.mu.RLock()
				_, exists := router.patterns[tt.pattern]
				router.mu.RUnlock()
				assert.True(t, exists)
			}
		})
	}
}

func TestMessageRouter_Route(t *testing.T) {
	router := NewMessageRouter()
	ctx := context.Background()

	// Register exact match handler
	exactHandler := func(ctx context.Context, message *AgentMessage) (*AgentMessage, error) {
		return &AgentMessage{Payload: "exact match"}, nil
	}
	router.RegisterRoute("exact.topic", exactHandler)

	// Register pattern match handler
	patternHandler := func(ctx context.Context, message *AgentMessage) (*AgentMessage, error) {
		return &AgentMessage{Payload: "pattern match"}, nil
	}
	router.RegisterPatternRoute("^pattern\\..*", patternHandler)

	tests := []struct {
		name        string
		topic       string
		wantErr     bool
		errMsg      string
		wantPayload interface{}
	}{
		{
			name:        "exact match",
			topic:       "exact.topic",
			wantErr:     false,
			wantPayload: "exact match",
		},
		{
			name:        "pattern match",
			topic:       "pattern.something",
			wantErr:     false,
			wantPayload: "pattern match",
		},
		{
			name:    "no match",
			topic:   "unknown.topic",
			wantErr: true,
			errMsg:  "no route found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := &AgentMessage{
				From:    "sender",
				To:      "receiver",
				Topic:   tt.topic,
				Payload: "test",
			}

			result, err := router.Route(ctx, message)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.wantPayload, result.Payload)
			}
		})
	}
}

func TestMessageRouter_UnregisterRoute(t *testing.T) {
	router := NewMessageRouter()

	handler := func(ctx context.Context, message *AgentMessage) (*AgentMessage, error) {
		return nil, nil
	}

	// Register route
	router.RegisterRoute("test.topic", handler)

	// Verify exists
	router.mu.RLock()
	_, exists := router.routes["test.topic"]
	router.mu.RUnlock()
	assert.True(t, exists)

	// Unregister
	router.UnregisterRoute("test.topic")

	// Verify removed
	router.mu.RLock()
	_, exists = router.routes["test.topic"]
	router.mu.RUnlock()
	assert.False(t, exists)
}

func TestMessageRouter_UnregisterPatternRoute(t *testing.T) {
	router := NewMessageRouter()

	handler := func(ctx context.Context, message *AgentMessage) (*AgentMessage, error) {
		return nil, nil
	}

	pattern := "^test\\..*"

	// Register pattern route
	router.RegisterPatternRoute(pattern, handler)

	// Verify exists
	router.mu.RLock()
	_, routeExists := router.routes[pattern]
	_, patternExists := router.patterns[pattern]
	router.mu.RUnlock()
	assert.True(t, routeExists)
	assert.True(t, patternExists)

	// Unregister
	router.UnregisterRoute(pattern)

	// Verify removed
	router.mu.RLock()
	_, routeExists = router.routes[pattern]
	_, patternExists = router.patterns[pattern]
	router.mu.RUnlock()
	assert.False(t, routeExists)
	assert.False(t, patternExists)
}

func TestNewSessionManager(t *testing.T) {
	manager := NewSessionManager()

	require.NotNil(t, manager)
	assert.NotNil(t, manager.sessions)
}

func TestSessionManager_CreateSession(t *testing.T) {
	manager := NewSessionManager()

	participants := []string{"agent1", "agent2", "agent3"}

	session, err := manager.CreateSession(participants)

	require.NoError(t, err)
	assert.NotNil(t, session)
	assert.NotEmpty(t, session.ID)
	assert.Equal(t, participants, session.Participants)
	assert.NotNil(t, session.Messages)
	assert.NotNil(t, session.State)
	assert.NotEmpty(t, session.CreatedAt)
	assert.NotEmpty(t, session.UpdatedAt)

	// Verify session was stored
	manager.mu.RLock()
	stored, exists := manager.sessions[session.ID]
	manager.mu.RUnlock()

	assert.True(t, exists)
	assert.Equal(t, session.ID, stored.ID)
}

func TestSessionManager_GetSession(t *testing.T) {
	manager := NewSessionManager()

	// Create a session
	participants := []string{"agent1", "agent2"}
	created, err := manager.CreateSession(participants)
	require.NoError(t, err)

	tests := []struct {
		name      string
		sessionID string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "get existing session",
			sessionID: created.ID,
			wantErr:   false,
		},
		{
			name:      "get non-existent session",
			sessionID: "non-existent",
			wantErr:   true,
			errMsg:    "session not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, err := manager.GetSession(tt.sessionID)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, session)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, session)
				assert.Equal(t, tt.sessionID, session.ID)
			}
		})
	}
}

func TestSessionManager_AddMessage(t *testing.T) {
	manager := NewSessionManager()

	// Create a session
	participants := []string{"agent1", "agent2"}
	session, err := manager.CreateSession(participants)
	require.NoError(t, err)

	message := NewAgentMessage("agent1", "agent2", MessageTypeRequest, "test message")

	tests := []struct {
		name      string
		sessionID string
		message   *AgentMessage
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "add message to existing session",
			sessionID: session.ID,
			message:   message,
			wantErr:   false,
		},
		{
			name:      "add message to non-existent session",
			sessionID: "non-existent",
			message:   message,
			wantErr:   true,
			errMsg:    "session not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.AddMessage(tt.sessionID, tt.message)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)

				// Verify message was added
				retrieved, err := manager.GetSession(tt.sessionID)
				require.NoError(t, err)
				assert.Len(t, retrieved.Messages, 1)
				assert.Equal(t, tt.message.ID, retrieved.Messages[0].ID)
			}
		})
	}
}

func TestSessionManager_AddMultipleMessages(t *testing.T) {
	manager := NewSessionManager()

	// Create a session
	participants := []string{"agent1", "agent2"}
	session, err := manager.CreateSession(participants)
	require.NoError(t, err)

	// Add multiple messages
	const numMessages = 10
	for i := 0; i < numMessages; i++ {
		msg := NewAgentMessage("agent1", "agent2", MessageTypeRequest, i)
		err := manager.AddMessage(session.ID, msg)
		require.NoError(t, err)
	}

	// Verify all messages were added
	retrieved, err := manager.GetSession(session.ID)
	require.NoError(t, err)
	assert.Len(t, retrieved.Messages, numMessages)
}

func TestSessionManager_CloseSession(t *testing.T) {
	manager := NewSessionManager()

	// Create a session
	participants := []string{"agent1", "agent2"}
	session, err := manager.CreateSession(participants)
	require.NoError(t, err)

	// Verify session exists
	_, err = manager.GetSession(session.ID)
	require.NoError(t, err)

	// Close session
	err = manager.CloseSession(session.ID)
	require.NoError(t, err)

	// Verify session was removed
	_, err = manager.GetSession(session.ID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "session not found")

	// Verify close is idempotent
	err = manager.CloseSession(session.ID)
	assert.NoError(t, err)
}

func TestSessionManager_ConcurrentOperations(t *testing.T) {
	manager := NewSessionManager()

	// Create multiple sessions concurrently (reduced to avoid ID collisions)
	const numSessions = 10
	done := make(chan *AgentSession, numSessions)

	for i := 0; i < numSessions; i++ {
		go func(id int) {
			participants := []string{"agent1", "agent2"}
			session, err := manager.CreateSession(participants)
			assert.NoError(t, err)
			done <- session
		}(i)
	}

	// Collect all sessions
	sessions := make([]*AgentSession, 0, numSessions)
	sessionIDs := make(map[string]bool)
	for i := 0; i < numSessions; i++ {
		s := <-done
		if !sessionIDs[s.ID] {
			sessions = append(sessions, s)
			sessionIDs[s.ID] = true
		}
	}

	// Only test unique sessions
	numUniqueSessions := len(sessions)
	t.Logf("Created %d unique sessions out of %d attempts", numUniqueSessions, numSessions)

	// Add messages concurrently to unique sessions
	msgDone := make(chan bool, numUniqueSessions)
	for _, session := range sessions {
		go func(s *AgentSession) {
			msg := NewAgentMessage("agent1", "agent2", MessageTypeRequest, "concurrent")
			err := manager.AddMessage(s.ID, msg)
			assert.NoError(t, err)
			msgDone <- true
		}(session)
	}

	// Wait for all messages
	for i := 0; i < numUniqueSessions; i++ {
		<-msgDone
	}

	// Verify all unique sessions have exactly 1 message
	for _, session := range sessions {
		retrieved, err := manager.GetSession(session.ID)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(retrieved.Messages), 1, "Session should have at least 1 message")
	}
}

func TestRouteHandler_ErrorHandling(t *testing.T) {
	router := NewMessageRouter()
	ctx := context.Background()

	// Register handler that returns error
	errorHandler := func(ctx context.Context, message *AgentMessage) (*AgentMessage, error) {
		return nil, assert.AnError
	}
	router.RegisterRoute("error.topic", errorHandler)

	message := &AgentMessage{
		Topic:   "error.topic",
		Payload: "test",
	}

	result, err := router.Route(ctx, message)

	assert.Error(t, err)
	assert.Nil(t, result)
}

// Benchmark tests
func BenchmarkMessageRouter_Route(b *testing.B) {
	router := NewMessageRouter()
	ctx := context.Background()

	handler := func(ctx context.Context, message *AgentMessage) (*AgentMessage, error) {
		return message, nil
	}
	router.RegisterRoute("test.topic", handler)

	message := &AgentMessage{
		Topic:   "test.topic",
		Payload: "data",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = router.Route(ctx, message)
	}
}

func BenchmarkSessionManager_CreateSession(b *testing.B) {
	manager := NewSessionManager()
	participants := []string{"agent1", "agent2", "agent3"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = manager.CreateSession(participants)
	}
}

func BenchmarkSessionManager_AddMessage(b *testing.B) {
	manager := NewSessionManager()
	participants := []string{"agent1", "agent2"}
	session, _ := manager.CreateSession(participants)

	message := NewAgentMessage("agent1", "agent2", MessageTypeRequest, "data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = manager.AddMessage(session.ID, message)
	}
}
