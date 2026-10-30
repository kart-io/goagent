# Shell Tool

## 概述

Shell Tool 是一个安全的命令执行工具，提供受控的 shell 命令执行能力。通过命令白名单、危险字符过滤和超时控制等多层安全机制，确保命令执行的安全性。

**核心特性：**

- 🔒 命令白名单机制 - 只允许执行预定义的安全命令
- 🛡️ 危险字符过滤 - 自动检测和阻止危险的 shell 操作符
- ⏱️ 超时控制 - 防止命令长时间运行
- 📁 工作目录隔离 - 支持指定命令执行的工作目录
- 🏗️ Builder 模式 - 提供灵活的配置方式

## 安全特性

### 1. 命令白名单

只有明确添加到白名单的命令才能被执行，有效防止命令注入攻击。

```go
// 只允许执行 ls 和 pwd 命令
tool := shell.NewShellTool([]string{"ls", "pwd"}, 30*time.Second)
```

### 2. 危险字符检测

自动检测并阻止包含以下危险字符的命令：

- `;` - 命令分隔符
- `|` - 管道操作符
- `&` - 后台执行
- `` ` `` - 命令替换
- `$` - 变量扩展
- `>` / `<` - 重定向操作符

### 3. 参数独立传递

命令参数通过数组独立传递，不经过 shell 解析，避免参数注入：

```go
// ✅ 安全：参数独立传递
tool.Invoke(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "command": "grep",
        "args": []interface{}{"pattern", "/path/to/file"},
    },
})

// ❌ 危险：字符串拼接（不会被执行）
// 这种方式会被危险字符检测拦截
```

## 使用示例

### 基本用法

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/tools/shell"
)

func main() {
    // 创建 Shell 工具，允许执行 ls 和 echo 命令
    tool := shell.NewShellTool(
        []string{"ls", "echo"},
        30*time.Second,
    )

    ctx := context.Background()

    // 执行 echo 命令
    output, err := tool.Invoke(ctx, &interfaces.ToolInput{
        Args: map[string]interface{}{
            "command": "echo",
            "args":    []interface{}{"Hello", "World"},
        },
    })

    if err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }

    if output.Success {
        result := output.Result.(map[string]interface{})
        fmt.Printf("Output: %s\n", result["output"])
        fmt.Printf("Exit code: %d\n", result["exit_code"])
    }
}
```

### 使用 Builder 模式

```go
// 使用 Builder 模式创建工具
tool := shell.NewShellToolBuilder().
    WithAllowedCommands("git", "npm", "go").
    WithTimeout(60 * time.Second).
    Build()

// 执行 git status
output, err := tool.Invoke(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "command": "git",
        "args":    []interface{}{"status"},
    },
})
```

### 指定工作目录

```go
output, err := tool.Invoke(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "command":  "ls",
        "args":     []interface{}{"-la"},
        "work_dir": "/tmp",
    },
})
```

### 自定义超时

```go
// 为单次命令设置超时（覆盖默认值）
output, err := tool.Invoke(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "command": "sleep",
        "args":    []interface{}{"5"},
        "timeout": 10, // 10 秒超时
    },
})
```

### 执行脚本

```go
// 注意：bash 必须在白名单中
tool := shell.NewShellTool([]string{"bash"}, 60*time.Second)

output, err := tool.ExecuteScript(ctx, "/path/to/script.sh", []string{"arg1", "arg2"})
```

### 执行命令管道

```go
// 注意：所有管道中的命令都必须在白名单中
tool := shell.NewShellTool(
    []string{"bash", "echo", "grep"},
    30*time.Second,
)

output, err := tool.ExecutePipeline(ctx, []string{
    "echo hello world",
    "grep hello",
})
```

### 使用预定义的常用工具

```go
// 获取预定义的常用工具集合
tools := shell.CommonShellTools()

// tools 包含：
// - 基础命令工具：ls, pwd, echo, cat, grep, find
// - Git 工具：git
// - 网络工具：curl, wget, ping
// - 系统信息工具：uname, hostname, whoami, date
```

## API 参考

### NewShellTool

```go
func NewShellTool(allowedCommands []string, timeout time.Duration) *ShellTool
```

创建一个新的 Shell 工具实例。

**参数：**

- `allowedCommands` - 允许执行的命令列表（白名单）
- `timeout` - 默认超时时间，0 表示使用默认值 30 秒

**返回：**

- `*ShellTool` - Shell 工具实例

### Invoke

```go
func (s *ShellTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error)
```

执行 shell 命令。

**输入参数（input.Args）：**

- `command` (string, 必需) - 要执行的命令名称
- `args` ([]interface{}, 可选) - 命令参数列表
- `work_dir` (string, 可选) - 工作目录
- `timeout` (int, 可选) - 超时时间（秒）

**输出（output.Result）：**

```go
{
    "command":   "ls",              // 执行的命令
    "args":      ["-la"],           // 命令参数
    "output":    "total 8\n...",    // 命令输出（stdout + stderr）
    "exit_code": 0,                 // 退出码
    "duration":  "123ms"            // 执行时长
}
```

### GetAllowedCommands

```go
func (s *ShellTool) GetAllowedCommands() []string
```

返回当前工具允许的所有命令列表。

### IsCommandAllowed

```go
func (s *ShellTool) IsCommandAllowed(command string) bool
```

检查指定命令是否在白名单中。

### ExecuteScript

```go
func (s *ShellTool) ExecuteScript(ctx context.Context, scriptPath string, args []string) (*interfaces.ToolOutput, error)
```

