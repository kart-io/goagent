package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/multiagent"
	"github.com/nats-io/nats.go"
)

// AIAgent 集成 LLM 的智能代理
type AIAgent struct {
	ID           string
	Role         string
	LLMClient    llm.Client
	Comm         multiagent.Communicator
	Description  string
	SystemPrompt string
}

// NewAIAgent 创建 AI 代理
func NewAIAgent(id, role, systemPrompt, description string, llmClient llm.Client, store multiagent.ChannelStore) (*AIAgent, error) {
	// 创建带 NATS 的通信器
	comm := multiagent.NewMemoryCommunicatorWithStore(id, store)

	return &AIAgent{
		ID:           id,
		Role:         role,
		LLMClient:    llmClient,
		Comm:         comm,
		Description:  description,
		SystemPrompt: systemPrompt,
	}, nil
}

// ProcessMessage 处理接收到的消息并用 LLM 生成响应
func (a *AIAgent) ProcessMessage(ctx context.Context, msg *multiagent.AgentMessage) (string, error) {
	// 构建提示词 - 结合系统提示和用户消息
	userMessage := fmt.Sprintf(`You received a message from %s:
Message Type: %s
Content: %v

Please provide a concise, helpful response based on your role.`,
		msg.From, msg.Type, msg.Payload)

	// 调用 LLM
	messages := []llm.Message{
		llm.SystemMessage(a.SystemPrompt),
		llm.UserMessage(userMessage),
	}

	response, err := a.LLMClient.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("LLM chat failed: %w", err)
	}

	return response.Content, nil
}

// Listen 监听消息并处理
func (a *AIAgent) Listen(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	fmt.Printf("[%s] Starting to listen for messages...\n", a.ID)

	for {
		select {
		case <-ctx.Done():
			fmt.Printf("[%s] Stopping listener\n", a.ID)
			return
		default:
			// 接收消息（带超时）
			msgCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			msg, err := a.Comm.Receive(msgCtx)
			cancel()

			if err != nil {
				if err == context.DeadlineExceeded || err == context.Canceled {
					continue
				}
				log.Printf("[%s] Error receiving message: %v", a.ID, err)
				continue
			}

			fmt.Printf("\n[%s] 📨 Received message from %s\n", a.ID, msg.From)
			fmt.Printf("[%s] Message Type: %s\n", a.ID, msg.Type)
			fmt.Printf("[%s] Content: %v\n", a.ID, msg.Payload)

			// 使用 LLM 处理消息
			fmt.Printf("[%s] 🤖 Processing with AI...\n", a.ID)
			response, err := a.ProcessMessage(ctx, msg)
			if err != nil {
				log.Printf("[%s] Error processing message: %v", a.ID, err)
				continue
			}

			fmt.Printf("[%s] 💡 AI Response: %s\n", a.ID, response)

			// 回复发送者
			if msg.Type == multiagent.MessageTypeRequest || msg.Type == multiagent.MessageTypeCommand {
				replyMsg := multiagent.NewAgentMessage(
					a.ID,
					msg.From,
					multiagent.MessageTypeResponse,
					map[string]interface{}{
						"original_request": msg.Payload,
						"ai_response":      response,
						"processed_at":     time.Now().Format(time.RFC3339),
					},
				)

				if err := a.Comm.Send(ctx, msg.From, replyMsg); err != nil {
					log.Printf("[%s] Error sending reply: %v", a.ID, err)
				} else {
					fmt.Printf("[%s] ✅ Sent reply to %s\n\n", a.ID, msg.From)
				}
			}
		}
	}
}

