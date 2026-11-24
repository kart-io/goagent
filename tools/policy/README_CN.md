# tools/policy 工具权限规则

[English](README.md) | [中文](README_CN.md)

按工具名判定一次工具调用是放行、拒绝还是交审批；带命令参数的调用还按命令前缀判定。策略作为调用前钩子接入 `tools.Pipeline`，适用于任何工具（编码工具、MCP 工具、HTTP 工具）。需要让 Dispatcher 直接执行一部分调用、拒绝一部分、其余交人工确认时使用它。

## 用法

```go
import (
    "github.com/kart-io/goagent/tools"
    "github.com/kart-io/goagent/tools/policy"
)

p := &policy.Policy{
    Rules: []policy.Rule{
        {Tool: "read_file", Decision: policy.Allow},
        {Tool: "run_command", Command: "go test", Decision: policy.Allow},
        {Tool: "run_command", Command: "rm", Decision: policy.Deny},
    },
    Default:      policy.Ask,
    SessionTools: []string{"write_file"},
}

dispatcher, err := tools.NewDispatcher(registry, tools.DispatchConfig{
    Pipeline: &tools.Pipeline{
        Pre:      []tools.PreHook{p.Hook()},
        Approver: approver, // 接收 PreAsk 的调用
    },
})
```

不经 Dispatcher 时，`p.Decide(call)` 直接返回判定结果。

## 主要 API

| 标识符 | 说明 |
|---|---|
| `Decision`：`Ask`、`Allow`、`Deny` | 规则判定；零值为 `Ask` |
| `Rule{Tool, Command, Decision}` | 一条规则；`Tool: "*"` 匹配所有工具，空 `Tool` 不匹配任何工具 |
| `Policy` | `Rules`、`Default`、`CommandArg`（默认 `"command"`）、`ArgsArg`（默认 `"args"`）、`SessionTools` |
| `(*Policy).Decide(call)` | 返回调用的判定 |
| `(*Policy).Hook()` | 把策略适配为 `tools.PreHook` |
| `CommandGrant`、`ToolGrant` | 审批时提供给审批方的"本会话允许"范围（`tools.SessionGrant`） |

## 行为说明

- 所有匹配规则中取最严格的判定（Deny > Ask > Allow），与规则顺序无关；没有规则匹配时才用 `Default`。追加规则只会收紧结果。
- `Hook` 把 `Allow` 映射为 `PreAllow`，`Deny` 映射为带反馈 `"<tool> is denied by policy"` 的 `PreDeny`，`Ask` 映射为 `PreAsk`。
- 带 `Command` 的规则只匹配含命令参数的调用。命令按空白分词；argv 风格的工具还会把 `ArgsArg` 数组的每个元素作为一个词接在后面。
- 放行规则要求首词完全相同，且不匹配含 shell 控制符、引号或反斜杠的命令。拒绝与询问规则比较首词的文件名（`/usr/bin/git push` 匹配 `"git push"`）；遇到无法判断的命令时至少判为 `Ask`。
- 放行规则放行该命令的全部参数。拒绝规则可被绕过（`env rm`、`sh -c`、脚本文件），不能用来做隔离；需要隔离时使用容器执行器。
- 判为 `Ask` 时，命令调用的会话授权收窄到首词，第二个词是纯字母子命令时一并包含（`go test`）；含 shell 控制符的命令不提供会话授权。列入 `SessionTools` 且无命令参数的工具可整工具授权；其他工具只能单次批准。

## 另见

- [工具中间件指南：Dispatcher 管线](../../docs/guides/TOOL_MIDDLEWARE_CN.md#dispatcher-管线)
- [Harness 迁移](../../docs/guides/HARNESS_MIGRATION_CN.md)
