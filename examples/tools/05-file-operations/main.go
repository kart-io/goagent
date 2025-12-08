// Package main 演示文件操作工具的使用方法
// 本示例展示 FileOperationsTool 的基本用法
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools/practical"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          文件操作工具 (FileOperationsTool) 示例                ║")
	fmt.Println("║   展示文件读写、搜索、压缩等操作                               ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 创建临时目录用于测试
	tmpDir, err := os.MkdirTemp("", "goagent-file-example-*")
	if err != nil {
		fmt.Printf("✗ 创建临时目录失败: %v\n", err)
		return
	}
	defer func() { _ = os.RemoveAll(tmpDir) }() // 清理临时目录

	fmt.Printf("测试目录: %s\n\n", tmpDir)

	// 1. 创建文件操作工具
	fmt.Println("【步骤 1】创建文件操作工具")
	fmt.Println("────────────────────────────────────────")

	fileTool := practical.NewFileOperationsTool(tmpDir)
	fmt.Printf("工具名称: %s\n", fileTool.Name())
	fmt.Printf("工具描述: %s\n", fileTool.Description())
	fmt.Println()

	// 2. 写入文件
	fmt.Println("【步骤 2】写入文件")
	fmt.Println("────────────────────────────────────────")

	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := `这是一个测试文件。
GoAgent 是一个强大的 AI Agent 框架。
它支持多种工具和推理模式。

文件操作工具可以：
- 读取文件
- 写入文件
- 搜索文件
- 压缩文件
- 解析 JSON/YAML`

	_, err = fileTool.Execute(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "write",
			"path":      testFile,
			"content":   testContent,
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ 写入文件失败: %v\n", err)
	} else {
		fmt.Printf("✓ 写入文件成功: %s\n", testFile)
		fmt.Printf("  内容长度: %d 字节\n", len(testContent))
	}
	fmt.Println()

	// 3. 读取文件
	fmt.Println("【步骤 3】读取文件")
	fmt.Println("────────────────────────────────────────")

	readOutput, err := fileTool.Execute(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "read",
			"path":      testFile,
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ 读取文件失败: %v\n", err)
	} else {
		fmt.Println("✓ 读取文件成功")
		if result, ok := readOutput.Result.(map[string]interface{}); ok {
			if content, ok := result["result"].(string); ok {
				// 只显示前 200 字符
				if len(content) > 200 {
					content = content[:200] + "..."
				}
				fmt.Printf("  内容:\n%s\n", content)
			}
		}
	}
	fmt.Println()

	// 4. 追加内容
	fmt.Println("【步骤 4】追加内容")
	fmt.Println("────────────────────────────────────────")

	appendContent := "\n\n--- 追加的内容 ---\n这是追加的新内容。"
	appendOutput, err := fileTool.Execute(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "append",
			"path":      testFile,
			"content":   appendContent,
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ 追加内容失败: %v\n", err)
	} else {
		fmt.Println("✓ 追加内容成功")
		_ = appendOutput
	}
	fmt.Println()

	// 5. 获取文件信息
	fmt.Println("【步骤 5】获取文件信息")
	fmt.Println("────────────────────────────────────────")

	infoOutput, err := fileTool.Execute(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "info",
			"path":      testFile,
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ 获取文件信息失败: %v\n", err)
	} else {
		fmt.Println("✓ 获取文件信息成功")
		if result, ok := infoOutput.Result.(map[string]interface{}); ok {
			// 文件信息直接在 result 中
			if name, ok := result["name"]; ok {
				fmt.Printf("  文件名: %v\n", name)
			}
			if size, ok := result["size"]; ok {
				fmt.Printf("  大小: %v 字节\n", size)
			}
			if modified, ok := result["modified"]; ok {
				fmt.Printf("  修改时间: %v\n", modified)
			}
			if isDir, ok := result["is_dir"]; ok {
				fmt.Printf("  是否为目录: %v\n", isDir)
			}
		} else {
			fmt.Printf("  结果: %v\n", infoOutput.Result)
		}
	}
	fmt.Println()

	// 6. 创建 JSON 文件并解析
	fmt.Println("【步骤 6】JSON 文件操作")
	fmt.Println("────────────────────────────────────────")

	jsonFile := filepath.Join(tmpDir, "config.json")
	jsonContent := `{
  "name": "GoAgent",
  "version": "1.0.0",
  "features": ["multi-agent", "tool-calling", "streaming"],
  "settings": {
    "max_iterations": 10,
    "timeout": 30
  }
}`

	// 写入 JSON
	_, err = fileTool.Execute(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "write",
			"path":      jsonFile,
			"content":   jsonContent,
		},
		Context: ctx,
	})
	if err != nil {
		fmt.Printf("✗ 写入 JSON 失败: %v\n", err)
	} else {
		fmt.Printf("✓ 写入 JSON 成功: %s\n", jsonFile)
	}

	// 解析 JSON
	parseOutput, err := fileTool.Execute(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "parse",
			"path":      jsonFile,
			"options": map[string]interface{}{
				"format": "json",
			},
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ 解析 JSON 失败: %v\n", err)
	} else {
		fmt.Println("✓ 解析 JSON 成功")
		if result, ok := parseOutput.Result.(map[string]interface{}); ok {
			if data, ok := result["data"].(map[string]interface{}); ok {
				fmt.Printf("  name: %v\n", data["name"])
				fmt.Printf("  version: %v\n", data["version"])
				fmt.Printf("  features: %v\n", data["features"])
			}
		}
	}
	fmt.Println()

	// 7. 列出目录
	fmt.Println("【步骤 7】列出目录内容")
	fmt.Println("────────────────────────────────────────")

	// 创建一些额外的测试文件
	_ = os.WriteFile(filepath.Join(tmpDir, "readme.md"), []byte("# README"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "data.csv"), []byte("name,value\ntest,123"), 0644)
	_ = os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "subdir", "nested.txt"), []byte("nested file"), 0644)

	listOutput, err := fileTool.Execute(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "list",
			"path":      tmpDir,
			"options": map[string]interface{}{
				"recursive": false,
			},
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ 列出目录失败: %v\n", err)
	} else {
		fmt.Println("✓ 列出目录成功")
		if result, ok := listOutput.Result.(map[string]interface{}); ok {
			if files, ok := result["files"].([]interface{}); ok {
				fmt.Printf("  共 %d 个项目:\n", len(files))
				for _, f := range files {
					if fileInfo, ok := f.(map[string]interface{}); ok {
						name := fileInfo["name"]
						isDir := fileInfo["is_dir"]
						if isDir.(bool) {
							fmt.Printf("    📁 %v/\n", name)
						} else {
							fmt.Printf("    📄 %v\n", name)
						}
					}
				}
			}
		}
	}
	fmt.Println()

	// 8. 搜索文件
	fmt.Println("【步骤 8】搜索文件")
	fmt.Println("────────────────────────────────────────")

	searchOutput, err := fileTool.Execute(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "search",
			"path":      tmpDir,
			"pattern":   "*.txt",
			"options": map[string]interface{}{
				"recursive": true,
			},
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ 搜索文件失败: %v\n", err)
	} else {
		fmt.Println("✓ 搜索 *.txt 文件成功")
		if result, ok := searchOutput.Result.(map[string]interface{}); ok {
			if matches, ok := result["matches"].([]interface{}); ok {
				fmt.Printf("  找到 %d 个匹配:\n", len(matches))
				for _, m := range matches {
					fmt.Printf("    - %v\n", m)
				}
			}
		}
	}
	fmt.Println()

	// 9. 复制文件
	fmt.Println("【步骤 9】复制文件")
	fmt.Println("────────────────────────────────────────")

	copyDest := filepath.Join(tmpDir, "test_copy.txt")
	copyOutput, err := fileTool.Execute(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation":   "copy",
			"path":        testFile,
			"destination": copyDest,
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ 复制文件失败: %v\n", err)
	} else {
		fmt.Printf("✓ 复制文件成功: %s -> %s\n", testFile, copyDest)
		_ = copyOutput
	}
	fmt.Println()

	// 10. 压缩文件
	fmt.Println("【步骤 10】压缩文件")
	fmt.Println("────────────────────────────────────────")

	compressOutput, err := fileTool.Execute(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "compress",
			"path":      testFile,
			"options": map[string]interface{}{
				"compression": "gzip",
			},
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ 压缩文件失败: %v\n", err)
	} else {
		fmt.Println("✓ 压缩文件成功 (gzip)")
		if result, ok := compressOutput.Result.(map[string]interface{}); ok {
			// 压缩信息在 info 字段中
			if info, ok := result["info"].(map[string]interface{}); ok {
				fmt.Printf("  压缩文件: %v\n", info["destination"])
				fmt.Printf("  原始大小: %v\n", info["original_size"])
				fmt.Printf("  压缩大小: %v\n", info["compressed_size"])
			} else {
				fmt.Printf("  结果: %v\n", result["result"])
			}
		}
	}
	fmt.Println()

	// 11. 删除文件
	fmt.Println("【步骤 11】删除文件")
	fmt.Println("────────────────────────────────────────")

	deleteOutput, err := fileTool.Execute(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "delete",
			"path":      copyDest,
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ 删除文件失败: %v\n", err)
	} else {
		fmt.Printf("✓ 删除文件成功: %s\n", copyDest)
		_ = deleteOutput
	}
	fmt.Println()

	// 总结
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("本示例演示了文件操作工具的核心功能:")
	fmt.Println("  ✓ 读取文件 (read)")
	fmt.Println("  ✓ 写入文件 (write)")
	fmt.Println("  ✓ 追加内容 (append)")
	fmt.Println("  ✓ 获取文件信息 (info)")
	fmt.Println("  ✓ 列出目录 (list)")
	fmt.Println("  ✓ 搜索文件 (search)")
	fmt.Println("  ✓ 复制文件 (copy)")
	fmt.Println("  ✓ 压缩文件 (compress)")
	fmt.Println("  ✓ 解析 JSON/YAML (parse)")
	fmt.Println("  ✓ 删除文件 (delete)")
	fmt.Println()
	fmt.Println("⚠️  安全提示:")
	fmt.Println("  - 文件操作工具默认限制在指定的 basePath 内")
	fmt.Println("  - 禁止访问系统敏感目录 (/etc, /sys, /proc)")
	fmt.Println("  - 有文件大小限制（默认 100MB）")
	fmt.Println()
	fmt.Println("更多工具示例请参考其他目录")
}
