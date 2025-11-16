package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
)

// MockAgent is a mock implementation of agentcore.Agent
type MockAgent struct {
	mock.Mock
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

// MockMemory is a mock implementation of Memory interface
type MockMemory struct {
	mock.Mock
}

func (m *MockMemory) SaveContext(ctx context.Context, sessionID string, input, output map[string]interface{}) error {
	args := m.Called(ctx, sessionID, input, output)
	return args.Error(0)
}

func (m *MockMemory) LoadHistory(ctx context.Context, sessionID string) ([]map[string]interface{}, error) {
	args := m.Called(ctx, sessionID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]map[string]interface{}), args.Error(1)
}

func (m *MockMemory) Clear(ctx context.Context, sessionID string) error {
	args := m.Called(ctx, sessionID)
	return args.Error(0)
}

// MockCallback is a mock callback for testing
type MockCallback struct {
	mock.Mock
}

func (m *MockCallback) OnStart(ctx context.Context, input interface{}) error {
	args := m.Called(ctx, input)
	return args.Error(0)
}

func (m *MockCallback) OnEnd(ctx context.Context, output interface{}) error {
	args := m.Called(ctx, output)
	return args.Error(0)
}

func (m *MockCallback) OnError(ctx context.Context, err error) error {
	args := m.Called(ctx, err)
	return args.Error(0)
}

func (m *MockCallback) OnLLMStart(ctx context.Context, prompts []string, model string) error {
	args := m.Called(ctx, prompts, model)
	return args.Error(0)
}

func (m *MockCallback) OnLLMEnd(ctx context.Context, output string, tokenUsage int) error {
	args := m.Called(ctx, output, tokenUsage)
	return args.Error(0)
}

func (m *MockCallback) OnLLMError(ctx context.Context, err error) error {
	args := m.Called(ctx, err)
	return args.Error(0)
}

func (m *MockCallback) OnChainStart(ctx context.Context, chainName string, input interface{}) error {
	args := m.Called(ctx, chainName, input)
	return args.Error(0)
}

func (m *MockCallback) OnChainEnd(ctx context.Context, chainName string, output interface{}) error {
	args := m.Called(ctx, chainName, output)
	return args.Error(0)
}

func (m *MockCallback) OnChainError(ctx context.Context, chainName string, err error) error {
	args := m.Called(ctx, chainName, err)
	return args.Error(0)
}

func (m *MockCallback) OnToolStart(ctx context.Context, toolName string, input interface{}) error {
	args := m.Called(ctx, toolName, input)
	return args.Error(0)
}

func (m *MockCallback) OnToolEnd(ctx context.Context, toolName string, output interface{}) error {
	args := m.Called(ctx, toolName, output)
	return args.Error(0)
}

func (m *MockCallback) OnToolError(ctx context.Context, toolName string, err error) error {
	args := m.Called(ctx, toolName, err)
	return args.Error(0)
}

func (m *MockCallback) OnAgentAction(ctx context.Context, action *agentcore.AgentAction) error {
	args := m.Called(ctx, action)
	return args.Error(0)
}

func (m *MockCallback) OnAgentFinish(ctx context.Context, output interface{}) error {
	args := m.Called(ctx, output)
	return args.Error(0)
}

