package providers

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

// MockTool for testing
type MockTool struct {
	mock.Mock
}

func (m *MockTool) Name() string {
	return "mock_tool"
}

func (m *MockTool) Description() string {
	return "A mock tool for testing"
}

func (m *MockTool) ArgsSchema() string {
	return `{
		"type": "object",
		"properties": {
			"input": {
				"type": "string"
			}
		}
	}`
}

func (m *MockTool) Execute(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interfaces.ToolOutput), args.Error(1)
}

// Implement Runnable interface
func (m *MockTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return m.Execute(ctx, input)
}

func (m *MockTool) Stream(ctx context.Context, input *interfaces.ToolInput) (<-chan agentcore.StreamChunk[*interfaces.ToolOutput], error) {
	ch := make(chan agentcore.StreamChunk[*interfaces.ToolOutput])
	go func() {
		defer close(ch)
		output, err := m.Execute(ctx, input)
		if err != nil {
			ch <- agentcore.StreamChunk[*interfaces.ToolOutput]{Error: err}
		} else {
			ch <- agentcore.StreamChunk[*interfaces.ToolOutput]{Data: output}
		}
	}()
	return ch, nil
}

func (m *MockTool) Batch(ctx context.Context, inputs []*interfaces.ToolInput) ([]*interfaces.ToolOutput, error) {
	outputs := make([]*interfaces.ToolOutput, len(inputs))
	for i, input := range inputs {
		output, err := m.Execute(ctx, input)
		if err != nil {
			return nil, err
		}
		outputs[i] = output
	}
	return outputs, nil
}

func (m *MockTool) Pipe(next agentcore.Runnable[*interfaces.ToolOutput, any]) agentcore.Runnable[*interfaces.ToolInput, any] {
	return nil
}

func (m *MockTool) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return m
}

func (m *MockTool) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return m
}

// TestOpenAIProvider tests
func TestOpenAIProvider_Creation(t *testing.T) {
	tests := []struct {
		name    string
		config  *llm.LLMOptions
		wantErr bool
	}{
		{
			name: "valid config",
			config: &llm.LLMOptions{
				Provider:    constants.ProviderOpenAI,
				APIKey:      "test-key",
				Model:       "gpt-4",
				MaxTokens:   2000,
				Temperature: 0.7,
			},
			wantErr: false,
		},
		{
			name: "missing API key",
			config: &llm.LLMOptions{
				Provider: constants.ProviderOpenAI,
			},
			wantErr: true,
		},
		{
			name: "with custom base URL",
			config: &llm.LLMOptions{
				Provider: constants.ProviderOpenAI,
				APIKey:   "test-key",
				BaseURL:  "https://custom.openai.com",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewOpenAI(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)
				assert.Equal(t, constants.ProviderOpenAI, provider.Provider())
			}
		})
	}
}

func TestOpenAIProvider_Complete(t *testing.T) {
	// This test would require mocking the OpenAI client
	// For unit testing, we'll skip actual API calls
	t.Skip("Skipping OpenAI Complete test - requires API mock")
}

func TestOpenAIProvider_Stream(t *testing.T) {
	// This test would require mocking the OpenAI streaming client
	t.Skip("Skipping OpenAI Stream test - requires API mock")
}

func TestOpenAIProvider_ConvertToolsToFunctions(t *testing.T) {
	provider := &OpenAIProvider{
		model:       "gpt-4",
		maxTokens:   2000,
		temperature: 0.7,
	}

	mockTool := &MockTool{}
	tools := []interfaces.Tool{mockTool}

	functions := provider.convertToolsToFunctions(tools)

	assert.Len(t, functions, 1)
	assert.Equal(t, "mock_tool", functions[0].Name)
	assert.Equal(t, "A mock tool for testing", functions[0].Description)
	assert.NotNil(t, functions[0].Parameters)
}

// TestGeminiProvider tests
func TestGeminiProvider_Creation(t *testing.T) {
	tests := []struct {
		name    string
		config  *llm.LLMOptions
		wantErr bool
	}{
		{
			name: "valid config",
			config: &llm.LLMOptions{
				Provider:    constants.ProviderGemini,
				APIKey:      "test-key",
				Model:       "gemini-pro",
				MaxTokens:   2000,
				Temperature: 0.7,
			},
			wantErr: false,
		},
		{
			name: "missing API key",
			config: &llm.LLMOptions{
				Provider: constants.ProviderGemini,
			},
			wantErr: true,
		},
		{
			name: "default model",
			config: &llm.LLMOptions{
				Provider: constants.ProviderGemini,
				APIKey:   "test-key",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "valid config" || tt.name == "default model" {
				t.Skip("Skipping Gemini creation test - requires valid API setup")
			}

			provider, err := NewGemini(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)
				assert.Equal(t, constants.ProviderGemini, provider.Provider())
			}
		})
	}
}

