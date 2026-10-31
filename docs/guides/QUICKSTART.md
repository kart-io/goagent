# GoAgent 快速入门

新应用统一使用 `agent.AsRunnable()`。同步和流式调用使用相同执行源，配置的工具、检索器、中间件及完成回调走同一路径。要求 Go 1.25+ 与实现 `llm.StreamClient` 的客户端；目前 OpenAI（含兼容端点）、DeepSeek、Gemini Provider 同时实现流式与原生工具流（`llm.ToolStreamClient`），其他 Provider 只能经旧的同步入口调用。

```sh
go get github.com/kart-io/goagent
export OPENAI_API_KEY=...
export OPENAI_MODEL=...
# 可选：export OPENAI_BASE_URL=https://api.deepseek.com
```

## 创建并调用 Agent

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/llm/providers"
)

func main() {
    client, err := providers.NewOpenAIWithOptions()
    if err != nil { log.Fatal(err) }
    agent, err := builder.NewSimpleBuilder(client).
        WithSystemPrompt("你是一个有帮助的助手。").
        WithMaxIterations(6).
        WithTimeout(30 * time.Second).
        Build()
    if err != nil { log.Fatal(err) }
    output, err := agent.AsRunnable().Invoke(context.Background(), "你好，请介绍一下自己。")
    if err != nil { log.Fatal(err) }
    fmt.Println(output.Result)
}
```

`Invoke` 收集真实流的最终结果，不额外调用一次同步模型。普通回答只需一次请求；典型工具任务需要一次选择工具、一次回答，MaxIterations 包含最终回答。

## 工具、RAG 与记忆

```go
agent, err := builder.NewSimpleBuilder(client).
    WithTools(lookupTool).
    WithRetriever(retriever, builder.RetrievalConfig{MaxDocuments: 4, MaxContextRunes: 12000}).
    WithMaxIterations(8).
    Build()
// 检查 err 后，通过 agent.AsRunnable().Invoke(ctx, question) 调用。
```

工具实现 `interfaces.Tool`，提供 Name、Description、ArgsSchema 和 Invoke，函数可用 `tools.NewFunctionTool` 包装。检索器实现 `interfaces.DocumentRetriever`；BM25、向量、混合检索及重排序实现位于 goagent-contrib。

- 结果已完整可展示的单个终态工具，可显式配置 `WithReturnDirectTools("lookup")`；不会再调用模型总结或补充引用。
- `WithMemory(manager).WithSessionID(sessionID)` 启用会话记忆；`WithMaxConversationHistory` 限制加载历史数量。
- 长任务可用 `WithOffload` 把超长工具结果换成引用、经读取工具分页读回，见[上下文治理](CONTEXT_POLICY.md)；默认关闭。历史摘要由 goagent-harness 的 compaction 组件提供。
- 检索器实现见 [goagent-contrib/retrieval](https://github.com/kart-io/goagent-contrib/tree/master/retrieval)；[MCP 与可恢复步骤](PRIORITY_CAPABILITIES.md)展示外部工具及审批恢复。

## 流式调用

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel() // 提前停止消费时必须取消。
chunks, err := agent.AsRunnable().Stream(ctx, question)
if err != nil { return err }
for chunk := range chunks {
    if chunk.Error != nil { return chunk.Error }
    if chunk.Event != nil {
        fmt.Print(chunk.Event.Delta) // 暂态文本，可能含工具调用前说明。
    }
    if chunk.Done {
        fmt.Println(chunk.Data.Result) // 经过中间件和回调后的最终结果。
    }
}
if err := ctx.Err(); err != nil { return err }
```

事件还包含 sources、step、usage。usage 是累计快照，不要重复求和。一个 Builder 的记忆和状态属于同一会话；所有 Builder 执行入口会串行化该会话的执行，排队计入 Timeout，跨会话并发应创建独立 Builder。

## 旧接口迁移

| 原调用 | 新应用推荐 | 行为变化 |
|---|---|---|
| `agent.Execute(ctx, input)` | `agent.AsRunnable().Invoke(ctx, input)` | 新入口需要流式模型，并执行已配置工具 |
| `agent.ExecuteWithTools(ctx, input)` | `agent.AsRunnable().Invoke(ctx, input)` | 原生工具流与最终输出统一，预算包含最终回答 |
| `agent.ExecuteStream(ctx, input)` | `agent.AsRunnable().Stream(ctx, input)` | 保留同一事件源，增加 Runnable 的组合能力 |

旧 Execute 保留同步语义，不执行工具；配置 `WithOffload` 或 `WithReturnDirectTools` 时直接报错，持久执行与根预算也只支持 AsRunnable。ExecuteWithTools 在支持 StreamClient 时委派统一管线，非流式客户端仍保留旧循环。`Pipe` 等待上游最终结果后再调用下游；复杂分支、并行汇合和有界循环使用 [workflow.Graph](../../workflow/README.md)，通过 Adapt 接入现有 Agent。详细边界见[执行契约](EXECUTION_CONTRACT.md)。

## 迁移检查与持续验证

```sh
# 列出指定目录里的迁移候选，不自动改代码。
go run ./scripts/apicheck -mode migration -root ./examples/my-app
# 检查真实 Go import 的层级约束。
bash scripts/verify_imports.sh
```

迁移检查基于 Go AST：识别旧 `interfaces.Agent/Runnable`（支持导入别名），并在导入 Builder 的文件中标记 `Execute/ExecuteWithTools` 选择器。它不做接收者类型推导、跨文件数据流或包装函数分析，因此是候选清单，可能误报或漏报。默认只报告；选定已迁移目录可加 `-strict` 作为门禁。不要据此批量替换：逐项确认 Provider 的 StreamClient/ToolStreamClient、自动工具执行、预算和回调语义。

[Provider 持续回归](PROVIDER_REGRESSION.md)提供离线门禁与有界真实测试。退款应用可复用 [refund.Service](../../workflow/refund/README.md)；串行图恢复使用 [CompileDurable](../../workflow/README.md#节点边界的持久化恢复)。

复杂业务可以完全采用 [GoAgent 原生编排与工具体系](NATIVE_ORCHESTRATION.md)：Dispatcher 同时服务图节点与 Builder，持久图 AsRunnable 提供统一调用、步骤事件和审批恢复。无需引入其他编排框架。
