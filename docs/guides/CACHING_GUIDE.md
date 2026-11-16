# 缓存指南

GoAgent 不默认缓存任何模型答案或工具结果（`ConfigureForRAG` 也不缓存）。需要缓存时按缓存对象选择组件：

| 缓存对象 | 组件 | 说明 |
|---|---|---|
| 通用键值 | `cache.Cache`；推荐 `cache.NewSimpleCache(ttl)`；无后台生命周期入口时用 `cache.WithoutBackgroundCleanup()` | `sync.Map` + TTL，后台清理；`NoOpCache` 为空实现。包说明见 [cache/README.md](../../cache/README.md) |
| 单个工具的结果 | `tools/middleware.Caching`（经 `middleware.Wrap`） | 底层是任意 `cache.Cache`，见[工具中间件](TOOL_MIDDLEWARE.md#内置中间件) |
| 单次模型调用 → 回答 | `builder.WithModelCache(builder.ModelCache{...})` | 以完整请求为键，按宿主声明的隔离身份隔离；命中只省掉一次模型调用，会话日志照常写入。见[模型调用缓存](#模型调用缓存) |

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

## 模型调用缓存

```go
mc := builder.ModelCache{
    Cache:  c,                // cache.Cache；物理删除需要实现 cache.Tagged（SimpleCache 已实现）
    TTL:    5 * time.Minute,  // 必须大于 0
    Scope:  func(ctx context.Context) (string, bool) { return tenantUser(ctx) },
    Erase:  builder.EraseAuto, // EraseOff / EraseAuto（默认）/ EraseRequired
    TagKey: tagKey,           // 标签摘要的 HMAC 密钥；使用标签时至少 32 字节
}
agent, err := builder.NewSimpleBuilder(client).WithModelCache(mc).Build()
```

`Scope` 返回可信的隔离身份（租户、用户），返回 `false` 或空串时本次不查也不写。会话 ID 不能当隔离依据：它由调用方任意设置，缺省时共享同一个 Agent 的所有用户取到同一个值。配置了长期记忆时 `Scope` 可以省略，默认取 `memory.WithNamespace` 绑定的命名空间；否则必填。

键是下列内容的 SHA-256：隔离身份、会话 ID、客户端身份与解析后的模型名、完整请求（含召回块、采样参数）、规范化后的工具定义、长期记忆的版本。任何一项变化都不命中。客户端可以实现 `CacheIdentity() string` 声明自己的身份（base URL、部署名等）。

### 适用场景

有会话日志时，同一会话的下一轮请求包含上一轮的问答，不会命中。命中主要发生在：

- 没有会话日志的无状态 Agent：同一身份下重复的相同问题，例如固定提示的分类、抽取与路由；
- 同一身份下以相同历史重复提交，例如用户重新生成、前端重试；
- 工具循环中两次完全相同的中间请求。

持久运行的崩溃恢复由持久日志回放覆盖，不依赖缓存。不要对有状态对话期待高命中率。以下回答不缓存：出错、截断、空回答、非法工具调用、带 `ProviderState` 的回答（开启思考签名的推理模型因此不走缓存）。两个相同请求并发到达时各自调用一次模型，不合并。

### 命中时的行为

命中不消耗 token，不占预算、不记账，turn 的 `Usage` 为零值。调用方判断是否命中：

- observe 事件的 `Cached` 标记；
- 输出元数据 `model_cache_hits`。`model_calls` 只计真实调用，`usage_complete` 不因命中变为 false。

事件流先发 `step: model_cached`，`Content` 非空时再发一整段 `text`，不重放多段增量。

### 信任边界

- 缓存后端与会话存储同等可信。能写入缓存的人可以伪造回答，包括伪造工具调用让 Agent 执行；命中后的工具调用仍要通过与写入前相同的校验（工具必须存在，参数合法），但这挡不住合法而恶意的调用。外部缓存必须有访问控制，不能与不可信服务共享。
- 缓存值是明文：模型回答、工具调用参数（可能含个人信息）、推理文本。加密与网络隔离由宿主负责。
- 标签是 HMAC 摘要，不含明文用户 ID 或会话 ID，不会经缓存键名、监控或慢查询日志泄露。

### 删除承诺

`Forget`、`Rewound`、`Purge` 之后，相同请求不会命中（键含记忆版本）。物理删除由 `Erase` 决定：

| 模式 | 行为 | 适用 |
|---|---|---|
| `EraseOff` | 只做逻辑删除，旧条目留到 TTL 到期 | 进程内缓存、TTL 很短，或没有合规要求 |
| `EraseAuto`（默认） | 缓存实现 `cache.Tagged` 且记忆配置了 `OnErase` 时物理删除，否则退回 TTL。缓存不支持 `Tagged` 时构建会记录警告；记忆没有配置 `OnErase` 时同样退回 TTL，但不会有警告，需要自行装配（用 `mc.Eraser()`）；TTL 超过 `EraseWithin` 时也会警告 | 大多数场景 |
| `EraseRequired` | 条件不满足时 `Build` 失败 | 有删除个人数据合规要求的生产环境 |

- `EraseRequired` 要求 TTL 不超过 `memory.Config.EraseWithin`，这个约束只在配置了长期记忆时生效。不配长期记忆时，删除会话后缓存残留时长的上限是 TTL。
- 物理删除失败不影响逻辑正确性，只推迟清除。`EraseRequired` 下推迟的上限是 TTL。
- 遗忘与回退会清掉该命名空间当前作用域下的全部缓存条目，粒度较粗。

### 装配 OnErase

长期记忆一侧只接通用回调，`memory` 不知道缓存：

```go
m, err := memory.New(memory.Config{
    // ...
    OnErase: mc.Eraser(), // memory.EraseFunc；EraseOff 或缓存不支持 Tagged 时为 nil
})
```

- 配置了 `OnErase` 时，`memory.New` 要求 `RetireGrace >= EraseWithin`，否则返回配置错误。
- 独立的 Worker 进程不经过 Builder，装配 `OnErase` 时必须使用与 Builder 相同的缓存后端，以及相同且至少 32 字节的 `TagKey`。否则标签对不上，删除会静默落空。`EraseRequired` 时 Builder 只能校验本进程的记忆实例。
- `OnErase` 持续失败时，每轮维护都会通过 `RunMaintenance` 的 `onError` 报错，并累计到 `Memory.EraseErrors()`（提交之后立即擦除失败的次数）。宿主应当监控它。
- 多进程部署时，擦除重做的截止时间（`Until`）依赖各进程的时钟，时钟偏差会改变实际的删除时限。

### 删除或回退会话

没有长期记忆的会话回退不会触发 `Memory.Rewound`，缓存靠键里的完整历史保证不命中，物理残留须宿主处理：在 `session.Rewind` 或删除会话之后调用 `mc.DeleteSession(ctx, sessionID)`。

`DeleteSession` 在 `EraseOff` 或后端不支持 `Tagged` 时返回 nil，但并没有物理删除，条目留到 TTL 到期。

### 升级

`LastRewindSeq` 是只增字段，存储格式版本不变。混合版本部署时，旧程序重写 head 会把它清零，回退之后的条目重新可达；物理删除仍然生效。升级期间要避免新旧程序同时服务带回退操作的命名空间。

## 选用建议

- 缓存键必须覆盖影响结果的全部维度：用户/租户、会话、知识或数据版本、工具版本。
- 检索增强回答优先每次检索新证据；需要答案缓存时，键中带上知识版本。
- TTL 按数据变化频率设置；`SimpleCache` 默认不限条目数，键由外部输入决定时用 `cache.WithMaxEntries(n)` 设置上限（工具缓存中间件的默认缓存上限为 10000 条），或由外部缓存实现 `cache.Cache`。

## 设计审核修复后的配置迁移

`SimpleCache` 默认仍需要 `Close()`；`NewSimpleCache(ttl, cache.WithoutBackgroundCleanup())` 不创建后台清理协程，在访问/写入时回收到期项。工具函数式缓存默认使用这一模式，注入缓存的生命周期归宿主管理，构造中间件不会额外创建或关闭宿主缓存。

工具缓存对业务输出做保留 Go 类型的隔离快照，保留 `AdditionalContext`、`Labels` 与嵌套结果。不支持安全快照的资源对象仍正常返回，但不会进入缓存。默认完整摘要改变缓存键，旧短摘要条目等待原 TTL 到期或由宿主清空；不会回退读取旧键。
