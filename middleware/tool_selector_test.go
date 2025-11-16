package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

// MockLLMClient for testing
type MockLLMClient struct {
	Response string
	Error    error
}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.Error != nil {
		return nil, m.Error
	}
	return &llm.CompletionResponse{
		Content:    m.Response,
		Model:      "mock-model",
		TokensUsed: 10,
	}, nil
}

func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return m.Complete(ctx, &llm.CompletionRequest{Messages: messages})
}

func (m *MockLLMClient) Provider() llm.Provider {
	return llm.ProviderCustom
}

func (m *MockLLMClient) IsAvailable() bool {
	return true
}

// MockTool for testing
type MockTool struct {
	*tools.BaseTool
}

func NewMockTool(name, description string) *MockTool {
	runFunc := func(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
		return &tools.ToolOutput{
			Result:  map[string]interface{}{"result": "ok"},
			Success: true,
		}, nil
	}

	return &MockTool{
		BaseTool: tools.NewBaseTool(name, description, "{}", runFunc),
	}
}

func TestNewLLMToolSelectorMiddleware(t *testing.T) {
	mockLLM := &MockLLMClient{Response: "tool1, tool2"}
	middleware := NewLLMToolSelectorMiddleware(mockLLM, 5)

	assert.NotNil(t, middleware)
	assert.Equal(t, "llm-tool-selector", middleware.Name())
	assert.Equal(t, 5, middleware.MaxTools)
	assert.Equal(t, mockLLM, middleware.Model)
	assert.NotNil(t, middleware.SelectionCache)
	assert.Equal(t, 5*time.Minute, middleware.CacheTTL)
}

func TestLLMToolSelectorMiddleware_WithAlwaysInclude(t *testing.T) {
	mockLLM := &MockLLMClient{Response: "tool1"}
	middleware := NewLLMToolSelectorMiddleware(mockLLM, 5)

	middleware.WithAlwaysInclude("critical_tool", "safety_tool")

	assert.Len(t, middleware.AlwaysInclude, 2)
	assert.Contains(t, middleware.AlwaysInclude, "critical_tool")
	assert.Contains(t, middleware.AlwaysInclude, "safety_tool")
}

func TestLLMToolSelectorMiddleware_Process_NoTools(t *testing.T) {
	mockLLM := &MockLLMClient{Response: "tool1"}
	middleware := NewLLMToolSelectorMiddleware(mockLLM, 5)

	state := core.NewAgentState()
	state.Set("query", "test query")

	resultState, err := middleware.Process(context.Background(), state)

	assert.NoError(t, err)
	assert.NotNil(t, resultState)
}

func TestLLMToolSelectorMiddleware_Process_NoQuery(t *testing.T) {
	mockLLM := &MockLLMClient{Response: "tool1"}
	middleware := NewLLMToolSelectorMiddleware(mockLLM, 5)

	state := core.NewAgentState()
	allTools := []tools.Tool{
		NewMockTool("tool1", "Tool 1 description"),
		NewMockTool("tool2", "Tool 2 description"),
	}
	state.Set("tools", allTools)

	resultState, err := middleware.Process(context.Background(), state)

	assert.NoError(t, err)
	assert.NotNil(t, resultState)
}

func TestLLMToolSelectorMiddleware_Process_Success(t *testing.T) {
	// LLM will return "tool1, tool3"
	mockLLM := &MockLLMClient{Response: "tool1, tool3"}
	middleware := NewLLMToolSelectorMiddleware(mockLLM, 5)

	state := core.NewAgentState()
	allTools := []tools.Tool{
		NewMockTool("tool1", "Tool 1 description"),
		NewMockTool("tool2", "Tool 2 description"),
		NewMockTool("tool3", "Tool 3 description"),
		NewMockTool("tool4", "Tool 4 description"),
	}
	state.Set("tools", allTools)
	state.Set("query", "Use tool1 and tool3")

	resultState, err := middleware.Process(context.Background(), state)

	require.NoError(t, err)

	// Check selected tools
	toolsVal, ok := resultState.Get("tools")
	require.True(t, ok)
	selectedTools, ok := toolsVal.([]tools.Tool)
	require.True(t, ok)
	assert.Len(t, selectedTools, 2)

	// Check metadata
	metadataVal, ok := resultState.Get("tool_selection_metadata")
	require.True(t, ok)
	metadata := metadataVal.(map[string]interface{})
	assert.Equal(t, 4, metadata["original_count"])
	assert.Equal(t, 2, metadata["selected_count"])
}

