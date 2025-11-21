# DeepSeek 快速入门

这是一个 5 分钟快速上手指南，帮助你快速使用 DeepSeek 与 GoAgent。

## 第一步：获取 API Key

1. 访问 [DeepSeek Platform](https://platform.deepseek.com/)
2. 注册并登录
3. 在控制台中创建 API Key

## 第二步：设置环境变量

```bash
export DEEPSEEK_API_KEY=your-api-key-here
```

## 第三步：运行示例

```bash
cd /home/hellotalk/code/go/src/github.com/kart-io/goagent/examples/basic/09-deepseek-simple
go run main.go
```

## 代码解析

### 方式 1：直接使用 Provider（最简单）

```go
// 1. 创建配置
config := &llm.Config{
    APIKey: apiKey,
    Model: "deepseek-chat",
}

// 2. 创建 Provider
client, err := providers.NewDeepSeek(config)

// 3. 发送消息
messages := []llm.Message{
    llm.SystemMessage("你是一个友好的助手"),
    llm.UserMessage("你好！"),
}
response, err := client.Chat(ctx, messages)

// 4. 获取回复
fmt.Println(response.Content)
```

### 方式 2：使用 Builder（更强大）

```go
// 1. 创建 Provider
client, err := providers.NewDeepSeek(config)

// 2. 构建 Agent（注意泛型参数）
agent, err := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
    WithSystemPrompt("你是一个专业顾问").
    Build()

// 3. 执行任务
output, err := agent.Execute(ctx, "你的问题")

// 4. 获取结果
fmt.Println(output.Result)
```

## 核心概念

### Provider vs Agent

- **Provider**：直接与 LLM API 交互，适合简单对话
- **Agent**：基于 Provider 构建，支持工具、中间件、状态管理等高级功能

### 配置参数

```go
config := &llm.Config{
    APIKey:      "your-key",      // 必填
    Model:       "deepseek-chat", // 模型名称
    Temperature: 0.7,             // 随机性 (0-1)
    MaxTokens:   1000,            // 最大输出长度
    Timeout:     30,              // 超时（秒）
}
```

### 消息类型

```go
llm.SystemMessage("系统提示")  // 设定 AI 角色
llm.UserMessage("用户输入")    // 用户消息
llm.AssistantMessage("AI回复") // AI 回复
```

## 常见使用场景

### 场景 1：简单问答

```go
response, err := client.Chat(ctx, []llm.Message{
    llm.SystemMessage("你是一个翻译助手"),
    llm.UserMessage("将 'Hello' 翻译成中文"),
})
```

### 场景 2：多轮对话

```go
messages := []llm.Message{
    llm.SystemMessage("你是一个编程助手"),
    llm.UserMessage("什么是 Go 语言?"),
}

// 第一轮
response1, _ := client.Chat(ctx, messages)
messages = append(messages, llm.AssistantMessage(response1.Content))

// 第二轮
messages = append(messages, llm.UserMessage("它有什么优势?"))
response2, _ := client.Chat(ctx, messages)
```

### 场景 3：结构化任务

```go
agent, _ := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
    WithSystemPrompt("你是一个代码审查专家").
    WithMetadata("task_type", "code_review").
    Build()

output, _ := agent.Execute(ctx, "审查这段代码: func main() { ... }")
```

### 场景 4：生成结构化数据（JSON）

```go
// 配置 Agent 输出 JSON 格式
agent, _ := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
    WithSystemPrompt(`你是一个数据生成助手。生成有效的 JSON 格式数据，不要包含其他文字。`).
    Build()

// 请求生成 JSON 数据
task := `请生成一个包含 3 个用户的 JSON 数组，每个用户包含 id, name, email, age 字段。`
output, _ := agent.Execute(ctx, task)

// 输出即为 JSON 格式的数据
fmt.Println(output.Result)  // 可以直接用于 json.Unmarshal
```

**关键技巧**：
- 使用低 Temperature (0.3) 获得稳定输出
- 在 SystemPrompt 中明确输出格式要求
- 在任务中详细描述数据结构

## 调试技巧

### 查看详细日志

```go
config.Verbose = true  // 启用详细日志
```

### 检查 Token 使用

```go
response, _ := client.Chat(ctx, messages)
fmt.Printf("使用 Tokens: %d\n", response.Usage.TotalTokens)
```

### 错误处理

```go
response, err := client.Chat(ctx, messages)
if err != nil {
    // 检查错误类型
    if strings.Contains(err.Error(), "timeout") {
        // 超时错误处理
    } else if strings.Contains(err.Error(), "rate limit") {
        // 速率限制处理
    }
}
```

## 下一步学习

1. **工具调用**：学习如何让 AI 使用工具
   - 查看 [08-deepseek-agent](../08-deepseek-agent/main.go)

2. **InvokeFast 性能优化**：了解如何自动提升 Agent 性能
   - 查看 [invokefast](./invokefast/)

3. **流式输出**：实时接收 AI 生成的文本
   - 查看 [streaming](../../advanced/streaming/)

4. **多 Agent 协作**：构建复杂的 AI 系统
   - 查看 [multi-agent-collaboration](../../advanced/multi-agent-collaboration/)

## 疑难解答

### 问题：API Key 无效

```text
错误: 请设置 DEEPSEEK_API_KEY 环境变量
```

**解决**：确保环境变量设置正确

```bash
echo $DEEPSEEK_API_KEY  # 检查是否设置
export DEEPSEEK_API_KEY=sk-xxx  # 重新设置
```

### 问题：网络超时

```text
LLM 请求失败: context deadline exceeded
```

**解决**：增加超时时间

```go
config.Timeout = 60  // 增加到 60 秒
```

### 问题：泛型参数错误

```text
cannot infer C (declared at ./builder/builder.go:105:1)
```

**解决**：显式指定泛型参数

```go
// ❌ 错误
agent := builder.NewAgentBuilder(client)

// ✅ 正确
agent, _ := builder.NewAgentBuilder[any, *agentcore.AgentState](client).Build()
```

## 完整示例代码

参考 [main.go](./main.go) 获取完整的可运行示例。

## 获取帮助

- [完整文档](./README.md)
- [GoAgent 项目主页](https://github.com/kart-io/goagent)
- [DeepSeek 官方文档](https://platform.deepseek.com/docs)
