// Package main 演示使用 builder 包快速创建各种推理型 Agent
//
// 本示例展示：
// - CoT (Chain-of-Thought) Agent - 链式思维推理
// - ReAct (Reasoning + Acting) Agent - 思考-行动循环
// - ToT (Tree-of-Thought) Agent - 树形搜索推理
// - PoT (Program-of-Thought) Agent - 代码生成推理
// - SoT (Skeleton-of-Thought) Agent - 骨架并行推理
// - GoT (Graph-of-Thought) Agent - 图结构推理
// - MetaCoT (Meta Chain-of-Thought) Agent - 自我提问推理
// - Supervisor Agent - 多 Agent 协调
// - AgentExecutor - Agent 执行器
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kart-io/goagent/builder"
	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/tools"
)

func main() {
	fmt.Println("========================================")
	fmt.Println("推理型 Agent 构建示例")
	fmt.Println("========================================")
	fmt.Println()

	// 从环境变量获取 API Key
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		fmt.Println("警告：未设置 DEEPSEEK_API_KEY 环境变量")
		fmt.Println("提示：export DEEPSEEK_API_KEY=your-api-key")
		fmt.Println("\n使用模拟模式运行示例...")
		runMockExample()
		return
	}

	// 创建 DeepSeek LLM 客户端
	llmClient, err := providers.NewDeepSeekWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("deepseek-chat"),
		llm.WithTemperature(0.7),
	)
	if err != nil {
		fmt.Printf("创建 LLM 客户端失败: %v\n", err)
		return
	}

	// 示例 1: CoT Agent
	fmt.Println("示例 1: CoT (Chain-of-Thought) Agent")
	fmt.Println("-----------------------------------")
	if err := runCoTExample(llmClient); err != nil {
		fmt.Printf("示例 1 失败: %v\n", err)
	}

	// 示例 2: ReAct Agent
	fmt.Println("\n示例 2: ReAct Agent")
	fmt.Println("-----------------------------------")
	if err := runReActExample(llmClient); err != nil {
		fmt.Printf("示例 2 失败: %v\n", err)
	}

	// 示例 3: ToT Agent
	fmt.Println("\n示例 3: ToT (Tree-of-Thought) Agent")
	fmt.Println("-----------------------------------")
	if err := runToTExample(llmClient); err != nil {
		fmt.Printf("示例 3 失败: %v\n", err)
	}

	// 示例 4: SoT Agent
	fmt.Println("\n示例 4: SoT (Skeleton-of-Thought) Agent")
	fmt.Println("-----------------------------------")
	if err := runSoTExample(llmClient); err != nil {
		fmt.Printf("示例 4 失败: %v\n", err)
	}

	// 示例 5: GoT Agent
	fmt.Println("\n示例 5: GoT (Graph-of-Thought) Agent")
	fmt.Println("-----------------------------------")
	if err := runGoTExample(llmClient); err != nil {
		fmt.Printf("示例 5 失败: %v\n", err)
	}

	// 示例 6: MetaCoT Agent
	fmt.Println("\n示例 6: MetaCoT (Self-Ask) Agent")
	fmt.Println("-----------------------------------")
	if err := runMetaCoTExample(llmClient); err != nil {
		fmt.Printf("示例 6 失败: %v\n", err)
	}

	// 示例 7: Supervisor Agent
	fmt.Println("\n示例 7: Supervisor Agent")
	fmt.Println("-----------------------------------")
	if err := runSupervisorExample(llmClient); err != nil {
		fmt.Printf("示例 7 失败: %v\n", err)
	}

	fmt.Println("\n所有示例完成!")
}

