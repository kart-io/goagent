package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	loggercore "github.com/kart-io/logger/core"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
)

// CodeIteration 代码迭代记录
type CodeIteration struct {
	Iteration   int        `json:"iteration"`
	Code        string     `json:"code"`
	TestResult  TestResult `json:"test_result"`
	Passed      bool       `json:"passed"`
	Feedback    string     `json:"feedback"`
	ProcessedAt time.Time  `json:"processed_at"`
}

// TestResult 测试结果
type TestResult struct {
	Passed       bool     `json:"passed"`
	TotalTests   int      `json:"total_tests"`
	PassedTests  int      `json:"passed_tests"`
	FailedTests  int      `json:"failed_tests"`
	ErrorMessage string   `json:"error_message,omitempty"`
	FailedCases  []string `json:"failed_cases,omitempty"`
}

// RunLoopPattern 运行循环模式示例
func RunLoopPattern(llmClient llm.Client, logger loggercore.Logger) error {
	fmt.Println("\n🔄 模式 3: Loop（循环模式）- 代码编写与测试迭代")
	fmt.Println("说明：编写者Agent和测试者Agent循环协作，直到代码通过所有测试")
	fmt.Println(strings.Repeat("=", 79))

	// 创建 Agent
	writerAgent := createCodeWriterAgent(llmClient)
	testerAgent := createCodeTesterAgent(llmClient)

	// 任务要求
	requirement := `编写一个 Go 函数 factorial，计算阶乘。
要求：
1. 函数签名：func factorial(n int) int
2. 处理负数输入（返回 -1）
3. 处理 0 和 1（返回 1）
4. 正确计算正整数的阶乘
5. 处理大数情况（n > 20 时返回 -1 避免溢出）`

	fmt.Printf("\n📝 任务要求:\n%s\n\n", requirement)

	const maxIterations = 5
	var iterations []CodeIteration
	currentCode := ""

	fmt.Println("开始迭代开发...")

	for i := 1; i <= maxIterations; i++ {
		fmt.Printf("--- 第 %d 次迭代 ---\n", i)

		// 步骤 1: 编写/修改代码
		fmt.Println("📝 编写者Agent正在编写代码...")

		var writerInput string
		if i == 1 {
			// 第一次迭代，提供需求
			writerInput = requirement
		} else {
			// 后续迭代，提供反馈
			prevIteration := iterations[i-2]
			writerInput = fmt.Sprintf(`之前的代码:
%s

测试结果: %s
错误信息: %s
失败的测试用例: %v

请根据反馈修改代码，确保通过所有测试。`,
				prevIteration.Code,
				map[bool]string{true: "通过", false: "失败"}[prevIteration.TestResult.Passed],
				prevIteration.TestResult.ErrorMessage,
				prevIteration.TestResult.FailedCases)
		}

		writerOutput, err := writerAgent.Invoke(context.Background(), &core.AgentInput{
			Task: writerInput,
		})
		if err != nil {
			return fmt.Errorf("编写者Agent失败: %w", err)
		}

		currentCode = writerOutput.Result.(string)
		fmt.Printf("✅ 代码已生成\n\n")

		// 步骤 2: 测试代码
		fmt.Println("🧪 测试者Agent正在测试代码...")

		testerOutput, err := testerAgent.Invoke(context.Background(), &core.AgentInput{
			Task: currentCode,
		})
		if err != nil {
			return fmt.Errorf("测试者Agent失败: %w", err)
		}

		testResult := testerOutput.Result.(TestResult)

		iteration := CodeIteration{
			Iteration:   i,
			Code:        currentCode,
			TestResult:  testResult,
			Passed:      testResult.Passed,
			Feedback:    testResult.ErrorMessage,
			ProcessedAt: time.Now(),
		}

		iterations = append(iterations, iteration)

		// 输出测试结果
		if testResult.Passed {
			fmt.Printf("✅ 测试通过! (%d/%d 测试用例通过)\n\n",
				testResult.PassedTests, testResult.TotalTests)
			fmt.Println("🎉 代码开发完成！")
			break
		}

		fmt.Printf("❌ 测试失败 (%d/%d 测试用例通过)\n",
			testResult.PassedTests, testResult.TotalTests)
		fmt.Printf("   失败原因: %s\n", testResult.ErrorMessage)
		if len(testResult.FailedCases) > 0 {
			fmt.Printf("   失败的测试: %v\n", testResult.FailedCases)
		}
		fmt.Println()

		if i == maxIterations {
			fmt.Println("⚠️  达到最大迭代次数，停止迭代")
		}
	}

	// 输出迭代历史
	fmt.Println("\n📊 迭代历史:")
	fmt.Println(strings.Repeat("-", 79))

	for _, iter := range iterations {
		fmt.Printf("\n第 %d 次迭代:\n", iter.Iteration)
		fmt.Printf("  状态: %s\n", map[bool]string{true: "✅ 通过", false: "❌ 失败"}[iter.Passed])
		fmt.Printf("  测试: %d/%d 通过\n", iter.TestResult.PassedTests, iter.TestResult.TotalTests)
		if !iter.Passed {
			fmt.Printf("  问题: %s\n", iter.Feedback)
		}
	}

	// 输出最终代码
	if len(iterations) > 0 && iterations[len(iterations)-1].Passed {
		fmt.Println("\n📄 最终代码:")
		fmt.Println(strings.Repeat("-", 79))
		fmt.Println(iterations[len(iterations)-1].Code)
		fmt.Println(strings.Repeat("-", 79))
	}

	fmt.Printf("\n💡 循环优势: 通过 %d 次迭代，代码从初版逐步完善到通过所有测试\n", len(iterations))

	return nil
}

