package performance

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
)

// MockAgent 模拟 Agent 用于测试
type MockAgent struct {
	*core.BaseAgent
	executeDelay time.Duration
	executeCount int
	mu           sync.Mutex
}

// NewMockAgent 创建模拟 Agent
func NewMockAgent(name string, delay time.Duration) *MockAgent {
	return &MockAgent{
		BaseAgent:    core.NewBaseAgent(name, "Mock Agent for testing", []string{"test"}),
		executeDelay: delay,
	}
}

// Execute 模拟执行
func (m *MockAgent) Execute(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	m.mu.Lock()
	m.executeCount++
	count := m.executeCount
	m.mu.Unlock()

	// 模拟执行延迟
	if m.executeDelay > 0 {
		time.Sleep(m.executeDelay)
	}

	return &core.AgentOutput{
		Result:    fmt.Sprintf("Result #%d for task: %s", count, input.Task),
		Status:    "success",
		Message:   "Execution completed",
		Latency:   m.executeDelay,
		Timestamp: time.Now(),
		Metadata:  map[string]interface{}{"count": count},
	}, nil
}

// Invoke implements the Runnable interface by calling Execute
func (m *MockAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	return m.Execute(ctx, input)
}

// GetExecuteCount 获取执行次数
func (m *MockAgent) GetExecuteCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.executeCount
}

// BenchmarkPooledVsNonPooled 池化 vs 非池化性能对比
func BenchmarkPooledVsNonPooled(b *testing.B) {
	ctx := context.Background()
	input := &core.AgentInput{
		Task:        "Test task",
		Instruction: "Test instruction",
		Timestamp:   time.Now(),
	}

	b.Run("NonPooled", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			agent := NewMockAgent("test", 10*time.Microsecond)
			_, err := agent.Execute(ctx, input)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Pooled", func(b *testing.B) {
		factory := func() (core.Agent, error) {
			return NewMockAgent("test", 10*time.Microsecond), nil
		}
		config := PoolConfig{
			InitialSize:     10,
			MaxSize:         50,
			IdleTimeout:     1 * time.Minute,
			MaxLifetime:     10 * time.Minute,
			AcquireTimeout:  5 * time.Second,
			CleanupInterval: 30 * time.Second,
		}
		pool, err := NewAgentPool(factory, config)
		if err != nil {
			b.Fatal(err)
		}
		defer pool.Close()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, err := pool.Execute(ctx, input)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkCachedVsUncached 缓存 vs 非缓存性能对比
func BenchmarkCachedVsUncached(b *testing.B) {
	ctx := context.Background()
	input := &core.AgentInput{
		Task:        "Test task",
		Instruction: "Test instruction",
		Context:     map[string]interface{}{"key": "value"},
		Timestamp:   time.Now(),
	}

	b.Run("Uncached", func(b *testing.B) {
		agent := NewMockAgent("test", 1*time.Millisecond)
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, err := agent.Execute(ctx, input)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Cached", func(b *testing.B) {
		agent := NewMockAgent("test", 1*time.Millisecond)
		config := DefaultCacheConfig()
		config.TTL = 10 * time.Minute
		cachedAgent := NewCachedAgent(agent, config)
		defer cachedAgent.Close()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, err := cachedAgent.Invoke(ctx, input)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkBatchExecution 批量执行性能测试
func BenchmarkBatchExecution(b *testing.B) {
	ctx := context.Background()

	testCases := []struct {
		name        string
		taskCount   int
		concurrency int
	}{
		{"10Tasks_5Concurrent", 10, 5},
		{"100Tasks_10Concurrent", 100, 10},
		{"1000Tasks_20Concurrent", 1000, 20},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			agent := NewMockAgent("test", 100*time.Microsecond)
			config := BatchConfig{
				MaxConcurrency: tc.concurrency,
				Timeout:        1 * time.Minute,
				ErrorPolicy:    ErrorPolicyContinue,
				EnableStats:    true,
			}
			executor := NewBatchExecutor(agent, config)

			// 准备输入
			inputs := make([]*core.AgentInput, tc.taskCount)
			for i := 0; i < tc.taskCount; i++ {
				inputs[i] = &core.AgentInput{
					Task:        fmt.Sprintf("Task #%d", i),
					Instruction: "Test instruction",
					Timestamp:   time.Now(),
				}
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				result := executor.Execute(ctx, inputs)
				if result.Stats.SuccessCount != tc.taskCount {
					b.Fatalf("Expected %d successes, got %d", tc.taskCount, result.Stats.SuccessCount)
				}
			}
		})
	}
}

// BenchmarkConcurrentPoolAccess 并发池访问性能测试
func BenchmarkConcurrentPoolAccess(b *testing.B) {
	ctx := context.Background()
	input := &core.AgentInput{
		Task:        "Test task",
		Instruction: "Test instruction",
		Timestamp:   time.Now(),
	}

	factory := func() (core.Agent, error) {
		return NewMockAgent("test", 50*time.Microsecond), nil
	}
	config := PoolConfig{
		InitialSize:     10,
		MaxSize:         50,
		IdleTimeout:     1 * time.Minute,
		MaxLifetime:     10 * time.Minute,
		AcquireTimeout:  5 * time.Second,
		CleanupInterval: 30 * time.Second,
	}
	pool, err := NewAgentPool(factory, config)
	if err != nil {
		b.Fatal(err)
	}
	defer pool.Close()

	b.Run("1Goroutine", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := pool.Execute(ctx, input)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("10Goroutines", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, err := pool.Execute(ctx, input)
				if err != nil {
					b.Error(err)
				}
			}
		})
	})
}

