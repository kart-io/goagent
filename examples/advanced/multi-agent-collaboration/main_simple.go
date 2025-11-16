package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/tools"
)

func main() {
	fmt.Println("========================================")
	fmt.Println("=== Multi-Agent Collaboration Example ===")
	fmt.Println("========================================")
	fmt.Println()

	// Initialize LLM client
	llmClient, err := initializeLLMClient()
	if err != nil {
		log.Fatalf("Failed to initialize LLM client: %v", err)
	}

	// Define the task
	task := "Analyze website performance data and optimize loading times for an e-commerce platform"
	fmt.Printf("Task: %s\n", task)
	fmt.Println("========================================")
	fmt.Println()

	// Create the three specialized agents
	analysisAgent, err := createAnalysisAgent(llmClient)
	if err != nil {
		log.Fatalf("Failed to create analysis agent: %v", err)
	}

	strategyAgent, err := createStrategyAgent(llmClient)
	if err != nil {
		log.Fatalf("Failed to create strategy agent: %v", err)
	}

	executionAgent, err := createExecutionAgent(llmClient)
	if err != nil {
		log.Fatalf("Failed to create execution agent: %v", err)
	}

	ctx := context.Background()

	// Step 1: Analysis Agent analyzes the task
	fmt.Println("🔍 Analysis Agent: Analyzing the task...")
	analysisResult, err := runAgent(ctx, analysisAgent, task)
	if err != nil {
		log.Fatalf("Analysis agent failed: %v", err)
	}
	fmt.Printf("✓ Analysis completed\n\n")
	printResult("Analysis", analysisResult)
	fmt.Println()

	// Step 2: Strategy Agent formulates approach
	fmt.Println("📋 Strategy Agent: Formulating strategy...")
	strategyInput := fmt.Sprintf("Based on this analysis, create a strategy:\n%s", formatResult(analysisResult))
	strategyResult, err := runAgent(ctx, strategyAgent, strategyInput)
	if err != nil {
		log.Fatalf("Strategy agent failed: %v", err)
	}
	fmt.Printf("✓ Strategy formulated\n\n")
	printResult("Strategy", strategyResult)
	fmt.Println()

	// Step 3: Execution Agent implements the strategy
	fmt.Println("⚡ Execution Agent: Executing the strategy...")
	executionInput := fmt.Sprintf("Execute this strategy:\n%s", formatResult(strategyResult))
	executionResult, err := runAgent(ctx, executionAgent, executionInput)
	if err != nil {
		log.Fatalf("Execution agent failed: %v", err)
	}
	fmt.Printf("✓ Execution completed\n\n")
	printResult("Execution", executionResult)
	fmt.Println()

	// Summary
	fmt.Println("========================================")
	fmt.Println("📊 Multi-Agent Workflow Complete!")
	fmt.Println("========================================")
	fmt.Println()
	fmt.Println("The three agents successfully collaborated:")
	fmt.Println("  1. 🔍 Analysis Agent: Analyzed the task and identified key areas")
	fmt.Println("  2. 📋 Strategy Agent: Created a phased implementation plan")
	fmt.Println("  3. ⚡ Execution Agent: Executed the strategy with concrete actions")
	fmt.Println()
	fmt.Println("✨ Task completed successfully!")
	fmt.Println()
}

// initializeLLMClient creates an LLM client based on environment configuration
func initializeLLMClient() (llm.Client, error) {
	// Priority 1: Try Ollama (local, no API key needed)
	if ollamaModel := os.Getenv("OLLAMA_MODEL"); ollamaModel != "" {
		fmt.Printf("Using Ollama with model: %s\n", ollamaModel)
		config := providers.DefaultOllamaConfig()
		config.Model = ollamaModel
		return providers.NewOllamaClient(config), nil
	}

	// Check if Ollama is available (try default)
	ollamaClient := providers.NewOllamaClientSimple("llama2")
	// Test connection by checking if Ollama is running
	if testOllamaConnection(ollamaClient) {
		fmt.Println("Using Ollama with default model: llama2")
		return ollamaClient, nil
	}

	// Priority 2: Try OpenAI
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		fmt.Println("Using OpenAI GPT-3.5-turbo")
		provider, err := providers.NewOpenAI(&llm.Config{
			APIKey:      apiKey,
			Model:       "gpt-3.5-turbo",
			Temperature: 0.7,
			MaxTokens:   2000,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create OpenAI client: %w", err)
		}
		return provider, nil
	}

	// Priority 3: Try Gemini
	if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		fmt.Println("Using Google Gemini")
		provider, err := providers.NewGemini(&llm.Config{
			APIKey:      apiKey,
			Model:       "gemini-pro",
			Temperature: 0.7,
			MaxTokens:   2000,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create Gemini client: %w", err)
		}
		return provider, nil
	}

	return nil, fmt.Errorf("no LLM provider configured. Please set OLLAMA_MODEL, OPENAI_API_KEY, or GEMINI_API_KEY")
}

// testOllamaConnection tests if Ollama is available
func testOllamaConnection(client llm.Client) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Try a simple request
	_, err := client.Chat(ctx, []llm.Message{
		{Role: "user", Content: "test"},
	})

	return err == nil
}

