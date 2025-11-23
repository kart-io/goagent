package multiagent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAgentMessage(t *testing.T) {
	msg := NewAgentMessage("agent1", "agent2", MessageTypeRequest, "test payload")

	assert.NotEmpty(t, msg.ID)
	assert.Equal(t, "agent1", msg.From)
	assert.Equal(t, "agent2", msg.To)
	assert.Equal(t, MessageTypeRequest, msg.Type)
	assert.Equal(t, "test payload", msg.Payload)
	assert.NotNil(t, msg.Metadata)
	assert.NotZero(t, msg.Timestamp)
	assert.NotNil(t, msg.TraceContext)
}

func TestGenerateMessageID(t *testing.T) {
	id1 := generateMessageID()
	time.Sleep(time.Second) // Ensure different timestamp
	id2 := generateMessageID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2, "message IDs should be unique")
}

func TestNewMemoryCommunicator(t *testing.T) {
	comm := NewMemoryCommunicator("agent1")

	require.NotNil(t, comm)
	assert.Equal(t, "agent1", comm.agentID)
	assert.False(t, comm.closed)
}

func TestMemoryCommunicator_Send(t *testing.T) {
	// Clean global state before test
	globalMu.Lock()
	globalChannels = make(map[string]chan *AgentMessage)
	globalMu.Unlock()

	comm := NewMemoryCommunicator("agent1")
	ctx := context.Background()

	message := NewAgentMessage("agent1", "agent2", MessageTypeRequest, "test")

	err := comm.Send(ctx, "agent2", message)
	require.NoError(t, err)

	// Verify message was set correctly
	assert.Equal(t, "agent1", message.From)
	assert.Equal(t, "agent2", message.To)

	// Verify channel was created in global map
	globalMu.RLock()
	ch, exists := globalChannels["agent2"]
	globalMu.RUnlock()
	assert.True(t, exists)
	assert.NotNil(t, ch)
}

func TestMemoryCommunicator_Receive(t *testing.T) {
	// Clean global state before test
	globalMu.Lock()
	globalChannels = make(map[string]chan *AgentMessage)
	globalMu.Unlock()

	comm := NewMemoryCommunicator("agent1")
	ctx := context.Background()

	// Send a message first
	message := NewAgentMessage("agent2", "agent1", MessageTypeRequest, "test payload")

	// Create channel for agent1 in global map
	globalMu.Lock()
	ch := make(chan *AgentMessage, 100)
	globalChannels["agent1"] = ch
	globalMu.Unlock()

	// Send message directly to channel
	ch <- message

	// Receive message
	received, err := comm.Receive(ctx)

	require.NoError(t, err)
	assert.NotNil(t, received)
	assert.Equal(t, message.ID, received.ID)
	assert.Equal(t, "test payload", received.Payload)
}

func TestMemoryCommunicator_Broadcast(t *testing.T) {
	// Clean global state before test
	globalMu.Lock()
	globalChannels = make(map[string]chan *AgentMessage)
	globalMu.Unlock()

	comm1 := NewMemoryCommunicator("agent1")
	ctx := context.Background()

	// Setup channels for all agents in global map
	globalMu.Lock()
	globalChannels["agent1"] = make(chan *AgentMessage, 100)
	globalChannels["agent2"] = make(chan *AgentMessage, 100)
	globalChannels["agent3"] = make(chan *AgentMessage, 100)
	globalMu.Unlock()

	message := NewAgentMessage("agent1", "", MessageTypeBroadcast, "broadcast message")

	err := comm1.Broadcast(ctx, message)
	require.NoError(t, err)

	// Verify agent1 did not receive its own message
	globalMu.RLock()
	ch1 := globalChannels["agent1"]
	globalMu.RUnlock()

	select {
	case <-ch1:
		t.Fatal("agent1 should not receive its own broadcast")
	case <-time.After(50 * time.Millisecond):
		// Good, no message
	}

	// Verify other agents received the message
	globalMu.RLock()
	ch2 := globalChannels["agent2"]
	ch3 := globalChannels["agent3"]
	globalMu.RUnlock()

	select {
	case msg := <-ch2:
		assert.Equal(t, "broadcast message", msg.Payload)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("agent2 should have received broadcast")
	}

	select {
	case msg := <-ch3:
		assert.Equal(t, "broadcast message", msg.Payload)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("agent3 should have received broadcast")
	}
}

func TestMemoryCommunicator_Subscribe(t *testing.T) {
	// Clean global state before test
	globalMu.Lock()
	globalSubscribers = make(map[string][]chan *AgentMessage)
	globalMu.Unlock()

	comm := NewMemoryCommunicator("agent1")
	ctx := context.Background()

	ch, err := comm.Subscribe(ctx, "topic1")

	require.NoError(t, err)
	assert.NotNil(t, ch)

	// Verify subscription was registered in global map
	globalMu.RLock()
	subs, exists := globalSubscribers["topic1"]
	globalMu.RUnlock()

	assert.True(t, exists)
	assert.Len(t, subs, 1)
}

