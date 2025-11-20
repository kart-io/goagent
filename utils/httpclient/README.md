# HTTP 客户端使用指南

## 概述

`utils/httpclient` 包提供了统一的 HTTP 客户端管理，封装了 Resty 库，提供了更简洁的 API 和更好的配置管理。

## 特性

- ✅ 统一的客户端实例管理（单例模式）
- ✅ 灵活的配置选项（超时、重试、请求头等）
- ✅ 链式调用支持
- ✅ 自动 JSON 编解码
- ✅ 连接池管理
- ✅ 调试模式支持

## 快速开始

### 1. 使用默认客户端（单例）

```go
package main

import (
    "context"
    "fmt"

    "github.com/kart-io/goagent/utils/httpclient"
)

func main() {
    // 获取默认客户端（单例）
    client := httpclient.Default()

    // 发送 GET 请求
    resp, err := client.R().
        SetContext(context.Background()).
        Get("https://api.example.com/users")

    if err != nil {
        fmt.Printf("Request failed: %v\n", err)
        return
    }

    fmt.Printf("Status: %d\n", resp.StatusCode())
    fmt.Printf("Body: %s\n", string(resp.Body()))
}
```

### 2. 创建自定义客户端

```go
package main

import (
    "time"

    "github.com/kart-io/goagent/utils/httpclient"
)

func main() {
    // 创建自定义配置
    config := &httpclient.Config{
        Timeout:      10 * time.Second,
        RetryCount:   3,
        BaseURL:      "https://api.example.com",
        Headers: map[string]string{
            "User-Agent": "MyApp/1.0",
        },
        Debug: true,
    }

    // 创建客户端
    client := httpclient.NewClient(config)

    // 发送请求
    resp, err := client.R().Get("/users")
    // ...
}
```

### 3. 链式调用

```go
// 创建并配置客户端
client := httpclient.NewClient(nil).
    SetTimeout(20 * time.Second).
    SetRetryCount(5).
    SetHeader("Authorization", "Bearer token").
    SetBaseURL("https://api.example.com")

// 发送 POST 请求
resp, err := client.R().
    SetHeader("Content-Type", "application/json").
    SetBody(map[string]interface{}{
        "name": "John",
        "age":  30,
    }).
    Post("/users")
```

### 4. 在 Tools 中使用

```go
package tools

import (
    "context"
    "time"

    "github.com/kart-io/goagent/utils/httpclient"
)

func createAPITool() {
    // 创建 HTTP 客户端
    client := httpclient.NewClient(&httpclient.Config{
        Timeout: 30 * time.Second,
        BaseURL: "https://api.example.com",
        Headers: map[string]string{
            "API-Key": "your-api-key",
        },
    })

    // 使用客户端发送请求
    resp, err := client.R().
        SetContext(context.Background()).
        SetQueryParam("page", "1").
        Get("/data")

    // 处理响应...
}
```

## API 文档

### 创建客户端

#### `NewClient(config *Config) *Client`

创建新的 HTTP 客户端。

```go
client := httpclient.NewClient(&httpclient.Config{
    Timeout:    30 * time.Second,
    RetryCount: 3,
    BaseURL:    "https://api.example.com",
})
```

#### `Default() *Client`

获取默认的单例客户端。

```go
client := httpclient.Default()
```

### 配置选项

```go
type Config struct {
    // 请求超时时间（默认：30秒）
    Timeout time.Duration

    // 重试次数（默认：3）
    RetryCount int

    // 重试等待时间（默认：1秒）
    RetryWaitTime time.Duration

    // 最大重试等待时间（默认：5秒）
    RetryMaxWaitTime time.Duration

    // 基础 URL（可选）
    BaseURL string

    // 默认请求头
    Headers map[string]string

    // 是否启用调试模式（默认：false）
    Debug bool

    // 是否禁用 Keep-Alive（默认：false）
    DisableKeepAlive bool

    // 每个主机的最大空闲连接数（默认：100）
    MaxIdleConnsPerHost int
}
```

### 客户端方法

#### 发送请求

```go
// 创建请求
req := client.R()

// GET 请求
resp, err := req.Get(url)

// POST 请求
resp, err := req.Post(url)

// PUT 请求
resp, err := req.Put(url)

// DELETE 请求
resp, err := req.Delete(url)

// PATCH 请求
resp, err := req.Patch(url)

// 通用方法
resp, err := req.Execute(method, url)
```

#### 配置客户端

