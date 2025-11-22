package observability

import (
	"context"
	"errors"
	"testing"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/logger/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// MockAgent implements the Agent interface for testing
type MockAgent struct {
	mock.Mock
}

func (m *MockAgent) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockAgent) Description() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockAgent) Capabilities() []string {
	args := m.Called()
	return args.Get(0).([]string)
}

func (m *MockAgent) Invoke(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*agentcore.AgentOutput), args.Error(1)
}

func (m *MockAgent) Stream(ctx context.Context, input *agentcore.AgentInput) (<-chan agentcore.StreamChunk[*agentcore.AgentOutput], error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(<-chan agentcore.StreamChunk[*agentcore.AgentOutput]), args.Error(1)
}

func (m *MockAgent) Batch(ctx context.Context, inputs []*agentcore.AgentInput) ([]*agentcore.AgentOutput, error) {
	args := m.Called(ctx, inputs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*agentcore.AgentOutput), args.Error(1)
}

func (m *MockAgent) Pipe(next agentcore.Runnable[*agentcore.AgentOutput, any]) agentcore.Runnable[*agentcore.AgentInput, any] {
	args := m.Called(next)
	return args.Get(0).(agentcore.Runnable[*agentcore.AgentInput, any])
}

func (m *MockAgent) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	args := m.Called(callbacks)
	return args.Get(0).(agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput])
}

func (m *MockAgent) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	args := m.Called(config)
	return args.Get(0).(agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput])
}

// MockLogger implements the Logger interface for testing
type MockLogger struct {
	mock.Mock
}

func (m *MockLogger) Info(args ...interface{}) {
	m.Called(args)
}

func (m *MockLogger) Error(args ...interface{}) {
	m.Called(args)
}

func (m *MockLogger) Warn(args ...interface{}) {
	m.Called(args)
}

func (m *MockLogger) Debug(args ...interface{}) {
	m.Called(args)
}

func (m *MockLogger) Infof(template string, args ...interface{}) {
	m.Called(template, args)
}

func (m *MockLogger) Errorf(template string, args ...interface{}) {
	m.Called(template, args)
}

func (m *MockLogger) Warnf(template string, args ...interface{}) {
	m.Called(template, args)
}

func (m *MockLogger) Debugf(template string, args ...interface{}) {
	m.Called(template, args)
}

func (m *MockLogger) Fatalf(template string, args ...interface{}) {
	m.Called(template, args)
}

func (m *MockLogger) Fatal(args ...interface{}) {
	m.Called(args)
}

func (m *MockLogger) Infow(msg string, keysAndValues ...interface{}) {
	m.Called(msg, keysAndValues)
}

func (m *MockLogger) Errorw(msg string, keysAndValues ...interface{}) {
	m.Called(msg, keysAndValues)
}

func (m *MockLogger) Warnw(msg string, keysAndValues ...interface{}) {
	m.Called(msg, keysAndValues)
}

func (m *MockLogger) Debugw(msg string, keysAndValues ...interface{}) {
	m.Called(msg, keysAndValues)
}

func (m *MockLogger) Fatalw(msg string, keysAndValues ...interface{}) {
	m.Called(msg, keysAndValues)
}

func (m *MockLogger) With(keyValues ...interface{}) core.Logger {
	args := m.Called(keyValues)
	return args.Get(0).(core.Logger)
}

func (m *MockLogger) WithCtx(ctx context.Context, keyValues ...interface{}) core.Logger {
	args := m.Called(ctx, keyValues)
	return args.Get(0).(core.Logger)
}

func (m *MockLogger) WithCallerSkip(skip int) core.Logger {
	args := m.Called(skip)
	return args.Get(0).(core.Logger)
}

func (m *MockLogger) SetLevel(level core.Level) {
	m.Called(level)
}

func (m *MockLogger) Flush() error {
	args := m.Called()
	return args.Error(0)
}

