# 02-http-api HTTP API 工具示例

演示 `tools/http` 中的 `APITool`。不调用 LLM，但会请求 `https://httpbin.org`，需要网络。

## 运行

```bash
go run ./examples/tools/02-http-api
```

## 内容

1. **创建工具**：`http.NewAPITool(baseURL, timeout, headers)` 直接创建；`http.NewAPIToolBuilder().WithTimeout(...).WithHeader(...).Build()` 链式创建。
2. **GET**：通过 `Invoke` 传 `method`、`url`、`headers`，或调用便捷方法 `apiTool.Get(ctx, url, headers)`。
3. **POST**：`Invoke` 传 `body`，或 `apiTool.Post(ctx, url, body, headers)`。
4. **PUT / DELETE**：`apiTool.Put`、`apiTool.Delete`。
5. **认证**：`WithBaseURL("https://api.example.com").WithAuth(token)`，`WithAuth` 设置 `Authorization: Bearer <token>`；示例只创建工具，不发请求。
6. **错误处理**：请求 `/status/404`；以及在参数里设置 `"timeout": 1.0` 请求 `/delay/3` 触发超时。

```go
apiTool := http.NewAPITool("", 30*time.Second, map[string]string{
    "User-Agent": "GoAgent-Example/1.0",
})
output, err := apiTool.Invoke(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "method": "GET",
        "url":    "https://httpbin.org/get",
    },
    Context: ctx,
})
```
