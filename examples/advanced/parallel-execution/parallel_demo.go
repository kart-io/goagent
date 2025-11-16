package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

func main() {
	fmt.Println("=== Parallel Tool Execution Demo ===")

	// Demo 1: Basic Parallel Execution
	demo1BasicParallel()

	fmt.Println()

	// Demo 2: Sequential vs Parallel Comparison
	demo2PerformanceComparison()

	fmt.Println()

	// Demo 3: Concurrency Limit
	demo3ConcurrencyLimit()

	fmt.Println()

	// Demo 4: Error Handling
	demo4ErrorHandling()

	fmt.Println()

	// Demo 5: Retry Policy
	demo5RetryPolicy()

	fmt.Println()

	// Demo 6: Timeout Handling
	demo6TimeoutHandling()

	fmt.Println("\n=== Demo Complete ===")
}

// Demo 1: Basic parallel execution
func demo1BasicParallel() {
	fmt.Println("--- Demo 1: Basic Parallel Execution ---")

	ctx := context.Background()

	// Create executor
	executor := tools.NewToolExecutor(
		tools.WithMaxConcurrency(3),
	)

	// Create sample tools
	searchTool := createSimulatedTool("web_search", "Searching the web...", 100*time.Millisecond)
	calculateTool := createSimulatedTool("calculator", "Calculating...", 80*time.Millisecond)
	translateTool := createSimulatedTool("translator", "Translating...", 120*time.Millisecond)

	// Prepare tool calls
	calls := []*tools.ToolCall{
		{
			ID:    "call1",
			Tool:  searchTool,
			Input: &interfaces.ToolInput{Args: map[string]interface{}{"query": "Go programming"}},
		},
		{
			ID:    "call2",
			Tool:  calculateTool,
			Input: &interfaces.ToolInput{Args: map[string]interface{}{"expression": "10 + 20"}},
		},
		{
			ID:    "call3",
			Tool:  translateTool,
			Input: &interfaces.ToolInput{Args: map[string]interface{}{"text": "Hello", "to": "Spanish"}},
		},
	}

	// Execute in parallel
	start := time.Now()
	results, err := executor.ExecuteParallel(ctx, calls)
	duration := time.Since(start)

	fmt.Printf("Execution completed in %v\n", duration)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}

	// Display results
	fmt.Println("\nResults:")
	for _, result := range results {
		if result.Error != nil {
			fmt.Printf("  [%s] ERROR: %v\n", result.CallID, result.Error)
		} else {
			fmt.Printf("  [%s] SUCCESS: %v (took %v)\n", result.CallID, result.Output.Result, result.Duration)
		}
	}
}

// Demo 2: Sequential vs Parallel performance comparison
func demo2PerformanceComparison() {
	fmt.Println("--- Demo 2: Sequential vs Parallel Comparison ---")

	ctx := context.Background()

	// Create tools with simulated delays
	tool1 := createSimulatedTool("tool1", "Processing 1...", 100*time.Millisecond)
	tool2 := createSimulatedTool("tool2", "Processing 2...", 100*time.Millisecond)
	tool3 := createSimulatedTool("tool3", "Processing 3...", 100*time.Millisecond)
	tool4 := createSimulatedTool("tool4", "Processing 4...", 100*time.Millisecond)

	calls := []*tools.ToolCall{
		{ID: "call1", Tool: tool1, Input: &interfaces.ToolInput{Args: map[string]interface{}{}}},
		{ID: "call2", Tool: tool2, Input: &interfaces.ToolInput{Args: map[string]interface{}{}}},
		{ID: "call3", Tool: tool3, Input: &interfaces.ToolInput{Args: map[string]interface{}{}}},
		{ID: "call4", Tool: tool4, Input: &interfaces.ToolInput{Args: map[string]interface{}{}}},
	}

	// Sequential execution
	executorSeq := tools.NewToolExecutor()
	startSeq := time.Now()
	_, _ = executorSeq.ExecuteSequential(ctx, calls)
	durationSeq := time.Since(startSeq)

	// Parallel execution
	executorPar := tools.NewToolExecutor(tools.WithMaxConcurrency(4))
	startPar := time.Now()
	_, _ = executorPar.ExecuteParallel(ctx, calls)
	durationPar := time.Since(startPar)

	fmt.Printf("Sequential execution: %v\n", durationSeq)
	fmt.Printf("Parallel execution:   %v\n", durationPar)
	fmt.Printf("Speedup: %.2fx\n", float64(durationSeq)/float64(durationPar))
}

