# Builder API 分层示例

本目录包含按 API 复杂度分层的 GoAgent Builder 示例代码，帮助用户逐步学习和掌握 Builder API。

---

## 📁 目录结构

```
builder/
├── simple/       # Simple API 示例（5-8 个方法，覆盖 80% 场景）
├── core/         # Core API 示例（15-20 个方法，覆盖 95% 场景）
├── advanced/     # Advanced API 示例（30+ 个方法，覆盖 100% 场景）
└── README.md     # 本文件
```

---

## 🎯 学习路径

### 第 1 步：Simple API（5 分钟）

**目录**: `simple/`

**学习目标**: 掌握最基本的 Agent 创建方法

**包含示例**:
1. ✅ 最简单的 Agent（仅 3 行代码）
2. ✅ 带工具的 Agent
3. ✅ 调整常用配置（MaxIterations, Temperature）
4. ✅ 使用快速构建函数

**运行示例**:
```bash
cd simple/
go run main.go
```

**使用的方法**:
- `WithSystemPrompt` - 设置系统提示词
- `WithTools` - 添加工具
- `WithMaxIterations` - 设置最大步骤数
- `WithTemperature` - 控制创造性
- `Build` - 构建 Agent

**适用场景**: 快速原型、简单应用、学习入门

---

### 第 2 步：Core API（30 分钟）

**目录**: `core/`

**学习目标**: 掌握生产级 Agent 的标准配置

**包含示例**:
1. ✅ 带监控和日志的 Agent（Callbacks）
2. ✅ 带超时和性能控制的 Agent
3. ✅ 带存储和持久化的 Agent
4. ✅ 带错误处理的 Agent

**运行示例**:
```bash
cd core/
go run main.go
```

**新增方法**:
- `WithCallbacks` - 添加回调函数（监控、日志）
- `WithVerbose` - 启用详细日志
- `WithTimeout` - 设置超时时间
- `WithMaxTokens` - 限制 token 数
- `WithStore` - 添加存储
- `WithErrorHandler` - 自定义错误处理

**适用场景**: 生产应用、功能完整的服务、需要监控和持久化的系统

---

### 第 3 步：Advanced API（2 小时）

**目录**: `advanced/`

**学习目标**: 掌握企业级 Agent 的高级配置

**包含示例**:
1. ✅ 带自定义状态的 Agent（泛型）
2. ✅ 带中间件的 Agent（缓存、限流、日志）
3. ✅ 带会话管理的 Agent（SessionID、自动保存）
4. ✅ 完整的企业级配置

**运行示例**:
```bash
cd advanced/
go run main.go
```

**新增方法**:
- `WithState` - 自定义状态类型（泛型）
- `WithContext` - 自定义上下文类型（泛型）
- `WithMiddleware` - 添加中间件
- `WithSessionID` - 设置会话 ID
- `WithAutoSaveEnabled` - 启用自动保存
- `WithSaveInterval` - 设置保存间隔
- `WithMetadata` - 添加元数据
- `WithStreamingEnabled` - 启用流式响应
- `WithTelemetry` - 集成遥测

**适用场景**: 企业级应用、多租户系统、需要细粒度控制的复杂场景

---

## 📊 API 层级对比

| 层级 | 方法数 | 代码行数 | 学习时间 | 适用场景 | 覆盖率 |
|------|--------|----------|----------|----------|--------|
| **Simple** | 5-8 | 3-10 行 | 5 分钟 | 快速原型 | 80% |
| **Core** | 15-20 | 10-20 行 | 30 分钟 | 生产应用 | 95% |
| **Advanced** | 30+ | 20-50 行 | 2 小时 | 企业级 | 100% |

---

## 💡 快速开始

### 配置 API Key

运行示例前需要配置 API Key（选择其一即可）：

```bash
# 方式 1: 使用 DeepSeek
export DEEPSEEK_API_KEY=your-deepseek-key

# 方式 2: 使用 Kimi (月之暗面)
export KIMI_API_KEY=your-kimi-key
```

**获取 API Key**：
- **DeepSeek**: <https://platform.deepseek.com/>
- **Kimi**: <https://platform.moonshot.cn/>

### 如果你是新手...

**从 `simple/` 开始！**

```bash
cd simple/
go run main.go
```

5 分钟内你将创建第一个 Agent：
```go
agent, _ := builder.NewSimpleBuilder(llm).
    WithSystemPrompt("你是一个助手").
    Build()
```

### 如果你需要生产级功能...

**参考 `core/` 示例！**

```bash
cd core/
go run main.go
```

30 分钟内你将掌握：
- 监控和日志
- 超时控制
- 存储和持久化
- 错误处理

### 如果你需要企业级定制...

**学习 `advanced/` 示例！**

```bash
cd advanced/
go run main.go
```

2 小时内你将掌握：
- 自定义状态类型
- 中间件集成
- 会话管理
- 元数据和遥测

---

## 🔗 相关文档

- **Builder API 速查表**: [../../docs/guides/BUILDER_API_REFERENCE.md](../../docs/guides/BUILDER_API_REFERENCE.md)
  - 完整的方法列表
  - 按层级分类
  - 使用示例
  - FAQ

- **快速开始指南**: [../../docs/guides/QUICKSTART.md](../../docs/guides/QUICKSTART.md)
  - 项目介绍
  - 安装步骤
  - 基础概念