执行脚本文件的便捷方法。

**注意：** 需要 `bash` 或 `sh` 命令在白名单中。

### ExecutePipeline

```go
func (s *ShellTool) ExecutePipeline(ctx context.Context, commands []string) (*interfaces.ToolOutput, error)
```

执行命令管道的便捷方法。

**注意：** 管道中的所有命令都必须在白名单中。

### ShellToolBuilder

```go
type ShellToolBuilder struct { /* ... */ }

func NewShellToolBuilder() *ShellToolBuilder
func (b *ShellToolBuilder) WithAllowedCommands(commands ...string) *ShellToolBuilder
func (b *ShellToolBuilder) WithTimeout(timeout time.Duration) *ShellToolBuilder
func (b *ShellToolBuilder) Build() *ShellTool
```

Builder 模式用于灵活配置 Shell 工具。

## 安全最佳实践

### 1. 最小权限原则

只添加必需的命令到白名单，避免添加危险命令：

```go
// ✅ 推荐：只添加必需的命令
tool := shell.NewShellTool([]string{"ls", "cat"}, 30*time.Second)

// ❌ 危险：不要添加危险命令
// 避免：rm, dd, mkfs, chmod, chown 等
```

### 2. 输入验证

始终验证所有用户输入，即使有白名单保护：

```go
// 验证命令参数
func validatePath(path string) error {
    if strings.Contains(path, "..") {
        return errors.New("path traversal detected")
    }
    if !filepath.IsAbs(path) {
        return errors.New("only absolute paths allowed")
    }
    return nil
}
```

### 3. 使用超时

始终设置合理的超时时间，防止命令长时间运行：

```go
// 为不同类型的命令设置不同的超时
quickTool := shell.NewShellTool([]string{"ls", "pwd"}, 5*time.Second)
slowTool := shell.NewShellTool([]string{"npm", "go"}, 5*time.Minute)
```

### 4. 审计日志

记录所有命令执行，便于安全审计：

```go
// 在生产环境记录命令执行
logger.Info("executing shell command",
    "command", command,
    "args", args,
    "user", userID,
    "timestamp", time.Now(),
)
```

### 5. 错误处理

正确处理命令执行错误，不要泄露敏感信息：

```go
if err != nil {
    // ✅ 推荐：记录详细错误，返回通用消息
    logger.Error("command execution failed", "error", err, "command", cmd)
    return fmt.Errorf("command execution failed")
}

// ❌ 危险：不要将内部错误直接返回给用户
// return fmt.Errorf("failed: %v", err)
```

### 6. 工作目录隔离

使用工作目录限制命令执行范围：

```go
// 限制命令只能在特定目录执行
output, err := tool.Invoke(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "command":  "ls",
        "work_dir": "/app/data/user123", // 用户隔离目录
    },
})
```

## 常见问题

### Q1: 为什么我的命令被拒绝执行？

**A:** 可能的原因：

1. 命令不在白名单中
2. 命令包含危险字符（`;`, `|`, `&` 等）
3. 命令参数格式不正确

检查方法：

```go
if !tool.IsCommandAllowed("mycommand") {
    fmt.Println("Command not in whitelist")
    fmt.Println("Allowed commands:", tool.GetAllowedCommands())
}
```

### Q2: 如何执行需要 sudo 的命令？

**A:** 出于安全考虑，不建议在应用中使用 sudo。推荐的替代方案：

1. 使用具有足够权限的服务账号运行应用
2. 使用 Linux capabilities 而不是 sudo
3. 将需要提权的操作放在单独的服务中

### Q3: ExecutePipeline 和直接使用管道有什么区别？

**A:** `ExecutePipeline` 会验证管道中的每个命令是否都在白名单中：

```go
// 这会验证 echo 和 grep 都在白名单中
tool.ExecutePipeline(ctx, []string{"echo hello", "grep hello"})

// 这会被危险字符检测拦截
tool.Invoke(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "command": "echo hello | grep hello", // ❌ 包含管道字符
    },
})
```

### Q4: 如何处理命令输出过大的情况？

**A:** 可以通过以下方式控制输出：

```go
// 1. 使用超时限制执行时间
tool := shell.NewShellTool(commands, 5*time.Second)

// 2. 使用命令参数限制输出
output, err := tool.Invoke(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "command": "ls",
        "args":    []interface{}{"-1"}, // 每行一个文件
    },
})

// 3. 在应用层截断输出
if len(outputStr) > maxLength {
    outputStr = outputStr[:maxLength] + "... (truncated)"
}
```

### Q5: 命令执行失败时如何调试？

**A:** 检查以下信息：

```go
if err != nil || !output.Success {
    result := output.Result.(map[string]interface{})

    fmt.Printf("Command: %s\n", result["command"])
    fmt.Printf("Args: %v\n", result["args"])
    fmt.Printf("Exit code: %d\n", result["exit_code"])
    fmt.Printf("Output: %s\n", result["output"])
    fmt.Printf("Error: %s\n", output.Error)

    if output.Metadata != nil {
        fmt.Printf("Work dir: %v\n", output.Metadata["work_dir"])
        fmt.Printf("Timeout: %v\n", output.Metadata["timeout"])
    }
}
```

## 相关文档

- [GoAgent 工具系统](../README.md)
- [错误处理指南](../../errors/README.md)

## 贡献

如果你发现安全问题或有改进建议，请：

1. 通过 GitHub Issues 报告问题
2. 提交 Pull Request
3. 参与代码审查

**安全问题请私下报告，不要公开披露。**