// BenchmarkCacheHitRate 缓存命中率测试
func BenchmarkCacheHitRate(b *testing.B) {
	ctx := context.Background()

	// 不同缓存命中率的场景
	testCases := []struct {
		name        string
		uniqueTasks int // 唯一任务数
		totalTasks  int // 总任务数
	}{
		{"HighHitRate_90%", 10, 100},   // 90% 命中率
		{"MediumHitRate_50%", 50, 100}, // 50% 命中率
		{"LowHitRate_10%", 90, 100},    // 10% 命中率
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			agent := NewMockAgent("test", 1*time.Millisecond)
			config := DefaultCacheConfig()
			config.TTL = 10 * time.Minute
			cachedAgent := NewCachedAgent(agent, config)
			defer cachedAgent.Close()

			// 准备输入（重复模式）
			inputs := make([]*core.AgentInput, tc.totalTasks)
			for i := 0; i < tc.totalTasks; i++ {
				taskID := i % tc.uniqueTasks
				inputs[i] = &core.AgentInput{
					Task:        fmt.Sprintf("Task #%d", taskID),
					Instruction: "Test instruction",
					Timestamp:   time.Now(),
				}
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				for _, input := range inputs {
					_, err := cachedAgent.Invoke(ctx, input)
					if err != nil {
						b.Fatal(err)
					}
				}
			}

			// 报告缓存统计
			stats := cachedAgent.Stats()
			b.ReportMetric(stats.HitRate, "hit_rate_%")
			b.ReportMetric(float64(stats.AvgHitTime.Microseconds()), "avg_hit_time_us")
			b.ReportMetric(float64(stats.AvgMissTime.Microseconds()), "avg_miss_time_us")
		})
	}
}

// BenchmarkPoolWithDifferentSizes 不同池大小的性能测试
func BenchmarkPoolWithDifferentSizes(b *testing.B) {
	ctx := context.Background()
	input := &core.AgentInput{
		Task:        "Test task",
		Instruction: "Test instruction",
		Timestamp:   time.Now(),
	}

	poolSizes := []int{5, 10, 20, 50, 100}

	for _, size := range poolSizes {
		b.Run(fmt.Sprintf("PoolSize_%d", size), func(b *testing.B) {
			factory := func() (core.Agent, error) {
				return NewMockAgent("test", 10*time.Microsecond), nil
			}
			config := PoolConfig{
				InitialSize:     size / 2,
				MaxSize:         size,
				IdleTimeout:     1 * time.Minute,
				MaxLifetime:     10 * time.Minute,
				AcquireTimeout:  5 * time.Second,
				CleanupInterval: 30 * time.Second,
			}
			pool, err := NewAgentPool(factory, config)
			if err != nil {
				b.Fatal(err)
			}
			defer pool.Close()

			b.ResetTimer()
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					_, err := pool.Execute(ctx, input)
					if err != nil {
						b.Error(err)
					}
				}
			})

			// 报告池统计
			stats := pool.Stats()
			b.ReportMetric(stats.UtilizationPct, "utilization_%")
			b.ReportMetric(float64(stats.WaitCount), "wait_count")
		})
	}
}

// BenchmarkBatchErrorPolicies 批量执行错误策略性能对比
func BenchmarkBatchErrorPolicies(b *testing.B) {
	ctx := context.Background()
	taskCount := 100

	// 准备输入（包含一些会失败的任务）
	inputs := make([]*core.AgentInput, taskCount)
	for i := 0; i < taskCount; i++ {
		inputs[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Test instruction",
			Timestamp:   time.Now(),
		}
	}

	b.Run("FailFast", func(b *testing.B) {
		agent := NewMockAgent("test", 100*time.Microsecond)
		config := BatchConfig{
			MaxConcurrency: 10,
			Timeout:        1 * time.Minute,
			ErrorPolicy:    ErrorPolicyFailFast,
			EnableStats:    true,
		}
		executor := NewBatchExecutor(agent, config)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_ = executor.Execute(ctx, inputs)
		}
	})

	b.Run("Continue", func(b *testing.B) {
		agent := NewMockAgent("test", 100*time.Microsecond)
		config := BatchConfig{
			MaxConcurrency: 10,
			Timeout:        1 * time.Minute,
			ErrorPolicy:    ErrorPolicyContinue,
			EnableStats:    true,
		}
		executor := NewBatchExecutor(agent, config)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_ = executor.Execute(ctx, inputs)
		}
	})
}

