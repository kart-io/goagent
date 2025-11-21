# DeepSeek Agent 示例

这个示例演示如何使用 DeepSeek LLM provider 创建智能 Agent。

## 功能展示

### 1. 基础对话

演示如何配置和使用 DeepSeek 进行简单对话：

```go
config := &llm.Config{
    APIKey:      "your-api-key",
    Model:       "deepseek-chat",
    Temperature: 0.7,
    MaxTokens:   2000,
}

deepseek, _ := providers.NewDeepSeek(config)
response, _ := deepseek.Chat(ctx, messages)
```

### 2. 工具调用

展示 DeepSeek 如何智能地选择和使用工具：

- **计算器工具**: 执行数学运算
- **天气查询工具**: 查询城市天气信息

```go
tools := []interfaces.Tool{
    &CalculatorTool{},
    &WeatherTool{},
}

result, _ := deepseek.GenerateWithTools(ctx, prompt, tools)
```

### 3. 流式输出

演示实时接收 AI 生成的文本：

```go
stream, _ := deepseek.Stream(ctx, prompt)
for token := range stream {
    fmt.Print(token)  // 逐个显示 token
}
```

### 4. ReAct Agent

展示使用 DeepSeek 的 ReAct（推理-行动）模式：

- 多步骤推理
- 自动工具选择
- 思考-行动-观察循环

```go
reactAgent := react.NewReActAgent(react.ReActConfig{
    Name:        "DeepSeek-ReAct-Agent",
    LLM:         deepseek,
    Tools:       tools,
    MaxSteps:    5,
    Verbose:     true,
})

output, _ := reactAgent.Invoke(ctx, input)
```

## 快速开始

### 1. 获取 API Key

访问 [DeepSeek Platform](https://platform.deepseek.com/) 注册并获取 API Key。

### 2. 设置环境变量

```bash
export DEEPSEEK_API_KEY=your-deepseek-api-key
```

### 3. 运行示例

```bash
cd examples/basic/08-deepseek-agent
go run main.go
```

## 输出示例

### 基础对话

```
示例 1: 基础 DeepSeek 配置
----------------------------
📡 检查 DeepSeek 连接...
✅ DeepSeek 连接成功

💬 发送消息到 DeepSeek...
🤖 DeepSeek 回复:
Go 语言是一门高效、简洁、并发性强的编译型语言，特别适合构建高性能的网络服务和分布式系统。

📊 Token 使用: 输入=25, 输出=42, 总计=67
```

### 工具调用

```
示例 2: DeepSeek + 工具调用
----------------------------
🔧 任务: 请帮我计算 15 * 8 的结果，然后查询北京的天气
🤖 DeepSeek 正在思考如何使用工具...
💭 思考: 我将先计算数学表达式，然后查询天气。
🔨 计划调用 2 个工具:
  1. calculator (参数: map[expression:15 * 8])
  2. get_weather (参数: map[city:北京])
```

### 流式输出

```
示例 3: DeepSeek 流式输出
----------------------------
💬 问题: 请用三句话介绍 AI Agent 的概念。
🤖 DeepSeek 回复: AI Agent 是能够感知环境、做出决策并执行行动的智能系统。它可以自主学习和适应，完成复杂任务。在 AI 应用中，Agent 通常结合大语言模型和工具来解决实际问题。
```

### ReAct Agent

```
示例 4: DeepSeek ReAct Agent
----------------------------
📋 任务: 计算 25 * 4，然后查询上海的天气
🔄 ReAct Agent 开始推理...

==================================================
✅ 任务状态: success
📝 最终结果:
计算结果是 100，上海今天天气晴朗，温度 25°C。
⏱️  执行时间: 2.5s

🧠 推理步骤 (4 步):
  ✅ 步骤 1: think
     描述: 我需要先计算 25 * 4
  ✅ 步骤 2: action_calculator
     结果: 计算结果：25 * 4 = 100
  ✅ 步骤 3: action_get_weather
     结果: 上海 今天天气：晴朗，温度 25°C
  ✅ 步骤 4: answer
     描述: 综合结果给出最终答案
```

## 配置选项

### DeepSeek Config

```go
config := &llm.Config{
    APIKey:      "your-key",           // 必需：API 密钥
    Model:       "deepseek-chat",      // 模型名称
    BaseURL:     "",                   // 可选：自定义 API 端点
    Temperature: 0.7,                  // 温度参数 (0.0-1.0)
    MaxTokens:   2000,                 // 最大 token 数
    Timeout:     30,                   // 超时时间（秒）
}
```

### 可用模型

- `deepseek-chat`: 通用对话模型（推荐）
- `deepseek-coder`: 代码生成专用模型
- `deepseek-embedding`: 文本嵌入模型

## 工具开发

自定义工具需要实现 `interfaces.Tool` 接口：

```go
type CustomTool struct{}

func (t *CustomTool) Name() string {
    return "custom_tool"
}

func (t *CustomTool) Description() string {
    return "工具功能描述"
}

func (t *CustomTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
    // 实现工具逻辑
    return &interfaces.ToolOutput{
        Result:  "结果",
        Success: true,
    }, nil
}

func (t *CustomTool) ArgsSchema() string {
    return `{
        "type": "object",
        "properties": {
            "param": {"type": "string", "description": "参数说明"}
        },
        "required": ["param"]
    }`
}
```

## 最佳实践

### 1. 错误处理

```go
response, err := deepseek.Chat(ctx, messages)
if err != nil {
    // 处理 API 错误
    if strings.Contains(err.Error(), "rate limit") {
        // 处理速率限制
    }
    return err
}
```

### 2. 超时控制

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

response, err := deepseek.Chat(ctx, messages)
```

### 3. Token 优化

```go
config := &llm.Config{
    MaxTokens: 1000,  // 限制输出长度
}

// 监控使用量
fmt.Printf("Token 使用: %d/%d\n",
    response.Usage.TotalTokens,
    config.MaxTokens)
```

### 4. 温度调节

```go
// 创意任务
config.Temperature = 0.8  // 更随机

// 精确任务
config.Temperature = 0.2  // 更确定
```

## 性能优化

### 使用 InvokeFast

对于 ReAct Agent 的内部调用，自动使用 InvokeFast 优化：

```go
reactAgent := react.NewReActAgent(config)

// Invoke 触发完整回调（用于监控）
output, _ := reactAgent.Invoke(ctx, input)

// InvokeFast 跳过回调（用于性能）
output, _ := reactAgent.InvokeFast(ctx, input)
```

### 批量处理

```go
inputs := []*agentcore.AgentInput{
    {Task: "任务1"},
    {Task: "任务2"},
    {Task: "任务3"},
}

outputs, _ := reactAgent.Batch(ctx, inputs)
```

## 常见问题

### Q: API Key 无效？

A: 确保：
1. API Key 正确复制（无多余空格）
2. 环境变量正确设置
3. 账户有足够余额

### Q: 请求超时？

A: 尝试：
1. 增加 `Timeout` 配置
2. 减少 `MaxTokens` 限制
3. 检查网络连接

### Q: 工具调用失败？

A: 检查：
1. 工具的 `ArgsSchema` 格式正确
2. `Description` 清晰描述工具用途
3. `Invoke` 方法正确处理参数

## 相关链接

- [DeepSeek 官方文档](https://platform.deepseek.com/docs)
- [GoAgent 文档](../../docs/guides/QUICKSTART.md)
- [LLM Providers 指南](../../docs/guides/LLM_PROVIDERS.md)
- [工具开发指南](../../docs/guides/TOOLS.md)

## 许可证

Apache License 2.0