- **架构文档**: [../../docs/architecture/CORE_ARCHITECTURE.md](../../docs/architecture/CORE_ARCHITECTURE.md)
  - 系统架构
  - 设计原则
  - 核心组件

---

## 📝 代码示例摘要

### Simple API 示例

```go
// 最简单的 Agent（仅 3 行代码）
agent, err := builder.NewSimpleBuilder(llmClient).
    WithSystemPrompt("你是一个助手").
    Build()
```

### Core API 示例

```go
// 生产级 Agent（带监控和存储）
agent, err := builder.NewSimpleBuilder(llmClient).
    WithSystemPrompt("你是一个助手").
    WithTools(tool1, tool2).
    WithTimeout(3 * time.Minute).
    WithMaxTokens(3000).
    WithCallbacks(stdoutCallback).
    WithStore(redisStore).
    Build()
```

### Advanced API 示例

```go
// 企业级 Agent（完整配置）
type CustomState struct {
    *core.AgentState
    UserProfile map[string]interface{}
}

agent, err := builder.NewAgentBuilder[any, *CustomState](llmClient).
    WithSystemPrompt("你是一个企业级助手").
    WithTools(tool1, tool2, tool3).
    WithMaxIterations(30).
    WithTimeout(10 * time.Minute).
    WithCallbacks(metricsCallback).
    WithStore(pgStore).
    WithState(customState).
    WithMiddleware(cachingMW, rateLimitMW).
    WithSessionID("session-001").
    WithAutoSaveEnabled(true).
    WithMetadata("tenant_id", tenantID).
    Build()
```

---

## 🎓 学习建议

### 按顺序学习

1. **第 1 天**: Simple API（5 分钟）
   - 运行 `simple/main.go`
   - 理解基本概念
   - 尝试修改示例

2. **第 2 天**: Core API（30 分钟）
   - 运行 `core/main.go`
   - 学习监控、存储、错误处理
   - 创建自己的生产级 Agent

3. **第 3 天**: Advanced API（2 小时）
   - 运行 `advanced/main.go`
   - 理解泛型、中间件、会话管理
   - 根据需求定制 Agent

### 实践练习

#### 练习 1: 创建一个翻译 Agent（Simple API）
```go
agent, _ := builder.NewSimpleBuilder(llm).
    WithSystemPrompt("你是一个中英文翻译助手").
    WithTemperature(0.3). // 降低创造性，提高准确性
    Build()
```

#### 练习 2: 添加监控（Core API）
```go
agent, _ := builder.NewSimpleBuilder(llm).
    WithSystemPrompt("你是一个翻译助手").
    WithCallbacks(core.NewStdoutCallback(true)). // 添加监控
    WithTimeout(1 * time.Minute).                // 添加超时
    Build()
```

#### 练习 3: 企业级配置（Advanced API）
```go
agent, _ := builder.NewAgentBuilder[any, *CustomState](llm).
    WithSystemPrompt("你是一个翻译助手").
    WithState(customState).           // 自定义状态
    WithSessionID("session-123").     // 会话管理
    WithMetadata("tenant_id", "001"). // 多租户
    Build()
```

---

## ❓ 常见问题

### Q1: 我应该从哪个示例开始？

**A**: 从 `simple/` 开始！80% 的使用场景只需要 Simple API。

### Q2: Simple、Core、Advanced 有什么区别？

**A**:
- **Simple**: 5-8 个方法，快速上手，覆盖 80% 场景
- **Core**: 15-20 个方法，生产级功能，覆盖 95% 场景
- **Advanced**: 30+ 个方法，企业级定制，覆盖 100% 场景

### Q3: 我需要学习所有三个层级吗？

**A**: 不需要！根据你的需求选择：
- 快速原型 → Simple
- 生产应用 → Core
- 企业级系统 → Advanced

### Q4: 这些示例可以直接用于生产吗？

**A**:
- **Simple**: 不建议直接用于生产（缺少监控、错误处理）
- **Core**: 可以用于生产（有监控、超时、存储）
- **Advanced**: 专为生产设计（完整的企业级功能）

### Q5: 如何选择合适的配置？

**A**: 参考 [Builder API 速查表](../../docs/guides/BUILDER_API_REFERENCE.md) 中的"场景选择指南"。

---

## 🔧 故障排查

### 问题 1: 示例运行失败

```bash
# 确保依赖已安装
go mod download

# 清理并重新编译
go clean -cache
go build ./examples/builder/simple/
```

### 问题 2: LLM 客户端未配置

示例使用 `mockllm.MockLLMClient` 进行演示。生产环境请使用真实的 LLM 客户端：

```go
// 替换为真实的 LLM 客户端
import "github.com/kart-io/goagent/llm/providers/openai"

llmClient := openai.NewClient(apiKey)
```

### 问题 3: 找不到包

```bash
# 确保在项目根目录
cd /path/to/goagent/

# 运行示例
go run examples/builder/simple/main.go
```

---

## 📚 更多资源

- **官方文档**: [https://github.com/kart-io/goagent](https://github.com/kart-io/goagent)
- **API 参考**: [../../docs/guides/BUILDER_API_REFERENCE.md](../../docs/guides/BUILDER_API_REFERENCE.md)
- **社区讨论**: [GitHub Discussions](https://github.com/kart-io/goagent/discussions)
- **问题报告**: [GitHub Issues](https://github.com/kart-io/goagent/issues)

---

**最后更新时间**: 2025-12-04
**适用版本**: GoAgent v1.x
**维护者**: GoAgent Team