// SendRequest 发送请求并等待响应
func (a *AIAgent) SendRequest(ctx context.Context, targetID, request string) (string, error) {
	msg := multiagent.NewAgentMessage(
		a.ID,
		targetID,
		multiagent.MessageTypeRequest,
		request,
	)

	fmt.Printf("[%s] 📤 Sending request to %s: %s\n", a.ID, targetID, request)

	if err := a.Comm.Send(ctx, targetID, msg); err != nil {
		return "", err
	}

	// 等待响应
	responseCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	response, err := a.Comm.Receive(responseCtx)
	if err != nil {
		return "", fmt.Errorf("timeout waiting for response: %w", err)
	}

	// 提取 AI 响应
	if payload, ok := response.Payload.(map[string]interface{}); ok {
		if aiResponse, ok := payload["ai_response"].(string); ok {
			return aiResponse, nil
		}
	}

	// 回退：返回原始 payload
	payloadBytes, _ := json.Marshal(response.Payload)
	return string(payloadBytes), nil
}

// Close 关闭代理
func (a *AIAgent) Close() error {
	return a.Comm.Close()
}

// RunAICollaboration 运行 AI 代理协作示例
func RunAICollaboration() {
	fmt.Println("=== AI-Powered Multi-Agent Collaboration via NATS ===")
	fmt.Println()

	// 获取 DeepSeek API 密钥
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		log.Fatal("❌ DEEPSEEK_API_KEY environment variable is required\n" +
			"Get your key from: https://platform.deepseek.com/")
	}

	// 获取 NATS URL
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = nats.DefaultURL
	}

	fmt.Printf("Connecting to NATS: %s\n", natsURL)

	// 创建 NATS ChannelStore
	natsStore, err := NewNATSChannelStore(natsURL)
	if err != nil {
		log.Fatalf("❌ Failed to connect to NATS: %v\n"+
			"Please start NATS: ./test.sh start", err)
	}
	defer func() {
		if err := natsStore.Close(); err != nil {
			log.Printf("Warning: failed to close NATS store: %v", err)
		}
	}()

	fmt.Println("✅ Connected to NATS")
	fmt.Println()

	// 创建 DeepSeek LLM 客户端
	llmConfig := &llm.LLMOptions{
		APIKey:      apiKey,
		Model:       "deepseek-chat",
		Temperature: 0.7,
		MaxTokens:   2000,
		Timeout:     30,
	}

	llmClient, err := providers.NewDeepSeek(llmConfig)
	if err != nil {
		log.Fatalf("❌ Failed to create DeepSeek client: %v", err)
	}

	// 创建三个专业化的 AI 代理
	fmt.Println("Creating AI Agents...")

	// 1. 协调代理：负责任务分配和结果汇总
	coordinator, err := NewAIAgent(
		"coordinator",
		"Project Coordinator",
		`You are a Project Coordinator AI Agent. Your role is to:
- Break down complex tasks into smaller subtasks
- Assign tasks to appropriate specialist agents
- Collect and synthesize results from multiple agents
- Provide final comprehensive reports
Be concise and professional.`,
		"Coordinates multi-agent collaboration and task distribution",
		llmClient,
		natsStore,
	)
	if err != nil {
		log.Fatalf("Failed to create coordinator: %v", err)
	}

	// 2. 研究代理：负责信息收集和分析
	researcher, err := NewAIAgent(
		"researcher",
		"Research Specialist",
		`You are a Research Specialist AI Agent. Your role is to:
- Gather and analyze information
- Provide detailed research findings
- Answer questions with evidence-based responses
- Present findings in 3 key points
Be thorough but concise.`,
		"Specializes in information gathering and analysis",
		llmClient,
		natsStore,
	)
	if err != nil {
		log.Fatalf("Failed to create researcher: %v", err)
	}

	// 3. 技术专家代理：负责技术问题解答
	techExpert, err := NewAIAgent(
		"tech-expert",
		"Technical Expert",
		`You are a Technical Expert AI Agent. Your role is to:
- Provide technical solutions and recommendations
- Suggest specific technologies and tools
- Give practical, actionable advice
- Present recommendations in a numbered list
Be practical and solution-oriented.`,
		"Provides technical expertise and solutions",
		llmClient,
		natsStore,
	)
	if err != nil {
		log.Fatalf("Failed to create tech expert: %v", err)
	}

	fmt.Printf("✅ Created %s\n", coordinator.ID)
	fmt.Printf("✅ Created %s\n", researcher.ID)
	fmt.Printf("✅ Created %s\n", techExpert.ID)
	fmt.Println()

	// 启动代理监听器
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	var wg sync.WaitGroup

	// 启动研究代理和技术专家的监听器
	wg.Add(2)
	go researcher.Listen(ctx, &wg)
	go techExpert.Listen(ctx, &wg)

	// 给监听器时间启动
	time.Sleep(500 * time.Millisecond)

	// 场景：协调代理向其他代理请求信息，然后汇总结果
	fmt.Println("=== Collaboration Scenario: Building a Microservices Architecture ===")
	fmt.Println()

	// 1. 协调代理询问研究代理
	fmt.Println("Step 1: Coordinator asks Researcher about microservices best practices")
	fmt.Println(strings.Repeat("-", 80))

	researchResponse, err := coordinator.SendRequest(ctx, "researcher",
		"What are the key benefits and challenges of microservices architecture? Provide 3 main points for each.")
	if err != nil {
		log.Fatalf("Error communicating with researcher: %v", err)
	}

	fmt.Printf("\n📊 Research Findings:\n%s\n\n", researchResponse)

	// 2. 协调代理询问技术专家
	fmt.Println("Step 2: Coordinator asks Tech Expert about implementation")
	fmt.Println(strings.Repeat("-", 80))

	techResponse, err := coordinator.SendRequest(ctx, "tech-expert",
		"What technologies and tools would you recommend for building a microservices system? List 3 key technologies with brief explanations.")
	if err != nil {
		log.Fatalf("Error communicating with tech expert: %v", err)
	}

	fmt.Printf("\n🔧 Technical Recommendations:\n%s\n\n", techResponse)

	// 3. 协调代理汇总所有信息
	fmt.Println("Step 3: Coordinator synthesizes all information")
	fmt.Println(strings.Repeat("-", 80))

	// 使用 LLM 生成最终报告
	finalPrompt := fmt.Sprintf(`You are the Project Coordinator. You've collected information from team members:

RESEARCH FINDINGS:
%s

TECHNICAL RECOMMENDATIONS:
%s

Please create a concise executive summary (3-4 sentences) that combines these insights to recommend whether and how to proceed with microservices architecture.`,
		researchResponse, techResponse)

	finalMessages := []llm.Message{
		llm.UserMessage(finalPrompt),
	}

	finalOutput, err := llmClient.Chat(ctx, finalMessages)
	if err != nil {
		log.Fatalf("Error generating final report: %v", err)
	}

	fmt.Printf("\n📋 Executive Summary:\n%s\n\n", finalOutput.Content)

	// 关闭代理
	fmt.Println("Shutting down agents...")
	cancel() // 停止监听器

	wg.Wait() // 等待监听器退出

	if err := coordinator.Close(); err != nil {
		log.Printf("Warning: failed to close coordinator: %v", err)
	}
	if err := researcher.Close(); err != nil {
		log.Printf("Warning: failed to close researcher: %v", err)
	}
	if err := techExpert.Close(); err != nil {
		log.Printf("Warning: failed to close techExpert: %v", err)
	}

	fmt.Println()
	fmt.Println("=== AI Collaboration Completed Successfully ===")
	fmt.Println()
	fmt.Println("This example demonstrated:")
	fmt.Println("✅ Multiple AI agents with specialized roles")
	fmt.Println("✅ Distributed communication via NATS")
	fmt.Println("✅ Inter-agent collaboration using LLMs")
	fmt.Println("✅ Information synthesis from multiple sources")
	fmt.Println("✅ Real-world problem solving with AI agents")
}
