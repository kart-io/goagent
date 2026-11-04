# Eino 对照：组件、编排、ADK 与扩展生态

日期：2026-11-04。GoAgent 基线 `bf8c7ef3`，goagent-contrib 基线 `f0e6bce`。Eino 一侧依据 cloudwego.io 官方文档与 eino-ext README（2026-11-04 经 context7 读取），未锁定发行版本；标注“未核实”的条目来自既有认知，选型前需按选定版本核验。

## 先明确比较对象

Eino 是组件、编排、ADK 与扩展生态齐全的 LLM 应用框架；GoAgent 按[定位决策](2026-10-29-goagent-positioning.md)收敛为执行内核，§3 已否决“补齐 LangChain 式功能广度”和“改为 Eino 之上的适配层”。因此本文的差距不等于待补清单，每项结论见“差距与采纳范围”。框架开销对比见[性能对测 v2](../evaluation/2026-10-27-framework-performance-v2/REPORT.md)，此处不复述。

## 组件抽象

| 维度 | Eino | GoAgent |
|---|---|---|
| 抽象方式 | 每类能力一个接口（`components/*`），统一 `schema.Message`/`schema.Document` | 核心只保留执行所需接口（`llm.Client`、`interfaces.Tool`、`DocumentRetriever`、`VectorStore`），RAG 接口在 contrib |
| 调用选项 | 所有组件接受 `opts ...Option`，可单次覆盖 | 在构造或 builder 上配置 |
| 流式 | 组件层原生，`Stream()` 返回 `*schema.StreamReader[T]` | Provider 按能力接口拆分（`StreamClient`、`ToolStreamClient`） |
| 作为编排节点 | 任意组件可直接作 Graph 节点 | 包装为 `core.Runnable` 或 `workflow.Node`；contrib `Retriever` 内嵌 `core.Runnable` |
| 回调/观测 | 组件统一接 `callbacks.Handler` | 集中在执行层：`core/observe` + OTel + Dispatcher 管道 |

## 逐组件对照

| 组件 | Eino 接口与实现 | GoAgent 接口与实现 |
|---|---|---|
| ChatModel | `BaseChatModel{Generate, Stream}`；`ToolCallingChatModel.WithTools()` 返回新实例；另有 `AgenticMessage` 路径。eino-ext：OpenAI、Claude、Gemini、Ark、Ollama、DeepSeek、Qwen 等 | `llm.Client{Complete, Chat, Provider, IsAvailable}` 加能力接口与 `CapabilityChecker`；`llm.Message` 含 `ToolCalls`、`ReasoningContent`、`ProviderState`。OpenAI 兼容、DeepSeek、Gemini 支持原生工具流，其余 Provider 仅同步入口 |
| Tool | `BaseTool.Info()`；`InvokableTool.InvokableRun(argsJSON) string`；`StreamableTool`。eino-ext：MCP、Google 搜索、DuckDuckGo、Bing 等 | `interfaces.Tool{Name, Description, Invoke, ArgsSchema}`，可选 `DurableCallTool`、`ArgumentErrorTool`、`ConcurrentCallTool`、`ValidatableTool`、`ReconcilableTool`；`tools.Dispatcher`/`Pipeline` 负责审批、守卫、准入、预算、持久日志。核心 compute/http/search/shell、`mcp/remote`；contrib database、文件、API 调用 |
| Retriever | `Retrieve(ctx, query, opts...)`。Redis、Milvus2、ES8、Qdrant、VikingDB 等 | 核心 `DocumentRetriever.GetRelevantDocuments`，经 `builder.WithRetriever` 每次请求检索并写出 `retrieval_sources`；contrib keyword/BM25、hybrid、multi-query、Qdrant、Reranker |
| Embedding | `Embedder.EmbedStrings(texts, opts...) [][]float64`。OpenAI、Ark、Ollama 等 | `llm.EmbeddingClient.Embed`（OpenAI、Gemini Provider 声明）；`memory.TextEmbedder` 由使用方定义；核心与 contrib `retrieval.Embedder` 均为 float32，OpenAI、HuggingFace、Cohere、VertexAI |
| Indexer | 独立 `Indexer.Store(docs, opts...) ids`，读写分离 | 无独立接口，写入为 `interfaces.VectorStore.AddDocuments` |
| Loader/Transformer | `Loader.Load(ctx, Source)`；`Transformer.Transform(docs)`（切分、重排）。WebURL、S3、File、HTMLSplitter 等 | contrib `DocumentLoader{Load(ctx), LoadAndSplit, GetMetadata}`、`TextSplitter`；text/json/markdown/web/pdf，字符/代码/token 切分 |
| Prompt | `ChatTemplate.Format(ctx, vars) []*schema.Message`；FString/GoTemplate/Jinja2（未核实） | 无模板组件；`WithSystemPrompt`、`WithPromptSections`、`WithPromptContexts` |
| 自定义节点/解析 | `compose.InvokableLambda`；eino-ext JSONMessageParser | `workflow.Node[T]`、`core.Runnable`；`parsers.OutputParser[T]` |
| 记忆/检查点 | 会话在 ADK 内管理；`CheckPointStore` | `memory` 对话与分层记忆；`interfaces.Checkpointer`、`core/durable`；contrib `pgstore` |

