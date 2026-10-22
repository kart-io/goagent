// Package main 演示 MCP 工具链编排功能
//
// 本示例展示：
// - 顺序执行多个工具
// - 工具间数据传递
// - 批量并行执行
// - 条件分支执行
// - 错误处理和回滚
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

// ToolChainExecutor 工具链执行器
type ToolChainExecutor struct {
	toolbox *toolbox.StandardToolBox
	results []*core.ToolCallResult
	context map[string]interface{}
}

// NewToolChainExecutor 创建工具链执行器
func NewToolChainExecutor(tb *toolbox.StandardToolBox) *ToolChainExecutor {
	return &ToolChainExecutor{
		toolbox: tb,
		results: make([]*core.ToolCallResult, 0),
		context: make(map[string]interface{}),
	}
}

// Execute 执行单个工具并保存结果到上下文
func (e *ToolChainExecutor) Execute(ctx context.Context, name, toolName string, input map[string]interface{}) (*core.ToolCallResult, error) {
	call := &core.ToolCall{
		ID:        fmt.Sprintf("chain-%s-%d", name, time.Now().UnixNano()),
		ToolName:  toolName,
		Input:     input,
		Timestamp: time.Now(),
	}

	result, err := e.toolbox.Execute(ctx, call)
	if err != nil {
		return nil, err
	}

	e.results = append(e.results, result)
	e.context[name] = result.Result.Data
	return result, nil
}

// GetContext 获取上下文数据
func (e *ToolChainExecutor) GetContext(name string) (interface{}, bool) {
	data, ok := e.context[name]
	return data, ok
}

// GetResults 获取所有执行结果
func (e *ToolChainExecutor) GetResults() []*core.ToolCallResult {
	return e.results
}

// Reset 重置执行器
func (e *ToolChainExecutor) Reset() {
	e.results = make([]*core.ToolCallResult, 0)
	e.context = make(map[string]interface{})
}

