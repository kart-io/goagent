# Tools System

完整的工具系统实现，借鉴 LangChain 的 Tool 设计理念。

## 概述

Tools 系统提供了一个统一的接口来定义、管理和执行各种工具。每个工具都是一个 `Runnable`，支持同步/异步执行、流式处理、批量执行和回调集成。

## 核心概念

### Tool 接口

所有工具都实现 `Tool` 接口：

```go
type Tool interface {
    Runnable[*ToolInput, *ToolOutput]

    Name() string
    Description() string
    ArgsSchema() string  // JSON Schema for arguments
}
```

### 工具输入输出

```go
type ToolInput struct {
    Args     map[string]interface{} // 参数映射
    Context  context.Context        // 上下文
    CallerID string                 // 调用者 ID
    TraceID  string                 // 追踪 ID
}

type ToolOutput struct {
    Result   interface{}            // 结果数据
    Success  bool                   // 是否成功
    Error    string                 // 错误信息
    Metadata map[string]interface{} // 元数据
}
```

## 内置工具

### 1. BaseTool

基础工具实现，可以包装任意函数：

```go
tool := tools.NewBaseTool(
    "hello",
    "Says hello to a name",
    `{"type": "object", "properties": {"name": {"type": "string"}}}`,
    func(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
        name, _ := input.Args["name"].(string)
        return &tools.ToolOutput{
            Result:  fmt.Sprintf("Hello, %s!", name),
            Success: true,
        }, nil
    },
)
```

### 2. FunctionTool

函数包装工具，提供更灵活的创建方式：

```go
// 基础用法
tool := tools.NewFunctionTool(
    "calculator",
    "Performs calculations",
    `{...}`,
    func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        // 实现逻辑
        return result, nil
    },
)

// 使用构建器
tool := tools.NewFunctionToolBuilder("multiplier").
    WithDescription("Multiplies two numbers").
    WithArgsSchema(`{...}`).
    WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        a := args["a"].(float64)
        b := args["b"].(float64)
        return a * b, nil
    }).
    Build()

// 简单函数工具
tool := tools.NewSimpleFunctionTool(
    "get_time",
    "Gets current time",
    func(ctx context.Context) (interface{}, error) {
        return time.Now().String(), nil
    },
)

// 类型安全工具
type CalculatorInput struct {
    Operation string  `json:"operation"`
    A         float64 `json:"a"`
    B         float64 `json:"b"`
}

tool := tools.NewTypedFunctionTool[CalculatorInput, float64](
    "calculator",
    "Performs calculations",
    `{...}`,
    func(ctx context.Context, input CalculatorInput) (float64, error) {
        // 类型安全的实现
        return result, nil
    },
)
```

### 3. CalculatorTool

数学计算工具：

```go
tool := tools.NewCalculatorTool()

output, _ := tool.Invoke(ctx, &tools.ToolInput{
    Args: map[string]interface{}{
        "expression": "2 + 3 * 4",
    },
})

// 支持的表达式：
// - 基本运算: +, -, *, /
// - 幂运算: ^
// - 括号: ()
// - 示例: "2 + 3", "(2 + 3) * 4", "2^8"
```

高级计算器工具：

```go
tool := tools.NewAdvancedCalculatorTool(tools.CalculatorOperations{
    Add:      true,
    Subtract: true,
    Multiply: true,
    Divide:   true,
    Power:    true,
    Sqrt:     true,
    Abs:      true,
})

output, _ := tool.Invoke(ctx, &tools.ToolInput{
    Args: map[string]interface{}{
        "operation": "sqrt",
        "operands":  []interface{}{16.0},
    },
})
```

### 4. SearchTool

搜索工具，支持多种搜索引擎：

```go
// 使用模拟搜索引擎
engine := tools.NewMockSearchEngine()
tool := tools.NewSearchTool(engine)

// 使用 Google 搜索
engine := tools.NewGoogleSearchEngine(apiKey, cx)
tool := tools.NewSearchTool(engine)

// 使用聚合搜索引擎
engine := tools.NewAggregatedSearchEngine(
    tools.NewGoogleSearchEngine(apiKey, cx),
    tools.NewDuckDuckGoSearchEngine(),
)
tool := tools.NewSearchTool(engine)

// 执行搜索
output, _ := tool.Invoke(ctx, &tools.ToolInput{
    Args: map[string]interface{}{
        "query":       "Go programming",
        "max_results": 5.0,
    },
})
```

### 5. ShellTool

安全的 Shell 命令执行工具：

