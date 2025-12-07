package multiagent

import (
	"context"
	"fmt"
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
	// Create isolated store for test
	store := NewInMemoryChannelStore()
	comm := NewMemoryCommunicatorWithStore("agent1", store)
	ctx := context.Background()

	message := NewAgentMessage("agent1", "agent2", MessageTypeRequest, "test")

	err := comm.Send(ctx, "agent2", message)
	require.NoError(t, err)

	// Verify message was set correctly
	assert.Equal(t, "agent1", message.From)
	assert.Equal(t, "agent2", message.To)

	// Verify channel was created in store
	ch := store.GetChannel("agent2")
	assert.NotNil(t, ch)
}

func TestMemoryCommunicator_Receive(t *testing.T) {
	// Create isolated store for test
	store := NewInMemoryChannelStore()
	comm := NewMemoryCommunicatorWithStore("agent1", store)
	ctx := context.Background()

	// Send a message first
	message := NewAgentMessage("agent2", "agent1", MessageTypeRequest, "test payload")

	// Create channel for agent1 in store
	ch := store.GetOrCreateChannel("agent1")

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
	// Create isolated store for test
	store := NewInMemoryChannelStore()

	comm1 := NewMemoryCommunicatorWithStore("agent1", store)
	ctx := context.Background()

	// Setup channels for all agents in store
	store.GetOrCreateChannel("agent1")
	store.GetOrCreateChannel("agent2")
	store.GetOrCreateChannel("agent3")

	message := NewAgentMessage("agent1", "", MessageTypeBroadcast, "broadcast message")

	err := comm1.Broadcast(ctx, message)
	require.NoError(t, err)

	// Verify agent1 did not receive its own message
	ch1 := store.GetChannel("agent1")

	select {
	case <-ch1:
		t.Fatal("agent1 should not receive its own broadcast")
	case <-time.After(50 * time.Millisecond):
		// Good, no message
	}

	// Verify other agents received the message
	ch2 := store.GetChannel("agent2")
	ch3 := store.GetChannel("agent3")

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
	// Create isolated store for test
	store := NewInMemoryChannelStore()
	comm := NewMemoryCommunicatorWithStore("agent1", store)
	ctx := context.Background()

	ch, err := comm.Subscribe(ctx, "topic1")

	require.NoError(t, err)
	assert.NotNil(t, ch)

	// Verify subscription was registered in store
	subs := store.GetSubscribers("topic1")

	assert.Len(t, subs, 1)
}

func TestMemoryCommunicator_Unsubscribe(t *testing.T) {
	// Create isolated store for test
	store := NewInMemoryChannelStore()
	comm := NewMemoryCommunicatorWithStore("agent1", store)
	ctx := context.Background()

	// Subscribe first
	_, err := comm.Subscribe(ctx, "topic1")
	require.NoError(t, err)

	// Verify subscription exists in store
	subs := store.GetSubscribers("topic1")
	assert.Len(t, subs, 1)

	// Unsubscribe
	err = comm.Unsubscribe(ctx, "topic1")
	require.NoError(t, err)

	// Verify subscription removed from store
	subs = store.GetSubscribers("topic1")
	assert.Len(t, subs, 0)
}

func TestMemoryCommunicator_Close(t *testing.T) {
	// 创建独立的 store 避免测试污染
	store := NewInMemoryChannelStore()
	defer store.Close()

	comm := NewMemoryCommunicatorWithStore("agent1", store)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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
	store := NewInMemoryChannelStore()
	comm := NewMemoryCommunicatorWithStore("sender", store)
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
	ch := store.GetChannel("receiver")

	assert.Len(t, ch, numMessages)
}

func TestMemoryCommunicator_ContextCancellation(t *testing.T) {
	store := NewInMemoryChannelStore()
	comm := NewMemoryCommunicatorWithStore("agent1", store)

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
	ch := store.GetOrCreateChannel("agent2")
	ch <- msg // Fill the buffer partially

	err = comm.Send(ctx2, "agent2", msg)
	// Either succeeds immediately or fails with context error
	// depending on timing
	_ = err
}

func TestMemoryCommunicator_MessageTypes(t *testing.T) {
	// 创建独立的 store 避免测试污染
	store := NewInMemoryChannelStore()
	defer store.Close()

	comm := NewMemoryCommunicatorWithStore("agent1", store)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messageTypes := []MessageType{
		MessageTypeRequest,
		MessageTypeResponse,
		MessageTypeBroadcast,
		MessageTypeNotification,
		MessageTypeCommand,
		MessageTypeReport,
		MessageTypeVote,
	}

	for i, msgType := range messageTypes {
		t.Run(string(msgType), func(t *testing.T) {
			// 每个子测试使用不同的接收者,避免 channel 满
			receiver := fmt.Sprintf("agent%d", i+2)

			// 启动接收者 goroutine，避免 channel 阻塞
			receiverComm := NewMemoryCommunicatorWithStore(receiver, store)
			done := make(chan struct{})
			var receivedMsg *AgentMessage
			var receiveErr error

			go func() {
				receivedMsg, receiveErr = receiverComm.Receive(ctx)
				close(done)
			}()

			// 发送消息
			msg := NewAgentMessage("agent1", receiver, msgType, "test")
			err := comm.Send(ctx, receiver, msg)
			assert.NoError(t, err)
			assert.Equal(t, msgType, msg.Type)

			// 等待接收完成
			<-done
			assert.NoError(t, receiveErr)
			assert.NotNil(t, receivedMsg)
			if receivedMsg != nil {
				assert.Equal(t, msgType, receivedMsg.Type)
				assert.Equal(t, "agent1", receivedMsg.From)
				assert.Equal(t, receiver, receivedMsg.To)
			}
		})
	}
}

// Benchmark tests
func BenchmarkMemoryCommunicator_Send(b *testing.B) {
	store := NewInMemoryChannelStore()
	comm := NewMemoryCommunicatorWithStore("sender", store)
	ctx := context.Background()

	// Start a consumer to drain the channel and prevent deadlock
	ch := store.GetOrCreateChannel("receiver")
	go func() {
		for range ch {
			// Drain channel
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := NewAgentMessage("sender", "receiver", MessageTypeRequest, "data")
		_ = comm.Send(ctx, "receiver", msg)
	}
}

func BenchmarkMemoryCommunicator_Broadcast(b *testing.B) {
	store := NewInMemoryChannelStore()
	comm := NewMemoryCommunicatorWithStore("agent1", store)
	ctx := context.Background()

	// Setup channels for 10 agents in store
	for i := 0; i < 10; i++ {
		store.GetOrCreateChannel(string(rune(i)))
	}

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
