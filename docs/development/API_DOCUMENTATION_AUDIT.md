# API 文档与源码一致性整理

日期：2026-10-31。源码基线：`033e744`。本轮只修改当前开发入口的文档及文档验证脚本，不改变执行器、工具、Provider、依赖或业务行为。

## 结论与范围

整理前的 API 文档与代码并不完全一致：有已弃用入口作为推荐 API、签名缺字段、构造方式错误，以及不存在的模块/执行保证。当前四份 API 参考、Builder 指南和主要导航已按源码整理。

本轮覆盖19份已有文档：根README两份、docs/README、四份docs/api、三份architecture、DESIGN_OVERVIEW、Builder参考、QUICKSTART、USER_GUIDE、EXECUTION_CONTRACT、COMPOSABLE_ORCHESTRATION、HARNESS_MIGRATION、LLM_PROVIDERS、HOST_INTEGRATION。此页为新增的审查记录。

历史设计、旧版本迁移表、研究和评测保留当时的语义；旧名称在“已删除/迁移”说明中出现不等于当前推荐。没有宣称全仓每个历史示例、所有外部 URL、Provider 当前模型目录或价格均已验证。

## 已修正问题

| 问题 | 核验来源 | 处理 |
| --- | --- | --- |
| 新开发仍被引向 interfaces.Agent/Runnable | interfaces/agent.go 的 Deprecated、core/runnable.go | 核心参考转向 core.Runnable、Builder.AsRunnable；保留兼容说明 |
| core.AgentOutput 与 builder.AgentOutput 混为一谈 | core/agent.go、builder/configurable_agent.go | 明确两种输入/输出类型与显式 Adapt |
| README 把 Tool 写成 Execute(map) | interfaces/tool.go | 改为真实 Invoke 协议并链接工具参考 |
| ToolInput/Output 缺调用身份和附加上下文字段 | interfaces/tool.go | 补齐字段与宿主身份责任 |
| 工具构造和模型路径混淆 | tools子包、dispatcher.go、executor_tool.go | 区分批量 Executor、裸 Invoke、模型 Dispatcher 工具及恢复能力 |
| WithCommunicator 被描述为内置通信能力 | builder/agent_options.go | 明确仅写入 metadata |
| Builder 默认值、选项与永久兼容承诺错误 | builder/builder.go、config_options.go、runtime_options.go | 逐项核对默认值、构建初始化及实际行为，不承诺全部配置都自动生效 |
| LLM 文档缺 ToolStreamClient 与续接消息字段 | llm客户端、消息及Provider源码 | 补齐原生接口、能力检查、usage未知与流取消 |
| 部分Provider构造返回类型错误 | llm/providers构造函数 | 修正类型并编译11种构造签名；不请求真实服务 |
| Middleware 被描述为自动重试handler | core/middleware实现 | 明确当前RetryMiddleware不重新调用handler，模型重试用RequestError |
| Middleware与Process组件或工具Pipeline混用 | core/middleware、顶层middleware、tools/pipeline.go | 区分具体挂载协议与生命周期 |
| 当前文档仍写旧ContextPolicy或遗漏新RequestError边界 | builder/offload.go、loop_hooks.go、model_call.go | 更新当前入口；迁移表保留已删除名称 |
| 组合指南声称不支持嵌套审批 | tools/runnable_tool.go、workflow/durable_runnable.go | 区分默认不透明工具与显式Resumable子图 |
| 架构图列出当前不存在的agents/multiagent/distributed/planning/retrieval目录 | 当前目录、scripts/apicheck/main.go | 改成实际职责图，核心与独立仓库分别标识 |
| 导入文档宣称比检查器实际规则更严格 | scripts/apicheck/main.go | 以真实AST规则为准，不将概念层级当作全部已实现门禁 |
| Harness还被描述为核心拆分未发布 | CHANGELOG v0.27删除记录、当前模块边界 | 明确旧包已移除，独立仓库版本以其自身维护为准 |
| 历史性能数字被当作通用保证 | 现有性能报告与执行契约 | README改为链接带版本/负载的可复现证据 |
| MonitoringAgent 被描述为启动定时监控 | builder/quick_builders.go | 明确构造提示词Agent，不自动运行后台循环 |

## 验证方法与结果

新增 `scripts/verify_doc_examples.py`：检查19份当前入口文档中的本地Markdown链接，并提取含package main及func main的完整Go代码块进行编译。

```bash
env -u GOROOT python3 scripts/verify_doc_examples.py
# 无真实模型请求的已有示例
env -u GOROOT go run ./examples/execution-scope
env -u GOROOT go run ./examples/workflow
env -u GOROOT bash scripts/verify_imports.sh
git diff --check
```

本机PATH工具链与GOROOT不一致，env处理仅限子进程。脚本检查19份文档、编译8个完整main示例，本地链接与编译均通过；临时产物自动清理，不运行付费模型示例。API页的五个新增离线示例另已实际运行通过；Provider构造签名另做仅编译检查。

接口签名展示、需要宿主变量的片段和Mermaid不属于自动编译范围，已逐项阅读源码核验；脚本不检查远端URL或Markdown锚点，不是完整语义证明。

独立复审完成：Runtime职责、contrib工具归属、历史模型清单标题等发现项已修复，复审无剩余P0/P1/P2。核对了19份文档中的412条显式路径/符号引用；这不表示所有自然语言断言或外部事实都已自动证明。此次不跑全仓race：没有改产品Go代码，文档完整示例编译、离线运行和源码复审是直接对应的检查。

## 后续维护

变更公共签名、弃用接口或恢复语义时，同步更新对应API页和迁移说明。新增完整Go示例后运行文档脚本。多份文档避免复制大段接口定义，优先链接源码和专门参考页。
