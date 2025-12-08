// Package main 演示 HTTP API 工具的使用方法
// 本示例展示 APITool 的基本用法，包括 GET、POST 请求和 Builder 模式
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools/http"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║              HTTP API 工具 (APITool) 示例                      ║")
	fmt.Println("║   展示 HTTP 请求工具的使用方法（GET、POST 等）                  ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. 使用 Builder 创建 API 工具
	fmt.Println("【步骤 1】创建 API 工具")
	fmt.Println("────────────────────────────────────────")

	// 方式 1：直接创建
	apiTool := http.NewAPITool("", 30*time.Second, map[string]string{
		"User-Agent": "GoAgent-Example/1.0",
	})
	fmt.Printf("工具名称: %s\n", apiTool.Name())
	fmt.Printf("工具描述: %s\n", apiTool.Description())
	fmt.Println()

	// 方式 2：使用 Builder 模式
	builderApiTool := http.NewAPIToolBuilder().
		WithTimeout(30*time.Second).
		WithHeader("Accept", "application/json").
		WithHeader("User-Agent", "GoAgent-Example/1.0").
		Build()

	fmt.Println("✓ 使用 Builder 创建工具成功")
	fmt.Printf("工具名称: %s\n", builderApiTool.Name())
	fmt.Println()

	// 2. GET 请求示例
	fmt.Println("【步骤 2】GET 请求示例")
	fmt.Println("────────────────────────────────────────")

	// 使用 Invoke 方法
	output, err := apiTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"method": "GET",
			"url":    "https://httpbin.org/get",
			"headers": map[string]interface{}{
				"X-Custom-Header": "test-value",
			},
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ GET 请求失败: %v\n", err)
	} else if output.Success {
		fmt.Println("✓ GET 请求成功")
		if result, ok := output.Result.(map[string]interface{}); ok {
			fmt.Printf("  状态码: %v\n", result["status_code"])
			fmt.Printf("  状态: %v\n", result["status"])
			fmt.Printf("  耗时: %v\n", result["duration"])
		}
	} else {
		fmt.Printf("✗ GET 请求失败: %s\n", output.Error)
	}
	fmt.Println()

	// 使用便捷方法
	fmt.Println("使用便捷方法 Get():")
	getOutput, err := apiTool.Get(ctx, "https://httpbin.org/get?name=goagent", nil)
	if err != nil {
		fmt.Printf("✗ 便捷 GET 请求失败: %v\n", err)
	} else if getOutput.Success {
		fmt.Println("✓ 便捷 GET 请求成功")
	}
	fmt.Println()

	// 3. POST 请求示例
	fmt.Println("【步骤 3】POST 请求示例")
	fmt.Println("────────────────────────────────────────")

	postOutput, err := apiTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"method": "POST",
			"url":    "https://httpbin.org/post",
			"body": map[string]interface{}{
				"name":    "GoAgent",
				"version": "1.0.0",
				"features": []string{
					"multi-agent",
					"tool-calling",
					"streaming",
				},
			},
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ POST 请求失败: %v\n", err)
	} else if postOutput.Success {
		fmt.Println("✓ POST 请求成功")
		if result, ok := postOutput.Result.(map[string]interface{}); ok {
			fmt.Printf("  状态码: %v\n", result["status_code"])
			fmt.Printf("  耗时: %v\n", result["duration"])
		}
	} else {
		fmt.Printf("✗ POST 请求失败: %s\n", postOutput.Error)
	}
	fmt.Println()

	// 使用便捷方法
	fmt.Println("使用便捷方法 Post():")
	postBody := map[string]interface{}{
		"message": "Hello from GoAgent!",
	}
	postConvOutput, err := apiTool.Post(ctx, "https://httpbin.org/post", postBody, nil)
	if err != nil {
		fmt.Printf("✗ 便捷 POST 请求失败: %v\n", err)
	} else if postConvOutput.Success {
		fmt.Println("✓ 便捷 POST 请求成功")
	}
	fmt.Println()

	// 4. PUT 请求示例
	fmt.Println("【步骤 4】PUT 请求示例")
	fmt.Println("────────────────────────────────────────")

	putOutput, err := apiTool.Put(ctx, "https://httpbin.org/put",
		map[string]interface{}{
			"id":     123,
			"status": "updated",
		}, nil)

	if err != nil {
		fmt.Printf("✗ PUT 请求失败: %v\n", err)
	} else if putOutput.Success {
		fmt.Println("✓ PUT 请求成功")
	}
	fmt.Println()

	// 5. DELETE 请求示例
	fmt.Println("【步骤 5】DELETE 请求示例")
	fmt.Println("────────────────────────────────────────")

	deleteOutput, err := apiTool.Delete(ctx, "https://httpbin.org/delete", nil)
	if err != nil {
		fmt.Printf("✗ DELETE 请求失败: %v\n", err)
	} else if deleteOutput.Success {
		fmt.Println("✓ DELETE 请求成功")
	}
	fmt.Println()

	// 6. 带认证的 API 工具
	fmt.Println("【步骤 6】带认证的 API 工具")
	fmt.Println("────────────────────────────────────────")

	authApiTool := http.NewAPIToolBuilder().
		WithBaseURL("https://api.example.com").
		WithTimeout(30*time.Second).
		WithAuth("your-api-token-here").
		WithHeader("X-API-Version", "v1").
		Build()

	fmt.Println("✓ 创建带认证的 API 工具成功")
	fmt.Printf("工具名称: %s\n", authApiTool.Name())
	fmt.Println("（注：示例中使用的是模拟 token，实际使用时请替换为真实 token）")
	fmt.Println()

	// 7. 错误处理示例
	fmt.Println("【步骤 7】错误处理示例")
	fmt.Println("────────────────────────────────────────")

	// 测试 404 错误
	notFoundOutput, err := apiTool.Get(ctx, "https://httpbin.org/status/404", nil)
	if err != nil {
		fmt.Printf("✓ 正确捕获 404 错误: %v\n", err)
	} else if !notFoundOutput.Success {
		fmt.Printf("✓ 正确返回 404 失败状态: %s\n", notFoundOutput.Error)
	}

	// 测试超时
	fmt.Println("测试超时（设置 1 秒超时，请求延迟 3 秒）:")
	timeoutOutput, err := apiTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"method":  "GET",
			"url":     "https://httpbin.org/delay/3",
			"timeout": 1.0, // 1 秒超时
		},
		Context: ctx,
	})
	if err != nil {
		fmt.Printf("✓ 正确捕获超时错误: %v\n", err)
	} else if !timeoutOutput.Success {
		fmt.Printf("✓ 正确返回超时失败: %s\n", timeoutOutput.Error)
	}
	fmt.Println()

	// 总结
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("本示例演示了 HTTP API 工具的核心功能:")
	fmt.Println("  ✓ 创建 API 工具（直接创建和 Builder 模式）")
	fmt.Println("  ✓ GET 请求（Invoke 方法和便捷方法）")
	fmt.Println("  ✓ POST 请求（发送 JSON 数据）")
	fmt.Println("  ✓ PUT/DELETE 请求")
	fmt.Println("  ✓ 带认证的 API 工具")
	fmt.Println("  ✓ 错误处理（404、超时）")
	fmt.Println()
	fmt.Println("更多工具示例请参考其他目录")
}