// createAnalysisAgent creates the analysis agent with analysis tools
func createAnalysisAgent(llmClient llm.Client) (*builder.ConfigurableAgent[any, core.State], error) {
	// Create analysis tools
	registry := tools.NewRegistry()
	registry.Register(createDataAnalysisTool())
	registry.Register(createSummarizeTool())

	systemPrompt := `You are an expert Analysis Agent. Your role is to:
1. Analyze tasks and data thoroughly
2. Use the data_analysis tool to extract insights
3. Use the summarize tool to create concise summaries
4. Identify patterns, risks, and opportunities

When given a task:
- First use data_analysis to understand the scope and complexity
- Extract key themes and potential issues
- Identify opportunities for improvement
- Provide a clear summary using the summarize tool

Always call the appropriate tools and return their results.`

	// Build the agent using the builder pattern
	agent, err := builder.NewAgentBuilder[any, core.State](llmClient).
		WithSystemPrompt(systemPrompt).
		WithTools(registry.List()...).
		Build()

	if err != nil {
		return nil, fmt.Errorf("failed to build analysis agent: %w", err)
	}

	return agent, nil
}

// createStrategyAgent creates the strategy formulation agent
func createStrategyAgent(llmClient llm.Client) (*builder.ConfigurableAgent[any, core.State], error) {
	// Create strategy tools
	registry := tools.NewRegistry()
	registry.Register(createFormulateStrategyTool())
	registry.Register(createPrioritizeTasksTool())

	systemPrompt := `You are an expert Strategy Agent. Your role is to:
1. Review analysis results
2. Use formulate_strategy to create comprehensive plans
3. Use prioritize_tasks to order actions by impact
4. Define clear success metrics and timelines

When creating a strategy:
- Consider the analysis findings carefully
- Use formulate_strategy to create a phased approach
- Break down the plan into prioritized tasks
- Define measurable success criteria

Always call the appropriate tools and return their results.`

	agent, err := builder.NewAgentBuilder[any, core.State](llmClient).
		WithSystemPrompt(systemPrompt).
		WithTools(registry.List()...).
		Build()

	if err != nil {
		return nil, fmt.Errorf("failed to build strategy agent: %w", err)
	}

	return agent, nil
}

// createExecutionAgent creates the execution agent
func createExecutionAgent(llmClient llm.Client) (*builder.ConfigurableAgent[any, core.State], error) {
	// Create execution tools
	registry := tools.NewRegistry()
	registry.Register(createHTTPRequestTool())
	registry.Register(createExecuteCommandTool())
	registry.Register(createFileOperationsTool())

	systemPrompt := `You are an expert Execution Agent. Your role is to:
1. Review strategic plans
2. Use http_request to interact with APIs
3. Use execute_command to run system commands (simulated)
4. Use file_operations to manage files (simulated)

When executing a strategy:
- Understand the strategic plan thoroughly
- Use http_request to gather data or interact with services
- Use execute_command for system operations (noted as simulated for safety)
- Use file_operations for file management tasks
- Report clear results for each action

Always call the appropriate tools and return their results.`

	agent, err := builder.NewAgentBuilder[any, core.State](llmClient).
		WithSystemPrompt(systemPrompt).
		WithTools(registry.List()...).
		Build()

	if err != nil {
		return nil, fmt.Errorf("failed to build execution agent: %w", err)
	}

	return agent, nil
}

// runAgent executes an agent with a given task
func runAgent(ctx context.Context, agent *builder.ConfigurableAgent[any, core.State], task string) (map[string]interface{}, error) {
	// Run the agent with text input
	output, err := agent.Execute(ctx, task)
	if err != nil {
		return nil, err
	}

	// Extract result from output
	if output != nil && output.Result != nil {
		// Try to convert result to map
		if resultMap, ok := output.Result.(map[string]interface{}); ok {
			return resultMap, nil
		}

		// If result is a string, try to parse as JSON
		if resultStr, ok := output.Result.(string); ok {
			var result map[string]interface{}
			if err := json.Unmarshal([]byte(resultStr), &result); err == nil {
				return result, nil
			}

			// If not JSON, return as text result
			return map[string]interface{}{
				"text": resultStr,
			}, nil
		}

		// Return as generic result
		return map[string]interface{}{
			"result": output.Result,
		}, nil
	}

	return map[string]interface{}{
		"status": "completed",
	}, nil
}

// formatResult converts a result map to a readable string
func formatResult(result map[string]interface{}) string {
	jsonBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", result)
	}
	return string(jsonBytes)
}

// printResult prints a formatted result
func printResult(title string, result map[string]interface{}) {
	fmt.Printf("--- %s Results ---\n", title)

	// Try to pretty-print the result
	if text, ok := result["text"].(string); ok {
		// It's a text result
		fmt.Println(text)
	} else {
		// It's structured data
		formatted := formatResult(result)
		// Limit output length for readability
		lines := strings.Split(formatted, "\n")
		displayLines := lines
		if len(lines) > 20 {
			displayLines = append(lines[:20], "... (output truncated)")
		}
		for _, line := range displayLines {
			fmt.Println(line)
		}
	}
}
