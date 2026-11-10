# 缓存指南

GoAgent 不默认缓存任何模型答案或工具结果（`ConfigureForRAG` 也不缓存）。需要缓存时按缓存对象选择组件：

| 缓存对象 | 组件 | 说明 |
|---|---|---|
| 通用键值 | `cache.Cache`；推荐 `cache.NewSimpleCache(ttl)`；无后台生命周期入口时用 `cache.WithoutBackgroundCleanup()` | `sync.Map` + TTL，后台清理；`NoOpCache` 为空实现。包说明见 [cache/README.md](../../cache/README.md) |
| 单个工具的结果 | `tools/middleware.Caching`（经 `middleware.Wrap`） | 底层是任意 `cache.Cache`，见[工具中间件](TOOL_MIDDLEWARE.md#内置中间件) |
| Agent 输入 → 响应 | `core/middleware.NewCacheMiddleware(ttl)`（经 `builder.WithMiddleware`） | 命中时跳过处理器；键只取输入的 `%v` 文本，不区分会话、用户或知识版本 |

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
cachedTool := middleware.Wrap(originalTool, middleware.Caching(
    middleware.WithCache(cache.NewSimpleCache(10*time.Minute)),
    middleware.WithTTL(10*time.Minute),
))
```

- 只缓存成功结果；键由工具名与参数的完整 SHA256 组成，并按会话（`tools.WithSessionID`，Builder 以 `SessionID` 设置）与 `ToolInput.CallerID` 隔离。租户、知识版本等其他影响结果的维度仍须用 `WithCacheKeyFunc` 放进键，或不缓存。
- 有副作用的工具不要缓存：命中时工具不执行。
- 持久运行已由 Dispatcher 调用日志保证恢复时回放、不重复执行；工具缓存解决的是跨运行的重复计算，二者无关。包装后的工具不转发 `DispatchTimeoutProvider` 等可选接口，注意事项见[工具中间件](TOOL_MIDDLEWARE.md#middlewarewrap)。

## 选用建议

- 缓存键必须覆盖影响结果的全部维度：用户/租户、会话、知识或数据版本、工具版本。
- 检索增强回答优先每次检索新证据；需要答案缓存时，键中带上知识版本。
- TTL 按数据变化频率设置；`SimpleCache` 默认不限条目数，键由外部输入决定时用 `cache.WithMaxEntries(n)` 设置上限（工具缓存中间件的默认缓存上限为 10000 条），或由外部缓存实现 `cache.Cache`。

## 设计审核修复后的配置迁移

`SimpleCache` 默认仍需要 `Close()`；`NewSimpleCache(ttl, cache.WithoutBackgroundCleanup())` 不创建后台清理协程，在访问/写入时回收到期项。工具函数式缓存默认使用这一模式，注入缓存的生命周期归宿主管理，构造中间件不会额外创建或关闭宿主缓存。

工具缓存对业务输出做保留 Go 类型的隔离快照，保留 `AdditionalContext`、`Labels` 与嵌套结果。不支持安全快照的资源对象仍正常返回，但不会进入缓存。默认完整摘要改变缓存键，旧短摘要条目等待原 TTL 到期或由宿主清空；不会回退读取旧键。

分片数上限为 `constants.MaxCacheShards`（65536）；超限配置在构造时以 `CodeInvalidConfig` panic 拒绝，不执行整数进位或分片分配。此限制也用于 Agent 缓存中间件。
