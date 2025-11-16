package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

// 这个示例展示了所有 LLM Provider 的接口一致性
// 所有 provider 都实现了相同的 llm.Client 接口

func main() {
	fmt.Println("=== LLM Provider 接口一致性测试 ===")
	fmt.Println()

	// 创建不同的 provider
	var clients []llm.Client

	// 1. OpenAI Provider
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		openaiConfig := &llm.Config{
			Provider:    llm.ProviderOpenAI,
			APIKey:      apiKey,
			Model:       "gpt-3.5-turbo",
			MaxTokens:   1000,
			Temperature: 0.7,
			Timeout:     30,
		}
		if client, err := providers.NewOpenAI(openaiConfig); err == nil {
			clients = append(clients, client)
			fmt.Println("✅ OpenAI Provider 创建成功")
		}
	}

	// 2. DeepSeek Provider
	if apiKey := os.Getenv("DEEPSEEK_API_KEY"); apiKey != "" {
		deepseekConfig := &llm.Config{
			Provider:    llm.ProviderDeepSeek,
			APIKey:      apiKey,
			BaseURL:     "https://api.deepseek.com",
			Model:       "deepseek-chat",
			MaxTokens:   1000,
			Temperature: 0.7,
			Timeout:     30,
		}
		if client, err := providers.NewDeepSeek(deepseekConfig); err == nil {
			clients = append(clients, client)
			fmt.Println("✅ DeepSeek Provider 创建成功")
		}
	}

	// 3. Gemini Provider
	if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		geminiConfig := &llm.Config{
			Provider:    llm.ProviderGemini,
			APIKey:      apiKey,
			Model:       "gemini-pro",
			MaxTokens:   1000,
			Temperature: 0.7,
			Timeout:     30,
		}
		if client, err := providers.NewGemini(geminiConfig); err == nil {
			clients = append(clients, client)
			fmt.Println("✅ Gemini Provider 创建成功")
		}
	}

	// 4. Ollama Provider (新增)
	ollamaClient := providers.NewOllamaClientSimple("llama2")
	clients = append(clients, ollamaClient)
	fmt.Println("✅ Ollama Provider 创建成功")

	fmt.Println()
	fmt.Println("=== 测试所有 Provider 的统一接口 ===")
	fmt.Println()

	// 测试每个 provider 的接口一致性
	for _, client := range clients {
		testProviderInterface(client)
	}

	fmt.Println()
	fmt.Println("=== 结论 ===")
	fmt.Println("✅ 所有 LLM Provider 都实现了相同的 llm.Client 接口:")
	fmt.Println("   - Complete(ctx, req) - 文本补全")
	fmt.Println("   - Chat(ctx, messages) - 对话")
	fmt.Println("   - Provider() - 返回提供商类型")
	fmt.Println("   - IsAvailable() - 检查可用性")
	fmt.Println()
	fmt.Println("这意味着你可以轻松切换不同的 LLM Provider，")
	fmt.Println("而不需要修改任何业务代码！")
}

// testProviderInterface 测试单个 provider 的接口
func testProviderInterface(client llm.Client) {
	// 1. 测试 Provider() 方法
	provider := client.Provider()
	fmt.Printf("\n测试 %s Provider:\n", provider)
	fmt.Println("--------------------")

	// 2. 测试 IsAvailable() 方法
	available := client.IsAvailable()
	fmt.Printf("  IsAvailable(): %v\n", available)

	if !available {
		fmt.Printf("  ⚠️  %s 不可用，跳过其他测试\n", provider)
		return
	}

	ctx := context.Background()

	// 3. 测试 Chat() 方法
	messages := []llm.Message{
		llm.SystemMessage("You are a helpful assistant. Answer in one sentence."),
		llm.UserMessage("What is 2+2?"),
	}

	chatResp, err := client.Chat(ctx, messages)
	if err != nil {
		fmt.Printf("  Chat() 错误: %v\n", err)
	} else {
		fmt.Printf("  Chat() 成功: %s\n", truncate(chatResp.Content, 50))
		fmt.Printf("    - Model: %s\n", chatResp.Model)
		fmt.Printf("    - Provider: %s\n", chatResp.Provider)
		fmt.Printf("    - Tokens: %d\n", chatResp.TokensUsed)
	}

	// 4. 测试 Complete() 方法
	req := &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.UserMessage("Say 'Hello' in one word"),
		},
		Temperature: 0.3,
		MaxTokens:   10,
	}

	completeResp, err := client.Complete(ctx, req)
	if err != nil {
		fmt.Printf("  Complete() 错误: %v\n", err)
	} else {
		fmt.Printf("  Complete() 成功: %s\n", truncate(completeResp.Content, 50))
	}
}

// truncate 截断字符串
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Unused example functions - kept for reference
/*
// 使用示例：如何在代码中灵活切换 Provider
func demonstrateProviderSwitching() {
	// 根据环境或配置选择 provider
	var client llm.Client

	switch os.Getenv("LLM_PROVIDER") {
	case "openai":
		client = createOpenAIClient()
	case "deepseek":
		client = createDeepSeekClient()
	case "gemini":
		client = createGeminiClient()
	case "ollama":
		client = createOllamaClient()
	default:
		// 默认使用 Ollama（本地）
		client = createOllamaClient()
		if !client.IsAvailable() {
			// 如果 Ollama 不可用，尝试 OpenAI
			client = createOpenAIClient()
		}
	}

	// 使用统一的接口，无论底层是哪个 provider
	ctx := context.Background()
	response, err := client.Chat(ctx, []llm.Message{
		llm.UserMessage("Hello!"),
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(response.Content)
}

func createOpenAIClient() llm.Client {
	config := &llm.Config{
		Provider: llm.ProviderOpenAI,
		APIKey:   os.Getenv("OPENAI_API_KEY"),
		Model:    "gpt-3.5-turbo",
	}
	client, _ := providers.NewOpenAI(config)
	return client
}

func createDeepSeekClient() llm.Client {
	config := &llm.Config{
		Provider: llm.ProviderDeepSeek,
		APIKey:   os.Getenv("DEEPSEEK_API_KEY"),
		BaseURL:  "https://api.deepseek.com",
		Model:    "deepseek-chat",
	}
	client, _ := providers.NewDeepSeek(config)
	return client
}

func createGeminiClient() llm.Client {
	config := &llm.Config{
		Provider: llm.ProviderGemini,
		APIKey:   os.Getenv("GEMINI_API_KEY"),
		Model:    "gemini-pro",
	}
	client, _ := providers.NewGemini(config)
	return client
}

func createOllamaClient() llm.Client {
	return providers.NewOllamaClientSimple("llama2")
}
*/
