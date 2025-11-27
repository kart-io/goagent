# DeepSeek Agent 简化示例

基于 [go-kratos/blades](https://github.com/go-kratos/blades) 的设计理念优化的简洁示例。

## 设计理念

- **简洁的 API** - 最少的代码实现功能
- **辅助函数** - 封装常用模式
- **专注核心** - 每个示例一个功能
- **快速上手** - 复制即用

## 快速开始

```bash
# 设置 API Key
export DEEPSEEK_API_KEY=your-api-key

# 运行示例
go run main.go
```

## 示例说明

### 示例 1: 最简单的对话

```go
// 一行创建 Agent
agent := quickAgent(apiKey, "你是一个友好的 AI 助手")

// 直接运行
output := run(agent, "用一句话介绍 Go 语言")
```

**输出：**
```text
🤖 回复: Go 语言是 Google 开发的静态类型、编译型编程语言，以简洁、高效和并发处理能力著称。
```

### 示例 2: 使用工具

```go
// 创建工具（简化）
calculator := simpleTool(
    "calculator",
    "计算数学表达式",
    func(ctx context.Context, input string) (string, error) {
        return "120", nil
    },
)

// 创建带工具的 Agent
agent := quickAgentWithTools(apiKey,
    "你是智能助手",
    calculator,
)

// 运行
output := runWithTools(agent, "计算 15 * 8 的结果")
```

### 示例 3: 聊天机器人

```go
// 创建聊天机器人
agent := chatbot(apiKey)

// 多轮对话
conversations := []string{
    "你好！",
    "告诉我一个有趣的事实",
    "再见！",
}

for _, msg := range conversations {
    output := run(agent, msg)
    fmt.Println(output)
}
```

## 核心辅助函数

### quickAgent - 快速创建 Agent

```go
func quickAgent(apiKey, prompt string) *builder.ConfigurableAgent[any, core.State]
```

最简化的 Agent 创建方式。

### quickAgentWithTools - 创建带工具的 Agent

```go
func quickAgentWithTools(apiKey, prompt string, tools ...interfaces.Tool) *builder.ConfigurableAgent[any, core.State]
```

创建可以使用工具的 Agent。

### chatbot - 创建聊天机器人

```go
func chatbot(apiKey string) *builder.ConfigurableAgent[any, core.State]
```

使用预设配置创建聊天机器人。

### run - 运行 Agent

```go
func run(agent *builder.ConfigurableAgent[any, core.State], input string) interface{}
```

简化的执行接口。

### simpleTool - 创建简单工具

```go
func simpleTool(name, description string, handler func(context.Context, string) (string, error)) interfaces.Tool
```

用一个函数创建工具，无需关心 JSON Schema 细节。

## 与完整示例对比

### 原版（11-deepseek-with-builder）

- 17KB 代码
- 5 个详细示例
- 完整的错误处理
- 详细的配置说明
- 适合学习完整 API

### 简化版（12-deepseek-simple）

- 5KB 代码
- 3 个核心示例
- 简化的 API
- 快速上手
- 适合快速开发

## 何时使用哪个版本？

### 使用简化版（本示例）

- ✅ 快速原型开发
- ✅ 简单的应用场景
- ✅ 学习核心概念
- ✅ 最小化代码量

### 使用完整版（11-deepseek-with-builder）

- ✅ 生产环境应用
- ✅ 需要完整错误处理
- ✅ 复杂的配置需求
- ✅ 学习完整 API

## 扩展示例

### 添加自定义工具

```go
// 创建天气查询工具
weatherTool := simpleTool(
    "get_weather",
    "查询城市天气",
    func(ctx context.Context, city string) (string, error) {
        // 实际应用中调用天气 API
        return fmt.Sprintf("%s: 晴朗，25°C", city), nil
    },
)

// 添加到 Agent
agent := quickAgentWithTools(apiKey,
    "你是天气助手",
    weatherTool,
)
```

### 添加成本追踪

```go
// 创建成本追踪器
pricing := map[string]float64{
    "deepseek-chat": 0.21 / 1_000_000,
}
costTracker := core.NewCostTrackingCallback(pricing)

// 添加到 Agent
agent, _ := builder.NewAgentBuilder[any, core.State](llm).
    WithSystemPrompt("你是 AI 助手").
    WithState(agentstate.NewAgentState()).
    WithCallbacks(costTracker).
    Build()

// 执行后查看成本
fmt.Printf("成本: $%.6f\n", costTracker.GetTotalCost())
```

## 最佳实践

### 1. 使用辅助函数

```go
// ✅ 好
agent := quickAgent(apiKey, prompt)

// ❌ 避免重复代码
llm, _ := providers.NewDeepSeekWithOptions(...)
agent, _ := builder.NewAgentBuilder[any, core.State](llm).
    WithSystemPrompt(prompt).
    WithState(agentstate.NewAgentState()).
    Build()
```

### 2. 简化工具定义

```go
// ✅ 好 - 使用 simpleTool
tool := simpleTool("name", "desc", handler)

// ❌ 避免手写 Schema
tool, _ := tools.NewFunctionToolBuilder("name").
    WithDescription("desc").
    WithArgsSchema(`{...}`).
    WithFunction(...).
    Build()
```

### 3. 统一错误处理

```go
// 在辅助函数中统一处理
// 应用代码保持简洁
output := run(agent, "问题")
```

## 从简化版迁移到完整版

当需求增长时，可以逐步迁移：

```go
// 简化版
agent := quickAgent(apiKey, prompt)
output := run(agent, input)

// 迁移步骤 1: 添加错误处理
output, err := agent.Execute(ctx, input)
if err != nil {
    // 处理错误
}

// 迁移步骤 2: 添加配置
agent, err := builder.NewAgentBuilder[any, core.State](llm).
    WithSystemPrompt(prompt).
    WithConfig(&builder.AgentConfig{
        Timeout: 30 * time.Second,
        Verbose: true,
    }).
    Build()

// 迁移步骤 3: 添加中间件和回调
agent, err := builder.NewAgentBuilder[any, core.State](llm).
    WithSystemPrompt(prompt).
    WithCallbacks(costTracker).
    WithMiddleware(loggingMW).
    Build()
```

## 参考资源

- [完整示例](../11-deepseek-with-builder/) - 详细的 API 使用
- [go-kratos/blades](https://github.com/go-kratos/blades) - 设计灵感来源
- [GoAgent 文档](https://github.com/kart-io/goagent)

## 许可证

本示例遵循 GoAgent 项目的许可证。