func TestNewInstrumentedAgent(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("test-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.MatchedBy(func(args []interface{}) bool { return true })).Return()
	mockLogger.On("Error", mock.MatchedBy(func(args []interface{}) bool { return true })).Return()

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	assert.NotNil(t, instrumentedAgent)
	assert.Equal(t, mockAgent, instrumentedAgent.agent)
	assert.Equal(t, "test-service", instrumentedAgent.serviceName)
}

func TestInstrumentedAgent_Name(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("my-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	assert.Equal(t, "my-agent", instrumentedAgent.Name())
	mockAgent.AssertExpectations(t)
}

func TestInstrumentedAgent_Description(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("test-agent")
	mockAgent.On("Description").Return("Test agent description")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.MatchedBy(func(args []interface{}) bool { return true })).Return()
	mockLogger.On("Error", mock.MatchedBy(func(args []interface{}) bool { return true })).Return()

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	assert.Equal(t, "Test agent description", instrumentedAgent.Description())
	mockAgent.AssertExpectations(t)
}

func TestInstrumentedAgent_Capabilities(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("test-agent")
	capabilities := []string{"capability1", "capability2", "capability3"}
	mockAgent.On("Capabilities").Return(capabilities)

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.MatchedBy(func(args []interface{}) bool { return true })).Return()
	mockLogger.On("Error", mock.MatchedBy(func(args []interface{}) bool { return true })).Return()

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	assert.Equal(t, capabilities, instrumentedAgent.Capabilities())
	mockAgent.AssertExpectations(t)
}

func TestInstrumentedAgent_Invoke_Success(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("test-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()

	output := &agentcore.AgentOutput{
		Status:         "completed",
		ToolCalls:      []agentcore.ToolCall{},
		ReasoningSteps: []agentcore.ReasoningStep{},
	}
	mockAgent.On("Invoke", mock.Anything, mock.Anything).Return(output, nil)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:      "test task",
		SessionID: "session-123",
	}

	result, err := instrumentedAgent.Invoke(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "completed", result.Status)
	mockAgent.AssertExpectations(t)
}

func TestInstrumentedAgent_Invoke_WithError(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("error-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.MatchedBy(func(args []interface{}) bool { return true })).Return()
	mockLogger.On("Error", mock.MatchedBy(func(args []interface{}) bool { return true })).Return()

	testErr := errors.New("agent execution failed")
	output := &agentcore.AgentOutput{
		Status:         "failed",
		ToolCalls:      []agentcore.ToolCall{},
		ReasoningSteps: []agentcore.ReasoningStep{},
	}
	mockAgent.On("Invoke", mock.Anything, mock.Anything).Return(output, testErr)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:      "failing task",
		SessionID: "session-456",
	}

	result, err := instrumentedAgent.Invoke(ctx, input)

	assert.Error(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, testErr, err)
	mockAgent.AssertExpectations(t)
}

func TestInstrumentedAgent_Invoke_WithToolCalls(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("tool-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()

	output := &agentcore.AgentOutput{
		Status: "completed",
		ToolCalls: []agentcore.ToolCall{
			{
				ToolName: "calculator",
				Success:  true,
				Duration: 100 * time.Millisecond,
			},
			{
				ToolName: "search",
				Success:  true,
				Duration: 500 * time.Millisecond,
			},
		},
		ReasoningSteps: []agentcore.ReasoningStep{},
	}
	mockAgent.On("Invoke", mock.Anything, mock.Anything).Return(output, nil)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:      "calculation task",
		SessionID: "session-789",
	}

	result, err := instrumentedAgent.Invoke(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.ToolCalls, 2)
	mockAgent.AssertExpectations(t)
}

func TestInstrumentedAgent_Invoke_WithFailedToolCalls(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("failing-tool-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()

	output := &agentcore.AgentOutput{
		Status: "completed",
		ToolCalls: []agentcore.ToolCall{
			{
				ToolName: "calculator",
				Success:  true,
				Duration: 100 * time.Millisecond,
			},
			{
				ToolName: "invalid-tool",
				Success:  false,
				Duration: 50 * time.Millisecond,
			},
		},
		ReasoningSteps: []agentcore.ReasoningStep{},
	}
	mockAgent.On("Invoke", mock.Anything, mock.Anything).Return(output, nil)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:      "mixed result task",
		SessionID: "session-mixed",
	}

	result, err := instrumentedAgent.Invoke(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.ToolCalls, 2)
	mockAgent.AssertExpectations(t)
}

