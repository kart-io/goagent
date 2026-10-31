# cache 缓存系统

通用键值缓存，供 LLM 调用结果缓存和工具缓存中间件（`tools/middleware.Caching`）使用。

## 核心类型

| 名称 | 说明 |
|---|---|
| `Cache` | 接口：`Get`/`Set`/`Delete`/`Clear`/`Has`/`GetStats` |
| `SimpleCache` | 推荐实现，`sync.Map` + TTL，后台每 `ttl/2` 清理一次过期条目，`Close()` 停止清理 |
| `NoOpCache` | 空实现：`Set` 不存储，`Get` 总是返回 `ErrCacheMiss` |
| `CacheKeyGenerator` | 基于 SHA256 的键生成器 |
| `CacheStats` | 统计信息；`SimpleCache` 填写 `Hits`、`Misses`、`Size`、`HitRate` |
| `CacheConfig` | 配置；`NewCacheFromConfig` 在 `Enabled` 为 false 时返回 `NoOpCache`，否则返回以 `DefaultTTL` 构造的 `SimpleCache` |

错误：`ErrCacheMiss`、`ErrCacheInvalid`、`ErrCacheDisabled`。

## 使用方法

### 基础操作

```go
c := cache.NewSimpleCache(5 * time.Minute) // ttl <= 0 时取 5 分钟
defer c.Close()

_ = c.Set(ctx, "key", "value", 10*time.Minute) // ttl <= 0 时使用构造时的默认 TTL

value, err := c.Get(ctx, "key")
if errors.Is(err, cache.ErrCacheMiss) {
    // 未命中或已过期
}

exists, _ := c.Has(ctx, "key")
_ = c.Delete(ctx, "key")
_ = c.Clear(ctx)

stats := c.GetStats()
fmt.Printf("命中率: %.2f%%\n", stats.HitRate*100)
```

### LLM 调用缓存

```go
keyGen := cache.NewCacheKeyGenerator("llm")
key := keyGen.GenerateKey(prompt, map[string]interface{}{
    "temperature": 0.7,
    "model":       "gpt-4",
})

if cached, err := llmCache.Get(ctx, key); err == nil {
    return cached.(*llm.CompletionResponse), nil
}
resp, err := client.Complete(ctx, req)
if err != nil {
    return nil, err
}
_ = llmCache.Set(ctx, key, resp, 5*time.Minute)
return resp, nil
```

### 工具执行缓存

```go
cachedTool := tools.WithMiddleware(originalTool, middleware.Caching(
    middleware.WithCache(cache.NewSimpleCache(10*time.Minute)),
    middleware.WithTTL(10*time.Minute),
))
result, err := cachedTool.Invoke(ctx, input)
```

### 键生成

```go
gen := cache.NewCacheKeyGenerator("llm")
gen.GenerateKey("hello world", map[string]interface{}{"temp": 0.7}) // "llm:" + SHA256(prompt|参数JSON)
gen.GenerateKeySimple("model", "gpt4", "system")                     // "llm:" + SHA256("model|gpt4|system|")
```

前缀为空时只返回十六进制哈希。

### 基于配置创建

```go
config := cache.DefaultCacheConfig() // Enabled=true，DefaultTTL=5m
config.DefaultTTL = 10 * time.Minute
c := cache.NewCacheFromConfig(config)
```

`CacheConfig` 的 `Type`、`MaxSize`、`CleanupInterval` 当前不影响 `NewCacheFromConfig` 的结果。

## 代码结构

```text
cache/
├── base.go          # 接口、配置、键生成器、NoOpCache
└── simple_cache.go  # SimpleCache
```

## 扩展阅读

- [llm/cache](../llm/cache/) - LLM 语义缓存
- [tools/middleware](../tools/middleware/) - 工具缓存中间件
