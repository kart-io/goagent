# GoAgent 使用指南

新应用以 Builder.AsRunnable 和类型化 Workflow 组合业务。Go 版本要求见 [go.mod](../../go.mod)；精确签名见 [核心 API](../api/CORE_API.md) 和 [Builder 参考](BUILDER_API_REFERENCE.md)。

## 选择入口

| 需求 | 入口 |
| --- | --- |
| 模型回答、检索、原生工具循环 | `builder.NewSimpleBuilder(client)` → `BuildContext(ctx)` → `AsRunnable().Invoke/Stream` |
| 固定步骤、分支、循环与并行 | `workflow.Graph[T]` → Compile |
| 节点/分支级恢复、审批 | CompileDurable 与持久模型/工具记录 |
| 子 Agent 或子图作为能力 | NewRunnableTool / NewRunnableToolFactory，经 Dispatcher 调用 |

不要把 Builder 的输出与 core.AgentOutput 混用。

## 运行第一个案例

以下命令使用仓内模拟模型，无需密钥：

```bash
go run ./examples/execution-scope
go run ./examples/workflow
```

接入真实模型见 [快速入门](QUICKSTART.md)、[LLM API](../api/LLM_API.md) 和 [Provider 能力](PROVIDER_CAPABILITIES.md)。实现 `llm.Client` 不等于支持 ToolStreamClient；工具场景先检查具体 Provider 的原生能力。

## 工具与业务效果

工具实现 interfaces.Tool，其 Invoke 输入是 *interfaces.ToolInput，不是裸 map。参数由 ArgsSchema 声明，结果为 *interfaces.ToolOutput。使用 Dispatcher.Tools() 注册到 Builder，获得校验、调用身份和持久回放路径；裸工具调用不会自动写入操作日志。

工具失败、模型可修订的参数拒绝、审批等待和业务不支持要分别处理。ReturnArgumentErrors 只作用于模型调用的特定路径，不是自动询问用户的对话管理功能。详见 [Tool API](../api/TOOL_API.md)。

## 会话与恢复

WithSessionID 与 WithMemory 组合记忆；WithSessionLog 使用会话日志推导模型历史。它们不能替代宿主的待澄清任务管理。

会话历史、Agent 会话状态检查点和工作流 durable.Store 职责不同。RunID/CallID/Agent作用域来自宿主并跨恢复稳定；新任务不能误用已完成运行身份。统一装配见 [宿主接入](HOST_INTEGRATION.md)。

// 1. 创建 LLM 客户端
client, err := providers.NewOpenAIWithOptions(
    llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    llm.WithModel("gpt-4o-mini"),
)
if err != nil {
    return err
}

## 流、取消与并发

// 3. 执行（需要支持流式的客户端）
output, err := agent.AsRunnable().Invoke(ctx, "你好")
if err != nil {
    return err
}
fmt.Println(output.Result)
```

`AsRunnable().Invoke` 是统一入口，配置的工具、检索、中间件与回调走同一路径；Builder 接受任意 `llm.Client`，配置工具时要求 `llm.ToolStreamClient`。入口对照见[快速入门](QUICKSTART.md#旧接口迁移)。

## 上下文、重试与观察

- 长工具结果通过 WithOffload 和已注册的读取工具处理；旧 ContextPolicy/WithContextPolicies 已删除，迁移见 [上下文指南](CONTEXT_POLICY.md)。
- 自定义请求前改写通过 WithIdentifiedPreStep 管理持久身份；角色和业务分派不放进通用上下文钩子。
- 模型暂时错误使用 WithRequestError 与 RetryTransient；WithModelRetry 已删除。每次尝试分别占预算，子 Agent 不自动继承父层重试钩子。
- WithMiddleware 接收 core/middleware 的处理链协议；工具准入与 Around/Post 由 tools.Pipeline 处理，详见 [中间件 API](../api/MIDDLEWARE_API.md)。
- Observer 只读，不是业务状态或检查点；宿主负责关闭借用资源。

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

output, err := agent.AsRunnable().Invoke(ctx, "北京天气怎么样？")
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

同一 `SessionID` 下的对话写入记忆，后续调用会带上历史上下文。配置 `WithSessionLog(store)` 后，模型历史改由 [core/session](../../core/session/README.md) 的会话日志推导，不再读取记忆。

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

### 会话日志

```go
import "github.com/kart-io/goagent/core/session"

store, err := session.NewFileStore(dir) // 或 session.NewMemoryStore()
if err != nil {
    return err
}
agent, err := builder.NewSimpleBuilder(client).
    WithSessionID("session-001").
    WithSessionLog(store).
    Build()
```

会话历史从日志推导，进程重启后以同一 SessionID 继续，详见 [core/session](../../core/session/README.md)。

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

推荐使用 `agent.AsRunnable().Stream`，见[快速入门](QUICKSTART.md#流式调用)。`ExecuteStream` 是同一事件源的通道形式：`text` 事件携带增量文本，`done` 事件携带最终输出，`error` 事件携带错误。

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

output, err := agent.AsRunnable().Invoke(ctx, "你好")
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

output, err := agent.AsRunnable().Invoke(ctx, "你好")
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
// MockLLMClient 实现 llm.ToolStreamClient，可直接经 AsRunnable 调用。
```