// Demo 3: Concurrency limit enforcement
func demo3ConcurrencyLimit() {
	fmt.Println("--- Demo 3: Concurrency Limit ---")

	ctx := context.Background()

	// Create 10 tools
	calls := make([]*tools.ToolCall, 10)
	for i := 0; i < 10; i++ {
		tool := createSimulatedTool(fmt.Sprintf("tool%d", i), fmt.Sprintf("Processing %d...", i), 100*time.Millisecond)
		calls[i] = &tools.ToolCall{
			ID:    fmt.Sprintf("call%d", i),
			Tool:  tool,
			Input: &interfaces.ToolInput{Args: map[string]interface{}{"index": i}},
		}
	}

	// Test with different concurrency limits
	limits := []int{2, 5, 10}
	for _, limit := range limits {
		executor := tools.NewToolExecutor(tools.WithMaxConcurrency(limit))

		start := time.Now()
		results, _ := executor.ExecuteParallel(ctx, calls)
		duration := time.Since(start)

		fmt.Printf("Concurrency %d: %d tools completed in %v\n", limit, len(results), duration)
	}
}

// Demo 4: Error handling in parallel execution
func demo4ErrorHandling() {
	fmt.Println("--- Demo 4: Error Handling ---")

	ctx := context.Background()
	executor := tools.NewToolExecutor(tools.WithMaxConcurrency(3))

	// Mix of successful and failing tools
	successTool := createSimulatedTool("success_tool", "Processing...", 50*time.Millisecond)
	failTool := createFailingTool("fail_tool", "This tool will fail")

	calls := []*tools.ToolCall{
		{ID: "call1", Tool: successTool, Input: &interfaces.ToolInput{Args: map[string]interface{}{}}},
		{ID: "call2", Tool: failTool, Input: &interfaces.ToolInput{Args: map[string]interface{}{}}},
		{ID: "call3", Tool: successTool, Input: &interfaces.ToolInput{Args: map[string]interface{}{}}},
		{ID: "call4", Tool: failTool, Input: &interfaces.ToolInput{Args: map[string]interface{}{}}},
	}

	results, _ := executor.ExecuteParallel(ctx, calls)

	// Count successes and failures
	successCount := 0
	failCount := 0
	for _, result := range results {
		if result.Error == nil {
			successCount++
		} else {
			failCount++
		}
	}

	fmt.Printf("Total: %d tools\n", len(results))
	fmt.Printf("Successful: %d\n", successCount)
	fmt.Printf("Failed: %d\n", failCount)

	// Show details
	fmt.Println("\nDetails:")
	for _, result := range results {
		if result.Error == nil {
			fmt.Printf("  [%s] ✓ Success\n", result.CallID)
		} else {
			fmt.Printf("  [%s] ✗ Failed: %v\n", result.CallID, result.Error)
		}
	}
}

// Demo 5: Retry policy
func demo5RetryPolicy() {
	fmt.Println("--- Demo 5: Retry Policy ---")

	ctx := context.Background()

	// Create executor with retry policy
	executor := tools.NewToolExecutor(
		tools.WithMaxConcurrency(2),
		tools.WithRetryPolicy(&tools.RetryPolicy{
			MaxRetries:      2,
			InitialDelay:    50 * time.Millisecond,
			MaxDelay:        200 * time.Millisecond,
			Multiplier:      2.0,
			RetryableErrors: []string{"temporary_failure"},
		}),
	)

	// Tool that fails on first attempt but succeeds on retry
	flakyTool := createFlakyTool("flaky_tool", 2) // Fails 2 times then succeeds

	calls := []*tools.ToolCall{
		{ID: "call1", Tool: flakyTool, Input: &interfaces.ToolInput{Args: map[string]interface{}{}}},
	}

	start := time.Now()
	results, _ := executor.ExecuteParallel(ctx, calls)
	duration := time.Since(start)

	fmt.Printf("Execution took %v (includes retries)\n", duration)
	for _, result := range results {
		if result.Error == nil {
			fmt.Printf("  [%s] Eventually succeeded after retries\n", result.CallID)
		} else {
			fmt.Printf("  [%s] Failed even after retries: %v\n", result.CallID, result.Error)
		}
	}
}

