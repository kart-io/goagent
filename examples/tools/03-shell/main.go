// Package main 演示 Shell 工具的使用方法
// 本示例展示 ShellTool 的基本用法，包括白名单机制和安全执行
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools/shell"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║              Shell 工具 (ShellTool) 示例                       ║")
	fmt.Println("║   展示安全的 Shell 命令执行，包括白名单机制                     ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. 创建 Shell 工具（带白名单）
	fmt.Println("【步骤 1】创建 Shell 工具（带白名单）")
	fmt.Println("────────────────────────────────────────")

	// 方式 1：直接创建
	shellTool := shell.NewShellTool(
		[]string{"ls", "pwd", "echo", "cat", "date", "whoami", "uname"},
		30*time.Second,
	)

	fmt.Printf("工具名称: %s\n", shellTool.Name())
	fmt.Printf("工具描述: %s\n", shellTool.Description())
	fmt.Printf("允许的命令: %v\n", shellTool.GetAllowedCommands())
	fmt.Println()

	// 方式 2：使用 Builder 模式
	builderShellTool := shell.NewShellToolBuilder().
		WithAllowedCommands("ls", "pwd", "echo", "date").
		WithTimeout(30 * time.Second).
		Build()

	fmt.Println("✓ 使用 Builder 创建工具成功")
	fmt.Printf("允许的命令: %v\n", builderShellTool.GetAllowedCommands())
	fmt.Println()

	// 2. 执行基本命令
	fmt.Println("【步骤 2】执行基本命令")
	fmt.Println("────────────────────────────────────────")

	commands := []struct {
		cmd  string
		args []interface{}
		desc string
	}{
		{"pwd", nil, "显示当前目录"},
		{"date", nil, "显示当前日期时间"},
		{"whoami", nil, "显示当前用户"},
		{"uname", []interface{}{"-a"}, "显示系统信息"},
		{"echo", []interface{}{"Hello", "from", "GoAgent!"}, "输出文本"},
	}

	for _, c := range commands {
		fmt.Printf("\n执行: %s %v\n", c.cmd, c.args)

		args := map[string]interface{}{
			"command": c.cmd,
		}
		if c.args != nil {
			args["args"] = c.args
		}

		output, err := shellTool.Invoke(ctx, &interfaces.ToolInput{
			Args:    args,
			Context: ctx,
		})
		if err != nil {
			fmt.Printf("✗ 执行失败: %v\n", err)
			continue
		}

		if output.Success {
			fmt.Printf("✓ %s 执行成功\n", c.desc)
			if result, ok := output.Result.(map[string]interface{}); ok {
				fmt.Printf("  输出: %v\n", result["output"])
				fmt.Printf("  退出码: %v\n", result["exit_code"])
				fmt.Printf("  耗时: %v\n", result["duration"])
			}
		} else {
			fmt.Printf("✗ 执行失败: %s\n", output.Error)
		}
	}
	fmt.Println()

	// 3. 列出目录内容
	fmt.Println("【步骤 3】列出目录内容")
	fmt.Println("────────────────────────────────────────")

	lsOutput, err := shellTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"command": "ls",
			"args":    []interface{}{"-la"},
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ ls 执行失败: %v\n", err)
	} else if lsOutput.Success {
		fmt.Println("✓ ls -la 执行成功")
		if result, ok := lsOutput.Result.(map[string]interface{}); ok {
			outputStr := result["output"].(string)
			// 只显示前 500 字符
			if len(outputStr) > 500 {
				outputStr = outputStr[:500] + "\n... (截断)"
			}
			fmt.Printf("输出:\n%s\n", outputStr)
		}
	}
	fmt.Println()

	// 4. 安全检查 - 白名单测试
	fmt.Println("【步骤 4】安全检查 - 白名单测试")
	fmt.Println("────────────────────────────────────────")

	blockedCommands := []string{"rm", "sudo", "chmod", "wget"}
	for _, cmd := range blockedCommands {
		output, err := shellTool.Invoke(ctx, &interfaces.ToolInput{
			Args: map[string]interface{}{
				"command": cmd,
			},
			Context: ctx,
		})

		if err != nil {
			fmt.Printf("✓ 命令 '%s' 被正确拦截: %v\n", cmd, err)
		} else if !output.Success {
			fmt.Printf("✓ 命令 '%s' 被正确拦截: %s\n", cmd, output.Error)
		} else {
			fmt.Printf("✗ 命令 '%s' 应该被拦截但执行成功了\n", cmd)
		}
	}
	fmt.Println()

	// 5. 使用命令检查方法
	fmt.Println("【步骤 5】检查命令是否允许")
	fmt.Println("────────────────────────────────────────")

	checkCommands := []string{"ls", "rm", "echo", "sudo", "cat", "wget"}
	for _, cmd := range checkCommands {
		allowed := shellTool.IsCommandAllowed(cmd)
		if allowed {
			fmt.Printf("  ✓ '%s' - 允许\n", cmd)
		} else {
			fmt.Printf("  ✗ '%s' - 不允许\n", cmd)
		}
	}
	fmt.Println()

	// 6. 使用常用工具集
	fmt.Println("【步骤 6】使用预定义的常用工具集")
	fmt.Println("────────────────────────────────────────")

	commonTools := shell.CommonShellTools()
	for i, tool := range commonTools {
		fmt.Printf("工具集 %d: 允许命令 %v\n", i+1, tool.GetAllowedCommands())
	}
	fmt.Println()

	// 7. 指定工作目录
	fmt.Println("【步骤 7】指定工作目录")
	fmt.Println("────────────────────────────────────────")

	workDirOutput, err := shellTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"command":  "ls",
			"args":     []interface{}{"-la"},
			"work_dir": "/tmp",
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✗ 在 /tmp 执行 ls 失败: %v\n", err)
	} else if workDirOutput.Success {
		fmt.Println("✓ 在 /tmp 目录执行 ls 成功")
	}
	fmt.Println()

	// 8. 设置超时
	fmt.Println("【步骤 8】超时控制")
	fmt.Println("────────────────────────────────────────")

	// 创建一个有 sleep 权限的工具（仅用于演示超时）
	sleepTool := shell.NewShellTool([]string{"sleep"}, 5*time.Second)

	timeoutOutput, err := sleepTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"command": "sleep",
			"args":    []interface{}{"10"}, // 睡眠 10 秒
			"timeout": 2.0,                 // 超时 2 秒
		},
		Context: ctx,
	})

	if err != nil {
		fmt.Printf("✓ 正确捕获超时: %v\n", err)
	} else if !timeoutOutput.Success {
		fmt.Printf("✓ 正确返回超时失败: %s\n", timeoutOutput.Error)
	} else {
		fmt.Println("✗ 预期应该超时，但命令执行成功了")
	}
	fmt.Println()

	// 总结
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("本示例演示了 Shell 工具的核心功能:")
	fmt.Println("  ✓ 创建 Shell 工具（直接创建和 Builder 模式）")
	fmt.Println("  ✓ 命令白名单安全机制")
	fmt.Println("  ✓ 执行基本命令（pwd、date、echo 等）")
	fmt.Println("  ✓ 带参数的命令执行")
	fmt.Println("  ✓ 指定工作目录")
	fmt.Println("  ✓ 超时控制")
	fmt.Println("  ✓ 预定义常用工具集")
	fmt.Println()
	fmt.Println("⚠️  安全提示:")
	fmt.Println("  - 始终使用白名单限制允许的命令")
	fmt.Println("  - 不要在白名单中包含危险命令（rm、sudo 等）")
	fmt.Println("  - 设置合理的超时时间")
	fmt.Println()
	fmt.Println("更多工具示例请参考其他目录")
}