func TestLLMToolSelectorMiddleware_Process_WithAlwaysInclude(t *testing.T) {
	// LLM will return only "tool1"
	mockLLM := &MockLLMClient{Response: "tool1"}
	middleware := NewLLMToolSelectorMiddleware(mockLLM, 5)
	middleware.WithAlwaysInclude("tool_critical")

	state := core.NewAgentState()
	allTools := []tools.Tool{
		NewMockTool("tool1", "Tool 1"),
		NewMockTool("tool2", "Tool 2"),
		NewMockTool("tool_critical", "Critical tool"),
	}
	state.Set("tools", allTools)
	state.Set("query", "Use tool1")

	resultState, err := middleware.Process(context.Background(), state)

	require.NoError(t, err)

	toolsVal, ok := resultState.Get("tools")
	require.True(t, ok)
	selectedTools, ok := toolsVal.([]tools.Tool)
	require.True(t, ok)

	// Should have tool1 + tool_critical
	assert.Len(t, selectedTools, 2)

	names := make(map[string]bool)
	for _, tool := range selectedTools {
		names[tool.Name()] = true
	}
	assert.True(t, names["tool1"])
	assert.True(t, names["tool_critical"])
}

func TestLLMToolSelectorMiddleware_Process_Caching(t *testing.T) {
	mockLLM := &MockLLMClient{Response: "tool1, tool2"}
	middleware := NewLLMToolSelectorMiddleware(mockLLM, 5)

	state := core.NewAgentState()
	allTools := []tools.Tool{
		NewMockTool("tool1", "Tool 1"),
		NewMockTool("tool2", "Tool 2"),
		NewMockTool("tool3", "Tool 3"),
	}
	state.Set("tools", allTools)
	state.Set("query", "test query")

	// First call - should call LLM
	resultState1, err := middleware.Process(context.Background(), state)
	require.NoError(t, err)

	// Second call with same query - should use cache
	state2 := core.NewAgentState()
	state2.Set("tools", allTools)
	state2.Set("query", "test query")

	resultState2, err := middleware.Process(context.Background(), state2)
	require.NoError(t, err)

	// Both should have the same result
	tools1Val, _ := resultState1.Get("tools")
	tools2Val, _ := resultState2.Get("tools")
	tools1 := tools1Val.([]tools.Tool)
	tools2 := tools2Val.([]tools.Tool)

	assert.Equal(t, len(tools1), len(tools2))
}