// Demo 6: Timeout handling
func demo6TimeoutHandling() {
	fmt.Println("--- Demo 6: Timeout Handling ---")

	ctx := context.Background()

	// Create executor with short timeout
	executor := tools.NewToolExecutor(
		tools.WithMaxConcurrency(2),
		tools.WithTimeout(100*time.Millisecond),
	)

	// Tools with different durations
	fastTool := createSimulatedTool("fast_tool", "Fast processing...", 50*time.Millisecond)
	slowTool := createSimulatedTool("slow_tool", "Slow processing...", 200*time.Millisecond)

	calls := []*tools.ToolCall{
		{ID: "call1", Tool: fastTool, Input: &interfaces.ToolInput{Args: map[string]interface{}{}}},
		{ID: "call2", Tool: slowTool, Input: &interfaces.ToolInput{Args: map[string]interface{}{}}},
	}

	results, _ := executor.ExecuteParallel(ctx, calls)

	fmt.Println("Results:")
	for _, result := range results {
		if result.Error == nil {
			fmt.Printf("  [%s] Completed in time (%v)\n", result.CallID, result.Duration)
		} else {
			fmt.Printf("  [%s] Timed out or failed: %v\n", result.CallID, result.Error)
		}
	}
}

// Helper functions to create sample tools

func createSimulatedTool(name, message string, delay time.Duration) interfaces.Tool {
	return tools.NewBaseTool(
		name,
		fmt.Sprintf("Simulated %s tool", name),
		"{}",
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			// Simulate processing
			select {
			case <-time.After(delay):
				// Normal completion
				result := fmt.Sprintf("%s completed successfully", message)
				return &interfaces.ToolOutput{
					Result:  result,
					Success: true,
				}, nil
			case <-ctx.Done():
				// Context cancelled (timeout)
				return nil, ctx.Err()
			}
		},
	)
}

func createFailingTool(name, errorMsg string) interfaces.Tool {
	return tools.NewBaseTool(
		name,
		fmt.Sprintf("Failing %s tool", name),
		"{}",
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			time.Sleep(10 * time.Millisecond)
			return nil, fmt.Errorf("%s", errorMsg)
		},
	)
}

func createFlakyTool(name string, failCount int) interfaces.Tool {
	attempts := 0
	return tools.NewBaseTool(
		name,
		fmt.Sprintf("Flaky %s tool", name),
		"{}",
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			attempts++
			if attempts <= failCount {
				time.Sleep(20 * time.Millisecond)
				return nil, fmt.Errorf("temporary_failure: attempt %d", attempts)
			}
			time.Sleep(20 * time.Millisecond)
			return &interfaces.ToolOutput{
				Result:  fmt.Sprintf("Success after %d attempts", attempts),
				Success: true,
			}, nil
		},
	)
}

// Unused example function - kept for reference
/*
func createRandomDelayTool(name string, minDelay, maxDelay time.Duration) interfaces.Tool {
	return tools.NewBaseTool(
		name,
		fmt.Sprintf("Random delay %s tool", name),
		"{}",
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			// Random delay between min and max
			delay := minDelay + time.Duration(rand.Int63n(int64(maxDelay-minDelay)))
			time.Sleep(delay)
			return &interfaces.ToolOutput{
				Result:  fmt.Sprintf("Completed with %v delay", delay),
				Success: true,
			}, nil
		},
	)
}
*/
