// Package main 演示中间件与可观测性功能
//
// 本示例展示：
// 1. 日志中间件（LoggingMiddleware）的使用
// 2. 缓存中间件（CachingMiddleware）的使用
// 3. 限流中间件（RateLimitMiddleware）的使用
// 4. 中间件链式组合
// 5. 自定义中间件创建
// 6. 可观测性指标收集
package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/multiagent"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/middleware"

	loggercore "github.com/kart-io/logger/core"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          中间件与可观测性示例                                   ║")
	fmt.Println("║   展示日志、缓存、限流中间件和可观测性指标收集                   ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 创建日志和系统
	logger := &simpleLogger{}
	system := multiagent.NewMultiAgentSystem(logger)
	defer func() { _ = system.Close() }()

	// 场景 1：日志中间件
	fmt.Println("【场景 1】日志中间件")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateLoggingMiddleware(ctx)

	// 场景 2：缓存中间件
	fmt.Println("\n【场景 2】缓存中间件")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateCachingMiddleware(ctx)

	// 场景 3：限流中间件
	fmt.Println("\n【场景 3】限流中间件")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateRateLimitMiddleware(ctx)

	// 场景 4：中间件链式组合
	fmt.Println("\n【场景 4】中间件链式组合")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateMiddlewareChain(ctx)

	// 场景 5：自定义中间件
	fmt.Println("\n【场景 5】自定义中间件")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateCustomMiddleware(ctx)

	// 场景 6：可观测性指标收集
	fmt.Println("\n【场景 6】可观测性指标收集")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateObservability(ctx, system)

	fmt.Println("\n╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
}

// ============================================================================
// 场景 1：日志中间件
// ============================================================================

func demonstrateLoggingMiddleware(ctx context.Context) {
	fmt.Println("\n场景描述: 展示日志中间件记录工具调用的输入输出")
	fmt.Println()

	// 创建日志记录器
	logRecorder := &LogRecorder{}

	// 创建日志中间件
	loggingMW := middleware.NewLoggingMiddleware(
		middleware.WithLogger(logRecorder),
	)

	fmt.Println("1. 创建带日志中间件的工具")
	fmt.Println("────────────────────────────────────────")

	// 创建基础工具
	calcTool := createCalculatorTool()

	// 包装中间件（使用接口式中间件）
	wrappedTool := tools.WithMiddleware(calcTool, loggingMW)

	fmt.Printf("  原始工具: %s\n", calcTool.Name())
	fmt.Printf("  包装后工具: %s\n", wrappedTool.Name())

	// 执行工具调用
	fmt.Println("\n2. 执行工具调用")
	fmt.Println("────────────────────────────────────────")

	result, err := wrappedTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "multiply",
			"a":         12.0,
			"b":         5.0,
		},
	})

	if err != nil {
		fmt.Printf("  ✗ 调用失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ 调用成功: %v\n", result.Result)
	}

	// 显示日志记录
	fmt.Println("\n3. 日志记录")
	fmt.Println("────────────────────────────────────────")
	for _, log := range logRecorder.GetLogs() {
		fmt.Printf("  [%s] %s\n", log.Level, log.Message)
	}
}

// ============================================================================
// 场景 2：缓存中间件
// ============================================================================

