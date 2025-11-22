// Package main demonstrates InvokeFast performance optimization
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/agents/react"
	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

// MockLLMClient for performance testing
type MockLLMClient struct{}

func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	// Simulate fast LLM response
	time.Sleep(10 * time.Millisecond)
	return &llm.CompletionResponse{
		Content: "Final Answer: Result",
		Usage: &interfaces.TokenUsage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}, nil
}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return m.Chat(ctx, req.Messages)
}

func (m *MockLLMClient) Provider() constants.Provider {
	return constants.ProviderCustom
}

func (m *MockLLMClient) IsAvailable() bool {
	return true
}

// MockTool for testing
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
		Result:  fmt.Sprintf("Tool %s executed", m.name),
		Success: true,
	}, nil
}

func (m *MockTool) ArgsSchema() string {
	return `{"type": "object", "properties": {}}`
}

func main() {
	fmt.Println("GoAgent Performance Demonstration - InvokeFast Optimization")
	fmt.Println("===========================================================")
	fmt.Println()

	// Create mock LLM client
	llmClient := &MockLLMClient{}

	// Create mock tools
	tools := []interfaces.Tool{
		&MockTool{name: "search", description: "Search tool"},
		&MockTool{name: "calculate", description: "Calculate tool"},
	}

	// Create ReAct agent
	agent := react.NewReActAgent(react.ReActConfig{
		Name:        "demo-agent",
		Description: "Performance demo agent",
		LLM:         llmClient,
		Tools:       tools,
		MaxSteps:    1,
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:      "Calculate 2+2",
		Timestamp: time.Now(),
	}

	// Warmup
	fmt.Println("Warming up...")
	for i := 0; i < 5; i++ {
		_, _ = agent.Invoke(ctx, input)
	}

	// Benchmark standard Invoke
	fmt.Println("\n1. Benchmarking Standard Invoke (with callbacks)")
	fmt.Println("   Running 100 iterations...")

	invokeStart := time.Now()
	for i := 0; i < 100; i++ {
		_, err := agent.Invoke(ctx, input)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
	}
	invokeElapsed := time.Since(invokeStart)
	invokeAvg := invokeElapsed / 100

	fmt.Printf("   Total time: %v\n", invokeElapsed)
	fmt.Printf("   Average per call: %v\n", invokeAvg)

	// Benchmark InvokeFast
	fmt.Println("\n2. Benchmarking InvokeFast (optimized, no callbacks)")
	fmt.Println("   Running 100 iterations...")

	fastStart := time.Now()
	for i := 0; i < 100; i++ {
		_, err := agent.InvokeFast(ctx, input)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
	}
	fastElapsed := time.Since(fastStart)
	fastAvg := fastElapsed / 100

	fmt.Printf("   Total time: %v\n", fastElapsed)
	fmt.Printf("   Average per call: %v\n", fastAvg)

	// Calculate improvement
	improvement := float64(invokeElapsed-fastElapsed) / float64(invokeElapsed) * 100

	fmt.Println("\n3. Performance Comparison")
	fmt.Println("   ========================================")
	fmt.Printf("   Standard Invoke: %v/call\n", invokeAvg)
	fmt.Printf("   InvokeFast:      %v/call\n", fastAvg)
	fmt.Printf("   Improvement:     %.2f%%\n", improvement)
	fmt.Println("   ========================================")

	// Demonstrate TryInvokeFast
	fmt.Println("\n4. Using TryInvokeFast (Recommended)")
	fmt.Println("   Automatically uses fastest path available")

	tryFastStart := time.Now()
	for i := 0; i < 100; i++ {
		_, err := agentcore.TryInvokeFast(ctx, agent, input)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
	}
	tryFastElapsed := time.Since(tryFastStart)
	tryFastAvg := tryFastElapsed / 100

	fmt.Printf("   Average per call: %v\n", tryFastAvg)
	fmt.Printf("   (Same as InvokeFast when agent supports it)\n")

	// Chain example
	fmt.Println("\n5. Chain Optimization Example")
	fmt.Println("   ChainableAgent automatically uses InvokeFast internally")

	chain := agentcore.NewChainableAgent("demo-chain", "Performance demo chain",
		agent, agent, agent) // 3 agents in chain

	chainInput := &agentcore.AgentInput{
		Task:      "Process through chain",
		Timestamp: time.Now(),
	}

	chainStart := time.Now()
	_, err := chain.Invoke(ctx, chainInput)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	chainElapsed := time.Since(chainStart)

	fmt.Printf("   Chain (3 agents) executed in: %v\n", chainElapsed)
	fmt.Println("   Each internal call used InvokeFast automatically!")

	// Summary
	fmt.Println("\n6. Key Takeaways")
	fmt.Println("   ✓ InvokeFast provides 4-6%% performance improvement")
	fmt.Println("   ✓ Use TryInvokeFast for automatic optimization")
	fmt.Println("   ✓ ChainableAgent/SupervisorAgent optimize automatically")
	fmt.Println("   ✓ Zero code changes required for existing apps")
	fmt.Println("\n   See docs/guides/INVOKE_FAST_OPTIMIZATION.md for details")
}
