package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

// MaskingSerializer masks sensitive data in input
type MaskingSerializer struct{}

func (s *MaskingSerializer) Serialize(input interface{}) (string, error) {
	str := fmt.Sprintf("%v", input)
	// Simple masking example: mask emails
	// In a real app, use regex
	if strings.Contains(str, "@") {
		return "MASKED_EMAIL", nil
	}
	return str, nil
}

func main() {
	// Check API Key
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		log.Fatal("DEEPSEEK_API_KEY environment variable is required")
	}

	// Initialize DeepSeek Client
	client, err := providers.NewDeepSeekWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("deepseek-chat"),
	)
	if err != nil {
		log.Fatalf("Failed to create DeepSeek client: %v", err)
	}

	// 1. Default Serializer
	fmt.Println("--- Default Serializer ---")
	agent1, _ := builder.NewSimpleBuilder(client).Build()
	// We expect the LLM to receive the raw email
	res1, err := agent1.Execute(context.Background(), "user@example.com")
	if err != nil {
		log.Printf("Agent1 execution failed: %v", err)
	} else {
		fmt.Printf("Result: %s\n", res1.Result)
	}

	// 2. Custom Masking Serializer
	fmt.Println("\n--- Custom Masking Serializer ---")
	agent2, _ := builder.NewSimpleBuilder(client).
		WithInputSerializer(&MaskingSerializer{}).
		WithSystemPrompt("你是一个有用的助手。请始终使用中文回答用户的请求。如果收到 'MASKED_EMAIL'，请解释什么是脱敏数据以及为什么保护隐私很重要。").
		Build()

	// We expect the LLM to receive "MASKED_EMAIL"
	res2, err := agent2.Execute(context.Background(), "user@example.com")
	if err != nil {
		log.Printf("Agent2 execution failed: %v", err)
	} else {
		fmt.Printf("Result: %s\n", res2.Result)
	}
}