func TestInstrumentedAgent_Invoke_ConcurrentExecutions(t *testing.T) {
	metricsOnce.Do(func() {})

	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("concurrent-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()

	output := &agentcore.AgentOutput{
		Status:         "completed",
		ToolCalls:      []agentcore.ToolCall{},
		ReasoningSteps: []agentcore.ReasoningStep{},
	}
	mockAgent.On("Invoke", mock.Anything, mock.Anything).Return(output, nil)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:      "test task",
		SessionID: "session-123",
	}

	// Invoke should increment and decrement concurrent executions
	result, err := instrumentedAgent.Invoke(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestInstrumentedAgent_Invoke_WithDifferentDurations(t *testing.T) {
	durations := []time.Duration{
		1 * time.Millisecond,
		100 * time.Millisecond,
		1 * time.Second,
		5 * time.Second,
	}

	for _, expectedDuration := range durations {
		t.Run("duration-"+expectedDuration.String(), func(t *testing.T) {
			mockAgent := new(MockAgent)
			mockAgent.On("Name").Return("duration-agent")

			mockLogger := new(MockLogger)
			mockLogger.On("With", mock.Anything).Return(mockLogger)
			mockLogger.On("Info", mock.Anything, mock.Anything).Return()

			output := &agentcore.AgentOutput{
				Status:         "completed",
				ToolCalls:      []agentcore.ToolCall{},
				ReasoningSteps: []agentcore.ReasoningStep{},
			}

			// Simulate execution time
			mockAgent.On("Invoke", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				time.Sleep(expectedDuration)
			}).Return(output, nil)

			instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

			ctx := context.Background()
			input := &agentcore.AgentInput{
				Task:      "timing test",
				SessionID: "session-timing",
			}

			start := time.Now()
			result, err := instrumentedAgent.Invoke(ctx, input)
			elapsed := time.Since(start)

			assert.NoError(t, err)
			assert.NotNil(t, result)
			assert.True(t, elapsed >= expectedDuration)
			mockAgent.AssertExpectations(t)
		})
	}
}

func TestInstrumentedAgent_Invoke_WithManyToolCalls(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("many-tools-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()

	// Create many tool calls
	toolCalls := make([]agentcore.ToolCall, 100)
	for i := 0; i < 100; i++ {
		toolCalls[i] = agentcore.ToolCall{
			ToolName: "tool-" + string(rune(i)),
			Success:  i%2 == 0,
			Duration: time.Duration(i*10) * time.Millisecond,
		}
	}

	output := &agentcore.AgentOutput{
		Status:         "completed",
		ToolCalls:      toolCalls,
		ReasoningSteps: []agentcore.ReasoningStep{},
	}
	mockAgent.On("Invoke", mock.Anything, mock.Anything).Return(output, nil)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:      "many tools task",
		SessionID: "session-many",
	}

	result, err := instrumentedAgent.Invoke(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.ToolCalls, 100)
}

func TestInstrumentedAgent_Delegation_Methods(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("test-agent")
	mockAgent.On("Description").Return("test description")
	mockAgent.On("Capabilities").Return([]string{"cap1", "cap2"})
	mockAgent.On("Stream", mock.Anything, mock.Anything).Return(nil, errors.New("not implemented"))
	mockAgent.On("Batch", mock.Anything, mock.Anything).Return(nil, errors.New("not implemented"))

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	// Add expectations for Info and Error calls from instrumentation
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()
	mockLogger.On("Error", mock.Anything, mock.Anything).Return()

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	// Test that delegation works
	assert.Equal(t, "test-agent", instrumentedAgent.Name())
	assert.Equal(t, "test description", instrumentedAgent.Description())
	assert.Equal(t, []string{"cap1", "cap2"}, instrumentedAgent.Capabilities())

	// Stream should delegate and properly handle errors
	_, err := instrumentedAgent.Stream(context.Background(), &agentcore.AgentInput{})
	assert.Error(t, err)
	assert.Equal(t, "not implemented", err.Error())

	// Batch should delegate and properly handle errors
	_, err = instrumentedAgent.Batch(context.Background(), []*agentcore.AgentInput{})
	assert.Error(t, err)
	assert.Equal(t, "not implemented", err.Error())

	mockAgent.AssertExpectations(t)
}

func TestWrapAgent(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("wrapped-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)

	wrapped := WrapAgent(mockAgent, "test-service", mockLogger)

	assert.NotNil(t, wrapped)
	assert.Equal(t, "wrapped-agent", wrapped.Name())
}

func TestInstrumentedAgent_WithOpenTelemetry(t *testing.T) {
	// Setup OpenTelemetry
	provider := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(provider)
	defer provider.Shutdown(context.Background())

	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("otel-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()

	output := &agentcore.AgentOutput{
		Status:         "completed",
		ToolCalls:      []agentcore.ToolCall{},
		ReasoningSteps: []agentcore.ReasoningStep{},
	}
	mockAgent.On("Invoke", mock.Anything, mock.Anything).Return(output, nil)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:      "otel test",
		SessionID: "session-otel",
	}

	result, err := instrumentedAgent.Invoke(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	mockAgent.AssertExpectations(t)
}

func TestInstrumentedAgent_ContextPropagation(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("context-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()

	output := &agentcore.AgentOutput{
		Status:         "completed",
		ToolCalls:      []agentcore.ToolCall{},
		ReasoningSteps: []agentcore.ReasoningStep{},
	}

	capturedCtx := context.Background()
	mockAgent.On("Invoke", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		capturedCtx = args.Get(0).(context.Context)
	}).Return(output, nil)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:      "context test",
		SessionID: "session-ctx",
	}

	_, err := instrumentedAgent.Invoke(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, capturedCtx)
}

func BenchmarkInstrumentedAgent_Invoke(b *testing.B) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("bench-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()

	output := &agentcore.AgentOutput{
		Status:         "completed",
		ToolCalls:      []agentcore.ToolCall{},
		ReasoningSteps: []agentcore.ReasoningStep{},
	}
	mockAgent.On("Invoke", mock.Anything, mock.Anything).Return(output, nil)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)
	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:      "benchmark",
		SessionID: "bench-session",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		instrumentedAgent.Invoke(ctx, input)
	}
}

