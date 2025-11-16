package performance

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/core"
)

// ExampleAgentPool demonstrates Agent pooling usage
func ExampleAgentPool() {
	ctx := context.Background()

	// Define agent factory
	factory := func() (core.Agent, error) {
		return NewMockAgent("example-agent", 10*time.Millisecond), nil
	}

	// Configure pool
	config := PoolConfig{
		InitialSize:     5,  // Pre-create 5 agents
		MaxSize:         20, // Max 20 agents
		IdleTimeout:     5 * time.Minute,
		MaxLifetime:     30 * time.Minute,
		AcquireTimeout:  5 * time.Second,
		CleanupInterval: 1 * time.Minute,
	}

	// Create pool
	pool, err := NewAgentPool(factory, config)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	// Execute tasks
	for i := 0; i < 10; i++ {
		input := &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Process this task",
			Timestamp:   time.Now(),
		}

		output, err := pool.Execute(ctx, input)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		fmt.Printf("Task %d: %s\n", i, output.Status)
	}

	// Check pool statistics
	stats := pool.Stats()
	fmt.Printf("\nPool Statistics:\n")
	fmt.Printf("  Total agents: %d\n", stats.TotalCount)
	fmt.Printf("  Active: %d, Idle: %d\n", stats.ActiveCount, stats.IdleCount)
	fmt.Printf("  Utilization: %.2f%%\n", stats.UtilizationPct)
	fmt.Printf("  Total acquisitions: %d\n", stats.AcquiredTotal)
}

// ExampleCachedAgent demonstrates caching usage
func ExampleCachedAgent() {
	ctx := context.Background()

	// Create base agent
	agent := NewMockAgent("example-agent", 1*time.Millisecond)

	// Configure cache
	config := CacheConfig{
		MaxSize:         1000,
		TTL:             10 * time.Minute,
		CleanupInterval: 1 * time.Minute,
		EnableStats:     true,
	}

	// Wrap with cache
	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	// Same input (will be cached)
	input := &core.AgentInput{
		Task:        "Analyze logs",
		Instruction: "Find errors in application logs",
		Context: map[string]interface{}{
			"timeRange": "last 1 hour",
		},
		Timestamp: time.Now(),
	}

	// First execution (cache miss)
	start := time.Now()
	output1, _ := cachedAgent.Invoke(ctx, input)
	duration1 := time.Since(start)
	fmt.Printf("First execution: %v (cache miss)\n", duration1)

	// Second execution (cache hit)
	start = time.Now()
	output2, _ := cachedAgent.Invoke(ctx, input)
	duration2 := time.Since(start)
	fmt.Printf("Second execution: %v (cache hit)\n", duration2)

	fmt.Printf("Speedup: %.2fx\n", float64(duration1)/float64(duration2))

	// Check cache statistics
	stats := cachedAgent.Stats()
	fmt.Printf("\nCache Statistics:\n")
	fmt.Printf("  Size: %d/%d\n", stats.Size, stats.MaxSize)
	fmt.Printf("  Hits: %d, Misses: %d\n", stats.Hits, stats.Misses)
	fmt.Printf("  Hit rate: %.2f%%\n", stats.HitRate)
	fmt.Printf("  Avg hit time: %v\n", stats.AvgHitTime)
	fmt.Printf("  Avg miss time: %v\n", stats.AvgMissTime)

	// Verify results are identical
	fmt.Printf("\nResults match: %v\n", output1.Result == output2.Result)
}

// ExampleBatchExecutor demonstrates batch execution
func ExampleBatchExecutor() {
	ctx := context.Background()

	// Create agent
	agent := NewMockAgent("example-agent", 100*time.Microsecond)

	// Configure batch executor
	config := BatchConfig{
		MaxConcurrency: 10,
		Timeout:        5 * time.Minute,
		ErrorPolicy:    ErrorPolicyContinue,
		EnableStats:    true,
	}

	executor := NewBatchExecutor(agent, config)

	// Prepare batch inputs
	inputs := make([]*core.AgentInput, 100)
	for i := 0; i < 100; i++ {
		inputs[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Process item #%d", i),
			Instruction: "Perform analysis",
			Timestamp:   time.Now(),
		}
	}

	// Execute batch
	fmt.Println("Executing 100 tasks with 10 concurrent workers...")
	start := time.Now()
	result := executor.Execute(ctx, inputs)
	duration := time.Since(start)

	// Print results
	fmt.Printf("\nBatch Execution Results:\n")
	fmt.Printf("  Total tasks: %d\n", result.Stats.TotalCount)
	fmt.Printf("  Successful: %d\n", result.Stats.SuccessCount)
	fmt.Printf("  Failed: %d\n", result.Stats.FailureCount)
	fmt.Printf("  Total duration: %v\n", duration)
	fmt.Printf("  Avg duration per task: %v\n", result.Stats.AvgDuration)
	fmt.Printf("  Min duration: %v\n", result.Stats.MinDuration)
	fmt.Printf("  Max duration: %v\n", result.Stats.MaxDuration)

	// Calculate theoretical serial time
	serialTime := time.Duration(100) * 100 * time.Microsecond
	speedup := float64(serialTime) / float64(duration)
	fmt.Printf("  Speedup vs serial: %.2fx\n", speedup)
}

