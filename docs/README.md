# GoAgent 文档

当前入口：[扩展契约](guides/EXTENSION_CONTRACT.md)、[执行治理](guides/EXECUTION_GOVERNANCE.md)、[优化验收清单](development/PRIORITY_IMPROVEMENTS.md)。

Harness 装配、编码工具与相关示例已迁至独立项目，见 [迁移指南](guides/HARNESS_MIGRATION.md)。

GoAgent 是一个全面的 Go AI Agent 框架，提供 Agent、工具、内存、LLM 抽象和编排能力。

## 文档导航

### 快速入门

- **[快速入门指南](guides/QUICKSTART.md)** - 5 分钟快速上手
- **[使用指南](guides/USER_GUIDE.md)** - 完整使用教程
- **[通用流程编排](../workflow/README.md)** - 图编译、分支、并行汇合、循环和 Agent 适配

### 架构设计

- **[架构概述](architecture/ARCHITECTURE.md)** - 系统架构和分层设计
- **[组件关系图](architecture/COMPONENT_DIAGRAM.md)** - 组件关系和依赖图（Mermaid）
- **[导入层级说明](architecture/IMPORT_LAYERING.md)** - 模块导入规则

### 设计文档

- **[框架性能对测](evaluation/2026-10-27-framework-performance-v2/REPORT.md)** - GoAgent/Eino 同语言基线与 Python 框架观察，32,000 次有效操作及复跑代码
- **[多框架调研与方案复审](research/2026-10-27-framework-orchestration-review.md)** - Eino、LangGraph/LangChain、OpenAI Agents SDK、Google ADK、Temporal 的依据与取舍
- **[可组合编排需求](superpowers/specs/2026-10-27-composable-orchestration-requirements.md)** - R2：工具化、根预算、上下文与观察；流处理先原型，反射映射后置
- **[可组合编排实现方案](superpowers/plans/2026-10-27-composable-orchestration.md)** - R2 API、阶段依赖、文件改动与实施任务（已执行 R2 首轮，流 API 为实验性）
- **[设计概述](design/DESIGN_OVERVIEW.md)** - 核心设计理念和模式
- **[时序图](design/SEQUENCE_DIAGRAMS.md)** - 详细的交互时序图（Mermaid）
- **[流程图](design/FLOW_DIAGRAMS.md)** - 核心流程图（Mermaid）

### API 参考

- **[核心 API](api/CORE_API.md)** - Agent、Runnable、Builder API
- **[Tool API](api/TOOL_API.md)** - 工具系统 API
- **[LLM API](api/LLM_API.md)** - LLM 客户端 API
- **[Middleware API](api/MIDDLEWARE_API.md)** - 中间件 API

### 使用指南

- **[可组合编排 R2](guides/COMPOSABLE_ORCHESTRATION.md)** - 根预算、子 Agent 工具、持久上下文引用与只读观察

