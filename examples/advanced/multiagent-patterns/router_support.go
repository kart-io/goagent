package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	loggercore "github.com/kart-io/logger/core"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/multiagent"
)

// TicketCategory 工单类别
type TicketCategory string

const (
	CategoryBilling   TicketCategory = "billing"   // 账单问题
	CategoryTechnical TicketCategory = "technical" // 技术支持
	CategoryGeneral   TicketCategory = "general"   // 一般咨询
	CategoryEscalate  TicketCategory = "escalate"  // 需要人工审查
)

// SupportTicket 客服工单
type SupportTicket struct {
	ID          string         `json:"id"`
	Customer    string         `json:"customer"`
	Subject     string         `json:"subject"`
	Description string         `json:"description"`
	Category    TicketCategory `json:"category"`
	Priority    string         `json:"priority"`
	CreatedAt   time.Time      `json:"created_at"`
}

// TicketResponse 工单响应
type TicketResponse struct {
	TicketID       string         `json:"ticket_id"`
	Category       TicketCategory `json:"category"`
	Handler        string         `json:"handler"`
	Response       string         `json:"response"`
	ProcessedAt    time.Time      `json:"processed_at"`
	ProcessingTime float64        `json:"processing_time"`
}

// RunRouterPattern 运行路由模式示例
func RunRouterPattern(llmClient llm.Client, logger loggercore.Logger) error {
	fmt.Println("\n🔀 模式 2: Router（路由模式）- 智能客服工单分配")
	fmt.Println("说明：中央路由器根据工单类型自动分配给专业Agent处理")
	fmt.Println(strings.Repeat("=", 79))

	// 创建多智能体系统
	system := multiagent.NewMultiAgentSystem(logger)

	// 创建专业客服 Agent
	billingAgent := createSupportAgent(llmClient, "billing", "账单专家")
	technicalAgent := createSupportAgent(llmClient, "technical", "技术支持专家")
	generalAgent := createSupportAgent(llmClient, "general", "一般客服专家")
	escalationAgent := createSupportAgent(llmClient, "escalation", "高级客服主管")

	// 注册 Agent
	agents := map[string]multiagent.CollaborativeAgent{
		"billing":    billingAgent,
		"technical":  technicalAgent,
		"general":    generalAgent,
		"escalation": escalationAgent,
	}

	for id, agent := range agents {
		if err := system.RegisterAgent(id, agent); err != nil {
			return fmt.Errorf("注册 Agent %s 失败: %w", id, err)
		}
	}

	// 创建测试工单
	tickets := []SupportTicket{
		{
			ID:          "TICKET-001",
			Customer:    "张三",
			Subject:     "账单金额异常",
			Description: "我这个月的账单比往常高出很多，能帮我检查一下吗？",
			CreatedAt:   time.Now(),
		},
		{
			ID:          "TICKET-002",
			Customer:    "李四",
			Subject:     "无法登录账户",
			Description: "我输入正确的密码但一直提示错误，已经尝试重置密码也不行",
			CreatedAt:   time.Now(),
		},
		{
			ID:          "TICKET-003",
			Customer:    "王五",
			Subject:     "产品使用咨询",
			Description: "我想了解一下你们的高级套餐有哪些功能？",
			CreatedAt:   time.Now(),
		},
		{
			ID:          "TICKET-004",
			Customer:    "赵六",
			Subject:     "数据丢失严重问题",
			Description: "我的所有数据突然消失了，这是严重的问题！需要立即处理！",
			CreatedAt:   time.Now(),
		},
	}

	fmt.Printf("\n📋 接收到 %d 个工单，开始智能分配...\n\n", len(tickets))

	var responses []TicketResponse

	// 处理每个工单
	for _, ticket := range tickets {
		fmt.Printf("处理工单 %s: %s\n", ticket.ID, ticket.Subject)

		startTime := time.Now()

		// 第一步：路由工单（分类）
		category := routeTicket(&ticket)
		ticket.Category = category

		fmt.Printf("  ├─ 分类: %s\n", category)

		// 第二步：分配给对应的 Agent
		var handlerID string
		switch category {
		case CategoryBilling:
			handlerID = "billing"
		case CategoryTechnical:
			handlerID = "technical"
		case CategoryGeneral:
			handlerID = "general"
		case CategoryEscalate:
			handlerID = "escalation"
		}

		fmt.Printf("  ├─ 分配给: %s\n", handlerID)

		// 第三步：Agent 处理工单
		handler := agents[handlerID]
		output, err := handler.Invoke(context.Background(), &core.AgentInput{
			Task: fmt.Sprintf("工单ID: %s\n客户: %s\n问题: %s\n描述: %s",
				ticket.ID, ticket.Customer, ticket.Subject, ticket.Description),
		})
		if err != nil {
			fmt.Printf("  └─ ❌ 处理失败: %v\n\n", err)
			continue
		}

		processingTime := time.Since(startTime)

		response := TicketResponse{
			TicketID:       ticket.ID,
			Category:       category,
			Handler:        handlerID,
			Response:       output.Result.(string),
			ProcessedAt:    time.Now(),
			ProcessingTime: processingTime.Seconds(),
		}

		responses = append(responses, response)

		fmt.Printf("  └─ ✅ 处理完成 (用时: %.2f秒)\n\n", processingTime.Seconds())
	}

	// 输出总结
	fmt.Println("📊 工单处理总结:")
	fmt.Println(strings.Repeat("-", 79))

	categoryCount := make(map[TicketCategory]int)
	for _, resp := range responses {
		categoryCount[resp.Category]++
	}

	fmt.Println("\n按类别统计:")
	for category, count := range categoryCount {
		fmt.Printf("  %s: %d 个工单\n", category, count)
	}

	fmt.Println("\n处理详情:")
	for i, resp := range responses {
		fmt.Printf("\n%d. 工单 %s:\n", i+1, resp.TicketID)
		fmt.Printf("   类别: %s\n", resp.Category)
		fmt.Printf("   处理者: %s\n", resp.Handler)
		fmt.Printf("   响应: %s\n", truncateString(resp.Response, 200))
		fmt.Printf("   处理时间: %.2f秒\n", resp.ProcessingTime)
	}

	fmt.Println("\n" + strings.Repeat("-", 79))

	fmt.Printf("\n💡 路由优势: 每个工单都被自动分配给最合适的专家处理，提高解决效率\n")

	return nil
}

