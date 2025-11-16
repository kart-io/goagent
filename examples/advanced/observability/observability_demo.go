package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/middleware"
	"github.com/kart-io/goagent/observability"
)

// MockLLMClient 模拟 LLM 客户端
type MockLLMClient struct{}

func (c *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{
		Content: "This is a mock response for: " + req.Messages[len(req.Messages)-1].Content,
	}, nil
}

func (c *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{
		Content: "This is a mock chat response",
	}, nil
}

func (c *MockLLMClient) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	ch := make(chan *llm.StreamChunk)
	return ch, nil
}

func (c *MockLLMClient) Provider() llm.Provider {
	return llm.ProviderOpenAI
}

func (c *MockLLMClient) IsAvailable() bool {
	return true
}

func main() {
	fmt.Println("=== OpenTelemetry Integration Example ===")

	// 1. 创建 Telemetry Provider
	fmt.Println("1. Creating Telemetry Provider...")
	telemetryConfig := &observability.TelemetryConfig{
		ServiceName:     "agent-example",
		ServiceVersion:  "1.0.0",
		Environment:     "development",
		TraceEnabled:    true,
		TraceExporter:   "noop", // 使用 noop 导出器用于演示
		TraceSampleRate: 1.0,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	}

	provider, err := observability.NewTelemetryProvider(telemetryConfig)
	if err != nil {
		log.Fatalf("Failed to create telemetry provider: %v", err)
	}
	defer provider.Shutdown(context.Background())
	fmt.Println("✓ Telemetry Provider created")

	// 2. 创建 Tracer 和 Metrics
	fmt.Println("2. Creating Tracer and Metrics...")
	tracer := observability.NewAgentTracer(provider, "example-tracer")
	metrics, err := observability.NewAgentMetrics(provider, "example-metrics")
	if err != nil {
		log.Fatalf("Failed to create metrics: %v", err)
	}
	fmt.Println("✓ Tracer and Metrics created")

	// 3. 创建带有可观测性的 Agent
	fmt.Println("3. Creating Agent with Observability...")

	// 创建可观测性中间件
	obsMiddleware, err := middleware.NewObservabilityMiddleware(provider)
	if err != nil {
		log.Fatalf("Failed to create observability middleware: %v", err)
	}

	// 创建 Agent
	llmClient := &MockLLMClient{}
	agent, err := builder.NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("You are a helpful AI assistant with full observability.").
		WithState(core.NewAgentState()).
		WithMiddleware(obsMiddleware).
		WithMetadata("agent.name", "observability-agent").
		WithMetadata("agent.type", "demo").
		WithTelemetry(provider).
		Build()
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer agent.Shutdown(context.Background())
	fmt.Println("✓ Agent created with observability middleware")

	// 4. 演示追踪
	fmt.Println("4. Demonstrating Tracing...")
	ctx := context.Background()

	// 创建带追踪的上下文
	ctx, span := tracer.StartAgentSpan(ctx, "example-agent",
		observability.AgentAttributes("example-agent", "demo")...,
	)
	defer span.End()

	// 添加事件
	tracer.AddEvent(ctx, "agent.started")

	// 执行一些操作并追踪
	err = tracer.WithSpanContext(ctx, "process-request", func(ctx context.Context) error {
		// 模拟 LLM 调用
		llmCtx, llmSpan := tracer.StartLLMSpan(ctx, "mock-model",
			observability.LLMAttributes("mock-model", "mock-provider", 100)...,
		)
		defer llmSpan.End()

		time.Sleep(100 * time.Millisecond) // 模拟处理

		tracer.AddEvent(llmCtx, "llm.call.completed")
		return nil
	})
	if err != nil {
		log.Printf("Error: %v", err)
	}

	fmt.Println("✓ Tracing demonstrated")

	// 5. 演示指标
	fmt.Println("5. Demonstrating Metrics...")

	// 记录 Agent 执行
	metrics.RecordAgentExecution(ctx, "example-agent", "demo", 0.5, true)

	// 记录工具调用
	metrics.RecordToolCall(ctx, "calculator", 0.1, true)
	metrics.RecordToolCall(ctx, "search", 0.3, true)

	// 记录 LLM 调用
	metrics.RecordLLMCall(ctx, "mock-model", "mock-provider", 100, 0.5, true)

	// 活跃 Agent 计数
	metrics.IncrementActiveAgents(ctx, 1)
	time.Sleep(100 * time.Millisecond)
	metrics.IncrementActiveAgents(ctx, -1)

	fmt.Println("✓ Metrics recorded")

	// 6. 执行 Agent（带完整可观测性）
	fmt.Println("6. Executing Agent with Full Observability...")

	output, err := agent.Execute(ctx, "What is the meaning of observability in AI systems?")
	if err != nil {
		log.Fatalf("Failed to execute agent: %v", err)
	}

	fmt.Printf("Agent Output: %v\n", output.Result)
	fmt.Printf("Execution Duration: %v\n", output.Duration)
	fmt.Println("✓ Agent executed successfully")

	// 7. 演示错误追踪
	fmt.Println("7. Demonstrating Error Tracking...")

	errCtx, errSpan := tracer.StartSpan(ctx, "error-demo")
	defer errSpan.End()

	// 模拟错误
	demoErr := fmt.Errorf("demo error for tracking")
	tracer.RecordError(errCtx, demoErr)
	metrics.RecordError(errCtx, "DemoError")

	fmt.Println("✓ Error tracking demonstrated")

	// 8. 强制刷新遥测数据
	fmt.Println("8. Flushing Telemetry Data...")
	flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := provider.ForceFlush(flushCtx); err != nil {
		log.Printf("Warning: Failed to flush telemetry: %v", err)
	}
	fmt.Println("✓ Telemetry data flushed")

	fmt.Println("=== Example Completed Successfully ===")
	fmt.Println("\nNotes:")
	fmt.Println("- This example uses 'noop' exporters for demonstration")
	fmt.Println("- In production, configure OTLP exporter to send to Jaeger/Prometheus")
	fmt.Println("- Example config: TraceExporter='otlp', TraceEndpoint='localhost:4317'")
	fmt.Println("- Metrics can be exported to Prometheus at /metrics endpoint")
	fmt.Println("- Traces can be viewed in Jaeger UI at http://localhost:16686")
}
