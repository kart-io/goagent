# GoAgent 快速入门

新应用统一使用 `agent.AsRunnable()`。同步和流式调用使用相同执行源，配置的工具、检索器、中间件及完成回调走同一路径。要求 Go 1.25+，以及支持流式补全的模型；使用工具时建议 OpenAI 兼容 Provider 的原生工具流。

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

工具实现 `interfaces.Tool`，提供 Name、Description、ArgsSchema 和 Invoke。检索器实现 `interfaces.DocumentRetriever`，也可直接使用 retrieval 包的 BM25、向量、混合检索及重排序。

- 结果已完整可展示的单个终态工具，可显式配置 `WithReturnDirectTools("lookup")`；不会再调用模型总结或补充引用。
- `WithMemory(manager).WithSessionID(sessionID)` 启用会话记忆；`WithMaxConversationHistory` 限制加载历史数量。
- 长任务可显式启用 [ContextPolicy](CONTEXT_POLICY.md)，卸载较早的大工具结果并选择历史摘要模型。默认关闭，摘要请求也消耗预算。
- [可运行的 RAG 示例](../../examples/rag/grounded/main.go)展示来源、检索和原生增量输出；[MCP 与可恢复步骤](PRIORITY_CAPABILITIES.md)展示外部工具及审批恢复。

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

事件还包含 sources、step、usage。usage 是累计快照，不要重复求和。一个 Builder 的记忆和状态属于同一会话；AsRunnable 会串行化该会话的执行，跨会话并发应创建独立 Builder。

## 旧接口迁移

| 原调用 | 新应用推荐 | 行为变化 |
|---|---|---|
| `agent.Execute(ctx, input)` | `agent.AsRunnable().Invoke(ctx, input)` | 新入口需要流式模型，并执行已配置工具 |
| `agent.ExecuteWithTools(ctx, input)` | `agent.AsRunnable().Invoke(ctx, input)` | 原生工具流与最终输出统一，预算包含最终回答 |
| `agent.ExecuteStream(ctx, input)` | `agent.AsRunnable().Stream(ctx, input)` | 保留同一事件源，增加 Runnable 的组合能力 |

旧方法保留兼容语义，不自动迁移。ReAct 的步骤流和其他推理策略不等同于 Builder 的 Token 流。`Pipe` 等待上游最终结果后再调用下游；复杂分支、并行汇合和有界循环使用 [workflow.Graph](../../workflow/README.md)，通过 Adapt 接入现有 Agent。详细边界见[执行契约](EXECUTION_CONTRACT.md)。
