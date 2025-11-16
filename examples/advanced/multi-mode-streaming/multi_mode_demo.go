package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/stream"
)

func main() {
	fmt.Println("=== Multi-Mode Streaming Demo ===")

	// Demo 1: Basic Multi-Mode Streaming
	demo1BasicMultiMode()

	fmt.Println()

	// Demo 2: Stream Writer Usage
	demo2StreamWriter()

	fmt.Println()

	// Demo 3: Subscribe All Modes
	demo3SubscribeAll()

	fmt.Println()

	// Demo 4: Stream Filtering
	demo4StreamFiltering()

	fmt.Println()

	// Demo 5: Stream Transformation
	demo5StreamTransformation()

	fmt.Println()

	// Demo 6: Multiple Stream Aggregation
	demo6StreamAggregation()

	fmt.Println("\n=== Demo Complete ===")
}

// Demo 1: Basic multi-mode streaming
func demo1BasicMultiMode() {
	fmt.Println("--- Demo 1: Basic Multi-Mode Streaming ---")

	ctx := context.Background()
	config := &stream.StreamConfig{
		Modes:      []stream.StreamMode{stream.StreamModeMessages, stream.StreamModeUpdates},
		BufferSize: 10,
	}

	multiStream := stream.NewMultiModeStream(ctx, config)
	defer multiStream.Close()

	// Subscribe to messages mode
	msgCh, _ := multiStream.Subscribe(stream.StreamModeMessages)

	// Subscribe to updates mode
	updatesCh, _ := multiStream.Subscribe(stream.StreamModeUpdates)

	// Send events to different modes
	multiStream.Stream(stream.StreamModeMessages, stream.StreamEvent{
		Mode:      stream.StreamModeMessages,
		Type:      "token",
		Data:      "Hello",
		Timestamp: time.Now(),
	})

	multiStream.Stream(stream.StreamModeUpdates, stream.StreamEvent{
		Mode:      stream.StreamModeUpdates,
		Type:      "state_change",
		Data:      map[string]interface{}{"user_id": "123", "status": "active"},
		Timestamp: time.Now(),
	})

	// Receive from different modes
	timeout := time.After(500 * time.Millisecond)

	select {
	case event := <-msgCh:
		fmt.Printf("[%s] Type: %s, Data: %v\n", event.Mode, event.Type, event.Data)
	case <-timeout:
		fmt.Println("No message event received")
	}

	select {
	case event := <-updatesCh:
		fmt.Printf("[%s] Type: %s, Data: %v\n", event.Mode, event.Type, event.Data)
	case <-timeout:
		fmt.Println("No update event received")
	}
}

// Demo 2: Using StreamWriter
func demo2StreamWriter() {
	fmt.Println("--- Demo 2: Stream Writer Usage ---")

	ctx := context.Background()
	config := &stream.StreamConfig{
		Modes:      []stream.StreamMode{stream.StreamModeMessages, stream.StreamModeCustom},
		BufferSize: 10,
	}

	multiStream := stream.NewMultiModeStream(ctx, config)
	defer multiStream.Close()

	// Get writers for different modes
	msgWriter, _ := multiStream.GetWriter(stream.StreamModeMessages)
	customWriter, _ := multiStream.GetWriter(stream.StreamModeCustom)

	// Subscribe to channels
	msgCh, _ := multiStream.Subscribe(stream.StreamModeMessages)
	customCh, _ := multiStream.Subscribe(stream.StreamModeCustom)

	// Write using writers
	msgWriter.Write("token", "LLM")
	msgWriter.Write("token", " generated")
	msgWriter.Write("token", " text")

	customWriter.WriteWithMetadata("progress", map[string]interface{}{
		"step":     1,
		"status":   "processing",
		"progress": 50,
	}, map[string]interface{}{
		"source": "tool_execution",
	})

	// Collect messages
	var tokens []string
	timeout := time.After(500 * time.Millisecond)

collectLoop:
	for {
		select {
		case event := <-msgCh:
			if event.Type == "token" {
				tokens = append(tokens, event.Data.(string))
			}
		case event := <-customCh:
			fmt.Printf("[Custom] %s: %+v (source: %v)\n",
				event.Type, event.Data, event.Metadata["source"])
		case <-timeout:
			break collectLoop
		}
	}

	fmt.Printf("Collected tokens: %v\n", tokens)
}

