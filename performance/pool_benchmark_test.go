package performance

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
)

// mockAgent 用于测试的模拟 Agent
type mockAgent struct {
	*core.BaseAgent
	id string
}

// mockFactory 创建模拟 Agent
func mockFactory() (core.Agent, error) {
	agent := &mockAgent{
		BaseAgent: core.NewBaseAgent("mock", "mock agent for testing", []string{}),
		id:        "mock",
	}
	return agent, nil
}

// 重写 Invoke 方法
func (m *mockAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	// 模拟一些工作
	time.Sleep(10 * time.Microsecond)
	return &core.AgentOutput{
		Result: "mock result",
		Status: "success",
	}, nil
}

// BenchmarkOriginalPool_Acquire_Sequential 原始池顺序获取基准测试
func BenchmarkOriginalPool_Acquire_Sequential(b *testing.B) {
	pool, _ := NewAgentPool(mockFactory, PoolConfig{
		InitialSize:     10,
		MaxSize:         100,
		AcquireTimeout:  1 * time.Second,
		CleanupInterval: 1 * time.Minute,
	})
	defer pool.Close()

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		agent, err := pool.Acquire(ctx)
		if err != nil {
			b.Fatal(err)
		}
		pool.Release(agent)
	}
}

// BenchmarkOptimizedPool_Acquire_Sequential 优化池顺序获取基准测试
func BenchmarkOptimizedPool_Acquire_Sequential(b *testing.B) {
	pool, _ := NewOptimizedAgentPool(mockFactory, PoolConfig{
		InitialSize:     10,
		MaxSize:         100,
		AcquireTimeout:  1 * time.Second,
		CleanupInterval: 1 * time.Minute,
	})
	defer pool.Close()

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		agent, err := pool.Acquire(ctx)
		if err != nil {
			b.Fatal(err)
		}
		pool.Release(agent)
	}
}

// BenchmarkOriginalPool_Acquire_Parallel 原始池并发获取基准测试
func BenchmarkOriginalPool_Acquire_Parallel(b *testing.B) {
	pool, _ := NewAgentPool(mockFactory, PoolConfig{
		InitialSize:     10,
		MaxSize:         100,
		AcquireTimeout:  1 * time.Second,
		CleanupInterval: 1 * time.Minute,
	})
	defer pool.Close()

	ctx := context.Background()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			agent, err := pool.Acquire(ctx)
			if err != nil {
				b.Error(err)
				continue
			}
			pool.Release(agent)
		}
	})
}

// BenchmarkOptimizedPool_Acquire_Parallel 优化池并发获取基准测试
func BenchmarkOptimizedPool_Acquire_Parallel(b *testing.B) {
	pool, _ := NewOptimizedAgentPool(mockFactory, PoolConfig{
		InitialSize:     10,
		MaxSize:         100,
		AcquireTimeout:  1 * time.Second,
		CleanupInterval: 1 * time.Minute,
	})
	defer pool.Close()

	ctx := context.Background()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			agent, err := pool.Acquire(ctx)
			if err != nil {
				b.Error(err)
				continue
			}
			pool.Release(agent)
		}
	})
}

// BenchmarkOriginalPool_HighContention 原始池高竞争场景（小池大并发）
func BenchmarkOriginalPool_HighContention(b *testing.B) {
	pool, _ := NewAgentPool(mockFactory, PoolConfig{
		InitialSize:     5,
		MaxSize:         10,
		AcquireTimeout:  1 * time.Second,
		CleanupInterval: 1 * time.Minute,
	})
	defer pool.Close()

	ctx := context.Background()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			agent, err := pool.Acquire(ctx)
			if err != nil {
				b.Error(err)
				continue
			}
			// 模拟一些工作
			time.Sleep(100 * time.Microsecond)
			pool.Release(agent)
		}
	})
}

// BenchmarkOptimizedPool_HighContention 优化池高竞争场景（小池大并发）
func BenchmarkOptimizedPool_HighContention(b *testing.B) {
	pool, _ := NewOptimizedAgentPool(mockFactory, PoolConfig{
		InitialSize:     5,
		MaxSize:         10,
		AcquireTimeout:  1 * time.Second,
		CleanupInterval: 1 * time.Minute,
	})
	defer pool.Close()

	ctx := context.Background()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			agent, err := pool.Acquire(ctx)
			if err != nil {
				b.Error(err)
				continue
			}
			// 模拟一些工作
			time.Sleep(100 * time.Microsecond)
			pool.Release(agent)
		}
	})
}

// BenchmarkOriginalPool_LargePool 原始池大池场景（1000 agents）
func BenchmarkOriginalPool_LargePool(b *testing.B) {
	pool, _ := NewAgentPool(mockFactory, PoolConfig{
		InitialSize:     100,
		MaxSize:         1000,
		AcquireTimeout:  1 * time.Second,
		CleanupInterval: 1 * time.Minute,
	})
	defer pool.Close()

	ctx := context.Background()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			agent, err := pool.Acquire(ctx)
			if err != nil {
				b.Error(err)
				continue
			}
			pool.Release(agent)
		}
	})
}