```go
// 使用构建器
tool := tools.NewShellToolBuilder().
    WithAllowedCommands("ls", "pwd", "echo", "cat").
    WithTimeout(30 * time.Second).
    Build()

// 执行命令
output, _ := tool.Invoke(ctx, &tools.ToolInput{
    Args: map[string]interface{}{
        "command": "echo",
        "args":    []interface{}{"Hello, World!"},
        "work_dir": "/tmp",
        "timeout": 10,
    },
})

// 便捷方法
output, _ := tool.ExecuteScript(ctx, "/path/to/script.sh", []string{"arg1", "arg2"})
output, _ := tool.ExecutePipeline(ctx, []string{"ls -la", "grep test"})
```

### 6. APITool

HTTP API 调用工具：

```go
// 使用构建器
tool := tools.NewAPIToolBuilder().
    WithBaseURL("https://api.example.com").
    WithTimeout(30 * time.Second).
    WithHeader("Content-Type", "application/json").
    WithAuth("your-token").
    Build()

// 执行请求
output, _ := tool.Invoke(ctx, &tools.ToolInput{
    Args: map[string]interface{}{
        "method": "POST",
        "url":    "/api/users",
        "body": map[string]interface{}{
            "name": "John Doe",
            "email": "john@example.com",
        },
        "headers": map[string]string{
            "X-Custom-Header": "value",
        },
    },
})

// 便捷方法
output, _ := tool.Get(ctx, "/api/users", headers)
output, _ := tool.Post(ctx, "/api/users", body, headers)
output, _ := tool.Put(ctx, "/api/users/1", body, headers)
output, _ := tool.Delete(ctx, "/api/users/1", headers)
output, _ := tool.Patch(ctx, "/api/users/1", body, headers)
```

## 工具集 (Toolkit)

### 创建工具集

```go
// 标准工具集
toolkit := tools.NewStandardToolkit()

// 开发工具集
toolkit := tools.NewDevelopmentToolkit()

// 自定义工具集
toolkit := tools.NewBaseToolkit(
    tools.NewCalculatorTool(),
    tools.NewSearchTool(engine),
    tools.NewShellTool(commands, timeout),
)

// 使用构建器
toolkit := tools.NewToolkitBuilder().
    WithCalculator().
    WithSearch(engine).
    WithShell(allowedCommands).
    WithAPI(baseURL, headers).
    AddTool(customTool).
    Build()
```

### 使用工具集

```go
// 获取所有工具
tools := toolkit.GetTools()

// 获取工具名称
names := toolkit.GetToolNames()

// 根据名称获取工具
tool, err := toolkit.GetToolByName("calculator")

// 执行工具
output, err := tool.Invoke(ctx, input)
```

## 工具注册表

全局工具注册和发现：

```go
// 创建注册表
registry := tools.NewToolRegistry()

// 注册工具
_ = registry.Register(tools.NewCalculatorTool())
_ = registry.Register(customTool)

// 获取工具
tool, _ := registry.Get("calculator")

// 列出所有工具
allTools := registry.List()

// 从注册表创建工具集
toolkit, _ := registry.CreateToolkit("calculator", "search")

// 使用全局注册表
tools.RegisterTool(myTool)
tool, _ := tools.GetTool("myTool")
allTools := tools.ListTools()
```

## 工具执行器

高级工具执行功能：

```go
executor := tools.NewToolExecutor(toolkit)

// 执行单个工具
output, err := executor.Execute(ctx, "calculator", input)

// 顺序执行多个工具
executor.WithParallel(false)
results, err := executor.ExecuteMultiple(ctx, map[string]*tools.ToolInput{
    "calculator": calcInput,
    "search": searchInput,
})

// 并行执行多个工具
executor.WithParallel(true)
results, err := executor.ExecuteMultiple(ctx, requests)
```

## 回调集成

工具支持完整的回调系统：

```go
// 创建回调
callback := &MyCallback{
    BaseCallback: agentcore.NewBaseCallback(),
}

func (c *MyCallback) OnToolStart(ctx context.Context, toolName string, input interface{}) error {
    fmt.Printf("Tool '%s' started\n", toolName)
    return nil
}

func (c *MyCallback) OnToolEnd(ctx context.Context, toolName string, output interface{}) error {
    fmt.Printf("Tool '%s' completed\n", toolName)
    return nil
}

// 为工具添加回调
toolWithCallback := tool.WithCallbacks(callback).(tools.Tool)

// 执行会触发回调
output, _ := toolWithCallback.Invoke(ctx, input)
```

## Runnable 特性

所有工具都是 Runnable，支持：

### 1. 单个执行

```go
output, err := tool.Invoke(ctx, input)
```

### 2. 流式执行

```go
stream, err := tool.Stream(ctx, input)
for chunk := range stream {
    if chunk.Error != nil {
        // 处理错误
    }
    // 处理数据
}
```

### 3. 批量执行

