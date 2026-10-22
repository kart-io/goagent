package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

func main() {
	provider, err := providers.NewKimiWithOptions(llm.WithAPIKey(os.Getenv("KIMI_API_KEY")), llm.WithModel("moonshot-v1-8k"))
	if err != nil {
		log.Fatalf("Failed to create Kimi provider: %v", err)
	}

	ctx := context.Background()
	resp, err := provider.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "What is deep learning?"},
			{Role: "assistant", Content: "Deep learning is a subset of machine learning that is concerned with algorithms and models that learn from data that is unstructured or unlabeled. It is a type of machine learning that is used to find patterns in data."},
			{Role: "user", Content: "What is the capital of France?"},
		},
	})
	if err != nil {
		log.Fatalf("Failed to complete: %v", err)
	}
	fmt.Println(resp.Content)
	chatExample()
}

func chatExample() {
	provider, err := providers.NewKimiWithOptions(llm.WithAPIKey(os.Getenv("KIMI_API_KEY")), llm.WithModel("moonshot-v1-8k"))
	if err != nil {
		log.Fatalf("Failed to create Kimi provider: %v", err)
	}

	ctx := context.Background()
	resp, err := provider.Chat(ctx, []llm.Message{
		{Role: "system", Content: "You are a helpful assistant. please answer the question in the chinese as the question"},
		{Role: "user", Content: "What is the capital of France?"},
	})
	if err != nil {
		log.Fatalf("Failed to chat: %v", err)
	}
	fmt.Println(resp.Content)
}