```go
// 设置超时
client.SetTimeout(20 * time.Second)

// 设置重试次数
client.SetRetryCount(5)

// 设置请求头
client.SetHeader("Authorization", "Bearer token")
client.SetHeaders(map[string]string{
    "X-Custom-1": "value1",
    "X-Custom-2": "value2",
})

// 设置基础 URL
client.SetBaseURL("https://api.example.com")

// 启用调试模式
client.SetDebug(true)

// 获取配置
config := client.Config()
```

#### 访问底层客户端

```go
// 获取 resty 客户端
restyClient := client.Resty()

// 或使用别名方法（已弃用）
restyClient := client.GetClient()
```

## 完整示例

### API 工具实现

```go
package http

import (
    "context"
    "time"

    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/utils/httpclient"
)

type APITool struct {
    client  *httpclient.Client
    baseURL string
}

func NewAPITool(baseURL string, timeout time.Duration) *APITool {
    config := &httpclient.Config{
        Timeout: timeout,
        BaseURL: baseURL,
    }

    return &APITool{
        client:  httpclient.NewClient(config),
        baseURL: baseURL,
    }
}

func (a *APITool) Execute(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
    method := input.Args["method"].(string)
    url := input.Args["url"].(string)

    // 创建请求
    req := a.client.R().SetContext(ctx)

    // 设置请求体（如果有）
    if body, ok := input.Args["body"]; ok {
        req.SetBody(body)
    }

    // 执行请求
    resp, err := req.Execute(method, url)
    if err != nil {
        return &interfaces.ToolOutput{
            Success: false,
            Error:   err.Error(),
        }, err
    }

    return &interfaces.ToolOutput{
        Success: true,
        Result: map[string]interface{}{
            "status_code": resp.StatusCode(),
            "body":        string(resp.Body()),
        },
    }, nil
}
```

## 最佳实践

### 1. 使用单例模式

对于大多数应用，使用默认客户端就足够了：

```go
client := httpclient.Default()
```

### 2. 自定义配置

需要特殊配置时，创建独立的客户端实例：

```go
apiClient := httpclient.NewClient(&httpclient.Config{
    Timeout:    10 * time.Second,
    RetryCount: 5,
    BaseURL:    "https://api.example.com",
})
```

### 3. 错误处理

始终检查错误并提供有意义的错误信息：

```go
resp, err := client.R().Get(url)
if err != nil {
    return fmt.Errorf("HTTP request failed: %w", err)
}

if !resp.IsSuccess() {
    return fmt.Errorf("HTTP request failed with status %d", resp.StatusCode())
}
```

### 4. 上下文传递

使用 context 来支持超时和取消：

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

resp, err := client.R().SetContext(ctx).Get(url)
```

### 5. 调试

在开发环境启用调试模式：

```go
if os.Getenv("DEBUG") == "true" {
    client.SetDebug(true)
}
```

## 迁移指南

### 从 net/http 迁移

```go
// 旧代码（net/http）
httpClient := &http.Client{
    Timeout: 30 * time.Second,
}
req, _ := http.NewRequest("GET", url, nil)
req.Header.Set("Authorization", "Bearer token")
resp, _ := httpClient.Do(req)
defer resp.Body.Close()
body, _ := io.ReadAll(resp.Body)

// 新代码（httpclient）
client := httpclient.NewClient(&httpclient.Config{
    Timeout: 30 * time.Second,
})
resp, _ := client.R().
    SetHeader("Authorization", "Bearer token").
    Get(url)
body := resp.Body()
```

### 从直接使用 resty 迁移

```go
// 旧代码（直接使用 resty）
restyClient := resty.New().
    SetTimeout(30 * time.Second).
    SetHeader("Authorization", "Bearer token")
resp, _ := restyClient.R().Get(url)

// 新代码（使用 httpclient）
client := httpclient.NewClient(&httpclient.Config{
    Timeout: 30 * time.Second,
    Headers: map[string]string{
        "Authorization": "Bearer token",
    },
})
resp, _ := client.R().Get(url)
```

## 注意事项

1. **线程安全**：`Default()` 方法使用单例模式，是线程安全的
2. **配置不可变**：配置一旦创建就不应该修改，使用 setter 方法来更新
3. **资源管理**：HTTP 响应体会自动处理，无需手动关闭
4. **性能**：客户端实例可以复用，避免重复创建

## 更多信息

- 源代码：`utils/httpclient/client.go`
- 测试代码：`utils/httpclient/client_test.go`
- Resty 文档：https://github.com/go-resty/resty
