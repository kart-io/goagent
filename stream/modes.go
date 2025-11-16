package stream

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// StreamMode defines different streaming modes
type StreamMode string

const (
	// StreamModeMessages streams LLM tokens as they are generated
	StreamModeMessages StreamMode = "messages"

	// StreamModeUpdates streams state updates after each step
	StreamModeUpdates StreamMode = "updates"

	// StreamModeCustom streams custom data from tools
	StreamModeCustom StreamMode = "custom"

	// StreamModeValues streams full state snapshots
	StreamModeValues StreamMode = "values"

	// StreamModeDebug streams detailed debug information
	StreamModeDebug StreamMode = "debug"
)

// StreamEvent represents an event in the stream
type StreamEvent struct {
	Mode      StreamMode             `json:"mode"`
	Type      string                 `json:"type"`
	Data      interface{}            `json:"data"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// StreamConfig configures multi-mode streaming
type StreamConfig struct {
	// Modes specifies which stream modes to enable
	Modes []StreamMode

	// BufferSize for each stream channel
	BufferSize int

	// IncludeMetadata adds extra metadata to events
	IncludeMetadata bool

	// Callback for handling stream events (optional)
	Callback func(mode StreamMode, event StreamEvent)
}

// DefaultStreamConfig returns default configuration
func DefaultStreamConfig() *StreamConfig {
	return &StreamConfig{
		Modes:           []StreamMode{StreamModeMessages},
		BufferSize:      100,
		IncludeMetadata: false,
	}
}

// MultiModeStream handles different streaming modes
type MultiModeStream struct {
	config   *StreamConfig
	channels map[StreamMode]chan StreamEvent
	writers  map[StreamMode]*StreamWriter
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewMultiModeStream creates a new multi-mode stream
func NewMultiModeStream(ctx context.Context, config *StreamConfig) *MultiModeStream {
	if config == nil {
		config = DefaultStreamConfig()
	}

	streamCtx, cancel := context.WithCancel(ctx)

	s := &MultiModeStream{
		config:   config,
		channels: make(map[StreamMode]chan StreamEvent),
		writers:  make(map[StreamMode]*StreamWriter),
		ctx:      streamCtx,
		cancel:   cancel,
	}

	// Initialize channels for requested modes
	for _, mode := range config.Modes {
		ch := make(chan StreamEvent, config.BufferSize)
		s.channels[mode] = ch
		s.writers[mode] = &StreamWriter{
			stream: s,
			mode:   mode,
		}

		// Start processor for callback if provided
		if config.Callback != nil {
			s.wg.Add(1)
			go s.processMode(mode, ch)
		}
	}

	return s
}

// processMode processes events for a specific mode
func (s *MultiModeStream) processMode(mode StreamMode, ch <-chan StreamEvent) {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			if s.config.Callback != nil {
				s.config.Callback(mode, event)
			}
		}
	}
}

// Stream sends an event to the appropriate mode channel
func (s *MultiModeStream) Stream(mode StreamMode, event StreamEvent) error {
	s.mu.RLock()
	ch, exists := s.channels[mode]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("stream mode %s not configured", mode)
	}

	// Add metadata if configured
	if s.config.IncludeMetadata {
		if event.Metadata == nil {
			event.Metadata = make(map[string]interface{})
		}
		event.Metadata["mode"] = string(mode)
		event.Metadata["timestamp"] = event.Timestamp.UnixNano()
	}

	select {
	case <-s.ctx.Done():
		return context.Canceled
	case ch <- event:
		return nil
	default:
		// Non-blocking send - drop if buffer is full
		return fmt.Errorf("stream buffer full for mode %s", mode)
	}
}

// GetWriter returns a writer for a specific mode
func (s *MultiModeStream) GetWriter(mode StreamMode) (*StreamWriter, error) {
	s.mu.RLock()
	writer, exists := s.writers[mode]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no writer for mode %s", mode)
	}

	return writer, nil
}

// Subscribe returns a channel for receiving events of a specific mode
func (s *MultiModeStream) Subscribe(mode StreamMode) (<-chan StreamEvent, error) {
	s.mu.RLock()
	ch, exists := s.channels[mode]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("mode %s not configured", mode)
	}

	return ch, nil
}

// SubscribeAll returns a merged channel for all configured modes
func (s *MultiModeStream) SubscribeAll() <-chan StreamEvent {
	merged := make(chan StreamEvent, s.config.BufferSize*len(s.config.Modes))

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer close(merged)

		// Create cases for select
		cases := make([]<-chan StreamEvent, 0, len(s.channels))
		s.mu.RLock()
		for _, ch := range s.channels {
			cases = append(cases, ch)
		}
		s.mu.RUnlock()

		// Read from all channels
		for {
			for _, ch := range cases {
				select {
				case <-s.ctx.Done():
					return
				case event, ok := <-ch:
					if ok {
						select {
						case merged <- event:
						case <-s.ctx.Done():
							return
						}
					}
				default:
					// Non-blocking
				}
			}

			// Small sleep to prevent busy loop
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()

	return merged
}

// Close closes all streams
func (s *MultiModeStream) Close() error {
	s.cancel()

	s.mu.Lock()
	for _, ch := range s.channels {
		close(ch)
	}
	s.mu.Unlock()

	// Wait for all processors to finish
	s.wg.Wait()

	return nil
}

// StreamWriter provides a writer interface for a specific mode
type StreamWriter struct {
	stream *MultiModeStream
	mode   StreamMode
}

// Write sends data to the stream
func (w *StreamWriter) Write(eventType string, data interface{}) error {
	return w.stream.Stream(w.mode, StreamEvent{
		Mode:      w.mode,
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now(),
	})
}

// WriteWithMetadata sends data with metadata
func (w *StreamWriter) WriteWithMetadata(eventType string, data interface{}, metadata map[string]interface{}) error {
	return w.stream.Stream(w.mode, StreamEvent{
		Mode:      w.mode,
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now(),
		Metadata:  metadata,
	})
}

// StreamAggregator aggregates multiple streams
type StreamAggregator struct {
	streams []*MultiModeStream
	mu      sync.RWMutex
}

// NewStreamAggregator creates a new aggregator
func NewStreamAggregator() *StreamAggregator {
	return &StreamAggregator{
		streams: make([]*MultiModeStream, 0),
	}
}

// AddStream adds a stream to the aggregator
func (a *StreamAggregator) AddStream(stream *MultiModeStream) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.streams = append(a.streams, stream)
}

// AggregateMode aggregates a specific mode from all streams
func (a *StreamAggregator) AggregateMode(mode StreamMode) <-chan StreamEvent {
	aggregated := make(chan StreamEvent, 100)

	go func() {
		defer close(aggregated)

		var wg sync.WaitGroup
		a.mu.RLock()
		streams := append([]*MultiModeStream{}, a.streams...)
		a.mu.RUnlock()

		for _, stream := range streams {
			ch, err := stream.Subscribe(mode)
			if err != nil {
				continue
			}

			wg.Add(1)
			go func(source <-chan StreamEvent) {
				defer wg.Done()
				for event := range source {
					select {
					case aggregated <- event:
					default:
						// Drop if full
					}
				}
			}(ch)
		}

		wg.Wait()
	}()

	return aggregated
}

// StreamModeSelector helps select appropriate modes
type StreamModeSelector struct {
	RequiredModes []StreamMode
	OptionalModes []StreamMode
	FallbackMode  StreamMode
}

// SelectModes returns the modes to use based on availability
func (s *StreamModeSelector) SelectModes(available []StreamMode) []StreamMode {
	selected := make([]StreamMode, 0)
	availableSet := make(map[StreamMode]bool)

	for _, mode := range available {
		availableSet[mode] = true
	}

	// Add required modes if available
	for _, mode := range s.RequiredModes {
		if availableSet[mode] {
			selected = append(selected, mode)
		}
	}

	// Add optional modes if available
	for _, mode := range s.OptionalModes {
		if availableSet[mode] {
			selected = append(selected, mode)
		}
	}

	// Use fallback if no modes selected
	if len(selected) == 0 && s.FallbackMode != "" {
		selected = append(selected, s.FallbackMode)
	}

	return selected
}

// StreamFilter filters stream events
type StreamFilter struct {
	Modes     []StreamMode           // Filter by modes
	Types     []string               // Filter by event types
	Predicate func(StreamEvent) bool // Custom filter function
}

// Apply applies the filter to an event
func (f *StreamFilter) Apply(event StreamEvent) bool {
	// Check mode filter
	if len(f.Modes) > 0 {
		found := false
		for _, mode := range f.Modes {
			if event.Mode == mode {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check type filter
	if len(f.Types) > 0 {
		found := false
		for _, typ := range f.Types {
			if event.Type == typ {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Apply custom predicate
	if f.Predicate != nil {
		return f.Predicate(event)
	}

	return true
}

// FilterStream creates a filtered stream
func FilterStream(input <-chan StreamEvent, filter *StreamFilter) <-chan StreamEvent {
	output := make(chan StreamEvent, 100)

	go func() {
		defer close(output)

		for event := range input {
			if filter.Apply(event) {
				select {
				case output <- event:
				default:
					// Drop if full
				}
			}
		}
	}()

	return output
}

// TransformStream transforms stream events
func TransformStream(input <-chan StreamEvent, transform func(StreamEvent) StreamEvent) <-chan StreamEvent {
	output := make(chan StreamEvent, 100)

	go func() {
		defer close(output)

		for event := range input {
			transformed := transform(event)
			select {
			case output <- transformed:
			default:
				// Drop if full
			}
		}
	}()

	return output
}

// MergeStreams merges multiple stream channels
func MergeStreams(streams ...<-chan StreamEvent) <-chan StreamEvent {
	merged := make(chan StreamEvent, 100*len(streams))

	var wg sync.WaitGroup
	for _, stream := range streams {
		wg.Add(1)
		go func(s <-chan StreamEvent) {
			defer wg.Done()
			for event := range s {
				select {
				case merged <- event:
				default:
					// Drop if full
				}
			}
		}(stream)
	}

	go func() {
		wg.Wait()
		close(merged)
	}()

	return merged
}
