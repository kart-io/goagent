# DeepSeek Agent 快速开始

## 5 分钟快速上手

### 第 1 步：获取 API Key

1. 访问 [DeepSeek Platform](https://platform.deepseek.com/)
2. 注册账号并登录
3. 在控制台创建 API Key
4. 复制 API Key（格式类似：`sk-xxxxxx...`）

### 第 2 步：设置环境变量

```bash
export DEEPSEEK_API_KEY=your-api-key-here
```

**提示**：可以将此行添加到 `~/.bashrc` 或 `~/.zshrc` 使其永久生效。

### 第 3 步：运行示例

```bash
# 方式 1: 使用 go run
go run main.go

# 方式 2: 编译后运行
go build -o deepseek-demo main.go
./deepseek-demo
```

## 运行效果预览

### 模拟模式（无 API Key）

```
GoAgent DeepSeek 示例
=====================

⚠️  警告：未设置 DEEPSEEK_API_KEY 环境变量
提示：export DEEPSEEK_API_KEY=your-api-key

使用模拟模式运行示例...

🎭 模拟模式示例
----------------------------

这个示例展示了 DeepSeek Agent 的基本用法：

1️⃣  基础对话
2️⃣  工具调用
3️⃣  流式输出
4️⃣  ReAct Agent
```

### 完整模式（有 API Key）

```
GoAgent DeepSeek 示例
=====================

示例 1: 基础 DeepSeek 配置
----------------------------
📡 检查 DeepSeek 连接...
✅ DeepSeek 连接成功

💬 发送消息到 DeepSeek...
🤖 DeepSeek 回复:
Go 语言是一门高效、简洁、并发性强的编译型语言...

📊 Token 使用: 输入=25, 输出=42, 总计=67

示例 2: DeepSeek + 工具调用
----------------------------
...

示例 3: DeepSeek 流式输出
----------------------------
...

示例 4: DeepSeek ReAct Agent
----------------------------
...
```

## 快速修改示例

### 1. 修改对话内容

在 `runBasicChatExample` 函数中修改提示词：

```go
messages := []llm.Message{
    llm.SystemMessage("你是一个友好的 AI 助手"),
    llm.UserMessage("你的问题"),  // 修改这里
}
```

### 2. 调整模型参数

在配置中修改：

```go
config := &llm.Config{
    Model:       "deepseek-chat",  // 或 "deepseek-coder"
    Temperature: 0.7,               // 0.0-1.0，越高越随机
    MaxTokens:   2000,              // 限制输出长度
}
```

### 3. 添加自定义工具

```go
type MyTool struct{}

func (t *MyTool) Name() string {
    return "my_tool"
}

func (t *MyTool) Description() string {
    return "工具功能描述"
}

func (t *MyTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
    // 你的工具逻辑
    return &interfaces.ToolOutput{
        Result:  "结果",
        Success: true,
    }, nil
}

func (t *MyTool) ArgsSchema() string {
    return `{"type": "object", "properties": {}}`
}

// 在 main() 中添加到工具列表
tools := []interfaces.Tool{
    &CalculatorTool{},
    &WeatherTool{},
    &MyTool{},  // 添加你的工具
}
```

## 常见问题

### Q1: API Key 无效？

**检查**：
- API Key 是否正确复制（无多余空格）
- 环境变量是否正确设置：`echo $DEEPSEEK_API_KEY`
- 账户是否有余额

### Q2: 连接超时？

**解决方案**：
```go
config := &llm.Config{
    Timeout: 60,  // 增加超时时间到 60 秒
}
```

### Q3: 编译错误？

**确保 Go 版本**：
```bash
go version  # 需要 Go 1.25.0+
```

**更新依赖**：
```bash
go mod tidy
go mod download
```

### Q4: 运行时错误？

**检查导入**：
```bash
cd /path/to/goagent
./verify_imports.sh  # 验证导入层级
```

**运行测试**：
```bash
go test ./llm/providers -v  # 测试 DeepSeek provider
```

## 下一步

### 学习更多示例

- 📖 查看 [完整 README](README.md) 了解所有功能
- 🔧 探索其他基础示例：`examples/basic/`
- 🚀 查看高级示例：`examples/advanced/`

### 集成到项目

1. **安装 GoAgent**：
   ```bash
   go get github.com/kart-io/goagent
   ```

2. **在代码中使用**：
   ```go
   import "github.com/kart-io/goagent/llm/providers"

   deepseek, _ := providers.NewDeepSeek(&llm.Config{
       APIKey: os.Getenv("DEEPSEEK_API_KEY"),
       Model:  "deepseek-chat",
   })
   ```

3. **创建 Agent**：
   ```go
   import "github.com/kart-io/goagent/agents/react"

   agent := react.NewReActAgent(react.ReActConfig{
       LLM:   deepseek,
       Tools: yourTools,
   })
   ```

### 性能优化

使用 InvokeFast 提升性能：

```go
// 标准调用（含监控）
output, _ := agent.Invoke(ctx, input)

// 快速调用（无监控，性能提升 4-6%）
output, _ := agent.InvokeFast(ctx, input)

// 自动选择最快路径
output, _ := agentcore.TryInvokeFast(ctx, agent, input)
```

查看 [InvokeFast 优化指南](../../../docs/guides/INVOKE_FAST_QUICKSTART.md)

## 获取帮助

- 📚 [GoAgent 文档](../../../docs/)
- 🐛 [提交 Issue](https://github.com/kart-io/goagent/issues)
- 💬 [讨论区](https://github.com/kart-io/goagent/discussions)
- 📘 [DeepSeek 官方文档](https://platform.deepseek.com/docs)

## 许可证

Apache License 2.0 - 查看 [LICENSE](../../../LICENSE) 文件
