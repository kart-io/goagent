# Shell Tool

`shell.ShellTool` 以命令白名单执行本地命令，工具名为 `shell`。命令经 `exec.CommandContext` 直接启动，参数独立传递、不经过 shell 解析。

## 行为

- **白名单**：`command` 必须与白名单中的某一项完全相同，否则返回校验错误，`Metadata["allowed_commands"]` 列出允许的命令。
- **字符检查**：`command` 中含 `;`、`|`、`&`、`` ` ``、`$`、`>`、`<` 时拒绝。只检查命令名，`args` 不经 shell 解析，因此不做该检查。
- **超时**：构造时的默认超时（0 表示 30 秒），可由单次调用的 `timeout` 覆盖。
- **输出**：stdout 与 stderr 合并。退出码非 0 或启动失败时返回执行错误，`Result` 仍带部分输出。

## 输入与输出

`input.Args`：

| 参数 | 类型 | 说明 |
|---|---|---|
| `command` | string，必需 | 命令名 |
| `args` | `[]interface{}`，可选 | 参数，逐个以 `fmt.Sprint` 转为字符串；其他切片类型（如 `[]string`）会被忽略 |
| `work_dir` | string，可选 | 工作目录 |
| `timeout` | float64，可选 | 超时秒数；JSON 解码得到的数字即为 float64，Go 中直接传 `int` 会被忽略 |

`output.Result` 是 `map[string]interface{}`：`command`、`args`（`[]string`）、`output`、`exit_code`、`duration`。成功时 `output.Metadata` 含 `work_dir` 与 `timeout`。

## 使用

```go
tool := shell.NewShellTool([]string{"ls", "echo"}, 30*time.Second)
// 或 shell.NewShellToolBuilder().WithAllowedCommands("git", "go").WithTimeout(time.Minute).Build()

output, err := tool.Invoke(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "command":  "ls",
        "args":     []interface{}{"-la"},
        "work_dir": "/tmp",
        "timeout":  10.0,
    },
})
if err != nil {
    return err
}
result := output.Result.(map[string]interface{})
fmt.Println(result["output"], result["exit_code"])
```

`GetAllowedCommands()` 返回白名单，`IsCommandAllowed(cmd)` 检查单个命令。`CommonShellTools()` 返回四个预设工具：`ls pwd echo cat grep find`（30 秒）、`git`（60 秒）、`curl wget ping`（60 秒）、`uname hostname whoami date`（10 秒）。

## ExecuteScript 与 ExecutePipeline

- `ExecuteScript(ctx, scriptPath, args)`：要求白名单含 `bash` 或 `sh`，以 `bash` 执行。
- `ExecutePipeline(ctx, commands)`：检查每段命令的首个单词都在白名单中，再以 `bash -c "<cmd1> | <cmd2> ..."` 执行，因此白名单还需包含 `bash`。

两者当前以 `[]string` 传递 `args`，而 `Invoke` 只解析 `[]interface{}`，参数被丢弃，实际运行的是不带参数的 `bash`（stdin 为空时立即以 0 退出、无输出）。在修复前不要依赖这两个方法。

## 相关文档

- [GoAgent 工具系统](../README.md)
- [tools/policy](../policy/)：按命令前缀裁决的 Dispatcher 权限规则
