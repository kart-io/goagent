package stream

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamMode_Constants(t *testing.T) {
	assert.Equal(t, StreamMode("messages"), StreamModeMessages)
	assert.Equal(t, StreamMode("updates"), StreamModeUpdates)
	assert.Equal(t, StreamMode("custom"), StreamModeCustom)
	assert.Equal(t, StreamMode("values"), StreamModeValues)
	assert.Equal(t, StreamMode("debug"), StreamModeDebug)
}

func TestDefaultStreamConfig(t *testing.T) {
	config := DefaultStreamConfig()

	assert.NotNil(t, config)
	assert.Equal(t, []StreamMode{StreamModeMessages}, config.Modes)
	assert.Equal(t, 100, config.BufferSize)
	assert.False(t, config.IncludeMetadata)
}

func TestNewMultiModeStream(t *testing.T) {
	ctx := context.Background()
	config := &StreamConfig{
		Modes:      []StreamMode{StreamModeMessages, StreamModeUpdates},
		BufferSize: 50,
	}

	stream := NewMultiModeStream(ctx, config)

	require.NotNil(t, stream)
	assert.NotNil(t, stream.channels)
	assert.Len(t, stream.channels, 2)
	assert.NotNil(t, stream.writers)
	assert.Len(t, stream.writers, 2)

	defer stream.Close()
}

func TestMultiModeStream_Stream(t *testing.T) {
	ctx := context.Background()
	config := &StreamConfig{
		Modes:      []StreamMode{StreamModeMessages},
		BufferSize: 10,
	}

	stream := NewMultiModeStream(ctx, config)
	defer stream.Close()

	event := StreamEvent{
		Mode:      StreamModeMessages,
		Type:      "token",
		Data:      "Hello",
		Timestamp: time.Now(),
	}

	err := stream.Stream(StreamModeMessages, event)
	assert.NoError(t, err)

	// Try streaming to non-configured mode
	err = stream.Stream(StreamModeUpdates, event)
	assert.Error(t, err)
}

func TestMultiModeStream_Subscribe(t *testing.T) {
	ctx := context.Background()
	config := &StreamConfig{
		Modes:      []StreamMode{StreamModeMessages},
		BufferSize: 10,
	}

	stream := NewMultiModeStream(ctx, config)
	defer stream.Close()

	// Subscribe to configured mode
	ch, err := stream.Subscribe(StreamModeMessages)
	assert.NoError(t, err)
	assert.NotNil(t, ch)

	// Subscribe to non-configured mode
	_, err = stream.Subscribe(StreamModeUpdates)
	assert.Error(t, err)
}

func TestMultiModeStream_StreamAndReceive(t *testing.T) {
	ctx := context.Background()
	config := &StreamConfig{
		Modes:      []StreamMode{StreamModeMessages},
		BufferSize: 10,
	}

	stream := NewMultiModeStream(ctx, config)
	defer stream.Close()

	ch, err := stream.Subscribe(StreamModeMessages)
	require.NoError(t, err)

	// Send events
	events := []StreamEvent{
		{Mode: StreamModeMessages, Type: "token", Data: "Hello"},
		{Mode: StreamModeMessages, Type: "token", Data: " World"},
	}

	for _, event := range events {
		event.Timestamp = time.Now()
		err := stream.Stream(StreamModeMessages, event)
		assert.NoError(t, err)
	}

	// Receive events
	received := 0
	timeout := time.After(1 * time.Second)

	for received < len(events) {
		select {
		case event := <-ch:
			assert.Equal(t, StreamModeMessages, event.Mode)
			assert.Equal(t, "token", event.Type)
			received++
		case <-timeout:
			t.Fatal("timeout waiting for events")
		}
	}

	assert.Equal(t, len(events), received)
}

