package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/stream"
)

// 演示流式响应的完整示例
func main() {
	fmt.Println("=== 流式响应示例 ===")

	// 示例 1: LLM 流式补全
	example1LLMStream()

	fmt.Println()

	// 示例 2: 流式管理器处理
	example2StreamManager()

	fmt.Println()

	// 示例 3: 流式多路复用
	example3StreamMultiplexer()

	fmt.Println()

	// 示例 4: 流式速率限制
	example4StreamRateLimiter()
}

// 示例 1: LLM 流式补全
func example1LLMStream() {
	fmt.Println("--- 示例 1: LLM 流式补全 ---")

	ctx := context.Background()

	// 创建模拟流式客户端
	client := llm.NewMockStreamClient()

	// 创建请求
	req := &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.UserMessage("Tell me a short story about AI."),
		},
		Temperature: 0.7,
		MaxTokens:   100,
	}

	// 流式补全
	stream, err := client.CompleteStream(ctx, req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// 读取流式输出
	fmt.Print("Response: ")
	var fullContent string
	for chunk := range stream {
		if chunk.Error != nil {
			fmt.Printf("\nError: %v\n", chunk.Error)
			break
		}

		// 打印增量内容
		if chunk.Delta != "" {
			fmt.Print(chunk.Delta)
			fullContent = chunk.Content
		}

		if chunk.Done {
			fmt.Println()
			if chunk.Usage != nil {
				fmt.Printf("Tokens used: %d\n", chunk.Usage.TotalTokens)
			}
			break
		}
	}

	fmt.Printf("Full content: %s\n", fullContent)
}

// 示例 2: 流式管理器处理
func example2StreamManager() {
	fmt.Println("--- 示例 2: 流式管理器处理 ---")

	ctx := context.Background()

	// 创建流式管理器
	manager := stream.NewStreamManager(stream.StreamManagerConfig{
		BufferSize: 10,
		Timeout:    30 * time.Second,
	})

	// 创建模拟数据流
	dataStream := make(chan *stream.StreamChunk, 10)

	go func() {
		defer close(dataStream)

		for i := 1; i <= 5; i++ {
			dataStream <- stream.NewStreamChunk(fmt.Sprintf("Data chunk %d", i))
			time.Sleep(200 * time.Millisecond)
		}

		// 最后一个块
		finalChunk := stream.NewStreamChunk("Final chunk")
		finalChunk.Done = true
		dataStream <- finalChunk
	}()

	// 创建处理器
	handler := stream.NewFuncStreamHandler(
		func(chunk *stream.StreamChunk) error {
			fmt.Printf("Received: %v\n", chunk.Data)
			return nil
		},
		func() error {
			fmt.Println("Stream completed")
			return nil
		},
		func(err error) error {
			fmt.Printf("Error: %v\n", err)
			return err
		},
	)

	// 处理流
	if err := manager.Process(ctx, dataStream, handler); err != nil {
		fmt.Printf("Stream processing error: %v\n", err)
	}
}

// 示例 3: 流式多路复用
func example3StreamMultiplexer() {
	fmt.Println("--- 示例 3: 流式多路复用 ---")

	ctx := context.Background()

	// 创建输入流
	input := make(chan *stream.StreamChunk, 10)

	// 创建多路复用器
	multiplexer := stream.NewStreamMultiplexer(input)

	// 添加两个消费者
	consumer1 := multiplexer.AddConsumer(10)
	consumer2 := multiplexer.AddConsumer(10)

	// 启动多路复用器
	go func() {
		if err := multiplexer.Start(ctx); err != nil {
			fmt.Printf("Multiplexer error: %v\n", err)
		}
	}()

	// 消费者 1
	go func() {
		for chunk := range consumer1 {
			fmt.Printf("Consumer 1 received: %v\n", chunk.Data)
		}
		fmt.Println("Consumer 1 closed")
	}()

	// 消费者 2
	go func() {
		for chunk := range consumer2 {
			fmt.Printf("Consumer 2 received: %v\n", chunk.Data)
		}
		fmt.Println("Consumer 2 closed")
	}()

	// 发送数据
	for i := 1; i <= 3; i++ {
		input <- stream.NewStreamChunk(fmt.Sprintf("Broadcast %d", i))
		time.Sleep(300 * time.Millisecond)
	}

	// 发送完成信号
	finalChunk := stream.NewStreamChunk("Broadcast complete")
	finalChunk.Done = true
	input <- finalChunk
	close(input)

	time.Sleep(time.Second) // 等待消费者处理完成
}

// 示例 4: 流式速率限制
func example4StreamRateLimiter() {
	fmt.Println("--- 示例 4: 流式速率限制 ---")

	ctx := context.Background()

	// 创建速率限制器（2 块/秒）
	limiter := stream.NewStreamRateLimiter(2)

	// 创建快速数据流
	input := make(chan *stream.StreamChunk, 10)

	go func() {
		defer close(input)

		for i := 1; i <= 5; i++ {
			input <- stream.NewStreamChunk(fmt.Sprintf("Fast chunk %d", i))
		}

		finalChunk := stream.NewStreamChunk("Done")
		finalChunk.Done = true
		input <- finalChunk
	}()

	// 应用速率限制
	limited := limiter.Limit(ctx, input)

	// 接收限速后的数据
	startTime := time.Now()
	for chunk := range limited {
		elapsed := time.Since(startTime).Seconds()
		fmt.Printf("[%.2fs] Received: %v\n", elapsed, chunk.Data)

		if chunk.Done {
			break
		}
	}

	fmt.Println("Rate limiting completed")
}
