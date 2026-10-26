# GoAgent / Eino 对照与编排验证

独立 Go 模块固定 Eino v0.9.22 和 eino-ext OpenAI v0.1.13。主模块不依赖 Eino；`replace ../..` 使用当前 GoAgent 工作区。根目录测试不会进入此模块，需单独运行。

```sh
cd evaluation/einocompare
go test ./... -count=1
go run ./cmd/workflow
go run ./cmd/compare -count 24 -out /tmp/goagent-eino-preview
```

默认命令只生成相同题集及 manifest，不请求模型。流程示例无需 API Key：验证输入后，Eino 编译图根据前缀选择现有 GoAgent Runnable，输出 `HELLO` 和 `alternate:question`。测试覆盖两条分支、空输入、业务错误和取消。适配调用 Invoke，边传递最终结果，不提供跨节点 Token Transform 或持久化恢复。

## 同条件在线评测

```sh
OPENAI_API_KEY=... OPENAI_BASE_URL=https://api.deepseek.com \
OPENAI_MODEL=deepseek-flash go run ./cmd/compare -live \
  -mode contract -count 120 -out /tmp/goagent-eino-live

# 或使用业务标注，格式与 agentbench SuiteCase 一致：
go run ./cmd/compare -live -mode baseline \
  -dataset /path/to/labeled-cases.json -out /tmp/goagent-eino-business
```

`baseline` 全部采用普通工具循环；`contract` 根据预先声明的 direct_eligible 选择直返；`all` 对直返适用题同时运行两种配置。每个配置分别运行 GoAgent 和 Eino ADK ChatModelAgent，逐任务轮换框架顺序，串行执行。参数 MaxCalls、MaxTokens、温度、超时、工具资料、系统提示和评分相同。Eino 使用原生 Provider 与 ADK，不通过 GoAgent 模拟 Eino 循环。

统一代理 Meter 统计上游模型请求和真实用量，并限制重试在内的请求数；Token 阈值只能阻止后续请求，不是硬性计费上限。Eino 不同执行器的内部提示格式及最后一轮工具选择语义可能不同，代理不重写这些框架行为。此处比较整条任务路径，不能作为纯框架性能排名。

输出 dataset.json、manifest.json、即时写入的 results.jsonl 和 summary.json。输出目录必须不存在；失败保留在分母和用量内，执行完后返回非零状态。中断保留已有结果和部分汇总。manifest 不写 API Key 或上游地址；数据文件仍可能包含业务资料，应自行选择保存位置。

离线测试以同一假 HTTP 上游验证真实 SDK 的普通回答、工具循环、Unicode 多行原文直返、预算耗尽、上游失败和取消。它证明协议与计量链路，不证明模型质量、生产可用性或成本优势。当前尚未使用真实业务标签完成在线比较。

## 引入 Eino 的决策

此验证支持将现有 GoAgent Runnable 封装为 Eino Lambda，而无需先迁移业务工具和检索器。主模块现有 [workflow.Graph](../../workflow/README.md)已覆盖同状态类型的分支、显式并行汇合和有界循环；需要异构字段映射、图恢复或跨节点流处理的业务可继续在此模块验证 Eino。普通 RAG 和工具查询继续使用 AsRunnable，主模块不引入 Eino 的调度状态和消息类型。
