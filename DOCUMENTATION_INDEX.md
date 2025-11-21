# GoAgent 文档索引

## 概述

本文档提供 GoAgent 项目所有文档的索引和导航指南。

## 项目简介

GoAgent 是一个全面的、生产就绪的 Go AI Agent 框架，灵感来自 LangChain。它提供：

- **多种 Agent 类型**：ReAct、CoT、ToT、GoT 等推理模式
- **工具系统**：可扩展的工具执行框架
- **内存管理**：对话历史和案例推理
- **多 LLM 支持**：OpenAI、Anthropic、Gemini、DeepSeek 等
- **可观测性**：OpenTelemetry 集成
- **分布式支持**：Redis、PostgreSQL、NATS

## 文档结构

```text
docs/
├── architecture/           # 架构文档
│   ├── ARCHITECTURE.md     # 架构概述
│   └── IMPORT_LAYERING.md  # 导入层级说明
├── guides/                 # 用户指南
│   ├── QUICKSTART.md       # 快速入门
│   └── LLM_PROVIDERS.md    # LLM 提供商指南
└── development/            # 开发文档
    └── TESTING_BEST_PRACTICES.md  # 测试最佳实践
```

## 核心文档

### 架构文档

| 文档 | 说明 | 适合阅读者 |
|------|------|-----------|
| [架构概述](docs/architecture/ARCHITECTURE.md) | 系统整体架构和设计原则 | 所有开发者 |
| [导入层级说明](docs/architecture/IMPORT_LAYERING.md) | 4 层架构的导入规则 | 贡献者 |

### 用户指南

| 文档 | 说明 | 适合阅读者 |
|------|------|-----------|
| [快速入门](docs/guides/QUICKSTART.md) | 从零开始使用 GoAgent | 新用户 |
| [LLM 提供商指南](docs/guides/LLM_PROVIDERS.md) | 配置不同的 LLM 提供商 | 所有用户 |

### 开发文档

| 文档 | 说明 | 适合阅读者 |
|------|------|-----------|
| [测试最佳实践](docs/development/TESTING_BEST_PRACTICES.md) | 测试指南和模式 | 贡献者 |

## 核心概念

### Agent 类型

GoAgent 支持多种推理模式的 Agent：

| Agent | 位置 | 说明 |
|-------|------|------|
| ExecutorAgent | `agents/executor/` | 工具执行 |
| ReActAgent | `agents/react/` | 思考-行动-观察循环 |
| CoTAgent | `agents/cot/` | 思维链推理 |
| ToTAgent | `agents/tot/` | 思维树搜索 |
| GoTAgent | `agents/got/` | 思维图推理 |
| PoTAgent | `agents/pot/` | 程序思维 |
| SoTAgent | `agents/sot/` | 骨架思维 |
| MetaCoTAgent | `agents/metacot/` | 元思维链 |

### 核心接口

| 接口 | 位置 | 说明 |
|------|------|------|
| Agent | `interfaces/agent.go` | Agent 接口定义 |
| Runnable | `interfaces/agent.go` | 可执行组件接口 |
| Tool | `interfaces/tool.go` | 工具接口 |
| MemoryManager | `interfaces/memory.go` | 内存管理接口 |
| Checkpointer | `interfaces/checkpoint.go` | 检查点接口 |

### LLM 提供商

| 提供商 | 位置 | 说明 |
|-------|------|------|
| OpenAI | `llm/providers/openai.go` | GPT 系列 |
| Anthropic | `llm/providers/anthropic.go` | Claude 系列 |
| Gemini | `llm/providers/gemini.go` | Google Gemini |
| DeepSeek | `llm/providers/deepseek.go` | DeepSeek |
| Cohere | `llm/providers/cohere.go` | Cohere |
| HuggingFace | `llm/providers/huggingface.go` | HuggingFace |
| Ollama | `llm/providers/ollama.go` | 本地部署 |
| SiliconFlow | `llm/providers/siliconflow.go` | SiliconFlow |
| Kimi | `llm/providers/kimi.go` | 月之暗面 |

## 示例代码

