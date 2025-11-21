package performance

import (
	"context"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/middleware"
)

// BenchmarkMiddlewareChain 中间件链性能对比
func BenchmarkMiddlewareChain(b *testing.B) {
	ctx := context.Background()
	handler := func(ctx context.Context, req *middleware.MiddlewareRequest) (*middleware.MiddlewareResponse, error) {
		return &middleware.MiddlewareResponse{
			Output:   "result",
			Duration: time.Microsecond,
			Metadata: make(map[string]interface{}),
			Headers:  make(map[string]string),
		}, nil
	}

	// 创建测试中间件
	mw1 := &NoOpMiddleware{name: "mw1"}
	mw2 := &NoOpMiddleware{name: "mw2"}
	mw3 := &NoOpMiddleware{name: "mw3"}

	req := &middleware.MiddlewareRequest{
		Input:     "test",
		Metadata:  make(map[string]interface{}),
		Headers:   make(map[string]string),
		Timestamp: time.Now(),
	}

	b.Run("OriginalChain", func(b *testing.B) {
		chain := middleware.NewMiddlewareChain(handler)
		chain.Use(mw1).Use(mw2).Use(mw3)

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_, err := chain.Execute(ctx, req)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ImmutableChain", func(b *testing.B) {
		chain := middleware.NewImmutableMiddlewareChain(handler, mw1, mw2, mw3)

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_, err := chain.Execute(ctx, req)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("FastChain", func(b *testing.B) {
		chain := middleware.NewFastMiddlewareChain(handler, mw1, mw2, mw3)

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_, err := chain.Execute(ctx, req)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkMiddlewareChainWithComplexMiddleware 使用复杂中间件的性能对比
func BenchmarkMiddlewareChainWithComplexMiddleware(b *testing.B) {
	ctx := context.Background()
	handler := func(ctx context.Context, req *middleware.MiddlewareRequest) (*middleware.MiddlewareResponse, error) {
		return &middleware.MiddlewareResponse{
			Output:   "result",
			Duration: time.Microsecond,
			Metadata: make(map[string]interface{}),
			Headers:  make(map[string]string),
		}, nil
	}

	// 使用真实的中间件实现
	loggingMW := middleware.NewLoggingMiddleware(func(msg string) {})
	timingMW := middleware.NewTimingMiddleware()
	cacheMW := middleware.NewCacheMiddleware(5 * time.Minute)

	req := &middleware.MiddlewareRequest{
		Input:     "test",
		Metadata:  make(map[string]interface{}),
		Headers:   make(map[string]string),
		Timestamp: time.Now(),
	}

	b.Run("OriginalChain_Complex", func(b *testing.B) {
		chain := middleware.NewMiddlewareChain(handler)
		chain.Use(loggingMW).Use(timingMW).Use(cacheMW)

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_, err := chain.Execute(ctx, req)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ImmutableChain_Complex", func(b *testing.B) {
		chain := middleware.NewImmutableMiddlewareChain(handler, loggingMW, timingMW, cacheMW)

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_, err := chain.Execute(ctx, req)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("FastChain_Complex", func(b *testing.B) {
		chain := middleware.NewFastMiddlewareChain(handler, loggingMW, timingMW, cacheMW)

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_, err := chain.Execute(ctx, req)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkHotPathOptimization 热路径优化对比
func BenchmarkHotPathOptimization(b *testing.B) {
	ctx := context.Background()
	agent := createMockAgent()
	input := &core.AgentInput{
		Task:      "test",
		Timestamp: time.Now(),
		Context:   make(map[string]interface{}),
		Options:   core.DefaultAgentOptions(),
	}

	b.Run("NormalInvoke", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_, err := agent.Invoke(ctx, input)
			// We expect ErrNotImplemented since this is BaseAgent
			if err != core.ErrNotImplemented {
				b.Fatalf("expected ErrNotImplemented, got: %v", err)
			}
		}
	})

	b.Run("FastInvoke", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_, err := agent.InvokeFast(ctx, input)
			// We expect ErrNotImplemented since this is BaseAgent
			if err != core.ErrNotImplemented {
				b.Fatalf("expected ErrNotImplemented, got: %v", err)
			}
		}
	})
}

// BenchmarkMiddlewareAllocation 中间件内存分配对比
func BenchmarkMiddlewareAllocation(b *testing.B) {
	ctx := context.Background()
	handler := func(ctx context.Context, req *middleware.MiddlewareRequest) (*middleware.MiddlewareResponse, error) {
		return &middleware.MiddlewareResponse{
			Output:   "result",
			Duration: time.Microsecond,
			Metadata: make(map[string]interface{}),
			Headers:  make(map[string]string),
		}, nil
	}

	mw1 := &NoOpMiddleware{name: "mw1"}
	mw2 := &NoOpMiddleware{name: "mw2"}
	mw3 := &NoOpMiddleware{name: "mw3"}
	mw4 := &NoOpMiddleware{name: "mw4"}
	mw5 := &NoOpMiddleware{name: "mw5"}

	req := &middleware.MiddlewareRequest{
		Input:     "test",
		Metadata:  make(map[string]interface{}),
		Headers:   make(map[string]string),
		Timestamp: time.Now(),
	}

	b.Run("OriginalChain_5MW", func(b *testing.B) {
		chain := middleware.NewMiddlewareChain(handler)
		chain.Use(mw1, mw2, mw3, mw4, mw5)

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_, err := chain.Execute(ctx, req)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ImmutableChain_5MW", func(b *testing.B) {
		chain := middleware.NewImmutableMiddlewareChain(handler, mw1, mw2, mw3, mw4, mw5)

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_, err := chain.Execute(ctx, req)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("FastChain_5MW", func(b *testing.B) {
		chain := middleware.NewFastMiddlewareChain(handler, mw1, mw2, mw3, mw4, mw5)

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_, err := chain.Execute(ctx, req)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// NoOpMiddleware 无操作中间件（用于测试）
type NoOpMiddleware struct {
	name string
}

func (m *NoOpMiddleware) Name() string { return m.name }

func (m *NoOpMiddleware) OnBefore(ctx context.Context, req *middleware.MiddlewareRequest) (*middleware.MiddlewareRequest, error) {
	return req, nil
}

func (m *NoOpMiddleware) OnAfter(ctx context.Context, resp *middleware.MiddlewareResponse) (*middleware.MiddlewareResponse, error) {
	return resp, nil
}

func (m *NoOpMiddleware) OnError(ctx context.Context, err error) error {
	return err
}

func createMockAgent() *core.BaseAgent {
	agent := core.NewBaseAgent("test", "Test agent", []string{"test"})
	return agent
}
