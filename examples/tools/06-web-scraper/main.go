// Package main 演示网页抓取工具的使用方法
// 本示例展示 WebScraperTool 的基本用法
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools/practical"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          网页抓取工具 (WebScraperTool) 示例                    ║")
	fmt.Println("║   展示网页内容抓取和数据提取                                   ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. 创建网页抓取工具
	fmt.Println("【步骤 1】创建网页抓取工具")
	fmt.Println("────────────────────────────────────────")

	scraper := practical.NewWebScraperTool()
	fmt.Printf("工具名称: %s\n", scraper.Name())
	fmt.Printf("工具描述: %s\n", scraper.Description())
	fmt.Println()

	// 2. 抓取网页基本信息
	fmt.Println("【步骤 2】抓取网页基本信息")
	fmt.Println("────────────────────────────────────────")

	// 使用一个公开的测试网站
	testURL := "https://httpbin.org/html"

	output, err := scraper.Execute(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":                testURL,
			"extract_metadata":   true,
			"max_content_length": 5000,
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ 抓取失败: %v\n", err)
	} else {
		fmt.Printf("✓ 抓取成功: %s\n", testURL)
		if result, ok := output.Result.(map[string]interface{}); ok {
			fmt.Printf("  标题: %v\n", result["title"])
			if content, ok := result["content"].(string); ok {
				// 截断显示
				if len(content) > 300 {
					content = content[:300] + "..."
				}
				fmt.Printf("  内容摘要:\n%s\n", content)
			}
		}
	}
	fmt.Println()

	// 3. 使用 CSS 选择器提取数据
	fmt.Println("【步骤 3】使用 CSS 选择器提取数据")
	fmt.Println("────────────────────────────────────────")

	// 抓取 example.com
	exampleURL := "https://example.com"

	selectorOutput, err := scraper.Execute(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url": exampleURL,
			"selectors": map[string]interface{}{
				"title":   "h1",
				"content": "p",
				"links":   "a",
			},
			"extract_metadata": true,
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ 抓取失败: %v\n", err)
	} else {
		fmt.Printf("✓ 使用选择器抓取成功: %s\n", exampleURL)
		if result, ok := selectorOutput.Result.(map[string]interface{}); ok {
			fmt.Printf("  标题 (h1): %v\n", result["title"])
			if content, ok := result["content"].(string); ok {
				if len(content) > 200 {
					content = content[:200] + "..."
				}
				fmt.Printf("  内容 (p): %s\n", content)
			}
			if links, ok := result["links"].([]interface{}); ok {
				fmt.Printf("  链接数量: %d\n", len(links))
				for i, link := range links {
					if i >= 3 {
						fmt.Printf("    ... 还有 %d 个链接\n", len(links)-3)
						break
					}
					fmt.Printf("    - %v\n", link)
				}
			}
		}
	}
	fmt.Println()

	// 4. 提取元数据
	fmt.Println("【步骤 4】提取页面元数据")
	fmt.Println("────────────────────────────────────────")

	metaOutput, err := scraper.Execute(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":                "https://www.wikipedia.org",
			"extract_metadata":   true,
			"max_content_length": 1000,
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ 抓取失败: %v\n", err)
	} else {
		fmt.Println("✓ 提取元数据成功")
		if result, ok := metaOutput.Result.(map[string]interface{}); ok {
			if metadata, ok := result["metadata"].(map[string]interface{}); ok {
				fmt.Println("  元数据:")
				for key, value := range metadata {
					valueStr := fmt.Sprintf("%v", value)
					if len(valueStr) > 50 {
						valueStr = valueStr[:50] + "..."
					}
					fmt.Printf("    %s: %s\n", key, valueStr)
				}
			}
		}
	}
	fmt.Println()

	// 5. 提取图片
	fmt.Println("【步骤 5】提取图片")
	fmt.Println("────────────────────────────────────────")

	imgOutput, err := scraper.Execute(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url": "https://example.com",
			"selectors": map[string]interface{}{
				"images": "img",
			},
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ 抓取失败: %v\n", err)
	} else {
		fmt.Println("✓ 提取图片成功")
		if result, ok := imgOutput.Result.(map[string]interface{}); ok {
			if images, ok := result["images"].([]interface{}); ok {
				fmt.Printf("  找到 %d 张图片\n", len(images))
				for i, img := range images {
					if i >= 5 {
						break
					}
					fmt.Printf("    - %v\n", img)
				}
			} else {
				fmt.Println("  该页面没有图片")
			}
		}
	}
	fmt.Println()

	// 6. 错误处理示例
	fmt.Println("【步骤 6】错误处理示例")
	fmt.Println("────────────────────────────────────────")

	// 测试无效 URL
	errorCases := []struct {
		url  string
		desc string
	}{
		{"ftp://invalid.protocol", "无效协议"},
		{"https://nonexistent.domain.invalid", "不存在的域名"},
		{"not-a-url", "无效 URL 格式"},
	}

	for _, tc := range errorCases {
		_, err := scraper.Execute(ctx, &interfaces.ToolInput{
			Args: map[string]interface{}{
				"url": tc.url,
			},
			Context: ctx,
		})

		if err != nil {
			fmt.Printf("✓ %s: 正确捕获错误\n", tc.desc)
		} else {
			fmt.Printf("✗ %s: 预期应该失败\n", tc.desc)
		}
	}
	fmt.Println()

	// 7. 自定义选择器示例
	fmt.Println("【步骤 7】自定义选择器")
	fmt.Println("────────────────────────────────────────")

	customOutput, err := scraper.Execute(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url": "https://httpbin.org/html",
			"selectors": map[string]interface{}{
				"custom": map[string]interface{}{
					"heading":    "h1",
					"paragraphs": "p",
				},
			},
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ 自定义选择器失败: %v\n", err)
	} else {
		fmt.Println("✓ 自定义选择器成功")
		if result, ok := customOutput.Result.(map[string]interface{}); ok {
			if custom, ok := result["custom"].(map[string]interface{}); ok {
				fmt.Printf("  自定义提取结果:\n")
				for key, value := range custom {
					valueStr := fmt.Sprintf("%v", value)
					if len(valueStr) > 100 {
						valueStr = valueStr[:100] + "..."
					}
					fmt.Printf("    %s: %s\n", key, valueStr)
				}
			}
		}
	}
	fmt.Println()

	// 总结
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("本示例演示了网页抓取工具的核心功能:")
	fmt.Println("  ✓ 抓取网页内容")
	fmt.Println("  ✓ 使用 CSS 选择器提取特定元素")
	fmt.Println("  ✓ 提取页面元数据（title、description、keywords 等）")
	fmt.Println("  ✓ 提取链接和图片")
	fmt.Println("  ✓ 自定义选择器")
	fmt.Println("  ✓ 错误处理")
	fmt.Println()
	fmt.Println("⚠️  使用提示:")
	fmt.Println("  - 遵守网站的 robots.txt 和服务条款")
	fmt.Println("  - 设置合理的请求间隔，避免对目标服务器造成压力")
	fmt.Println("  - 某些网站可能需要设置特定的 User-Agent")
	fmt.Println("  - JavaScript 渲染的内容可能无法直接抓取")
	fmt.Println()
	fmt.Println("更多工具示例请参考其他目录")
}
