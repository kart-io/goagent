package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kart-io/goagent/utils/json"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/llm/providers"
)

// MultiAgentSystem coordinates multiple specialized agents working together
type MultiAgentSystem struct {
	llmClient llm.Client
}

// NewMultiAgentSystem creates a new multi-agent system
func NewMultiAgentSystem(llmClient llm.Client) *MultiAgentSystem {
	return &MultiAgentSystem{
		llmClient: llmClient,
	}
}

// Execute runs the multi-agent workflow
func (mas *MultiAgentSystem) Execute(ctx context.Context, task string) (map[string]interface{}, error) {
	results := make(map[string]interface{})

	// Step 1: Analysis Agent analyzes the task
	fmt.Println("\n🔍 Analysis Agent: Analyzing the task...")
	analysisPrompt := fmt.Sprintf(`You are an Analysis Agent. Analyze this task and provide insights:
Task: %s

Please provide:
1. Key data points and patterns
2. Complexity assessment
3. Risks and opportunities
4. Recommended approach`, task)

	analysisResp, err := mas.llmClient.Chat(ctx, []llm.Message{
		llm.UserMessage(analysisPrompt),
	})
	if err != nil {
		return nil, fmt.Errorf("analysis failed: %w", err)
	}
	results["analysis"] = analysisResp.Content
	fmt.Printf("✓ Analysis completed: %s\n", truncate(analysisResp.Content, 100))

	// Step 2: Strategy Agent formulates a strategy
	fmt.Println("\n📋 Strategy Agent: Formulating strategy...")
	strategyPrompt := fmt.Sprintf(`You are a Strategy Agent. Based on this analysis, create a strategy:

Analysis: %s

Please provide:
1. Phased implementation approach
2. Priority order of tasks
3. Resource requirements
4. Success metrics`, analysisResp.Content)

	strategyResp, err := mas.llmClient.Chat(ctx, []llm.Message{
		llm.UserMessage(strategyPrompt),
	})
	if err != nil {
		return nil, fmt.Errorf("strategy formulation failed: %w", err)
	}
	results["strategy"] = strategyResp.Content
	fmt.Printf("✓ Strategy formulated: %s\n", truncate(strategyResp.Content, 100))

	// Step 3: Execution Agent executes the strategy
	fmt.Println("\n⚡ Execution Agent: Planning execution...")
	executionPrompt := fmt.Sprintf(`You are an Execution Agent. Create an execution plan for this strategy:

Strategy: %s

Please provide:
1. Specific actions to take
2. Tools to use (HTTP APIs, commands, files)
3. Execution order
4. Expected outcomes`, strategyResp.Content)

	executionResp, err := mas.llmClient.Chat(ctx, []llm.Message{
		llm.UserMessage(executionPrompt),
	})
	if err != nil {
		return nil, fmt.Errorf("execution planning failed: %w", err)
	}
	results["execution_plan"] = executionResp.Content
	fmt.Printf("✓ Execution plan created: %s\n", truncate(executionResp.Content, 100))

	// Simulate tool execution
	fmt.Println("\n🛠️  Executing with tools...")
	toolResults := mas.executeTools(ctx, task)
	results["tool_execution"] = toolResults

	// Aggregate results
	results["summary"] = "Task completed successfully through multi-agent collaboration"

	return results, nil
}

// executeTools simulates tool execution
func (mas *MultiAgentSystem) executeTools(ctx context.Context, task string) map[string]interface{} {
	toolResults := make(map[string]interface{})

	// Simulate different tools based on task keywords
	if strings.Contains(strings.ToLower(task), "api") || strings.Contains(strings.ToLower(task), "weather") {
		// Simulate HTTP tool
		httpTool := &HTTPTool{}
		result, _ := httpTool.Execute(ctx, map[string]interface{}{
			"method": "GET",
			"url":    "https://api.example.com/data",
		})
		toolResults["http_request"] = result
		fmt.Println("  ✓ HTTP API call simulated")
	}

	if strings.Contains(strings.ToLower(task), "file") || strings.Contains(strings.ToLower(task), "data") {
		// Simulate file operations
		fileTool := &FileOperationsTool{}
		result, _ := fileTool.Execute(ctx, map[string]interface{}{
			"operation": "read",
			"path":      "/data/metrics.json",
		})
		toolResults["file_operation"] = result
		fmt.Println("  ✓ File operation simulated")
	}

	if strings.Contains(strings.ToLower(task), "analyze") || strings.Contains(strings.ToLower(task), "process") {
		// Simulate data analysis
		analysisTool := &DataAnalysisTool{}
		result, _ := analysisTool.Execute(ctx, "Sample data for analysis")
		toolResults["data_analysis"] = result
		fmt.Println("  ✓ Data analysis completed")
	}

	return toolResults
}

// Tool Implementations

// DataAnalysisTool analyzes data
type DataAnalysisTool struct{}

