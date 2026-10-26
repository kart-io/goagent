# 检索、事件流、MCP 与可恢复步骤

本轮在原有 API 之外补充可直接使用、可验证的入口。原有 `Execute`、`ExecuteWithTools` 和 `InterruptManager` 不会自动迁移为新流程。

## Retriever 执行契约

内置检索器构造函数显式绑定检索实现，`Invoke/Stream/Batch/Pipe`、`WithConfig/WithCallbacks` 不再误调用嵌入基类的空实现。自定义检索器使用：

```go
r := retrieval.AsRetriever(customDocumentRetriever)
// 或
r := retrieval.NewFuncRetriever(func(ctx context.Context, query string) ([]*interfaces.Document, error) {
    return loadDocuments(ctx, query)
})
```

未绑定实现的 `NewBaseRetriever()` 返回配置错误。不要只嵌入 BaseRetriever 并期待 Go 自动进行虚方法分派。Batch 使用受 MaxConcurrency 限制的工作协程，保留输入顺序；各输入错误不会被吞掉。复合检索失败会取消其余任务，后端仍须遵守 context。MultiQuery/Ensemble 不再静默返回部分成功结果，依赖旧容错语义的调用方应显式实现降级。

中文关键词检索使用汉字单字、相邻双字索引，与英文词项混合使用；这是无需词库的词法基线，不具备语义同义词、专业分词或跨语言召回能力。BM25 读取索引中的文档长度，避免每次查询反复切分文档。混合检索的加权求和使用归一化分数，避免 BM25 与向量分值尺度不一致。

## 真实 LLM 重排序

```go
ranker := retrieval.NewLLMRerankerWithClient(client, 4)
ranker.MaxCandidates = 20
ranker.MaxDocumentRunes = 4000
r := retrieval.NewRerankingRetriever(base, ranker, 20, retrieval.RetrieverConfig{TopK: 4})
```

一次请求对所有候选评分，用候选下标识别文档，验证评分完整、无重复且位于 `[0,1]`。错误直接返回，不回退到模拟评分。文档按 Unicode 字符截断；候选超限报错，默认上限 20 篇、每篇 4000 字符。查询长度由调用方控制。`FetchK` 限制送入重排序的候选数；基础 retriever 的 TopK 应设为不小于 FetchK，包装器不会修改共享检索器。

**兼容变化：** `NewLLMReranker(topN)` 仍能构造实例，但非空候选在未设置 Client 时返回错误。旧 `CrossEncoderReranker` 仍是词汇重合算法，已标记弃用；它不是实际 cross-encoder 推理。需要真实模型时使用上述 LLM 入口或已有 CohereReranker。

