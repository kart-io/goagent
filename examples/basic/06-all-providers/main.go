package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

func main() {
	fmt.Println("=== All LLM Providers Test ===")
	fmt.Println("Testing all 6 LLM providers implementation")
	fmt.Println()

	ctx := context.Background()

	// 1. Test OpenAI Provider
	fmt.Println("1. Testing OpenAI Provider")
	fmt.Println("--------------------------")
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		config := &llm.Config{
			Provider:    llm.ProviderOpenAI,
			APIKey:      apiKey,
			Model:       "gpt-3.5-turbo",
			MaxTokens:   100,
			Temperature: 0.7,
		}

		client, err := providers.NewOpenAI(config)
		if err != nil {
			fmt.Printf("   ❌ Error creating OpenAI client: %v\n", err)
		} else {
			testProvider(ctx, client, "OpenAI")
		}
	} else {
		fmt.Println("   ⚠️  OPENAI_API_KEY not set, skipping")
	}
	fmt.Println()

	// 2. Test Gemini Provider
	fmt.Println("2. Testing Gemini Provider")
	fmt.Println("--------------------------")
	if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		config := &llm.Config{
			Provider:    llm.ProviderGemini,
			APIKey:      apiKey,
			Model:       "gemini-pro",
			MaxTokens:   100,
			Temperature: 0.7,
		}

		client, err := providers.NewGemini(config)
		if err != nil {
			fmt.Printf("   ❌ Error creating Gemini client: %v\n", err)
		} else {
			testProvider(ctx, client, "Gemini")
		}
	} else {
		fmt.Println("   ⚠️  GEMINI_API_KEY not set, skipping")
	}
	fmt.Println()

	// 3. Test DeepSeek Provider
	fmt.Println("3. Testing DeepSeek Provider")
	fmt.Println("----------------------------")
	if apiKey := os.Getenv("DEEPSEEK_API_KEY"); apiKey != "" {
		config := &llm.Config{
			Provider:    llm.ProviderDeepSeek,
			APIKey:      apiKey,
			BaseURL:     "https://api.deepseek.com/v1",
			Model:       "deepseek-chat",
			MaxTokens:   100,
			Temperature: 0.7,
		}

		client, err := providers.NewDeepSeek(config)
		if err != nil {
			fmt.Printf("   ❌ Error creating DeepSeek client: %v\n", err)
		} else {
			testProvider(ctx, client, "DeepSeek")
		}
	} else {
		fmt.Println("   ⚠️  DEEPSEEK_API_KEY not set, skipping")
	}
	fmt.Println()

	// 4. Test Ollama Provider (Local)
	fmt.Println("4. Testing Ollama Provider")
	fmt.Println("--------------------------")
	ollamaClient := providers.NewOllamaClientSimple("llama2")
	if ollamaClient.IsAvailable() {
		testProvider(ctx, ollamaClient, "Ollama")
	} else {
		fmt.Println("   ⚠️  Ollama not running locally, skipping")
		fmt.Println("   💡 Start Ollama with: ollama serve")
		fmt.Println("   💡 Pull a model with: ollama pull llama2")
	}
	fmt.Println()

	// 5. Test SiliconFlow Provider (New!)
	fmt.Println("5. Testing SiliconFlow Provider")
	fmt.Println("-------------------------------")
	if apiKey := os.Getenv("SILICONFLOW_API_KEY"); apiKey != "" {
		config := &llm.Config{
			Provider:    llm.ProviderSiliconFlow,
			APIKey:      apiKey,
			Model:       "Qwen/Qwen2-7B-Instruct",
			MaxTokens:   100,
			Temperature: 0.7,
		}

		client, err := providers.NewSiliconFlow(config)
		if err != nil {
			fmt.Printf("   ❌ Error creating SiliconFlow client: %v\n", err)
		} else {
			testProvider(ctx, client, "SiliconFlow")

			// Show available models
			fmt.Println("   📝 Available SiliconFlow models:")
			models := client.ListModels()
			for i, model := range models[:5] { // Show first 5 models
				fmt.Printf("      - %s\n", model)
				if i == 4 {
					fmt.Printf("      ... and %d more models\n", len(models)-5)
				}
			}
		}
	} else {
		fmt.Println("   ⚠️  SILICONFLOW_API_KEY not set, skipping")
		fmt.Println("   💡 Get API key from: https://siliconflow.cn")
	}
	fmt.Println()

	// 6. Test Kimi Provider (New!)
	fmt.Println("6. Testing Kimi Provider")
	fmt.Println("------------------------")
	if apiKey := os.Getenv("KIMI_API_KEY"); apiKey != "" {
		config := &llm.Config{
			Provider:    llm.ProviderKimi,
			APIKey:      apiKey,
			Model:       "moonshot-v1-8k",
			MaxTokens:   100,
			Temperature: 0.7,
		}

		client, err := providers.NewKimi(config)
		if err != nil {
			fmt.Printf("   ❌ Error creating Kimi client: %v\n", err)
		} else {
			testProvider(ctx, client, "Kimi")

			// Show Kimi's special features
			fmt.Println("   📝 Kimi special features:")
			fmt.Println("      - Supports up to 128K context (moonshot-v1-128k)")
			fmt.Println("      - Excellent Chinese language support")
			fmt.Println("      - File upload and processing capabilities")

			// Show supported models
			fmt.Println("   📝 Supported models:")
			for _, model := range client.GetSupportedModels() {
				contextSize := client.GetModelContextSize(model)
				fmt.Printf("      - %s (context: %dK tokens)\n", model, contextSize/1000)
			}
		}
	} else {
		fmt.Println("   ⚠️  KIMI_API_KEY not set, skipping")
		fmt.Println("   💡 Get API key from: https://platform.moonshot.cn")
	}
	fmt.Println()

	// Summary
	fmt.Println("=== Summary ===")
	fmt.Println("All 6 LLM providers have been implemented:")
	fmt.Println("✅ OpenAI - Most mature, full-featured")
	fmt.Println("✅ Gemini - Google's multimodal model")
	fmt.Println("✅ DeepSeek - Chinese optimized, strong coding")
	fmt.Println("✅ Ollama - Local execution, privacy-first")
	fmt.Println("✅ SiliconFlow - Multiple open-source models")
	fmt.Println("✅ Kimi - Ultra-long context (up to 128K)")
	fmt.Println()
	fmt.Println("All providers implement the same llm.Client interface,")
	fmt.Println("making them fully interchangeable in your code!")
}

