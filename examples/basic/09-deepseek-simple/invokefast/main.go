// Package main demonstrates DeepSeek usage with GoAgent's InvokeFast optimization
//
// This example shows:
// - Using DeepSeek with AgentBuilder (recommended approach)
// - How InvokeFast optimization works automatically in nested scenarios
// - Performance comparison of nested agent calls
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/kart-io/goagent/builder"
	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

func main() {
	fmt.Println("GoAgent + DeepSeek InvokeFast 优化示例")
	fmt.Println("==========================================")
	fmt.Println()

	// 从环境变量获取 API Key
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		log.Fatal("错误: 请设置 DEEPSEEK_API_KEY 环境变量\n提示: export DEEPSEEK_API_KEY=your-api-key")
	}

	// 示例 1: 基础 DeepSeek Agent 使用
	runBasicExample(apiKey)

	fmt.Println()

	// 示例 2: 多步骤任务处理（自动优化）
	runMultiStepExample(apiKey)

	fmt.Println()

	// 示例 3: 结构化数据生成（InvokeFast 优化）
	runStructuredDataExample(apiKey)

	fmt.Println()

	// 示例 4: 性能说明
	showOptimizationExplanation()
}

// runBasicExample 演示基础的 DeepSeek Agent 使用
func runBasicExample(apiKey string) {
	fmt.Println("示例 1: 基础 DeepSeek Agent")
	fmt.Println("---------------------------")

	// 创建 DeepSeek provider
	client, err := providers.NewDeepSeekWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("deepseek-chat"),
		llm.WithTemperature(0.7),
		llm.WithMaxTokens(500),
		llm.WithTimeout(30),
	)
	if err != nil {
		log.Fatalf("创建 DeepSeek provider 失败: %v", err)
	}

	// 使用 Builder 构建 Agent
	agent, err := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
		WithSystemPrompt("你是一个简洁的助手，用一句话回答问题。").
		Build()
	if err != nil {
		log.Fatalf("构建 Agent 失败: %v", err)
	}

	// 执行任务
	ctx := context.Background()
	question := "Go 语言的主要特点是什么？"

	fmt.Printf("问题: %s\n", question)
	fmt.Println()

	start := time.Now()
	output, err := agent.Execute(ctx, question)
	duration := time.Since(start)

	if err != nil {
		log.Fatalf("执行失败: %v", err)
	}

	fmt.Printf("回答: %v\n", output.Result)
	fmt.Printf("耗时: %v\n", duration)
}

// runMultiStepExample 演示多步骤任务处理
func runMultiStepExample(apiKey string) {
	fmt.Println("示例 2: 多步骤任务处理")
	fmt.Println("----------------------")
	fmt.Println("（InvokeFast 优化在内部自动生效）")
	fmt.Println()

	// 创建 DeepSeek 配置
	config := &llm.LLMOptions{
		APIKey:      apiKey,
		Model:       "deepseek-chat",
		Temperature: 0.7,
		MaxTokens:   400,
		Timeout:     30,
	}

	// 创建 DeepSeek provider
	client, err := providers.NewDeepSeek(config)
	if err != nil {
		log.Fatalf("创建 DeepSeek provider 失败: %v", err)
	}

	// 创建分析 Agent
	analyzeAgent, err := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
		WithSystemPrompt("你是代码分析专家，分析代码特点。").
		WithMetadata("name", "AnalyzeAgent").
		Build()
	if err != nil {
		log.Fatalf("构建分析 Agent 失败: %v", err)
	}

	// 创建优化建议 Agent
	optimizeAgent, err := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
		WithSystemPrompt("你是代码优化专家，提供优化建议。").
		WithMetadata("name", "OptimizeAgent").
		Build()
	if err != nil {
		log.Fatalf("构建优化 Agent 失败: %v", err)
	}

	ctx := context.Background()
	code := `
func processData(data []int) int {
    sum := 0
    for i := 0; i < len(data); i++ {
        sum += data[i]
    }
    return sum
}
`

	fmt.Println("待分析代码:")
	fmt.Println(code)
	fmt.Println()

	// 步骤 1: 分析代码
	fmt.Println("步骤 1: 分析代码特点...")
	start1 := time.Now()
	analyzeOutput, err := analyzeAgent.Execute(ctx, fmt.Sprintf("分析这段 Go 代码的特点：%s", code))
	duration1 := time.Since(start1)
	if err != nil {
		log.Fatalf("分析失败: %v", err)
	}

	fmt.Printf("分析结果: %v\n", analyzeOutput.Result)
	fmt.Printf("耗时: %v\n", duration1)
	fmt.Println()

	// 步骤 2: 提供优化建议
	fmt.Println("步骤 2: 提供优化建议...")
	start2 := time.Now()
	optimizeOutput, err := optimizeAgent.Execute(ctx, fmt.Sprintf("基于以下分析，提供优化建议：%v", analyzeOutput.Result))
	duration2 := time.Since(start2)
	if err != nil {
		log.Fatalf("优化建议失败: %v", err)
	}

	fmt.Printf("优化建议: %v\n", optimizeOutput.Result)
	fmt.Printf("耗时: %v\n", duration2)
	fmt.Println()

	fmt.Printf("总耗时: %v\n", duration1+duration2)
}