func demonstrateCachingMiddleware(ctx context.Context) {
	fmt.Println("\n场景描述: 展示缓存中间件避免重复计算")
	fmt.Println()

	// 创建缓存中间件（使用函数式接口）
	cachingMW := middleware.Caching(
		middleware.WithTTL(5 * time.Minute),
	)

	// 创建带执行计数的工具
	execCount := int32(0)
	slowTool := tools.NewFunctionTool(
		"slow_calculator",
		"模拟耗时计算",
		`{"type": "object", "properties": {"value": {"type": "number"}}}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			atomic.AddInt32(&execCount, 1)
			// 模拟耗时操作
			time.Sleep(100 * time.Millisecond)
			value := args["value"].(float64)
			return map[string]interface{}{
				"input":  value,
				"result": value * value,
			}, nil
		},
	)

	// 包装缓存中间件
	cachedTool := tools.WithMiddleware(slowTool, cachingMW)

	fmt.Println("1. 首次调用（缓存未命中）")
	fmt.Println("────────────────────────────────────────")

	start := time.Now()
	result1, _ := cachedTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{"value": 10.0},
	})
	duration1 := time.Since(start)
	fmt.Printf("  结果: %v\n", result1.Result)
	fmt.Printf("  耗时: %v\n", duration1)
	fmt.Printf("  实际执行次数: %d\n", atomic.LoadInt32(&execCount))

	fmt.Println("\n2. 第二次调用（相同参数，缓存命中）")
	fmt.Println("────────────────────────────────────────")

	start = time.Now()
	result2, _ := cachedTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{"value": 10.0},
	})
	duration2 := time.Since(start)
	fmt.Printf("  结果: %v\n", result2.Result)
	fmt.Printf("  耗时: %v\n", duration2)
	fmt.Printf("  实际执行次数: %d (未增加，使用缓存)\n", atomic.LoadInt32(&execCount))

	fmt.Println("\n3. 第三次调用（不同参数，缓存未命中）")
	fmt.Println("────────────────────────────────────────")

	start = time.Now()
	result3, _ := cachedTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{"value": 20.0},
	})
	duration3 := time.Since(start)
	fmt.Printf("  结果: %v\n", result3.Result)
	fmt.Printf("  耗时: %v\n", duration3)
	fmt.Printf("  实际执行次数: %d\n", atomic.LoadInt32(&execCount))

	fmt.Println("\n4. 缓存效果统计")
	fmt.Println("────────────────────────────────────────")
	fmt.Printf("  总调用次数: 3\n")
	fmt.Printf("  实际执行次数: %d\n", atomic.LoadInt32(&execCount))
	fmt.Printf("  缓存命中率: %.1f%%\n", float64(3-int(atomic.LoadInt32(&execCount)))/3*100)
}

// ============================================================================
// 场景 3：限流中间件
// ============================================================================

func demonstrateRateLimitMiddleware(ctx context.Context) {
	fmt.Println("\n场景描述: 展示限流中间件控制调用频率")
	fmt.Println()

	// 创建限流中间件（每秒 2 个请求，突发 3 个）
	rateLimitMW := middleware.RateLimit(
		middleware.WithQPS(2),
		middleware.WithBurst(3),
	)

	// 创建简单工具
	simpleTool := tools.NewFunctionTool(
		"echo",
		"回显输入",
		`{"type": "object", "properties": {"message": {"type": "string"}}}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			return args["message"], nil
		},
	)

	// 包装限流中间件
	rateLimitedTool := tools.WithMiddleware(simpleTool, rateLimitMW)

	fmt.Println("1. 限流配置")
	fmt.Println("────────────────────────────────────────")
	fmt.Println("  速率限制: 2 请求/秒")
	fmt.Println("  突发容量: 3 请求")

	fmt.Println("\n2. 快速连续调用测试")
	fmt.Println("────────────────────────────────────────")

	successCount := 0
	failCount := 0

	// 快速发送 6 个请求
	for i := 1; i <= 6; i++ {
		_, err := rateLimitedTool.Invoke(ctx, &interfaces.ToolInput{
			Args: map[string]interface{}{"message": fmt.Sprintf("请求 %d", i)},
		})

		if err != nil {
			failCount++
			fmt.Printf("  请求 %d: ✗ 被限流\n", i)
		} else {
			successCount++
			fmt.Printf("  请求 %d: ✓ 成功\n", i)
		}
	}

	fmt.Println("\n3. 等待令牌恢复后重试")
	fmt.Println("────────────────────────────────────────")
	fmt.Println("  等待 1 秒...")
	time.Sleep(1 * time.Second)

	// 再次尝试
	_, err := rateLimitedTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{"message": "恢复后的请求"},
	})

	if err != nil {
		fmt.Printf("  请求: ✗ 被限流\n")
	} else {
		successCount++
		fmt.Println("  请求: ✓ 成功")
	}

	fmt.Println("\n4. 统计结果")
	fmt.Println("────────────────────────────────────────")
	fmt.Printf("  成功请求: %d\n", successCount)
	fmt.Printf("  被限流请求: %d\n", failCount)
}

// ============================================================================
// 场景 4：中间件链式组合
// ============================================================================

