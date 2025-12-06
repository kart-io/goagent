// Package main 演示使用 LLM 的多智能体协作系统
// 本示例展示如何创建具有 LLM 推理能力的协作 Agent
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/multiagent"
	loggercore "github.com/kart-io/logger/core"
)

// simpleLogger 简单的日志实现
type simpleLogger struct{}

func (l *simpleLogger) Debug(args ...interface{}) { fmt.Print("[DEBUG] "); fmt.Println(args...) }
func (l *simpleLogger) Info(args ...interface{})  { fmt.Print("[INFO] "); fmt.Println(args...) }
func (l *simpleLogger) Warn(args ...interface{})  { fmt.Print("[WARN] "); fmt.Println(args...) }
func (l *simpleLogger) Error(args ...interface{}) { fmt.Print("[ERROR] "); fmt.Println(args...) }
func (l *simpleLogger) Fatal(args ...interface{}) {
	fmt.Print("[FATAL] ")
	fmt.Println(args...)
	os.Exit(1)
}
func (l *simpleLogger) Debugf(template string, args ...interface{}) {
	fmt.Printf("[DEBUG] "+template+"\n", args...)
}
func (l *simpleLogger) Infof(template string, args ...interface{}) {
	fmt.Printf("[INFO] "+template+"\n", args...)
}
func (l *simpleLogger) Warnf(template string, args ...interface{}) {
	fmt.Printf("[WARN] "+template+"\n", args...)
}
func (l *simpleLogger) Errorf(template string, args ...interface{}) {
	fmt.Printf("[ERROR] "+template+"\n", args...)
}
func (l *simpleLogger) Fatalf(template string, args ...interface{}) {
	fmt.Printf("[FATAL] "+template+"\n", args...)
	os.Exit(1)
}
func (l *simpleLogger) Debugw(msg string, keysAndValues ...interface{}) {
	fmt.Printf("[DEBUG] %s %v\n", msg, keysAndValues)
}
func (l *simpleLogger) Infow(msg string, keysAndValues ...interface{}) {
	fmt.Printf("[INFO] %s %v\n", msg, keysAndValues)
}
func (l *simpleLogger) Warnw(msg string, keysAndValues ...interface{}) {
	fmt.Printf("[WARN] %s %v\n", msg, keysAndValues)
}
func (l *simpleLogger) Errorw(msg string, keysAndValues ...interface{}) {
	fmt.Printf("[ERROR] %s %v\n", msg, keysAndValues)
}
func (l *simpleLogger) Fatalw(msg string, keysAndValues ...interface{}) {
	fmt.Printf("[FATAL] %s %v\n", msg, keysAndValues)
	os.Exit(1)
}
func (l *simpleLogger) With(keyValues ...interface{}) loggercore.Logger { return l }
func (l *simpleLogger) WithCtx(ctx context.Context, keyValues ...interface{}) loggercore.Logger {
	return l
}
func (l *simpleLogger) WithCallerSkip(skip int) loggercore.Logger { return l }
func (l *simpleLogger) SetLevel(level loggercore.Level)           {}
func (l *simpleLogger) Flush() error                              { return nil }

// LLMCollaborativeAgent 具有 LLM 推理能力的协作 Agent
type LLMCollaborativeAgent struct {
	*multiagent.BaseCollaborativeAgent
	llmClient    llm.Client
	systemPrompt string
	expertise    string
}

// NewLLMCollaborativeAgent 创建一个使用 LLM 的协作 Agent
func NewLLMCollaborativeAgent(
	id, description string,
	role multiagent.Role,
	system *multiagent.MultiAgentSystem,
	llmClient llm.Client,
	systemPrompt, expertise string,
) *LLMCollaborativeAgent {
	return &LLMCollaborativeAgent{
		BaseCollaborativeAgent: multiagent.NewBaseCollaborativeAgent(id, description, role, system),
		llmClient:              llmClient,
		systemPrompt:           systemPrompt,
		expertise:              expertise,
	}
}

