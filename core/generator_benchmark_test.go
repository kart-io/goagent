package core

import (
	"context"
	"testing"
)

// mockAgent 用于基准测试的模拟 Agent
type mockAgent struct {
	*BaseAgent
	iterations int
}

func newMockAgent(iterations int) *mockAgent {
	return &mockAgent{
		BaseAgent:  NewBaseAgent("mock", "mock agent", []string{}),
		iterations: iterations,
	}
}

// Invoke 模拟执行
func (m *mockAgent) Invoke(ctx context.Context, input *AgentInput) (*AgentOutput, error) {
	return &AgentOutput{
		Status:  "success",
		Message: "mock output",
	}, nil
}

// Stream 模拟流式执行（使用 Channel）
func (m *mockAgent) Stream(ctx context.Context, input *AgentInput) (<-chan StreamChunk[*AgentOutput], error) {
	ch := make(chan StreamChunk[*AgentOutput], m.iterations)

	go func() {
		defer close(ch)
		for i := 0; i < m.iterations; i++ {
			select {
			case <-ctx.Done():
				ch <- StreamChunk[*AgentOutput]{Error: ctx.Err()}
				return
			case ch <- StreamChunk[*AgentOutput]{
				Data: &AgentOutput{
					Status:  "success",
					Message: "mock output",
				},
				Error: nil,
				Done:  i == m.iterations-1,
			}:
			}
		}
	}()

	return ch, nil
}

// RunGenerator 模拟流式执行（使用 Generator）
func (m *mockAgent) RunGenerator(ctx context.Context, input *AgentInput) Generator[*AgentOutput] {
	return func(yield func(*AgentOutput, error) bool) {
		for i := 0; i < m.iterations; i++ {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			output := &AgentOutput{
				Status:  "success",
				Message: "mock output",
			}

			if !yield(output, nil) {
				return
			}
		}
	}
}

// BenchmarkStream_Channel 基准测试：Channel 模式
func BenchmarkStream_Channel(b *testing.B) {
	agent := newMockAgent(10)
	ctx := context.Background()
	input := &AgentInput{
		Task: "test task",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch, err := agent.Stream(ctx, input)
		if err != nil {
			b.Fatal(err)
		}

		for event := range ch {
			if event.Error != nil {
				b.Fatal(event.Error)
			}
			_ = event.Data
		}
	}
}

// BenchmarkRunGenerator 基准测试：Generator 模式
func BenchmarkRunGenerator(b *testing.B) {
	agent := newMockAgent(10)
	ctx := context.Background()
	input := &AgentInput{
		Task: "test task",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for output, err := range agent.RunGenerator(ctx, input) {
			if err != nil {
				b.Fatal(err)
			}
			_ = output
		}
	}
}

// BenchmarkStream_Memory 内存分配对比：Channel
func BenchmarkStream_Memory(b *testing.B) {
	agent := newMockAgent(10)
	ctx := context.Background()
	input := &AgentInput{
		Task: "test task",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ch, _ := agent.Stream(ctx, input)
		for range ch {
		}
	}
}

// BenchmarkRunGenerator_Memory 内存分配对比：Generator
func BenchmarkRunGenerator_Memory(b *testing.B) {
	agent := newMockAgent(10)
	ctx := context.Background()
	input := &AgentInput{
		Task: "test task",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for range agent.RunGenerator(ctx, input) {
		}
	}
}

// BenchmarkCollect 基准测试：Collect 辅助函数
func BenchmarkCollect(b *testing.B) {
	agent := newMockAgent(10)
	ctx := context.Background()
	input := &AgentInput{
		Task: "test task",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gen := agent.RunGenerator(ctx, input)
		results, err := Collect(gen)
		if err != nil {
			b.Fatal(err)
		}
		_ = results
	}
}

// BenchmarkTake 基准测试：Take 操作
func BenchmarkTake(b *testing.B) {
	agent := newMockAgent(100)
	ctx := context.Background()
	input := &AgentInput{
		Task: "test task",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gen := agent.RunGenerator(ctx, input)
		taken := Take(gen, 10)

		for range taken {
		}
	}
}

