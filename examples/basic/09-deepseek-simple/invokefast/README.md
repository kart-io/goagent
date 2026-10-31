# DeepSeek 多 Agent 串行调用示例

用 `builder.NewAgentBuilder` 创建多个 DeepSeek Agent，依次调用 `Execute` 并打印每步耗时。

## 运行

```bash
export DEEPSEEK_API_KEY=your-deepseek-api-key   # 未设置时直接退出
go run ./examples/basic/09-deepseek-simple/invokefast
```

会发起多次真实的 DeepSeek 请求（模型 `deepseek-chat`，超时 120 秒）。

## 内容

1. **基础 Agent**：一句话回答"Go 语言的主要特点是什么？"。
2. **两步任务**：`AnalyzeAgent` 分析一段求和代码，`OptimizeAgent` 基于分析结果给出优化建议，打印各步与总耗时。
3. **结构化数据**：`UserDataGenerator` 与 `ProductDataGenerator` 生成 JSON 文本；随后 `CoordinatorAgent` 分析任务，再顺序调用两个生成 Agent。
4. **说明文字**：打印一段关于 InvokeFast 的固定文本。

## 关于 InvokeFast

本示例的所有调用都是普通的 `agent.Execute`，没有使用 InvokeFast。`core.FastInvoker` 接口与 `core.TryInvokeFast` 定义在 [core/fast_invoker.go](../../../../core/fast_invoker.go)，实现者是 `core.BaseAgent` 与 `core.ChainableAgent`；`builder` 构建出的 `ConfigurableAgent` 不实现该接口。程序第 4 部分打印的文字（"AgentBuilder 创建的 Agent 自动享受优化"、基准数据与文档路径）与当前代码不符，以本节为准。
