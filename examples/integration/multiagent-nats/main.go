package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/kart-io/goagent/multiagent"
	"github.com/nats-io/nats.go"
)

func main() {
	// 检查是否运行 AI 协作模式
	mode := os.Getenv("MODE")
	if mode == "ai" || mode == "AI" {
		RunAICollaboration()
		return
	}

	fmt.Println("=== NATS-Based Multi-Agent Communication Example ===")
	fmt.Println()
	fmt.Println("Running Mode: Basic Communication Demo")
	fmt.Println("For AI-powered collaboration, set MODE=ai and DEEPSEEK_API_KEY")
	fmt.Println()

	// 获取 NATS 服务器地址（默认本地）
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = nats.DefaultURL // "nats://localhost:4222"
	}

	fmt.Printf("Connecting to NATS server: %s\n", natsURL)
	fmt.Println("Note: This example requires a running NATS server")
	fmt.Println("      Start NATS: docker-compose up -d nats")
	fmt.Println()

	// 创建 NATS ChannelStore
	natsStore, err := NewNATSChannelStore(natsURL)
	if err != nil {
		log.Printf("⚠️  Failed to connect to NATS: %v", err)
		log.Println("⚠️  Falling back to in-memory store for demonstration")
		log.Println()

		// 回退到内存存储用于演示
		runWithInMemoryStore()
		return
	}
	defer func() {
		if err := natsStore.Close(); err != nil {
			log.Printf("Warning: failed to close NATS store: %v", err)
		}
	}()

	fmt.Println("✓ Connected to NATS server")
	fmt.Println()

	ctx := context.Background()

	// 1. 创建使用 NATS store 的 MemoryCommunicators
	fmt.Println("1. Creating Agents with NATS-backed Communication...")
	agent1Comm := multiagent.NewMemoryCommunicatorWithStore("agent-1", natsStore)
	agent2Comm := multiagent.NewMemoryCommunicatorWithStore("agent-2", natsStore)
	agent3Comm := multiagent.NewMemoryCommunicatorWithStore("agent-3", natsStore)

	defer func() {
		if err := agent1Comm.Close(); err != nil {
			log.Printf("Warning: failed to close agent1: %v", err)
		}
	}()
	defer func() {
		if err := agent2Comm.Close(); err != nil {
			log.Printf("Warning: failed to close agent2: %v", err)
		}
	}()
	defer func() {
		if err := agent3Comm.Close(); err != nil {
			log.Printf("Warning: failed to close agent3: %v", err)
		}
	}()

	fmt.Println("   Agent 1: agent-1 (Coordinator)")
	fmt.Println("   Agent 2: agent-2 (Worker)")
	fmt.Println("   Agent 3: agent-3 (Worker)")
	fmt.Println()

	// 2. 演示点对点通信（通过 NATS）
	fmt.Println("2. Point-to-Point Communication via NATS...")

	done := make(chan struct{})

	// Agent 2 监听消息
	go func() {
		msg, err := agent2Comm.Receive(ctx)
		if err != nil {
			log.Printf("Failed to receive: %v", err)
			close(done)
			return
		}
		fmt.Printf("   Agent 2 received: %v (From: %s)\n", msg.Payload, msg.From)

		// Agent 2 回复
		response := multiagent.NewAgentMessage("agent-2", "agent-1", multiagent.MessageTypeResponse, map[string]string{
			"status": "task completed",
			"result": "data processed successfully",
		})

		// 通过 NATS 发布回复
		if err := natsStore.PublishMessage("agent-1", response); err != nil {
			log.Printf("Failed to send response: %v", err)
		} else {
			fmt.Println("   Agent 2 → Agent 1: Response sent via NATS")
		}
		close(done)
	}()

	// 给 goroutine 时间启动
	time.Sleep(100 * time.Millisecond)

	// Agent 1 发送任务
	taskMsg := multiagent.NewAgentMessage("agent-1", "agent-2", multiagent.MessageTypeCommand, map[string]string{
		"task": "process data",
		"data": "sample dataset",
	})

	if err := natsStore.PublishMessage("agent-2", taskMsg); err != nil {
		log.Fatalf("Failed to send task: %v", err)
	}
	fmt.Println("   Agent 1 → Agent 2: Task sent via NATS")

	// 等待处理完成
	<-done
	fmt.Println("✓ Point-to-point communication completed")
	fmt.Println()

	// 3. 演示广播通信
	fmt.Println("3. Broadcast Communication via NATS...")

	// 创建用于接收广播的 goroutines
	agent2Done := make(chan struct{})
	agent3Done := make(chan struct{})

	go func() {
		msg, err := agent2Comm.Receive(ctx)
		if err != nil {
			log.Printf("Agent 2 receive error: %v", err)
			close(agent2Done)
			return
		}
		fmt.Printf("   Agent 2 received broadcast: %v\n", msg.Payload)
		close(agent2Done)
	}()

	go func() {
		msg, err := agent3Comm.Receive(ctx)
		if err != nil {
			log.Printf("Agent 3 receive error: %v", err)
			close(agent3Done)
			return
		}
		fmt.Printf("   Agent 3 received broadcast: %v\n", msg.Payload)
		close(agent3Done)
	}()

	time.Sleep(100 * time.Millisecond)

	// Agent 1 广播系统通知
	broadcastMsg := multiagent.NewAgentMessage("agent-1", "", multiagent.MessageTypeBroadcast, map[string]string{
		"event":   "system_maintenance",
		"message": "System will restart in 5 minutes",
	})

	// 发送给所有其他 agents
	for _, agentID := range []string{"agent-2", "agent-3"} {
		if err := natsStore.PublishMessage(agentID, broadcastMsg); err != nil {
			log.Printf("Failed to broadcast to %s: %v", agentID, err)
		}
	}
	fmt.Println("   Agent 1 broadcasted system notification via NATS")

	<-agent2Done
	<-agent3Done
	fmt.Println("✓ Broadcast communication completed")
	fmt.Println()

	// 4. 演示发布/订阅模式
	fmt.Println("4. Pub/Sub Pattern via NATS...")

	// Agents 订阅 "alerts" 主题
	alertCh2, err := agent2Comm.Subscribe(ctx, "alerts")
	if err != nil {
		log.Fatalf("Failed to subscribe: %v", err)
	}
	fmt.Println("   Agent 2 subscribed to 'alerts' topic")

	alertCh3, err := agent3Comm.Subscribe(ctx, "alerts")
	if err != nil {
		log.Fatalf("Failed to subscribe: %v", err)
	}
	fmt.Println("   Agent 3 subscribed to 'alerts' topic")

	time.Sleep(200 * time.Millisecond)

	// Agent 1 发布告警
	alertMsg := multiagent.NewAgentMessage("agent-1", "", multiagent.MessageTypeNotification, map[string]string{
		"severity": "high",
		"message":  "CPU usage exceeded 90%",
	})
	alertMsg.Topic = "alerts"

	if err := natsStore.PublishToTopic("alerts", alertMsg); err != nil {
		log.Fatalf("Failed to publish alert: %v", err)
	}
	fmt.Println("   Agent 1 published alert to 'alerts' topic")

	// 等待订阅者接收
	agent2Received := false
	agent3Received := false

	for i := 0; i < 2; i++ {
		select {
		case msg := <-alertCh2:
			fmt.Printf("   Agent 2 received alert: %v\n", msg.Payload)
			agent2Received = true
		case msg := <-alertCh3:
			fmt.Printf("   Agent 3 received alert: %v\n", msg.Payload)
			agent3Received = true
		case <-time.After(2 * time.Second):
			fmt.Println("   Timeout waiting for alert delivery")
		}
	}

	if agent2Received && agent3Received {
		fmt.Println("✓ Pub/Sub pattern completed")
	} else {
		fmt.Println("⚠️  Some subscribers did not receive the message")
	}
	fmt.Println()

	// 5. 演示分布式协作场景
	fmt.Println("5. Distributed Collaboration Scenario...")
	fmt.Println("   Scenario: Coordinator assigns tasks to distributed workers")
	fmt.Println()

	fmt.Println("   Coordinator (Agent 1) → Worker 1 (Agent 2): Analyze dataset A")
	task1 := multiagent.NewAgentMessage("agent-1", "agent-2", multiagent.MessageTypeCommand, map[string]string{
		"task":    "analyze",
		"dataset": "A",
	})
	if err := natsStore.PublishMessage("agent-2", task1); err != nil {
		log.Printf("Warning: failed to publish task1: %v", err)
	}

	fmt.Println("   Coordinator (Agent 1) → Worker 2 (Agent 3): Analyze dataset B")
	task2 := multiagent.NewAgentMessage("agent-1", "agent-3", multiagent.MessageTypeCommand, map[string]string{
		"task":    "analyze",
		"dataset": "B",
	})
	if err := natsStore.PublishMessage("agent-3", task2); err != nil {
		log.Printf("Warning: failed to publish task2: %v", err)
	}

	fmt.Println()
	fmt.Println("✓ Distributed collaboration scenario demonstrated")
	fmt.Println()

	fmt.Println("=== Example Completed Successfully ===")
	fmt.Println()
	fmt.Println("Key Benefits of NATS-based Communication:")
	fmt.Println("• Distributed: Agents can run on different machines")
	fmt.Println("• Scalable: NATS handles millions of messages per second")
	fmt.Println("• Reliable: Built-in message persistence and delivery guarantees")
	fmt.Println("• Flexible: Easy to implement custom ChannelStore backends")
}