// Example_combinedOptimizations demonstrates using all optimizations together
func Example_combinedOptimizations() {
	ctx := context.Background()

	fmt.Println("=== Combined Performance Optimizations Demo ===")

	// 1. Create agent pool
	factory := func() (core.Agent, error) {
		agent := NewMockAgent("combined-agent", 10*time.Millisecond)
		// Wrap with cache
		config := CacheConfig{
			MaxSize:     1000,
			TTL:         10 * time.Minute,
			EnableStats: true,
		}
		return NewCachedAgent(agent, config), nil
	}

	poolConfig := PoolConfig{
		InitialSize: 5,
		MaxSize:     20,
		IdleTimeout: 5 * time.Minute,
	}

	pool, err := NewAgentPool(factory, poolConfig)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	// 2. Create batch executor with pooled agents
	batchConfig := BatchConfig{
		MaxConcurrency: 10,
		Timeout:        5 * time.Minute,
		ErrorPolicy:    ErrorPolicyContinue,
		EnableStats:    true,
	}

	// Custom agent wrapper that uses pool
	pooledAgent := &poolAgent{pool: pool}
	executor := NewBatchExecutor(pooledAgent, batchConfig)

	// 3. Prepare batch with some repeated tasks (to benefit from cache)
	inputs := make([]*core.AgentInput, 50)
	for i := 0; i < 50; i++ {
		// Every 5th task is repeated
		taskID := i / 5
		inputs[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Analyze resource #%d", taskID),
			Instruction: "Check for anomalies",
			Context: map[string]interface{}{
				"resourceID": taskID,
			},
			Timestamp: time.Now(),
		}
	}

	// 4. Execute batch
	fmt.Println("Executing 50 tasks (with repeats) using pool + cache + batch...")
	start := time.Now()
	result := executor.Execute(ctx, inputs)
	duration := time.Since(start)

	// 5. Print comprehensive results
	fmt.Printf("\n--- Results ---\n")
	fmt.Printf("Execution time: %v\n", duration)
	fmt.Printf("Success rate: %d/%d (%.2f%%)\n",
		result.Stats.SuccessCount,
		result.Stats.TotalCount,
		float64(result.Stats.SuccessCount)/float64(result.Stats.TotalCount)*100)

	// Pool statistics
	poolStats := pool.Stats()
	fmt.Printf("\n--- Pool Statistics ---\n")
	fmt.Printf("Agents: %d/%d (%.2f%% utilization)\n",
		poolStats.ActiveCount, poolStats.MaxSize, poolStats.UtilizationPct)
	fmt.Printf("Total acquisitions: %d\n", poolStats.AcquiredTotal)
	fmt.Printf("Avg wait time: %v\n", poolStats.AvgWaitTime)

	// Batch executor statistics
	execStats := executor.Stats()
	fmt.Printf("\n--- Batch Executor Statistics ---\n")
	fmt.Printf("Total executions: %d\n", execStats.TotalExecutions)
	fmt.Printf("Total tasks: %d\n", execStats.TotalTasks)
	fmt.Printf("Success rate: %.2f%%\n", execStats.SuccessRate)
	fmt.Printf("Avg execution time: %v\n", execStats.AvgDuration)

	fmt.Println("\n=== Demo Complete ===")
}

// poolAgent wraps a pool to implement Agent interface
type poolAgent struct {
	pool *AgentPool
}

func (p *poolAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	return p.pool.Execute(ctx, input)
}

func (p *poolAgent) Name() string {
	return "pooled-agent"
}

func (p *poolAgent) Description() string {
	return "Agent backed by a pool"
}

func (p *poolAgent) Capabilities() []string {
	return []string{"pooled", "cached"}
}

