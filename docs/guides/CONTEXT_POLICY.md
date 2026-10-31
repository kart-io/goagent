# 工具上下文：结果卸载与历史压缩

上下文治理只有一种机制：PreStep 钩子在每次模型请求前改写对话，改写后的对话成为之后的历史；持久运行中每步的结果记入日志，恢复时回放。结果卸载（`contextpolicy.Offload`）与历史压缩（[goagent-harness](https://github.com/kart-io/goagent-harness) 的 `preset` 中的 `compaction` 组件）都是 PreStep 组件。要求原生工具流：配置工具并使用 `llm.ToolStreamClient`；旧 `Execute` 遇到 `WithOffload` 报错。

## 大结果卸载

```go
offload, err := contextpolicy.NewOffload(contextpolicy.OffloadConfig{
    Store:                contextpolicy.NewMemoryStore(), // 持久运行用 contextpolicy.NewDurableResultStore(store)
    MaxToolResultRunes:   8000,
    KeepRecentToolRounds: 2,
    ReadPageRunes:        1024,
    ExcludeTools:         []string{"payment_receipt"},
})
agent, err := builder.NewSimpleBuilder(client).
    WithTools(searchTool, offload). // 读取工具即 offload 本身；持久运行与其他工具一起经 Dispatcher
    WithMaxIterations(12).
    WithOffload(offload).
    Build()
```

每次模型请求前，保留最近 `KeepRecentToolRounds` 个完整工具回合（一轮多个工具视为同一回合），更早且超过 `MaxToolResultRunes` 的工具正文写入 `Store`，在对话中换成引用：

```json
{"context_result":"r1","runes":12000,"read_tool":"read_context_result","stored":{"key":"…","sha256":"…"}}
```

assistant 的工具调用、reasoning_content、结果角色和 tool_call_id 保持不变。模型以 `read_context_result` 的 `key`（`context_result` 的值）和可选 `offset` 分页读取原文；offset 按 Unicode 字符计数，每页默认 1024 字符，最大 16384。返回 `text`、`next_offset` 和 `done`。别名按对话中已有的引用数编号（`r1`、`r2`…），避免模型抄写长哈希出错；键不存在时返回 `{"error":"unknown context_result key","valid_keys":[...]}`，`offset` 非法时返回 `{"error":"invalid offset","min_offset":0,"max_offset":<原文字符数>}`，都不中止执行。读取工具本身与排除名单中的工具不卸载。

最后一次模型请求（`Step.Final`）不再提供工具，模型无法读取，因此把引用全部还原为原文；不承诺最终请求的上下文变小，需要时在此前用工具提取业务字段或增加预算。`WithReturnDirectTools` 返回完整工具原文。

### 实例、作用域与恢复

- 一个 `Offload` 供多次执行并发共享，引用表按结果作用域区分；同一 Agent 只能装一个，读取工具须恰好注册一次（Build 时校验）。
- 作用域：宿主经 `contextpolicy.WithResultScope` 提供时由宿主在运行不再恢复后调用 `offload.Release`；否则持久执行使用执行键（同样由宿主释放），非持久执行使用临时作用域，执行结束（成功或失败）时释放。
- 证据先写入存储再发出引用；同一逻辑结果内容不同时拒绝，读回时核对摘要。
- 持久运行要求持久的结果存储与稳定的执行键。恢复时 PreStep 的结果被回放、不重新计算，Builder 用回放的对话重建引用表，读取不受影响。
- `Offload.Identity()` 由 ID、Version、Namespace 与各项限制计算，作为 PreStep 的身份进入日志：改了配置再恢复旧运行，在请求模型前以 `intent changed` 失败。`Namespace` 须标识真实的存储后端，切换后端时修改它。
- `offload.Stats()["offloaded_results"]` 是实例累计的卸载数。

持久结果存储的释放、墓碑与 `PruneReleased` 见 [可组合编排指南](COMPOSABLE_ORCHESTRATION.md#上下文结果卸载)。

## 历史压缩

长对话的压缩由独立项目 goagent-harness 的 `preset` 中的 `compaction` 组件提供：对话估算长度超过阈值时，用 `Host.CompactionModel` 把较早的消息写成一条摘要，保留 system、最近的消息与本步新收到的输入。摘要随 PreStep 记入日志，恢复时不再调用模型。配置见 [goagent-harness/preset](https://github.com/kart-io/goagent-harness/blob/main/preset/README.md)，迁移见 [Harness 独立项目迁移](HARNESS_MIGRATION.md)。

## 自定义改写

其他改写直接写成 PreStep：

```go
b := builder.NewSimpleBuilder(client)
b.WithIdentifiedPreStep("glossary/"+version+"/"+hash, func(ctx context.Context, step *builder.Step) (builder.PreStepResult, error) {
    step.Messages = withGlossary(step.Messages) // 改写后的消息成为之后的历史；须幂等
    return builder.PreStepResult{}, nil
})
```

`identity` 标识钩子的配置，改变后旧的持久日志不被复用；不影响结果的钩子用 `WithPreStep`。钩子可以改写任意消息，须自行保持工具调用与结果的配对，并且不改动带 `ProviderState` 的消息（原生签名会失效）。

## 迁移

| 旧 | 新 |
|---|---|
| `WithContextPolicy(ContextPolicy{MaxToolResultRunes, …})` | `WithOffload(offload)`，并把 `offload` 注册为工具；`ExcludeTools` 等字段移到 `OffloadConfig` |
| `ContextPolicy.HistorySummary` | goagent-harness `preset` 的 `compaction` 组件 |
| `WithContextPolicies(factories...)` + `WithPolicyToolAdapter` | Offload：`WithOffload`，读取工具与业务工具一起经 Dispatcher；自定义策略：`WithIdentifiedPreStep` |
| `contextpolicy.Policy`、`Descriptor`、`Turn`、`Factory` | 删除；`Descriptor.ConfigHash` 的作用由 PreStep 的身份承担 |
| 元数据 `context_offloaded_results`、`policy.<id>.*`、`summary_calls` | 删除；用 `offload.Stats()` |

用旧版本记录的持久运行在升级前须结束：模型请求的意图不再包含策略描述符，Offload 的引用格式也已改变。
