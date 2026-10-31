# Input Serialization 输入序列化示例

演示 `AgentBuilder.WithInputSerializer`：在用户输入发送给模型之前自定义序列化（如脱敏）。

## InputSerializer 接口

```go
type InputSerializer interface {
    Serialize(input interface{}) (string, error)
}
```

示例实现了 `MaskingSerializer`：输入包含 `@` 时整体替换为 `MASKED_EMAIL`，否则原样返回。

```go
agent, _ := builder.NewSimpleBuilder(client).
    WithInputSerializer(&MaskingSerializer{}).
    WithSystemPrompt("你是一个有用的助手。请始终使用中文回答用户的请求。如果收到 'MASKED_EMAIL'，请解释什么是脱敏数据以及为什么保护隐私很重要。").
    Build()
```

## 运行

需要 `DEEPSEEK_API_KEY`（模型 `deepseek-chat`）。

```bash
export DEEPSEEK_API_KEY=your_api_key
go run ./examples/builder/serialization
```

程序用同一输入 `user@example.com` 执行两次：

- `--- Default Serializer ---`：默认序列化，模型收到原始邮箱；
- `--- Custom Masking Serializer ---`：模型收到 `MASKED_EMAIL`，按系统提示解释脱敏数据。
