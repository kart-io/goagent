# utils/httpclient

封装 [resty v2](https://github.com/go-resty/resty) 的 HTTP 客户端，统一超时、重试、默认请求头与连接池配置，并提供按配置复用的客户端缓存。LLM Provider 等内部组件经它发起请求。

## 创建客户端

```go
// 默认配置
client := httpclient.NewClient(nil)

// 自定义配置：未填写的字段取零值，不会补默认值，因此建议从 DefaultConfig() 修改
cfg := httpclient.DefaultConfig()
cfg.Timeout = 10 * time.Second
cfg.BaseURL = "https://api.example.com"
cfg.Headers["User-Agent"] = "MyApp/1.0"
client = httpclient.NewClient(cfg)

resp, err := client.R().
    SetContext(ctx).
    SetQueryParam("page", "1").
    Get("/users")
if err != nil {
    return err
}
if !resp.IsSuccess() {
    return fmt.Errorf("unexpected status %d", resp.StatusCode())
}
fmt.Println(string(resp.Body()))
```

`R()` 返回 `*resty.Request`，请求构造与发送使用 resty 的 API；`Resty()` 返回底层 `*resty.Client`。

### Config

| 字段 | `DefaultConfig()` 取值 | 说明 |
|---|---|---|
| `Timeout` | 30s | 请求超时 |
| `RetryCount` | 3 | resty 重试次数 |
| `RetryWaitTime` / `RetryMaxWaitTime` | 1s / 5s | 重试等待 |
| `BaseURL` | 空 | |
| `Headers` | 空 map | 默认请求头 |
| `Debug` | false | resty 调试日志 |
| `DisableKeepAlive` | false | |
| `MaxIdleConnsPerHost` | 100 | 每主机空闲连接数；为 0 时使用 net/http 的默认值 |

Transport 基于 `http.DefaultTransport` 克隆，`MaxIdleConns` 固定为 100，`IdleConnTimeout` 为 90 秒。

### 修改配置

```go
client := httpclient.NewClient(nil).
    SetTimeout(20 * time.Second).
    SetRetryCount(5).
    SetHeader("Authorization", "Bearer token").
    SetBaseURL("https://api.example.com")

cfg := client.Config() // 配置副本，修改它不影响客户端
```

另有 `SetHeaders(map[string]string)`、`SetDebug(bool)`。这些方法修改客户端本身，经 `GetOrCreateClient` 取得的共享实例不应再修改。

## 默认客户端与缓存

- `Default()`：首次调用时以默认配置创建，之后返回同一实例。`SetDefault(c)` 替换它（可在首次 `Default()` 之前调用），`ResetDefault()` 使下次 `Default()` 重新创建；三者均并发安全。
- `GetOrCreateClient(cfg)`：按 `BaseURL`、`Headers`、超时、重试、`Debug`、`DisableKeepAlive`、`MaxIdleConnsPerHost` 的哈希复用客户端；`SetCachingEnabled(false)` 后每次新建。`ClearCache()` 清空缓存，`GetCacheStats()` 返回 `cached_clients` 与 `enabled`。

## 日志脱敏

resty 日志写到 stderr。重试警告与 Debug 日志中的 URL 经 `errors.RedactURL`/`RedactURLError` 去掉 userinfo、query 与 fragment；Debug 日志中的 `Authorization`、`Proxy-Authorization`、`Cookie`、`Set-Cookie` 以及名称含 `key`、`token`、`secret` 的头显示为 `[REDACTED]`，实际请求不受影响。

## 测试

```bash
go test ./utils/httpclient/
```
