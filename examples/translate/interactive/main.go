package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/state"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

// 这个示例展示了如何使用 MultiAgentTranslator
// 不使用预定义的测试用例，而是接受命令行输入

func main() {
	fmt.Println("========================================")
	fmt.Println("=== 智能翻译系统交互示例 ===")
	fmt.Println("========================================")
	fmt.Println()

	// 初始化 DeepSeek 客户端
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		log.Fatal("错误: 请设置 DEEPSEEK_API_KEY 环境变量")
	}

	fmt.Println("使用 DeepSeek Chat 模型")
	fmt.Println()

	llmClient, err := providers.NewDeepSeekWithOptions(agentllm.WithAPIKey(apiKey), agentllm.WithModel("deepseek-chat"), agentllm.WithTemperature(0.3), agentllm.WithMaxTokens(2000))
	if err != nil {
		log.Fatalf("初始化 DeepSeek 客户端失败: %v", err)
	}

	// 创建翻译系统
	translator := NewSimpleTranslator(llmClient)

	// 从命令行参数或标准输入获取文本
	var text string
	if len(os.Args) > 1 {
		text = os.Args[1]
	} else {
		fmt.Print("请输入要翻译的文本: ")
		if _, err := fmt.Scanln(&text); err != nil {
			log.Fatalf("无法读取输入: %v", err)
		}
	}

	if text == "" {
		log.Fatal("错误: 未提供要翻译的文本")
	}

	fmt.Printf("\n输入文本: %s\n", text)
	fmt.Println("----------------------------------------")

	// 执行翻译
	ctx := context.Background()
	result, err := translator.Translate(ctx, text)
	if err != nil {
		log.Fatalf("翻译失败: %v", err)
	}

	fmt.Println()
	fmt.Printf("✓ 检测语言: %s\n", result.DetectedLanguage)
	fmt.Printf("✓ 翻译结果: %s\n", result.TranslatedText)
	fmt.Println()
	fmt.Printf("📊 Token 使用情况:\n")
	fmt.Printf("  - 语言检测: %d tokens\n", result.DetectionTokens)
	fmt.Printf("  - 翻译: %d tokens\n", result.TranslationTokens)
	fmt.Printf("  - 总计: %d tokens\n", result.TokensUsed)
	fmt.Printf("  - 预估成本: $%.6f\n", translator.tokenTracker.GetTotalCost())
	fmt.Println()
	fmt.Println("✨ 翻译完成!")
}

// SimpleTranslator 简化的翻译器
type SimpleTranslator struct {
	llmClient        llm.Client
	detectionAgent   *builder.ConfigurableAgent[any, core.State]
	translationAgent *builder.ConfigurableAgent[any, core.State]
	tokenTracker     *core.CostTrackingCallback // Token 追踪器
}

type TranslationResult struct {
	OriginalText      string
	DetectedLanguage  string
	TranslatedText    string
	TokensUsed        int // 使用的 token 总数
	DetectionTokens   int // 语言检测使用的 token
	TranslationTokens int // 翻译使用的 token
}

func NewSimpleTranslator(llmClient llm.Client) *SimpleTranslator {
	// 创建 token 追踪器（DeepSeek 定价）
	pricing := map[string]float64{
		"deepseek-chat": 0.21 / 1_000_000,
	}
	tokenTracker := core.NewCostTrackingCallback(pricing)

	translator := &SimpleTranslator{
		llmClient:    llmClient,
		tokenTracker: tokenTracker,
	}

	// 创建代理（带 token 追踪）
	translator.detectionAgent = createDetectionAgent(llmClient, tokenTracker)
	translator.translationAgent = createTranslationAgent(llmClient, tokenTracker)

	return translator
}

func createDetectionAgent(llmClient llm.Client, tokenTracker *core.CostTrackingCallback) *builder.ConfigurableAgent[any, core.State] {
	systemPrompt := `你是语言检测专家。识别文本语言并用中文返回语言名称（如：英语、法语、日语、西班牙语、德语、俄语、中文、韩语等）。只返回语言名称，不要其他内容。`

	agent, err := builder.NewAgentBuilder[any, core.State](llmClient).
		WithSystemPrompt(systemPrompt).
		WithState(state.NewAgentState()).
		WithCallbacks(tokenTracker). // 添加 token 追踪
		Build()

	if err != nil {
		log.Fatalf("创建语言检测代理失败: %v", err)
	}

	return agent
}

func createTranslationAgent(llmClient llm.Client, tokenTracker *core.CostTrackingCallback) *builder.ConfigurableAgent[any, core.State] {
	systemPrompt := `你是专业翻译专家。将输入文本翻译成简体中文。保持原文语气和风格，确保翻译准确、自然、流畅。如果输入已经是中文，保持不变。只返回翻译结果，不要其他内容。`

	agent, err := builder.NewAgentBuilder[any, core.State](llmClient).
		WithSystemPrompt(systemPrompt).
		WithState(state.NewAgentState()).
		WithCallbacks(tokenTracker). // 添加 token 追踪
		Build()

	if err != nil {
		log.Fatalf("创建翻译代理失败: %v", err)
	}

	return agent
}

func (t *SimpleTranslator) Translate(ctx context.Context, text string) (*TranslationResult, error) {
	result := &TranslationResult{
		OriginalText: text,
	}

	// 记录开始时的 token 数
	initialTokens := t.tokenTracker.GetTotalTokens()

	// 步骤 1: 语言检测
	fmt.Println("🔍 正在检测语言...")
	output, err := t.detectionAgent.Execute(ctx, text)
	if err != nil {
		return nil, err
	}

	if output != nil && output.Result != nil {
		if language, ok := output.Result.(string); ok {
			result.DetectedLanguage = language
		} else {
			result.DetectedLanguage = fmt.Sprintf("%v", output.Result)
		}
	}
	result.DetectionTokens = t.tokenTracker.GetTotalTokens() - initialTokens

	// 步骤 2: 翻译
	tokensBeforeTranslation := t.tokenTracker.GetTotalTokens()
	if result.DetectedLanguage != "中文" {
		fmt.Println("🌐 正在翻译...")
		prompt := fmt.Sprintf("请将以下%s文本翻译成中文：\n\n%s", result.DetectedLanguage, text)
		output, err := t.translationAgent.Execute(ctx, prompt)
		if err != nil {
			return nil, err
		}

		if output != nil && output.Result != nil {
			if translated, ok := output.Result.(string); ok {
				result.TranslatedText = translated
			} else {
				result.TranslatedText = fmt.Sprintf("%v", output.Result)
			}
		}
	} else {
		result.TranslatedText = text
	}
	result.TranslationTokens = t.tokenTracker.GetTotalTokens() - tokensBeforeTranslation
	result.TokensUsed = t.tokenTracker.GetTotalTokens() - initialTokens

	return result, nil
}