// TestNewAgentExecutor tests the constructor with various configurations
func TestNewAgentExecutor(t *testing.T) {
	tests := []struct {
		name        string
		config      ExecutorConfig
		wantMaxIter int
		wantMaxTime time.Duration
		wantMethod  string
	}{
		{
			name: "default configuration",
			config: ExecutorConfig{
				Agent: &MockAgent{},
			},
			wantMaxIter: 15,
			wantMaxTime: 5 * time.Minute,
			wantMethod:  "force",
		},
		{
			name: "custom configuration",
			config: ExecutorConfig{
				Agent:               &MockAgent{},
				MaxIterations:       10,
				MaxExecutionTime:    2 * time.Minute,
				EarlyStoppingMethod: "generate",
			},
			wantMaxIter: 10,
			wantMaxTime: 2 * time.Minute,
			wantMethod:  "generate",
		},
		{
			name: "partial configuration",
			config: ExecutorConfig{
				Agent:         &MockAgent{},
				MaxIterations: 20,
			},
			wantMaxIter: 20,
			wantMaxTime: 5 * time.Minute,
			wantMethod:  "force",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewAgentExecutor(tt.config)

			assert.NotNil(t, executor)
			assert.Equal(t, tt.wantMaxIter, executor.maxIterations)
			assert.Equal(t, tt.wantMaxTime, executor.maxExecutionTime)
			assert.Equal(t, tt.wantMethod, executor.earlyStoppingMethod)
		})
	}
}

// TestRun tests the Run method
func TestRun(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		mockSetup   func(*MockAgent)
		wantResult  string
		wantError   bool
		errorSubstr string
	}{
		{
			name:  "successful execution with string result",
			input: "test task",
			mockSetup: func(m *MockAgent) {
				m.On("Invoke", mock.Anything, mock.Anything).Return(&agentcore.AgentOutput{
					Result: "test result",
					Status: "success",
				}, nil)
			},
			wantResult: "test result",
			wantError:  false,
		},
		{
			name:  "successful execution with non-string result",
			input: "test task",
			mockSetup: func(m *MockAgent) {
				m.On("Invoke", mock.Anything, mock.Anything).Return(&agentcore.AgentOutput{
					Result: map[string]string{"key": "value"},
					Status: "success",
				}, nil)
			},
			wantResult: "map[key:value]",
			wantError:  false,
		},
		{
			name:  "agent execution failure",
			input: "test task",
			mockSetup: func(m *MockAgent) {
				m.On("Invoke", mock.Anything, mock.Anything).Return(nil, errors.New("agent error"))
			},
			wantError:   true,
			errorSubstr: "agent error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAgent := new(MockAgent)
			tt.mockSetup(mockAgent)

			executor := NewAgentExecutor(ExecutorConfig{
				Agent: mockAgent,
			})

			result, err := executor.Run(context.Background(), tt.input)

			if tt.wantError {
				assert.Error(t, err)
				if tt.errorSubstr != "" {
					assert.Contains(t, err.Error(), tt.errorSubstr)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantResult, result)
			}

			mockAgent.AssertExpectations(t)
		})
	}
}