// routeTicket 路由工单（智能分类）
func routeTicket(ticket *SupportTicket) TicketCategory {
	subject := strings.ToLower(ticket.Subject)
	description := strings.ToLower(ticket.Description)
	content := subject + " " + description

	// 基于关键词的简单路由逻辑（实际应用中应该使用 LLM 进行更智能的分类）
	if strings.Contains(content, "账单") || strings.Contains(content, "费用") ||
		strings.Contains(content, "金额") || strings.Contains(content, "支付") {
		return CategoryBilling
	}

	if strings.Contains(content, "登录") || strings.Contains(content, "密码") ||
		strings.Contains(content, "无法") || strings.Contains(content, "错误") ||
		strings.Contains(content, "bug") || strings.Contains(content, "故障") {
		return CategoryTechnical
	}

	if strings.Contains(content, "严重") || strings.Contains(content, "紧急") ||
		strings.Contains(content, "数据丢失") || strings.Contains(content, "立即") {
		return CategoryEscalate
	}

	return CategoryGeneral
}

// createSupportAgent 创建客服 Agent
func createSupportAgent(llmClient llm.Client, agentID, expertise string) multiagent.CollaborativeAgent {
	agent := NewCollaborativeMockAgent(agentID, expertise, multiagent.RoleSpecialist)

	agent.SetInvokeFn(func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
		// 模拟 Agent 处理（实际应该调用 LLM）
		var response string

		switch agentID {
		case "billing":
			response = fmt.Sprintf("【%s】您好！我已经检查了您的账单。"+
				"上月使用量增加是由于启用了新功能。我会为您提供详细的账单明细，并安排退款审核。", expertise)
		case "technical":
			response = fmt.Sprintf("【%s】您好！登录问题通常是浏览器缓存导致的。"+
				"请尝试：1) 清除浏览器缓存 2) 使用无痕模式 3) 尝试其他浏览器。"+
				"如问题仍存在，我会升级到技术团队深入调查。", expertise)
		case "general":
			response = fmt.Sprintf("【%s】您好！高级套餐包含：无限存储空间、优先技术支持、"+
				"高级分析功能、团队协作工具等。我会发送详细的功能对比表到您的邮箱。", expertise)
		case "escalation":
			response = fmt.Sprintf("【%s】您好！我理解这个问题的严重性。"+
				"我已经将您的情况标记为最高优先级，技术总监和数据恢复团队将在30分钟内联系您。"+
				"同时我们会立即启动数据恢复流程。", expertise)
		default:
			response = fmt.Sprintf("【%s】感谢您的咨询，我们会尽快处理。", expertise)
		}

		// 模拟处理延迟
		time.Sleep(200 * time.Millisecond)

		return &core.AgentOutput{
			Result: response,
			Status: "success",
		}, nil
	})

	return agent
}

// truncateString 截断字符串
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