## 编排

| | Eino `compose` | GoAgent `workflow`/`core` |
|---|---|---|
| 形态 | Chain、Graph（节点间类型可异构）、Workflow（字段映射 DAG，未核实） | `core.Chain`、`Runnable.Pipe`；`workflow.Graph[T]`：`AddNode`、`AddRunnable`、`AddEdge`、`AddBranch`、并行汇合、有界循环 |
| 类型检查 | 编译期检查上下游类型对接 | 单一状态类型 T；`Compile` 校验可达性、出口与引用 |
| 流 | 四种范式，自动拼接/拆分 | 统一由 `StreamRunnable` 产出 |
| 持久化 | checkpoint 与中断恢复 | `CompileDurable` 节点/并行分支检查点、审批续跑、重启恢复、根预算联动 |

## ADK

| 能力 | Eino ADK | GoAgent |
|---|---|---|
| 单 Agent | `ChatModelAgent` + `Runner.Query`，`AsyncIterator` 事件 | `builder.AgentBuilder` 构建后 `AsRunnable()`；`ConfigurableAgent.Start()` 返回 `Handle`（Steer/Inject/Redirect/Followup/Interrupt） |
| 多 Agent | `NewSequentialAgent`、`NewParallelAgent`、`NewLoopAgent`、`supervisor.New`、`deep.New`、transfer | 无预置；`tools.NewRunnableToolFactory` 把子 Agent/持久图包装成工具，或用 `workflow.Graph` |
| 中断恢复 | `RunnerConfig.CheckPointStore` + `Resume(checkpointID)` | 子 Agent 待审批时父 Agent 挂起（`builder.ErrWaiting`），持久日志续跑 |
| 用量/预算 | callbacks 统计 | `WithUsageHook` 逐次与累计统计；父子共享根预算、`MaxModelRequests` |

## 扩展生态

| | Eino | GoAgent |
|---|---|---|
| 扩展仓库 | eino-ext：model、embedding、retriever、indexer、tool、document、callbacks、`adk/backend/local` | goagent-contrib：retrieval、document、tools、pgstore、httpagent、acp |
| 观测对接 | CozeLoop、APMPlus、Langfuse、LangSmith | OTel 与 Prometheus |
| 开发工具 | Eino Dev 可视化编排与调试（未核实） | `devtools`、session log 回放（`testing`）、`evaluation/agentbench` |
| 示例 | eino-examples | goagent-examples |

## 差距与采纳范围

