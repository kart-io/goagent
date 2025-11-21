package react

import (
	"context"
	"testing"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// MockLLMClient 用于基准测试的 Mock LLM 客户端
type MockLLMClient struct{}

func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	// 模拟 LLM 响应，直接返回最终答案
	return &llm.CompletionResponse{
		Content:    "Final Answer: 42",
		TokensUsed: 10,
		Usage: &interfaces.TokenUsage{
			PromptTokens:     5,
			CompletionTokens: 5,
			TotalTokens:      10,
		},
	}, nil
}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return m.Chat(ctx, req.Messages)
}

func (m *MockLLMClient) Provider() llm.Provider {
	return llm.ProviderOpenAI
}

func (m *MockLLMClient) IsAvailable() bool {
	return true
}

// MockTool 用于基准测试的 Mock Tool
type MockTool struct {
	name        string
	description string
}

func (m *MockTool) Name() string {
	return m.name
}

func (m *MockTool) Description() string {
	return m.description
}

func (m *MockTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return &interfaces.ToolOutput{
		Result:  "mock result",
		Success: true,
	}, nil
}

func (m *MockTool) ArgsSchema() string {
	return `{"type": "object", "properties": {}}`
}

// MockCallback 用于基准测试的 Mock Callback
type MockCallback struct {
	agentcore.BaseCallback
	callCount int
}

func (m *MockCallback) OnStart(ctx context.Context, input interface{}) error {
	m.callCount++
	return nil
}

func (m *MockCallback) OnEnd(ctx context.Context, output interface{}) error {
	m.callCount++
	return nil
}

func (m *MockCallback) OnLLMStart(ctx context.Context, prompts []string, model string) error {
	m.callCount++
	return nil
}

func (m *MockCallback) OnLLMEnd(ctx context.Context, output string, tokenUsage int) error {
	m.callCount++
	return nil
}

func (m *MockCallback) OnToolStart(ctx context.Context, toolName string, input interface{}) error {
	m.callCount++
	return nil
}

func (m *MockCallback) OnToolEnd(ctx context.Context, toolName string, output interface{}) error {
	m.callCount++
	return nil
}

func (m *MockCallback) OnAgentFinish(ctx context.Context, output interface{}) error {
	m.callCount++
	return nil
}

// createTestAgent 创建测试用 ReAct Agent
func createTestAgent(withCallbacks bool) *ReActAgent {
	llmClient := &MockLLMClient{}
	tools := []interfaces.Tool{
		&MockTool{name: "search", description: "Search tool"},
		&MockTool{name: "calculate", description: "Calculate tool"},
	}

	agent := NewReActAgent(ReActConfig{
		Name:        "test-agent",
		Description: "Test ReAct Agent",
		LLM:         llmClient,
		Tools:       tools,
		MaxSteps:    1, // 限制为 1 步以快速完成
	})

	if withCallbacks {
		callback := &MockCallback{}
		agent = agent.WithCallbacks(callback).(*ReActAgent)
	}

	return agent
}

// BenchmarkInvoke 基准测试：Invoke 方法（含回调）
func BenchmarkInvoke(b *testing.B) {
	agent := createTestAgent(true)
	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:      "What is 2+2?",
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := agent.Invoke(ctx, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkInvokeFast 基准测试：InvokeFast 方法（无回调）
func BenchmarkInvokeFast(b *testing.B) {
	agent := createTestAgent(false)
	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:      "What is 2+2?",
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := agent.InvokeFast(ctx, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkInvokeWithoutCallbacks 基准测试：Invoke 方法（无回调配置）
func BenchmarkInvokeWithoutCallbacks(b *testing.B) {
	agent := createTestAgent(false)
	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:      "What is 2+2?",
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := agent.Invoke(ctx, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkInvokeWithMultipleCallbacks 基准测试：带多个回调的 Invoke
func BenchmarkInvokeWithMultipleCallbacks(b *testing.B) {
	agent := createTestAgent(false)

	// 添加多个回调
	callbacks := []agentcore.Callback{
		&MockCallback{},
		&MockCallback{},
		&MockCallback{},
		&MockCallback{},
		&MockCallback{},
	}
	agent = agent.WithCallbacks(callbacks...).(*ReActAgent)

	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:      "What is 2+2?",
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := agent.Invoke(ctx, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCallbackOverhead 基准测试：测量回调开销
func BenchmarkCallbackOverhead(b *testing.B) {
	b.Run("NoCallbacks", func(b *testing.B) {
		agent := createTestAgent(false)
		ctx := context.Background()
		input := &agentcore.AgentInput{
			Task:      "What is 2+2?",
			Timestamp: time.Now(),
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := agent.Invoke(ctx, input)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("OneCallback", func(b *testing.B) {
		agent := createTestAgent(true)
		ctx := context.Background()
		input := &agentcore.AgentInput{
			Task:      "What is 2+2?",
			Timestamp: time.Now(),
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := agent.Invoke(ctx, input)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("FiveCallbacks", func(b *testing.B) {
		agent := createTestAgent(false)
		callbacks := []agentcore.Callback{
			&MockCallback{},
			&MockCallback{},
			&MockCallback{},
			&MockCallback{},
			&MockCallback{},
		}
		agent = agent.WithCallbacks(callbacks...).(*ReActAgent)
		ctx := context.Background()
		input := &agentcore.AgentInput{
			Task:      "What is 2+2?",
			Timestamp: time.Now(),
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := agent.Invoke(ctx, input)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("InvokeFast", func(b *testing.B) {
		agent := createTestAgent(false)
		ctx := context.Background()
		input := &agentcore.AgentInput{
			Task:      "What is 2+2?",
			Timestamp: time.Now(),
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := agent.InvokeFast(ctx, input)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkChainCalling 基准测试：链式调用场景
func BenchmarkChainCalling(b *testing.B) {
	agent := createTestAgent(false)
	ctx := context.Background()

	b.Run("Invoke_10x", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for j := 0; j < 10; j++ {
				input := &agentcore.AgentInput{
					Task:      "What is 2+2?",
					Timestamp: time.Now(),
				}
				_, err := agent.Invoke(ctx, input)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("InvokeFast_10x", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for j := 0; j < 10; j++ {
				input := &agentcore.AgentInput{
					Task:      "What is 2+2?",
					Timestamp: time.Now(),
				}
				_, err := agent.InvokeFast(ctx, input)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}