func main() {
	fmt.Println("=== MCP 工具链编排示例 ===")

	// 初始化工具箱
	tb := toolbox.NewStandardToolBox()
	if err := tools.RegisterBuiltinTools(tb); err != nil {
		fmt.Printf("工具注册失败: %v\n", err)
		return
	}

	ctx := context.Background()
	executor := NewToolChainExecutor(tb)

	// 示例 1: 顺序执行工具链
	fmt.Println("=== 示例 1: 顺序执行工具链 ===")
	fmt.Println("场景: 读取配置文件 -> 解析 JSON -> 处理数据")

	// 创建测试配置文件
	configFile := "/tmp/mcp_config.json"
	configContent := `{
		"app_name": "MCP Demo",
		"version": "1.0.0",
		"settings": {
			"debug": true,
			"max_retries": 3,
			"timeout": 30
		}
	}`
	if err := os.WriteFile(configFile, []byte(configContent), 0o644); err != nil {
		fmt.Printf("创建配置文件失败: %v\n", err)
		return
	}
	fmt.Printf("  步骤 0: 创建配置文件 %s\n", configFile)

	// 步骤 1: 读取文件
	fmt.Println("  步骤 1: 读取配置文件")
	readResult, err := executor.Execute(ctx, "read_config", "read_file", map[string]interface{}{
		"path": configFile,
	})
	if err != nil {
		fmt.Printf("    ✗ 失败: %v\n", err)
		return
	}
	fmt.Printf("    ✓ 成功, 耗时: %v\n", readResult.Result.Duration)

	// 步骤 2: 解析 JSON
	fmt.Println("  步骤 2: 解析 JSON 配置")
	configData, _ := executor.GetContext("read_config")
	content := configData.(map[string]interface{})["content"].(string)

	parseResult, err := executor.Execute(ctx, "parse_config", "json_parse", map[string]interface{}{
		"json": content,
	})
	if err != nil {
		fmt.Printf("    ✗ 失败: %v\n", err)
		return
	}
	fmt.Printf("    ✓ 成功, 耗时: %v\n", parseResult.Result.Duration)

	// 显示解析结果
	parsedConfig, _ := executor.GetContext("parse_config")
	parsedData := parsedConfig.(map[string]interface{})["parsed"].(map[string]interface{})
	fmt.Printf("    解析的配置:\n")
	fmt.Printf("      应用名称: %v\n", parsedData["app_name"])
	fmt.Printf("      版本: %v\n", parsedData["version"])
	if settings, ok := parsedData["settings"].(map[string]interface{}); ok {
		fmt.Printf("      调试模式: %v\n", settings["debug"])
		fmt.Printf("      最大重试: %v\n", settings["max_retries"])
	}
	fmt.Println()

	// 示例 2: 批量并行执行
	fmt.Println("=== 示例 2: 批量并行执行 ===")
	fmt.Println("场景: 同时发送多个 HTTP 请求")

	calls := []*core.ToolCall{
		{
			ID:       "batch-1",
			ToolName: "http_request",
			Input: map[string]interface{}{
				"url":     "https://httpbin.org/get",
				"method":  "GET",
				"timeout": 10,
			},
			Timestamp: time.Now(),
		},
		{
			ID:       "batch-2",
			ToolName: "http_request",
			Input: map[string]interface{}{
				"url":     "https://httpbin.org/headers",
				"method":  "GET",
				"timeout": 10,
			},
			Timestamp: time.Now(),
		},
		{
			ID:       "batch-3",
			ToolName: "http_request",
			Input: map[string]interface{}{
				"url":     "https://httpbin.org/ip",
				"method":  "GET",
				"timeout": 10,
			},
			Timestamp: time.Now(),
		},
	}

	fmt.Printf("  发送 %d 个并行请求...\n", len(calls))
	startTime := time.Now()
	results, err := tb.ExecuteBatch(ctx, calls)
	totalDuration := time.Since(startTime)

	if err != nil {
		fmt.Printf("  ✗ 批量执行有错误: %v\n", err)
	}

	fmt.Printf("  总耗时: %v\n", totalDuration)
	for i, result := range results {
		if result != nil && result.Result != nil {
			data := result.Result.Data.(map[string]interface{})
			fmt.Printf("  请求 %d: 状态码 %v, 耗时 %v\n",
				i+1, data["status_code"], result.Result.Duration)
		}
	}
	fmt.Println()

	// 示例 3: 条件分支执行
	fmt.Println("=== 示例 3: 条件分支执行 ===")
	fmt.Println("场景: 根据配置决定执行不同的工具")

	// 根据 debug 配置决定执行逻辑
	settings := parsedData["settings"].(map[string]interface{})
	debugMode := settings["debug"].(bool)

	if debugMode {
		fmt.Println("  调试模式已启用，执行详细日志记录")

		// 创建调试日志文件
		debugLogFile := "/tmp/mcp_debug.log"
		debugContent := fmt.Sprintf("[%s] Debug mode activated\nConfig: %v\n",
			time.Now().Format(time.RFC3339), parsedData)

		writeResult, err := executor.Execute(ctx, "write_debug_log", "write_file", map[string]interface{}{
			"path":    debugLogFile,
			"content": debugContent,
		})
		if err != nil {
			fmt.Printf("    ✗ 写入调试日志失败: %v\n", err)
		} else {
			fmt.Printf("    ✓ 调试日志已写入: %s, 耗时: %v\n",
				debugLogFile, writeResult.Result.Duration)
		}
	} else {
		fmt.Println("  调试模式已禁用，跳过详细日志记录")
	}
	fmt.Println()

	// 示例 4: 错误处理和重试
	fmt.Println("=== 示例 4: 错误处理和重试 ===")
	fmt.Println("场景: 读取不存在的文件，处理错误")

	nonExistentFile := "/tmp/non_existent_file.txt"
	maxRetries := 3

	for attempt := 1; attempt <= maxRetries; attempt++ {
		fmt.Printf("  尝试 %d/%d: 读取 %s\n", attempt, maxRetries, nonExistentFile)

		_, err := executor.Execute(ctx, "read_missing", "read_file", map[string]interface{}{
			"path": nonExistentFile,
		})

		if err == nil {
			fmt.Println("    ✓ 成功")
			break
		}

		fmt.Printf("    ✗ 失败: %v\n", err)

		if attempt < maxRetries {
			fmt.Printf("    等待 1 秒后重试...\n")
			time.Sleep(1 * time.Second)
		} else {
			fmt.Printf("    已达到最大重试次数，放弃操作\n")
		}
	}
	fmt.Println()

	// 示例 5: 数据转换链
	fmt.Println("=== 示例 5: 数据转换链 ===")
	fmt.Println("场景: 获取 API 数据 -> 解析 -> 提取字段")

	// 步骤 1: 获取 API 数据
	fmt.Println("  步骤 1: 获取 API 数据")
	apiResult, err := executor.Execute(ctx, "fetch_api", "http_request", map[string]interface{}{
		"url":     "https://httpbin.org/json",
		"method":  "GET",
		"timeout": 10,
	})
	if err != nil {
		fmt.Printf("    ✗ 失败: %v\n", err)
	} else {
		fmt.Printf("    ✓ 成功, 耗时: %v\n", apiResult.Result.Duration)
	}

	// 步骤 2: 解析响应
	if apiData, ok := executor.GetContext("fetch_api"); ok {
		responseData := apiData.(map[string]interface{})
		if jsonData, ok := responseData["json"]; ok && jsonData != nil {
			fmt.Println("  步骤 2: 解析响应数据")
			fmt.Printf("    ✓ JSON 数据: %v\n", jsonData)
		}
	}
	fmt.Println()

	// 打印执行摘要
	fmt.Println("=== 执行摘要 ===")
	allResults := executor.GetResults()
	successCount := 0
	failCount := 0
	var totalTime time.Duration

	for _, r := range allResults {
		if r.Result.Success {
			successCount++
		} else {
			failCount++
		}
		totalTime += r.Result.Duration
	}

	fmt.Printf("  总执行次数: %d\n", len(allResults))
	fmt.Printf("  成功: %d\n", successCount)
	fmt.Printf("  失败: %d\n", failCount)
	fmt.Printf("  总耗时: %v\n", totalTime)
	fmt.Println()

	// 清理
	fmt.Println("=== 清理 ===")
	filesToClean := []string{
		configFile,
		"/tmp/mcp_debug.log",
	}
	for _, f := range filesToClean {
		if err := os.Remove(f); err == nil {
			fmt.Printf("  ✓ 已删除: %s\n", f)
		}
	}

	fmt.Println("\n=== 工具链编排示例完成 ===")
}