// BenchmarkFilter 基准测试：Filter 操作
func BenchmarkFilter(b *testing.B) {
	agent := newMockAgent(100)
	ctx := context.Background()
	input := &AgentInput{
		Task: "test task",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gen := agent.RunGenerator(ctx, input)
		filtered := Filter(gen, func(output *AgentOutput) bool {
			return output.Status == "success"
		})

		for range filtered {
		}
	}
}

// BenchmarkMap 基准测试：Map 操作
func BenchmarkMap(b *testing.B) {
	agent := newMockAgent(100)
	ctx := context.Background()
	input := &AgentInput{
		Task: "test task",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gen := agent.RunGenerator(ctx, input)
		mapped := Map(gen, func(output *AgentOutput) string {
			return output.Message
		})

		for range mapped {
		}
	}
}

// BenchmarkChainedOperations 基准测试：链式操作
func BenchmarkChainedOperations(b *testing.B) {
	agent := newMockAgent(100)
	ctx := context.Background()
	input := &AgentInput{
		Task: "test task",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gen := agent.RunGenerator(ctx, input)
		result := Take(
			Map(
				Filter(gen, func(output *AgentOutput) bool {
					return output.Status == "success"
				}),
				func(output *AgentOutput) string {
					return output.Message
				},
			),
			10,
		)

		for range result {
		}
	}
}

// BenchmarkToChannel_Conversion 基准测试：Generator 转 Channel 的开销
func BenchmarkToChannel_Conversion(b *testing.B) {
	agent := newMockAgent(10)
	ctx := context.Background()
	input := &AgentInput{
		Task: "test task",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gen := agent.RunGenerator(ctx, input)
		ch := ToChannel(ctx, gen, 10)

		for range ch {
		}
	}
}

// BenchmarkFromChannel_Conversion 基准测试：Channel 转 Generator 的开销
func BenchmarkFromChannel_Conversion(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch := make(chan StreamChunk[*AgentOutput], 10)
		go func() {
			defer close(ch)
			for j := 0; j < 10; j++ {
				ch <- StreamChunk[*AgentOutput]{
					Data: &AgentOutput{
						Status:  "success",
						Message: "mock output",
					},
				}
			}
		}()

		gen := FromChannel(ch)
		for range gen {
		}
	}
}

// BenchmarkGeneratorFunc 基准测试：GeneratorFunc 包装器
func BenchmarkGeneratorFunc(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gen := GeneratorFunc(func(yield func(int, error) bool) {
			for j := 0; j < 10; j++ {
				if !yield(j, nil) {
					return
				}
			}
		})

		for range gen {
		}
	}
}

// BenchmarkChannel_Traditional 基准测试：传统 Channel 模式（作为对照）
func BenchmarkChannel_Traditional(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch := make(chan int, 10)
		go func() {
			defer close(ch)
			for j := 0; j < 10; j++ {
				ch <- j
			}
		}()

		for range ch {
		}
	}
}

// BenchmarkGenerator_EarlyTermination 基准测试：早期终止的性能
func BenchmarkGenerator_EarlyTermination(b *testing.B) {
	agent := newMockAgent(100)
	ctx := context.Background()
	input := &AgentInput{
		Task: "test task",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gen := agent.RunGenerator(ctx, input)
		count := 0
		for range gen {
			count++
			if count >= 10 {
				break
			}
		}
	}
}

// BenchmarkChannel_EarlyTermination 基准测试：Channel 早期终止的性能
func BenchmarkChannel_EarlyTermination(b *testing.B) {
	agent := newMockAgent(100)
	ctx := context.Background()
	input := &AgentInput{
		Task: "test task",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch, _ := agent.Stream(ctx, input)
		count := 0
		for range ch {
			count++
			if count >= 10 {
				break
			}
		}
	}
}
