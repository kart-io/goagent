# 快速入门指南 - 智能 Agent 示例

这是一个快速入门指南，帮助你快速理解和使用智能 Agent 示例。

## 🚀 快速开始

### 1. 运行示例

```bash
cd examples/basic/07-smart-agent-with-tools
go run main.go
```

### 2. 查看输出

程序会依次演示三个示例：

1. **时间获取工具** - 获取当前时间（支持不同时区）
2. **API 调用工具** - 调用真实的 REST API
3. **集成智能 Agent** - 展示工具组合使用

## 📚 核心概念

### 什么是工具（Tool）？

工具是 Agent 可以调用的功能，比如：
- 获取当前时间
- 调用 API 接口
- 查询数据库
- 发送邮件
- ...等等

### 如何创建一个工具？

使用 `FunctionToolBuilder`：

```go
timeTool := tools.NewFunctionToolBuilder("get_current_time").
    WithDescription("获取当前时间").
    WithArgsSchema(`{"type": "object", "properties": {...}}`).
    WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        // 你的实现
        return result, nil
    }).
    Build()
```

### 如何使用工具？

```go
output, err := timeTool.Invoke(ctx, &tools.ToolInput{
    Args: map[string]interface{}{
        "timezone": "Asia/Shanghai",
    },
})

if err != nil {
    log.Fatal(err)
}

fmt.Printf("结果: %v\n", output.Result)
```

## 🔧 示例详解

### 示例 1: 时间工具

**功能：** 获取当前时间，支持不同时区和格式

**代码位置：** `createTimeTool()` 函数

**关键特性：**
- 支持自定义时区（如 Asia/Shanghai, UTC, America/New_York）
- 支持自定义时间格式
- 返回详细时间信息（年、月、日、时、分、秒、星期）

**使用示例：**
```go
output, err := timeTool.Invoke(ctx, &tools.ToolInput{
    Args: map[string]interface{}{
        "format":   "2006-01-02 15:04:05",
        "timezone": "Asia/Shanghai",
    },
})
```

### 示例 2: API 工具

**功能：** 调用 HTTP API 接口

**代码位置：** `createAPITool()` 函数

**支持的方法：**
- GET - 获取数据
- POST - 提交数据
- PUT - 更新数据
- DELETE - 删除数据

**使用示例：**
```go
// GET 请求
output, err := apiTool.Invoke(ctx, &tools.ToolInput{
    Args: map[string]interface{}{
        "method": "GET",
        "url":    "https://api.example.com/users/1",
    },
})

// POST 请求
output, err := apiTool.Invoke(ctx, &tools.ToolInput{
    Args: map[string]interface{}{
        "method": "POST",
        "url":    "https://api.example.com/posts",
        "body": map[string]interface{}{
            "title": "标题",
            "content": "内容",
        },
    },
})
```

### 示例 3: 天气工具

**功能：** 查询城市天气（模拟数据）

**代码位置：** `createWeatherAPITool()` 函数

**使用示例：**
```go
output, err := weatherTool.Invoke(ctx, &tools.ToolInput{
    Args: map[string]interface{}{
        "city": "Beijing",
    },
})
```

## 🤖 集成到 LLM Agent

要创建一个完整的智能 Agent（需要 LLM API Key）：

### 步骤 1: 设置环境变量

```bash
export OPENAI_API_KEY=your_api_key
# 或者使用其他提供商
export ANTHROPIC_API_KEY=your_api_key
```

### 步骤 2: 创建 Agent

```go
import (
    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/llm/providers"
)

// 创建 LLM 客户端
llmClient := providers.NewOpenAIClient(
    os.Getenv("OPENAI_API_KEY"),
    "gpt-4",
)

// 创建 Agent
agent, err := builder.NewAgentBuilder(llmClient).
    WithName("SmartAssistant").
    WithDescription("智能助手").
    WithTools(
        createTimeTool(),
        createWeatherAPITool(),
        createAPITool(),
    ).
    Build()
```

