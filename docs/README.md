# GoAgent 文档

GoAgent 是嵌入 Go 服务的 Agent 执行内核：原生工具循环、类型化工作流图、持久执行与审批恢复、请求预算、会话日志、只读观察与 MCP。Preset、编码工具等装配能力位于独立项目 goagent-harness（见[迁移指南](guides/HARNESS_MIGRATION.md)），检索器与多实例持久存储位于 [goagent-contrib](https://github.com/kart-io/goagent-contrib)，业务案例位于 [goagent-examples](https://github.com/kart-io/goagent-examples)。

## 入门

- [快速入门](guides/QUICKSTART.md)：`AsRunnable` 入口、工具、RAG、流式调用与旧接口迁移
- [使用指南](guides/USER_GUIDE.md)：工具、记忆、中间件、错误处理
- [Builder API 参考](guides/BUILDER_API_REFERENCE.md)：Builder 方法、默认值与循环扩展点
- [执行契约](guides/EXECUTION_CONTRACT.md)：各执行入口的能力与边界

## 编排与持久执行

- [原生编排与工具体系](guides/NATIVE_ORCHESTRATION.md)：`workflow.Graph`、持久图、Dispatcher 与审批恢复
- [通用流程编排](../workflow/README.md)：图编译、分支、并行汇合、有界循环与 Agent 适配
- [可组合编排](guides/COMPOSABLE_ORCHESTRATION.md)：根预算、子 Agent 工具、持久上下文引用与只读观察
- [会话日志](../core/session/README.md)：事件日志、分叉、轨迹导出
- [上下文治理](guides/CONTEXT_POLICY.md)：工具结果卸载与自定义 PreStep
- [基于证据的 RAG 与流式](guides/GROUNDED_RAG_AND_STREAMING.md)

## 宿主接入与治理

- [宿主标准接入](guides/HOST_INTEGRATION.md)：Scope、BuildContext、恢复身份与资源生命周期
- [扩展契约](guides/EXTENSION_CONTRACT.md)
- [执行治理](guides/EXECUTION_GOVERNANCE.md)：预算、诊断与资源治理
- [MCP 与可恢复步骤](guides/PRIORITY_CAPABILITIES.md)
- [工具中间件](guides/TOOL_MIDDLEWARE.md)、[缓存](guides/CACHING_GUIDE.md)、[插件系统](guides/PLUGIN_SYSTEM_GUIDE.md)
- [Option 模式](guides/OPTION_PATTERN_BEST_PRACTICES.md)

## LLM 提供商

- [LLM 提供商指南](guides/LLM_PROVIDERS.md)、[能力对照](guides/PROVIDER_CAPABILITIES.md)、[最佳实践](guides/PROVIDER_BEST_PRACTICES.md)、[持续回归](guides/PROVIDER_REGRESSION.md)
- [Embedder 提供商](https://github.com/kart-io/goagent-contrib/blob/master/retrieval/EMBEDDER_PROVIDERS.md)（goagent-contrib）

| 提供商 | 构造函数（`llm/providers`） | `AsRunnable` 与原生工具流 |
|---|---|---|
| OpenAI 及兼容端点 | `NewOpenAIWithOptions` | 支持 |
| DeepSeek | `NewDeepSeekWithOptions` | 支持 |
| Gemini | `NewGeminiWithOptions` | 支持 |
| Anthropic | `NewAnthropicWithOptions` | 不支持，经 `Execute` / `ExecuteWithTools` 调用 |
| Kimi | `NewKimiWithOptions` | 不支持，经 `Execute` / `ExecuteWithTools` 调用 |
| SiliconFlow | `NewSiliconFlowWithOptions` | 不支持，经 `Execute` / `ExecuteWithTools` 调用 |
| Ollama | `NewOllamaWithOptions` | 不支持，经 `Execute` / `ExecuteWithTools` 调用 |
| Cohere | `NewCohereWithOptions` | 不支持，经 `Execute` / `ExecuteWithTools` 调用 |
| Hugging Face | `NewHuggingFaceWithOptions` | 不支持，经 `Execute` / `ExecuteWithTools` 调用 |

`AsRunnable`、`ExecuteStream` 需要 `llm.StreamClient`；原生工具循环、`Start` 句柄与循环扩展点需要 `llm.ToolStreamClient`。表中“支持”的三个 Provider 同时实现两者。

## 架构与设计

- [架构概述](architecture/ARCHITECTURE.md)、[组件关系图](architecture/COMPONENT_DIAGRAM.md)、[导入层级](architecture/IMPORT_LAYERING.md)
- [设计概述](design/DESIGN_OVERVIEW.md)、[时序图](design/SEQUENCE_DIAGRAMS.md)、[流程图](design/FLOW_DIAGRAMS.md)
- [Harness 核心设计](design/HARNESS_CORE_DESIGN.md)
- [多框架调研与方案复审](research/2026-10-27-framework-orchestration-review.md)
- [可组合编排需求](superpowers/specs/2026-10-27-composable-orchestration-requirements.md)与[实现方案](superpowers/plans/2026-10-27-composable-orchestration.md)

## API 参考

- [核心 API](api/CORE_API.md)、[Tool API](api/TOOL_API.md)、[LLM API](api/LLM_API.md)、[Middleware API](api/MIDDLEWARE_API.md)
- 完整签名以 [pkg.go.dev](https://pkg.go.dev/github.com/kart-io/goagent) 或 `go doc` 为准

## 评测

- [执行开销实测](evaluation/2026-10-31-execution-overhead/REPORT.md)：三种预算模式与可复现原始结果
- [框架性能对测](evaluation/2026-10-27-framework-performance-v2/REPORT.md)：GoAgent/Eino 基线与复跑代码
- [GoAgent 评测](../evaluation/agentbench/README.md)

## 开发

- [开发指南](development/DEVELOPMENT_GUIDE.md)、[测试最佳实践](development/TESTING_BEST_PRACTICES.md)、[Panic 处理](development/PANIC_HANDLING.md)
- [优化验收清单](development/PRIORITY_IMPROVEMENTS.md)

## 相关链接

- [项目 README](../README.md)
- [更新日志](../CHANGELOG.md)
- [示例代码](../examples/README.md)
