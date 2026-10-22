package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/utils/httpclient"
	"github.com/kart-io/goagent/utils/json"
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
		return providers.NewOllamaWithOptions(
			llm.WithModel(ollamaModel),
		)
	}

	// Check if Ollama is available (try default)
	ollamaClient, _ := providers.NewOllamaClientSimple("llama2")
	// Test connection by checking if Ollama is running
	if testOllamaConnection(ollamaClient) {
		fmt.Println("Using Ollama with default model: llama2")
		return ollamaClient, nil
	}

	// Priority 2: Try OpenAI
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		fmt.Println("Using OpenAI GPT-3.5-turbo")
		provider, err := providers.NewOpenAIWithOptions(llm.WithAPIKey(apiKey), llm.WithModel("gpt-3.5-turbo"), llm.WithMaxTokens(2000), llm.WithTemperature(0.7))
		if err != nil {
			return nil, fmt.Errorf("failed to create OpenAI client: %w", err)
		}
		return provider, nil
	}

	// Priority 3: Try Gemini
	if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		fmt.Println("Using Google Gemini")
		provider, err := providers.NewGeminiWithOptions(llm.WithAPIKey(apiKey), llm.WithModel("gemini-pro"), llm.WithTemperature(0.7), llm.WithMaxTokens(2000))
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
	_ = registry.Register(createDataAnalysisTool())
	_ = registry.Register(createSummarizeTool())

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
		WithState(core.NewAgentState()).
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
	_ = registry.Register(createFormulateStrategyTool())
	_ = registry.Register(createPrioritizeTasksTool())

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
		WithState(core.NewAgentState()).
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
	_ = registry.Register(createHTTPRequestTool())
	_ = registry.Register(createExecuteCommandTool())
	_ = registry.Register(createFileOperationsTool())

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
		WithState(core.NewAgentState()).
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

// ==================== Analysis Tools ====================

// createDataAnalysisTool creates a tool that analyzes data and extracts insights
func createDataAnalysisTool() interfaces.Tool {
	schema := `{
		"type": "object",
		"properties": {
			"data": {
				"type": "string",
				"description": "Data or task description to analyze"
			}
		},
		"required": ["data"]
	}`

	runFunc := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		data, ok := input.Args["data"].(string)
		if !ok {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "data parameter must be a string",
			}, nil
		}

		// Simulate analysis
		words := strings.Fields(data)
		complexity := "low"
		if len(words) > 20 {
			complexity = "medium"
		}
		if len(words) > 50 {
			complexity = "high"
		}

		// Extract key themes (simplified - look for common words)
		keyThemes := []string{}
		themes := map[string]int{}
		for _, word := range words {
			word = strings.ToLower(strings.Trim(word, ".,!?"))
			if len(word) > 4 { // Only consider longer words
				themes[word]++
			}
		}
		for theme, count := range themes {
			if count > 1 {
				keyThemes = append(keyThemes, theme)
			}
		}
		if len(keyThemes) == 0 {
			keyThemes = []string{"general", "analysis", "optimization"}
		}

		// Generate insights
		risks := []string{
			"Potential resource constraints",
			"Timeline dependencies",
			"Technical complexity",
		}

		opportunities := []string{
			"Process automation potential",
			"Performance optimization",
			"Cost reduction possibilities",
		}

		result := map[string]interface{}{
			"data_points":   len(words),
			"complexity":    complexity,
			"key_themes":    keyThemes[:min(3, len(keyThemes))],
			"risks":         risks,
			"opportunities": opportunities,
			"analysis_time": time.Now().Format(time.RFC3339),
		}

		return &interfaces.ToolOutput{
			Success: true,
			Result:  result,
		}, nil
	}

	return tools.NewBaseTool(
		"data_analysis",
		"Analyze data and extract insights including complexity, key themes, risks, and opportunities",
		schema,
		runFunc,
	)
}

// createSummarizeTool creates a tool that summarizes text
func createSummarizeTool() interfaces.Tool {
	schema := `{
		"type": "object",
		"properties": {
			"text": {
				"type": "string",
				"description": "Text to summarize"
			}
		},
		"required": ["text"]
	}`

	runFunc := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		text, ok := input.Args["text"].(string)
		if !ok {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "text parameter must be a string",
			}, nil
		}

		words := strings.Fields(text)
		sentences := strings.Split(text, ".")

		// Create a concise summary (take first 2-3 sentences)
		summaryParts := []string{}
		for i := 0; i < min(3, len(sentences)); i++ {
			if strings.TrimSpace(sentences[i]) != "" {
				summaryParts = append(summaryParts, strings.TrimSpace(sentences[i]))
			}
		}
		summary := strings.Join(summaryParts, ". ")
		if !strings.HasSuffix(summary, ".") {
			summary += "."
		}

		// Extract key points
		keyPoints := fmt.Sprintf("Analyzed %d words across %d sentences. Main topics identified.", len(words), len(sentences))

		result := map[string]interface{}{
			"summary":    summary,
			"word_count": fmt.Sprintf("%d words", len(words)),
			"key_points": keyPoints,
		}

		return &interfaces.ToolOutput{
			Success: true,
			Result:  result,
		}, nil
	}

	return tools.NewBaseTool(
		"summarize",
		"Create a concise summary of text content with key points",
		schema,
		runFunc,
	)
}

