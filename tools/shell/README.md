# Shell Tool

`shell.ShellTool` 以命令白名单执行本地命令，工具名为 `shell`。命令经 `exec.CommandContext` 直接启动，参数独立传递、不经过 shell 解析。

## 行为

- **白名单**：`command` 必须与白名单中的某一项完全相同，否则返回校验错误，`Metadata["allowed_commands"]` 列出允许的命令。白名单不校验参数：`find -exec`、`git -c`、`curl -o`、`env`、`xargs`、解释器等可经参数执行任意程序，列入白名单即等于放行任意执行。需要按子命令收窄时用 [tools/policy](../policy/)，它把 `command` 与 `args` 连成词序列匹配规则。
- **字符检查**：`command` 中含 `;`、`|`、`&`、`` ` ``、`$`、`>`、`<`、换行时拒绝。只检查命令名，`args` 不经 shell 解析，因此不做该检查。
- **工作目录**：默认原样使用 `work_dir`。`NewShellToolBuilder().WithRootDir(dir)` 把它限制在 `dir` 内：未指定时在 `dir` 中执行，相对路径基于 `dir` 解析，经 `..` 或符号链接离开的被拒绝。它只约束工作目录，参数仍可引用任意绝对路径。
- **环境变量**：子进程只继承宿主的 `PATH`、`LANG`、`LC_ALL`、`TZ`、`TMPDIR`，不继承 API key 等其余变量（白名单里的 `cat` 可读 `/proc/self/environ`）。需要其他变量时用 `NewShellToolBuilder().WithEnv(names...)` 按名透传，列入的变量对模型可见。
- **超时**：构造时的超时（0 表示 30 秒）。单次调用的 `timeout` 只能缩短它，超过或非正时按构造时的值。
- **进程清理**：Unix 上命令在独立进程组运行，超时或取消会终止同组后代；父命令退出后最多再等 1 秒输出并清理后台进程。主动脱离进程组的进程不在此范围。其他平台沿用 `CommandContext` 的父进程取消，输出等待同样有界。
- **输出**：stdout 与 stderr 合并，最多保留 1 MiB，超出部分丢弃并置 `truncated`。退出码非 0 或启动失败时返回执行错误，`Result` 仍带部分输出。
- **错误链**：超时、取消保留 `context.DeadlineExceeded` / `context.Canceled`；非零退出仍可通过 `errors.As` 取得 `*exec.ExitError`。

## 输入与输出

`input.Args`：

| 参数 | 类型 | 说明 |
|---|---|---|
| `command` | string，必需 | 命令名 |
| `args` | `[]string` 或 `[]interface{}`，可选 | 参数；`[]interface{}`（JSON 解码结果）的元素逐个以 `fmt.Sprint` 转为字符串，其他类型被忽略 |
| `work_dir` | string，可选 | 工作目录 |
| `timeout` | float64 或 int，可选 | 超时秒数，float64 可含小数 |

`output.Result` 是 `map[string]interface{}`：`command`、`args`（`[]string`）、`output`、`exit_code`、`duration`、`truncated`。成功时 `output.Metadata` 含 `work_dir` 与 `timeout`。

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

`GetAllowedCommands()` 返回白名单，`IsCommandAllowed(cmd)` 检查单个命令。`CommonShellTools()` 返回两个预设工具：`ls pwd echo cat grep`（30 秒）、`uname hostname whoami date`（10 秒）。`find`、`git`、`curl`、`wget` 可经参数执行任意程序或写任意文件，不作预设。

## ExecuteScript 与 ExecutePipeline

- `ExecuteScript(ctx, scriptPath, args)`：要求白名单含 `bash` 或 `sh`，以 `bash` 执行。
- `ExecutePipeline(ctx, commands)`：检查每段命令的首个单词都在白名单中，再以 `bash -c "<cmd1> | <cmd2> ..."` 执行，因此白名单还需包含 `bash`。由于经 shell 解析，任一段命令含 `;`、`|`、`&`、`` ` ``、`$`、`>`、`<`、换行时拒绝。

## 相关文档

- [GoAgent 工具系统](../README.md)
- [tools/policy](../policy/)：按命令前缀裁决的 Dispatcher 权限规则
