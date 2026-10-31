# Builder API 分层示例

按配置复杂度分层的 `builder` 包示例。`simple`、`core`、`advanced` 各包含 5 个示例，均需真实模型。

| 目录 | 内容 | 用到的方法 |
| --- | --- | --- |
| [simple/](simple/main.go) | 最简 Agent、带工具、常用配置、`QuickAgent`/`ChatAgent`、输出格式 | `NewSimpleBuilder`、`WithSystemPrompt`、`WithTools`、`WithMaxIterations`、`WithTemperature`、`WithOutputFormat`、`WithCustomOutputFormat` |
| [core/](core/main.go) | 回调监控、超时与 token 控制、对话记忆多轮对话、错误处理、输出格式 | 上表加 `WithCallbacks`、`WithVerbose`、`WithTimeout`、`WithMaxTokens`、`WithMemory`、`WithSessionID`、`WithMaxConversationHistory`、`WithErrorHandler` |
| [advanced/](advanced/main.go) | 泛型自定义状态、带中间件（日志、限流、缓存）的 Agent、会话管理、企业级配置（回调 + 存储 + 元数据）、输出格式 | 上表加 `NewAgentBuilder[C, S]`、`WithState`、`WithMiddleware`、`WithStore`、`WithAutoSaveEnabled`、`WithMetadata` |
| [serialization/](serialization/) | 用 `WithInputSerializer` 在输入发给模型前脱敏 | `WithInputSerializer` |

## 运行

`simple`、`core`、`advanced` 优先使用 `DEEPSEEK_API_KEY`（模型 `deepseek-chat`），未设置时使用 `KIMI_API_KEY`（模型 `moonshot-v1-8k`）；两者都未设置时打印配置提示后退出。

```bash
export DEEPSEEK_API_KEY=your-key   # 或 export KIMI_API_KEY=your-key
go run ./examples/builder/simple
go run ./examples/builder/core
go run ./examples/builder/advanced
```

## 代码摘要

```go
// simple：最简 Agent
agent, err := builder.NewSimpleBuilder(llmClient).
    WithSystemPrompt("你是一个翻译助手，专门将中文翻译成英文。").
    WithOutputFormat(builder.OutputFormatPlainText).
    Build()

// core：对话记忆
memMgr := memory.NewInMemoryManager(memory.DefaultConfig())
agent, err := builder.NewSimpleBuilder(llmClient).
    WithMemory(memMgr).
    WithSessionID(sessionID).
    WithMaxConversationHistory(20).
    Build()

// advanced：自定义状态类型
agent, err := builder.NewAgentBuilder[any, *CustomState](llmClient).
    WithState(customState).
    WithStore(memoryStore).
    WithMetadata("tenant_id", "tenant-001").
    Build()
```

完整方法列表见 [Builder API 速查表](../../docs/guides/BUILDER_API_REFERENCE.md)。
