# GoAgent 文档

GoAgent 是一个全面的 Go AI Agent 框架，提供 Agent、工具、内存、LLM 抽象和编排能力。

## 文档结构

### 快速入门

- [QUICKSTART.md](guides/QUICKSTART.md) - 快速入门指南，包含基础示例

### 架构

- [ARCHITECTURE.md](architecture/ARCHITECTURE.md) - 系统架构概述
- [IMPORT_LAYERING.md](architecture/IMPORT_LAYERING.md) - 导入层级说明

### 使用指南

- [LLM_PROVIDERS.md](guides/LLM_PROVIDERS.md) - LLM 提供商配置指南
- [EMBEDDER_PROVIDERS.md](guides/EMBEDDER_PROVIDERS.md) - Embedder 提供商配置指南
- [PROVIDER_BEST_PRACTICES.md](guides/PROVIDER_BEST_PRACTICES.md) - Provider 最佳实践
- [PLUGIN_SYSTEM_GUIDE.md](guides/PLUGIN_SYSTEM_GUIDE.md) - 插件系统完整指南
- [TOOL_MIDDLEWARE.md](guides/TOOL_MIDDLEWARE.md) - 工具中间件使用指南

### 开发规范

- [TESTING_BEST_PRACTICES.md](development/TESTING_BEST_PRACTICES.md) - 测试最佳实践
- [PANIC_HANDLING.md](development/PANIC_HANDLING.md) - Panic 处理开发指南

## 核心概念

### Agent

Agent 是能够推理、使用工具和做出决策的自主实体。GoAgent 支持多种 Agent 类型：

- **ExecutorAgent** - 工具执行 Agent
- **ReActAgent** - ReAct 推理 Agent
- **CoTAgent** - 思维链 Agent

### Builder 模式

流式 `AgentBuilder` 是构建 Agent 的主要方式：

```go
agent := builder.NewAgentBuilder(llmClient).
    WithSystemPrompt("你是一个有帮助的助手").
    WithTools(searchTool, calcTool).
    WithMemory(memoryManager).
    Build()
```

### LLM 提供商

支持多种 LLM 提供商：

| 提供商 | 说明 |
|-------|------|
| OpenAI | GPT 系列 |
| Anthropic | Claude 系列 |
| Google Gemini | Gemini 系列 |
| DeepSeek | 高性价比 |
| Ollama | 本地部署 |
| SiliconFlow | 国内服务 |
| Kimi | 月之暗面 |

详细配置请参考 [LLM 提供商指南](guides/LLM_PROVIDERS.md)。

## 示例代码

完整示例请参考项目 `examples/` 目录：

```
examples/
├── basic/          # 基础示例
├── advanced/       # 高级示例
└── integration/    # 集成示例
```

## 相关链接

- [项目 README](../README.md)
- [更新日志](../CHANGELOG.md)
- [发布说明](../RELEASE_NOTES.md)
