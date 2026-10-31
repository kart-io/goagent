# 缓存指南

GoAgent 不默认缓存任何模型答案或工具结果（`ConfigureForRAG` 也不缓存）。需要缓存时按缓存对象选择组件：

| 缓存对象 | 组件 | 说明 |
|---|---|---|
| 通用键值 | `cache.Cache`；推荐 `cache.NewSimpleCache(ttl)` | `sync.Map` + TTL，后台清理；`NoOpCache` 为空实现。包说明见 [cache/README.md](../../cache/README.md) |
| 单个工具的结果 | `tools/middleware.Caching`（经 `tools.WithMiddleware`） | 底层是任意 `cache.Cache`，见[工具中间件](TOOL_MIDDLEWARE.md#内置中间件) |
| 单个工具的结果（简化包装） | `tools.NewCachedTool(tool, ttl)` | 内部使用 `tools.SimpleToolCache`；ttl<=0 时取 5 分钟 |
| 工具结果存储 | `tools.ToolCache` 接口：`SimpleToolCache`、`MemoryToolCache`、`ShardedToolCache` | 按工具名或正则失效；分片版预设 `NewHighPerformanceCache`/`NewLowMemoryCache`，示例见 [02-cache-presets](../../examples/optimization/02-cache-presets/main.go) |
| Agent 输入 → 响应 | `core/middleware.NewCacheMiddleware(ttl)`（经 `builder.WithMiddleware`） | 命中时跳过处理器；键只取输入的 `%v` 文本，不区分会话、用户或知识版本 |
| 语义相近的模型请求 | `llm/cache.NewCachedLLMClient` / `WithSemanticCache` | 见下文限制 |

## 通用键值缓存

```go
c := cache.NewSimpleCache(5 * time.Minute) // ttl <= 0 时取 5 分钟
defer c.Close()

_ = c.Set(ctx, "key", "value", 10*time.Minute) // ttl <= 0 时使用构造时的 TTL
value, err := c.Get(ctx, "key")
if errors.Is(err, cache.ErrCacheMiss) {
    // 未命中或已过期
}
stats := c.GetStats() // Hits、Misses、Size、HitRate
```

`cache.NewCacheKeyGenerator(prefix)` 用 SHA256 生成键（`GenerateKey(prompt, params)`、`GenerateKeySimple(parts...)`）。`cache.NewCacheFromConfig(cfg)` 在 `Enabled=false` 时返回 `NoOpCache`，否则返回以 `DefaultTTL` 构造的 `SimpleCache`（不读取 `Type`）。

`NewInMemoryCache`、`NewLRUCache`、`NewMultiTierCache` 已标记 Deprecated：

| 旧 | 新 |
|---|---|
| `cache.NewInMemoryCache(maxSize, ttl, cleanup)` | `cache.NewSimpleCache(ttl)` |
| `cache.NewLRUCache(maxSize, ttl, cleanup)` | `cache.NewSimpleCache(ttl)`（不限容量，按 TTL 过期） |
| `cache.NewMultiTierCache(tiers...)` | `cache.NewSimpleCache(ttl)`，或宿主自行组合 |

## 工具结果缓存

```go
cachedTool := tools.WithMiddleware(originalTool, middleware.Caching(
    middleware.WithCache(cache.NewSimpleCache(10*time.Minute)),
    middleware.WithTTL(10*time.Minute),
))
```

- 只缓存成功结果；默认键只含工具名与参数，多租户或按用户返回不同结果的工具必须用 `WithCacheKeyFunc` 把身份放进键，或不缓存。
- 有副作用的工具不要缓存：命中时工具不执行。
- 持久运行已由 Dispatcher 调用日志保证恢复时回放、不重复执行；工具缓存解决的是跨运行的重复计算，二者无关。包装后的工具不转发 `DispatchTimeoutProvider` 等可选接口，注意事项见[工具中间件](TOOL_MIDDLEWARE.md#toolswithmiddleware)。

## 语义缓存（llm/cache）

```go
client, err := llmcache.NewCachedLLMClient(&llmcache.CachedLLMClientConfig{
    Client:            baseClient,
    EmbeddingProvider: embedder, // Embed、EmbedBatch、Dimension；llmcache.NewOpenAIEmbeddingProvider 是一个实现
    CacheConfig:       llmcache.DefaultSemanticCacheConfig(),
})
if err != nil { return err }
defer client.Close()
resp, err := client.Complete(ctx, req)
```

`llmcache` 指 `github.com/kart-io/goagent/llm/cache`。把消息按 `role: content` 拼接后求向量，相似度达到 `SimilarityThreshold`（默认 0.95）且模型相同时（`ModelSpecific`，默认开启）返回缓存；默认最多 10000 条、TTL 24 小时。`WithSemanticCache(embedder, cfg)` 返回 `func(llm.Client) llm.Client`，创建失败时原样返回原客户端。

限制：

- 包装后的客户端只实现 `llm.Client`（`Complete`、`Chat`），不实现 `StreamClient`/`ToolStreamClient`，因此不能用于 Builder 的 `AsRunnable`、`ExecuteStream`、原生工具循环或持久运行。
- 键不含工具、温度等请求参数；命中只返回文本（`FinishReason="cache_hit"`、`TokensUsed=0`），不含工具调用与用量。
- 缓存在进程内存中，`Stats()` 返回命中统计，`ClearCache` 清空。

## 选用建议

- 缓存键必须覆盖影响结果的全部维度：用户/租户、会话、知识或数据版本、工具版本。
- 检索增强回答优先每次检索新证据；需要答案缓存时，键中带上知识版本。
- TTL 按数据变化频率设置；`SimpleCache` 不限容量，条目数不可控时改用 `tools.ShardedToolCache`（容量与淘汰策略可配）或外部缓存实现 `cache.Cache`。
