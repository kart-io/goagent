package main

import (
	"fmt"
	"log"

	// 导入 provider 包以触发自动注册
	// 使用空白导入 (_) 确保 init() 函数被调用
	_ "github.com/kart-io/goagent/contrib/llm-providers/anthropic"
	_ "github.com/kart-io/goagent/contrib/llm-providers/cohere"
	_ "github.com/kart-io/goagent/contrib/llm-providers/deepseek"
	_ "github.com/kart-io/goagent/contrib/llm-providers/gemini"
	_ "github.com/kart-io/goagent/contrib/llm-providers/huggingface"
	_ "github.com/kart-io/goagent/contrib/llm-providers/kimi"
	_ "github.com/kart-io/goagent/contrib/llm-providers/ollama"
	_ "github.com/kart-io/goagent/contrib/llm-providers/openai"
	_ "github.com/kart-io/goagent/contrib/llm-providers/siliconflow"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/llm/registry"
)

func main() {
	fmt.Println("=== Provider Registry 示例 ===\n")

	// 1. 列出所有已注册的 providers
	fmt.Println("已注册的 Providers:")
	providers := registry.List()
	for _, provider := range providers {
		fmt.Printf("  - %s\n", provider)
	}
	fmt.Println()

	// 2. 检查特定 provider 是否已注册
	if registry.IsRegistered(constants.ProviderOpenAI) {
		fmt.Println("✓ OpenAI provider 已注册")
	}
	fmt.Println()

	// 3. 使用 registry 创建 provider 实例（方法 1：使用 registry.New）
	fmt.Println("方法 1: 使用 registry.New 创建 provider")
	openaiClient, err := registry.New(
		constants.ProviderOpenAI,
		agentllm.WithAPIKey("your-api-key"),
		agentllm.WithModel("gpt-3.5-turbo"),
	)
	if err != nil {
		log.Printf("创建 OpenAI provider 失败: %v\n", err)
	} else {
		fmt.Printf("  成功创建 OpenAI provider: %T\n", openaiClient)
	}
	fmt.Println()

	// 4. 使用 registry 创建 provider 实例（方法 2：手动获取工厂函数）
	fmt.Println("方法 2: 手动获取工厂函数")
	factory, err := registry.Get(constants.ProviderDeepSeek)
	if err != nil {
		log.Fatal(err)
	}

	deepseekClient, err := factory(
		agentllm.WithAPIKey("your-api-key"),
		agentllm.WithModel("deepseek-chat"),
	)
	if err != nil {
		log.Printf("创建 DeepSeek provider 失败: %v\n", err)
	} else {
		fmt.Printf("  成功创建 DeepSeek provider: %T\n", deepseekClient)
	}
	fmt.Println()

	// 5. 动态选择 provider（基于配置或用户输入）
	fmt.Println("方法 3: 动态选择 provider")
	selectedProvider := constants.ProviderOpenAI // 可以从配置文件或环境变量读取

	client, err := registry.New(
		selectedProvider,
		agentllm.WithAPIKey("your-api-key"),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("  动态创建的 provider: %s (%T)\n", selectedProvider, client)
	fmt.Println()

	// 6. 批量创建多个 providers
	fmt.Println("方法 4: 批量创建多个 providers")
	providerNames := []constants.Provider{
		constants.ProviderOpenAI,
		constants.ProviderDeepSeek,
		constants.ProviderGemini,
	}

	clients := make(map[constants.Provider]agentllm.Client)
	for _, providerName := range providerNames {
		if registry.IsRegistered(providerName) {
			client, err := registry.New(
				providerName,
				agentllm.WithAPIKey("your-api-key"),
			)
			if err != nil {
				log.Printf("  创建 %s 失败: %v\n", providerName, err)
				continue
			}
			clients[providerName] = client
			fmt.Printf("  ✓ %s 创建成功\n", providerName)
		}
	}
	fmt.Println()

	// 7. 实际使用 provider 发送请求（示例）
	fmt.Println("实际使用示例（需要有效的 API key）:")

	// 注意：以下代码需要有效的 API key 才能运行
	// 取消注释以下代码来测试实际请求

	/*
	ctx := context.Background()
	req := &agentllm.CompletionRequest{
		Messages: []agentllm.Message{
			{Role: "user", Content: "Hello, how are you?"},
		},
	}

	if openaiClient != nil {
		resp, err := openaiClient.Complete(ctx, req)
		if err != nil {
			log.Printf("请求失败: %v\n", err)
		} else {
			fmt.Printf("响应: %s\n", resp.Content)
		}
	}
	*/

	fmt.Println("  （需要有效的 API key 才能测试实际请求）")
	fmt.Println()

	// 8. 展示如何实现 provider 切换逻辑
	fmt.Println("Provider 切换示例:")
	demonstrateProviderSwitching()
}

// demonstrateProviderSwitching 展示如何实现灵活的 provider 切换
func demonstrateProviderSwitching() {
	// 定义 provider 优先级列表
	providerPriority := []constants.Provider{
		constants.ProviderOpenAI,
		constants.ProviderDeepSeek,
		constants.ProviderGemini,
		constants.ProviderOllama, // Fallback 到本地模型
	}

	// 尝试按优先级创建 provider
	var client agentllm.Client
	var err error

	for _, provider := range providerPriority {
		if !registry.IsRegistered(provider) {
			fmt.Printf("  ✗ %s 未注册，跳过\n", provider)
			continue
		}

		client, err = registry.New(
			provider,
			agentllm.WithAPIKey("your-api-key"),
		)
		if err == nil {
			fmt.Printf("  ✓ 成功使用 provider: %s\n", provider)
			break
		}
		fmt.Printf("  ✗ %s 创建失败: %v，尝试下一个\n", provider, err)
	}

	if client == nil {
		fmt.Println("  ✗ 所有 providers 都不可用")
	}
}

// 辅助函数：演示如何根据用户输入选择 provider
func selectProviderByName(name string) (agentllm.Client, error) {
	var provider constants.Provider

	switch name {
	case "openai":
		provider = constants.ProviderOpenAI
	case "deepseek":
		provider = constants.ProviderDeepSeek
	case "gemini":
		provider = constants.ProviderGemini
	case "anthropic":
		provider = constants.ProviderAnthropic
	case "ollama":
		provider = constants.ProviderOllama
	default:
		return nil, fmt.Errorf("unknown provider: %s", name)
	}

	return registry.New(
		provider,
		agentllm.WithAPIKey("your-api-key"),
	)
}
