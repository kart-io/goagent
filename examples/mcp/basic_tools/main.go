// Package main 演示 MCP 工具箱的基础功能
//
// 本示例展示：
// - 工具注册
// - 工具执行
// - 参数验证
// - 统计信息
// - 调用历史
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kart-io/goagent/mcp/core"
	"github.com/kart-io/goagent/mcp/toolbox"
	"github.com/kart-io/goagent/mcp/tools"
)

func main() {
	fmt.Println("=== MCP 工具箱基础示例 ===")

	// 1. 创建工具箱
	fmt.Println("步骤 1: 创建工具箱")
	tb := toolbox.NewStandardToolBox()
	fmt.Println("  ✓ 工具箱创建成功")

	// 2. 注册内置工具
	fmt.Println("步骤 2: 注册内置工具")
	if err := tools.RegisterBuiltinTools(tb); err != nil {
		fmt.Printf("  ✗ 注册失败: %v\n", err)
		return
	}
	fmt.Println("  ✓ 内置工具注册成功")

	// 3. 列出所有工具
	fmt.Println("步骤 3: 列出所有已注册工具")
	allTools := tb.List()
	for i, tool := range allTools {
		fmt.Printf("  %d. %s (%s)\n", i+1, tool.Name(), tool.Description())
		fmt.Printf("     分类: %s, 需要认证: %v, 危险操作: %v\n",
			tool.Category(), tool.RequiresAuth(), tool.IsDangerous())
	}
	fmt.Println()

	// 4. 按分类列出工具
	fmt.Println("步骤 4: 按分类列出工具")
	for _, category := range tools.ToolCategories {
		categoryTools := tb.ListByCategory(category)
		if len(categoryTools) > 0 {
			fmt.Printf("  [%s] %s:\n", category, tools.CategoryDescriptions[category])
			for _, tool := range categoryTools {
				fmt.Printf("    - %s: %s\n", tool.Name(), tool.Description())
			}
		}
	}
	fmt.Println()

	// 5. 搜索工具
	fmt.Println("步骤 5: 搜索工具 (关键词: 'file')")
	searchResults := tb.Search("file")
	for _, tool := range searchResults {
		fmt.Printf("  - %s: %s\n", tool.Name(), tool.Description())
	}
	fmt.Println()

	// 6. 获取工具元数据
	fmt.Println("步骤 6: 获取工具元数据 (read_file)")
	metadata, err := tb.GetMetadata("read_file")
	if err != nil {
		fmt.Printf("  ✗ 获取失败: %v\n", err)
	} else {
		fmt.Printf("  名称: %s\n", metadata.Name)
		fmt.Printf("  描述: %s\n", metadata.Description)
		fmt.Printf("  分类: %s\n", metadata.Category)
		fmt.Printf("  参数:\n")
		for name, prop := range metadata.Schema.Properties {
			required := "可选"
			for _, req := range metadata.Schema.Required {
				if req == name {
					required = "必需"
					break
				}
			}
			fmt.Printf("    - %s (%s, %s): %s\n", name, prop.Type, required, prop.Description)
		}
	}
	fmt.Println()

	// 7. 创建临时测试文件
	fmt.Println("步骤 7: 创建临时测试文件")
	tmpFile := "/tmp/mcp_test_file.txt"
	testContent := "Hello, MCP Toolbox!\n这是一个测试文件。\nCreated at: " + time.Now().Format(time.RFC3339)
	if err := os.WriteFile(tmpFile, []byte(testContent), 0o644); err != nil {
		fmt.Printf("  ✗ 创建失败: %v\n", err)
		return
	}
	fmt.Printf("  ✓ 测试文件创建成功: %s\n\n", tmpFile)

	// 8. 执行工具: read_file
	fmt.Println("步骤 8: 执行工具 - read_file")
	ctx := context.Background()
	call := &core.ToolCall{
		ID:        "call-read-1",
		ToolName:  "read_file",
		Input:     map[string]interface{}{"path": tmpFile},
		Timestamp: time.Now(),
		UserID:    "demo-user",
		SessionID: "session-001",
	}

	result, err := tb.Execute(ctx, call)
	if err != nil {
		fmt.Printf("  ✗ 执行失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ 执行成功\n")
		fmt.Printf("    调用 ID: %s\n", result.Call.ID)
		fmt.Printf("    执行耗时: %v\n", result.Result.Duration)
		if data, ok := result.Result.Data.(map[string]interface{}); ok {
			fmt.Printf("    文件大小: %v 字节\n", data["size"])
			content := data["content"].(string)
			if len(content) > 100 {
				content = content[:100] + "..."
			}
			fmt.Printf("    文件内容: %s\n", content)
		}
	}
	fmt.Println()

	// 9. 执行工具: http_request
	fmt.Println("步骤 9: 执行工具 - http_request (GET https://httpbin.org/get)")
	httpCall := &core.ToolCall{
		ID:       "call-http-1",
		ToolName: "http_request",
		Input: map[string]interface{}{
			"url":     "https://httpbin.org/get",
			"method":  "GET",
			"timeout": 10,
		},
		Timestamp: time.Now(),
		UserID:    "demo-user",
		SessionID: "session-001",
	}

	httpResult, err := tb.Execute(ctx, httpCall)
	if err != nil {
		fmt.Printf("  ✗ 执行失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ 执行成功\n")
		fmt.Printf("    执行耗时: %v\n", httpResult.Result.Duration)
		if data, ok := httpResult.Result.Data.(map[string]interface{}); ok {
			fmt.Printf("    状态码: %v\n", data["status_code"])
			fmt.Printf("    响应大小: %v 字节\n", data["size"])
		}
	}
	fmt.Println()

	// 10. 执行工具: json_parse
	fmt.Println("步骤 10: 执行工具 - json_parse")
	jsonCall := &core.ToolCall{
		ID:       "call-json-1",
		ToolName: "json_parse",
		Input: map[string]interface{}{
			"json": `{"name": "MCP Toolbox", "version": "1.0.0", "features": ["file", "http", "json"]}`,
		},
		Timestamp: time.Now(),
		UserID:    "demo-user",
		SessionID: "session-001",
	}

	jsonResult, err := tb.Execute(ctx, jsonCall)
	if err != nil {
		fmt.Printf("  ✗ 执行失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ 执行成功\n")
		fmt.Printf("    执行耗时: %v\n", jsonResult.Result.Duration)
		if data, ok := jsonResult.Result.Data.(map[string]interface{}); ok {
			fmt.Printf("    解析结果: %v\n", data["parsed"])
		}
	}
	fmt.Println()

	// 11. 参数验证错误示例
	fmt.Println("步骤 11: 参数验证错误示例")
	invalidCall := &core.ToolCall{
		ID:       "call-invalid-1",
		ToolName: "read_file",
		Input:    map[string]interface{}{}, // 缺少必需参数 path
	}
	if err := tb.Validate(invalidCall); err != nil {
		fmt.Printf("  ✓ 验证失败（预期行为）: %v\n", err)
	}
	fmt.Println()

	// 12. 获取统计信息
	fmt.Println("步骤 12: 获取工具箱统计信息")
	stats := tb.Statistics()
	fmt.Printf("  工具总数: %d\n", stats.TotalTools)
	fmt.Printf("  总调用次数: %d\n", stats.TotalCalls)
	fmt.Printf("  成功调用: %d\n", stats.SuccessfulCalls)
	fmt.Printf("  失败调用: %d\n", stats.FailedCalls)
	fmt.Printf("  平均延迟: %.2f ms\n", stats.AverageLatency)
	fmt.Println("  工具使用统计:")
	for tool, count := range stats.ToolUsage {
		fmt.Printf("    - %s: %d 次\n", tool, count)
	}
	fmt.Println("  分类使用统计:")
	for category, count := range stats.CategoryUsage {
		fmt.Printf("    - %s: %d 次\n", category, count)
	}
	fmt.Println()

	// 13. 获取调用历史
	fmt.Println("步骤 13: 获取调用历史")
	history := tb.GetCallHistory()
	for i, record := range history {
		fmt.Printf("  %d. [%s] %s\n", i+1,
			record.ExecutedAt.Format("15:04:05"),
			record.Call.ToolName)
		fmt.Printf("     成功: %v, 耗时: %v\n",
			record.Result.Success,
			record.Result.Duration)
	}
	fmt.Println()

	// 14. 清理
	fmt.Println("步骤 14: 清理")
	if err := os.Remove(tmpFile); err != nil {
		fmt.Printf("  ✗ 清理失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ 临时文件已删除: %s\n", tmpFile)
	}

	fmt.Println("\n=== 示例完成 ===")
}