func TestLLMToolSelectorMiddleware_ParseToolSelection(t *testing.T) {
	mockLLM := &MockLLMClient{}
	middleware := NewLLMToolSelectorMiddleware(mockLLM, 5)

	tests := []struct {
		name     string
		response string
		expected []string
	}{
		{
			name:     "comma separated",
			response: "tool1, tool2, tool3",
			expected: []string{"tool1", "tool2", "tool3"},
		},
		{
			name:     "with quotes",
			response: `"tool1", "tool2"`,
			expected: []string{"tool1", "tool2"},
		},
		{
			name:     "with brackets",
			response: "[tool1, tool2]",
			expected: []string{"tool1", "tool2"},
		},
		{
			name:     "with extra spaces",
			response: "  tool1  ,  tool2  ",
			expected: []string{"tool1", "tool2"},
		},
		{
			name:     "single tool",
			response: "tool1",
			expected: []string{"tool1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := middleware.parseToolSelection(tt.response)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLLMToolSelectorMiddleware_EnsureAlwaysIncluded(t *testing.T) {
	mockLLM := &MockLLMClient{}
	middleware := NewLLMToolSelectorMiddleware(mockLLM, 3)
	middleware.WithAlwaysInclude("critical_tool")

	tests := []struct {
		name     string
		selected []string
		expected []string
	}{
		{
			name:     "critical tool not in list",
			selected: []string{"tool1", "tool2"},
			expected: []string{"tool1", "tool2", "critical_tool"},
		},
		{
			name:     "critical tool already in list",
			selected: []string{"tool1", "critical_tool"},
			expected: []string{"tool1", "critical_tool"},
		},
		{
			name:     "exceeds max tools",
			selected: []string{"tool1", "tool2", "tool3", "tool4"},
			expected: []string{"tool1", "tool2", "tool3"}, // Trimmed to MaxTools
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := middleware.ensureAlwaysIncluded(tt.selected)
			assert.Len(t, result, len(tt.expected))
			for _, expected := range tt.expected {
				assert.Contains(t, result, expected)
			}
		})
	}
}

func TestLLMToolSelectorMiddleware_FilterTools(t *testing.T) {
	mockLLM := &MockLLMClient{}
	middleware := NewLLMToolSelectorMiddleware(mockLLM, 5)

	allTools := []tools.Tool{
		NewMockTool("tool1", "Tool 1"),
		NewMockTool("tool2", "Tool 2"),
		NewMockTool("tool3", "Tool 3"),
		NewMockTool("tool4", "Tool 4"),
	}

	selectedNames := []string{"tool1", "tool3"}
	filtered := middleware.filterTools(allTools, selectedNames)

	assert.Len(t, filtered, 2)
	names := make(map[string]bool)
	for _, tool := range filtered {
		names[tool.Name()] = true
	}
	assert.True(t, names["tool1"])
	assert.True(t, names["tool3"])
	assert.False(t, names["tool2"])
	assert.False(t, names["tool4"])
}

func TestLLMToolSelectorMiddleware_BuildToolDescriptions(t *testing.T) {
	mockLLM := &MockLLMClient{}
	middleware := NewLLMToolSelectorMiddleware(mockLLM, 5)

	allTools := []tools.Tool{
		NewMockTool("calculator", "Performs mathematical calculations"),
		NewMockTool("web_search", "Searches the web"),
	}

	descriptions := middleware.buildToolDescriptions(allTools)

	assert.Contains(t, descriptions, "calculator")
	assert.Contains(t, descriptions, "Performs mathematical calculations")
	assert.Contains(t, descriptions, "web_search")
	assert.Contains(t, descriptions, "Searches the web")
}

func TestLLMToolSelectorMiddleware_GetCacheKey(t *testing.T) {
	mockLLM := &MockLLMClient{}
	middleware := NewLLMToolSelectorMiddleware(mockLLM, 5)

	allTools := []tools.Tool{
		NewMockTool("tool1", "Tool 1"),
		NewMockTool("tool2", "Tool 2"),
	}

	key1 := middleware.getCacheKey("query1", allTools)
	key2 := middleware.getCacheKey("query1", allTools)
	key3 := middleware.getCacheKey("query2", allTools)

	// Same query and tools should produce same key
	assert.Equal(t, key1, key2)

	// Different query should produce different key
	assert.NotEqual(t, key1, key3)
}

func TestLLMToolSelectorMiddleware_CacheOperations(t *testing.T) {
	mockLLM := &MockLLMClient{}
	middleware := NewLLMToolSelectorMiddleware(mockLLM, 5)

	// Test cache miss
	result := middleware.getCachedSelection("test_key")
	assert.Nil(t, result)

	// Test cache set
	middleware.cacheSelection("test_key", []string{"tool1", "tool2"})

	// Test cache hit
	result = middleware.getCachedSelection("test_key")
	assert.NotNil(t, result)
	assert.Equal(t, []string{"tool1", "tool2"}, result)

	// Test cache expiration (after TTL)
	middleware.CacheTTL = 100 * time.Millisecond
	middleware.cacheSelection("expire_key", []string{"tool3"})
	time.Sleep(200 * time.Millisecond)
	result = middleware.getCachedSelection("expire_key")
	assert.Nil(t, result)
}

func TestLLMToolSelectorMiddleware_Process_LLMError(t *testing.T) {
	mockLLM := &MockLLMClient{Error: assert.AnError}
	middleware := NewLLMToolSelectorMiddleware(mockLLM, 5)

	state := core.NewAgentState()
	allTools := []tools.Tool{
		NewMockTool("tool1", "Tool 1"),
		NewMockTool("tool2", "Tool 2"),
	}
	state.Set("tools", allTools)
	state.Set("query", "test query")

	// Should not fail, but use all tools as fallback
	resultState, err := middleware.Process(context.Background(), state)

	assert.NoError(t, err)

	// Should still have all tools (fallback behavior)
	toolsVal, ok := resultState.Get("tools")
	require.True(t, ok)
	tools := toolsVal.([]tools.Tool)
	assert.Len(t, tools, 2)
}

// BenchmarkLLMToolSelectorMiddleware_Process benchmarks tool selection
func BenchmarkLLMToolSelectorMiddleware_Process(b *testing.B) {
	mockLLM := &MockLLMClient{Response: "tool1, tool2, tool3"}
	middleware := NewLLMToolSelectorMiddleware(mockLLM, 5)

	state := core.NewAgentState()
	allTools := []tools.Tool{
		NewMockTool("tool1", "Tool 1"),
		NewMockTool("tool2", "Tool 2"),
		NewMockTool("tool3", "Tool 3"),
		NewMockTool("tool4", "Tool 4"),
		NewMockTool("tool5", "Tool 5"),
	}
	state.Set("tools", allTools)
	state.Set("query", "test query")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		middleware.Process(context.Background(), state)
	}
}
