# 缓存指南

GoAgent 不默认缓存任何模型答案或工具结果（`ConfigureForRAG` 也不缓存）。需要缓存时按缓存对象选择组件：

| 缓存对象 | 组件 | 说明 |
|---|---|---|
| 通用键值 | `cache.Cache`；推荐 `cache.NewSimpleCache(ttl)`；无后台生命周期入口时用 `cache.WithoutBackgroundCleanup()` | `sync.Map` + TTL，后台清理；`NoOpCache` 为空实现。包说明见 [cache/README.md](../../cache/README.md) |
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
if errors.Is(err, agentErrors.ErrCacheMiss) {
    // 未命中或已过期
}
stats := c.GetStats() // Hits、Misses、Size、HitRate
```

`cache.NewCacheKeyGenerator(prefix)` 用 SHA256 生成键（`GenerateKey(prompt, params)`、`GenerateKeySimple(parts...)`）。`cache.NewCacheFromConfig(cfg)` 在 `Enabled=false` 时返回 `NoOpCache`，否则返回以 `DefaultTTL` 构造的 `SimpleCache`。`CacheConfig` 只包含实际生效的 `Enabled` 和 `DefaultTTL`；原 `Type`、`MaxSize`、`CleanupInterval` 字段已删除，严格配置解码会拒绝对应旧键。

`InMemoryCache`、`LRUCache`、`MultiTierCache` 及其构造器已经删除，不保留兼容别名：

| 旧 | 新 |
|---|---|
| `cache.NewInMemoryCache(maxSize, ttl, cleanup)` | `cache.NewSimpleCache(ttl)`；无后台生命周期入口时用 `cache.WithoutBackgroundCleanup()` |
| `cache.NewLRUCache(maxSize, ttl, cleanup)` | 仅需 TTL 时改用 `cache.NewSimpleCache(ttl)`；需要容量和 LRU 语义时注入明确实现这些契约的 `cache.Cache` |
| `cache.NewMultiTierCache(tiers...)` | 仅需单层 TTL 时改用 `cache.NewSimpleCache(ttl)`；需要多层语义时显式组合并实现 `cache.Cache` |

`SimpleCache` 不提供容量限制、LRU 或多级回填语义，不能把上述构造器机械替换后仍宣称保留这些策略。`NoOpCache.Get` 返回中央 `errors.ErrCacheDisabled`，`Set` 不存储。其它接口清理见[兼容入口迁移指南](COMPATIBILITY_CLEANUP.md)。

## 工具结果缓存

```go
cachedTool := tools.WithMiddleware(originalTool, middleware.Caching(
    middleware.WithCache(cache.NewSimpleCache(10*time.Minute)),
    middleware.WithTTL(10*time.Minute),
))
```

- 只缓存成功结果；默认键只含工具名与参数的完整 SHA256，多租户或按用户返回不同结果的工具必须用 `WithCacheKeyFunc` 把身份放进键，或不缓存。
- 有副作用的工具不要缓存：命中时工具不执行。
- 持久运行已由 Dispatcher 调用日志保证恢复时回放、不重复执行；工具缓存解决的是跨运行的重复计算，二者无关。包装后的工具不转发 `DispatchTimeoutProvider` 等可选接口，注意事项见[工具中间件](TOOL_MIDDLEWARE.md#toolswithmiddleware)。

## 语义缓存（llm/cache）

```go
client, err := llmcache.NewCachedLLMClient(&llmcache.CachedLLMClientConfig{
    Client:            baseClient,
    EmbeddingProvider: embedder, // 只需 Embed；任意 llm.EmbeddingClient，如 providers.OpenAIProvider
    CacheConfig:       llmcache.DefaultSemanticCacheConfig(),
})
if err != nil { return err }
defer client.Close()
resp, err := llm.Complete(ctx, client, req)
```

`llmcache` 指 `github.com/kart-io/goagent/llm/cache`。把消息按 `role: content` 拼接后求向量，相似度达到 `SimilarityThreshold`（默认 0.95）且模型相同时（`ModelSpecific`，默认开启）返回缓存；默认最多 10000 条、TTL 24 小时。`WithSemanticCache(embedder, cfg)` 返回 `func(llm.Client) llm.Client`，创建失败时原样返回原客户端。

限制：

- 包装后的客户端只实现 `llm.Client`（`CompleteStream`）：未命中时转发上游流，只缓存成功终止的答案。它不实现 `ToolStreamClient`，因此不能用于配置了工具的 Builder 或原生工具循环。
- 键不含工具、温度等请求参数；命中只返回文本（`FinishReason="cache_hit"`、`TokensUsed=0`），不含工具调用与用量。
- 缓存在进程内存中，`Stats()` 返回命中统计，`ClearCache` 清空。

## 选用建议

- 缓存键必须覆盖影响结果的全部维度：用户/租户、会话、知识或数据版本、工具版本。
- 检索增强回答优先每次检索新证据；需要答案缓存时，键中带上知识版本。
- TTL 按数据变化频率设置；`SimpleCache` 不限容量，条目数不可控时改用 `tools.ShardedToolCache`（容量可配，固定哈希分片和 LRU 淘汰）或外部缓存实现 `cache.Cache`。

## 设计审核修复后的配置迁移

`ShardedToolCache` 固定使用哈希分片和 LRU。已移除没有执行实现的淘汰策略、负载均衡、压缩、字节内存上限、分片并发数、指标开关和自动调优字段及相应 `With*` 选项，不保留空壳兼容入口。容量表示条目数，不表示进程内存字节数；需要字节约束或其他淘汰算法时由宿主提供对应缓存实现。性能/工作负载预设仅调整实际生效的分片与清理参数。

`SimpleCache` 默认仍需要 `Close()`；`NewSimpleCache(ttl, cache.WithoutBackgroundCleanup())` 不创建后台清理协程，在访问/写入时回收到期项。工具函数式缓存默认使用这一模式，注入缓存的生命周期归宿主管理，构造中间件不会额外创建或关闭宿主缓存。

工具缓存对业务输出做保留 Go 类型的隔离快照，保留 `AdditionalContext`、`Labels` 与嵌套结果。不支持安全快照的资源对象仍正常返回，但不会进入缓存。默认完整摘要改变缓存键，旧短摘要条目等待原 TTL 到期或由宿主清空；不会回退读取旧键。

分片数上限为 `constants.MaxCacheShards`（65536）；超限配置在构造时以 `CodeInvalidConfig` panic 拒绝，不执行整数进位或分片分配。此限制也用于 Agent 缓存中间件。
