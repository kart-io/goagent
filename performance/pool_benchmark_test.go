package performance

import (
	"context"
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

// BenchmarkAgentPool_Acquire_Sequential 池顺序获取基准测试
func BenchmarkAgentPool_Acquire_Sequential(b *testing.B) {
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

// BenchmarkAgentPool_Acquire_Parallel 池并发获取基准测试
func BenchmarkAgentPool_Acquire_Parallel(b *testing.B) {
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

// BenchmarkAgentPool_HighContention 池高竞争场景（小池大并发）
func BenchmarkAgentPool_HighContention(b *testing.B) {
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

// BenchmarkAgentPool_LargePool 池大池场景（1000 agents）
func BenchmarkAgentPool_LargePool(b *testing.B) {
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

// BenchmarkAgentPool_Execute 池 Execute 方法基准测试
func BenchmarkAgentPool_Execute(b *testing.B) {
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