// ==================== Strategy Tools ====================

// createFormulateStrategyTool creates a tool that formulates strategic approaches
func createFormulateStrategyTool() interfaces.Tool {
	schema := `{
		"type": "object",
		"properties": {
			"analysis": {
				"type": "string",
				"description": "Analysis results to base strategy on"
			}
		},
		"required": ["analysis"]
	}`

	runFunc := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		_, ok := input.Args["analysis"].(string)
		if !ok {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "analysis parameter must be a string",
			}, nil
		}

		// Generate strategic approach
		phases := []map[string]string{
			{
				"phase":  "Phase 1: Assessment",
				"action": "Evaluate current state and gather baseline metrics",
			},
			{
				"phase":  "Phase 2: Planning",
				"action": "Design optimization approach and allocate resources",
			},
			{
				"phase":  "Phase 3: Implementation",
				"action": "Execute planned optimizations and monitor progress",
			},
		}

		result := map[string]interface{}{
			"approach":        "Phased implementation with continuous monitoring",
			"phases":          phases,
			"timeline":        "2-4 weeks for full implementation",
			"resources":       []string{"Technical team", "Monitoring tools", "Testing environment"},
			"success_metrics": []string{"Performance improvement >20%", "Error rate <1%", "User satisfaction >90%"},
		}

		return &interfaces.ToolOutput{
			Success: true,
			Result:  result,
		}, nil
	}

	return tools.NewBaseTool(
		"formulate_strategy",
		"Create a strategic approach with phases, timeline, resources, and success metrics",
		schema,
		runFunc,
	)
}

// createPrioritizeTasksTool creates a tool that prioritizes tasks
func createPrioritizeTasksTool() interfaces.Tool {
	schema := `{
		"type": "object",
		"properties": {
			"tasks": {
				"type": "array",
				"items": {"type": "string"},
				"description": "List of tasks to prioritize"
			}
		},
		"required": ["tasks"]
	}`

	runFunc := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		tasksRaw, ok := input.Args["tasks"]
		if !ok {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "tasks parameter is required",
			}, nil
		}

		// Convert to string slice
		var tasks []string
		switch v := tasksRaw.(type) {
		case []interface{}:
			for _, t := range v {
				if str, ok := t.(string); ok {
					tasks = append(tasks, str)
				}
			}
		case []string:
			tasks = v
		default:
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "tasks must be an array of strings",
			}, nil
		}

		// Prioritize tasks with impact and effort assessment
		prioritized := []map[string]string{}
		priorities := []string{"High", "Medium", "High", "Low", "Medium"}
		impacts := []string{"High", "Medium", "High", "Low", "High"}
		efforts := []string{"Low", "Medium", "Medium", "Low", "High"}

		for i, task := range tasks {
			priorityIdx := i % len(priorities)
			prioritized = append(prioritized, map[string]string{
				"priority": priorities[priorityIdx],
				"task":     task,
				"impact":   impacts[priorityIdx],
				"effort":   efforts[priorityIdx],
			})
		}

		return &interfaces.ToolOutput{
			Success: true,
			Result:  prioritized,
		}, nil
	}

	return tools.NewBaseTool(
		"prioritize_tasks",
		"Prioritize tasks by impact and effort, returning ordered list with priority levels",
		schema,
		runFunc,
	)
}

// ==================== Execution Tools ====================

// createHTTPRequestTool creates a tool that makes HTTP requests
func createHTTPRequestTool() interfaces.Tool {
	schema := `{
		"type": "object",
		"properties": {
			"method": {
				"type": "string",
				"enum": ["GET", "POST", "PUT", "DELETE", "PATCH"],
				"description": "HTTP method"
			},
			"url": {
				"type": "string",
				"description": "URL to request"
			},
			"headers": {
				"type": "object",
				"description": "Request headers (optional)"
			},
			"body": {
				"type": "object",
				"description": "Request body for POST/PUT/PATCH (optional)"
			}
		},
		"required": ["method", "url"]
	}`

	runFunc := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		method, ok := input.Args["method"].(string)
		if !ok {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "method parameter must be a string",
			}, nil
		}

		url, ok := input.Args["url"].(string)
		if !ok {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "url parameter must be a string",
			}, nil
		}

		// Create HTTP client with timeout
		client := httpclient.NewClient(&httpclient.Config{
			Timeout: 10 * time.Second,
		})

		// Prepare request
		req := client.R().SetContext(ctx)

		// Add custom headers
		if headers, ok := input.Args["headers"].(map[string]interface{}); ok {
			headerMap := make(map[string]string)
			for key, value := range headers {
				if strValue, ok := value.(string); ok {
					headerMap[key] = strValue
				}
			}
			req.SetHeaders(headerMap)
		}

		// Set request body for POST/PUT/PATCH
		if method == "POST" || method == "PUT" || method == "PATCH" {
			bodyData := input.Args["body"]
			if bodyData != nil {
				req.SetBody(bodyData).
					SetHeader("Content-Type", "application/json")
			}
		}

		// Execute request
		resp, err := req.Execute(method, url)
		if err != nil {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   fmt.Sprintf("request failed: %v", err),
			}, nil
		}

		// Parse JSON response if possible
		var respBody interface{}
		if err := json.Unmarshal(resp.Body(), &respBody); err != nil {
			respBody = string(resp.Body())
		}

		// Collect response headers
		respHeaders := make(map[string]string)
		for key, values := range resp.Header() {
			if len(values) > 0 {
				respHeaders[key] = values[0]
			}
		}

		result := map[string]interface{}{
			"status_code": resp.StatusCode(),
			"body":        respBody,
			"headers":     respHeaders,
		}

		return &interfaces.ToolOutput{
			Success: true,
			Result:  result,
		}, nil
	}

	return tools.NewBaseTool(
		"http_request",
		"Make HTTP requests (GET, POST, PUT, DELETE, PATCH) to external APIs",
		schema,
		runFunc,
	)
}