// TestExecute tests the Execute method
func TestExecute(t *testing.T) {
	tests := []struct {
		name       string
		input      *agentcore.AgentInput
		withMemory bool
		mockSetup  func(*MockAgent, *MockMemory)
		wantError  bool
		wantStatus string
	}{
		{
			name: "successful execution without memory",
			input: &agentcore.AgentInput{
				Task:      "test task",
				SessionID: "session1",
				Timestamp: time.Now(),
			},
			withMemory: false,
			mockSetup: func(ma *MockAgent, mm *MockMemory) {
				ma.On("Invoke", mock.Anything, mock.Anything).Return(&agentcore.AgentOutput{
					Result: "success",
					Status: "completed",
				}, nil)
			},
			wantError:  false,
			wantStatus: "completed",
		},
		{
			name: "successful execution with memory",
			input: &agentcore.AgentInput{
				Task:      "test task",
				SessionID: "session1",
				Timestamp: time.Now(),
			},
			withMemory: true,
			mockSetup: func(ma *MockAgent, mm *MockMemory) {
				mm.On("LoadHistory", mock.Anything, "session1").Return([]map[string]interface{}{
					{"role": "user", "content": "previous message"},
				}, nil)
				ma.On("Invoke", mock.Anything, mock.Anything).Return(&agentcore.AgentOutput{
					Result:   "success",
					Status:   "completed",
					Metadata: map[string]interface{}{},
				}, nil)
				mm.On("SaveContext", mock.Anything, "session1", mock.Anything, mock.Anything).Return(nil)
			},
			wantError:  false,
			wantStatus: "completed",
		},
		{
			name: "memory load failure",
			input: &agentcore.AgentInput{
				Task:      "test task",
				SessionID: "session1",
				Timestamp: time.Now(),
			},
			withMemory: true,
			mockSetup: func(ma *MockAgent, mm *MockMemory) {
				mm.On("LoadHistory", mock.Anything, "session1").Return(nil, errors.New("memory error"))
			},
			wantError: true,
		},
		{
			name: "max iterations exceeded with force method",
			input: &agentcore.AgentInput{
				Task:      "test task",
				SessionID: "session1",
				Timestamp: time.Now(),
			},
			withMemory: false,
			mockSetup: func(ma *MockAgent, mm *MockMemory) {
				// Simulate output with many reasoning steps (more than max iterations)
				steps := make([]agentcore.ReasoningStep, 20)
				for i := 0; i < 20; i++ {
					steps[i] = agentcore.ReasoningStep{Step: i, Action: "think"}
				}
				ma.On("Invoke", mock.Anything, mock.Anything).Return(&agentcore.AgentOutput{
					Result:         "partial result",
					Status:         "running",
					ReasoningSteps: steps,
				}, nil)
			},
			wantError:  false,
			wantStatus: "partial",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAgent := new(MockAgent)
			var mockMemory *MockMemory
			var mem Memory

			if tt.withMemory {
				mockMemory = new(MockMemory)
				mem = mockMemory
			}

			tt.mockSetup(mockAgent, mockMemory)

			config := ExecutorConfig{
				Agent:               mockAgent,
				Memory:              mem,
				MaxIterations:       15,
				EarlyStoppingMethod: "force",
			}
			executor := NewAgentExecutor(config)

			output, err := executor.Execute(context.Background(), tt.input)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, output)
				if tt.wantStatus != "" {
					assert.Equal(t, tt.wantStatus, output.Status)
				}
				assert.NotZero(t, output.Latency)
			}

			mockAgent.AssertExpectations(t)
			if mockMemory != nil {
				mockMemory.AssertExpectations(t)
			}
		})
	}
}

// TestExecuteWithTimeout tests execution timeout handling
func TestExecuteWithTimeout(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgent.On("Invoke", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		// Simulate slow execution
		time.Sleep(200 * time.Millisecond)
	}).Return(&agentcore.AgentOutput{
		Result: "timeout",
	}, nil)

	executor := NewAgentExecutor(ExecutorConfig{
		Agent:            mockAgent,
		MaxExecutionTime: 100 * time.Millisecond,
	})

	input := &agentcore.AgentInput{
		Task:      "test task",
		Timestamp: time.Now(),
	}

	output, err := executor.Execute(context.Background(), input)

	// Should complete but context might be done
	if err != nil {
		assert.Contains(t, err.Error(), "timeout")
	} else {
		assert.NotNil(t, output)
	}
}

// TestExecuteWithCallbacks tests callback integration
func TestExecuteWithCallbacks(t *testing.T) {
	mockAgent := new(MockAgent)
	mockAgentWithCallbacks := new(MockAgent)

	// Mock the original agent's behavior
	mockAgent.On("WithCallbacks", mock.Anything).Return(mockAgentWithCallbacks)

	// Mock the callback-enabled agent's behavior
	mockAgentWithCallbacks.On("Invoke", mock.Anything, mock.Anything).Return(&agentcore.AgentOutput{
		Result: "callback result",
		Status: "success",
	}, nil)

	executor := NewAgentExecutor(ExecutorConfig{
		Agent: mockAgent,
	})

	input := &agentcore.AgentInput{
		Task:      "test task",
		Timestamp: time.Now(),
	}

	// Create a simple callback
	callback := new(MockCallback)

	output, err := executor.ExecuteWithCallbacks(context.Background(), input, callback)

	assert.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, "callback result", output.Result)

	mockAgent.AssertExpectations(t)
	mockAgentWithCallbacks.AssertExpectations(t)
}