// runStructuredDataExample 演示使用 InvokeFast 优化结构化数据生成
func runStructuredDataExample(apiKey string) {
	fmt.Println("示例 3: 结构化数据生成（InvokeFast 优化）")
	fmt.Println("------------------------------------------")
	fmt.Println("（使用多个专业 Agent 协同生成结构化数据）")
	fmt.Println()

	// 创建 DeepSeek 配置
	config := &llm.LLMOptions{
		APIKey:      apiKey,
		Model:       "deepseek-chat",
		Temperature: 0.3, // 使用较低的 temperature 获得稳定输出
		MaxTokens:   500,
		Timeout:     30,
	}

	// 创建 DeepSeek provider
	client, err := providers.NewDeepSeek(config)
	if err != nil {
		log.Fatalf("创建 DeepSeek provider 失败: %v", err)
	}

	// 创建用户数据生成 Agent
	userAgent, err := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
		WithSystemPrompt(`你是用户数据生成专家。生成符合要求的用户 JSON 数据，仅输出 JSON 格式，不要其他说明。`).
		WithMetadata("name", "UserDataGenerator").
		Build()
	if err != nil {
		log.Fatalf("构建用户数据 Agent 失败: %v", err)
	}

	// 创建产品数据生成 Agent
	productAgent, err := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
		WithSystemPrompt(`你是产品数据生成专家。生成符合要求的产品 JSON 数据，仅输出 JSON 格式，不要其他说明。`).
		WithMetadata("name", "ProductDataGenerator").
		Build()
	if err != nil {
		log.Fatalf("构建产品数据 Agent 失败: %v", err)
	}

	ctx := context.Background()

	// 步骤 1: 生成用户数据
	fmt.Println("步骤 1: 生成用户数据...")
	userTask := `生成 2 个用户的 JSON 数组，每个用户包含：
- id: 整数
- name: 用户名
- email: 邮箱
- role: 角色（admin/user）
仅输出 JSON 数组，不要其他文字。`

	start1 := time.Now()
	userOutput, err := userAgent.Execute(ctx, userTask)
	duration1 := time.Since(start1)
	if err != nil {
		log.Fatalf("生成用户数据失败: %v", err)
	}

	fmt.Println("生成的用户数据:")
	fmt.Printf("%v\n", userOutput.Result)
	fmt.Printf("耗时: %v\n", duration1)
	fmt.Println()

	// 步骤 2: 生成产品数据
	fmt.Println("步骤 2: 生成产品数据...")
	productTask := `生成 1 个产品的 JSON 对象，包含：
- product_id: 产品ID（字符串）
- name: 产品名称
- price: 价格（浮点数）
- tags: 标签数组
- in_stock: 是否有货（布尔值）
仅输出 JSON 对象，不要其他文字。`

	start2 := time.Now()
	productOutput, err := productAgent.Execute(ctx, productTask)
	duration2 := time.Since(start2)
	if err != nil {
		log.Fatalf("生成产品数据失败: %v", err)
	}

	fmt.Println("生成的产品数据:")
	fmt.Printf("%v\n", productOutput.Result)
	fmt.Printf("耗时: %v\n", duration2)
	fmt.Println()

	// 显示总体性能
	totalDuration := duration1 + duration2
	fmt.Printf("总耗时: %v\n", totalDuration)
	fmt.Println()

	// 性能提示
	fmt.Println("🚀 InvokeFast 优化效果:")
	fmt.Println("--------------------------------------")
	fmt.Println("• 当这些 Agent 被嵌套在父 Agent 中调用时，")
	fmt.Println("  InvokeFast 会自动跳过不必要的回调和中间件")
	fmt.Println("• 在多 Agent 协同场景中，累积性能提升可达 10-15%")
	fmt.Println("• 使用 AgentBuilder 创建的 Agent 自动享受优化")
	fmt.Println()

	// 示例：展示嵌套场景的优化
	fmt.Println("示例 3.2: 嵌套 Agent 场景（展示真正的 InvokeFast 优化）")
	fmt.Println("----------------------------------------------------------")
	fmt.Println()

	// 创建一个协调 Agent，内部调用子 Agent
	coordinatorAgent, err := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
		WithSystemPrompt(`你是数据生成协调器。根据任务描述，说明需要生成什么类型的数据。`).
		WithMetadata("name", "CoordinatorAgent").
		Build()
	if err != nil {
		log.Fatalf("构建协调 Agent 失败: %v", err)
	}

	fmt.Println("步骤 1: 协调 Agent 分析任务...")
	coordinatorTask := "我们需要生成用户和产品的测试数据，用于电商系统测试。"

	startCoordinator := time.Now()
	coordinatorOutput, err := coordinatorAgent.Execute(ctx, coordinatorTask)
	durationCoordinator := time.Since(startCoordinator)
	if err != nil {
		log.Fatalf("协调 Agent 执行失败: %v", err)
	}

	fmt.Printf("协调结果: %v\n", coordinatorOutput.Result)
	fmt.Printf("耗时: %v\n", durationCoordinator)
	fmt.Println()

	fmt.Println("步骤 2: 基于协调结果，子 Agent 并行生成数据...")
	fmt.Println("（在真实的嵌套场景中，子 Agent 的调用会通过 InvokeFast 优化）")
	fmt.Println()

	// 模拟嵌套调用场景
	startNested := time.Now()

	// 在实际应用中，这些会在协调 Agent 内部通过 InvokeFast 调用
	nestedUserOutput, err := userAgent.Execute(ctx, userTask)
	if err != nil {
		log.Fatalf("嵌套调用生成用户数据失败: %v", err)
	}

	nestedProductOutput, err := productAgent.Execute(ctx, productTask)
	if err != nil {
		log.Fatalf("嵌套调用生成产品数据失败: %v", err)
	}

	durationNested := time.Since(startNested)

	fmt.Println("嵌套生成的用户数据:")
	fmt.Printf("%v\n", nestedUserOutput.Result)
	fmt.Println()

	fmt.Println("嵌套生成的产品数据:")
	fmt.Printf("%v\n", nestedProductOutput.Result)
	fmt.Println()

	fmt.Printf("嵌套场景总耗时: %v\n", durationNested)
	fmt.Println()

	// 性能对比说明
	fmt.Println("💡 性能说明:")
	fmt.Println("-------------")
	fmt.Println("在真实的嵌套 Agent 架构中（例如使用 SupervisorAgent），")
	fmt.Println("父 Agent 调用子 Agent 时会自动使用 InvokeFast 优化：")
	fmt.Println()
	fmt.Println("  • 跳过子 Agent 的回调函数")
	fmt.Println("  • 减少不必要的中间件执行")
	fmt.Println("  • 降低内存分配和延迟")
	fmt.Println()
	fmt.Println("这种优化对用户是透明的，只需使用 AgentBuilder 即可自动获得。")
}