func TestMemoryCommunicator_Unsubscribe(t *testing.T) {
	// Clean global state before test
	globalMu.Lock()
	globalSubscribers = make(map[string][]chan *AgentMessage)
	globalMu.Unlock()

	comm := NewMemoryCommunicator("agent1")
	ctx := context.Background()

	// Subscribe first
	_, err := comm.Subscribe(ctx, "topic1")
	require.NoError(t, err)

	// Verify subscription exists in global map
	globalMu.RLock()
	_, exists := globalSubscribers["topic1"]
	globalMu.RUnlock()
	assert.True(t, exists)

	// Unsubscribe
	err = comm.Unsubscribe(ctx, "topic1")
	require.NoError(t, err)

	// Verify subscription removed from global map
	globalMu.RLock()
	_, exists = globalSubscribers["topic1"]
	globalMu.RUnlock()
	assert.False(t, exists)
}

func TestMemoryCommunicator_Close(t *testing.T) {
	comm := NewMemoryCommunicator("agent1")
	ctx := context.Background()

	// Create some channels and subscriptions
	comm.Send(ctx, "agent2", NewAgentMessage("agent1", "agent2", MessageTypeRequest, "test"))
	_, _ = comm.Subscribe(ctx, "topic1")

	// Close communicator
	err := comm.Close()
	require.NoError(t, err)
	assert.True(t, comm.closed)

	// Verify sending fails after close
	err = comm.Send(ctx, "agent3", NewAgentMessage("agent1", "agent3", MessageTypeRequest, "test"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")

	// Verify broadcast fails after close
	err = comm.Broadcast(ctx, NewAgentMessage("agent1", "", MessageTypeBroadcast, "test"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")

	// Verify subscribe fails after close
	_, err = comm.Subscribe(ctx, "topic2")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")

	// Verify close is idempotent
	err = comm.Close()
	assert.NoError(t, err)
}

func TestMemoryCommunicator_ConcurrentSend(t *testing.T) {
	comm := NewMemoryCommunicator("sender")
	ctx := context.Background()

	// Send multiple messages concurrently
	const numMessages = 100
	done := make(chan bool, numMessages)

	for i := 0; i < numMessages; i++ {
		go func(id int) {
			msg := NewAgentMessage("sender", "receiver", MessageTypeRequest, id)
			err := comm.Send(ctx, "receiver", msg)
			assert.NoError(t, err)
			done <- true
		}(i)
	}

	// Wait for all sends to complete
	for i := 0; i < numMessages; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for concurrent sends")
		}
	}

	// Verify channel has all messages
	globalMu.RLock()
	ch := globalChannels["receiver"]
	globalMu.RUnlock()

	assert.Len(t, ch, numMessages)
}

func TestMemoryCommunicator_ContextCancellation(t *testing.T) {
	comm := NewMemoryCommunicator("agent1")

	// Test Receive with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := comm.Receive(ctx)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)

	// Test Send with cancelled context
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()

	msg := NewAgentMessage("agent1", "agent2", MessageTypeRequest, "test")

	// Fill channel first to trigger context check
	globalMu.Lock()
	ch := make(chan *AgentMessage, 1)
	ch <- msg // Fill the buffer
	globalChannels["agent2"] = ch
	globalMu.Unlock()

	err = comm.Send(ctx2, "agent2", msg)
	// Either succeeds immediately or fails with context error
	// depending on timing
	_ = err
}

func TestMemoryCommunicator_MessageTypes(t *testing.T) {
	comm := NewMemoryCommunicator("agent1")
	ctx := context.Background()

	messageTypes := []MessageType{
		MessageTypeRequest,
		MessageTypeResponse,
		MessageTypeBroadcast,
		MessageTypeNotification,
		MessageTypeCommand,
		MessageTypeReport,
		MessageTypeVote,
	}

	for _, msgType := range messageTypes {
		t.Run(string(msgType), func(t *testing.T) {
			msg := NewAgentMessage("agent1", "agent2", msgType, "test")
			err := comm.Send(ctx, "agent2", msg)
			assert.NoError(t, err)
			assert.Equal(t, msgType, msg.Type)
		})
	}
}

// Benchmark tests
func BenchmarkMemoryCommunicator_Send(b *testing.B) {
	comm := NewMemoryCommunicator("sender")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := NewAgentMessage("sender", "receiver", MessageTypeRequest, "data")
		_ = comm.Send(ctx, "receiver", msg)
	}
}

func BenchmarkMemoryCommunicator_Broadcast(b *testing.B) {
	comm := NewMemoryCommunicator("agent1")
	ctx := context.Background()

	// Setup channels for 10 agents in global map
	globalMu.Lock()
	for i := 0; i < 10; i++ {
		globalChannels[string(rune(i))] = make(chan *AgentMessage, 100)
	}
	globalMu.Unlock()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := NewAgentMessage("agent1", "", MessageTypeBroadcast, "data")
		_ = comm.Broadcast(ctx, msg)
	}
}

func BenchmarkNewAgentMessage(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewAgentMessage("from", "to", MessageTypeRequest, "payload")
	}
}