// runCoTExample 演示 CoT Agent 使用
func runCoTExample(llmClient llm.Client) error {
	// 使用自定义配置创建 CoT Agent，设置中文格式
	agent := builder.CoTAgent(llmClient, &builder.CoTAgentConfig{
		MaxSteps:        10,
		ZeroShot:        true,
		ShowStepNumbers: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	input := &agentcore.AgentInput{
		Task: "如果一个农场有 15 只鸡和 12 只兔子，请计算总共有多少只脚？请一步一步思考，最后用 'Therefore, the final answer is:' 给出答案。",
	}

	output, err := agent.Invoke(ctx, input)
	if err != nil {
		return fmt.Errorf("CoT Agent 执行失败: %w", err)
	}

	fmt.Printf("问题: %s\n", input.Task)
	// 如果 Result 为空，显示推理步骤
	if output.Result == nil || output.Result == "" {
		fmt.Println("推理步骤:")
		for i, step := range output.Steps {
			fmt.Printf("  步骤 %d: %v\n", i+1, step.Result)
		}
	} else {
		fmt.Printf("推理结果: %v\n", output.Result)
	}
	return nil
}

// runReActExample 演示 ReAct Agent 使用
func runReActExample(llmClient llm.Client) error {
	// 创建计算器工具
	calculatorTool := createCalculatorTool()

	// 使用 QuickReActAgent 快速创建
	agent := builder.QuickReActAgent(llmClient, []interfaces.Tool{calculatorTool})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	input := &agentcore.AgentInput{
		Task: "计算 (25 + 17) * 3 的结果",
	}

	output, err := agent.Invoke(ctx, input)
	if err != nil {
		return fmt.Errorf("ReAct Agent 执行失败: %w", err)
	}

	fmt.Printf("问题: %s\n", input.Task)
	fmt.Printf("结果: %v\n", output.Result)
	return nil
}

// runToTExample 演示 ToT Agent 使用
func runToTExample(llmClient llm.Client) error {
	// 使用自定义配置创建 ToT Agent
	agent := builder.ToTAgent(llmClient, &builder.ToTAgentConfig{
		MaxDepth:        3,
		BranchingFactor: 2,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	input := &agentcore.AgentInput{
		Task: "有一个 3x3 的数独谜题，第一行是 [1, _, 3]，第二行是 [_, 2, _]，第三行是 [3, _, 1]。找出所有空格的数字。",
	}

	output, err := agent.Invoke(ctx, input)
	if err != nil {
		return fmt.Errorf("ToT Agent 执行失败: %w", err)
	}

	fmt.Printf("问题: %s\n", input.Task)
	fmt.Printf("结果: %v\n", output.Result)
	return nil
}

// runSoTExample 演示 SoT Agent 使用
func runSoTExample(llmClient llm.Client) error {
	// 使用自定义配置创建 SoT Agent，减少骨架点数量以加快执行
	agent := builder.SoTAgent(llmClient, &builder.SoTAgentConfig{
		MaxSkeletonPoints:   5,
		MinSkeletonPoints:   2,
		MaxConcurrency:      3,
		ElaborationTimeout:  60 * time.Second,
		AggregationStrategy: "sequential",
	})

	// 增加超时时间，SoT 需要多次 LLM 调用
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	input := &agentcore.AgentInput{
		Task: "请简要介绍 Go 语言的三个主要特点",
	}

	output, err := agent.Invoke(ctx, input)
	if err != nil {
		return fmt.Errorf("SoT Agent 执行失败: %w", err)
	}

	fmt.Printf("问题: %s\n", input.Task)
	fmt.Printf("结果: %v\n", output.Result)
	return nil
}

// runGoTExample 演示 GoT Agent 使用
func runGoTExample(llmClient llm.Client) error {
	// 使用极简模式：专为慢速 API（如 DeepSeek）优化
	// 极简模式仅使用 2 次 LLM 调用：
	//   1. 一次生成多个思考路径
	//   2. 一次合成最终答案
	// 相比标准模式的 6-10 次调用，显著减少等待时间
	agent := builder.GoTAgent(llmClient, &builder.GoTAgentConfig{
		MinimalMode:    true,              // 启用极简模式
		FastEvaluation: true,              // 快速评估
		NodeTimeout:    120 * time.Second, // 单次调用超时
	})

	// 极简模式只需 2 次 LLM 调用，总超时设置为 5 分钟
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	input := &agentcore.AgentInput{
		Task: "简述 API 设计的两个要点。",
	}

	output, err := agent.Invoke(ctx, input)
	if err != nil {
		return fmt.Errorf("GoT Agent 执行失败: %w", err)
	}

	fmt.Printf("问题: %s\n", input.Task)
	fmt.Printf("模式: %v\n", output.Metadata["mode"])
	fmt.Printf("LLM调用次数: %v\n", output.Metadata["llm_calls"])
	fmt.Printf("结果: %v\n", output.Result)
	return nil
}

// runMetaCoTExample 演示 MetaCoT Agent 使用
func runMetaCoTExample(llmClient llm.Client) error {
	// 使用自定义配置创建 MetaCoT Agent，减少问题数量
	agent := builder.MetaCoTAgent(llmClient, &builder.MetaCoTAgentConfig{
		MaxQuestions: 2,
		MaxDepth:     2,
		SelfCritique: false, // 关闭自我批判以加快速度
	})

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	input := &agentcore.AgentInput{
		Task: "为什么天空是蓝色的？",
	}

	output, err := agent.Invoke(ctx, input)
	if err != nil {
		return fmt.Errorf("MetaCoT Agent 执行失败: %w", err)
	}

	fmt.Printf("问题: %s\n", input.Task)
	fmt.Printf("结果: %v\n", output.Result)
	return nil
}

// runSupervisorExample 演示 Supervisor Agent 使用
func runSupervisorExample(llmClient llm.Client) error {
	// 创建子 Agent
	cotAgent := builder.QuickCoTAgent(llmClient)

	// 创建 Supervisor Agent（只使用一个子 Agent 以简化示例）
	supervisor := builder.SupervisorAgent(llmClient, map[string]agentcore.Agent{
		"reasoning": cotAgent,
	}, &builder.SupervisorAgentConfig{
		MaxConcurrentAgents: 1,
		SubAgentTimeout:     60 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	input := &agentcore.AgentInput{
		Task: "解释微服务架构的主要优点。",
	}

	output, err := supervisor.Invoke(ctx, input)
	if err != nil {
		return fmt.Errorf("supervisor agent 执行失败: %w", err)
	}

	fmt.Printf("任务: %s\n", input.Task)
	fmt.Printf("结果: %v\n", output.Result)
	return nil
}

// createCalculatorTool 创建计算器工具
func createCalculatorTool() interfaces.Tool {
	tool, err := tools.NewFunctionToolBuilder("calculator").
		WithDescription("执行数学计算，支持基本的加减乘除运算。输入格式：'数字 运算符 数字'，例如 '15 * 8'").
		WithArgsSchema(`{
			"type": "object",
			"properties": {
				"expression": {
					"type": "string",
					"description": "要计算的数学表达式，如 '15 * 8'"
				}
			},
			"required": ["expression"]
		}`).
		WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			expression, ok := args["expression"].(string)
			if !ok {
				return nil, fmt.Errorf("需要提供 expression 参数")
			}

			// 简化的计算器实现
			var num1, num2 float64
			var op string
			_, err := fmt.Sscanf(expression, "%f %s %f", &num1, &op, &num2)
			if err != nil {
				return nil, fmt.Errorf("无法解析表达式: %w", err)
			}

			var result float64
			switch op {
			case "+":
				result = num1 + num2
			case "-":
				result = num1 - num2
			case "*":
				result = num1 * num2
			case "/":
				if num2 == 0 {
					return nil, fmt.Errorf("除数不能为零")
				}
				result = num1 / num2
			default:
				return nil, fmt.Errorf("不支持的运算符: %s", op)
			}

			return map[string]interface{}{
				"expression": expression,
				"result":     result,
			}, nil
		}).
		Build()
	if err != nil {
		panic(fmt.Sprintf("创建计算器工具失败: %v", err))
	}
	return tool
}

// runMockExample 在没有 API Key 时运行的模拟示例
func runMockExample() {
	fmt.Println("模拟模式：展示各种推理型 Agent 的创建方式")
	fmt.Println("-------------------------------------------")
	fmt.Println()

	fmt.Println("1. CoT (Chain-of-Thought) Agent - 链式思维推理")
	fmt.Print(`
   // 快速创建
   agent := builder.QuickCoTAgent(llmClient)

   // 自定义配置
   agent := builder.CoTAgent(llmClient, &builder.CoTAgentConfig{
       MaxSteps: 10,
       ZeroShot: true,
       ShowStepNumbers: true,
   })
`)

	fmt.Println("\n2. ReAct Agent - 思考-行动循环")
	fmt.Print(`
   // 快速创建（需要工具）
   agent := builder.QuickReActAgent(llmClient, tools)

   // 自定义配置
   agent := builder.ReActAgent(llmClient, tools, &builder.ReActAgentConfig{
       MaxSteps: 15,
       Name: "my-react-agent",
   })
`)

	fmt.Println("\n3. ToT (Tree-of-Thought) Agent - 树形搜索推理")
	fmt.Print(`
   // 快速创建
   agent := builder.QuickToTAgent(llmClient)

   // 自定义配置
   agent := builder.ToTAgent(llmClient, &builder.ToTAgentConfig{
       MaxDepth: 5,
       BranchingFactor: 3,
       SearchStrategy: interfaces.StrategyBeamSearch,
   })
`)

	fmt.Println("\n4. PoT (Program-of-Thought) Agent - 代码生成推理")
	fmt.Print(`
   // 快速创建（默认 Python）
   agent := builder.QuickPoTAgent(llmClient)

   // 自定义配置
   agent := builder.PoTAgent(llmClient, &builder.PoTAgentConfig{
       Language: "python",
       ExecutionTimeout: 30 * time.Second,
       SafeMode: true,
   })
`)

	fmt.Println("\n5. SoT (Skeleton-of-Thought) Agent - 骨架并行推理")
	fmt.Print(`
   // 快速创建
   agent := builder.QuickSoTAgent(llmClient)

   // 自定义配置
   agent := builder.SoTAgent(llmClient, &builder.SoTAgentConfig{
       MaxSkeletonPoints: 10,
       MaxConcurrency: 5,
       AggregationStrategy: "sequential",
   })
`)

	fmt.Println("\n6. GoT (Graph-of-Thought) Agent - 图结构推理")
	fmt.Println("   注意：GoT 需要多次 LLM 调用，建议使用较小的 MaxNodes")
	fmt.Print(`
   // 快速创建（适合快速 API）
   agent := builder.QuickGoTAgent(llmClient)

   // 推荐配置（适合大多数场景）
   agent := builder.GoTAgent(llmClient, &builder.GoTAgentConfig{
       MaxNodes: 5,               // 减少节点数量
       FastEvaluation: true,      // 启用快速评估
       ParallelExecution: false,  // 顺序执行更稳定
       MergeStrategy: "weighted",
   })
`)

	fmt.Println("\n7. MetaCoT (Self-Ask) Agent - 自我提问推理")
	fmt.Print(`
   // 快速创建
   agent := builder.QuickMetaCoTAgent(llmClient)

   // 自定义配置
   agent := builder.MetaCoTAgent(llmClient, &builder.MetaCoTAgentConfig{
       MaxQuestions: 5,
       SelfCritique: true,
       ConfidenceThreshold: 0.7,
   })
`)

	fmt.Println("\n8. Supervisor Agent - 多 Agent 协调")
	fmt.Print(`
   // 创建子 Agent
   cotAgent := builder.QuickCoTAgent(llmClient)
   reactAgent := builder.QuickReActAgent(llmClient, tools)

   // 创建 Supervisor
   supervisor := builder.SupervisorAgent(llmClient, map[string]core.Agent{
       "reasoning": cotAgent,
       "acting": reactAgent,
   }, &builder.SupervisorAgentConfig{
       MaxConcurrentAgents: 3,
       EnableCaching: true,
   })
`)

	fmt.Println("\n9. Agent Executor - Agent 执行器")
	fmt.Print(`
   // 包装任意 Agent
   agent := builder.QuickReActAgent(llmClient, tools)
   executor := builder.AgentExecutor(agent, &builder.ExecutorConfig{
       MaxIterations: 15,
       Verbose: true,
   })
`)

	fmt.Println("\n配置步骤:")
	fmt.Println("   1. 设置环境变量: export DEEPSEEK_API_KEY=your-key")
	fmt.Println("   2. 运行此程序: go run main.go")
}
