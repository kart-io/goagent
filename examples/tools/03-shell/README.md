# 03-shell Shell 工具示例

演示 `tools/shell` 中的 `ShellTool`：只执行白名单内的命令。不调用 LLM，会在本机执行 `pwd`、`date`、`whoami`、`uname`、`echo`、`ls` 和 `sleep`。

## 运行

```bash
go run ./examples/tools/03-shell
```

## 内容

1. **创建工具**：`shell.NewShellTool([]string{"ls", "pwd", "echo", "cat", "date", "whoami", "uname"}, 30*time.Second)`，或 `shell.NewShellToolBuilder().WithAllowedCommands(...).WithTimeout(...).Build()`。
2. **执行命令**：参数 `command` 与 `args`，结果包含输出、退出码与耗时。
3. **列出目录**：`ls -la`，输出超长时截断。
4. **白名单拦截**：`rm`、`sudo`、`chmod`、`wget` 返回 `command not allowed`。
5. **`IsCommandAllowed`**：逐个检查命令是否在白名单中。
6. **`shell.CommonShellTools()`**：返回按用途分组的预置工具（基础命令、git、网络、系统信息）。
7. **工作目录**：参数 `work_dir: "/tmp"`。
8. **超时**：`sleep 10` 配合参数 `"timeout": 2.0`，命令被终止并返回错误。

```go
shellTool := shell.NewShellTool([]string{"ls", "pwd", "echo"}, 30*time.Second)
output, err := shellTool.Invoke(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "command":  "ls",
        "args":     []interface{}{"-la"},
        "work_dir": "/tmp",
    },
    Context: ctx,
})
```