| Eino 有、GoAgent 核心没有 | 结论 | 依据 |
|---|---|---|
| Indexer、Loader/Transformer、更多 Embedding 与向量库后端 | 归 contrib，核心不做 | 定位 §4.2：`retrieval`、`document` 已迁至 contrib |
| ChatTemplate | 不做 | 定位 §4.2：`prompt` 包无导入方已删除；提示词由 `WithPromptSections`/`WithPromptContexts` 承担 |
| 异构 DAG、字段映射 | 不做 | 定位 §5“不做”：反射字段映射 |
| 四种流范式自动适配 | 不做完整运行时，仅作参照 | 定位 §5 第 4 项与“不做：完整四范式流运行时”；[编排复审](2026-10-27-framework-orchestration-review.md) S14 |
| Sequential/Parallel/Loop/Supervisor/Deep 预置模式 | 不进核心；如需要归 goagent-harness | 定位 §4.2 删除 `multiagent`；[Harness 迁移指南](../guides/HARNESS_MIGRATION.md) |
| 观测平台专用回调适配 | 不做 | 定位 §2.1：复用 OTel，不自建 trace 体系 |
| 组件级单次调用 `opts` | 不做 | 无业务证据，不满足 R2 准入原则（定位 §5） |

本轮对照没有发现需要进入核心的能力缺口。

## 后续事项：Embedding 接口清理

核对使用方后，真实问题小于“多处重复定义”：

| 定义 | 使用情况 | 结论 |
|---|---|---|
| `llm.EmbeddingClient` | Provider 能力声明，OpenAI 实现 | 保留 |
| `memory.TextEmbedder` | `WithEmbedder` 使用；单方法设计使 `llm.EmbeddingClient` 直接满足（`memory/enhanced.go:160-163`） | 保留，属于使用方定义接口 |
| `memory.Embedder` | 主模块、goagent-examples、contrib 均无引用 | 删除 |
| `llm.BatchEmbeddingClient` | 无使用方；`llm/providers` 无实现（现有 `EmbedBatch` 均返回 float32，不满足该接口） | 删除 |
| `memory.EmbeddingModel`（float32） | `EmbeddingVectorStore` 使用 | 已统一：核心向量改为 float32（见 CHANGELOG Unreleased），`SimpleEmbeddingModel` 可直接用于 `WithEmbedder` |
| `llm/cache.EmbeddingProvider`（float32） | 语义缓存内部使用，自带 `OpenAIEmbeddingProvider` 与 Mock 实现，与 `llm/providers` 的 OpenAI Embedding 并存 | 精度已一致；重复的 OpenAI 实现未处理 |
| contrib `retrieval.Embedder`（float32） | contrib 检索链路 | 层次不同，不与核心统一 |

删除属于破坏性变更，需在 CHANGELOG 说明；作为独立代码改动执行，不随本文提交。

## 复核方式

```bash
# GoAgent 接口
grep -rn "^type .* interface" interfaces llm/*.go tools/*.go memory/*.go workflow/*.go parsers/*.go | grep -v _test
# contrib 接口
(cd ../goagent-contrib && grep -rn "^type [A-Z][A-Za-z]* interface" --include='*.go' . | grep -v _test)
# Embedding 定义与使用方
grep -n -A6 "type \(EmbeddingClient\|BatchEmbeddingClient\|Embedder\|EmbeddingModel\|TextEmbedder\|EmbeddingProvider\) interface" llm/capabilities.go llm/cache/types.go memory/*.go
grep -rn --include='*.go' "func (.*) EmbedBatch" . | grep -v _test
grep -rn --include='*.go' "memory\.Embedder\b\|BatchEmbeddingClient" . ../goagent-examples ../goagent-contrib | grep -v _test
```

Eino 接口参考：[组件文档](https://www.cloudwego.io/docs/eino/core_modules/components/)、[ADK 概览](https://www.cloudwego.io/docs/eino/overview/)、[eino-ext](https://github.com/cloudwego/eino-ext)。
