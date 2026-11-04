package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/state"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

// TranslationResult 翻译结果
type TranslationResult struct {
	OriginalText      string `json:"original_text"`
	DetectedLanguage  string `json:"detected_language"`
	TranslatedText    string `json:"translated_text"`
	TokensUsed        int    `json:"tokens_used"`        // 使用的 token 总数
	DetectionTokens   int    `json:"detection_tokens"`   // 语言检测使用的 token
	TranslationTokens int    `json:"translation_tokens"` // 翻译使用的 token
}

func main() {
	fmt.Println("========================================")
	fmt.Println("=== 智能翻译系统 (Multi-Agent) ===")
	fmt.Println("========================================")
	fmt.Println()

	// 初始化 DeepSeek LLM 客户端
	llmClient, err := initializeDeepSeekClient()
	if err != nil {
		log.Fatalf("初始化 DeepSeek 客户端失败: %v", err)
	}

	// 创建翻译系统
	translator := NewMultiAgentTranslator(llmClient)

	// 测试用例
	testCases := []string{
		"Hello, how are you today?",
		"Bonjour, comment allez-vous?",
		"こんにちは、お元気ですか？",
		"Hola, ¿cómo estás?",
		"Guten Tag, wie geht es Ihnen?",
		"Привет, как дела?",
		"你好，今天天气怎么样？",
		// 添加更长的测试文本以更好地展示 token 使用情况
		"Artificial Intelligence is transforming the way we live and work. From healthcare to transportation, AI systems are making significant impacts across various industries. Machine learning algorithms can now analyze vast amounts of data and make predictions with remarkable accuracy.",
		"La technologie de l'intelligence artificielle évolue rapidement et transforme notre société. Les applications vont de la médecine à l'éducation, en passant par les transports et la finance. Cette révolution technologique apporte à la fois des opportunités et des défis.",
	}

	ctx := context.Background()

	for i, text := range testCases {
		fmt.Printf("\n【测试 %d】\n", i+1)
		fmt.Printf("输入: %s\n", text)
		fmt.Println(strings.Repeat("-", 60))

		result, err := translator.Translate(ctx, text)
		if err != nil {
			log.Printf("翻译失败: %v\n", err)
			continue
		}

		fmt.Printf("检测语言: %s\n", result.DetectedLanguage)
		fmt.Printf("翻译结果: %s\n", result.TranslatedText)
		fmt.Printf("Token 使用情况:\n")
		fmt.Printf("  - 语言检测: %d tokens\n", result.DetectionTokens)
		fmt.Printf("  - 翻译: %d tokens\n", result.TranslationTokens)
		fmt.Printf("  - 总计: %d tokens\n", result.TokensUsed)
		fmt.Println(strings.Repeat("=", 60))
	}

	fmt.Println("\n✨ 翻译系统测试完成!")
	fmt.Printf("\n📊 总体统计:\n")
	fmt.Printf("  - 总 Token 使用量: %d tokens\n", translator.tokenTracker.GetTotalTokens())
	fmt.Printf("  - 总成本: $%.6f\n", translator.tokenTracker.GetTotalCost())
}

// initializeDeepSeekClient 初始化 DeepSeek LLM 客户端
func initializeDeepSeekClient() (llm.Client, error) {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("请设置 DEEPSEEK_API_KEY 环境变量")
	}

	fmt.Println("使用 DeepSeek Chat 模型")
	opts := []llm.ClientOption{
		llm.WithAPIKey(apiKey),
		llm.WithModel("deepseek-chat"),
		llm.WithTemperature(0.3),
		llm.WithMaxTokens(2000),
	}

	// 创建 DeepSeek 客户端
	return providers.NewDeepSeekWithOptions(opts...)
}

// MultiAgentTranslator 多代理翻译器
type MultiAgentTranslator struct {
	llmClient        llm.Client
	detectionAgent   *builder.ConfigurableAgent[any, core.State]
	translationAgent *builder.ConfigurableAgent[any, core.State]
	tokenTracker     *core.CostTrackingCallback // Token 追踪器
}

// NewMultiAgentTranslator 创建新的多代理翻译器
func NewMultiAgentTranslator(llmClient llm.Client) *MultiAgentTranslator {
	// 创建 token 追踪器（DeepSeek 定价：输入 $0.14/M tokens，输出 $0.28/M tokens）
	// 简化为平均 $0.21/M tokens
	pricing := map[string]float64{
		"deepseek-chat": 0.21 / 1_000_000, // 每个 token 的成本
	}
	tokenTracker := core.NewCostTrackingCallback(pricing)

	translator := &MultiAgentTranslator{
		llmClient:    llmClient,
		tokenTracker: tokenTracker,
	}

	// 创建语言检测代理（带 token 追踪）
	translator.detectionAgent = translator.createLanguageDetectionAgent()
	// 创建翻译代理（带 token 追踪）
	translator.translationAgent = translator.createTranslationAgent()

	return translator
}

