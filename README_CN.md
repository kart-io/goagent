# GoAgent - Go 语言 AI Agent 框架

[![Go Version](https://img.shields.io/badge/go-1.25.0-blue?style=flat-square&logo=go)](https://go.dev/)
[![Build](https://img.shields.io/github/actions/workflow/status/kart-io/goagent/ci.yml?branch=master&style=flat-square&logo=github)](https://github.com/kart-io/goagent/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/kart-io/goagent?style=flat-square)](LICENSE)
[![Documentation](https://img.shields.io/badge/docs-latest-brightgreen?style=flat-square&logo=readthedocs)](docs/)
[![Downloads](https://img.shields.io/endpoint?url=https://api.pinstudios.net/api/badges/downloads/kart-io/goagent/total)](https://github.com/kart-io/goagent/releases/latest)
[![Go Report Card](https://img.shields.io/goreportcard/grade/github.com/kart-io/goagent?style=flat-square)](https://goreportcard.com/report/github.com/kart-io/goagent)
[![Go Reference](https://img.shields.io/badge/go-reference-blue?style=flat-square&logo=go)](https://pkg.go.dev/github.com/kart-io/goagent)
[![Deepwiki](https://img.shields.io/badge/deepwiki-view-blueviolet?style=flat-square)](https://deepwiki.com/kart-io/goagent)

[English](README.md) | [中文](README_CN.md)

GoAgent 是嵌入 Go 业务服务的可恢复、可计量、可观测 Agent 执行内核：原生工具循环、带持久检查点与审批的类型化工作流图、请求预算、会话日志、OpenTelemetry 事件与 MCP。

相关仓库：

- [goagent-contrib](https://github.com/kart-io/goagent-contrib)：检索器（BM25、混合、重排序、Qdrant）、文档加载、实用工具与多实例 Postgres `durable.Store`。
- [goagent-examples](https://github.com/kart-io/goagent-examples)：业务案例（DevOps、文章审核、自动回复、退款恢复、真实模型编排）。
- `github.com/kart-io/goagent-harness`：Preset、计划模式、压缩、技能、子 Agent 组件与编码工具，见 [Harness 迁移指南](docs/guides/HARNESS_MIGRATION.md)。

## 特性

- **统一执行入口**：`agent.AsRunnable()` 的 `Invoke`、`Stream`、`Batch`、`Pipe` 共用同一执行源，工具、检索、中间件与回调走同一路径。
- **原生工具循环**：模型在一次流式请求中选择工具或直接回答（OpenAI、DeepSeek、Gemini Provider）；可选并发工具调用与直接返回工具。
- **循环扩展点**：PreStep、Request、RequestError、TurnStopping 钩子，提示词段落与上下文，以及接受 Steer/Inject/Redirect/Followup 的运行句柄 `Handle`。
- **持久执行**：`core/durable` 记录模型回合与工具调用；`workflow.Graph` 编译带分支、并行汇合、有界循环与审批恢复的类型化图。
- **执行治理**：根预算（`core/budget`）、带审批、守卫与准入限制的 `tools.Dispatcher`/`Pipeline`、工具结果卸载、会话日志（`core/session`）。
- **LLM 提供商**：OpenAI（及兼容端点）、DeepSeek、Gemini 支持 `AsRunnable` 与原生工具循环；Anthropic、Kimi、SiliconFlow、Ollama、Cohere、Hugging Face 经同步入口 `Execute` 使用。见[提供商对照](docs/README.md#llm-提供商)。
- **可观测性**：只读执行事件（`core/observe`）与 OpenTelemetry 集成（`observability`）。
- **MCP**：把官方 MCP 客户端会话适配为工具（`mcp/remote`），以及本地工具箱。

## 快速开始

```bash
go get github.com/kart-io/goagent
export OPENAI_API_KEY=...
export OPENAI_MODEL=...   # 可选：OPENAI_BASE_URL 指向 OpenAI 兼容端点
```

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
    // 未通过 llm.With* 选项显式设置时，读取 OPENAI_API_KEY / OPENAI_MODEL / OPENAI_BASE_URL。
    client, err := providers.NewOpenAIWithOptions()
    if err != nil {
        log.Fatal(err)
    }

    agent, err := builder.NewSimpleBuilder(client).
        WithSystemPrompt("你是一个有帮助的助手。").
        WithMaxIterations(6).
        WithTimeout(30 * time.Second).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    out, err := agent.AsRunnable().Invoke(context.Background(), "用一句话介绍你自己。")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(out.Result)
}
```

### 工具

工具实现 `interfaces.Tool`（`Name`、`Description`、`ArgsSchema`、`Invoke`）。`tools.NewFunctionTool` 把函数包装为工具：

```go
weather := tools.NewFunctionTool(
    "get_weather",
    "查询城市当前天气",
    `{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`,
    func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        city, _ := args["city"].(string)
        return map[string]string{"city": city, "weather": "晴"}, nil
    },
)

agent, err := builder.NewSimpleBuilder(client).
    WithSystemPrompt("你可以查询天气。").
    WithTools(weather).
    WithMaxIterations(6). // 包含最终回答
    WithRequestError(3, builder.RetryTransient(3, func(retry int) time.Duration {
        return time.Duration(retry) * 200 * time.Millisecond
    })).
    Build()
if err != nil {
    return err
}
out, err := agent.AsRunnable().Invoke(ctx, "巴黎天气怎么样？")
```

### 流式调用

```go
ctx, cancel := context.WithCancel(ctx)
defer cancel() // 提前停止消费时必须取消

chunks, err := agent.AsRunnable().Stream(ctx, question)
if err != nil {
    return err
}
for chunk := range chunks {
    if chunk.Error != nil {
        return chunk.Error
    }
    if chunk.Event != nil && chunk.Event.Type == "text" {
        fmt.Print(chunk.Event.Delta) // 暂态文本，可能出现在工具调用之前
    }
    if chunk.Done {
        fmt.Println(chunk.Data.Result) // 经过中间件和回调后的最终结果
    }
}
return ctx.Err()
```

其他事件类型有 `sources`、`step`（如 `tool_start`/`tool_end`）与 `usage`；`usage` 是累计快照。参见[快速入门](docs/guides/QUICKSTART.md)与[执行契约](docs/guides/EXECUTION_CONTRACT.md)。

### 预配置 Agent

```go
agent, err := builder.QuickAgent(client, "你是一个有帮助的助手。")
chat, err := builder.ChatAgent(client, "Alice")
rag, err := builder.RAGAgent(client, retriever) // DocumentRetriever、带 Retrieve 方法的对象或 VectorStore
```

`AnalysisAgent`、`WorkflowAgent`、`MonitoringAgent`、`ResearchAgent` 以同样方式预设提示词、状态与中间件。

### 基于证据的 RAG

`WithRetriever(retriever, builder.RetrievalConfig{MaxDocuments: 4, MaxContextRunes: 12000})` 在每次请求时检索证据，并在输出元数据 `retrieval_sources` 中返回来源。检索器实现 `interfaces.DocumentRetriever`，现成实现位于 [goagent-contrib/retrieval](https://github.com/kart-io/goagent-contrib/tree/master/retrieval)。参见 [RAG 与流式指南](docs/guides/GROUNDED_RAG_AND_STREAMING.md)。

### 运行句柄与循环钩子

客户端实现 `llm.ToolStreamClient` 且配置了工具时，`agent.Start(ctx, input)` 在后台运行并返回 `*builder.Handle`（`Steer`、`Inject`、`Redirect`、`Followup`、`Interrupt`、`Wait`）。钩子（`WithPreStep`、`WithIdentifiedPreStep`、`WithRequestHook`、`WithRequestError`、`WithTurnStopping`、`WithPromptSections`、`WithPromptContexts`）、工具结果卸载（`WithOffload`）与会话日志（`WithSessionLog`）见 [builder README](builder/README.md)、[上下文治理指南](docs/guides/CONTEXT_POLICY.md) 与 [core/session](core/session/README.md)。

### 持久执行与编排

- [原生编排](docs/guides/NATIVE_ORCHESTRATION.md)：`workflow.Graph`、持久图 `AsRunnable`、分支级恢复与 `tools.Dispatcher`。
- [工作流](workflow/README.md)：类型化图、分支、并行汇合与有界循环。
- [可组合编排](docs/guides/COMPOSABLE_ORCHESTRATION.md)：用 `tools.NewRunnableToolFactory` 把子 Agent 或 `workflow.DurableGraph` 包装为工具；子流程等待审批时，父流程以 `builder.ErrWaiting` 挂起。
- [宿主接入](docs/guides/HOST_INTEGRATION.md)：`execution.NewScope` + `execution.ConfigureBuilder` + `BuildContext`；离线示例：`go run ./examples/execution-scope`。
- [扩展契约](docs/guides/EXTENSION_CONTRACT.md)与[执行治理](docs/guides/EXECUTION_GOVERNANCE.md)：预算、诊断与资源限制。

## 包一览

| 包 | 说明 |
|----|------|
| [builder](builder/) | 流式构建 Agent、原生工具循环、钩子、`Handle` |
| [builder/contextpolicy](builder/contextpolicy/) | 工具结果卸载（`Offload`） |
| [core](core/) | `Runnable`/`StreamRunnable`、回调、状态；子包 `durable`、`budget`、`session`、`observe`、`checkpoint`、`middleware` |
| [execution](execution/) | 宿主作用域：运行与会话身份、日志、预算、期限、观察者 |
| [workflow](workflow/) | 类型化图编译与持久图 |
| [llm](llm/) | 客户端接口与选项；`llm/providers` 为各提供商实现 |
| [tools](tools/) | 工具注册表、函数工具、`Dispatcher`、`Pipeline`；`tools/policy`、`tools/spill` 与内置工具 |
| [mcp](mcp/) | MCP 客户端适配与工具箱 |
| [memory](memory/) | 对话与案例记忆、向量存储 |
| [harness/env](harness/env/) | 供工具使用的文件系统与命令执行接口 |
| [observability](observability/) | OpenTelemetry 追踪与指标 |
| [evaluation/agentbench](evaluation/agentbench/README.md) | 可复现的任务评测 |
| [testing](testing/) | Mock、测试工具与会话日志回放 |

其他包：`cache`、`errors`、`interfaces`、`parsers`、`store`、`middleware`、`devtools`、`utils`。

## 文档

- [快速入门](docs/guides/QUICKSTART.md)
- [使用指南](docs/guides/USER_GUIDE.md)
- [Builder API 参考](docs/guides/BUILDER_API_REFERENCE.md)
- [LLM 提供商](docs/guides/LLM_PROVIDERS.md)
- [执行开销实测](docs/evaluation/2026-10-31-execution-overhead/REPORT.md)

完整索引见 [docs/README.md](docs/README.md)，破坏性变更见 [CHANGELOG.md](CHANGELOG.md)。

## 示例

可运行示例位于 [`examples/`](examples/README.md)；业务案例位于 [goagent-examples](https://github.com/kart-io/goagent-examples)。

```bash
go run ./examples/basic/01-simple-agent
go run ./examples/workflow
```

## 开发

```bash
git clone https://github.com/kart-io/goagent.git
cd goagent
go test ./...
golangci-lint run
bash scripts/verify_imports.sh   # 导入分层检查
bash scripts/check_coverage.sh   # 覆盖率门槛
```

## 许可证

Apache License 2.0，见 [LICENSE](LICENSE)。

## 支持

- **问题反馈**：[GitHub Issues](https://github.com/kart-io/goagent/issues)
- **讨论**：[GitHub Discussions](https://github.com/kart-io/goagent/discussions)

## 致谢

GoAgent 受 [LangChain](https://github.com/langchain-ai/langchain)、[LangGraph](https://github.com/langchain-ai/langgraph) 与 [AutoGen](https://github.com/microsoft/autogen) 启发。