## 统一事件流

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel() // 消费者提前退出时必须取消
stream, err := agent.ExecuteStream(ctx, "退款政策是什么？")
if err != nil { return err }
for event := range stream {
    switch event.Type {
    case "sources": // event.Sources，编号与模型参考资料一致
    case "step":    // select_tools、tool_start、tool_end
    case "text":    // event.Delta，原生模型增量
    case "usage":   // event.Usage，工具选择请求＋最终答案请求的累计用量
    case "error":   // event.Error（Go）；JSON 对应 event.Message
    case "done":    // event.Output，经过中间件与完成回调的最终结果
    }
}
```

需要 `llm.StreamClient`；配置了工具还需支持工具调用。工具选择采用普通请求，最后单独发起一次流式答案请求，因此有额外模型调用。工具参数不做逐 Token 展示。选择轮数使用 `MaxIterations`，达到限制报错，不输出伪成功答案。

中间件与 Execute 共用，包括 OnBefore、OnAfter、OnError；中间事件是暂态内容，以 done 为最终成功依据。缓存命中可能只有 done，OnAfter 修改的最终文本也可能与已发出的增量不同。错误终止不会再发送 done。取消时 error 事件是尽力发送，调用方同时检查 context；正常完成也应收到 done。消费者必须继续读取或取消，避免背压挂起上游。最终请求遵守 Builder 的 MaxTokens/Temperature；成功生成后更新状态，启用自动保存时写检查点（保存失败终止，不发送 done）。未知用量不会估算为真实值，`answer_usage_reported` 表示最终模型是否报告用量。

实跑示例：

```sh
OPENAI_API_KEY="$DEEPSEEK_API_KEY" OPENAI_BASE_URL=https://api.deepseek.com \
OPENAI_MODEL=deepseek-flash go run ./examples/rag/grounded -stream
```

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

`remote` 为 `github.com/kart-io/goagent/mcp/remote`；`mcp` 为 `github.com/modelcontextprotocol/go-sdk/mcp`。官方 SDK v1.8.0 负责协议协商、传输、分页与取消；也可传入 CommandTransport 使用 stdio。HTTP 与真实子进程 stdio 均有互操作测试。工具保留完整 CallToolResult（含结构化内容与其他内容块），IsError 转为 Go error。认证、请求大小限制与 HTTPClient 由 SDK transport 配置。需交互输入的结果明确报错，应用可通过 SDK session 继续交互，适配层不自动批准输入请求。工具列表是本次发现的快照，变化后重新读取。

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

`core/durable` 将下一步位置、数据、审批状态、幂等键持久化。FileStore 使用每运行 ID 的 OS 文件锁、文件 fsync、同目录原子替换与目录同步。已提交步骤不重跑；运行版本、步骤名称或审批设置变化会拒绝旧检查点。函数实现变化时必须增加 Version，无法自动比较闭包内容。

执行副作用后、检查点保存前崩溃，会以**相同键重试**该步骤；这是至少一次执行，需要外部服务按键去重，不保证恰好一次。审批键不是认证凭证，应用负责身份、授权与审批界面。同一运行初始数据只写入一次。FileStore 面向具有可靠锁/原子替换语义的本地文件系统，不是跨主机租约服务。

测试包含真实子进程退出后恢复、审批匹配、失败后复用幂等键、已完成步骤不重跑、锁等待取消和损坏检查点拒绝。它用于显式注册的顺序步骤，不恢复任意 Agent 调用栈、动态图或旧 InterruptManager 的内存 channel。

## 推理输出契约补充

### ReAct

解析器支持位于行首的 `Final Answer:`、`ANSWER:`、`最终答案：`、`最终回答：`，以及常见 Markdown 标题/加粗形式；保留答案中的换行和段落。不会把 Thought 或工具 JSON 参数中嵌入的 `Final Answer:` 当作最终答案。空答案依然报错。

可选择启用一次格式修复：

```go
agent := react.NewReActAgent(react.ReActConfig{
    LLM: client,
    MaxSteps: 8,
    MaxParseRetries: 1,
})
```

默认 `MaxParseRetries=0`，保持失败即返回的调用成本。修复使用下一次循环额度，计入 MaxSteps、模型调用数和 Token 用量；达到任一上限就停止，不通过额外隐藏请求突破预算。格式错误的响应不执行工具，`Metadata["parse_retries"]` 记录修复次数。Invoke、InvokeFast、Stream、RunGenerator 共用此逻辑。

### ToT

生成候选、评分、检查完成状态、生成最终答案的模型错误均向上传播，不再用 `0.5` 默认分数或 `false` 掩盖失败。评分必须是 `[0,1]` 有限数；检查完成状态必须明确返回 yes/no。评分和检查指令放在 system 消息中，题目和候选路径作为数据传递，避免题目要求的最终输出格式覆盖评估协议。

搜索达到边界后的候选路径不再直接作为成功结果。新增一次受同一预算限制的最终请求，返回 `{"solved":true,"answer":"..."}` 或 `{"solved":false,"answer":""}`：

- `success`：模型确认完成且答案非空，Result 仅包含最终答案。
- `partial`：没有可完成的候选，或模型确认仍不完整，Result 不伪装为成功答案。
- `failed`：模型调用、预算、评分协议或最终 JSON 失败，调用返回原错误或可解包的包装错误。

**兼容变化：** Result 不再拼接整条搜索路径。路径保存在 `candidate_path`，成功时另有 `solution_path`；消费方须检查 Status。最终验证仍是模型判断，不是事实正确性的数学证明。它需要一个模型调用额度，预算不足会明确失败。根节点只是问题，不再为它发送无意义的“是否已解答”请求。

Invoke、InvokeFast、Stream 和 RunGenerator 使用相同最终答案处理。Stream 仍是单个最终块，不代表 Token 流；RunGenerator 的中间事件均为暂态，以最后的状态和错误为准。ToT 的 TokenUsage 汇总所有模型阶段，快照复制用量以避免后续修改旧事件。
