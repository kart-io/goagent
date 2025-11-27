# DeepSeek Agent Builder 示例

这个示例演示如何使用 GoAgent 的 `AgentBuilder` API 与 DeepSeek LLM 集成。

## 功能特性

本示例涵盖以下主题：

### 1. 基础 Agent Builder

- 创建 DeepSeek LLM 客户端
- 使用 `NewAgentBuilder` 构建 Agent
- 设置系统提示词
- 配置状态管理
- 运行 Agent 并获取结果

### 2. Agent Builder + 工具

- 使用 `WithTools` 添加工具
- 创建自定义工具（计算器、天气查询、时间查询）
- Agent 自动选择和调用工具
- 查看工具使用情况

### 3. 中间件和回调

- 添加成本追踪回调
- 启用日志和计时中间件
- 监控 Agent 执行过程
- 追踪 Token 使用和成本

### 4. 自定义配置

- 配置最大迭代次数
- 设置超时时间
- 调整温度和 MaxTokens
- 添加元数据

### 5. 预设配置

- `ConfigureForChatbot()` - 聊天机器人配置
- `ConfigureForRAG()` - RAG 系统配置
- `ConfigureForAnalysis()` - 数据分析配置

## 前置要求

### 环境要求

- Go 1.25.0 或更高版本
- DeepSeek API Key

### 获取 DeepSeek API Key

