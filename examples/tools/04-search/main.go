// Package main 演示搜索工具的使用方法
// 本示例展示 SearchTool 的基本用法，包括模拟搜索引擎和聚合搜索
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools/search"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║              搜索工具 (SearchTool) 示例                        ║")
	fmt.Println("║   展示搜索工具的使用方法，包括模拟引擎和聚合搜索               ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. 创建模拟搜索引擎
	fmt.Println("【步骤 1】创建模拟搜索引擎")
	fmt.Println("────────────────────────────────────────")

	mockEngine := search.NewMockSearchEngine()

	// 添加预设响应
	mockEngine.AddResponse("golang", []search.SearchResult{
		{
			Title:       "Go 编程语言官网",
			URL:         "https://golang.org",
			Snippet:     "Go 是一门开源的编程语言，使构建简单、可靠、高效的软件变得容易。",
			Source:      "golang.org",
			PublishDate: time.Now().AddDate(0, -1, 0),
			Score:       0.98,
		},
		{
			Title:       "Go 语言教程 - 菜鸟教程",
			URL:         "https://www.runoob.com/go/go-tutorial.html",
			Snippet:     "Go 语言教程，Go 语言是一种静态类型、编译型语言...",
			Source:      "runoob.com",
			PublishDate: time.Now().AddDate(0, -3, 0),
			Score:       0.92,
		},
		{
			Title:       "Go by Example",
			URL:         "https://gobyexample.com",
			Snippet:     "Go by Example 是一个实践性的 Go 语言学习资源...",
			Source:      "gobyexample.com",
			PublishDate: time.Now().AddDate(0, -2, 0),
			Score:       0.89,
		},
	})

	mockEngine.AddResponse("ai agent", []search.SearchResult{
		{
			Title:       "什么是 AI Agent？",
			URL:         "https://example.com/ai-agent",
			Snippet:     "AI Agent 是能够自主执行任务的智能程序...",
			Source:      "example.com",
			PublishDate: time.Now().AddDate(0, 0, -10),
			Score:       0.95,
		},
		{
			Title:       "构建 AI Agent 框架",
			URL:         "https://example.com/building-ai-agents",
			Snippet:     "本文介绍如何构建一个完整的 AI Agent 框架...",
			Source:      "example.com",
			PublishDate: time.Now().AddDate(0, 0, -5),
			Score:       0.90,
		},
	})

	fmt.Println("✓ 模拟搜索引擎创建成功")
	fmt.Println("✓ 已添加 'golang' 和 'ai agent' 的预设响应")
	fmt.Println()

	// 2. 创建搜索工具
	fmt.Println("【步骤 2】创建搜索工具")
	fmt.Println("────────────────────────────────────────")

	searchTool := search.NewSearchTool(mockEngine)
	fmt.Printf("工具名称: %s\n", searchTool.Name())
	fmt.Printf("工具描述: %s\n", searchTool.Description())
	fmt.Println()

	// 3. 执行搜索
	fmt.Println("【步骤 3】执行搜索")
	fmt.Println("────────────────────────────────────────")

	queries := []struct {
		query      string
		maxResults int
	}{
		{"golang", 5},
		{"ai agent", 3},
		{"unknown topic", 5}, // 测试通用响应
	}

	for _, q := range queries {
		fmt.Printf("\n搜索: '%s' (最多 %d 条结果)\n", q.query, q.maxResults)

		output, err := searchTool.Invoke(ctx, &interfaces.ToolInput{
			Args: map[string]interface{}{
				"query":       q.query,
				"max_results": float64(q.maxResults),
			},
			Context: ctx,
		})
		if err != nil {
			fmt.Printf("✗ 搜索失败: %v\n", err)
			continue
		}

		if output.Success {
			fmt.Println("✓ 搜索成功")
			if results, ok := output.Result.([]search.SearchResult); ok {
				fmt.Printf("  找到 %d 条结果:\n", len(results))
				for i, result := range results {
					fmt.Printf("  %d. %s\n", i+1, result.Title)
					fmt.Printf("     URL: %s\n", result.URL)
					fmt.Printf("     摘要: %s\n", truncateString(result.Snippet, 50))
					fmt.Printf("     评分: %.2f\n", result.Score)
				}
			}
		} else {
			fmt.Printf("✗ 搜索失败: %s\n", output.Error)
		}
	}
	fmt.Println()

	// 4. 使用不同搜索引擎
	fmt.Println("【步骤 4】使用不同搜索引擎")
	fmt.Println("────────────────────────────────────────")

	// Google 搜索引擎（模拟）
	googleEngine := search.NewGoogleSearchEngine("your-api-key", "your-cx")
	googleTool := search.NewSearchTool(googleEngine)
	fmt.Printf("✓ Google 搜索工具创建成功: %s\n", googleTool.Name())

	// DuckDuckGo 搜索引擎（模拟）
	ddgEngine := search.NewDuckDuckGoSearchEngine()
	ddgTool := search.NewSearchTool(ddgEngine)
	fmt.Printf("✓ DuckDuckGo 搜索工具创建成功: %s\n", ddgTool.Name())

	fmt.Println("（注：以上搜索引擎为模拟实现，生产环境需集成真实 API）")
	fmt.Println()

	// 5. 聚合搜索
	fmt.Println("【步骤 5】聚合搜索")
	fmt.Println("────────────────────────────────────────")

	// 创建多个模拟引擎
	engine1 := search.NewMockSearchEngine()
	engine1.AddResponse("test", []search.SearchResult{
		{Title: "Engine1 Result 1", URL: "https://engine1.com/1", Score: 0.95},
		{Title: "Engine1 Result 2", URL: "https://engine1.com/2", Score: 0.85},
	})

	engine2 := search.NewMockSearchEngine()
	engine2.AddResponse("test", []search.SearchResult{
		{Title: "Engine2 Result 1", URL: "https://engine2.com/1", Score: 0.92},
		{Title: "Engine2 Result 2", URL: "https://engine2.com/2", Score: 0.80},
	})

	// 创建聚合搜索引擎
	aggregatedEngine := search.NewAggregatedSearchEngine(engine1, engine2)
	aggregatedTool := search.NewSearchTool(aggregatedEngine)

	fmt.Println("✓ 创建聚合搜索引擎（包含 2 个搜索源）")

	// 执行聚合搜索
	aggOutput, err := aggregatedTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"query":       "test",
			"max_results": float64(5),
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ 聚合搜索失败: %v\n", err)
	} else if aggOutput.Success {
		fmt.Println("✓ 聚合搜索成功")
		if results, ok := aggOutput.Result.([]search.SearchResult); ok {
			fmt.Printf("  合并并排序后得到 %d 条结果:\n", len(results))
			for i, result := range results {
				fmt.Printf("  %d. %s (评分: %.2f)\n", i+1, result.Title, result.Score)
			}
		}
	}
	fmt.Println()

	// 6. 获取搜索元数据
	fmt.Println("【步骤 6】获取搜索元数据")
	fmt.Println("────────────────────────────────────────")

	metaOutput, _ := searchTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"query":       "golang",
			"max_results": float64(3),
		},
		Context: ctx,
	})

	if metaOutput != nil && metaOutput.Metadata != nil {
		fmt.Println("搜索元数据:")
		fmt.Printf("  查询: %v\n", metaOutput.Metadata["query"])
		fmt.Printf("  结果数: %v\n", metaOutput.Metadata["result_count"])
		fmt.Printf("  最大结果数: %v\n", metaOutput.Metadata["max_results"])
	}
	fmt.Println()

	// 总结
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("本示例演示了搜索工具的核心功能:")
	fmt.Println("  ✓ 创建模拟搜索引擎")
	fmt.Println("  ✓ 添加预设搜索响应")
	fmt.Println("  ✓ 执行搜索查询")
	fmt.Println("  ✓ 使用不同搜索引擎（Google、DuckDuckGo）")
	fmt.Println("  ✓ 聚合搜索（多引擎合并去重排序）")
	fmt.Println("  ✓ 获取搜索元数据")
	fmt.Println()
	fmt.Println("💡 生产环境提示:")
	fmt.Println("  - 需要集成真实的搜索 API（如 Google Custom Search）")
	fmt.Println("  - 注意 API 调用限制和费用")
	fmt.Println("  - 考虑添加搜索结果缓存")
	fmt.Println()
	fmt.Println("更多工具示例请参考其他目录")
}

// truncateString 截断字符串
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