示例代码位于 `examples/` 目录：

### 基础示例

| 示例 | 位置 | 说明 |
|------|------|------|
| 简单 Agent | `examples/basic/01-simple-agent/` | 基本 Agent 使用 |
| 工具使用 | `examples/basic/02-tools/` | 工具集成 |
| 内存管理 | `examples/basic/03-agent-with-memory/` | 对话记忆 |
| Ollama | `examples/basic/04-ollama-agent/` | 本地 LLM |
| 提供商一致性 | `examples/basic/05-provider-consistency/` | 多提供商 |
| 所有提供商 | `examples/basic/06-all-providers/` | 提供商示例 |
| 智能 Agent | `examples/basic/07-smart-agent-with-tools/` | 完整示例 |

### 高级示例

| 示例 | 位置 | 说明 |
|------|------|------|
| ReAct | `examples/advanced/react/` | ReAct 推理 |
| 流式输出 | `examples/advanced/streaming/` | 流式响应 |
| 多模式流式 | `examples/advanced/multi-mode-streaming/` | 多种流式模式 |
| 并行执行 | `examples/advanced/parallel-execution/` | 并行工具 |
| 工具选择器 | `examples/advanced/tool-selector/` | 智能工具选择 |
| 工具运行时 | `examples/advanced/tool-runtime/` | 工具运行时管理 |
| 可观测性 | `examples/advanced/observability/` | OpenTelemetry |
| 多 Agent | `examples/advanced/multi-agent-*/` | 多 Agent 协作 |

### 集成示例

| 示例 | 位置 | 说明 |
|------|------|------|
| LangChain 完整 | `examples/integration/langchain-complete/` | 完整 LangChain 风格 |
| 多 Agent | `examples/integration/multiagent/` | 多 Agent 系统 |
| Human-in-Loop | `examples/integration/human-in-loop/` | 人机交互 |
| 预配置 Agent | `examples/integration/preconfig-agents/` | 预设 Agent |

## 开发命令

### 常用命令

```bash
# 测试
make test
make test-short
make coverage

# 代码质量
make fmt
make lint
make vet
make check

# 构建
make build
make build-all

# 依赖管理
make deps
make deps-update
make mod-tidy

# 导入验证
./verify_imports.sh
./verify_imports.sh --strict
```

### 提交前检查清单

```bash
make fmt              # 格式化代码
make lint             # 检查问题
./verify_imports.sh   # 验证导入层级
make test             # 运行测试
```

## 包目录

### 第 1 层：基础

- `interfaces/` - 公共接口
- `errors/` - 错误类型
- `cache/` - 缓存工具
- `utils/` - 工具函数

### 第 2 层：业务逻辑

- `core/` - 核心实现
- `builder/` - Agent 构建器
- `llm/` - LLM 客户端
- `memory/` - 内存管理
- `store/` - 存储实现
- `observability/` - 可观测性
- `prompt/` - 提示工程

### 第 3 层：实现

- `agents/` - Agent 实现
- `tools/` - 工具实现
- `middleware/` - 中间件
- `parsers/` - 解析器
- `stream/` - 流处理
- `multiagent/` - 多 Agent
- `distributed/` - 分布式
- `document/` - 文档处理
- `mcp/` - MCP 协议

## 外部资源

- **GitHub**：[https://github.com/kart-io/goagent](https://github.com/kart-io/goagent)
- **Go Doc**：查看代码注释
- **CHANGELOG**：版本更新记录

## 快速链接

- [快速入门](docs/guides/QUICKSTART.md) - 立即开始
- [架构概述](docs/architecture/ARCHITECTURE.md) - 了解设计
- [LLM 提供商](docs/guides/LLM_PROVIDERS.md) - 配置 LLM
- [测试实践](docs/development/TESTING_BEST_PRACTICES.md) - 编写测试

## 贡献指南

1. Fork 项目
2. 创建特性分支
3. 编写代码和测试
4. 运行 `make check` 和 `./verify_imports.sh`
5. 提交 PR

详细信息请参考 [CLAUDE.md](CLAUDE.md)。