// TestPerformanceReport 生成性能报告
func TestPerformanceReport(t *testing.T) {
	ctx := context.Background()

	t.Log("\n=== Agent Performance Report ===\n")

	// 1. 池化性能测试
	t.Run("PoolingPerformance", func(t *testing.T) {
		t.Log("\n--- Pooling Performance ---")

		// 非池化
		agent := NewMockAgent("test", 10*time.Microsecond)
		start := time.Now()
		for i := 0; i < 1000; i++ {
			input := &core.AgentInput{Task: "Test", Timestamp: time.Now()}
			_, err := agent.Execute(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
		}
		nonPooledTime := time.Since(start)
		t.Logf("Non-pooled (1000 executions): %v", nonPooledTime)

		// 池化
		factory := func() (core.Agent, error) {
			return NewMockAgent("test", 10*time.Microsecond), nil
		}
		pool, _ := NewAgentPool(factory, DefaultPoolConfig())
		defer pool.Close()

		start = time.Now()
		for i := 0; i < 1000; i++ {
			input := &core.AgentInput{Task: "Test", Timestamp: time.Now()}
			_, err := pool.Execute(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
		}
		pooledTime := time.Since(start)
		t.Logf("Pooled (1000 executions): %v", pooledTime)

		improvement := float64(nonPooledTime-pooledTime) / float64(nonPooledTime) * 100
		t.Logf("Performance improvement: %.2f%%", improvement)

		stats := pool.Stats()
		t.Logf("Pool stats: Total=%d, Active=%d, Idle=%d, Utilization=%.2f%%",
			stats.TotalCount, stats.ActiveCount, stats.IdleCount, stats.UtilizationPct)
	})

	// 2. 缓存性能测试
	t.Run("CachingPerformance", func(t *testing.T) {
		t.Log("\n--- Caching Performance ---")

		agent := NewMockAgent("test", 1*time.Millisecond)
		input := &core.AgentInput{
			Task:        "Test task",
			Instruction: "Test instruction",
			Timestamp:   time.Now(),
		}

		// 非缓存
		start := time.Now()
		for i := 0; i < 100; i++ {
			_, err := agent.Execute(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
		}
		uncachedTime := time.Since(start)
		t.Logf("Uncached (100 executions): %v", uncachedTime)

		// 缓存
		cachedAgent := NewCachedAgent(agent, DefaultCacheConfig())
		defer cachedAgent.Close()

		start = time.Now()
		for i := 0; i < 100; i++ {
			_, err := cachedAgent.Invoke(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
		}
		cachedTime := time.Since(start)
		t.Logf("Cached (100 executions): %v", cachedTime)

		improvement := float64(uncachedTime-cachedTime) / float64(uncachedTime) * 100
		t.Logf("Performance improvement: %.2f%%", improvement)

		stats := cachedAgent.Stats()
		t.Logf("Cache stats: Size=%d, Hits=%d, Misses=%d, HitRate=%.2f%%",
			stats.Size, stats.Hits, stats.Misses, stats.HitRate)
		t.Logf("Avg hit time: %v, Avg miss time: %v", stats.AvgHitTime, stats.AvgMissTime)
	})

	// 3. 批量执行性能测试
	t.Run("BatchExecutionPerformance", func(t *testing.T) {
		t.Log("\n--- Batch Execution Performance ---")

		agent := NewMockAgent("test", 100*time.Microsecond)

		// 准备 1000 个任务
		inputs := make([]*core.AgentInput, 1000)
		for i := 0; i < 1000; i++ {
			inputs[i] = &core.AgentInput{
				Task:        fmt.Sprintf("Task #%d", i),
				Instruction: "Test instruction",
				Timestamp:   time.Now(),
			}
		}

		// 串行执行
		start := time.Now()
		for _, input := range inputs {
			_, err := agent.Execute(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
		}
		serialTime := time.Since(start)
		t.Logf("Serial execution (1000 tasks): %v", serialTime)

		// 批量执行
		executor := NewBatchExecutor(agent, DefaultBatchConfig())
		start = time.Now()
		result := executor.Execute(ctx, inputs)
		batchTime := time.Since(start)
		t.Logf("Batch execution (1000 tasks, 10 concurrent): %v", batchTime)

		speedup := float64(serialTime) / float64(batchTime)
		t.Logf("Speedup: %.2fx", speedup)
		t.Logf("Batch stats: Success=%d, Failed=%d, AvgDuration=%v",
			result.Stats.SuccessCount, result.Stats.FailureCount, result.Stats.AvgDuration)
	})

	t.Log("\n=== End of Performance Report ===\n")
}