// TestStream tests the streaming execution
func TestStream(t *testing.T) {
	tests := []struct {
		name       string
		withMemory bool
		mockSetup  func(*MockAgent, *MockMemory)
		wantError  bool
	}{
		{
			name:       "successful stream without memory",
			withMemory: false,
			mockSetup: func(ma *MockAgent, mm *MockMemory) {
				ch := make(chan agentcore.StreamChunk[*agentcore.AgentOutput], 1)
				ch <- agentcore.StreamChunk[*agentcore.AgentOutput]{
					Data: &agentcore.AgentOutput{Result: "chunk1"},
					Done: true,
				}
				close(ch)
				ma.On("Stream", mock.Anything, mock.Anything).Return((<-chan agentcore.StreamChunk[*agentcore.AgentOutput])(ch), nil)
			},
			wantError: false,
		},
		{
			name:       "successful stream with memory",
			withMemory: true,
			mockSetup: func(ma *MockAgent, mm *MockMemory) {
				mm.On("LoadHistory", mock.Anything, mock.Anything).Return([]map[string]interface{}{
					{"role": "user", "content": "history"},
				}, nil)
				ch := make(chan agentcore.StreamChunk[*agentcore.AgentOutput], 1)
				ch <- agentcore.StreamChunk[*agentcore.AgentOutput]{
					Data: &agentcore.AgentOutput{Result: "chunk1"},
					Done: true,
				}
				close(ch)
				ma.On("Stream", mock.Anything, mock.Anything).Return((<-chan agentcore.StreamChunk[*agentcore.AgentOutput])(ch), nil)
			},
			wantError: false,
		},
		{
			name:       "memory load error during stream",
			withMemory: true,
			mockSetup: func(ma *MockAgent, mm *MockMemory) {
				mm.On("LoadHistory", mock.Anything, mock.Anything).Return(nil, errors.New("memory error"))
			},
			wantError: false, // Returns channel with error, not error from function
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAgent := new(MockAgent)
			var mockMemory *MockMemory
			var mem Memory

			if tt.withMemory {
				mockMemory = new(MockMemory)
				mem = mockMemory
			}

			tt.mockSetup(mockAgent, mockMemory)

			executor := NewAgentExecutor(ExecutorConfig{
				Agent:  mockAgent,
				Memory: mem,
			})

			input := &agentcore.AgentInput{
				Task:      "test task",
				SessionID: "session1",
				Timestamp: time.Now(),
			}

			resultChan, err := executor.Stream(context.Background(), input)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resultChan)

				// Read from channel
				chunk, ok := <-resultChan
				if ok {
					// Either we got data or an error
					assert.True(t, chunk.Data != nil || chunk.Error != nil)
				}
			}

			mockAgent.AssertExpectations(t)
			if mockMemory != nil {
				mockMemory.AssertExpectations(t)
			}
		})
	}
}

// TestBatch tests batch execution
func TestBatch(t *testing.T) {
	tests := []struct {
		name      string
		inputs    []*agentcore.AgentInput
		mockSetup func(*MockAgent)
		wantError bool
		wantCount int
	}{
		{
			name: "successful batch execution",
			inputs: []*agentcore.AgentInput{
				{Task: "task1", Timestamp: time.Now()},
				{Task: "task2", Timestamp: time.Now()},
				{Task: "task3", Timestamp: time.Now()},
			},
			mockSetup: func(m *MockAgent) {
				m.On("Invoke", mock.Anything, mock.Anything).Return(&agentcore.AgentOutput{
					Result: "success",
					Status: "completed",
				}, nil).Times(3)
			},
			wantError: false,
			wantCount: 3,
		},
		{
			name: "batch with one failure",
			inputs: []*agentcore.AgentInput{
				{Task: "task1", Timestamp: time.Now()},
				{Task: "task2", Timestamp: time.Now()},
			},
			mockSetup: func(m *MockAgent) {
				m.On("Invoke", mock.Anything, mock.Anything).Return(&agentcore.AgentOutput{
					Result: "success",
				}, nil).Once()
				m.On("Invoke", mock.Anything, mock.Anything).Return(nil, errors.New("task failed")).Once()
			},
			wantError: true,
		},
		{
			name:      "empty batch",
			inputs:    []*agentcore.AgentInput{},
			mockSetup: func(m *MockAgent) {},
			wantError: false,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAgent := new(MockAgent)
			tt.mockSetup(mockAgent)

			executor := NewAgentExecutor(ExecutorConfig{
				Agent: mockAgent,
			})

			outputs, err := executor.Batch(context.Background(), tt.inputs)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, outputs, tt.wantCount)
			}

			mockAgent.AssertExpectations(t)
		})
	}
}