func TestInstrumentedAgent_Stream_Success(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("stream-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()

	// Create a channel with stream chunks
	streamChan := make(chan agentcore.StreamChunk[*agentcore.AgentOutput], 3)
	go func() {
		defer close(streamChan)
		streamChan <- agentcore.StreamChunk[*agentcore.AgentOutput]{
			Data: &agentcore.AgentOutput{
				Status:         "processing",
				ToolCalls:      []agentcore.ToolCall{},
				ReasoningSteps: []agentcore.ReasoningStep{},
			},
			Done: false,
		}
		streamChan <- agentcore.StreamChunk[*agentcore.AgentOutput]{
			Data: &agentcore.AgentOutput{
				Status:         "processing",
				ToolCalls:      []agentcore.ToolCall{},
				ReasoningSteps: []agentcore.ReasoningStep{},
			},
			Done: false,
		}
		streamChan <- agentcore.StreamChunk[*agentcore.AgentOutput]{
			Data: &agentcore.AgentOutput{
				Status:         "completed",
				ToolCalls:      []agentcore.ToolCall{},
				ReasoningSteps: []agentcore.ReasoningStep{},
			},
			Done: true,
		}
	}()

	mockAgent.On("Stream", mock.Anything, mock.Anything).Return((<-chan agentcore.StreamChunk[*agentcore.AgentOutput])(streamChan), nil)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:      "stream task",
		SessionID: "session-stream",
	}

	resultChan, err := instrumentedAgent.Stream(ctx, input)
	assert.NoError(t, err)
	assert.NotNil(t, resultChan)

	// Consume all chunks
	chunkCount := 0
	for chunk := range resultChan {
		chunkCount++
		assert.NotNil(t, chunk.Data)
		if chunk.Done {
			assert.Equal(t, "completed", chunk.Data.Status)
		}
	}

	assert.Equal(t, 3, chunkCount)
	mockAgent.AssertExpectations(t)
}

