# GoAgent 文档

GoAgent 是嵌入 Go 服务的 Agent 执行内核：原生工具循环、类型化工作流图、持久执行与审批恢复、请求预算、会话日志、只读观察与 MCP。Preset、编码工具等装配能力位于独立项目 goagent-harness（见[迁移指南](guides/HARNESS_MIGRATION.md)），检索器与多实例持久存储位于 [goagent-contrib](https://github.com/kart-io/goagent-contrib)，业务案例位于 [goagent-examples](https://github.com/kart-io/goagent-examples)。

## 入门

- [快速入门](guides/QUICKSTART.md)：`AsRunnable` 入口、工具、RAG、流式调用与旧接口迁移
- [使用指南](guides/USER_GUIDE.md)：工具、记忆、中间件、错误处理
- [Builder API 参考](guides/BUILDER_API_REFERENCE.md)：Builder 方法、默认值与循环扩展点
- [执行契约](guides/EXECUTION_CONTRACT.md)：各执行入口的能力与边界

- [统一运行管理](../management/README.md)：Agent、Session、Task、Approval、Worker 与只读运行观测；[迁移指南](guides/MANAGEMENT_MIGRATION.md)

## 编排与持久执行

- [人工介入：暂停、恢复与检查点](guides/HUMAN_IN_THE_LOOP.md)：人工输入、状态修改、审批分支与跨进程恢复

- [原生编排与工具体系](guides/NATIVE_ORCHESTRATION.md)：`workflow.Graph`、持久图、Dispatcher 与审批恢复
- [通用流程编排](../workflow/README.md)：图编译、分支、并行汇合、有界循环与 Agent 适配
- [可组合编排](guides/COMPOSABLE_ORCHESTRATION.md)：根预算、子 Agent 工具、持久上下文引用与只读观察
- [会话日志](../core/session/README.md)：事件日志、分叉、轨迹导出
- [上下文治理](guides/CONTEXT_POLICY.md)：工具结果卸载与自定义 PreStep
- [基于证据的 RAG 与流式](guides/GROUNDED_RAG_AND_STREAMING.md)

## 宿主接入与治理

- [宿主标准接入](guides/HOST_INTEGRATION.md)：Scope、BuildContext、恢复身份与资源生命周期
- [HTTP/SSE/WebSocket、Webhook与Hooks](guides/HTTP_AGENT.md)：安装、页面/客户端案例、宿主装配、可替换连接接口与正式文档入口
- [扩展契约](guides/EXTENSION_CONTRACT.md)
- [执行治理](guides/EXECUTION_GOVERNANCE.md)：预算、诊断与资源治理
- [MCP 与可恢复步骤](guides/PRIORITY_CAPABILITIES.md)
- [工具中间件](guides/TOOL_MIDDLEWARE.md)、[缓存](guides/CACHING_GUIDE.md)、[插件：Agent 能力包与 Runnable 注册表](guides/PLUGIN_SYSTEM_GUIDE.md)
- [Option 模式](guides/OPTION_PATTERN_BEST_PRACTICES.md)

## LLM 提供商

- [LLM 提供商指南](guides/LLM_PROVIDERS.md)、[能力对照](guides/PROVIDER_CAPABILITIES.md)、[最佳实践](guides/PROVIDER_BEST_PRACTICES.md)、[持续回归](guides/PROVIDER_REGRESSION.md)
- [MiniMax Anthropic 协议接入](guides/MINIMAX_ANTHROPIC.md)：国内端点、显式凭据配置、原生工具续接与真实验证边界
- [Embedder 提供商](https://github.com/kart-io/goagent-contrib/blob/master/retrieval/EMBEDDER_PROVIDERS.md)（goagent-contrib）

| 提供商 | 构造函数（`llm/providers`） | `AsRunnable` 与原生工具流 |
|---|---|---|
| OpenAI 及兼容端点 | `NewOpenAIWithOptions` | 支持 |
| DeepSeek | `NewDeepSeekWithOptions` | 支持 |
| Gemini | `NewGeminiWithOptions` | 支持 |
| Anthropic | `NewAnthropicWithOptions` | 支持（官方 SDK） |
| Kimi | `NewKimiWithOptions` | 支持（OpenAI 兼容预设） |
| SiliconFlow | `NewSiliconFlowWithOptions` | 支持（OpenAI 兼容预设） |
| Ollama | `NewOllamaWithOptions` | 支持（OpenAI 兼容预设） |

`AsRunnable`、`ExecuteStream` 接受任意 `llm.Client`；原生工具循环、`Start` 句柄与循环扩展点需要 `llm.ToolStreamClient`。表中“支持”的 Provider 同时实现两者。

## 架构与设计

- [架构概述](architecture/ARCHITECTURE.md)、[组件关系图](architecture/COMPONENT_DIAGRAM.md)、[导入层级](architecture/IMPORT_LAYERING.md)
- [设计概述](design/DESIGN_OVERVIEW.md)、[时序图](design/SEQUENCE_DIAGRAMS.md)、[流程图](design/FLOW_DIAGRAMS.md)
- [Harness 核心设计](design/HARNESS_CORE_DESIGN.md)
- [HTTP Agent设计](design/HTTP_AGENT_DESIGN.md)：内核与网络模块边界、执行与接纳/通知的资源责任
- [多框架调研与方案复审](research/2026-10-27-framework-orchestration-review.md)
- [Eino 对照：组件、编排、ADK 与扩展生态](research/2026-11-04-eino-comparison.md)
- [可组合编排需求](superpowers/specs/2026-10-27-composable-orchestration-requirements.md)与[实现方案](superpowers/plans/2026-10-27-composable-orchestration.md)
- [统一管理需求](superpowers/specs/2026-11-08-unified-management-requirements.md)、[可行性调研](research/2026-11-08-unified-management-feasibility.md)、[技术设计](superpowers/specs/2026-11-08-unified-management-design.md)与[实施计划](superpowers/plans/2026-11-08-unified-management.md)：管理模块已在 contrib 仓库实现，GoAgent 核心已发布 v0.51.0，见[实施验收](evaluation/2026-11-08-unified-management/REPORT.md)
- [持久事实记忆服务需求](superpowers/specs/2026-11-08-persistent-factual-memory-requirements.md)：定义公开可扩展接口、外部 Provider 注入、数据隔离、持久化、管理和上下文召回验收
- [持久事实记忆服务架构与设计](superpowers/specs/2026-11-08-persistent-factual-memory-design.md)：双层扩展边界、Builder 集成、索引同步，以及保存、召回、替代和删除流程图
- [持久事实记忆服务实施计划](superpowers/plans/2026-11-08-persistent-factual-memory.md)：PM-01～PM-35 的任务拆分、接口验收、维护恢复和端到端案例
- [持久事实记忆实现与验收](../examples/fact-memory/README.md)：外部 Provider 扩展、PostgreSQL/pgvector 示例、跨进程重启测试与运行命令

## API 参考

- [核心 API](api/CORE_API.md)、[Tool API](api/TOOL_API.md)、[LLM API](api/LLM_API.md)、[Middleware API](api/MIDDLEWARE_API.md)
- 完整签名以 [pkg.go.dev](https://pkg.go.dev/github.com/kart-io/goagent) 或 `go doc` 为准

## 评测

- [执行开销实测](evaluation/2026-10-31-execution-overhead/REPORT.md)：三种预算模式与可复现原始结果
- [框架性能对测](evaluation/2026-10-27-framework-performance-v2/REPORT.md)：GoAgent/Eino 基线与复跑代码
- [GoAgent 评测](../evaluation/agentbench/README.md)

## 开发

- [开发指南](development/DEVELOPMENT_GUIDE.md)、[测试最佳实践](development/TESTING_BEST_PRACTICES.md)、[Panic 处理](development/PANIC_HANDLING.md)
- [业务接入与故障恢复验收](development/BUSINESS_ACCEPTANCE.md)：授权、业务幂等、崩溃恢复、数据库接管与验收证据
- [优化验收清单](development/PRIORITY_IMPROVEMENTS.md)

## 相关链接

- [项目 README](../README.md)
- [更新日志](../CHANGELOG.md)
- [示例代码](../examples/README.md)
