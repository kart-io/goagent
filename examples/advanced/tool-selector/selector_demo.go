package main

import (
	"context"
	"fmt"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/middleware"
	"github.com/kart-io/goagent/tools"
)

// SimpleLLMClient 简单的 LLM 客户端实现
type SimpleLLMClient struct {
	responses map[string]string
}

func NewSimpleLLMClient() *SimpleLLMClient {
	return &SimpleLLMClient{
		responses: map[string]string{
			"selection": "calculator, web_search, code_analyzer",
			"default":   "I have selected the most relevant tools for your query.",
		},
	}
}

func (s *SimpleLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	// Check if this is a tool selection request
	content := "I have selected the most relevant tools."
	if len(req.Messages) > 0 {
		lastMsg := req.Messages[len(req.Messages)-1]
		if lastMsg.Role == "user" {
			// Simple pattern matching for tool selection
			userContent := lastMsg.Content
			if len(userContent) > 0 {
				// If the prompt contains "Select up to", return tool names
				if len(userContent) > 100 { // Tool selection prompts are longer
					content = "calculator, web_search, code_analyzer"
				}
			}
		}
	}

	return &llm.CompletionResponse{
		Content:    content,
		Model:      "mock-model",
		TokensUsed: 15,
	}, nil
}

func (s *SimpleLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return s.Complete(ctx, &llm.CompletionRequest{Messages: messages})
}

func (s *SimpleLLMClient) Provider() llm.Provider {
	return llm.ProviderCustom
}

func (s *SimpleLLMClient) IsAvailable() bool {
	return true
}

// createSampleTools 创建示例工具集
func createSampleTools() []tools.Tool {
	allTools := []tools.Tool{
		createTool("calculator", "Performs mathematical calculations"),
		createTool("web_search", "Searches the web for information"),
		createTool("code_analyzer", "Analyzes code for bugs and improvements"),
		createTool("file_reader", "Reads files from the filesystem"),
		createTool("database_query", "Queries databases"),
		createTool("api_caller", "Makes HTTP API calls"),
		createTool("email_sender", "Sends emails"),
		createTool("calendar_manager", "Manages calendar events"),
		createTool("weather_checker", "Checks weather information"),
		createTool("translator", "Translates text between languages"),
	}
	return allTools
}

// createTool 创建一个工具
func createTool(name, description string) interfaces.Tool {
	runFunc := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &tools.ToolOutput{
			Result:  fmt.Sprintf("Result from %s", name),
			Success: true,
		}, nil
	}

	return tools.NewBaseTool(name, description, "{}", runFunc)
}

func main() {
	fmt.Println("=== Tool Selector Middleware Demo ===")

	ctx := context.Background()
	llmClient := NewSimpleLLMClient()

	// Demo 1: Basic Tool Selection
	demo1BasicSelection(ctx, llmClient)

	fmt.Println()

	// Demo 2: Tool Selection with Always-Include
	demo2AlwaysInclude(ctx, llmClient)

	fmt.Println()

	// Demo 3: Tool Selection with Max Limit
	demo3MaxLimit(ctx, llmClient)

	fmt.Println()

	// Demo 4: Tool Selection Caching
	demo4Caching(ctx, llmClient)

	fmt.Println()

	// Demo 5: Compare Token Usage
	demo5TokenComparison(ctx, llmClient)

	fmt.Println("\n=== Demo Complete ===")
}

// Demo 1: Basic tool selection
func demo1BasicSelection(ctx context.Context, llmClient llm.Client) {
	fmt.Println("--- Demo 1: Basic Tool Selection ---")

	// Create middleware with max 3 tools
	selector := middleware.NewLLMToolSelectorMiddleware(llmClient, 3)

	// Create state with all tools
	state := core.NewAgentState()
	allTools := createSampleTools()
	state.Set("tools", allTools)
	state.Set("query", "I need to calculate some numbers and search the web")

	fmt.Printf("Original tools count: %d\n", len(allTools))

	// Process through middleware
	resultState, err := selector.Process(ctx, state)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Check selected tools
	if toolsVal, ok := resultState.Get("tools"); ok {
		selectedTools := toolsVal.([]tools.Tool)
		fmt.Printf("Selected tools count: %d\n", len(selectedTools))
		fmt.Println("Selected tools:")
		for _, tool := range selectedTools {
			fmt.Printf("  - %s: %s\n", tool.Name(), tool.Description())
		}
	}

	// Show metadata
	if metaVal, ok := resultState.Get("tool_selection_metadata"); ok {
		meta := metaVal.(map[string]interface{})
		fmt.Printf("\nMetadata:\n")
		fmt.Printf("  Original count: %d\n", meta["original_count"])
		fmt.Printf("  Selected count: %d\n", meta["selected_count"])
		fmt.Printf("  Token savings: %.1f%%\n",
			(1.0-float64(meta["selected_count"].(int))/float64(meta["original_count"].(int)))*100)
	}
}

// Demo 2: Always include critical tools
func demo2AlwaysInclude(ctx context.Context, llmClient llm.Client) {
	fmt.Println("--- Demo 2: Tool Selection with Always-Include ---")

	// Create middleware with always-include tools
	selector := middleware.NewLLMToolSelectorMiddleware(llmClient, 3).
		WithAlwaysInclude("file_reader", "database_query")

	state := core.NewAgentState()
	allTools := createSampleTools()
	state.Set("tools", allTools)
	state.Set("query", "Calculate mathematical expressions")

	fmt.Printf("Original tools count: %d\n", len(allTools))
	fmt.Println("Always-include tools: file_reader, database_query")

	resultState, err := selector.Process(ctx, state)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if toolsVal, ok := resultState.Get("tools"); ok {
		selectedTools := toolsVal.([]tools.Tool)
		fmt.Printf("\nSelected tools count: %d\n", len(selectedTools))
		fmt.Println("Selected tools:")
		for _, tool := range selectedTools {
			fmt.Printf("  - %s: %s\n", tool.Name(), tool.Description())
		}
	}
}