// TestGettersAndSetters tests the getter and setter methods
func TestGettersAndSetters(t *testing.T) {
	mockAgent := new(MockAgent)
	mockMemory := new(MockMemory)

	executor := NewAgentExecutor(ExecutorConfig{
		Agent:  mockAgent,
		Memory: mockMemory,
		Tools:  []interfaces.Tool{},
	})

	// Test GetTools
	toolsList := executor.GetTools()
	assert.Len(t, toolsList, 0)

	// Test GetMemory
	mem := executor.GetMemory()
	assert.Equal(t, mockMemory, mem)

	// Test SetMemory
	newMockMemory := new(MockMemory)
	executor.SetMemory(newMockMemory)
	assert.Equal(t, newMockMemory, executor.GetMemory())

	// Test SetVerbose
	executor.SetVerbose(true)
	assert.True(t, executor.verbose)

	executor.SetVerbose(false)
	assert.False(t, executor.verbose)
}

// TestConversationChain tests the ConversationChain
func TestConversationChain(t *testing.T) {
	t.Run("create conversation chain", func(t *testing.T) {
		mockAgent := new(MockAgent)
		mockMemory := new(MockMemory)

		chain := NewConversationChain(mockAgent, mockMemory)

		assert.NotNil(t, chain)
		assert.NotNil(t, chain.AgentExecutor)
		assert.Equal(t, mockMemory, chain.conversationMemory)
	})

	t.Run("chat with successful response", func(t *testing.T) {
		mockAgent := new(MockAgent)
		mockMemory := new(MockMemory)

		mockMemory.On("LoadHistory", mock.Anything, "session1").Return([]map[string]interface{}{}, nil)
		mockAgent.On("Invoke", mock.Anything, mock.Anything).Return(&agentcore.AgentOutput{
			Result:   "Hello! How can I help you?",
			Metadata: map[string]interface{}{},
		}, nil)
		mockMemory.On("SaveContext", mock.Anything, "session1", mock.Anything, mock.Anything).Return(nil)

		chain := NewConversationChain(mockAgent, mockMemory)

		response, err := chain.Chat(context.Background(), "Hello", "session1")

		assert.NoError(t, err)
		assert.Equal(t, "Hello! How can I help you?", response)

		mockAgent.AssertExpectations(t)
		mockMemory.AssertExpectations(t)
	})

	t.Run("clear memory", func(t *testing.T) {
		mockAgent := new(MockAgent)
		mockMemory := new(MockMemory)

		mockMemory.On("Clear", mock.Anything, "session1").Return(nil)

		chain := NewConversationChain(mockAgent, mockMemory)

		err := chain.ClearMemory(context.Background(), "session1")

		assert.NoError(t, err)
		mockMemory.AssertExpectations(t)
	})

	t.Run("get history", func(t *testing.T) {
		mockAgent := new(MockAgent)
		mockMemory := new(MockMemory)

		expectedHistory := []map[string]interface{}{
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi there!"},
		}

		mockMemory.On("LoadHistory", mock.Anything, "session1").Return(expectedHistory, nil)

		chain := NewConversationChain(mockAgent, mockMemory)

		history, err := chain.GetHistory(context.Background(), "session1")

		assert.NoError(t, err)
		assert.Equal(t, expectedHistory, history)
		mockMemory.AssertExpectations(t)
	})

	t.Run("chat with nil memory", func(t *testing.T) {
		mockAgent := new(MockAgent)

		mockAgent.On("Invoke", mock.Anything, mock.Anything).Return(&agentcore.AgentOutput{
			Result: "Response without memory",
		}, nil)

		chain := NewConversationChain(mockAgent, nil)
		chain.conversationMemory = nil

		response, err := chain.Chat(context.Background(), "Hello", "session1")

		assert.NoError(t, err)
		assert.Equal(t, "Response without memory", response)

		mockAgent.AssertExpectations(t)
	})

	t.Run("clear memory with nil memory", func(t *testing.T) {
		mockAgent := new(MockAgent)

		chain := NewConversationChain(mockAgent, nil)
		chain.conversationMemory = nil

		err := chain.ClearMemory(context.Background(), "session1")

		assert.NoError(t, err)
	})

	t.Run("get history with nil memory", func(t *testing.T) {
		mockAgent := new(MockAgent)

		chain := NewConversationChain(mockAgent, nil)
		chain.conversationMemory = nil

		history, err := chain.GetHistory(context.Background(), "session1")

		assert.NoError(t, err)
		assert.Nil(t, history)
	})
}

