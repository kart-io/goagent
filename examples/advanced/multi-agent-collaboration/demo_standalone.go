package main

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/utils/json"
	"time"

	"github.com/kart-io/goagent/interfaces"
)

// RunDemo demonstrates the multi-agent workflow without requiring an LLM
func RunDemo() {
	fmt.Println("========================================")
	fmt.Println("=== Multi-Agent Demo (No LLM Required) ===")
	fmt.Println("========================================")
	fmt.Println()
	fmt.Println("This demonstration shows the multi-agent workflow")
	fmt.Println("without requiring an LLM connection.")
	fmt.Println()

	task := "Analyze website performance data and optimize loading times for an e-commerce platform"
	fmt.Printf("Task: %s\n", task)
	fmt.Println("========================================")
	fmt.Println()

	ctx := context.Background()

	// Simulate the three-agent workflow
	analysisResult := runAnalysisDemo(ctx, task)
	strategyResult := runStrategyDemo(ctx, analysisResult)
	_ = runExecutionDemo(ctx, strategyResult)

	// Summary
	fmt.Println("========================================")
	fmt.Println("📊 Multi-Agent Demo Complete!")
	fmt.Println("========================================")
	fmt.Println()
	fmt.Println("This demonstration showed how three specialized agents")
	fmt.Println("collaborate to solve complex tasks:")
	fmt.Println()
	fmt.Println("  1. 🔍 Analysis Agent")
	fmt.Println("     - Analyzed the task")
	fmt.Println("     - Identified complexity and themes")
	fmt.Println("     - Highlighted risks and opportunities")
	fmt.Println()
	fmt.Println("  2. 📋 Strategy Agent")
	fmt.Println("     - Formulated strategic approach")
	fmt.Println("     - Created phased implementation plan")
	fmt.Println("     - Prioritized tasks by impact")
	fmt.Println()
	fmt.Println("  3. ⚡ Execution Agent")
	fmt.Println("     - Executed the strategy")
	fmt.Println("     - Made API calls")
	fmt.Println("     - Performed file operations")
	fmt.Println()
	fmt.Println("To run with a real LLM:")
	fmt.Println("  • Install Ollama: https://ollama.ai")
	fmt.Println("  • Or set OPENAI_API_KEY or GEMINI_API_KEY")
	fmt.Println("  • Then run: ./run.sh or go run main_simple.go")
	fmt.Println()
	fmt.Println("✨ Demo completed successfully!")
	fmt.Println()
}

func runAnalysisDemo(ctx context.Context, task string) map[string]interface{} {
	fmt.Println("🔍 Analysis Agent: Analyzing the task...")
	time.Sleep(500 * time.Millisecond) // Simulate processing

	// Demonstrate data_analysis tool
	analysisInput := map[string]interface{}{
		"data": task,
	}
	fmt.Println("\nCalling tool: data_analysis")
	printJSON(analysisInput)

	analysisTool := createDataAnalysisTool()
	toolInput := &interfaces.ToolInput{
		Args:    analysisInput,
		Context: ctx,
	}

	analysisOutput, _ := analysisTool.Invoke(ctx, toolInput)
	analysisData := analysisOutput.Result.(map[string]interface{})

	fmt.Println("\nTool result:")
	printJSON(analysisData)

	// Demonstrate summarize tool
	fmt.Println("\nCalling tool: summarize")
	summarizeInput := map[string]interface{}{
		"text": "Website performance optimization for e-commerce platform. Focus on loading times, user experience, and conversion rates. Critical areas include image optimization, caching strategies, and CDN configuration.",
	}
	printJSON(summarizeInput)

	summarizeTool := createSummarizeTool()
	summaryToolInput := &interfaces.ToolInput{
		Args:    summarizeInput,
		Context: ctx,
	}

	summaryOutput, _ := summarizeTool.Invoke(ctx, summaryToolInput)
	summaryData := summaryOutput.Result.(map[string]interface{})

	fmt.Println("\nTool result:")
	printJSON(summaryData)

	result := map[string]interface{}{
		"analysis": analysisData,
		"summary":  summaryData,
	}

	fmt.Printf("\n✓ Analysis completed\n")
	fmt.Println("\n--- Analysis Results ---")
	printJSON(result)
	fmt.Println()

	return result
}

