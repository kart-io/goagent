package multiagent

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRealNATSCommunicatorRoundTrip(t *testing.T) {
	endpoint := os.Getenv("GOAGENT_TEST_NATS_URL")
	if endpoint == "" {
		t.Skip("real service endpoint not configured: GOAGENT_TEST_NATS_URL")
	}
	sender, err := NewNATSCommunicator("sender-"+uuid.NewString(), &NATSConfig{URL: endpoint})
	require.NoError(t, err)
	defer sender.Close()
	receiverID := "receiver-" + uuid.NewString()
	receiver, err := NewNATSCommunicator(receiverID, &NATSConfig{URL: endpoint})
	require.NoError(t, err)
	defer receiver.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	topic := "agent." + receiverID + ".inbox"
	inbox, err := receiver.Subscribe(ctx, topic)
	require.NoError(t, err)
	require.NoError(t, receiver.conn.FlushTimeout(time.Second))
	message := NewAgentMessage("", receiverID, MessageType("validation"), map[string]any{"text": "甲乙\n原文"})
	require.NoError(t, sender.Send(ctx, receiverID, message))
	require.NoError(t, sender.conn.FlushTimeout(time.Second))
	select {
	case received := <-inbox:
		require.Equal(t, message.ID, received.ID)
		require.Equal(t, sender.agentID, received.From)
		require.Equal(t, receiverID, received.To)
		require.Equal(t, "甲乙\n原文", received.Payload.(map[string]any)["text"])
	case <-ctx.Done():
		t.Fatal("point-to-point delivery timed out")
	}
	broadcasts, err := receiver.Subscribe(ctx, "agent.broadcast")
	require.NoError(t, err)
	require.NoError(t, receiver.conn.FlushTimeout(time.Second))
	require.NoError(t, sender.Broadcast(ctx, NewAgentMessage("", "", MessageType("validation"), "broadcast")))
	require.NoError(t, sender.conn.FlushTimeout(time.Second))
	select {
	case received := <-broadcasts:
		require.Equal(t, "broadcast", received.Payload)
	case <-ctx.Done():
		t.Fatal("broadcast delivery timed out")
	}
	require.NoError(t, receiver.Unsubscribe(ctx, topic))
	require.NoError(t, receiver.conn.FlushTimeout(time.Second))
	require.NoError(t, sender.Send(ctx, receiverID, NewAgentMessage("", receiverID, MessageType("validation"), "after unsubscribe")))
	require.NoError(t, sender.conn.FlushTimeout(time.Second))
	select {
	case <-inbox:
		t.Fatal("unsubscribed receiver still received a message")
	case <-time.After(50 * time.Millisecond):
	}
}