// createCodeWriterAgent 创建代码编写 Agent
func createCodeWriterAgent(llmClient llm.Client) core.Agent {
	return &writerAgentImpl{
		iterationCount: 0,
	}
}

// writerAgentImpl 实现 Agent 接口
type writerAgentImpl struct {
	iterationCount int
}

func (w *writerAgentImpl) Name() string           { return "code_writer" }
func (w *writerAgentImpl) Description() string    { return "代码编写Agent" }
func (w *writerAgentImpl) Capabilities() []string { return []string{"code_writing"} }

func (w *writerAgentImpl) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	w.iterationCount++

	// 模拟不同迭代的代码质量
	var code string

	switch w.iterationCount {
	case 1:
		// 第一版：缺少负数和大数处理
		code = `package main

func factorial(n int) int {
    if n == 0 || n == 1 {
        return 1
    }
    result := 1
    for i := 2; i <= n; i++ {
        result *= i
    }
    return result
}`

	case 2:
		// 第二版：添加负数处理，但还缺少大数处理
		code = `package main

func factorial(n int) int {
    if n < 0 {
        return -1  // 处理负数
    }
    if n == 0 || n == 1 {
        return 1
    }
    result := 1
    for i := 2; i <= n; i++ {
        result *= i
    }
    return result
}`

	default:
		// 第三版：完整版本，处理所有边界情况
		code = `package main

func factorial(n int) int {
    // 处理负数
    if n < 0 {
        return -1
    }
    // 处理大数避免溢出
    if n > 20 {
        return -1
    }
    // 处理 0 和 1
    if n == 0 || n == 1 {
        return 1
    }
    // 计算阶乘
    result := 1
    for i := 2; i <= n; i++ {
        result *= i
    }
    return result
}`
	}

	// 模拟思考时间
	time.Sleep(300 * time.Millisecond)

	return &core.AgentOutput{
		Result: code,
		Status: "success",
	}, nil
}

func (w *writerAgentImpl) Stream(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[*core.AgentOutput], error) {
	ch := make(chan core.StreamChunk[*core.AgentOutput])
	close(ch)
	return ch, nil
}

func (w *writerAgentImpl) Batch(ctx context.Context, inputs []*core.AgentInput) ([]*core.AgentOutput, error) {
	return nil, nil
}

func (w *writerAgentImpl) Pipe(next core.Runnable[*core.AgentOutput, any]) core.Runnable[*core.AgentInput, any] {
	return nil
}

func (w *writerAgentImpl) WithCallbacks(callbacks ...core.Callback) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return w
}

func (w *writerAgentImpl) WithConfig(config core.RunnableConfig) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return w
}

// createCodeTesterAgent 创建代码测试 Agent
func createCodeTesterAgent(llmClient llm.Client) core.Agent {
	return &testerAgentImpl{}
}

// testerAgentImpl 实现 Agent 接口
type testerAgentImpl struct{}

func (t *testerAgentImpl) Name() string           { return "code_tester" }
func (t *testerAgentImpl) Description() string    { return "代码测试Agent" }
func (t *testerAgentImpl) Capabilities() []string { return []string{"code_testing"} }

func (t *testerAgentImpl) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	code := input.Task

	// 模拟测试（实际应该执行真实的单元测试）
	testCases := []struct {
		name     string
		input    int
		expected int
	}{
		{"负数测试", -5, -1},
		{"零值测试", 0, 1},
		{"一值测试", 1, 1},
		{"小正整数", 5, 120},
		{"大数测试", 25, -1}, // 应该返回 -1 避免溢出
	}

	var passedTests int
	var failedCases []string
	var errorMessage string

	// 检查代码是否包含必要的处理
	hasNegativeCheck := strings.Contains(code, "n < 0")
	hasOverflowCheck := strings.Contains(code, "n > 20") || strings.Contains(code, "n > 15")

	for _, tc := range testCases {
		// 简化的测试逻辑
		passed := true

		switch {
		case tc.input < 0:
			if !hasNegativeCheck {
				passed = false
				errorMessage = "缺少负数输入处理"
			}
		case tc.input > 20:
			if !hasOverflowCheck {
				passed = false
				errorMessage = "缺少大数溢出处理"
			}
		}

		if passed {
			passedTests++
		} else {
			failedCases = append(failedCases, tc.name)
		}
	}

	totalTests := len(testCases)
	allPassed := passedTests == totalTests

	if allPassed {
		errorMessage = ""
		failedCases = nil
	}

	result := TestResult{
		Passed:       allPassed,
		TotalTests:   totalTests,
		PassedTests:  passedTests,
		FailedTests:  totalTests - passedTests,
		ErrorMessage: errorMessage,
		FailedCases:  failedCases,
	}

	// 模拟测试执行时间
	time.Sleep(200 * time.Millisecond)

	return &core.AgentOutput{
		Result: result,
		Status: "success",
	}, nil
}

func (t *testerAgentImpl) Stream(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[*core.AgentOutput], error) {
	ch := make(chan core.StreamChunk[*core.AgentOutput])
	close(ch)
	return ch, nil
}

func (t *testerAgentImpl) Batch(ctx context.Context, inputs []*core.AgentInput) ([]*core.AgentOutput, error) {
	return nil, nil
}

func (t *testerAgentImpl) Pipe(next core.Runnable[*core.AgentOutput, any]) core.Runnable[*core.AgentInput, any] {
	return nil
}

func (t *testerAgentImpl) WithCallbacks(callbacks ...core.Callback) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return t
}

func (t *testerAgentImpl) WithConfig(config core.RunnableConfig) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return t
}