// createExecuteCommandTool creates a tool that simulates command execution
func createExecuteCommandTool() interfaces.Tool {
	schema := `{
		"type": "object",
		"properties": {
			"command": {
				"type": "string",
				"description": "Command to execute"
			}
		},
		"required": ["command"]
	}`

	runFunc := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		command, ok := input.Args["command"].(string)
		if !ok {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "command parameter must be a string",
			}, nil
		}

		// SAFETY: Simulate command execution instead of actually running it
		// In production, you would add security checks and use exec.CommandContext

		// Simulate different command outputs
		var output string
		var status string

		switch {
		case strings.Contains(command, "ls"):
			output = "file1.txt\nfile2.txt\nconfig.json"
			status = "success"
		case strings.Contains(command, "ps"):
			output = "PID   COMMAND\n1234  app_server\n5678  worker"
			status = "success"
		case strings.Contains(command, "ping"):
			output = "PING successful: 5 packets transmitted, 5 received"
			status = "success"
		default:
			output = fmt.Sprintf("Simulated execution of: %s", command)
			status = "simulated"
		}

		result := map[string]interface{}{
			"command": command,
			"status":  status,
			"output":  output,
			"note":    "Command execution is simulated for safety",
		}

		return &interfaces.ToolOutput{
			Success: true,
			Result:  result,
		}, nil
	}

	return tools.NewBaseTool(
		"execute_command",
		"Execute system commands (simulated for safety). In production, implement with proper security checks.",
		schema,
		runFunc,
	)
}

// createFileOperationsTool creates a tool for file operations
func createFileOperationsTool() interfaces.Tool {
	schema := `{
		"type": "object",
		"properties": {
			"operation": {
				"type": "string",
				"enum": ["read", "write", "list", "delete"],
				"description": "File operation to perform"
			},
			"path": {
				"type": "string",
				"description": "File or directory path"
			},
			"content": {
				"type": "string",
				"description": "Content to write (for write operation)"
			}
		},
		"required": ["operation", "path"]
	}`

	runFunc := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		operation, ok := input.Args["operation"].(string)
		if !ok {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "operation parameter must be a string",
			}, nil
		}

		path, ok := input.Args["path"].(string)
		if !ok {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "path parameter must be a string",
			}, nil
		}

		// SAFETY: Simulate file operations instead of actually performing them
		// In production, add security checks and use os package functions

		var result map[string]interface{}

		switch operation {
		case "read":
			result = map[string]interface{}{
				"operation": "read",
				"path":      path,
				"content":   "Simulated file content for: " + path,
				"size":      "1024 bytes",
				"note":      "File operation is simulated for safety",
			}

		case "write":
			content, _ := input.Args["content"].(string)
			result = map[string]interface{}{
				"operation": "write",
				"path":      path,
				"bytes":     len(content),
				"status":    "simulated",
				"note":      "File operation is simulated for safety",
			}

		case "list":
			result = map[string]interface{}{
				"operation": "list",
				"path":      path,
				"files":     []string{"file1.txt", "file2.json", "config.yaml"},
				"count":     3,
				"note":      "File operation is simulated for safety",
			}

		case "delete":
			result = map[string]interface{}{
				"operation": "delete",
				"path":      path,
				"status":    "simulated",
				"note":      "File operation is simulated for safety",
			}

		default:
			return &interfaces.ToolOutput{
				Success: false,
				Error:   fmt.Sprintf("unknown operation: %s", operation),
			}, nil
		}

		return &interfaces.ToolOutput{
			Success: true,
			Result:  result,
		}, nil
	}

	return tools.NewBaseTool(
		"file_operations",
		"Perform file operations (read, write, list, delete) - simulated for safety",
		schema,
		runFunc,
	)
}

// Helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