func demonstrateMiddlewareChain(ctx context.Context) {
	fmt.Println("\n场景描述: 展示多个中间件的链式组合（洋葱模型）")
	fmt.Println()

	// 创建执行顺序记录器
	orderRecorder := &OrderRecorder{}

	// 创建多个自定义中间件（使用函数式接口）
	mw1 := createOrderTrackingMiddleware("日志中间件", orderRecorder)
	mw2 := createOrderTrackingMiddleware("认证中间件", orderRecorder)
	mw3 := createOrderTrackingMiddleware("缓存中间件", orderRecorder)

	// 创建工具
	tool := tools.NewFunctionTool(
		"test_tool",
		"测试工具",
		`{"type": "object"}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			orderRecorder.Record("工具执行")
			return "工具执行完成", nil
		},
	)

	// 使用 WithMiddleware 链式组合中间件
	wrappedTool := tools.WithMiddleware(tool, mw1, mw2, mw3)

	fmt.Println("1. 中间件链配置")
	fmt.Println("────────────────────────────────────────")
	fmt.Println("  中间件顺序: 日志 → 认证 → 缓存 → 工具")
	fmt.Println("  执行模型: 洋葱模型（先进后出）")

	fmt.Println("\n2. 执行工具调用")
	fmt.Println("────────────────────────────────────────")

	_, _ = wrappedTool.Invoke(ctx, &interfaces.ToolInput{Args: map[string]interface{}{}})

	fmt.Println("\n3. 执行顺序记录")
	fmt.Println("────────────────────────────────────────")

	for i, record := range orderRecorder.GetRecords() {
		fmt.Printf("  %d. %s\n", i+1, record)
	}

	fmt.Println("\n4. 洋葱模型说明")
	fmt.Println("────────────────────────────────────────")
	fmt.Println("  请求进入顺序: 日志 → 认证 → 缓存 → 工具")
	fmt.Println("  响应返回顺序: 工具 → 缓存 → 认证 → 日志")
}

// ============================================================================
// 场景 5：自定义中间件
// ============================================================================

func demonstrateCustomMiddleware(ctx context.Context) {
	fmt.Println("\n场景描述: 展示如何创建自定义中间件")
	fmt.Println()

	// 1. 计时中间件
	fmt.Println("1. 计时中间件")
	fmt.Println("────────────────────────────────────────")

	timingMW := createTimingMiddleware()

	tool := tools.NewFunctionTool(
		"slow_operation",
		"模拟耗时操作",
		`{"type": "object", "properties": {"delay_ms": {"type": "number"}}}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			delay := int(args["delay_ms"].(float64))
			time.Sleep(time.Duration(delay) * time.Millisecond)
			return map[string]interface{}{"delayed": delay}, nil
		},
	)

	timedTool := tools.WithMiddleware(tool, timingMW)

	_, _ = timedTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{"delay_ms": 150.0},
	})

	// 2. 重试中间件
	fmt.Println("\n2. 重试中间件")
	fmt.Println("────────────────────────────────────────")

	retryMW := createRetryMiddleware(3, 100*time.Millisecond)

	failCount := int32(0)
	unreliableTool := tools.NewFunctionTool(
		"unreliable",
		"模拟不稳定服务",
		`{"type": "object"}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			count := atomic.AddInt32(&failCount, 1)
			if count < 3 {
				return nil, fmt.Errorf("模拟失败 (第 %d 次)", count)
			}
			return "成功", nil
		},
	)

	retriedTool := tools.WithMiddleware(unreliableTool, retryMW)

	result, err := retriedTool.Invoke(ctx, &interfaces.ToolInput{Args: map[string]interface{}{}})
	if err != nil {
		fmt.Printf("  ✗ 最终失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ 最终成功: %v (经过 %d 次尝试)\n", result.Result, atomic.LoadInt32(&failCount))
	}

	// 3. 指标收集中间件
	fmt.Println("\n3. 指标收集中间件")
	fmt.Println("────────────────────────────────────────")

	metricsMW, getStats := createMetricsMiddleware()

	metricTool := tools.NewFunctionTool(
		"metric_test",
		"指标测试工具",
		`{"type": "object"}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			return "ok", nil
		},
	)

	instrumentedTool := tools.WithMiddleware(metricTool, metricsMW)

	// 执行多次调用
	for i := 0; i < 5; i++ {
		_, _ = instrumentedTool.Invoke(ctx, &interfaces.ToolInput{Args: map[string]interface{}{}})
	}

	stats := getStats()
	fmt.Printf("  总调用次数: %d\n", stats.TotalCalls)
	fmt.Printf("  成功次数: %d\n", stats.SuccessCalls)
	fmt.Printf("  失败次数: %d\n", stats.FailedCalls)
	fmt.Printf("  平均耗时: %v\n", stats.AvgDuration)
}