// Demo 3: Max tools limit enforcement
func demo3MaxLimit(ctx context.Context, llmClient llm.Client) {
	fmt.Println("--- Demo 3: Tool Selection with Max Limit ---")

	// Test different max limits
	limits := []int{2, 5, 8}

	state := core.NewAgentState()
	allTools := createSampleTools()
	state.Set("tools", allTools)
	state.Set("query", "I need multiple tools for a complex task")

	fmt.Printf("Original tools count: %d\n\n", len(allTools))

	for _, maxTools := range limits {
		selector := middleware.NewLLMToolSelectorMiddleware(llmClient, maxTools)

		// Clone state for each test
		testState := core.NewAgentState()
		testState.Set("tools", allTools)
		testState.Set("query", "I need multiple tools for a complex task")

		resultState, err := selector.Process(ctx, testState)
		if err != nil {
			fmt.Printf("Error with max=%d: %v\n", maxTools, err)
			continue
		}

		if toolsVal, ok := resultState.Get("tools"); ok {
			selectedTools := toolsVal.([]tools.Tool)
			fmt.Printf("Max limit: %d -> Selected: %d tools\n", maxTools, len(selectedTools))
		}
	}
}

// Demo 4: Caching behavior
func demo4Caching(ctx context.Context, llmClient llm.Client) {
	fmt.Println("--- Demo 4: Tool Selection Caching ---")

	selector := middleware.NewLLMToolSelectorMiddleware(llmClient, 3)

	allTools := createSampleTools()
	query := "Calculate and search for information"

	// First call - should call LLM
	state1 := core.NewAgentState()
	state1.Set("tools", allTools)
	state1.Set("query", query)

	fmt.Println("First call (will call LLM)...")
	resultState1, _ := selector.Process(ctx, state1)
	if toolsVal, ok := resultState1.Get("tools"); ok {
		selectedTools := toolsVal.([]tools.Tool)
		fmt.Printf("  Selected %d tools\n", len(selectedTools))
	}

	// Second call with same query - should use cache
	state2 := core.NewAgentState()
	state2.Set("tools", allTools)
	state2.Set("query", query)

	fmt.Println("\nSecond call with same query (will use cache)...")
	resultState2, _ := selector.Process(ctx, state2)
	if toolsVal, ok := resultState2.Get("tools"); ok {
		selectedTools := toolsVal.([]tools.Tool)
		fmt.Printf("  Selected %d tools (from cache)\n", len(selectedTools))
	}

	// Third call with different query - should call LLM again
	state3 := core.NewAgentState()
	state3.Set("tools", allTools)
	state3.Set("query", "Different query for email and calendar")

	fmt.Println("\nThird call with different query (will call LLM)...")
	resultState3, _ := selector.Process(ctx, state3)
	if toolsVal, ok := resultState3.Get("tools"); ok {
		selectedTools := toolsVal.([]tools.Tool)
		fmt.Printf("  Selected %d tools\n", len(selectedTools))
	}
}

// Demo 5: Token usage comparison
func demo5TokenComparison(ctx context.Context, llmClient llm.Client) {
	fmt.Println("--- Demo 5: Token Usage Comparison ---")

	allTools := createSampleTools()

	// Scenario 1: Without tool selector (all tools)
	fmt.Println("\nScenario 1: Without Tool Selector")
	fmt.Printf("  Tools in prompt: %d\n", len(allTools))

	// Estimate token count (rough: ~50 tokens per tool description)
	tokensWithoutSelector := len(allTools) * 50
	fmt.Printf("  Estimated prompt tokens: ~%d\n", tokensWithoutSelector)

	// Scenario 2: With tool selector (limited tools)
	fmt.Println("\nScenario 2: With Tool Selector (max 3 tools)")
	selector := middleware.NewLLMToolSelectorMiddleware(llmClient, 3)

	state := core.NewAgentState()
	state.Set("tools", allTools)
	state.Set("query", "Calculate and analyze code")

	resultState, _ := selector.Process(ctx, state)

	var selectedCount int
	if toolsVal, ok := resultState.Get("tools"); ok {
		selectedTools := toolsVal.([]tools.Tool)
		selectedCount = len(selectedTools)
		fmt.Printf("  Tools in prompt: %d\n", selectedCount)
	}

	// Tokens for selection + selected tools
	selectionTokens := 100 // Cost of running tool selector
	tokensWithSelector := selectionTokens + (selectedCount * 50)
	fmt.Printf("  Selection cost: ~%d tokens\n", selectionTokens)
	fmt.Printf("  Selected tools cost: ~%d tokens\n", selectedCount*50)
	fmt.Printf("  Total prompt tokens: ~%d\n", tokensWithSelector)

	// Calculate savings
	savings := float64(tokensWithoutSelector-tokensWithSelector) / float64(tokensWithoutSelector) * 100
	fmt.Printf("\n  Token savings: %.1f%%\n", savings)
	fmt.Printf("  Cost reduction: ~%.1f%% (assuming same token cost)\n", savings)
}
