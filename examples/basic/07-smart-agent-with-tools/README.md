# 智能 Agent 示例 - 时间获取与 API 调用

这个示例演示如何创建一个智能 Agent，实现以下功能：

1. **获取当前时间** - 支持不同时区和格式
2. **调用 API 接口** - 进行 HTTP 请求（GET/POST）
3. **集成多个工具** - 将多个工具组合到一个 Agent 中

## 功能特性

### 1. 时间工具 (get_current_time)

获取当前时间，支持：
- 自定义时区（如 Asia/Shanghai、UTC、America/New_York）
- 自定义时间格式
- 返回详细的时间信息（年、月、日、时、分、秒、星期）

### 2. API 调用工具 (api_tool)

支持各种 HTTP 请求：
- GET 请求获取数据
- POST 请求提交数据
- 自定义请求头
- 超时控制

### 3. 天气查询工具 (get_weather)

查询城市天气信息：
- 温度、湿度、风速
- 天气状况描述
- 更新时间

## 使用方法

### 运行示例

```bash
cd examples/basic/07-smart-agent-with-tools
go run main.go
```

### 示例输出

```
=== 智能 Agent 示例 - 时间获取与 API 调用 ===

--- 示例 1: 获取当前时间工具 ---
当前时间: 2025-11-15 10:30:45
时区: Asia/Shanghai
Unix 时间戳: 1731636645

--- 示例 2: API 调用工具 ---
2.1: GET 请求获取用户信息
用户名: Leanne Graham
邮箱: Sincere@april.biz
城市: Gwenborough

2.2: GET 请求获取文章列表
获取到 3 篇文章:
1. sunt aut facere repellat provident...
2. qui est esse...
3. ea molestias quasi...

2.3: POST 请求创建新文章
创建成功! ID: 101
标题: 智能 Agent 测试文章

--- 示例 3: 集成智能 Agent ---
可用工具:
1. get_current_time - 获取当前时间，支持不同的时区和格式
2. api_tool - Make HTTP API requests
3. get_weather - 查询指定城市的天气信息

演示场景：获取当前时间并查询天气信息

步骤 1: 获取当前时间
✓ 当前时间: 2025-11-15 10:30:45

步骤 2: 查询天气信息
✓ 城市: Beijing
✓ 天气: 晴朗
✓ 温度: 22°C
✓ 湿度: 65%
```

## 代码结构

```
07-smart-agent-with-tools/
├── main.go          # 主程序文件
└── README.md        # 说明文档
```

## 核心代码示例

### 创建时间工具

```go
timeTool := tools.NewFunctionToolBuilder("get_current_time").
    WithDescription("获取当前时间，支持不同的时区和格式").
    WithArgsSchema(`{
        "type": "object",
        "properties": {
            "format": {"type": "string"},
            "timezone": {"type": "string"}
        }
    }`).
    WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        // 实现时间获取逻辑
        now := time.Now()
        return map[string]interface{}{
            "time": now.Format("2006-01-02 15:04:05"),
            "timestamp": now.Unix(),
        }, nil
    }).
    Build()
```

### 创建 API 工具

```go
apiTool := http.NewAPIToolBuilder().
    WithBaseURL("https://api.example.com").
    WithTimeout(30 * time.Second).
    Build()

// 使用工具
output, err := apiTool.Invoke(ctx, &tools.ToolInput{
    Args: map[string]interface{}{
        "method": "GET",
        "url": "/endpoint",
    },
})
```

### 集成到 Agent（需要 LLM 配置）

```go
agent, err := builder.NewAgentBuilder().
    WithName("SmartAssistant").
    WithDescription("智能助手").
    WithOpenAI(os.Getenv("OPENAI_API_KEY"), "gpt-4").
    WithTools(timeTool, apiTool, weatherTool).
    Build()

result, err := agent.Invoke(ctx, map[string]interface{}{
    "input": "现在几点了？北京的天气怎么样？",
})
```

## 扩展建议

### 1. 添加更多工具

```go
// 数据库查询工具
dbTool := createDatabaseTool()

// 文件操作工具
fileTool := createFileOperationTool()

// 邮件发送工具
emailTool := createEmailTool()
```

### 2. 使用真实的天气 API

```go
// 集成 OpenWeatherMap API
func createRealWeatherTool() tools.Tool {
    return tools.NewFunctionToolBuilder("get_weather").
        WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
            city := args["city"].(string)
            apiKey := os.Getenv("OPENWEATHER_API_KEY")
            url := fmt.Sprintf("https://api.openweathermap.org/data/2.5/weather?q=%s&appid=%s", city, apiKey)
            // 发起实际的 HTTP 请求
            // ...
        }).
        Build()
}
```

### 3. 添加缓存机制

```go
// 缓存时间查询结果
cachedTimeTool := cache.WithCache(timeTool, cache.NewMemoryCache(5*time.Minute))

// 缓存 API 响应
cachedAPITool := cache.WithCache(apiTool, cache.NewMemoryCache(10*time.Minute))
```

### 4. 添加重试机制

```go
// 为 API 调用添加重试
retryAPITool := retry.WithRetry(apiTool, retry.Config{
    MaxAttempts: 3,
    Backoff: retry.ExponentialBackoff,
})
```

## 常见问题

### Q: 如何使用完整的 LLM Agent？

A: 需要配置 LLM API Key：

```bash
export OPENAI_API_KEY=your_api_key
# 或使用其他提供商
export ANTHROPIC_API_KEY=your_api_key
```

### Q: 如何调用真实的 API？

A: 修改 API 工具的 URL 和参数：

```go
output, err := apiTool.Invoke(ctx, &tools.ToolInput{
    Args: map[string]interface{}{
        "method": "GET",
        "url": "https://your-api.com/endpoint",
        "headers": map[string]interface{}{
            "Authorization": "Bearer your_token",
        },
    },
})
```

### Q: 如何添加自定义工具？

A: 使用 `NewFunctionToolBuilder` 或 `NewBaseTool`：

```go
customTool := tools.NewFunctionToolBuilder("my_tool").
    WithDescription("我的自定义工具").
    WithArgsSchema(`{"type": "object", "properties": {...}}`).
    WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        // 你的实现逻辑
        return result, nil
    }).
    Build()
```

## 相关资源

- [GoAgent 文档](../../README.md)
- [工具系统详解](../02-tools/README.md)
- [Agent 构建指南](../../docs/guides/)
- [API 参考](../../docs/api/)

## 许可证

与 GoAgent 项目保持一致

