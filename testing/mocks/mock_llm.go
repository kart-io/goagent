package mocks

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

var (
	_ llm.ToolStreamClient = (*MockLLMClient)(nil)
	_ llm.ToolStreamClient = (*MockStreamingLLMClient)(nil)
)

// MockLLMClient provides a mock implementation of the LLM client for testing
type MockLLMClient struct {
	mu                sync.Mutex
	responses         []llm.CompletionResponse
	currentIndex      int
	shouldError       bool
	errorMessage      string
	requestHistory    []llm.CompletionRequest
	functionCallsMode bool
	functionResponses map[string]interface{}
}

// NewMockLLMClient creates a new mock LLM client
func NewMockLLMClient() *MockLLMClient {
	return &MockLLMClient{
		responses:         []llm.CompletionResponse{},
		functionResponses: make(map[string]interface{}),
		requestHistory:    []llm.CompletionRequest{},
	}
}

// SetResponses sets the predefined responses to return
func (m *MockLLMClient) SetResponses(responses ...llm.CompletionResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses = responses
	m.currentIndex = 0
}

// SetError configures the client to return an error
func (m *MockLLMClient) SetError(shouldError bool, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldError = shouldError
	m.errorMessage = message
}

// SetFunctionCallMode enables function calling mode
func (m *MockLLMClient) SetFunctionCallMode(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.functionCallsMode = enabled
}

// SetFunctionResponse sets a response for a specific function
func (m *MockLLMClient) SetFunctionResponse(functionName string, response interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.functionResponses[functionName] = response
}

// Complete implements the LLM completion method
func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Store request for inspection
	m.requestHistory = append(m.requestHistory, *req)

	// Check if error mode
	if m.shouldError {
		return nil, fmt.Errorf("%s", m.errorMessage)
	}

	// If in function call mode, generate function call response
	if m.functionCallsMode {
		// Look for tool-related keywords in the messages
		for _, msg := range req.Messages {
			if strings.Contains(msg.Content, "calculate") {
				return &llm.CompletionResponse{
					Content:    "Function call: calculator(expression='2 + 2')",
					Model:      "mock-model",
					TokensUsed: 10,
				}, nil
			}
			if strings.Contains(msg.Content, "search") {
				return &llm.CompletionResponse{
					Content:    "Function call: search(query='test query')",
					Model:      "mock-model",
					TokensUsed: 8,
				}, nil
			}
		}
	}

	// Return predefined response if available
	if m.currentIndex < len(m.responses) {
		response := m.responses[m.currentIndex]
		m.currentIndex++
		return &response, nil
	}

	// Default response
	return &llm.CompletionResponse{
		Content:    "This is a mock response",
		Model:      "mock-model",
		TokensUsed: 5,
	}, nil
}

// Chat implements the chat method
func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return m.Complete(ctx, &llm.CompletionRequest{
		Messages: messages,
	})
}

// CompleteStream 把 Complete 的结果作为一个增量块和一个终止块返回，供 Builder 等流式入口使用。
func (m *MockLLMClient) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	response, err := m.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	return streamChunks(ctx, []string{response.Content}, response.TokensUsed), nil
}

// ChatStream implements the streaming chat method
func (m *MockLLMClient) ChatStream(ctx context.Context, messages []llm.Message) (<-chan *llm.StreamChunk, error) {
	return m.CompleteStream(ctx, &llm.CompletionRequest{Messages: messages})
}

// CompleteToolsStream 不发起工具调用，直接给出回答。
func (m *MockLLMClient) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, _ []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	return m.CompleteStream(ctx, req)
}

// Provider returns the provider type
func (m *MockLLMClient) Provider() constants.Provider {
	return constants.ProviderCustom
}

// IsAvailable returns whether the client is available
func (m *MockLLMClient) IsAvailable() bool {
	return !m.shouldError
}

// GetRequestHistory returns the history of requests made
func (m *MockLLMClient) GetRequestHistory() []llm.CompletionRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requestHistory
}

// Reset resets the mock client state
func (m *MockLLMClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses = []llm.CompletionResponse{}
	m.currentIndex = 0
	m.shouldError = false
	m.errorMessage = ""
	m.requestHistory = []llm.CompletionRequest{}
	m.functionCallsMode = false
	m.functionResponses = make(map[string]interface{})
}

// MockStreamingLLMClient provides streaming capabilities
type MockStreamingLLMClient struct {
	*MockLLMClient
	streamChunks []string
	streamDelay  int // milliseconds between chunks
}

// NewMockStreamingLLMClient creates a new streaming mock client
func NewMockStreamingLLMClient() *MockStreamingLLMClient {
	return &MockStreamingLLMClient{
		MockLLMClient: NewMockLLMClient(),
		streamChunks:  []string{},
	}
}

// SetStreamChunks sets the chunks to stream
func (m *MockStreamingLLMClient) SetStreamChunks(chunks ...string) {
	m.streamChunks = chunks
}

// CompleteStream 依次返回 SetStreamChunks 设置的增量，最后一个块为终止块。
func (m *MockStreamingLLMClient) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	m.mu.Lock()
	m.requestHistory = append(m.requestHistory, *req)
	shouldError, message := m.shouldError, m.errorMessage
	m.mu.Unlock()
	if shouldError {
		return nil, fmt.Errorf("%s", message)
	}
	return streamChunks(ctx, m.streamChunks, 0), nil
}

// ChatStream implements the streaming chat method
func (m *MockStreamingLLMClient) ChatStream(ctx context.Context, messages []llm.Message) (<-chan *llm.StreamChunk, error) {
	return m.CompleteStream(ctx, &llm.CompletionRequest{Messages: messages})
}

// CompleteToolsStream 不发起工具调用，直接流式回答。
func (m *MockStreamingLLMClient) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, _ []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	return m.CompleteStream(ctx, req)
}

// streamChunks 逐个发送增量，最后发送带完整内容的成功终止块。
func streamChunks(ctx context.Context, deltas []string, totalTokens int) <-chan *llm.StreamChunk {
	ch := make(chan *llm.StreamChunk)
	go func() {
		defer close(ch)
		var content strings.Builder
		for i, delta := range deltas {
			content.WriteString(delta)
			select {
			case <-ctx.Done():
				return
			case ch <- &llm.StreamChunk{Delta: delta, Content: content.String(), Index: i}:
			}
		}
		select {
		case <-ctx.Done():
		case ch <- &llm.StreamChunk{Content: content.String(), Index: len(deltas), FinishReason: "stop", Done: true, Usage: &llm.Usage{TotalTokens: totalTokens}}:
		}
	}()
	return ch
}