// Collaborate 使用 LLM 执行协作任务
func (a *LLMCollaborativeAgent) Collaborate(ctx context.Context, task *multiagent.CollaborativeTask) (*multiagent.Assignment, error) {
	assignment := &multiagent.Assignment{
		AgentID:   a.Name(),
		Role:      a.GetRole(),
		Subtask:   task.Input,
		Status:    multiagent.TaskStatusExecuting,
		StartTime: time.Now(),
	}

	// 构建 LLM 请求
	taskDescription := fmt.Sprintf("%v", task.Input)
	prompt := fmt.Sprintf(`任务: %s

请根据你的专业领域(%s)分析并完成此任务。
提供详细的分析结果和建议。`, taskDescription, a.expertise)

	// 调用 LLM
	response, err := a.llmClient.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: a.systemPrompt},
			{Role: "user", Content: prompt},
		},
	})

	if err != nil {
		assignment.Status = multiagent.TaskStatusFailed
		return assignment, fmt.Errorf("LLM 调用失败: %w", err)
	}

	// 构建结果
	result := map[string]interface{}{
		"agent_id":    a.Name(),
		"expertise":   a.expertise,
		"analysis":    response.Content,
		"model":       response.Model,
		"tokens_used": response.TokensUsed,
	}

	assignment.Result = result
	assignment.Status = multiagent.TaskStatusCompleted
	assignment.EndTime = time.Now()

	return assignment, nil
}

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          LLM 多智能体协作示例                                  ║")
	fmt.Println("║   展示如何创建具有 LLM 推理能力的协作 Agent                    ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// 创建 LLM 客户端
	llmClient, err := createLLMClient()
	if err != nil {
		fmt.Printf("警告: 无法创建真实 LLM 客户端: %v\n", err)
		fmt.Println("将使用 Mock LLM 客户端进行演示")
		llmClient = &MockLLMClient{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	logger := &simpleLogger{}
	system := multiagent.NewMultiAgentSystem(logger, multiagent.WithMaxAgents(10))

	// 演示 1: 代码审查场景
	fmt.Println("【场景 1】多专家代码审查")
	fmt.Println("════════════════════════════════════════════════════════════════")
	runCodeReviewScenario(ctx, system, llmClient)
	fmt.Println()

	// 演示 2: 研究分析场景
	fmt.Println("【场景 2】多专家研究分析")
	fmt.Println("════════════════════════════════════════════════════════════════")
	runResearchScenario(ctx, system, llmClient)
	fmt.Println()

	// 演示 3: 顺序处理场景
	fmt.Println("【场景 3】顺序处理流水线")
	fmt.Println("════════════════════════════════════════════════════════════════")
	runPipelineScenario(ctx, system, llmClient)

	printSummary()
}

// createLLMClient 创建 LLM 客户端
func createLLMClient() (llm.Client, error) {
	// 尝试使用 DeepSeek
	if apiKey := os.Getenv("DEEPSEEK_API_KEY"); apiKey != "" {
		return providers.NewDeepSeekWithOptions(
			llm.WithAPIKey(apiKey),
			llm.WithModel("deepseek-chat"),
			llm.WithMaxTokens(1000),
			llm.WithTemperature(0.7),
		)
	}

	// 尝试使用 Kimi (Moonshot)
	if apiKey := os.Getenv("KIMI_API_KEY"); apiKey != "" {
		return providers.NewKimiWithOptions(
			llm.WithAPIKey(apiKey),
			llm.WithModel("moonshot-v1-8k"),
			llm.WithMaxTokens(1000),
			llm.WithTemperature(0.7),
		)
	}

	// 尝试使用 OpenAI
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		return providers.NewOpenAIWithOptions(
			llm.WithAPIKey(apiKey),
			llm.WithModel("gpt-3.5-turbo"),
			llm.WithMaxTokens(1000),
			llm.WithTemperature(0.7),
		)
	}

	// 尝试使用 Ollama（本地）
	ollamaClient, err := providers.NewOllamaClientSimple("qwen2:7b")
	if err == nil && ollamaClient.IsAvailable() {
		return ollamaClient, nil
	}

	return nil, fmt.Errorf("未找到可用的 LLM 提供商，请设置 DEEPSEEK_API_KEY、KIMI_API_KEY 或 OPENAI_API_KEY")
}

// runCodeReviewScenario 代码审查场景
func runCodeReviewScenario(ctx context.Context, system *multiagent.MultiAgentSystem, llmClient llm.Client) {
	fmt.Println()
	fmt.Println("场景描述: 多位专家从不同角度审查代码")
	fmt.Println()

	// 创建专家 Agent
	securityAgent := NewLLMCollaborativeAgent(
		"security-reviewer",
		"安全审查专家",
		multiagent.RoleSpecialist,
		system,
		llmClient,
		"你是一位资深的代码安全专家，专注于识别安全漏洞和潜在风险。",
		"安全分析",
	)

	performanceAgent := NewLLMCollaborativeAgent(
		"performance-reviewer",
		"性能优化专家",
		multiagent.RoleSpecialist,
		system,
		llmClient,
		"你是一位性能优化专家，专注于识别性能瓶颈和优化机会。",
		"性能分析",
	)

	qualityAgent := NewLLMCollaborativeAgent(
		"quality-reviewer",
		"代码质量专家",
		multiagent.RoleSpecialist,
		system,
		llmClient,
		"你是一位代码质量专家，专注于代码可读性、可维护性和最佳实践。",
		"质量分析",
	)

	// 注册 Agent
	agents := []struct {
		id    string
		agent multiagent.CollaborativeAgent
	}{
		{"security-reviewer", securityAgent},
		{"performance-reviewer", performanceAgent},
		{"quality-reviewer", qualityAgent},
	}

	for _, a := range agents {
		if err := system.RegisterAgent(a.id, a.agent); err != nil {
			continue // 可能已经注册
		}
		fmt.Printf("✓ 注册专家: %s\n", a.id)
	}

	// 待审查的代码
	codeToReview := `
func ProcessUserData(db *sql.DB, userID string) ([]byte, error) {
    query := "SELECT * FROM users WHERE id = '" + userID + "'"
    rows, err := db.Query(query)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var results []map[string]interface{}
    for rows.Next() {
        // 处理每一行
        results = append(results, make(map[string]interface{}))
    }

    data, _ := json.Marshal(results)
    return data, nil
}
`

	// 创建并行审查任务
	task := &multiagent.CollaborativeTask{
		ID:          "code-review-001",
		Name:        "多专家代码审查",
		Description: "从安全、性能、质量三个维度审查代码",
		Type:        multiagent.CollaborationTypeParallel,
		Input: map[string]interface{}{
			"code":     codeToReview,
			"language": "Go",
			"context":  "用户数据处理函数",
		},
		Assignments: make(map[string]multiagent.Assignment),
	}

	fmt.Println()
	fmt.Println("待审查代码:")
	fmt.Println("────────────────────────────────────────")
	fmt.Println(codeToReview)
	fmt.Println("────────────────────────────────────────")
	fmt.Println()

	fmt.Println("执行多专家并行审查...")
	startTime := time.Now()
	result, err := system.ExecuteTask(ctx, task)
	duration := time.Since(startTime)

	if err != nil {
		fmt.Printf("✗ 审查失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 审查完成 (耗时: %v)\n", duration)
	fmt.Println()

	// 显示各专家的审查结果
	fmt.Println("审查结果:")
	fmt.Println("════════════════════════════════════════")
	for agentID, assignment := range result.Assignments {
		fmt.Printf("\n【%s】\n", agentID)
		if resultMap, ok := assignment.Result.(map[string]interface{}); ok {
			if analysis, exists := resultMap["analysis"]; exists {
				fmt.Printf("%v\n", truncate(fmt.Sprintf("%v", analysis), 500))
			}
		}
	}
}

// runResearchScenario 研究分析场景
func runResearchScenario(ctx context.Context, system *multiagent.MultiAgentSystem, llmClient llm.Client) {
	fmt.Println()
	fmt.Println("场景描述: 多位研究员协作分析一个技术主题")
	fmt.Println()

	// 创建研究员 Agent
	techResearcher := NewLLMCollaborativeAgent(
		"tech-researcher",
		"技术研究员",
		multiagent.RoleSpecialist,
		system,
		llmClient,
		"你是一位技术研究员，专注于分析技术实现和架构设计。",
		"技术分析",
	)

	marketResearcher := NewLLMCollaborativeAgent(
		"market-researcher",
		"市场研究员",
		multiagent.RoleSpecialist,
		system,
		llmClient,
		"你是一位市场研究员，专注于分析市场趋势和商业价值。",
		"市场分析",
	)

	summaryAgent := NewLLMCollaborativeAgent(
		"summary-writer",
		"报告撰写专家",
		multiagent.RoleLeader,
		system,
		llmClient,
		"你是一位报告撰写专家，负责综合多方观点并生成最终报告。",
		"报告撰写",
	)

	// 注册 Agent
	for _, a := range []struct {
		id    string
		agent multiagent.CollaborativeAgent
	}{
		{"tech-researcher", techResearcher},
		{"market-researcher", marketResearcher},
		{"summary-writer", summaryAgent},
	} {
		if err := system.RegisterAgent(a.id, a.agent); err != nil {
			continue
		}
		fmt.Printf("✓ 注册研究员: %s\n", a.id)
	}

	// 研究主题
	researchTopic := "大语言模型(LLM)在企业应用中的最佳实践"

	task := &multiagent.CollaborativeTask{
		ID:          "research-001",
		Name:        "LLM 企业应用研究",
		Description: researchTopic,
		Type:        multiagent.CollaborationTypeParallel,
		Input: map[string]interface{}{
			"topic":    researchTopic,
			"scope":    "技术实现、成本效益、风险评估",
			"audience": "技术决策者",
		},
		Assignments: make(map[string]multiagent.Assignment),
	}

	fmt.Println()
	fmt.Printf("研究主题: %s\n", researchTopic)
	fmt.Println()

	fmt.Println("执行多研究员协作分析...")
	startTime := time.Now()
	result, err := system.ExecuteTask(ctx, task)
	duration := time.Since(startTime)

	if err != nil {
		fmt.Printf("✗ 研究失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 研究完成 (耗时: %v)\n", duration)
	fmt.Println()

	// 显示研究结果
	fmt.Println("研究结果摘要:")
	fmt.Println("════════════════════════════════════════")
	for agentID, assignment := range result.Assignments {
		fmt.Printf("\n【%s】\n", agentID)
		if resultMap, ok := assignment.Result.(map[string]interface{}); ok {
			if analysis, exists := resultMap["analysis"]; exists {
				fmt.Printf("%v\n", truncate(fmt.Sprintf("%v", analysis), 400))
			}
		}
	}
}

// runPipelineScenario 流水线处理场景
func runPipelineScenario(ctx context.Context, system *multiagent.MultiAgentSystem, llmClient llm.Client) {
	fmt.Println()
	fmt.Println("场景描述: Agent 按顺序处理，前一个的输出作为后一个的输入")
	fmt.Println()

	// 创建流水线 Agent
	outlinerAgent := NewLLMCollaborativeAgent(
		"outliner",
		"大纲生成器",
		multiagent.RoleWorker,
		system,
		llmClient,
		"你是一位文章大纲专家，负责根据主题生成清晰的文章大纲。",
		"大纲生成",
	)

	writerAgent := NewLLMCollaborativeAgent(
		"writer",
		"内容撰写者",
		multiagent.RoleWorker,
		system,
		llmClient,
		"你是一位技术文章撰写专家，负责根据大纲撰写详细内容。",
		"内容撰写",
	)

	editorAgent := NewLLMCollaborativeAgent(
		"editor",
		"编辑审校者",
		multiagent.RoleValidator,
		system,
		llmClient,
		"你是一位资深编辑，负责审校和优化文章内容。",
		"编辑审校",
	)

	// 注册 Agent
	for _, a := range []struct {
		id    string
		agent multiagent.CollaborativeAgent
	}{
		{"outliner", outlinerAgent},
		{"writer", writerAgent},
		{"editor", editorAgent},
	} {
		if err := system.RegisterAgent(a.id, a.agent); err != nil {
			continue
		}
		fmt.Printf("✓ 注册流水线节点: %s\n", a.id)
	}

	// 写作任务
	writingTask := "写一篇关于 Go 语言并发编程最佳实践的技术文章"

	task := &multiagent.CollaborativeTask{
		ID:          "pipeline-001",
		Name:        "文章写作流水线",
		Description: "大纲 -> 撰写 -> 编辑",
		Type:        multiagent.CollaborationTypeSequential,
		Input: map[string]interface{}{
			"topic":       writingTask,
			"target_word": 500,
			"style":       "技术博客",
		},
		Assignments: make(map[string]multiagent.Assignment),
	}

	fmt.Println()
	fmt.Printf("写作任务: %s\n", writingTask)
	fmt.Println()

	fmt.Println("执行写作流水线...")
	startTime := time.Now()
	result, err := system.ExecuteTask(ctx, task)
	duration := time.Since(startTime)

	if err != nil {
		fmt.Printf("✗ 写作失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 写作完成 (耗时: %v)\n", duration)
	fmt.Println()

	// 显示流水线各阶段结果
	fmt.Println("流水线执行结果:")
	fmt.Println("════════════════════════════════════════")
	stage := 1
	for agentID, assignment := range result.Assignments {
		fmt.Printf("\n【阶段 %d: %s】\n", stage, agentID)
		if resultMap, ok := assignment.Result.(map[string]interface{}); ok {
			if analysis, exists := resultMap["analysis"]; exists {
				fmt.Printf("%v\n", truncate(fmt.Sprintf("%v", analysis), 400))
			}
		}
		stage++
	}
}

// MockLLMClient Mock LLM 客户端用于演示
type MockLLMClient struct{}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	// 根据系统提示生成模拟响应
	var content string

	if len(req.Messages) > 0 {
		systemMsg := req.Messages[0].Content

		switch {
		case contains(systemMsg, "安全"):
			content = `## 安全分析报告

**严重问题:**
1. SQL 注入漏洞：直接拼接用户输入到 SQL 查询中
   - 风险等级: 高危
   - 建议: 使用参数化查询

2. 错误处理不完整：json.Marshal 的错误被忽略
   - 风险等级: 中等
   - 建议: 正确处理所有错误

**建议修复方案:**
使用 db.QueryContext(ctx, "SELECT * FROM users WHERE id = ?", userID)`

		case contains(systemMsg, "性能"):
			content = `## 性能分析报告

**性能问题:**
1. SELECT * 查询效率低
   - 建议: 只查询需要的字段

2. 未使用连接池配置
   - 建议: 配置合适的连接池参数

3. 未设置查询超时
   - 建议: 使用 context.WithTimeout

**优化建议:**
- 添加查询结果缓存
- 使用批量查询减少数据库往返`

		case contains(systemMsg, "质量"):
			content = `## 代码质量分析报告

**质量问题:**
1. 函数职责不单一
   - 建议: 拆分为查询和序列化两个函数

2. 缺少文档注释
   - 建议: 添加函数文档和参数说明

3. 错误信息不够详细
   - 建议: 包装错误并添加上下文

**重构建议:**
- 使用结构体代替 map[string]interface{}
- 添加单元测试`

		case contains(systemMsg, "技术研究"):
			content = `## 技术分析

LLM 企业应用技术要点:
1. 模型选型: 根据场景选择合适的模型规模
2. 部署方案: 云端 API vs 本地部署
3. 提示工程: 构建高效的提示模板
4. 向量数据库: RAG 架构实现知识增强
5. 安全合规: 数据隐私和内容安全`

		case contains(systemMsg, "市场研究"):
			content = `## 市场分析

LLM 企业应用市场趋势:
1. 市场规模: 预计 2025 年达到 500 亿美元
2. 主要场景: 客服、文档处理、代码辅助
3. 竞争格局: OpenAI、Anthropic、国内厂商
4. 成本效益: ROI 通常在 6-12 个月内实现
5. 风险因素: 模型幻觉、数据安全`

		case contains(systemMsg, "大纲"):
			content = `## 文章大纲: Go 并发编程最佳实践

1. 引言
   - 为什么并发很重要
   - Go 并发模型简介

2. Goroutine 最佳实践
   - 启动和管理
   - 避免 goroutine 泄漏

3. Channel 使用模式
   - 无缓冲 vs 有缓冲
   - select 多路复用

4. 同步原语
   - sync.Mutex 使用场景
   - sync.WaitGroup 模式

5. 总结与建议`

		case contains(systemMsg, "撰写"):
			content = `## Go 并发编程最佳实践

Go 语言的并发模型基于 CSP (Communicating Sequential Processes)，
通过 goroutine 和 channel 提供了优雅的并发编程方式。

### Goroutine 最佳实践

1. 始终确保 goroutine 有退出机制
2. 使用 context 传递取消信号
3. 避免在循环中无限启动 goroutine

### Channel 使用模式

无缓冲 channel 适合同步，有缓冲 channel 适合解耦...`

		case contains(systemMsg, "编辑"):
			content = `## 编辑审校报告

**已优化内容:**
1. 调整了段落结构，提高可读性
2. 补充了代码示例
3. 修正了技术术语的使用

**最终版本质量评分: 8.5/10**

建议发布，文章结构清晰，技术内容准确。`

		default:
			content = "这是一个 Mock 响应，用于演示多智能体协作。"
		}
	}

	return &llm.CompletionResponse{
		Content:    content,
		Model:      "mock-model",
		TokensUsed: 150,
		Provider:   "mock",
	}, nil
}

func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return m.Complete(ctx, &llm.CompletionRequest{Messages: messages})
}

func (m *MockLLMClient) Provider() constants.Provider {
	return constants.ProviderCustom
}

func (m *MockLLMClient) IsAvailable() bool {
	return true
}

// contains 检查字符串是否包含子串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// truncate 截断字符串
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// printSummary 打印总结
func printSummary() {
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("LLM 多智能体协作核心模式:")
	fmt.Println()
	fmt.Println("┌────────────────────┬──────────────────────────────────────────┐")
	fmt.Println("│ 模式               │ 说明                                     │")
	fmt.Println("├────────────────────┼──────────────────────────────────────────┤")
	fmt.Println("│ 并行专家审查       │ 多个专家同时分析，综合多维度观点         │")
	fmt.Println("│ 协作研究           │ 不同领域研究员协作完成复杂分析           │")
	fmt.Println("│ 流水线处理         │ 前一个 Agent 的输出作为后一个的输入      │")
	fmt.Println("└────────────────────┴──────────────────────────────────────────┘")
	fmt.Println()
	fmt.Println("使用真实 LLM:")
	fmt.Println("  export DEEPSEEK_API_KEY=\"your-api-key\"  # DeepSeek")
	fmt.Println("  export KIMI_API_KEY=\"your-api-key\"      # Kimi (Moonshot)")
	fmt.Println("  export OPENAI_API_KEY=\"your-api-key\"    # OpenAI")
	fmt.Println("  go run main.go")
	fmt.Println()
	fmt.Println("或使用本地 Ollama:")
	fmt.Println("  ollama run qwen2:7b")
	fmt.Println("  go run main.go")
}