// TestToolCallResponse tests
func TestToolCallResponse(t *testing.T) {
	response := &ToolCallResponse{
		Content: "Here's the weather information",
		ToolCalls: []ToolCall{
			{
				ID:   "call_123",
				Name: "get_weather",
				Arguments: map[string]interface{}{
					"location": "New York",
				},
			},
		},
	}

	assert.Equal(t, "Here's the weather information", response.Content)
	assert.Len(t, response.ToolCalls, 1)
	assert.Equal(t, "get_weather", response.ToolCalls[0].Name)
	assert.Equal(t, "New York", response.ToolCalls[0].Arguments["location"])
}

// TestToolChunk tests
func TestToolChunk(t *testing.T) {
	chunks := []ToolChunk{
		{
			Type:  "content",
			Value: "Processing...",
		},
		{
			Type:  "tool_name",
			Value: "calculator",
		},
		{
			Type: "tool_args",
			Value: map[string]interface{}{
				"expression": "2+2",
			},
		},
		{
			Type: "tool_call",
			Value: ToolCall{
				ID:   "call_456",
				Name: "calculator",
				Arguments: map[string]interface{}{
					"expression": "2+2",
				},
			},
		},
	}

	assert.Equal(t, "content", chunks[0].Type)
	assert.Equal(t, "Processing...", chunks[0].Value)

	assert.Equal(t, "tool_name", chunks[1].Type)
	assert.Equal(t, "calculator", chunks[1].Value)

	assert.Equal(t, "tool_args", chunks[2].Type)
	args := chunks[2].Value.(map[string]interface{})
	assert.Equal(t, "2+2", args["expression"])

	assert.Equal(t, "tool_call", chunks[3].Type)
	call := chunks[3].Value.(ToolCall)
	assert.Equal(t, "calculator", call.Name)
}

// TestStreamingProvider tests
func TestOpenAIStreamingProvider_Creation(t *testing.T) {
	config := &llm.LLMOptions{
		Provider:    constants.ProviderOpenAI,
		APIKey:      "test-key",
		Model:       "gpt-4",
		MaxTokens:   2000,
		Temperature: 0.7,
	}

	provider, err := NewOpenAIStreaming(config)
	assert.NoError(t, err)
	assert.NotNil(t, provider)
	assert.NotNil(t, provider.OpenAIProvider)
}

func TestTokenWithMetadata(t *testing.T) {
	tokens := []TokenWithMetadata{
		{
			Type:    "token",
			Content: "Hello",
			Metadata: map[string]interface{}{
				"index": 1,
			},
		},
		{
			Type:    "token",
			Content: " world",
			Metadata: map[string]interface{}{
				"index": 2,
			},
		},
		{
			Type: "finish",
			Metadata: map[string]interface{}{
				"total_tokens": 2,
			},
		},
	}

	assert.Equal(t, "token", tokens[0].Type)
	assert.Equal(t, "Hello", tokens[0].Content)
	assert.Equal(t, 1, tokens[0].Metadata["index"])

	assert.Equal(t, "finish", tokens[2].Type)
	assert.Equal(t, 2, tokens[2].Metadata["total_tokens"])
}

// Benchmark tests
func BenchmarkOpenAIProvider_ConvertTools(b *testing.B) {
	provider := &OpenAIProvider{
		model:       "gpt-4",
		maxTokens:   2000,
		temperature: 0.7,
	}

	mockTools := make([]interfaces.Tool, 10)
	for i := 0; i < 10; i++ {
		mockTools[i] = &MockTool{}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = provider.convertToolsToFunctions(mockTools)
	}
}

func BenchmarkGenerateCallID(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = generateCallID()
	}
}

// TestGeminiStreamingProvider tests
func TestGeminiStreamingProvider_Creation(t *testing.T) {
	t.Skip("Skipping Gemini streaming test - requires valid API setup")

	config := &llm.LLMOptions{
		Provider:    constants.ProviderGemini,
		APIKey:      "test-key",
		Model:       "gemini-pro",
		MaxTokens:   2000,
		Temperature: 0.7,
	}

	provider, err := NewGeminiStreaming(config)
	assert.NoError(t, err)
	assert.NotNil(t, provider)
	assert.NotNil(t, provider.GeminiProvider)
}

func TestStreamEvent(t *testing.T) {
	event := StreamEvent{
		Type:      "token",
		Content:   "test",
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"index": 1,
		},
	}

	assert.Equal(t, "token", event.Type)
	assert.Equal(t, "test", event.Content)
	assert.Nil(t, event.Error)
	assert.NotNil(t, event.Metadata)
	assert.Equal(t, 1, event.Metadata["index"])
}

// Integration test example (would require actual API keys)
func TestOpenAIProvider_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	// This would require actual API key
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	config := &llm.LLMOptions{
		Provider:    constants.ProviderOpenAI,
		APIKey:      apiKey,
		Model:       "gpt-3.5-turbo",
		MaxTokens:   100,
		Temperature: 0.7,
	}

	provider, err := NewOpenAI(config)
	assert.NoError(t, err)

	ctx := context.Background()
	req := &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.UserMessage("What is 2+2?"),
		},
		MaxTokens: 50,
	}

	resp, err := provider.Complete(ctx, req)
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.Content)
	assert.Contains(t, resp.Content, "4")
}
