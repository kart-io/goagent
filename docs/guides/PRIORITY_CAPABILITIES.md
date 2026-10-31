# 检索、事件流、MCP 与可恢复步骤

本文汇总四类入口：goagent-contrib 中的检索器契约、Builder 统一事件流、官方 MCP 客户端适配与 `core/durable` 顺序步骤。`ExecuteWithTools` 在支持结构化流的 Provider 上使用统一执行管道，非流式客户端保留兼容循环；`Execute` 保持同步语义。`core.InterruptManager` 是进程内的中断机制，不会自动变成持久化步骤。

## Retriever 执行契约

检索器位于独立模块 [goagent-contrib/retrieval](https://github.com/kart-io/goagent-contrib/tree/master/retrieval)（导入路径 `github.com/kart-io/goagent-contrib/retrieval`），本节与下一节的 API 均在该模块中，满足 `interfaces.DocumentRetriever`，可直接传给 `builder.WithRetriever`。

内置检索器构造函数显式绑定检索实现，`Invoke/Stream/Batch/Pipe`、`WithConfig/WithCallbacks` 调用的都是该实现。自定义检索器使用：

```go
r := retrieval.AsRetriever(customDocumentRetriever)
// 或
r := retrieval.NewFuncRetriever(func(ctx context.Context, query string) ([]*interfaces.Document, error) {
    return loadDocuments(ctx, query)
})
```

未绑定实现的 `NewBaseRetriever()` 返回配置错误；只嵌入 BaseRetriever 不会得到虚方法分派。Batch 使用受 MaxConcurrency 限制的工作协程，保留输入顺序，不吞掉各输入的错误。复合检索失败会取消其余任务，后端仍须遵守 context；MultiQuery/Ensemble 不返回部分成功结果，需要降级时由调用方显式实现。

中文关键词检索使用汉字单字、相邻双字索引，与英文词项混合；这是无需词库的词法基线，不具备语义同义词、专业分词或跨语言召回能力。BM25 读取索引中的文档长度；混合检索的加权求和使用归一化分数，避免 BM25 与向量分值尺度不一致。

## 真实 LLM 重排序

```go
ranker := retrieval.NewLLMRerankerWithClient(client, 4)
ranker.MaxCandidates = 20
ranker.MaxDocumentRunes = 4000
r := retrieval.NewRerankingRetriever(base, ranker, 20, retrieval.RetrieverConfig{TopK: 4})
```

一次请求对所有候选评分，用候选下标识别文档，验证评分完整、无重复且位于 `[0,1]`。错误直接返回，不回退到模拟评分。文档按 Unicode 字符截断；候选超限报错，默认上限 20 篇、每篇 4000 字符。查询长度由调用方控制。`FetchK` 限制送入重排序的候选数；基础 retriever 的 TopK 应不小于 FetchK，包装器不会修改共享检索器。

`NewLLMReranker(topN)` 未设置 Client 时，对非空候选返回错误。`CrossEncoderReranker` 已标记弃用，实际是词汇重合算法（同 `LexicalOverlapReranker`），不做 cross-encoder 推理；需要真实模型时使用上述 LLM 入口或 `retrieval/cohere` 的 `CohereReranker`。

## 统一事件流

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel() // 消费者提前退出时必须取消
stream, err := agent.ExecuteStream(ctx, "退款政策是什么？")
if err != nil { return err }
for event := range stream {
    switch event.Type {
    case "sources": // event.Sources，编号与模型参考资料一致
    case "step":    // event.Step：model_start、model_replayed、tool_start、tool_end（旧工具循环为 select_tools）
    case "text":    // event.Delta，原生模型增量
    case "usage":   // event.Usage，全部模型请求的累计用量快照
    case "error":   // event.Error（Go）；JSON 对应 event.Message
    case "done":    // event.Output，经过中间件与完成回调的最终结果
    }
}
```

需要 `llm.StreamClient`；配置了工具还需支持工具调用。实现 `llm.ToolStreamClient` 的 Provider（OpenAI、DeepSeek、Gemini）使用原生工具对话循环：模型每轮选择工具或直接回答，省去 READY 确认与独立答案请求；其他 Provider 取决于实际接口能力。工具参数不做逐 Token 展示。原生路径的 `MaxIterations` 计入全部模型请求，达到上限报错；元数据记录 `model_calls` 和 `usage_complete`。详见[执行契约与请求优化](EXECUTION_CONTRACT.md)。

中间件与 Execute 共用，包括 OnBefore、OnAfter、OnError；中间事件是暂态内容，以 done 为最终成功依据。缓存命中（如 `core/middleware.CacheMiddleware`）可能只有 done，OnAfter 修改的最终文本也可能与已发出的增量不同。错误终止不会再发送 done。取消时 error 事件是尽力发送，调用方同时检查 context。消费者必须继续读取或取消，避免背压挂起上游。最终请求遵守 Builder 的 MaxTokens/Temperature；成功生成后更新状态，启用自动保存时写检查点（保存失败终止，不发送 done）。未知用量不会估算为真实值，`answer_usage_reported` 表示最终模型是否报告用量。

## 官方 MCP 客户端适配

```go
session, err := remote.Connect(ctx, &mcp.StreamableClientTransport{
    Endpoint: "http://localhost:8080/mcp",
})
if err != nil { return err }
defer session.Close()
tools, err := remote.Tools(ctx, session)
// builder.NewSimpleBuilder(client).WithTools(tools...)
```

`remote` 为 `github.com/kart-io/goagent/mcp/remote`；`mcp` 为 `github.com/modelcontextprotocol/go-sdk/mcp`。官方 SDK v1.8.0 负责协议协商、传输、分页与取消；也可传入 `CommandTransport` 使用 stdio。HTTP 与真实子进程 stdio 均有互操作测试。工具保留完整 CallToolResult（含结构化内容与其他内容块），IsError 转为 Go error（`remote.WithToolErrorsAsRejected()` 改为交还模型的拒绝结果）。`remote.WithToolName` 重命名或过滤工具，`remote.WithDurablePolicy` 设置经 Dispatcher 持久调用时的操作策略。认证、请求大小限制与 HTTPClient 由 SDK transport 配置。需交互输入的结果明确报错，应用可通过 SDK session 继续交互，适配层不自动批准输入请求。工具列表是本次发现的快照，变化后重新读取。

参考：[官方 SDK](https://github.com/modelcontextprotocol/go-sdk/tree/v1.8.0)、[传输说明](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/docs/protocol.md)。

## 持久化步骤执行

```go
store, err := durable.NewFileStore("./checkpoints")
if err != nil { return err }
runner := &durable.Runner{Store: store, Version: "refund-v1", Steps: []durable.Step{
    {Name: "refund", RequireApproval: true,
     Run: func(ctx context.Context, in durable.StepInput) (json.RawMessage, error) {
         // 将 in.Key 传给外部服务作为幂等键，in.Data 是上一步输出。
         return refundWithIdempotency(ctx, in.Key, in.Data)
     }},
}}
snapshot, err := runner.Run(ctx, "request-123", json.RawMessage(`{"order":"123"}`), "")
// errors.Is(err, durable.ErrApprovalRequired): 保存/展示 snapshot，等待应用授权。
// 重启后重新注册同一版本的步骤，再传入经批准的 snapshot.Key：
snapshot, err = runner.Run(ctx, "request-123", nil, snapshot.Key)
```

`core/durable` 将下一步位置、数据、审批状态、幂等键持久化。FileStore 使用每运行 ID 的 OS 文件锁、文件 fsync、同目录原子替换与目录同步。已提交步骤不重跑；运行版本、步骤名称或审批设置变化会拒绝旧检查点。函数实现变化时必须增加 Version，无法自动比较闭包内容。`Runner.BeforeApprove` 可在批准生效前核对暂停期间外部数据是否被改动。

执行副作用后、检查点保存前崩溃，会以**相同键重试**该步骤；这是至少一次执行，需要外部服务按键去重，不保证恰好一次。审批键不是认证凭证，应用负责身份、授权与审批界面。同一运行初始数据只写入一次。FileStore 面向具有可靠锁/原子替换语义的本地文件系统；多实例共享运行用 goagent-contrib 的 `durable/pgstore`。

测试包含真实子进程退出后恢复、审批匹配、失败后复用幂等键、已完成步骤不重跑、锁等待取消和损坏检查点拒绝。它用于显式注册的顺序步骤，不恢复任意 Agent 调用栈、动态图或 InterruptManager 的内存 channel；图级恢复见[原生编排](NATIVE_ORCHESTRATION.md)。

[退款审批与崩溃恢复案例](https://github.com/kart-io/goagent-examples/blob/main/workflow/refund-recovery/README.md)组合检查图、审批检查点与独立 SQLite 模拟退款服务，并通过真实进程退出验证副作用提交后的幂等重试。

## 推理输出契约补充

ReAct、ToT 等推理策略已在 v0.17.0 删除，代码保留在 `v0.16.1`。当前替代方案是原生工具循环与 `workflow` Graph，见[原生编排与工具体系](NATIVE_ORCHESTRATION.md)。