// testProvider tests a single provider
func testProvider(ctx context.Context, client llm.Client, name string) {
	// Test IsAvailable
	available := client.IsAvailable()
	fmt.Printf("   IsAvailable: %v\n", available)

	if !available {
		fmt.Printf("   ⚠️  %s is not available\n", name)
		return
	}

	// Test Chat
	testMessages := []llm.Message{
		llm.UserMessage("Say 'Hello from " + name + "!' exactly"),
	}

	// Add timeout
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	response, err := client.Chat(ctx, testMessages)
	if err != nil {
		fmt.Printf("   ❌ Chat error: %v\n", err)
		return
	}

	fmt.Printf("   ✅ Response: %s\n", response.Content)
	fmt.Printf("   Provider: %s, Model: %s\n", response.Provider, response.Model)
}

// Example of provider switching based on requirements
func selectProviderByRequirement(requirement string) llm.Client {
	switch requirement {
	case "long-context":
		// Use Kimi for long context
		config := &llm.Config{
			Provider: llm.ProviderKimi,
			APIKey:   os.Getenv("KIMI_API_KEY"),
			Model:    "moonshot-v1-128k",
		}
		client, _ := providers.NewKimi(config)
		return client

	case "local-privacy":
		// Use Ollama for local execution
		return providers.NewOllamaClientSimple("llama2")

	case "chinese":
		// Use DeepSeek or Kimi for Chinese
		config := &llm.Config{
			Provider: llm.ProviderDeepSeek,
			APIKey:   os.Getenv("DEEPSEEK_API_KEY"),
			Model:    "deepseek-chat",
		}
		client, _ := providers.NewDeepSeek(config)
		return client

	case "coding":
		// Use DeepSeek-Coder or Codellama
		if os.Getenv("DEEPSEEK_API_KEY") != "" {
			config := &llm.Config{
				Provider: llm.ProviderDeepSeek,
				APIKey:   os.Getenv("DEEPSEEK_API_KEY"),
				Model:    "deepseek-coder",
			}
			client, _ := providers.NewDeepSeek(config)
			return client
		}
		// Fallback to Ollama Codellama
		return providers.NewOllamaClientSimple("codellama")

	case "multimodal":
		// Use Gemini for multimodal
		config := &llm.Config{
			Provider: llm.ProviderGemini,
			APIKey:   os.Getenv("GEMINI_API_KEY"),
			Model:    "gemini-pro-vision",
		}
		client, _ := providers.NewGemini(config)
		return client

	case "open-source":
		// Use SiliconFlow for open-source models
		config := &llm.Config{
			Provider: llm.ProviderSiliconFlow,
			APIKey:   os.Getenv("SILICONFLOW_API_KEY"),
			Model:    "meta-llama/Meta-Llama-3.1-70B-Instruct",
		}
		client, _ := providers.NewSiliconFlow(config)
		return client

	default:
		// Default to OpenAI
		config := &llm.Config{
			Provider: llm.ProviderOpenAI,
			APIKey:   os.Getenv("OPENAI_API_KEY"),
			Model:    "gpt-3.5-turbo",
		}
		client, _ := providers.NewOpenAI(config)
		return client
	}
}