1. 访问 [DeepSeek Platform](https://platform.deepseek.com/)
2. 注册账号或登录
3. 在控制台创建 API Key
4. 设置环境变量：

```bash
export DEEPSEEK_API_KEY=your-api-key
```

## 安装和运行

### 1. 克隆项目

```bash
git clone https://github.com/kart-io/goagent.git
cd goagent/examples/basic/11-deepseek-with-builder
```

### 2. 安装依赖

```bash
go mod download
```

### 3. 运行示例

```bash
# 设置 API Key
export DEEPSEEK_API_KEY=your-api-key

# 运行所有示例
go run main.go
```

## 代码示例

### 基础用法

```go
// 创建 DeepSeek 客户端
llmClient, err := providers.NewDeepSeekWithOptions(
    llm.WithAPIKey(apiKey),
    llm.WithModel("deepseek-chat"),
    llm.WithTemperature(0.7),
    llm.WithMaxTokens(2000),
)

// 使用 AgentBuilder 构建 Agent
agent, err := builder.NewAgentBuilder[any, core.State](llmClient).
    WithSystemPrompt("你是一个友好的 AI 助手").
    WithState(state.NewMemoryState()).
    Build()

// 运行 Agent
ctx := context.Background()
input := &core.AgentInput{
    Task:      "请用一句话介绍 Go 语言的主要特点",
    Timestamp: time.Now(),
}

output, err := agent.Invoke(ctx, input)
fmt.Printf("回复: %v\n", output.Result)
```

### 添加工具

```go
// 创建工具
calculatorTool := createCalculatorTool()
weatherTool := createWeatherTool()

// 构建带工具的 Agent
agent, err := builder.NewAgentBuilder[any, core.State](llmClient).
    WithSystemPrompt("你是一个智能助手").
    WithTools(calculatorTool, weatherTool).
    WithState(state.NewMemoryState()).
    WithConfig(&builder.AgentConfig{
        MaxIterations: 10,
        Timeout:       30 * time.Second,
        Verbose:       true,
    }).
    Build()
```

### 添加回调和中间件

```go
// 创建成本追踪器
pricing := map[string]float64{
    "deepseek-chat": 0.21 / 1_000_000,
}
costTracker := core.NewCostTrackingCallback(pricing)

// 构建 Agent
agent, err := builder.NewAgentBuilder[any, core.State](llmClient).
    WithSystemPrompt("你是一个专业的 AI 助手").
    WithState(state.NewMemoryState()).
    WithCallbacks(costTracker).
    WithConfig(&builder.AgentConfig{
        Verbose: true, // 自动添加日志和计时中间件
    }).
    Build()

// 运行后查看成本
fmt.Printf("总 Tokens: %d\n", costTracker.GetTotalTokens())
fmt.Printf("总成本: $%.6f\n", costTracker.GetTotalCost())
```

### 使用预设配置

```go
// 聊天机器人配置
agent, err := builder.NewAgentBuilder[any, core.State](llmClient).
    WithSystemPrompt("你是一个友好的聊天机器人").
    WithState(state.NewMemoryState()).
    ConfigureForChatbot(). // 预设配置
    Build()
```

## AgentBuilder API 参考

### 核心方法

| 方法 | 说明 |
|------|------|
| `NewAgentBuilder[C, S](llmClient)` | 创建新的 Agent Builder |
| `WithSystemPrompt(prompt)` | 设置系统提示词 |
| `WithTools(tools...)` | 添加工具 |
| `WithState(state)` | 设置状态管理器 |
| `WithConfig(config)` | 设置配置选项 |
| `WithCallbacks(callbacks...)` | 添加回调函数 |
| `WithMiddleware(mw...)` | 添加中间件 |
| `WithMetadata(key, value)` | 添加元数据 |
| `Build()` | 构建最终的 Agent |

### 预设配置方法

| 方法 | 说明 | 特点 |
|------|------|------|
| `ConfigureForChatbot()` | 聊天机器人配置 | 启用流式输出，较高温度(0.8)，速率限制 |
| `ConfigureForRAG()` | RAG 系统配置 | 缓存中间件，低温度(0.3)，大 Token 限制 |
| `ConfigureForAnalysis()` | 数据分析配置 | 极低温度(0.1)，更多迭代次数，计时中间件 |

### AgentConfig 配置项

```go
type AgentConfig struct {
    MaxIterations   int           // 最大迭代次数
    Timeout         time.Duration // 超时时间
    EnableStreaming bool          // 启用流式输出
    EnableAutoSave  bool          // 自动保存状态
    SaveInterval    time.Duration // 保存间隔
    MaxTokens       int           // 最大 Token 数
    Temperature     float64       // 温度参数
    SessionID       string        // 会话 ID
    Verbose         bool          // 详细日志
}
```

## 输出示例

### 示例 1: 基础 Agent Builder

```text
示例 1: 基础 Agent Builder
----------------------------
📡 创建 DeepSeek LLM 客户端...
🔨 使用 AgentBuilder 构建 Agent...
🚀 运行 Agent...

📝 结果:
状态: completed
回复: Go 语言是一种静态类型、编译型的编程语言，以其简洁的语法、高效的并发处理能力和快速的编译速度著称。
耗时: 1.234s
```

### 示例 2: Agent Builder + 工具

```text
示例 2: Agent Builder + 工具
----------------------------
🔧 创建工具...
🔨 构建带工具的 Agent...
🚀 运行任务...

📝 结果:
状态: completed
回复: 15 * 8 = 120，现在的时间是 2025-11-27 14:30:45
耗时: 2.456s

🔨 使用的工具 (2 个):
  1. calculator
  2. get_current_time
```

### 示例 3: 成本追踪

```text
示例 3: Agent Builder + 中间件
----------------------------
📊 配置成本追踪...
🔨 构建 Agent...
🚀 运行任务...

📝 结果:
状态: completed
回复: 机器学习是人工智能的一个分支，通过算法让计算机从数据中学习模式，无需显式编程即可做出预测或决策。
耗时: 1.567s

💰 成本信息:
总 Tokens: 150
总成本: $0.000032
```

## 工具说明

示例中包含三个自定义工具：

### 1. 计算器工具 (`calculator`)

执行基本数学运算（加减乘除）。

**输入示例：**

```json
{
  "expression": "15 * 8"
}
```

**输出示例：**

```json
{
  "expression": "15 * 8",
  "result": 120
}
```

### 2. 天气查询工具 (`get_weather`)

查询城市天气信息（模拟数据）。

**输入示例：**

```json
{
  "city": "北京"
}
```

**输出示例：**

```json
{
  "city": "北京",
  "weather": "晴朗",
  "temperature": 22,
  "humidity": 60,
  "wind_speed": "3-4级",
  "air_quality": "优"
}
```

### 3. 时间查询工具 (`get_current_time`)

获取当前时间（支持时区）。

**输入示例：**

```json
{
  "timezone": "Asia/Shanghai"
}
```

**输出示例：**

```json
{
  "time": "2025-11-27 14:30:45",
  "timezone": "Asia/Shanghai",
  "timestamp": 1732691445,
  "weekday": "Wednesday"
}
```

## DeepSeek 定价

DeepSeek 的定价如下：

- **输入 Token:** $0.14 / 1M tokens
- **输出 Token:** $0.28 / 1M tokens
- **平均成本:** ~$0.21 / 1M tokens

示例中使用的成本追踪器按平均成本计算。

## 最佳实践

### 1. 系统提示词

编写清晰、具体的系统提示词：

```go
// ✅ 好的示例
"你是一个专业的技术文档写作助手，专注于准确性和清晰度。请用简洁的语言回答问题。"

// ❌ 不好的示例
"你是一个助手"
```

### 2. 温度设置

根据任务类型调整温度：

- **创造性任务（聊天）：** 0.7-0.9
- **一般任务：** 0.5-0.7
- **事实性任务（分析）：** 0.1-0.3

### 3. 工具设计

- 提供清晰的工具描述
- 使用 JSON Schema 定义参数
- 处理错误情况
- 返回结构化数据

### 4. 错误处理

```go
output, err := agent.Invoke(ctx, input)
if err != nil {
    // 检查错误类型
    if agentErrors.IsLLMError(err) {
        log.Printf("LLM 错误: %v", err)
    } else if agentErrors.IsToolError(err) {
        log.Printf("工具执行错误: %v", err)
    } else {
        log.Printf("未知错误: %v", err)
    }
    return
}
```

### 5. 超时控制

```go
config := &builder.AgentConfig{
    Timeout:       30 * time.Second,
    MaxIterations: 10,
}

// 或者使用上下文超时
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
```

## 故障排查

### 问题 1: API Key 未设置

**错误信息：**

```text
⚠️  警告：未设置 DEEPSEEK_API_KEY 环境变量
```

**解决方法：**

```bash
export DEEPSEEK_API_KEY=your-api-key
```

### 问题 2: 超时错误

**错误信息：**

```text
context deadline exceeded
```

**解决方法：**

增加超时时间：

```go
config := &builder.AgentConfig{
    Timeout: 60 * time.Second, // 增加到 60 秒
}
```

### 问题 3: Token 限制错误

**错误信息：**

```text
maximum token limit exceeded
```

**解决方法：**

调整 MaxTokens 设置：

```go
llmClient, err := providers.NewDeepSeekWithOptions(
    llm.WithAPIKey(apiKey),
    llm.WithMaxTokens(4000), // 增加限制
)
```

## 相关资源

### GoAgent 文档

- [项目主页](https://github.com/kart-io/goagent)
- [AgentBuilder 指南](https://github.com/kart-io/goagent/tree/master/builder)
- [工具开发指南](https://github.com/kart-io/goagent/tree/master/tools)

### DeepSeek 文档

- [DeepSeek Platform](https://platform.deepseek.com/)
- [API 文档](https://platform.deepseek.com/docs)
- [定价信息](https://platform.deepseek.com/pricing)

### 其他示例

- [基础示例](../01-simple-agent/)
- [工具示例](../02-tools/)
- [高级示例](../../advanced/)

## 许可证

本示例遵循 GoAgent 项目的许可证。
