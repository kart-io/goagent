package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/kart-io/goagent/examples/advanced/multi-agent-modular/agents"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

func main() {
	fmt.Println("=== Multi-Agent Collaboration System ===")
	fmt.Println("A modular implementation with specialized agents")
	fmt.Println("")

	// Initialize LLM client with Ollama priority
	llmClient := initializeLLM()

	// Create coordinator
	coordinator := agents.NewCoordinator(llmClient)

	// Example tasks
	tasks := []string{
		"Develop a microservices architecture for an e-commerce platform",
		"Optimize database performance for high-traffic application",
		"Implement CI/CD pipeline with security scanning",
	}

	ctx := context.Background()

	// Execute workflows
	for i, task := range tasks {
		fmt.Printf("\n%s\n", strings.Repeat("=", 60))
		fmt.Printf("Task %d: %s\n", i+1, task)
		fmt.Println(strings.Repeat("=", 60))

		result, err := coordinator.ExecuteWorkflow(ctx, task)
		if err != nil {
			fmt.Printf("❌ Workflow failed: %v\n", err)
			continue
		}

		// Display results
		displayResults(result)
	}

	// Demonstrate parallel execution
	fmt.Printf("\n\n%s\n", strings.Repeat("=", 60))
	fmt.Println("PARALLEL EXECUTION DEMO")
	fmt.Println(strings.Repeat("=", 60))

	parallelTasks := []string{
		"Analyze system logs for anomalies",
		"Monitor API performance metrics",
	}

	results, err := coordinator.ExecuteParallel(ctx, parallelTasks)
	if err != nil {
		fmt.Printf("❌ Parallel execution had errors: %v\n", err)
	}

	for i, result := range results {
		if result != nil {
			fmt.Printf("\n📊 Parallel Task %d Results:\n", i+1)
			fmt.Printf("   Status: %s\n", result.Status)
			fmt.Printf("   Duration: %s\n", result.Duration)
		}
	}

	// Demonstrate custom workflow
	fmt.Printf("\n\n%s\n", strings.Repeat("=", 60))
	fmt.Println("CUSTOM WORKFLOW DEMO")
	fmt.Println(strings.Repeat("=", 60))

	customSequence := []string{"analysis", "analysis", "strategy", "execution"}
	fmt.Printf("Custom sequence: %v\n", customSequence)

	customResult, err := coordinator.ExecuteCustomWorkflow(
		ctx,
		"Implement real-time data processing pipeline",
		customSequence,
	)
	if err != nil {
		fmt.Printf("❌ Custom workflow failed: %v\n", err)
	} else {
		fmt.Printf("✅ Custom workflow completed: %s\n", customResult.Status)
	}

	fmt.Printf("\n\n%s\n", strings.Repeat("=", 60))
	fmt.Println("✨ Multi-Agent Collaboration Complete!")
	fmt.Println(strings.Repeat("=", 60))
}

// initializeLLM sets up the LLM client with priority for Ollama
func initializeLLM() llm.Client {
	// Priority 1: Ollama (local, no API key needed)
	ollamaModel := os.Getenv("OLLAMA_MODEL")
	if ollamaModel == "" {
		ollamaModel = "llama2"
	}

	fmt.Printf("Checking Ollama with model '%s'...\n", ollamaModel)
	ollamaClient := providers.NewOllamaClientSimple(ollamaModel)
	if ollamaClient.IsAvailable() {
		fmt.Printf("✓ Using Ollama provider with model: %s\n\n", ollamaModel)
		return ollamaClient
	}

	fmt.Println("Ollama not available, checking cloud providers...")

	// Priority 2: OpenAI
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		config := &llm.Config{
			Provider:    llm.ProviderOpenAI,
			APIKey:      apiKey,
			Model:       "gpt-3.5-turbo",
			MaxTokens:   1000,
			Temperature: 0.7,
		}
		client, err := providers.NewOpenAI(config)
		if err == nil && client.IsAvailable() {
			fmt.Println("✓ Using OpenAI provider")
			return client
		}
	}

	// Priority 3: Gemini
	if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		config := &llm.Config{
			Provider:    llm.ProviderGemini,
			APIKey:      apiKey,
			Model:       "gemini-pro",
			MaxTokens:   1000,
			Temperature: 0.7,
		}
		client, err := providers.NewGemini(config)
		if err == nil && client.IsAvailable() {
			fmt.Println("✓ Using Gemini provider")
			return client
		}
	}

	// Priority 4: Mock client for demonstration
	fmt.Println("⚠️  No real LLM available, using mock client for demonstration")
	return &MockLLMClient{}
}

// displayResults shows workflow results in a formatted way
func displayResults(result *agents.WorkflowResult) {
	fmt.Println("\n📊 Workflow Results")
	fmt.Println(strings.Repeat("-", 40))
	fmt.Printf("Status: %s\n", result.Status)
	fmt.Printf("Duration: %s\n", result.Duration)

	if result.Analysis != "" {
		fmt.Printf("\n🔍 Analysis:\n%s\n", truncate(result.Analysis, 200))
	}

	if result.Strategy != "" {
		fmt.Printf("\n📋 Strategy:\n%s\n", truncate(result.Strategy, 200))
	}

	if result.Execution != "" {
		fmt.Printf("\n⚡ Execution:\n%s\n", truncate(result.Execution, 200))
	}

	if len(result.ToolResults) > 0 {
		fmt.Println("\n🛠️  Tool Results:")
		for tool, res := range result.ToolResults {
			jsonData, _ := json.MarshalIndent(res, "  ", "  ")
			fmt.Printf("  %s:\n  %s\n", tool, truncate(string(jsonData), 150))
		}
	}

	fmt.Printf("\n📝 Summary:\n%s\n", result.Summary)
}

// truncate helper function
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// MockLLMClient for demonstration when no real LLM is available
type MockLLMClient struct{}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{
		Content:  "Mock response for demonstration",
		Provider: "mock",
		Model:    "mock-model",
	}, nil
}

func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}

	// Generate contextual mock responses
	lastMessage := messages[len(messages)-1].Content
	content := "Mock response: Processing request..."

	if contains(lastMessage, "Analysis Agent") || contains(lastMessage, "analyze") {
		content = `Analysis Results:
• Identified 5 key patterns in the data
• Complexity: Medium-High
• Risks: Scalability, data consistency
• Opportunities: Caching, parallel processing
• Recommended approach: Phased implementation`
	} else if contains(lastMessage, "Strategy Agent") || contains(lastMessage, "strategy") {
		content = `Strategic Plan:
• Phase 1: Assessment and preparation (Week 1)
• Phase 2: Core implementation (Week 2-3)
• Phase 3: Testing and optimization (Week 4)
• Resources: Development team, infrastructure
• Success metrics: Performance improvement by 50%`
	} else if contains(lastMessage, "Execution Agent") || contains(lastMessage, "execute") {
		content = `Execution Plan:
• Step 1: Set up monitoring
• Step 2: Deploy core components
• Step 3: Configure load balancing
• Step 4: Implement caching layer
• Step 5: Validate and test`
	}

	return &llm.CompletionResponse{
		Content:  content,
		Provider: "mock",
		Model:    "mock-model",
	}, nil
}

func (m *MockLLMClient) Provider() llm.Provider {
	return llm.ProviderCustom
}

func (m *MockLLMClient) IsAvailable() bool {
	return true
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 &&
		(s == substr || len(s) > len(substr) &&
			(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
				findSubstring(s, substr)))
}

// findSubstring helper for contains
func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