### 步骤 3: 运行 Agent

```go
ctx := context.Background()
state := map[string]interface{}{
    "input": "现在几点了？北京的天气怎么样？",
}

result, err := agent.Invoke(ctx, state)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Agent 回复: %v\n", result["output"])
```

## 🎯 实际应用场景

### 场景 1: 定时任务助手

```go
// Agent 自动获取时间并执行任务
agent.Invoke(ctx, map[string]interface{}{
    "input": "如果现在是下午 3 点之后，请获取用户列表",
})
```

### 场景 2: API 数据分析

```go
// Agent 调用 API 并分析数据
agent.Invoke(ctx, map[string]interface{}{
    "input": "请获取最近 10 篇文章，并总结主要话题",
})
```

### 场景 3: 多步骤任务

```go
// Agent 串联多个工具完成复杂任务
agent.Invoke(ctx, map[string]interface{}{
    "input": "查询北京的天气，如果温度低于 10 度，发送提醒邮件",
})
```

## 🔥 进阶技巧

### 1. 添加错误处理和重试

```go
tool := tools.NewFunctionToolBuilder("api_call").
    WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        maxRetries := 3
        for i := 0; i < maxRetries; i++ {
            result, err := callAPI(args)
            if err == nil {
                return result, nil
            }
            time.Sleep(time.Second * time.Duration(i+1))
        }
        return nil, fmt.Errorf("API 调用失败")
    }).
    Build()
```

### 2. 添加缓存

```go
var cache = make(map[string]interface{})
var cacheMutex sync.RWMutex

tool := tools.NewFunctionToolBuilder("cached_api").
    WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        key := fmt.Sprintf("%v", args)

        // 检查缓存
        cacheMutex.RLock()
        if result, ok := cache[key]; ok {
            cacheMutex.RUnlock()
            return result, nil
        }
        cacheMutex.RUnlock()

        // 调用 API
        result, err := callAPI(args)
        if err != nil {
            return nil, err
        }

        // 存入缓存
        cacheMutex.Lock()
        cache[key] = result
        cacheMutex.Unlock()

        return result, nil
    }).
    Build()
```

### 3. 添加日志和监控

```go
tool := tools.NewFunctionToolBuilder("monitored_tool").
    WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        start := time.Now()

        log.Printf("工具开始执行: %v", args)

        result, err := doWork(args)

        duration := time.Since(start)
        log.Printf("工具执行完成，耗时: %v", duration)

        if err != nil {
            log.Printf("工具执行失败: %v", err)
        }

        return result, err
    }).
    Build()
```

## 📖 相关文档

- [工具系统详解](../02-tools/README.md)
- [Agent 构建指南](../../../docs/guides/)
- [API 参考](../../../docs/api/)

## ❓ 常见问题

### Q: 如何调试工具？

A: 在工具函数中添加日志输出：
```go
WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    log.Printf("输入参数: %+v", args)
    result, err := doWork(args)
    log.Printf("输出结果: %+v", result)
    return result, err
})
```

### Q: 如何处理超时？

A: 使用 context 的超时控制：
```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

output, err := tool.Invoke(ctx, input)
```

### Q: 如何处理并发？

A: 使用 goroutines 并发执行：
```go
results := make(chan *tools.ToolOutput, len(tools))

for _, tool := range tools {
    go func(t tools.Tool) {
        output, _ := t.Invoke(ctx, input)
        results <- output
    }(tool)
}

for i := 0; i < len(tools); i++ {
    result := <-results
    fmt.Printf("结果: %v\n", result)
}
```

## 🎉 下一步

1. 尝试修改示例代码，添加你自己的工具
2. 集成真实的 API（如 OpenWeatherMap）
3. 创建一个完整的 Agent 应用
4. 探索其他示例和高级功能

Happy coding! 🚀