func runStrategyDemo(ctx context.Context, analysisResult map[string]interface{}) map[string]interface{} {
	fmt.Println("📋 Strategy Agent: Formulating strategy...")
	time.Sleep(500 * time.Millisecond) // Simulate processing

	// Demonstrate formulate_strategy tool
	fmt.Println("\nCalling tool: formulate_strategy")
	strategyInput := map[string]interface{}{
		"analysis": fmt.Sprintf("%v", analysisResult),
	}
	printJSON(strategyInput)

	strategyTool := createFormulateStrategyTool()
	toolInput := &interfaces.ToolInput{
		Args:    strategyInput,
		Context: ctx,
	}

	strategyOutput, _ := strategyTool.Invoke(ctx, toolInput)
	strategyData := strategyOutput.Result.(map[string]interface{})

	fmt.Println("\nTool result:")
	printJSON(strategyData)

	// Demonstrate prioritize_tasks tool
	fmt.Println("\nCalling tool: prioritize_tasks")
	tasks := []string{
		"Implement image compression",
		"Configure CDN caching",
		"Optimize database queries",
		"Enable browser caching",
		"Minify CSS and JavaScript",
	}
	prioritizeInput := map[string]interface{}{
		"tasks": tasks,
	}
	printJSON(prioritizeInput)

	prioritizeTool := createPrioritizeTasksTool()
	prioritizeToolInput := &interfaces.ToolInput{
		Args:    prioritizeInput,
		Context: ctx,
	}

	prioritizeOutput, _ := prioritizeTool.Invoke(ctx, prioritizeToolInput)
	prioritizedTasks := prioritizeOutput.Result

	fmt.Println("\nTool result:")
	printJSON(prioritizedTasks)

	result := map[string]interface{}{
		"strategy":          strategyData,
		"prioritized_tasks": prioritizedTasks,
	}

	fmt.Printf("\n✓ Strategy formulated\n")
	fmt.Println("\n--- Strategy Results ---")
	printJSON(result)
	fmt.Println()

	return result
}

func runExecutionDemo(ctx context.Context, strategyResult map[string]interface{}) map[string]interface{} {
	fmt.Println("⚡ Execution Agent: Executing the strategy...")
	time.Sleep(500 * time.Millisecond) // Simulate processing

	executionResults := []map[string]interface{}{}

	// Demonstrate http_request tool
	fmt.Println("\nCalling tool: http_request")
	httpInput := map[string]interface{}{
		"method": "GET",
		"url":    "https://jsonplaceholder.typicode.com/posts/1",
	}
	printJSON(httpInput)

	httpTool := createHTTPRequestTool()
	httpToolInput := &interfaces.ToolInput{
		Args:    httpInput,
		Context: ctx,
	}

	httpOutput, err := httpTool.Invoke(ctx, httpToolInput)
	if err == nil && httpOutput.Success {
		httpData := httpOutput.Result.(map[string]interface{})
		fmt.Println("\nTool result:")
		printJSONLimited(httpData, 10)
		executionResults = append(executionResults, map[string]interface{}{
			"tool":   "http_request",
			"status": "success",
		})
	}

	// Demonstrate execute_command tool
	fmt.Println("\nCalling tool: execute_command")
	cmdInput := map[string]interface{}{
		"command": "ls -la /var/www/html",
	}
	printJSON(cmdInput)

	cmdTool := createExecuteCommandTool()
	cmdToolInput := &interfaces.ToolInput{
		Args:    cmdInput,
		Context: ctx,
	}

	cmdOutput, _ := cmdTool.Invoke(ctx, cmdToolInput)
	cmdData := cmdOutput.Result.(map[string]interface{})

	fmt.Println("\nTool result:")
	printJSON(cmdData)
	executionResults = append(executionResults, map[string]interface{}{
		"tool":   "execute_command",
		"status": "simulated",
	})

	// Demonstrate file_operations tool
	fmt.Println("\nCalling tool: file_operations")
	fileInput := map[string]interface{}{
		"operation": "list",
		"path":      "/var/www/html/assets",
	}
	printJSON(fileInput)

	fileTool := createFileOperationsTool()
	fileToolInput := &interfaces.ToolInput{
		Args:    fileInput,
		Context: ctx,
	}

	fileOutput, _ := fileTool.Invoke(ctx, fileToolInput)
	fileData := fileOutput.Result.(map[string]interface{})

	fmt.Println("\nTool result:")
	printJSON(fileData)
	executionResults = append(executionResults, map[string]interface{}{
		"tool":   "file_operations",
		"status": "simulated",
	})

	result := map[string]interface{}{
		"execution_results": executionResults,
		"total_steps":       len(executionResults),
		"status":            "completed",
	}

	fmt.Printf("\n✓ Execution completed\n")
	fmt.Println("\n--- Execution Results ---")
	printJSON(result)
	fmt.Println()

	return result
}

// Use interfaces.ToolInput and interfaces.ToolOutput directly

// Helper functions
func printJSON(data interface{}) {
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Printf("%v\n", data)
		return
	}
	fmt.Println(string(jsonBytes))
}

func printJSONLimited(data interface{}, maxLines int) {
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Printf("%v\n", data)
		return
	}

	lines := splitLines(string(jsonBytes))
	if len(lines) > maxLines {
		for i := 0; i < maxLines; i++ {
			fmt.Println(lines[i])
		}
		fmt.Printf("... (%d more lines)\n", len(lines)-maxLines)
	} else {
		fmt.Println(string(jsonBytes))
	}
}

func splitLines(s string) []string {
	result := []string{}
	current := ""
	for _, c := range s {
		current += string(c)
		if c == '\n' {
			result = append(result, current)
			current = ""
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}