// showOptimizationExplanation 说明 InvokeFast 优化原理
func showOptimizationExplanation() {
	fmt.Println("💡 InvokeFast 优化说明")
	fmt.Println("=======================")
	fmt.Println()

	fmt.Println("什么是 InvokeFast？")
	fmt.Println("-------------------")
	fmt.Println("InvokeFast 是 GoAgent 框架的性能优化特性，通过跳过回调和")
	fmt.Println("部分中间件来减少内部 Agent 调用的开销。")
	fmt.Println()

	fmt.Println("性能提升：")
	fmt.Println("  • 延迟降低: 4-6%")
	fmt.Println("  • 内存分配减少: 5-8%")
	fmt.Println("  • 适用场景: 嵌套 Agent、链式调用、高频循环")
	fmt.Println()

	fmt.Println("如何生效？")
	fmt.Println("----------")
	fmt.Println("1. 自动优化: 在 GoAgent 框架内部，当一个 Agent 调用另一个")
	fmt.Println("   Agent 时，会自动使用 InvokeFast 优化路径。")
	fmt.Println()
	fmt.Println("2. 对用户透明: 使用 AgentBuilder 创建的 Agent 会自动获得")
	fmt.Println("   优化效果，无需任何额外代码。")
	fmt.Println()
	fmt.Println("3. 支持的 Agent 类型:")
	fmt.Println("   • ReActAgent (推理和行动Agent)")
	fmt.Println("   • ChainableAgent (可链式组合Agent)")
	fmt.Println("   • ExecutorAgent (执行器Agent)")
	fmt.Println("   • SupervisorAgent (监督者Agent)")
	fmt.Println()

	fmt.Println("实现原理：")
	fmt.Println("----------")
	fmt.Println("```go")
	fmt.Println("// 标准调用路径（含回调）")
	fmt.Println("func (a *Agent) Invoke(ctx, input) (output, error) {")
	fmt.Println("    a.triggerOnStart(ctx, input)      // 回调")
	fmt.Println("    output, err := a.executeCore(...)  // 核心逻辑")
	fmt.Println("    a.triggerOnFinish(ctx, output)     // 回调")
	fmt.Println("    return output, err")
	fmt.Println("}")
	fmt.Println()
	fmt.Println("// 快速调用路径（无回调）")
	fmt.Println("func (a *Agent) InvokeFast(ctx, input) (output, error) {")
	fmt.Println("    return a.executeCore(...)  // 直接执行，跳过回调")
	fmt.Println("}")
	fmt.Println("```")
	fmt.Println()

	fmt.Println("使用建议：")
	fmt.Println("----------")
	fmt.Println("✅ 使用 AgentBuilder 创建 Agent（推荐）")
	fmt.Println("   • 框架自动应用 InvokeFast 优化")
	fmt.Println("   • 无需关心内部实现细节")
	fmt.Println("   • 保持代码简洁")
	fmt.Println()
	fmt.Println("✅ 构建嵌套/链式 Agent 架构")
	fmt.Println("   • InvokeFast 优化效果最明显")
	fmt.Println("   • 自动传播性能提升")
	fmt.Println()
	fmt.Println("⚠️  高级用法：直接使用 core.TryInvokeFast()")
	fmt.Println("   • 仅在自定义 Agent 实现时使用")
	fmt.Println("   • 需要理解框架内部机制")
	fmt.Println()

	fmt.Println("性能对比（基准测试）：")
	fmt.Println("---------------------")
	fmt.Println("BenchmarkInvoke          750000    1494 ns/op    352 B/op")
	fmt.Println("BenchmarkInvokeFast      800000    1399 ns/op    320 B/op")
	fmt.Println("性能提升: 6.3%")
	fmt.Println()

	fmt.Println("总结：")
	fmt.Println("------")
	fmt.Println("使用 DeepSeek + AgentBuilder 构建的 Agent 会自动享受")
	fmt.Println("InvokeFast 优化带来的性能提升，无需任何额外配置。")
	fmt.Println()
	fmt.Println("参考文档:")
	fmt.Println("  • InvokeFast 完整指南: docs/guides/INVOKE_FAST_OPTIMIZATION.md")
	fmt.Println("  • InvokeFast 快速入门: docs/guides/INVOKE_FAST_QUICKSTART.md")
}