func TestMultiModeStream_GetWriter(t *testing.T) {
	ctx := context.Background()
	config := &StreamConfig{
		Modes:      []StreamMode{StreamModeMessages},
		BufferSize: 10,
	}

	stream := NewMultiModeStream(ctx, config)
	defer stream.Close()

	// Get writer for configured mode
	writer, err := stream.GetWriter(StreamModeMessages)
	assert.NoError(t, err)
	assert.NotNil(t, writer)

	// Get writer for non-configured mode
	_, err = stream.GetWriter(StreamModeUpdates)
	assert.Error(t, err)
}

func TestStreamWriter_Write(t *testing.T) {
	ctx := context.Background()
	config := &StreamConfig{
		Modes:      []StreamMode{StreamModeMessages},
		BufferSize: 10,
	}

	stream := NewMultiModeStream(ctx, config)
	defer stream.Close()

	writer, err := stream.GetWriter(StreamModeMessages)
	require.NoError(t, err)

	ch, err := stream.Subscribe(StreamModeMessages)
	require.NoError(t, err)

	// Write using writer
	err = writer.Write("token", "Hello")
	assert.NoError(t, err)

	// Receive event
	select {
	case event := <-ch:
		assert.Equal(t, StreamModeMessages, event.Mode)
		assert.Equal(t, "token", event.Type)
		assert.Equal(t, "Hello", event.Data)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestStreamWriter_WriteWithMetadata(t *testing.T) {
	ctx := context.Background()
	config := &StreamConfig{
		Modes:      []StreamMode{StreamModeMessages},
		BufferSize: 10,
	}

	stream := NewMultiModeStream(ctx, config)
	defer stream.Close()

	writer, err := stream.GetWriter(StreamModeMessages)
	require.NoError(t, err)

	ch, err := stream.Subscribe(StreamModeMessages)
	require.NoError(t, err)

	metadata := map[string]interface{}{
		"source": "test",
		"index":  1,
	}

	err = writer.WriteWithMetadata("token", "Hello", metadata)
	assert.NoError(t, err)

	select {
	case event := <-ch:
		assert.Equal(t, "token", event.Type)
		assert.Equal(t, "Hello", event.Data)
		assert.Equal(t, "test", event.Metadata["source"])
		assert.Equal(t, 1, event.Metadata["index"])
	case <-time.After(1 * time.Second):
		t.Fatal("timeout")
	}
}

func TestMultiModeStream_SubscribeAll(t *testing.T) {
	ctx := context.Background()
	config := &StreamConfig{
		Modes:      []StreamMode{StreamModeMessages, StreamModeUpdates},
		BufferSize: 10,
	}

	stream := NewMultiModeStream(ctx, config)
	defer stream.Close()

	allCh := stream.SubscribeAll()

	// Send events to different modes
	err := stream.Stream(StreamModeMessages, StreamEvent{
		Mode: StreamModeMessages,
		Type: "token",
		Data: "msg1",
	})
	assert.NoError(t, err)

	err = stream.Stream(StreamModeUpdates, StreamEvent{
		Mode: StreamModeUpdates,
		Type: "state",
		Data: "update1",
	})
	assert.NoError(t, err)

	// Receive from merged channel
	received := 0
	timeout := time.After(2 * time.Second)

	for received < 2 {
		select {
		case event := <-allCh:
			assert.NotNil(t, event)
			received++
		case <-timeout:
			t.Fatalf("timeout, received %d/2 events", received)
		}
	}

	assert.Equal(t, 2, received)
}

func TestMultiModeStream_WithCallback(t *testing.T) {
	ctx := context.Background()

	var mu sync.Mutex
	callbackCalled := 0
	var receivedMode StreamMode
	var receivedEvent StreamEvent

	config := &StreamConfig{
		Modes:      []StreamMode{StreamModeMessages},
		BufferSize: 10,
		Callback: func(mode StreamMode, event StreamEvent) {
			mu.Lock()
			defer mu.Unlock()
			callbackCalled++
			receivedMode = mode
			receivedEvent = event
		},
	}

	stream := NewMultiModeStream(ctx, config)
	defer stream.Close()

	event := StreamEvent{
		Mode: StreamModeMessages,
		Type: "token",
		Data: "test",
	}

	err := stream.Stream(StreamModeMessages, event)
	assert.NoError(t, err)

	// Wait for callback
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, callbackCalled)
	assert.Equal(t, StreamModeMessages, receivedMode)
	assert.Equal(t, "token", receivedEvent.Type)
}

func TestMultiModeStream_IncludeMetadata(t *testing.T) {
	ctx := context.Background()
	config := &StreamConfig{
		Modes:           []StreamMode{StreamModeMessages},
		BufferSize:      10,
		IncludeMetadata: true,
	}

	stream := NewMultiModeStream(ctx, config)
	defer stream.Close()

	ch, _ := stream.Subscribe(StreamModeMessages)

	event := StreamEvent{
		Mode:      StreamModeMessages,
		Type:      "token",
		Data:      "test",
		Timestamp: time.Now(),
	}

	err := stream.Stream(StreamModeMessages, event)
	assert.NoError(t, err)

	select {
	case received := <-ch:
		assert.NotNil(t, received.Metadata)
		assert.Equal(t, "messages", received.Metadata["mode"])
		assert.NotNil(t, received.Metadata["timestamp"])
	case <-time.After(1 * time.Second):
		t.Fatal("timeout")
	}
}

func TestStreamFilter_Apply(t *testing.T) {
	filter := &StreamFilter{
		Modes: []StreamMode{StreamModeMessages},
		Types: []string{"token"},
	}

	tests := []struct {
		name     string
		event    StreamEvent
		expected bool
	}{
		{
			name: "matching mode and type",
			event: StreamEvent{
				Mode: StreamModeMessages,
				Type: "token",
			},
			expected: true,
		},
		{
			name: "non-matching mode",
			event: StreamEvent{
				Mode: StreamModeUpdates,
				Type: "token",
			},
			expected: false,
		},
		{
			name: "non-matching type",
			event: StreamEvent{
				Mode: StreamModeMessages,
				Type: "state",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.Apply(tt.event)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStreamFilter_WithPredicate(t *testing.T) {
	filter := &StreamFilter{
		Predicate: func(event StreamEvent) bool {
			// Only allow events with "important" in data
			if str, ok := event.Data.(string); ok {
				return str == "important"
			}
			return false
		},
	}

	assert.True(t, filter.Apply(StreamEvent{Data: "important"}))
	assert.False(t, filter.Apply(StreamEvent{Data: "normal"}))
}

func TestFilterStream(t *testing.T) {
	input := make(chan StreamEvent, 10)
	filter := &StreamFilter{
		Modes: []StreamMode{StreamModeMessages},
	}

	output := FilterStream(input, filter)

	// Send mixed events
	input <- StreamEvent{Mode: StreamModeMessages, Data: "msg1"}
	input <- StreamEvent{Mode: StreamModeUpdates, Data: "update1"}
	input <- StreamEvent{Mode: StreamModeMessages, Data: "msg2"}
	close(input)

	// Collect filtered events
	var results []StreamEvent
	for event := range output {
		results = append(results, event)
	}

	assert.Len(t, results, 2)
	assert.Equal(t, StreamModeMessages, results[0].Mode)
	assert.Equal(t, StreamModeMessages, results[1].Mode)
}

func TestTransformStream(t *testing.T) {
	input := make(chan StreamEvent, 10)

	transform := func(event StreamEvent) StreamEvent {
		// Uppercase string data
		if str, ok := event.Data.(string); ok {
			event.Data = str + "_TRANSFORMED"
		}
		return event
	}

	output := TransformStream(input, transform)

	input <- StreamEvent{Data: "hello"}
	input <- StreamEvent{Data: "world"}
	close(input)

	results := make([]StreamEvent, 0)
	for event := range output {
		results = append(results, event)
	}

	assert.Len(t, results, 2)
	assert.Equal(t, "hello_TRANSFORMED", results[0].Data)
	assert.Equal(t, "world_TRANSFORMED", results[1].Data)
}

func TestMergeStreams(t *testing.T) {
	stream1 := make(chan StreamEvent, 5)
	stream2 := make(chan StreamEvent, 5)

	// Send events to both streams
	stream1 <- StreamEvent{Data: "s1_1"}
	stream1 <- StreamEvent{Data: "s1_2"}
	close(stream1)

	stream2 <- StreamEvent{Data: "s2_1"}
	stream2 <- StreamEvent{Data: "s2_2"}
	close(stream2)

	merged := MergeStreams(stream1, stream2)

	results := make([]StreamEvent, 0)
	for event := range merged {
		results = append(results, event)
	}

	assert.Len(t, results, 4)
}

func TestStreamAggregator(t *testing.T) {
	ctx := context.Background()

	aggregator := NewStreamAggregator()

	// Create two streams
	stream1 := NewMultiModeStream(ctx, &StreamConfig{
		Modes:      []StreamMode{StreamModeMessages},
		BufferSize: 10,
	})
	defer stream1.Close()

	stream2 := NewMultiModeStream(ctx, &StreamConfig{
		Modes:      []StreamMode{StreamModeMessages},
		BufferSize: 10,
	})
	defer stream2.Close()

	aggregator.AddStream(stream1)
	aggregator.AddStream(stream2)

	// Aggregate messages mode
	aggregated := aggregator.AggregateMode(StreamModeMessages)

	// Send events to both streams
	stream1.Stream(StreamModeMessages, StreamEvent{Data: "stream1"})
	stream2.Stream(StreamModeMessages, StreamEvent{Data: "stream2"})

	// Collect aggregated events
	received := 0
	timeout := time.After(2 * time.Second)

	for received < 2 {
		select {
		case event := <-aggregated:
			assert.NotNil(t, event)
			received++
		case <-timeout:
			t.Fatalf("timeout, received %d/2", received)
		}
	}

	assert.Equal(t, 2, received)
}

func TestStreamModeSelector(t *testing.T) {
	selector := &StreamModeSelector{
		RequiredModes: []StreamMode{StreamModeMessages},
		OptionalModes: []StreamMode{StreamModeUpdates, StreamModeDebug},
		FallbackMode:  StreamModeValues,
	}

	available := []StreamMode{StreamModeMessages, StreamModeUpdates}
	selected := selector.SelectModes(available)

	assert.Contains(t, selected, StreamModeMessages) // Required
	assert.Contains(t, selected, StreamModeUpdates)  // Optional available
	assert.NotContains(t, selected, StreamModeDebug) // Optional not available
}

func TestStreamModeSelector_Fallback(t *testing.T) {
	selector := &StreamModeSelector{
		RequiredModes: []StreamMode{StreamModeCustom},
		FallbackMode:  StreamModeMessages,
	}

	// None of required modes available
	available := []StreamMode{StreamModeUpdates}
	selected := selector.SelectModes(available)

	assert.Contains(t, selected, StreamModeMessages) // Fallback
}

// Benchmark tests

func BenchmarkMultiModeStream_Stream(b *testing.B) {
	ctx := context.Background()
	stream := NewMultiModeStream(ctx, &StreamConfig{
		Modes:      []StreamMode{StreamModeMessages},
		BufferSize: 1000,
	})
	defer stream.Close()

	event := StreamEvent{
		Mode: StreamModeMessages,
		Type: "token",
		Data: "test",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stream.Stream(StreamModeMessages, event)
	}
}

func BenchmarkStreamWriter_Write(b *testing.B) {
	ctx := context.Background()
	stream := NewMultiModeStream(ctx, &StreamConfig{
		Modes:      []StreamMode{StreamModeMessages},
		BufferSize: 1000,
	})
	defer stream.Close()

	writer, _ := stream.GetWriter(StreamModeMessages)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		writer.Write("token", "test")
	}
}