```go
inputs := []*tools.ToolInput{input1, input2, input3}
outputs, err := tool.Batch(ctx, inputs)
```

### 4. 管道连接

```go
// 连接工具形成管道
pipeline := tool1.Pipe(tool2).Pipe(tool3)
output, err := pipeline.Invoke(ctx, input)
```

### 5. 配置

```go
config := agentcore.RunnableConfig{
    MaxConcurrency: 10,
    RetryPolicy: &agentcore.RetryPolicy{
        MaxRetries: 3,
    },
}

toolWithConfig := tool.WithConfig(config)
```

## 最佳实践

### 1. 安全性

- Shell 工具始终使用白名单
- API 工具验证输入
- 设置合理的超时时间

```go
// 好的做法
tool := tools.NewShellTool([]string{"ls", "cat", "grep"}, 30*time.Second)

// 不好的做法 - 不要允许所有命令
tool := tools.NewShellTool([]string{"bash", "sh"}, 0)
```

### 2. 错误处理

始终检查工具执行结果：

```go
output, err := tool.Invoke(ctx, input)
if err != nil {
    // 处理执行错误
    return err
}

if !output.Success {
    // 处理业务错误
    log.Printf("Tool failed: %s", output.Error)
}
```

### 3. 使用构建器

使用构建器创建复杂工具：

```go
tool := tools.NewAPIToolBuilder().
    WithBaseURL("https://api.example.com").
    WithTimeout(30 * time.Second).
    WithAuth(token).
    Build()
```

### 4. 工具组合

使用工具集和执行器组合多个工具：

```go
toolkit := tools.NewToolkitBuilder().
    WithCalculator().
    WithSearch(engine).
    WithAPI(baseURL, nil).
    Build()

executor := tools.NewToolExecutor(toolkit).
    WithParallel(true)
```

### 5. 监控和日志

使用回调进行监控：

```go
metricsCallback := &MetricsCallback{}
loggingCallback := &LoggingCallback{}

tool := tool.WithCallbacks(metricsCallback, loggingCallback)
```

## 示例

完整示例见 `examples/tools/main.go`：

```bash
# 运行示例
go run examples/tools/main.go
```

## 测试

运行测试：

```bash
# 运行所有测试
go test -v ./tools/

# 运行性能测试
go test -bench=. ./tools/

# 查看覆盖率
go test -cover ./tools/
```

## 扩展

### 创建自定义工具

```go
type MyCustomTool struct {
    *tools.BaseTool
    config MyConfig
}

func NewMyCustomTool(config MyConfig) *MyCustomTool {
    tool := &MyCustomTool{
        config: config,
    }

    tool.BaseTool = tools.NewBaseTool(
        "my_tool",
        "My custom tool description",
        `{"type": "object", "properties": {...}}`,
        tool.run,
    )

    return tool
}

func (m *MyCustomTool) run(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
    // 实现工具逻辑
    return &tools.ToolOutput{
        Result:  result,
        Success: true,
    }, nil
}
```

### 创建自定义搜索引擎

```go
type MySearchEngine struct{}

func (m *MySearchEngine) Search(ctx context.Context, query string, maxResults int) ([]tools.SearchResult, error) {
    // 实现搜索逻辑
    return results, nil
}

// 使用自定义搜索引擎
engine := &MySearchEngine{}
tool := tools.NewSearchTool(engine)
```

## 架构设计

```
Tool (Interface)
    ├── Runnable[*ToolInput, *ToolOutput]
    ├── Name() string
    ├── Description() string
    └── ArgsSchema() string

BaseTool
    ├── BaseRunnable
    ├── name, description, argsSchema
    └── runFunc

具体工具
    ├── FunctionTool
    ├── CalculatorTool
    ├── SearchTool
    ├── ShellTool
    └── APITool

工具集
    ├── Toolkit (Interface)
    ├── BaseToolkit
    ├── StandardToolkit
    └── DevelopmentToolkit

工具注册表
    └── ToolRegistry

工具执行器
    └── ToolExecutor
```

## 性能考虑

1. **并发执行**: 使用 `ToolExecutor.WithParallel(true)` 并行执行多个工具
2. **批量处理**: 使用 `Batch()` 方法批量处理多个输入
3. **资源池**: Shell 和 API 工具内部使用连接池
4. **缓存**: 考虑为搜索结果等添加缓存层

## 总结

Tools 系统提供了：

- 统一的工具接口和实现
- 丰富的内置工具（计算器、搜索、Shell、API）
- 灵活的工具组合和管理（Toolkit、Registry）
- 完整的 Runnable 特性支持
- 强大的回调和监控能力
- 类型安全和易用的 API

通过这个系统，你可以轻松创建、组合和执行各种工具，构建强大的 AI Agent 应用。
