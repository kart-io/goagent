# GoAgent 快速入门

新应用统一使用 `agent.AsRunnable()`。同步和流式调用使用相同执行源，配置的工具、检索器、中间件及完成回调走同一路径。要求 Go 1.27+ 与实现 `llm.Client`（`CompleteStream`）的客户端；内置 Provider（OpenAI 及其兼容预设、DeepSeek、Gemini、Anthropic）都同时实现原生工具流（`llm.ToolStreamClient`）。

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
- `WithSessionLog(store).WithSessionID(sessionID)` 启用多轮对话；`WithCompaction` 在对话过长时把较早的消息压缩成摘要。
- 长任务可用 `WithOffload` 把超长工具结果换成引用、经读取工具分页读回，见[上下文治理](CONTEXT_POLICY.md)；默认关闭。
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
| `agent.Execute(ctx, input)`（已删除） | `agent.AsRunnable().Invoke(ctx, input)` | 需要流式模型，并执行已配置工具 |
| `agent.ExecuteWithTools(ctx, input)`（已删除） | `agent.AsRunnable().Invoke(ctx, input)` | 需要 `llm.ToolStreamClient`，预算包含最终回答 |
| `agent.ExecuteStream(ctx, input)` | `agent.AsRunnable().Stream(ctx, input)` | 保留同一事件源，增加 Runnable 的组合能力 |

Builder 接受任意 `llm.Client`，配置工具时还要求 `llm.ToolStreamClient`，Build 阶段校验。旧 `Execute` 不执行工具，迁移后已配置的工具会被调用，需要确认工具副作用与预算。`Pipe` 等待上游最终结果后再调用下游；复杂分支、并行汇合和有界循环使用 [workflow.Graph](../../workflow/README.md)，通过 Adapt 接入现有 Agent。详细边界见[执行契约](EXECUTION_CONTRACT.md)。

## 迁移检查与持续验证

从 v0.42 或更早版本升级到 v0.46 时，还需处理以下变化；完整签名与删除列表见 [CHANGELOG](../../CHANGELOG.md)。

| 版本 | 接入方需要处理的变化 |
|---|---|
| v0.43 | `llm.Client` 统一为流式接口，`llm.StreamClient` 并入其中；同步收集使用 `llm.Complete` / `llm.Chat`。工具调用实现 `llm.ToolStreamClient`。自定义客户端、缓存包装器和测试 Mock 需同步迁移。 |
| v0.43 | 旧 `interfaces.Agent/Runnable` 改用 `core.Runnable`；本地 `mcp/core`、`mcp/toolbox`、`mcp/tools` 已删除，本地工具用 `interfaces.Tool` 与 `tools.Pipeline`，远程 MCP 用 `mcp/remote`。 |
| v0.44 | Embedding、Document/Case 向量与相关存储接口统一为 `[]float32`；修改自定义 embedder、向量存储签名和向量字面量。 |
| v0.45 | 语义缓存删除独立 OpenAI Embedding 实现，改传 `providers.NewOpenAIWithOptions` 创建的客户端；用 `llm.WithEmbeddingModel` 显式保留已有缓存模型。原默认 `text-embedding-3-small` 与新默认 `text-embedding-ada-002` 的向量不兼容，切换模型时须重建缓存。 |
| v0.46 | 可选 `RetrievalConfig.Filter` 按请求生成过滤条件；检索器须同时实现 `interfaces.RequestRetriever`。过滤函数返回错误或空条件时执行失败，不会退回无过滤检索；租户与授权条件由可信宿主提供。 |

```sh
# 检查真实 Go import 的层级约束。
bash scripts/verify_imports.sh
```

旧入口与旧接口已经删除，残留调用会直接编译失败。逐项迁移时确认 Provider 的 ToolStreamClient、自动工具执行、预算和回调语义，不要批量替换。

[Provider 持续回归](PROVIDER_REGRESSION.md)提供离线门禁与有界真实测试。退款应用可复用 [refund.Service](../../workflow/refund/README.md)；串行图恢复使用 [CompileDurable](../../workflow/README.md#节点边界的持久化恢复)。

复杂业务可以完全采用 [GoAgent 原生编排与工具体系](NATIVE_ORCHESTRATION.md)：Dispatcher 同时服务图节点与 Builder，持久图 AsRunnable 提供统一调用、步骤事件和审批恢复。无需引入其他编排框架。