// ============================================================================
// 场景 6：可观测性指标收集
// ============================================================================

func demonstrateObservability(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println("\n场景描述: 展示完整的可观测性指标收集方案")
	fmt.Println()

	// 创建可观测性收集器
	collector := NewObservabilityCollector()

	// 创建带完整可观测性的工具集
	fmt.Println("1. 创建可观测工具集")
	fmt.Println("────────────────────────────────────────")

	toolConfigs := []struct {
		name   string
		create func() interfaces.Tool
	}{
		{"calculator", func() interfaces.Tool { return createCalculatorTool() }},
		{"text_processor", func() interfaces.Tool { return createTextProcessorTool() }},
		{"datetime", func() interfaces.Tool { return createDateTimeTool() }},
	}

	observableTools := make(map[string]interfaces.Tool)
	for _, tc := range toolConfigs {
		baseTool := tc.create()
		obsMW := collector.CreateMiddleware(tc.name)
		observableTools[tc.name] = tools.WithMiddleware(baseTool, obsMW)
		fmt.Printf("  ✓ %s: 已添加可观测性\n", tc.name)
	}

	// 模拟多次调用
	fmt.Println("\n2. 模拟工具调用")
	fmt.Println("────────────────────────────────────────")

	// 调用 calculator
	for i := 0; i < 10; i++ {
		_, _ = observableTools["calculator"].Invoke(ctx, &interfaces.ToolInput{
			Args: map[string]interface{}{"operation": "add", "a": float64(i), "b": float64(i * 2)},
		})
	}
	fmt.Println("  calculator: 10 次调用")

	// 调用 text_processor
	for i := 0; i < 5; i++ {
		_, _ = observableTools["text_processor"].Invoke(ctx, &interfaces.ToolInput{
			Args: map[string]interface{}{"text": "hello", "action": "uppercase"},
		})
	}
	fmt.Println("  text_processor: 5 次调用")

	// 调用 datetime
	for i := 0; i < 3; i++ {
		_, _ = observableTools["datetime"].Invoke(ctx, &interfaces.ToolInput{
			Args: map[string]interface{}{"format": "full"},
		})
	}
	fmt.Println("  datetime: 3 次调用")

	// 显示可观测性报告
	fmt.Println("\n3. 可观测性报告")
	fmt.Println("────────────────────────────────────────")

	report := collector.GenerateReport()
	fmt.Println(report)

	// 显示告警（如果有）
	fmt.Println("4. 告警检查")
	fmt.Println("────────────────────────────────────────")

	alerts := collector.CheckAlerts()
	if len(alerts) == 0 {
		fmt.Println("  ✓ 无告警")
	} else {
		for _, alert := range alerts {
			fmt.Printf("  ⚠ %s\n", alert)
		}
	}
}

// ============================================================================
// 工具定义
// ============================================================================

func createCalculatorTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"calculator",
		"执行基本数学计算",
		`{
			"type": "object",
			"properties": {
				"operation": {"type": "string", "enum": ["add", "subtract", "multiply", "divide"]},
				"a": {"type": "number"},
				"b": {"type": "number"}
			},
			"required": ["operation", "a", "b"]
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			op := args["operation"].(string)
			a := args["a"].(float64)
			b := args["b"].(float64)

			var result float64
			switch op {
			case "add":
				result = a + b
			case "subtract":
				result = a - b
			case "multiply":
				result = a * b
			case "divide":
				if b == 0 {
					return nil, fmt.Errorf("除数不能为零")
				}
				result = a / b
			}
			return map[string]interface{}{"result": result}, nil
		},
	)
}

func createTextProcessorTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"text_processor",
		"处理文本",
		`{
			"type": "object",
			"properties": {
				"text": {"type": "string"},
				"action": {"type": "string", "enum": ["uppercase", "lowercase"]}
			},
			"required": ["text", "action"]
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			text := args["text"].(string)
			action := args["action"].(string)

			var result string
			switch action {
			case "uppercase":
				result = strings.ToUpper(text)
			case "lowercase":
				result = strings.ToLower(text)
			}
			return map[string]interface{}{"result": result}, nil
		},
	)
}

func createDateTimeTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"datetime",
		"获取当前日期时间",
		`{
			"type": "object",
			"properties": {
				"format": {"type": "string", "enum": ["date", "time", "full"]}
			}
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			format := "full"
			if f, ok := args["format"].(string); ok {
				format = f
			}

			now := time.Now()
			var result string
			switch format {
			case "date":
				result = now.Format("2006-01-02")
			case "time":
				result = now.Format("15:04:05")
			case "full":
				result = now.Format("2006-01-02 15:04:05")
			}
			return map[string]interface{}{"result": result}, nil
		},
	)
}

// ============================================================================
// 自定义中间件（函数式实现）
// ============================================================================

// createOrderTrackingMiddleware 创建顺序跟踪中间件
func createOrderTrackingMiddleware(name string, recorder *OrderRecorder) middleware.ToolMiddlewareFunc {
	return func(tool interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
		return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			recorder.Record(fmt.Sprintf("%s: OnBeforeInvoke", name))
			output, err := next(ctx, input)
			recorder.Record(fmt.Sprintf("%s: OnAfterInvoke", name))
			return output, err
		}
	}
}

// createTimingMiddleware 创建计时中间件
func createTimingMiddleware() middleware.ToolMiddlewareFunc {
	return func(tool interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
		return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			start := time.Now()
			output, err := next(ctx, input)
			duration := time.Since(start)
			fmt.Printf("  [计时] %s 执行耗时: %v\n", tool.Name(), duration)
			return output, err
		}
	}
}

// createRetryMiddleware 创建重试中间件
func createRetryMiddleware(maxRetries int, delay time.Duration) middleware.ToolMiddlewareFunc {
	return func(tool interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
		return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			var output *interfaces.ToolOutput
			var err error

			for i := 0; i < maxRetries; i++ {
				output, err = next(ctx, input)
				if err == nil {
					if i > 0 {
						fmt.Printf("  [重试] 第 %d 次重试成功\n", i)
					}
					return output, nil
				}

				if i < maxRetries-1 {
					fmt.Printf("  [重试] 第 %d 次重试 (原因: %v)\n", i+1, err)
					time.Sleep(delay)
				}
			}

			return output, fmt.Errorf("重试 %d 次后仍失败: %w", maxRetries, err)
		}
	}
}

// MetricsStats 指标统计
type MetricsStats struct {
	TotalCalls   int64
	SuccessCalls int64
	FailedCalls  int64
	AvgDuration  time.Duration
}

// createMetricsMiddleware 创建指标收集中间件
func createMetricsMiddleware() (middleware.ToolMiddlewareFunc, func() MetricsStats) {
	var totalCalls, successCalls, failedCalls, totalTime int64

	mw := func(tool interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
		return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			start := time.Now()
			output, err := next(ctx, input)
			duration := time.Since(start)

			atomic.AddInt64(&totalCalls, 1)
			atomic.AddInt64(&totalTime, int64(duration))

			if err != nil {
				atomic.AddInt64(&failedCalls, 1)
			} else {
				atomic.AddInt64(&successCalls, 1)
			}

			return output, err
		}
	}

	getStats := func() MetricsStats {
		total := atomic.LoadInt64(&totalCalls)
		var avgDuration time.Duration
		if total > 0 {
			avgDuration = time.Duration(atomic.LoadInt64(&totalTime) / total)
		}
		return MetricsStats{
			TotalCalls:   total,
			SuccessCalls: atomic.LoadInt64(&successCalls),
			FailedCalls:  atomic.LoadInt64(&failedCalls),
			AvgDuration:  avgDuration,
		}
	}

	return mw, getStats
}

// ============================================================================
// 可观测性收集器
// ============================================================================

// ObservabilityCollector 可观测性收集器
type ObservabilityCollector struct {
	toolMetrics map[string]*ToolMetrics
	mu          sync.RWMutex
}

// ToolMetrics 工具指标
type ToolMetrics struct {
	Name         string
	TotalCalls   int64
	SuccessCalls int64
	FailedCalls  int64
	TotalTime    int64
	MinTime      int64
	MaxTime      int64
}

// NewObservabilityCollector 创建可观测性收集器
func NewObservabilityCollector() *ObservabilityCollector {
	return &ObservabilityCollector{
		toolMetrics: make(map[string]*ToolMetrics),
	}
}

// CreateMiddleware 创建可观测性中间件
func (c *ObservabilityCollector) CreateMiddleware(toolName string) middleware.ToolMiddlewareFunc {
	c.mu.Lock()
	if _, exists := c.toolMetrics[toolName]; !exists {
		c.toolMetrics[toolName] = &ToolMetrics{
			Name:    toolName,
			MinTime: int64(^uint64(0) >> 1), // MaxInt64
		}
	}
	c.mu.Unlock()

	return func(tool interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
		return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			start := time.Now()
			output, err := next(ctx, input)
			duration := time.Since(start)

			c.recordCall(toolName, duration, err == nil)

			return output, err
		}
	}
}

func (c *ObservabilityCollector) recordCall(toolName string, duration time.Duration, success bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	metrics := c.toolMetrics[toolName]
	if metrics == nil {
		return
	}

	metrics.TotalCalls++
	if success {
		metrics.SuccessCalls++
	} else {
		metrics.FailedCalls++
	}

	durationNs := int64(duration)
	metrics.TotalTime += durationNs
	if durationNs < metrics.MinTime {
		metrics.MinTime = durationNs
	}
	if durationNs > metrics.MaxTime {
		metrics.MaxTime = durationNs
	}
}

// GenerateReport 生成报告
func (c *ObservabilityCollector) GenerateReport() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString("\n  ┌─────────────────┬────────┬────────┬────────┬──────────┬──────────┐\n")
	sb.WriteString("  │ 工具名称        │ 总调用 │ 成功   │ 失败   │ 平均耗时 │ 最大耗时 │\n")
	sb.WriteString("  ├─────────────────┼────────┼────────┼────────┼──────────┼──────────┤\n")

	for _, metrics := range c.toolMetrics {
		avgTime := time.Duration(0)
		if metrics.TotalCalls > 0 {
			avgTime = time.Duration(metrics.TotalTime / metrics.TotalCalls)
		}
		maxTime := time.Duration(metrics.MaxTime)

		sb.WriteString(fmt.Sprintf("  │ %-15s │ %6d │ %6d │ %6d │ %8v │ %8v │\n",
			metrics.Name,
			metrics.TotalCalls,
			metrics.SuccessCalls,
			metrics.FailedCalls,
			avgTime.Truncate(time.Microsecond),
			maxTime.Truncate(time.Microsecond),
		))
	}

	sb.WriteString("  └─────────────────┴────────┴────────┴────────┴──────────┴──────────┘")
	return sb.String()
}

// CheckAlerts 检查告警
func (c *ObservabilityCollector) CheckAlerts() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var alerts []string
	for _, metrics := range c.toolMetrics {
		// 检查错误率
		if metrics.TotalCalls > 0 {
			errorRate := float64(metrics.FailedCalls) / float64(metrics.TotalCalls)
			if errorRate > 0.1 { // 10% 错误率告警
				alerts = append(alerts, fmt.Sprintf("%s: 错误率过高 (%.1f%%)", metrics.Name, errorRate*100))
			}
		}

		// 检查平均耗时
		if metrics.TotalCalls > 0 {
			avgTime := time.Duration(metrics.TotalTime / metrics.TotalCalls)
			if avgTime > 100*time.Millisecond { // 100ms 耗时告警
				alerts = append(alerts, fmt.Sprintf("%s: 平均耗时过长 (%v)", metrics.Name, avgTime))
			}
		}
	}
	return alerts
}

// ============================================================================
// 辅助结构
// ============================================================================

// LogRecorder 日志记录器
type LogRecorder struct {
	logs []LogEntry
	mu   sync.Mutex
}

// LogEntry 日志条目
type LogEntry struct {
	Level   string
	Message string
}

func (r *LogRecorder) Debug(args ...interface{})                   {}
func (r *LogRecorder) Info(args ...interface{})                    { r.log("INFO", args...) }
func (r *LogRecorder) Warn(args ...interface{})                    { r.log("WARN", args...) }
func (r *LogRecorder) Error(args ...interface{})                   { r.log("ERROR", args...) }
func (r *LogRecorder) Fatal(args ...interface{})                   {}
func (r *LogRecorder) Debugf(template string, args ...interface{}) {}
func (r *LogRecorder) Infof(template string, args ...interface{})  { r.logf("INFO", template, args...) }
func (r *LogRecorder) Warnf(template string, args ...interface{})  { r.logf("WARN", template, args...) }
func (r *LogRecorder) Errorf(template string, args ...interface{}) {
	r.logf("ERROR", template, args...)
}
func (r *LogRecorder) Fatalf(template string, args ...interface{})     {}
func (r *LogRecorder) Debugw(msg string, keysAndValues ...interface{}) {}
func (r *LogRecorder) Infow(msg string, keysAndValues ...interface{}) {
	r.logw("INFO", msg, keysAndValues...)
}
func (r *LogRecorder) Warnw(msg string, keysAndValues ...interface{}) {
	r.logw("WARN", msg, keysAndValues...)
}
func (r *LogRecorder) Errorw(msg string, keysAndValues ...interface{}) {
	r.logw("ERROR", msg, keysAndValues...)
}
func (r *LogRecorder) Fatalw(msg string, keysAndValues ...interface{}) {}
func (r *LogRecorder) With(keyValues ...interface{}) loggercore.Logger { return r }
func (r *LogRecorder) WithCtx(_ context.Context, keyValues ...interface{}) loggercore.Logger {
	return r
}
func (r *LogRecorder) WithCallerSkip(skip int) loggercore.Logger { return r }
func (r *LogRecorder) SetLevel(level loggercore.Level)           {}
func (r *LogRecorder) Sync() error                               { return nil }
func (r *LogRecorder) Flush() error                              { return nil }

func (r *LogRecorder) log(level string, args ...interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, LogEntry{Level: level, Message: fmt.Sprint(args...)})
}

func (r *LogRecorder) logf(level string, template string, args ...interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, LogEntry{Level: level, Message: fmt.Sprintf(template, args...)})
}

func (r *LogRecorder) logw(level string, msg string, keysAndValues ...interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, LogEntry{Level: level, Message: msg})
}

// GetLogs 获取日志
func (r *LogRecorder) GetLogs() []LogEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]LogEntry{}, r.logs...)
}

var _ loggercore.Logger = (*LogRecorder)(nil)

// OrderRecorder 顺序记录器
type OrderRecorder struct {
	records []string
	mu      sync.Mutex
}

// Record 记录
func (r *OrderRecorder) Record(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, msg)
}

// GetRecords 获取记录
func (r *OrderRecorder) GetRecords() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.records...)
}

// ============================================================================
// 日志实现
// ============================================================================

type simpleLogger struct{}

func (l *simpleLogger) Debug(args ...interface{})                       {}
func (l *simpleLogger) Info(args ...interface{})                        {}
func (l *simpleLogger) Warn(args ...interface{})                        {}
func (l *simpleLogger) Error(args ...interface{})                       {}
func (l *simpleLogger) Fatal(args ...interface{})                       {}
func (l *simpleLogger) Debugf(template string, args ...interface{})     {}
func (l *simpleLogger) Infof(template string, args ...interface{})      {}
func (l *simpleLogger) Warnf(template string, args ...interface{})      {}
func (l *simpleLogger) Errorf(template string, args ...interface{})     {}
func (l *simpleLogger) Fatalf(template string, args ...interface{})     {}
func (l *simpleLogger) Debugw(msg string, keysAndValues ...interface{}) {}
func (l *simpleLogger) Infow(msg string, keysAndValues ...interface{})  {}
func (l *simpleLogger) Warnw(msg string, keysAndValues ...interface{})  {}
func (l *simpleLogger) Errorw(msg string, keysAndValues ...interface{}) {}
func (l *simpleLogger) Fatalw(msg string, keysAndValues ...interface{}) {}
func (l *simpleLogger) With(keyValues ...interface{}) loggercore.Logger { return l }
func (l *simpleLogger) WithCtx(_ context.Context, keyValues ...interface{}) loggercore.Logger {
	return l
}
func (l *simpleLogger) WithCallerSkip(skip int) loggercore.Logger { return l }
func (l *simpleLogger) SetLevel(level loggercore.Level)           {}
func (l *simpleLogger) Sync() error                               { return nil }
func (l *simpleLogger) Flush() error                              { return nil }

var _ loggercore.Logger = (*simpleLogger)(nil)
