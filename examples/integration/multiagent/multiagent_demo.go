package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kart-io/goagent/multiagent"
)

func main() {
	fmt.Println("=== Multi-Agent Communication Example ===")

	// 1. 创建内存通信器（单机多Agent）
	fmt.Println("1. Creating Memory Communicators for 3 Agents...")

	agent1Comm := multiagent.NewMemoryCommunicator("agent-1")
	agent2Comm := multiagent.NewMemoryCommunicator("agent-2")
	agent3Comm := multiagent.NewMemoryCommunicator("agent-3")

	defer agent1Comm.Close()
	defer agent2Comm.Close()
	defer agent3Comm.Close()

	fmt.Println("✓ Communicators created")

	// 2. 演示点对点通信
	fmt.Println("2. Demonstrating Point-to-Point Communication...")

	ctx := context.Background()

	// Agent 1 发送消息给 Agent 2
	msg := multiagent.NewAgentMessage("agent-1", "agent-2", multiagent.MessageTypeRequest, map[string]string{
		"task": "analyze data",
	})
	msg.Metadata["priority"] = "high"

	if err := agent1Comm.Send(ctx, "agent-2", msg); err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}
	fmt.Printf("Agent 1 → Agent 2: %s (Type: %s)\n", msg.Payload, msg.Type)

	// Agent 2 接收消息
	go func() {
		receivedMsg, err := agent2Comm.Receive(ctx)
		if err != nil {
			log.Printf("Failed to receive: %v", err)
			return
		}
		fmt.Printf("Agent 2 received: %v (From: %s)\n", receivedMsg.Payload, receivedMsg.From)

		// Agent 2 回复
		response := multiagent.NewAgentMessage("agent-2", "agent-1", multiagent.MessageTypeResponse, map[string]string{
			"result": "analysis complete",
		})
		agent2Comm.Send(ctx, "agent-1", response)
		fmt.Printf("Agent 2 → Agent 1: Response sent\n")
	}()

	time.Sleep(100 * time.Millisecond)
	fmt.Println("✓ Point-to-point communication demonstrated")

	// 3. 演示广播通信
	fmt.Println("3. Demonstrating Broadcast Communication...")

	broadcastMsg := multiagent.NewAgentMessage("agent-1", "", multiagent.MessageTypeNotification, map[string]string{
		"event": "system_ready",
	})

	if err := agent1Comm.Broadcast(ctx, broadcastMsg); err != nil {
		log.Fatalf("Failed to broadcast: %v", err)
	}
	fmt.Println("Agent 1 broadcasted: system_ready event")
	fmt.Println("✓ Broadcast demonstrated")

	// 4. 演示发布/订阅模式
	fmt.Println("4. Demonstrating Pub/Sub Pattern...")

	// Agent 2 和 Agent 3 订阅 "notifications" 主题
	topic := "notifications"

	ch2, err := agent2Comm.Subscribe(ctx, topic)
	if err != nil {
		log.Fatalf("Failed to subscribe: %v", err)
	}
	fmt.Println("Agent 2 subscribed to 'notifications'")

	ch3, err := agent3Comm.Subscribe(ctx, topic)
	if err != nil {
		log.Fatalf("Failed to subscribe: %v", err)
	}
	fmt.Println("Agent 3 subscribed to 'notifications'")

	// Agent 1 发布到主题
	pubMsg := multiagent.NewAgentMessage("agent-1", "", multiagent.MessageTypeNotification, map[string]string{
		"notification": "new task available",
	})
	pubMsg.Topic = topic
	time.Sleep(200 * time.Millisecond)
	// 演示订阅 (在实际实现中，订阅者会监听主题消息)
	fmt.Println("(Note: In actual implementation, subscribers would listen for published messages)")
	_ = ch2 // Subscribers would read from these channels
	_ = ch3

	fmt.Println("✓ Pub/Sub pattern demonstrated")

	// 5. 演示消息路由
	fmt.Println("5. Demonstrating Message Router...")

	router := multiagent.NewMessageRouter()

	// 注册路由处理器
	router.RegisterRoute("task.analyze", func(ctx context.Context, msg *multiagent.AgentMessage) (*multiagent.AgentMessage, error) {
		fmt.Printf("Router handling 'task.analyze': %v\n", msg.Payload)
		return multiagent.NewAgentMessage("router", msg.From, multiagent.MessageTypeResponse, map[string]string{
			"status": "processing",
		}), nil
	})

	router.RegisterRoute("task.report", func(ctx context.Context, msg *multiagent.AgentMessage) (*multiagent.AgentMessage, error) {
		fmt.Printf("Router handling 'task.report': %v\n", msg.Payload)
		return multiagent.NewAgentMessage("router", msg.From, multiagent.MessageTypeResponse, map[string]string{
			"status": "reported",
		}), nil
	})

	// 路由消息
	routeMsg1 := multiagent.NewAgentMessage("agent-1", "", multiagent.MessageTypeCommand, "do analysis")
	routeMsg1.Topic = "task.analyze"

	response1, err := router.Route(ctx, routeMsg1)
	if err != nil {
		log.Printf("Router error: %v", err)
	} else {
		fmt.Printf("Router response: %v\n", response1.Payload)
	}

	fmt.Println("✓ Message routing demonstrated")

	// 6. 演示会话管理
	fmt.Println("6. Demonstrating Session Management...")

	sessionMgr := multiagent.NewSessionManager()

	// 创建会话
	session, err := sessionMgr.CreateSession([]string{"agent-1", "agent-2", "agent-3"})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	fmt.Printf("Session created: %s\n", session.ID)
	fmt.Printf("Participants: %v\n", session.Participants)

	// 添加消息到会话
	sessionMsg1 := multiagent.NewAgentMessage("agent-1", "agent-2", multiagent.MessageTypeRequest, "start collaboration")
	sessionMgr.AddMessage(session.ID, sessionMsg1)

	sessionMsg2 := multiagent.NewAgentMessage("agent-2", "agent-3", multiagent.MessageTypeRequest, "join collaboration")
	sessionMgr.AddMessage(session.ID, sessionMsg2)

	// 获取会话
	retrievedSession, err := sessionMgr.GetSession(session.ID)
	if err != nil {
		log.Fatalf("Failed to get session: %v", err)
	}
	fmt.Printf("Session has %d messages\n", len(retrievedSession.Messages))

	// 关闭会话
	sessionMgr.CloseSession(session.ID)
	fmt.Println("Session closed")
	fmt.Println("✓ Session management demonstrated")

	// 7. 演示多Agent协作场景
	fmt.Println("7. Demonstrating Multi-Agent Collaboration Scenario...")
	fmt.Println("Scenario: Research task with 3 specialized agents")

	// Coordinator Agent
	coordinatorMsg := multiagent.NewAgentMessage("coordinator", "researcher", multiagent.MessageTypeCommand, map[string]string{
		"task": "research AI trends",
	})
	fmt.Printf("Coordinator → Researcher: %v\n", coordinatorMsg.Payload)

	// Researcher Agent
	researcherMsg := multiagent.NewAgentMessage("researcher", "analyzer", multiagent.MessageTypeRequest, map[string]string{
		"data": "collected research data",
	})
	fmt.Printf("Researcher → Analyzer: %v\n", researcherMsg.Payload)

	// Analyzer Agent
	analyzerMsg := multiagent.NewAgentMessage("analyzer", "coordinator", multiagent.MessageTypeResponse, map[string]string{
		"result": "analysis complete",
	})
	fmt.Printf("Analyzer → Coordinator: %v\n", analyzerMsg.Payload)

	fmt.Println("✓ Collaboration scenario demonstrated")

	fmt.Println("=== Example Completed Successfully ===")
	fmt.Println("\nNotes:")
	fmt.Println("- This example uses in-memory communication for demonstration")
	fmt.Println("- For distributed scenarios, use NATSCommunicator")
	fmt.Println("- Messages can carry trace context for distributed tracing")
	fmt.Println("- Router patterns support regex for flexible message handling")
	fmt.Println("- Sessions provide conversation history and state management")
}
