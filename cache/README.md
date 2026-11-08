# cache 缓存系统

通用键值缓存，供 LLM 调用结果缓存和工具缓存中间件（`tools/middleware.Caching`）使用。

## 核心类型

| 名称 | 说明 |
|---|---|
| `Cache` | 接口：`Get`/`Set`/`Delete`/`Clear`/`Has`/`GetStats` |
| `SimpleCache` | 内置实现，`sync.Map` + TTL，默认后台每 `ttl/2` 清理一次过期条目，`Close()` 停止清理；访问驱动模式见 `WithoutBackgroundCleanup` |
| `NoOpCache` | 禁用实现：不存储条目，读写均返回 `agentErrors.ErrCacheDisabled` |
| `CacheKeyGenerator` | 基于 SHA256 的键生成器 |
| `CacheStats` | 统计信息；`SimpleCache` 填写 `Hits`、`Misses`、`Size`、`HitRate` |
| `CacheConfig` | 配置；`NewCacheFromConfig` 在 `Enabled` 为 false 时返回 `NoOpCache`，否则返回以 `DefaultTTL` 构造的 `SimpleCache` |

缓存错误统一使用 `github.com/kart-io/goagent/errors` 的 `ErrCacheMiss`、`ErrCacheInvalid`、`ErrCacheDisabled`。

## 使用方法

### 基础操作

示例中的 `ctx` 为调用方提供的 `context.Context`，导入如下：

```go
import (
    "errors"
	"fmt"
    "time"

    "github.com/kart-io/goagent/cache"
    agentErrors "github.com/kart-io/goagent/errors"
)
```

```go
c := cache.NewSimpleCache(5 * time.Minute) // ttl <= 0 时取 5 分钟
defer c.Close()

_ = c.Set(ctx, "key", "value", 10*time.Minute) // ttl <= 0 时使用构造时的默认 TTL

value, err := c.Get(ctx, "key")
if errors.Is(err, agentErrors.ErrCacheMiss) {
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
resp, err := llm.Complete(ctx, client, req)
if err != nil {
    return nil, err
}
_ = llmCache.Set(ctx, key, resp, 5*time.Minute)
return resp, nil
```

### 工具执行缓存

```go
c := cache.NewSimpleCache(10 * time.Minute)
defer c.Close()
cachedTool := tools.WithMiddleware(originalTool, middleware.Caching(
    middleware.WithCache(c),
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
if enabled, ok := c.(*cache.SimpleCache); ok {
    defer enabled.Close()
}
```

`CacheConfig` 仅接受实际执行的 `Enabled` 和 `DefaultTTL`。旧 `Type`、`MaxSize`、`CleanupInterval` 已删除；配置文件须移除这些字段，使用统一 codec 的 Strict 解码时会拒绝旧字段。

`NewInMemoryCache`、`NewLRUCache`、`NewMultiTierCache` 及其类型已删除，不提供旧名转发。`SimpleCache` 按 TTL 过期，不承诺容量上限、LRU 或多级回源；需要这些能力的宿主应提供实现 `Cache` 的缓存，并自行管理其资源生命周期。

## 代码结构

```text
cache/
├── base.go          # 接口、配置、键生成器、NoOpCache
└── simple_cache.go  # SimpleCache
```

## 扩展阅读

- [llm/cache](../llm/cache/) - LLM 语义缓存
- [tools/middleware](../tools/middleware/) - 工具缓存中间件