- **[LLM 提供商指南](guides/LLM_PROVIDERS.md)** - 配置各种 LLM 提供商
- **[Embedder 提供商指南](https://github.com/kart-io/goagent-contrib/blob/master/retrieval/EMBEDDER_PROVIDERS.md)** - 配置向量嵌入提供商（goagent-contrib）
- **[Provider 最佳实践](guides/PROVIDER_BEST_PRACTICES.md)** - Provider 使用最佳实践
- **[插件系统指南](guides/PLUGIN_SYSTEM_GUIDE.md)** - 插件开发和使用
- **[工具中间件指南](guides/TOOL_MIDDLEWARE.md)** - 工具中间件使用
- **[缓存指南](guides/CACHING_GUIDE.md)** - 缓存配置和使用
- **[Builder API 参考](guides/BUILDER_API_REFERENCE.md)** - Builder 完整参考

### 开发规范

- **[开发指南](development/DEVELOPMENT_GUIDE.md)** - 开发环境和贡献指南
- **[测试最佳实践](development/TESTING_BEST_PRACTICES.md)** - 测试编写规范
- **[Panic 处理指南](development/PANIC_HANDLING.md)** - Panic 处理开发指南

## 核心概念

### Agent 类型

| Agent 类型 | 描述 | 适用场景 |
|-----------|------|----------|
| **ExecutorAgent** | 工具执行 Agent | 需要调用工具完成任务 |
| **ReActAgent** | ReAct 推理 Agent | 复杂推理 + 工具调用 |
| **CoTAgent** | 思维链 Agent | 需要逐步推理 |
| **ToTAgent** | 思维树 Agent | 需要探索多种方案 |
| **GoTAgent** | 思维图 Agent | 复杂依赖关系 |
| **PoTAgent** | 程序思维 Agent | 结构化问题 |
| **SoTAgent** | 骨架思维 Agent | 需要先规划后执行 |
| **MetaCoTAgent** | 元思维链 Agent | 需要自适应推理 |

### LLM 提供商

| 提供商 | 说明 | 模型示例 |
|-------|------|----------|
| OpenAI | GPT 系列 | gpt-4, gpt-4-turbo, gpt-3.5-turbo |
| Anthropic | Claude 系列 | claude-3-opus, claude-3-sonnet |
| Google Gemini | Gemini 系列 | gemini-pro, gemini-1.5-pro |
| DeepSeek | 高性价比 | deepseek-chat, deepseek-coder |
| Ollama | 本地部署 | llama3, mistral, codellama |
| Kimi | 月之暗面 | moonshot-v1-8k, moonshot-v1-128k |
| SiliconFlow | 国内服务 | 多种模型 |
| Cohere | Cohere 系列 | command, command-light |
| HuggingFace | HF 推理 | 多种开源模型 |

## 快速示例

推荐入口、完整可运行代码和旧接口迁移说明统一维护在[快速入门](guides/QUICKSTART.md)。

- [执行契约与能力状态](guides/EXECUTION_CONTRACT.md)
- [工具上下文与历史摘要](guides/CONTEXT_POLICY.md)


## 文档结构

```
docs/
├── README.md                      # 本文件 - 文档索引
├── api/                           # API 参考
│   ├── CORE_API.md               # 核心 API
│   ├── TOOL_API.md               # 工具 API
│   ├── LLM_API.md                # LLM API
│   └── MIDDLEWARE_API.md         # 中间件 API
├── architecture/                  # 架构文档
│   ├── ARCHITECTURE.md           # 架构概述
│   ├── COMPONENT_DIAGRAM.md      # 组件关系图
│   └── IMPORT_LAYERING.md        # 导入层级
├── design/                        # 设计文档
│   ├── DESIGN_OVERVIEW.md        # 设计概述
│   ├── SEQUENCE_DIAGRAMS.md      # 时序图
│   └── FLOW_DIAGRAMS.md          # 流程图
├── guides/                        # 使用指南
│   ├── QUICKSTART.md             # 快速入门
│   ├── USER_GUIDE.md             # 使用指南
│   ├── LLM_PROVIDERS.md          # LLM 提供商
│   ├── PROVIDER_BEST_PRACTICES.md # Provider 最佳实践
│   ├── PLUGIN_SYSTEM_GUIDE.md    # 插件系统
│   ├── TOOL_MIDDLEWARE.md        # 工具中间件
│   ├── CACHING_GUIDE.md          # 缓存指南
│   └── BUILDER_API_REFERENCE.md  # Builder 参考
└── development/                   # 开发规范
    ├── DEVELOPMENT_GUIDE.md      # 开发指南
    ├── TESTING_BEST_PRACTICES.md # 测试最佳实践
    └── PANIC_HANDLING.md         # Panic 处理
```

## 图表说明

本文档中的所有图表均使用 [Mermaid](https://mermaid.js.org/) 语法编写，可在支持 Mermaid 的 Markdown 渲染器中直接查看，包括：

- GitHub
- GitLab
- VS Code（需安装插件）
- Typora
- Notion

## 相关链接

- **[项目 README](../README.md)** - 项目主页
- **[更新日志](../CHANGELOG.md)** - 版本更新记录
- **[示例代码](../examples/)** - 完整示例

## 贡献文档

欢迎贡献文档改进！请参阅 [开发指南](development/DEVELOPMENT_GUIDE.md) 了解贡献流程。