// createLanguageDetectionAgent 创建语言检测代理
func (t *MultiAgentTranslator) createLanguageDetectionAgent() *builder.ConfigurableAgent[any, core.State] {
	systemPrompt := `你是一个专业的语言检测专家。你的任务是：

1. 分析输入的文本
2. 准确识别文本使用的语言
3. 用中文返回语言名称

语言识别规则：
- 英语 (English) -> 返回 "英语"
- 法语 (French) -> 返回 "法语"
- 日语 (Japanese) -> 返回 "日语"
- 西班牙语 (Spanish) -> 返回 "西班牙语"
- 德语 (German) -> 返回 "德语"
- 俄语 (Russian) -> 返回 "俄语"
- 中文 (Chinese) -> 返回 "中文"
- 韩语 (Korean) -> 返回 "韩语"
- 其他语言 -> 返回具体的语言名称（中文）

请只返回语言名称，不要包含其他解释。

示例：
输入: "Hello, world!"
输出: 英语

输入: "Bonjour!"
输出: 法语

输入: "こんにちは"
输出: 日语`

	agent, err := builder.NewAgentBuilder[any, core.State](t.llmClient).
		WithSystemPrompt(systemPrompt).
		WithState(state.NewAgentState()).
		WithCallbacks(t.tokenTracker). // 添加 token 追踪回调
		Build()
	if err != nil {
		log.Fatalf("创建语言检测代理失败: %v", err)
	}

	return agent
}

// createTranslationAgent 创建翻译代理
func (t *MultiAgentTranslator) createTranslationAgent() *builder.ConfigurableAgent[any, core.State] {
	systemPrompt := `你是一个专业的翻译专家。你的任务是：

1. 将输入的文本翻译成简体中文
2. 保持原文的语气和风格
3. 确保翻译准确、自然、流畅

翻译要求：
- 如果输入已经是中文，保持不变
- 保留专有名词和品牌名称
- 保持原文的格式（如果有标点符号、换行等）
- 使用地道的中文表达
- 不要添加任何额外的解释或说明

请只返回翻译结果，不要包含其他内容。

示例：
输入: "Hello, how are you?"
输出: 你好，你好吗？

输入: "Good morning!"
输出: 早上好！

输入: "Thank you very much!"
输出: 非常感谢！`

	agent, err := builder.NewAgentBuilder[any, core.State](t.llmClient).
		WithSystemPrompt(systemPrompt).
		WithState(state.NewAgentState()).
		WithCallbacks(t.tokenTracker). // 添加 token 追踪回调
		Build()
	if err != nil {
		log.Fatalf("创建翻译代理失败: %v", err)
	}

	return agent
}

// Translate 执行翻译流程
func (t *MultiAgentTranslator) Translate(ctx context.Context, text string) (*TranslationResult, error) {
	result := &TranslationResult{
		OriginalText: text,
	}

	// 记录开始时的 token 数
	initialTokens := t.tokenTracker.GetTotalTokens()

	// 步骤 1: 语言检测
	fmt.Println("🔍 语言检测代理: 正在识别语言...")
	detectedLanguage, err := t.detectLanguage(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("语言检测失败: %w", err)
	}
	result.DetectedLanguage = detectedLanguage
	result.DetectionTokens = t.tokenTracker.GetTotalTokens() - initialTokens
	fmt.Printf("✓ 检测完成: %s (使用 %d tokens)\n", detectedLanguage, result.DetectionTokens)

	// 步骤 2: 翻译
	fmt.Println("🌐 翻译代理: 正在翻译文本...")
	tokensBeforeTranslation := t.tokenTracker.GetTotalTokens()
	translated, err := t.translateText(ctx, text, detectedLanguage)
	if err != nil {
		return nil, fmt.Errorf("翻译失败: %w", err)
	}
	result.TranslatedText = translated
	result.TranslationTokens = t.tokenTracker.GetTotalTokens() - tokensBeforeTranslation
	result.TokensUsed = t.tokenTracker.GetTotalTokens() - initialTokens
	fmt.Printf("✓ 翻译完成 (使用 %d tokens)\n", result.TranslationTokens)

	return result, nil
}

// detectLanguage 使用语言检测代理识别语言
func (t *MultiAgentTranslator) detectLanguage(ctx context.Context, text string) (string, error) {
	// 执行语言检测
	output, err := t.detectionAgent.AsRunnable().Invoke(ctx, text)
	if err != nil {
		return "", err
	}

	// 提取结果
	if output != nil && output.Result != nil {
		// 尝试转换为字符串
		if language, ok := output.Result.(string); ok {
			return strings.TrimSpace(language), nil
		}

		// 如果不是字符串，转换为字符串
		return fmt.Sprintf("%v", output.Result), nil
	}

	return "未知语言", nil
}

// translateText 使用翻译代理翻译文本
func (t *MultiAgentTranslator) translateText(ctx context.Context, text string, language string) (string, error) {
	// 如果已经是中文，直接返回
	if language == "中文" {
		return text, nil
	}

	// 构建翻译提示
	prompt := fmt.Sprintf("请将以下%s文本翻译成中文：\n\n%s", language, text)

	// 执行翻译
	output, err := t.translationAgent.AsRunnable().Invoke(ctx, prompt)
	if err != nil {
		return "", err
	}

	// 提取结果
	if output != nil && output.Result != nil {
		// 尝试转换为字符串
		if translated, ok := output.Result.(string); ok {
			return strings.TrimSpace(translated), nil
		}

		// 如果不是字符串，转换为字符串
		return fmt.Sprintf("%v", output.Result), nil
	}

	return text, nil
}