// TestMemorySaveFailureHandling tests graceful handling of memory save failures
func TestMemorySaveFailureHandling(t *testing.T) {
	mockAgent := new(MockAgent)
	mockMemory := new(MockMemory)

	mockMemory.On("LoadHistory", mock.Anything, "session1").Return([]map[string]interface{}{}, nil)
	mockAgent.On("Invoke", mock.Anything, mock.Anything).Return(&agentcore.AgentOutput{
		Result:   "success",
		Metadata: map[string]interface{}{},
	}, nil)
	// Memory save fails but execution should continue
	mockMemory.On("SaveContext", mock.Anything, "session1", mock.Anything, mock.Anything).Return(errors.New("save failed"))

	executor := NewAgentExecutor(ExecutorConfig{
		Agent:   mockAgent,
		Memory:  mockMemory,
		Verbose: true, // Enable verbose to test warning path
	})

	input := &agentcore.AgentInput{
		Task:      "test task",
		SessionID: "session1",
		Timestamp: time.Now(),
	}

	// Execution should succeed despite memory save failure
	output, err := executor.Execute(context.Background(), input)

	assert.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, "success", output.Result)

	mockAgent.AssertExpectations(t)
	mockMemory.AssertExpectations(t)
}

// TestEarlyStoppingMethods tests different early stopping strategies
func TestEarlyStoppingMethods(t *testing.T) {
	tests := []struct {
		name   string
		method string
		steps  int
		want   string
	}{
		{
			name:   "force method - partial status",
			method: "force",
			steps:  20,
			want:   "partial",
		},
		{
			name:   "generate method - final answer",
			method: "generate",
			steps:  20,
			want:   "running", // Status remains as returned from agent
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAgent := new(MockAgent)

			steps := make([]agentcore.ReasoningStep, tt.steps)
			for i := 0; i < tt.steps; i++ {
				steps[i] = agentcore.ReasoningStep{Step: i, Action: "think"}
			}

			mockAgent.On("Invoke", mock.Anything, mock.Anything).Return(&agentcore.AgentOutput{
				Result:         "result",
				Status:         "running",
				ReasoningSteps: steps,
			}, nil)

			executor := NewAgentExecutor(ExecutorConfig{
				Agent:               mockAgent,
				MaxIterations:       15,
				EarlyStoppingMethod: tt.method,
			})

			input := &agentcore.AgentInput{
				Task:      "test",
				Timestamp: time.Now(),
			}

			output, err := executor.Execute(context.Background(), input)

			assert.NoError(t, err)
			assert.Equal(t, tt.want, output.Status)

			mockAgent.AssertExpectations(t)
		})
	}
}