// Demo 3: Subscribe to all modes
func demo3SubscribeAll() {
	fmt.Println("--- Demo 3: Subscribe All Modes ---")

	ctx := context.Background()
	config := &stream.StreamConfig{
		Modes:      []stream.StreamMode{stream.StreamModeMessages, stream.StreamModeUpdates, stream.StreamModeCustom},
		BufferSize: 10,
	}

	multiStream := stream.NewMultiModeStream(ctx, config)
	defer multiStream.Close()

	// Subscribe to all modes at once
	allCh := multiStream.SubscribeAll()

	// Send events to different modes
	multiStream.Stream(stream.StreamModeMessages, stream.StreamEvent{
		Mode: stream.StreamModeMessages,
		Type: "token",
		Data: "Message 1",
	})

	multiStream.Stream(stream.StreamModeUpdates, stream.StreamEvent{
		Mode: stream.StreamModeUpdates,
		Type: "state",
		Data: "State update 1",
	})

	multiStream.Stream(stream.StreamModeCustom, stream.StreamEvent{
		Mode: stream.StreamModeCustom,
		Type: "tool_output",
		Data: "Tool result 1",
	})

	// Receive all events
	received := 0
	timeout := time.After(1 * time.Second)

loop1:
	for received < 3 {
		select {
		case event := <-allCh:
			fmt.Printf("[%s] %s: %v\n", event.Mode, event.Type, event.Data)
			received++
		case <-timeout:
			fmt.Printf("Timeout, received %d/3 events\n", received)
			break loop1
		}
	}
}

// Demo 4: Stream filtering
func demo4StreamFiltering() {
	fmt.Println("--- Demo 4: Stream Filtering ---")

	input := make(chan stream.StreamEvent, 10)

	// Create filter: only messages mode events
	filter := &stream.StreamFilter{
		Modes: []stream.StreamMode{stream.StreamModeMessages},
	}

	output := stream.FilterStream(input, filter)

	// Send mixed events
	go func() {
		input <- stream.StreamEvent{Mode: stream.StreamModeMessages, Data: "msg1"}
		input <- stream.StreamEvent{Mode: stream.StreamModeUpdates, Data: "update1"}
		input <- stream.StreamEvent{Mode: stream.StreamModeMessages, Data: "msg2"}
		input <- stream.StreamEvent{Mode: stream.StreamModeCustom, Data: "custom1"}
		input <- stream.StreamEvent{Mode: stream.StreamModeMessages, Data: "msg3"}
		close(input)
	}()

	// Collect filtered results
	fmt.Println("Filtered events (messages only):")
	for event := range output {
		fmt.Printf("  - [%s] %v\n", event.Mode, event.Data)
	}
}

// Demo 5: Stream transformation
func demo5StreamTransformation() {
	fmt.Println("--- Demo 5: Stream Transformation ---")

	input := make(chan stream.StreamEvent, 10)

	// Create transform function: uppercase string data
	transform := func(event stream.StreamEvent) stream.StreamEvent {
		if str, ok := event.Data.(string); ok {
			event.Data = "[TRANSFORMED] " + str
		}
		return event
	}

	output := stream.TransformStream(input, transform)

	// Send events
	go func() {
		input <- stream.StreamEvent{Data: "hello"}
		input <- stream.StreamEvent{Data: "world"}
		input <- stream.StreamEvent{Data: 123} // Non-string
		close(input)
	}()

	// Collect transformed results
	fmt.Println("Transformed events:")
	for event := range output {
		fmt.Printf("  - %v\n", event.Data)
	}
}

// Demo 6: Stream aggregation
func demo6StreamAggregation() {
	fmt.Println("--- Demo 6: Multiple Stream Aggregation ---")

	ctx := context.Background()

	// Create aggregator
	aggregator := stream.NewStreamAggregator()

	// Create multiple streams (simulating different agents)
	agent1Stream := stream.NewMultiModeStream(ctx, &stream.StreamConfig{
		Modes:      []stream.StreamMode{stream.StreamModeMessages},
		BufferSize: 10,
	})
	defer agent1Stream.Close()

	agent2Stream := stream.NewMultiModeStream(ctx, &stream.StreamConfig{
		Modes:      []stream.StreamMode{stream.StreamModeMessages},
		BufferSize: 10,
	})
	defer agent2Stream.Close()

	// Add streams to aggregator
	aggregator.AddStream(agent1Stream)
	aggregator.AddStream(agent2Stream)

	// Get aggregated channel for messages mode
	aggregated := aggregator.AggregateMode(stream.StreamModeMessages)

	// Send events from different agents
	agent1Stream.Stream(stream.StreamModeMessages, stream.StreamEvent{
		Mode: stream.StreamModeMessages,
		Type: "response",
		Data: "Agent 1: Hello",
		Metadata: map[string]interface{}{
			"agent": "agent1",
		},
	})

	agent2Stream.Stream(stream.StreamModeMessages, stream.StreamEvent{
		Mode: stream.StreamModeMessages,
		Type: "response",
		Data: "Agent 2: Hi there",
		Metadata: map[string]interface{}{
			"agent": "agent2",
		},
	})

	// Collect aggregated events
	fmt.Println("Aggregated events from multiple agents:")
	received := 0
	timeout := time.After(1 * time.Second)

loop2:
	for received < 2 {
		select {
		case event := <-aggregated:
			agent := "unknown"
			if event.Metadata != nil {
				if a, ok := event.Metadata["agent"]; ok {
					agent = a.(string)
				}
			}
			fmt.Printf("  - [%s] %v\n", agent, event.Data)
			received++
		case <-timeout:
			fmt.Printf("Timeout, received %d/2 events\n", received)
			break loop2
		}
	}
}