func (t *DataAnalysisTool) Name() string        { return "data_analysis" }
func (t *DataAnalysisTool) Description() string { return "Analyze data and extract insights" }

func (t *DataAnalysisTool) Execute(ctx context.Context, input interface{}) (interface{}, error) {
	data := fmt.Sprintf("%v", input)

	analysis := map[string]interface{}{
		"data_points": len(strings.Split(data, " ")),
		"complexity":  "medium",
		"key_themes":  []string{"automation", "efficiency", "scalability"},
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	return analysis, nil
}

// HTTPTool performs HTTP requests
type HTTPTool struct{}

func (t *HTTPTool) Name() string        { return "http_request" }
func (t *HTTPTool) Description() string { return "Make HTTP requests to APIs" }

func (t *HTTPTool) Execute(ctx context.Context, input interface{}) (interface{}, error) {
	params, ok := input.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid input format")
	}

	method, _ := params["method"].(string)
	url, _ := params["url"].(string)

	// Simulate HTTP request (don't actually make the request in this example)
	return map[string]interface{}{
		"status_code": 200,
		"body":        fmt.Sprintf("Simulated response from %s", url),
		"method":      method,
		"timestamp":   time.Now().Format(time.RFC3339),
	}, nil
}

// FileOperationsTool handles file operations
type FileOperationsTool struct{}

func (t *FileOperationsTool) Name() string        { return "file_operations" }
func (t *FileOperationsTool) Description() string { return "Perform file operations" }

func (t *FileOperationsTool) Execute(ctx context.Context, input interface{}) (interface{}, error) {
	params, ok := input.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid input format")
	}

	operation, _ := params["operation"].(string)
	path, _ := params["path"].(string)

	// Simulate file operations (don't actually perform them)
	switch operation {
	case "read":
		return map[string]interface{}{
			"content":   fmt.Sprintf("Simulated content of %s", path),
			"size":      "1024 bytes",
			"timestamp": time.Now().Format(time.RFC3339),
		}, nil
	case "write":
		return map[string]interface{}{
			"status":    "success",
			"path":      path,
			"timestamp": time.Now().Format(time.RFC3339),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported operation: %s", operation)
	}
}

// Helper function to truncate strings
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func main() {
	fmt.Println("=== Multi-Agent Collaboration Example ===")
	fmt.Println("Demonstrating three specialized agents working together:")
	fmt.Println("1. Analysis Agent - Analyzes data and identifies patterns")
	fmt.Println("2. Strategy Agent - Formulates optimal approaches")
	fmt.Println("3. Execution Agent - Executes tasks with tools and HTTP")
	fmt.Println()

	// Initialize LLM client
	var llmClient llm.Client

	// First, try Ollama (local, no API key needed)
	ollamaModel := os.Getenv("OLLAMA_MODEL")
	if ollamaModel == "" {
		ollamaModel = "gemma3:12b" // Default model, can also use qwen2, deepseek-coder, etc.
	}

	fmt.Printf("Checking Ollama with model '%s'...\n", ollamaModel)
	ollamaClient, err := providers.NewOllamaClientSimple(ollamaModel)
	if err != nil {
		fmt.Printf("⚠️  Error creating Ollama client: %v\n", err)
	} else if ollamaClient.IsAvailable() {
		fmt.Printf("✓ Using Ollama provider with model: %s\n", ollamaModel)
		fmt.Println("  Tip: You can change the model by setting OLLAMA_MODEL environment variable")
		fmt.Println("  Example: export OLLAMA_MODEL=qwen2 or export OLLAMA_MODEL=deepseek-coder")
		llmClient = ollamaClient
	} else {
		fmt.Println("ℹ️  Ollama not available. Checking other providers...")

		if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
			fmt.Println("Trying OpenAI provider...")
			config := &llm.LLMOptions{
				Provider:    constants.ProviderOpenAI,
				APIKey:      apiKey,
				Model:       "gpt-3.5-turbo",
				MaxTokens:   500,
				Temperature: 0.7,
			}
			client, err := providers.NewOpenAI(config)
			if err == nil && client.IsAvailable() {
				fmt.Println("✓ Using OpenAI provider")
				llmClient = client
			}
		}

		if llmClient == nil {
			if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
				fmt.Println("Trying Gemini provider...")
				config := &llm.LLMOptions{
					Provider:    constants.ProviderGemini,
					APIKey:      apiKey,
					Model:       "gemini-pro",
					MaxTokens:   500,
					Temperature: 0.7,
				}
				client, err := providers.NewGemini(config)
				if err == nil && client.IsAvailable() {
					fmt.Println("✓ Using Gemini provider")
					llmClient = client
				}
			}
		}
	}

	// Use mock client for demonstration if no real LLM is available
	if llmClient == nil {
		fmt.Println("Using Mock LLM provider for demonstration")
		llmClient = &MockLLMClient{}
	}

	// Create multi-agent system
	system := NewMultiAgentSystem(llmClient)

	// Example tasks
	tasks := []string{
		"Analyze website performance data and optimize loading times",
		"Process customer feedback and create improvement plan",
		"Fetch weather data from API and generate daily report",
	}

	ctx := context.Background()

	for i, task := range tasks {
		fmt.Printf("\n========================================")
		fmt.Printf("\nTask %d: %s\n", i+1, task)
		fmt.Printf("========================================\n")

		results, err := system.Execute(ctx, task)
		if err != nil {
			fmt.Printf("❌ Task failed: %v\n", err)
			continue
		}

		fmt.Println("\n📊 Final Results:")
		fmt.Println("----------------------------------------")
		for key, value := range results {
			if key != "summary" {
				// Pretty print JSON if it's a map or slice
				if _, ok := value.(map[string]interface{}); ok {
					jsonBytes, _ := json.MarshalIndent(value, "  ", "  ")
					fmt.Printf("%s:\n  %s\n", strings.ToTitle(key), string(jsonBytes))
				} else {
					fmt.Printf("%s:\n  %v\n", strings.ToTitle(key), truncate(fmt.Sprintf("%v", value), 200))
				}
			}
		}
		fmt.Printf("\n✨ %v\n", results["summary"])
	}

	fmt.Println("\n=== Multi-Agent Collaboration Complete ===")
	fmt.Println("\nKey Features Demonstrated:")
	fmt.Println("✓ Agent specialization and roles")
	fmt.Println("✓ Sequential workflow coordination")
	fmt.Println("✓ Tool usage (HTTP, file ops, data analysis)")
	fmt.Println("✓ LLM-based reasoning and planning")
	fmt.Println("✓ Task execution with simulated tools")
}