// Stream 流式执行 Agent（通过池获取 agent 并执行）
func (p *poolAgent) Stream(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[*core.AgentOutput], error) {
	agent, err := p.pool.Acquire(ctx)
	if err != nil {
		outChan := make(chan core.StreamChunk[*core.AgentOutput], 1)
		outChan <- core.StreamChunk[*core.AgentOutput]{Error: err, Done: true}
		close(outChan)
		return outChan, err
	}
	defer p.pool.Release(agent)
	return agent.Stream(ctx, input)
}

// Batch 批量执行 Agent（通过池获取 agent 并执行）
func (p *poolAgent) Batch(ctx context.Context, inputs []*core.AgentInput) ([]*core.AgentOutput, error) {
	agent, err := p.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer p.pool.Release(agent)
	return agent.Batch(ctx, inputs)
}

// Pipe 连接到另一个 Runnable（通过池获取 agent 并执行）
func (p *poolAgent) Pipe(next core.Runnable[*core.AgentOutput, any]) core.Runnable[*core.AgentInput, any] {
	// 为了简化，返回一个函数式 Runnable
	return core.NewRunnableFunc(func(ctx context.Context, input *core.AgentInput) (any, error) {
		output, err := p.Invoke(ctx, input)
		if err != nil {
			return nil, err
		}
		return next.Invoke(ctx, output)
	})
}

// WithCallbacks 添加回调处理器（返回新实例）
func (p *poolAgent) WithCallbacks(callbacks ...core.Callback) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	// poolAgent 不支持回调，返回自身
	return p
}

// WithConfig 配置 Agent（返回新实例）
func (p *poolAgent) WithConfig(config core.RunnableConfig) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	// poolAgent 不支持配置，返回自身
	return p
}

// Example_streamingBatch demonstrates streaming batch execution
func Example_streamingBatch() {
	ctx := context.Background()

	agent := NewMockAgent("streaming-agent", 50*time.Millisecond)
	config := BatchConfig{
		MaxConcurrency: 5,
		Timeout:        5 * time.Minute,
		ErrorPolicy:    ErrorPolicyContinue,
	}
	executor := NewBatchExecutor(agent, config)

	// Prepare inputs
	inputs := make([]*core.AgentInput, 20)
	for i := 0; i < 20; i++ {
		inputs[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Stream task #%d", i),
			Instruction: "Process",
			Timestamp:   time.Now(),
		}
	}

	// Execute with streaming results
	fmt.Println("Executing tasks with streaming results...")
	resultChan, errorChan := executor.ExecuteStream(ctx, inputs)

	successCount := 0
	errorCount := 0

	// Process results as they arrive
	done := false
	for !done {
		select {
		case output, ok := <-resultChan:
			if !ok {
				done = true
				break
			}
			if output != nil {
				successCount++
				fmt.Printf("✓ Task completed: %s\n", output.Status)
			}
		case err, ok := <-errorChan:
			if !ok {
				break
			}
			errorCount++
			fmt.Printf("✗ Task failed (index %d): %v\n", err.Index, err.Error)
		}
	}

	fmt.Printf("\nStreaming complete: %d successes, %d errors\n", successCount, errorCount)
}

// Example_customCacheKey demonstrates custom cache key generation
func Example_customCacheKey() {
	ctx := context.Background()

	agent := NewMockAgent("custom-cache-agent", 1*time.Millisecond)

	// Custom key generator that ignores timestamp
	customKeyGen := func(input *core.AgentInput) string {
		return fmt.Sprintf("%s:%s", input.Task, input.Instruction)
	}

	config := CacheConfig{
		MaxSize:      1000,
		TTL:          10 * time.Minute,
		EnableStats:  true,
		KeyGenerator: customKeyGen,
	}

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	// Execute with different timestamps but same task
	for i := 0; i < 5; i++ {
		input := &core.AgentInput{
			Task:        "Same task",
			Instruction: "Same instruction",
			Timestamp:   time.Now(), // Different timestamp
		}

		start := time.Now()
		_, _ = cachedAgent.Invoke(ctx, input)
		duration := time.Since(start)

		if i == 0 {
			fmt.Printf("Execution %d: %v (cache miss)\n", i+1, duration)
		} else {
			fmt.Printf("Execution %d: %v (cache hit)\n", i+1, duration)
		}
	}

	stats := cachedAgent.Stats()
	fmt.Printf("\nCache hit rate: %.2f%%\n", stats.HitRate)
}