func TestInstrumentedAgent_Stream_WithError(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("stream-error-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()
	mockLogger.On("Error", mock.Anything, mock.Anything).Return()

	testErr := errors.New("stream initialization failed")
	mockAgent.On("Stream", mock.Anything, mock.Anything).Return(nil, testErr)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:      "failing stream",
		SessionID: "session-fail",
	}

	resultChan, err := instrumentedAgent.Stream(ctx, input)
	assert.Error(t, err)
	assert.Nil(t, resultChan)
	assert.Equal(t, testErr, err)
	mockAgent.AssertExpectations(t)
}

func TestInstrumentedAgent_Stream_WithChunkError(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("stream-chunk-error-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()
	mockLogger.On("Error", mock.Anything, mock.Anything).Return()

	// Create a channel with an error chunk
	streamChan := make(chan agentcore.StreamChunk[*agentcore.AgentOutput], 2)
	go func() {
		defer close(streamChan)
		streamChan <- agentcore.StreamChunk[*agentcore.AgentOutput]{
			Data: &agentcore.AgentOutput{
				Status: "processing",
			},
			Done: false,
		}
		streamChan <- agentcore.StreamChunk[*agentcore.AgentOutput]{
			Error: errors.New("processing error"),
			Done:  true,
		}
	}()

	mockAgent.On("Stream", mock.Anything, mock.Anything).Return((<-chan agentcore.StreamChunk[*agentcore.AgentOutput])(streamChan), nil)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:      "chunk error task",
		SessionID: "session-chunk-error",
	}

	resultChan, err := instrumentedAgent.Stream(ctx, input)
	assert.NoError(t, err)

	// Consume chunks and find the error
	var foundError error
	for chunk := range resultChan {
		if chunk.Error != nil {
			foundError = chunk.Error
		}
	}

	assert.Error(t, foundError)
	assert.Equal(t, "processing error", foundError.Error())
	mockAgent.AssertExpectations(t)
}

func TestInstrumentedAgent_Stream_WithToolCalls(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("stream-tools-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()

	// Create a channel with tool calls in the final chunk
	streamChan := make(chan agentcore.StreamChunk[*agentcore.AgentOutput], 1)
	go func() {
		defer close(streamChan)
		streamChan <- agentcore.StreamChunk[*agentcore.AgentOutput]{
			Data: &agentcore.AgentOutput{
				Status: "completed",
				ToolCalls: []agentcore.ToolCall{
					{
						ToolName: "calculator",
						Success:  true,
						Duration: 100 * time.Millisecond,
					},
					{
						ToolName: "search",
						Success:  false,
						Duration: 50 * time.Millisecond,
					},
				},
			},
			Done: true,
		}
	}()

	mockAgent.On("Stream", mock.Anything, mock.Anything).Return((<-chan agentcore.StreamChunk[*agentcore.AgentOutput])(streamChan), nil)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:      "stream with tools",
		SessionID: "session-stream-tools",
	}

	resultChan, err := instrumentedAgent.Stream(ctx, input)
	assert.NoError(t, err)

	var lastOutput *agentcore.AgentOutput
	for chunk := range resultChan {
		if chunk.Data != nil {
			lastOutput = chunk.Data
		}
	}

	assert.NotNil(t, lastOutput)
	assert.Len(t, lastOutput.ToolCalls, 2)
	mockAgent.AssertExpectations(t)
}

