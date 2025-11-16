package main

import (
	"context"
	"fmt"
	"log"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/store/memory"
	"github.com/kart-io/goagent/tools"
)

// SimpleLLMClient 简单的 LLM 客户端实现
type SimpleLLMClient struct{}

func NewSimpleLLMClient() *SimpleLLMClient {
	return &SimpleLLMClient{}
}

func (s *SimpleLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{
		Content:    "This is a mock response from the LLM.",
		Model:      "mock-model",
		TokensUsed: 10,
	}, nil
}

func (s *SimpleLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return s.Complete(ctx, &llm.CompletionRequest{Messages: messages})
}

func (s *SimpleLLMClient) Provider() llm.Provider {
	return llm.ProviderCustom
}

func (s *SimpleLLMClient) IsAvailable() bool {
	return true
}

func main() {
	fmt.Println("=== ToolRuntime Pattern Demo ===")

	// 1. 创建基础组件
	llmClient := NewSimpleLLMClient()
	state := core.NewAgentState()
	store := memory.New()

	// 2. 设置用户上下文
	state.Set("user_id", "user_123")
	state.Set("user_name", "Alice")

	// 3. 预先存储用户数据
	ctx := context.Background()
	store.Put(ctx, []string{"users"}, "user_123", map[string]interface{}{
		"name":  "Alice",
		"email": "alice@example.com",
		"tier":  "premium",
	})

	// 4. 创建支持 Runtime 的工具
	userInfoTool := tools.NewUserInfoTool()
	savePrefTool := tools.NewSavePreferenceTool()

	// 5. 创建 ToolRuntime
	runtime := tools.NewToolRuntime(ctx, state, store)
	runtime.WithStreamWriter(func(data interface{}) error {
		fmt.Printf("[Stream] %+v\n", data)
		return nil
	})

	// 6. 演示 UserInfoTool
	fmt.Println("--- Demo 1: Get User Info ---")
	input1 := &tools.ToolInput{
		Args:    map[string]interface{}{},
		Context: ctx,
	}

	output1, err := userInfoTool.ExecuteWithRuntime(ctx, input1, runtime)
	if err != nil {
		log.Fatalf("Failed to get user info: %v", err)
	}
	fmt.Printf("Result: %+v\n\n", output1.Result)

	// 7. 演示 SavePreferenceTool
	fmt.Println("--- Demo 2: Save Preference ---")
	input2 := &tools.ToolInput{
		Args: map[string]interface{}{
			"key":   "theme",
			"value": "dark",
		},
		Context: ctx,
	}

	output2, err := savePrefTool.ExecuteWithRuntime(ctx, input2, runtime)
	if err != nil {
		log.Fatalf("Failed to save preference: %v", err)
	}
	fmt.Printf("Result: %+v\n\n", output2.Result)

	// 8. 验证偏好已保存到存储
	fmt.Println("--- Demo 3: Verify Saved Preference ---")
	savedPrefs, _ := store.Get(ctx, []string{"preferences"}, "user_123")
	if savedPrefs != nil {
		fmt.Printf("Saved preferences: %+v\n\n", savedPrefs.Value)
	}

	// 9. 演示在 Agent 中使用 RuntimeTool
	fmt.Println("--- Demo 4: RuntimeTool in Agent (Advanced) ---")

	// 创建带 Runtime 的 Agent (需要自定义集成)
	agent, err := builder.NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("You are a helpful assistant with access to user context.").
		WithTools(userInfoTool, savePrefTool).
		WithState(state).
		WithStore(store).
		Build()
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	metrics := agent.GetMetrics()
	if toolsCount, ok := metrics["tools_count"].(int); ok {
		fmt.Printf("Agent created successfully with %d tools\n", toolsCount)
	}

	// 10. 演示 Runtime 配置
	fmt.Println("\n--- Demo 5: Runtime Configuration ---")

	// 创建受限的 Runtime
	restrictedRuntime := tools.NewToolRuntime(ctx, state, store).
		WithConfig(&tools.RuntimeConfig{
			EnableStateAccess: true,
			EnableStoreAccess: true,
			EnableStreaming:   false,                            // 禁用流式输出
			AllowedNamespaces: []string{"users", "preferences"}, // 只允许访问特定命名空间
		})

	// 尝试访问受限命名空间
	_, err = restrictedRuntime.GetFromStore([]string{"admin"}, "secret")
	if err != nil {
		fmt.Printf("Expected error (namespace restricted): %v\n", err)
	}

	// 11. 演示 Runtime Metadata
	fmt.Println("\n--- Demo 6: Runtime Metadata ---")
	runtime.WithMetadata("request_id", "req_12345")
	runtime.WithMetadata("session_start", "2024-01-01T00:00:00Z")

	if reqID, ok := runtime.GetMetadata("request_id"); ok {
		fmt.Printf("Request ID: %v\n", reqID)
	}

	// 12. 演示 Runtime Clone
	fmt.Println("\n--- Demo 7: Runtime Clone ---")
	clonedRuntime := runtime.Clone()
	clonedRuntime.WithMetadata("cloned", true)

	if _, ok := runtime.GetMetadata("cloned"); !ok {
		fmt.Println("Original runtime not affected by clone modifications ✓")
	}

	fmt.Println("\n=== Demo Complete ===")
}
