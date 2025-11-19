package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kart-io/goagent/examples/testhelpers"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

func main() {
	// 创建 LLM 客户端
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		fmt.Println("❌ 请设置 DEEPSEEK_API_KEY 环境变量")
		os.Exit(1)
	}

	llmClient, err := providers.NewDeepSeek(&llm.Config{
		APIKey: apiKey,
		Model:  "deepseek-chat",
	})
	if err != nil {
		panic(err)
	}

	// 待审查的代码
	codeToReview := `
func ProcessUserData(data string) error {
    // 直接使用用户输入构建 SQL
    query := "SELECT * FROM users WHERE name = '" + data + "'"

    // 执行查询
    for i := 0; i < 1000000; i++ {
        result := db.Query(query)
        // 处理结果...
    }

    return nil
}
`

	// 创建安全审查 Agent
	securityAgent := testhelpers.NewMockAgent("security")
	securityAgent.SetInvokeFn(func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
		prompt := fmt.Sprintf(`你是一个代码安全审查专家。

%s

请从**安全角度**审查上述代码，重点关注：
1. SQL 注入漏洞
2. XSS 攻击风险
3. 数据验证缺失
4. 敏感信息泄露

**请按以下格式输出：**
- 安全评分：X/10分
- 发现的安全问题（列出具体问题）
- 改进建议（给出具体的修复方案）`, input.Task)

		response, err := llmClient.Complete(ctx, &llm.CompletionRequest{
			Messages: []llm.Message{
				{Role: "user", Content: prompt},
			},
		})

		if err != nil {
			return nil, err
		}

		return &core.AgentOutput{
			Result:     response.Content,
			Status:     "success",
			TokenUsage: response.Usage,
		}, nil
	})

	// 构建任务
	task := fmt.Sprintf(`请仔细审查以下 Go 代码的安全性。

**待审查代码：**
%s

**要求：**
从安全角度进行专业分析，给出评分和改进建议。`, codeToReview)

	fmt.Println("=== 直接测试 SubAgent ===\n")
	fmt.Printf("📝 任务:\n%s\n\n", task)
	fmt.Println("🔍 正在执行安全审查...")

	// 直接调用 Agent
	result, err := securityAgent.Invoke(context.Background(), &core.AgentInput{
		Task: task,
	})

	if err != nil {
		fmt.Printf("❌ 执行失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n✅ 审查完成！\n")
	fmt.Println("📊 审查结果:")
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println(result.Result)
	fmt.Println("--------------------------------------------------------------------------------")

	if result.TokenUsage != nil {
		fmt.Printf("\n🎫 Token 使用: %d\n", result.TokenUsage.TotalTokens)
	}
}
