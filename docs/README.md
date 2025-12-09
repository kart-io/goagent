# GoAgent 文档

GoAgent 是一个全面的 Go AI Agent 框架，提供 Agent、工具、内存、LLM 抽象和编排能力。

## 文档导航

### 快速入门

- **[快速入门指南](guides/QUICKSTART.md)** - 5 分钟快速上手
- **[使用指南](guides/USER_GUIDE.md)** - 完整使用教程

### 架构设计

- **[架构概述](architecture/ARCHITECTURE.md)** - 系统架构和分层设计
- **[组件关系图](architecture/COMPONENT_DIAGRAM.md)** - 组件关系和依赖图（Mermaid）
- **[导入层级说明](architecture/IMPORT_LAYERING.md)** - 模块导入规则

### 设计文档

- **[设计概述](design/DESIGN_OVERVIEW.md)** - 核心设计理念和模式
- **[时序图](design/SEQUENCE_DIAGRAMS.md)** - 详细的交互时序图（Mermaid）
- **[流程图](design/FLOW_DIAGRAMS.md)** - 核心流程图（Mermaid）

### API 参考

- **[核心 API](api/CORE_API.md)** - Agent、Runnable、Builder API
- **[Tool API](api/TOOL_API.md)** - 工具系统 API
- **[LLM API](api/LLM_API.md)** - LLM 客户端 API
- **[Middleware API](api/MIDDLEWARE_API.md)** - 中间件 API

### 使用指南

- **[LLM 提供商指南](guides/LLM_PROVIDERS.md)** - 配置各种 LLM 提供商
- **[Embedder 提供商指南](guides/EMBEDDER_PROVIDERS.md)** - 配置向量嵌入提供商
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

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/llm/providers"
)

func main() {
    // 创建 LLM 客户端
    client, _ := providers.NewOpenAIWithOptions(
        providers.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
        providers.WithModel("gpt-4"),
    )

    // 创建 Agent
    agent, _ := builder.NewSimpleBuilder(client).
        WithSystemPrompt("你是一个有帮助的助手").
        Build()

    // 调用
    output, _ := agent.Invoke(context.Background(), &interfaces.Input{
        Messages: []interfaces.Message{
            {Role: "user", Content: "你好"},
        },
    })

    fmt.Println(output.Messages[0].Content)
}
```

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
│   ├── EMBEDDER_PROVIDERS.md     # Embedder 提供商
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