// runWithInMemoryStore 使用内存存储运行演示（当 NATS 不可用时）
func runWithInMemoryStore() {
	fmt.Println("=== Running with In-Memory Store (Fallback) ===")
	fmt.Println()

	ctx := context.Background()

	// 创建内存通信器
	agent1Comm := multiagent.NewMemoryCommunicator("agent-1")
	agent2Comm := multiagent.NewMemoryCommunicator("agent-2")

	defer func() {
		if err := agent1Comm.Close(); err != nil {
			log.Printf("Warning: failed to close agent1: %v", err)
		}
	}()
	defer func() {
		if err := agent2Comm.Close(); err != nil {
			log.Printf("Warning: failed to close agent2: %v", err)
		}
	}()

	done := make(chan struct{})

	// Agent 2 监听
	go func() {
		msg, err := agent2Comm.Receive(ctx)
		if err != nil {
			log.Printf("Failed to receive: %v", err)
			close(done)
			return
		}
		fmt.Printf("Agent 2 received: %v (From: %s)\n", msg.Payload, msg.From)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)

	// Agent 1 发送消息
	testMsg := multiagent.NewAgentMessage("agent-1", "agent-2", multiagent.MessageTypeRequest, "Hello from Agent 1")
	if err := agent1Comm.Send(ctx, "agent-2", testMsg); err != nil {
		log.Fatalf("Failed to send: %v", err)
	}
	fmt.Println("Agent 1 → Agent 2: Message sent")

	<-done
	fmt.Println()
	fmt.Println("✓ In-memory communication successful")
	fmt.Println()
	fmt.Println("To use NATS, please start a NATS server:")
	fmt.Println("  docker run -p 4222:4222 nats:latest")
}