func TestInstrumentedAgent_Batch_Success(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("batch-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()

	inputs := []*agentcore.AgentInput{
		{Task: "task 1", SessionID: "session-1"},
		{Task: "task 2", SessionID: "session-2"},
		{Task: "task 3", SessionID: "session-3"},
	}

	outputs := []*agentcore.AgentOutput{
		{Status: "completed", ToolCalls: []agentcore.ToolCall{}, ReasoningSteps: []agentcore.ReasoningStep{}},
		{Status: "completed", ToolCalls: []agentcore.ToolCall{}, ReasoningSteps: []agentcore.ReasoningStep{}},
		{Status: "completed", ToolCalls: []agentcore.ToolCall{}, ReasoningSteps: []agentcore.ReasoningStep{}},
	}

	mockAgent.On("Batch", mock.Anything, inputs).Return(outputs, nil)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	ctx := context.Background()
	results, err := instrumentedAgent.Batch(ctx, inputs)

	assert.NoError(t, err)
	assert.Len(t, results, 3)
	for _, result := range results {
		assert.Equal(t, "completed", result.Status)
	}
	mockAgent.AssertExpectations(t)
}

func TestInstrumentedAgent_Batch_WithError(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("batch-error-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()
	mockLogger.On("Error", mock.Anything, mock.Anything).Return()

	inputs := []*agentcore.AgentInput{
		{Task: "task 1", SessionID: "session-1"},
		{Task: "task 2", SessionID: "session-2"},
	}

	testErr := errors.New("batch execution failed")
	mockAgent.On("Batch", mock.Anything, inputs).Return([]*agentcore.AgentOutput{}, testErr)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	ctx := context.Background()
	results, err := instrumentedAgent.Batch(ctx, inputs)

	assert.Error(t, err)
	assert.Equal(t, testErr, err)
	assert.Empty(t, results)
	mockAgent.AssertExpectations(t)
}

func TestInstrumentedAgent_Batch_WithToolCalls(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("batch-tools-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()

	inputs := []*agentcore.AgentInput{
		{Task: "task 1", SessionID: "session-1"},
		{Task: "task 2", SessionID: "session-2"},
	}

	outputs := []*agentcore.AgentOutput{
		{
			Status: "completed",
			ToolCalls: []agentcore.ToolCall{
				{ToolName: "calculator", Success: true, Duration: 100 * time.Millisecond},
				{ToolName: "search", Success: true, Duration: 150 * time.Millisecond},
			},
			ReasoningSteps: []agentcore.ReasoningStep{},
		},
		{
			Status: "completed",
			ToolCalls: []agentcore.ToolCall{
				{ToolName: "validator", Success: false, Duration: 50 * time.Millisecond},
			},
			ReasoningSteps: []agentcore.ReasoningStep{},
		},
	}

	mockAgent.On("Batch", mock.Anything, inputs).Return(outputs, nil)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	ctx := context.Background()
	results, err := instrumentedAgent.Batch(ctx, inputs)

	assert.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Len(t, results[0].ToolCalls, 2)
	assert.Len(t, results[1].ToolCalls, 1)
	mockAgent.AssertExpectations(t)
}

func TestInstrumentedAgent_Batch_EmptyInputs(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("batch-empty-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()

	inputs := []*agentcore.AgentInput{}
	outputs := []*agentcore.AgentOutput{}

	mockAgent.On("Batch", mock.Anything, inputs).Return(outputs, nil)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	ctx := context.Background()
	results, err := instrumentedAgent.Batch(ctx, inputs)

	assert.NoError(t, err)
	assert.Empty(t, results)
	mockAgent.AssertExpectations(t)
}

func TestInstrumentedAgent_Batch_LargeBatch(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Name").Return("batch-large-agent")

	mockLogger := new(MockLogger)
	mockLogger.On("With", mock.Anything).Return(mockLogger)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()

	// Create a large batch
	batchSize := 100
	inputs := make([]*agentcore.AgentInput, batchSize)
	outputs := make([]*agentcore.AgentOutput, batchSize)

	for i := 0; i < batchSize; i++ {
		inputs[i] = &agentcore.AgentInput{
			Task:      "task",
			SessionID: "session",
		}
		outputs[i] = &agentcore.AgentOutput{
			Status:         "completed",
			ToolCalls:      []agentcore.ToolCall{},
			ReasoningSteps: []agentcore.ReasoningStep{},
		}
	}

	mockAgent.On("Batch", mock.Anything, inputs).Return(outputs, nil)

	instrumentedAgent := NewInstrumentedAgent(mockAgent, "test-service", mockLogger)

	ctx := context.Background()
	results, err := instrumentedAgent.Batch(ctx, inputs)

	assert.NoError(t, err)
	assert.Len(t, results, batchSize)
	mockAgent.AssertExpectations(t)
}