// BenchmarkOptimizedPool_LargePool 优化池大池场景（1000 agents）
func BenchmarkOptimizedPool_LargePool(b *testing.B) {
	pool, _ := NewOptimizedAgentPool(mockFactory, PoolConfig{
		InitialSize:     100,
		MaxSize:         1000,
		AcquireTimeout:  1 * time.Second,
		CleanupInterval: 1 * time.Minute,
	})
	defer pool.Close()

	ctx := context.Background()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			agent, err := pool.Acquire(ctx)
			if err != nil {
				b.Error(err)
				continue
			}
			pool.Release(agent)
		}
	})
}

// BenchmarkOriginalPool_Execute 原始池 Execute 方法基准测试
func BenchmarkOriginalPool_Execute(b *testing.B) {
	pool, _ := NewAgentPool(mockFactory, PoolConfig{
		InitialSize:     10,
		MaxSize:         50,
		AcquireTimeout:  1 * time.Second,
		CleanupInterval: 1 * time.Minute,
	})
	defer pool.Close()

	ctx := context.Background()
	input := &core.AgentInput{
		Task: "test task",
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := pool.Execute(ctx, input)
			if err != nil {
				b.Error(err)
			}
		}
	})
}

// BenchmarkOptimizedPool_Execute 优化池 Execute 方法基准测试
func BenchmarkOptimizedPool_Execute(b *testing.B) {
	pool, _ := NewOptimizedAgentPool(mockFactory, PoolConfig{
		InitialSize:     10,
		MaxSize:         50,
		AcquireTimeout:  1 * time.Second,
		CleanupInterval: 1 * time.Minute,
	})
	defer pool.Close()

	ctx := context.Background()
	input := &core.AgentInput{
		Task: "test task",
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := pool.Execute(ctx, input)
			if err != nil {
				b.Error(err)
			}
		}
	})
}

// BenchmarkComparison_MixedWorkload 混合负载对比测试
func BenchmarkComparison_MixedWorkload(b *testing.B) {
	b.Run("Original", func(b *testing.B) {
		pool, _ := NewAgentPool(mockFactory, PoolConfig{
			InitialSize:     20,
			MaxSize:         100,
			AcquireTimeout:  1 * time.Second,
			CleanupInterval: 1 * time.Minute,
		})
		defer pool.Close()

		ctx := context.Background()
		b.ResetTimer()

		var wg sync.WaitGroup
		// 模拟不同的工作负载
		for i := 0; i < b.N; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				agent, err := pool.Acquire(ctx)
				if err != nil {
					return
				}
				// 随机持有时间
				time.Sleep(time.Duration(i%10) * time.Microsecond)
				pool.Release(agent)
			}()
		}
		wg.Wait()
	})

	b.Run("Optimized", func(b *testing.B) {
		pool, _ := NewOptimizedAgentPool(mockFactory, PoolConfig{
			InitialSize:     20,
			MaxSize:         100,
			AcquireTimeout:  1 * time.Second,
			CleanupInterval: 1 * time.Minute,
		})
		defer pool.Close()

		ctx := context.Background()
		b.ResetTimer()

		var wg sync.WaitGroup
		// 模拟不同的工作负载
		for i := 0; i < b.N; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				agent, err := pool.Acquire(ctx)
				if err != nil {
					return
				}
				// 随机持有时间
				time.Sleep(time.Duration(i%10) * time.Microsecond)
				pool.Release(agent)
			}()
		}
		wg.Wait()
	})
}

// TestOptimizedPool_Correctness 优化池正确性测试
func TestOptimizedPool_Correctness(t *testing.T) {
	pool, err := NewOptimizedAgentPool(mockFactory, PoolConfig{
		InitialSize:     5,
		MaxSize:         10,
		AcquireTimeout:  1 * time.Second,
		CleanupInterval: 1 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	ctx := context.Background()

	// 测试基本获取和释放
	agent1, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := pool.Release(agent1); err != nil {
		t.Fatal(err)
	}

	// 测试并发获取
	const concurrency = 50
	var wg sync.WaitGroup
	errors := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			agent, err := pool.Acquire(ctx)
			if err != nil {
				errors <- err
				return
			}
			time.Sleep(10 * time.Millisecond)
			if err := pool.Release(agent); err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}

	// 测试统计信息
	stats := pool.Stats()
	if stats.AcquiredTotal < int64(concurrency+1) {
		t.Errorf("Expected at least %d acquisitions, got %d", concurrency+1, stats.AcquiredTotal)
	}
}

// TestOptimizedPool_Timeout 测试超时行为
func TestOptimizedPool_Timeout(t *testing.T) {
	pool, _ := NewOptimizedAgentPool(mockFactory, PoolConfig{
		InitialSize:    1,
		MaxSize:        1,
		AcquireTimeout: 100 * time.Millisecond,
	})
	defer pool.Close()

	ctx := context.Background()

	// 获取唯一的 Agent
	agent, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// 尝试获取第二个 Agent，应该超时
	_, err = pool.Acquire(ctx)
	if err != ErrPoolTimeout {
		t.Errorf("Expected ErrPoolTimeout, got %v", err)
	}

	// 释放 Agent
	pool.Release(agent)

	// 现在应该能获取
	agent2, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pool.Release(agent2)
}
