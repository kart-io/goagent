# GoAgent 使用指南

本文档介绍用 Builder 创建 Agent、接入工具与记忆、使用中间件、流式输出与错误处理的基本方式。编排多步任务见[原生编排与工具体系](NATIVE_ORCHESTRATION.md)，Builder 的完整方法见 [Builder API 参考](BUILDER_API_REFERENCE.md)。

## 目录

- [安装配置](#安装配置)
- [Agent 使用](#agent-使用)
- [工具使用](#工具使用)
- [内存管理](#内存管理)
- [中间件使用](#中间件使用)
- [流式处理](#流式处理)
- [错误处理](#错误处理)
- [最佳实践](#最佳实践)

---

## 安装配置

### 系统要求

- Go 1.25.0 或更高版本
- 支持的操作系统：Linux、macOS、Windows

### 安装

```bash
go get github.com/kart-io/goagent
```

### 环境变量

Provider 未显式传入 API Key 时从环境变量读取：

```bash
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
export DEEPSEEK_API_KEY="sk-..."
export KIMI_API_KEY="..."
export SILICONFLOW_API_KEY="..."
```

---

## Agent 使用

### 基础用法

```go
import (
    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

// 1. 创建 LLM 客户端
client, err := providers.NewOpenAIWithOptions(
    llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    llm.WithModel("gpt-4"),
)
if err != nil {
    return err
}

// 2. 使用 Builder 创建 Agent
agent, err := builder.NewSimpleBuilder(client).
    WithSystemPrompt("你是一个有帮助的助手").
    Build()
if err != nil {
    return err
}

// 3. 执行
output, err := agent.Execute(ctx, "你好")
if err != nil {
    return err
}
fmt.Println(output.Result)
```

`Execute` 只调用一次模型，不执行工具；配置了工具时使用 `ExecuteWithTools` 或 `AsRunnable().Invoke`（见[工具使用](#工具使用)）。

### 配置选项

```go
agent, err := builder.NewSimpleBuilder(client).
    WithSystemPrompt("你是一个助手").
    WithMaxIterations(10).
    WithTimeout(30 * time.Second).
    WithMaxTokens(2000).
    WithTemperature(0.7).
    WithSessionID("session-001").
    WithMaxConversationHistory(20).
    WithVerbose(true).
    WithOutputFormat(builder.OutputFormatMarkdown).
    Build()
```

---

## 工具使用

### 创建工具

```go
import "github.com/kart-io/goagent/tools"

weatherTool := tools.NewFunctionTool(
    "get_weather",
    "查询城市天气",
    `{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`,
    func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        city, _ := args["city"].(string)
        return map[string]string{"city": city, "weather": "晴"}, nil
    },
)
```

也可以使用构建器，并从结构体生成参数 Schema：

```go
type WeatherArgs struct {
    City string `json:"city"`
}

weatherTool, err := tools.NewFunctionToolBuilder("get_weather").
    WithDescription("查询城市天气").
    WithArgsSchemaFromStruct(WeatherArgs{}).
    WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        return "晴", nil
    }).
    Build()
```

### 使用内置工具

```go
import (
    "github.com/kart-io/goagent/tools/compute"
    "github.com/kart-io/goagent/tools/http"
    "github.com/kart-io/goagent/tools/shell"
)

shellTool := shell.NewShellTool([]string{"ls", "cat"}, 10*time.Second)
calculator := compute.NewCalculatorTool()
apiTool := http.NewAPITool("https://api.example.com", 30*time.Second, nil)
```

### 让 Agent 调用工具

```go
agent, err := builder.NewSimpleBuilder(client).
    WithSystemPrompt("你可以查询天气和做计算").
    WithTools(weatherTool, calculator).
    Build()
if err != nil {
    return err
}

output, err := agent.ExecuteWithTools(ctx, "北京天气怎么样？")
```

---

## 内存管理

### 对话记忆

```go
import "github.com/kart-io/goagent/memory"

memManager := memory.NewInMemoryManager(memory.DefaultConfig())

agent, err := builder.NewSimpleBuilder(client).
    WithMemory(memManager).
    WithSessionID("session-001").
    WithMaxConversationHistory(20).
    Build()
```

同一 `SessionID` 下的对话写入记忆，后续调用会带上历史上下文。

### 手动管理对话

```go
_ = memManager.AddConversation(ctx, &interfaces.Conversation{
    SessionID: "session-001",
    Role:      "user",
    Content:   "你好",
    Timestamp: time.Now(),
})

history, err := memManager.GetConversationHistory(ctx, "session-001", 10)

_ = memManager.ClearConversation(ctx, "session-001")
```

### 案例记忆

```go
_ = memManager.AddCase(ctx, &interfaces.Case{
    Title:    "数据库连接超时",
    Problem:  "应用启动时连接数据库超时",
    Solution: "检查连接池配置与网络策略",
    Category: "database",
})

cases, err := memManager.SearchSimilarCases(ctx, "数据库连接问题", 5)
for _, c := range cases {
    fmt.Printf("相似度 %.2f: %s\n", c.Similarity, c.Title)
}
```

### 检查点

```go
import "github.com/kart-io/goagent/core/checkpoint"

agent, err := builder.NewSimpleBuilder(client).
    WithCheckpointer(checkpoint.NewInMemorySaver()).
    WithSessionID("session-001").
    WithAutoSaveEnabled(true).
    WithSaveInterval(5 * time.Second).
    Build()
```

需要进程重启后续跑的多步任务使用 `core/durable`，见[原生编排与工具体系](NATIVE_ORCHESTRATION.md)。

---

## 中间件使用

### 添加中间件

```go
import "github.com/kart-io/goagent/core/middleware"

agent, err := builder.NewSimpleBuilder(client).
    WithMiddleware(
        middleware.NewLoggingMiddleware(func(msg string) { log.Println(msg) }),
        middleware.NewTimingMiddleware(),
        middleware.NewCacheMiddleware(5*time.Minute),
        middleware.NewRateLimiterMiddleware(10, time.Second),
    ).
    Build()
```

### 自定义中间件

嵌入 `*middleware.BaseMiddleware`，只覆盖需要的钩子：

```go
type TraceMiddleware struct {
    *middleware.BaseMiddleware
}

func NewTraceMiddleware() *TraceMiddleware {
    return &TraceMiddleware{BaseMiddleware: middleware.NewBaseMiddleware("trace")}
}

func (m *TraceMiddleware) OnBefore(ctx context.Context, req *middleware.MiddlewareRequest) (*middleware.MiddlewareRequest, error) {
    if req.Metadata == nil {
        req.Metadata = map[string]interface{}{}
    }
    req.Metadata["start_time"] = time.Now()
    return req, nil
}
```

---

## 流式处理

`ExecuteStream` 返回事件通道：`text` 事件携带增量文本，`done` 事件携带最终输出，`error` 事件携带错误。需要客户端支持结构化流式接口。

```go
events, err := agent.ExecuteStream(ctx, "请写一首诗")
if err != nil {
    return err
}

var full strings.Builder
for event := range events {
    switch event.Type {
    case "text":
        full.WriteString(event.Delta)
        fmt.Print(event.Delta)
    case "error":
        return event.Error
    case "done":
        fmt.Println("\n完整内容:", full.String())
    }
}
```

消费方须读完通道或取消 ctx，否则上游会因背压挂起。

---

## 错误处理

### 按错误码判断

```go
import agentErrors "github.com/kart-io/goagent/errors"

output, err := agent.Execute(ctx, "你好")
if err != nil {
    switch {
    case agentErrors.IsCode(err, agentErrors.CodeInvalidInput):
        log.Printf("输入错误: %v", err)
    case agentErrors.IsCode(err, agentErrors.CodeLLMRequest),
        agentErrors.IsCode(err, agentErrors.CodeLLMTimeout):
        log.Printf("LLM 错误: %v", err)
    case agentErrors.IsCode(err, agentErrors.CodeToolExecution):
        log.Printf("工具错误: %v", err)
    default:
        log.Printf("其他错误: %v", err)
    }
}
```

### 自定义错误处理

```go
agent, err := builder.NewSimpleBuilder(client).
    WithErrorHandler(func(err error) error {
        log.Printf("Agent 错误: %v", err)
        return fmt.Errorf("wrapped: %w", err)
    }).
    Build()
```

### 模型调用重试

```go
agent, err := builder.NewSimpleBuilder(client).
    WithRequestError(3, builder.RetryTransient(3, func(retry int) time.Duration {
        return time.Duration(retry) * 100 * time.Millisecond
    })).
    Build()
```

`RetryTransient` 只原样重试 429、5xx 与连接错误，已输出文本后不重试；持久运行中重试发生在同一条回合记录内。

---

## 最佳实践

### 1. 用上下文控制超时与取消

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

output, err := agent.Execute(ctx, "你好")
```

### 2. 关闭时释放资源

```go
if err := agent.Shutdown(ctx); err != nil {
    log.Printf("关闭 Agent 失败: %v", err)
}
```

### 3. 测试中使用 Mock 客户端

```go
import "github.com/kart-io/goagent/testing/mocks"

mockClient := mocks.NewMockLLMClient()
mockClient.SetResponses(llm.CompletionResponse{Content: "预设回答"})

agent, err := builder.NewSimpleBuilder(mockClient).Build()
```