// MockLLMClient for demonstration when no real LLM is available
type MockLLMClient struct{}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	// Generate mock responses based on the messages
	content := "Mock response: Processing request..."

	// Check the messages for content hints
	if len(req.Messages) > 0 {
		lastMessage := req.Messages[len(req.Messages)-1].Content
		if strings.Contains(lastMessage, "Analysis Agent") {
			content = "Analysis complete: Identified 5 key patterns in the data. Complexity: Medium. Main risks: scalability, data quality. Opportunities: caching, parallel processing."
		} else if strings.Contains(lastMessage, "Strategy Agent") {
			content = "Strategy formulated: Phase 1 - Data preparation (Week 1), Phase 2 - Core implementation (Week 2-3), Phase 3 - Optimization (Week 4). Priority: Performance > Reliability > UX."
		} else if strings.Contains(lastMessage, "Execution Agent") {
			content = "Execution plan: 1) Call API endpoints for data collection, 2) Process data with parallel workers, 3) Store results in cache, 4) Update monitoring dashboard."
		}
	}

	return &llm.CompletionResponse{
		Content:  content,
		Provider: "mock",
		Model:    "mock-model",
	}, nil
}

func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}

	lastMessage := messages[len(messages)-1]
	prompt := lastMessage.Content

	// Generate appropriate mock response based on agent type
	content := "Mock response: Processing request..."

	if strings.Contains(prompt, "Analysis Agent") {
		content = `Analysis Results:
• Data Points: 15 identified patterns across 3 categories
• Complexity: Medium-High due to distributed nature
• Key Risks: Data consistency, processing latency
• Opportunities: Implement caching layer, optimize queries
• Recommendation: Phased approach with monitoring`
	} else if strings.Contains(prompt, "Strategy Agent") {
		content = `Strategic Plan:
• Phase 1 (Week 1): Assessment and preparation
  - Gather baseline metrics
  - Set up monitoring
• Phase 2 (Week 2-3): Core implementation
  - Deploy optimization changes
  - Implement caching
• Phase 3 (Week 4): Validation and scaling
  - Performance testing
  - Gradual rollout
• Success Metrics: 50% reduction in latency, 99.9% uptime`
	} else if strings.Contains(prompt, "Execution Agent") {
		content = `Execution Plan:
• Step 1: Initialize monitoring dashboard
  - Tool: HTTP API (POST /api/dashboard/init)
• Step 2: Deploy cache layer
  - Tool: File operations (write config.yaml)
• Step 3: Update load balancer
  - Tool: HTTP API (PUT /api/loadbalancer/config)
• Step 4: Validate changes
  - Tool: HTTP API (GET /api/metrics/validate)
• Expected Outcome: All systems operational with improved performance`
	}

	return &llm.CompletionResponse{
		Content:  content,
		Provider: "mock",
		Model:    "mock-model",
	}, nil
}

func (m *MockLLMClient) Provider() constants.Provider {
	return constants.ProviderCustom
}

func (m *MockLLMClient) IsAvailable() bool {
	return true
}
