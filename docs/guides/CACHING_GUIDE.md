# GoAgent 缓存使用指南

本指南介绍 GoAgent 中缓存系统的使用方法、最佳实践和迁移建议。

---

## 目录

- [快速开始](#快速开始)
- [核心概念](#核心概念)
- [SimpleCache 推荐用法](#simplecache-推荐用法)
- [高级用法](#高级用法)
- [性能调优](#性能调优)
- [最佳实践](#最佳实践)
- [Deprecated API 迁移](#deprecated-api-迁移)
- [故障排查](#故障排查)

---

## 快速开始

### 创建缓存实例（推荐）

```go
import (
    "time"
    "github.com/kart-io/goagent/cache"
)

// 使用 SimpleCache（推荐）
cacheInstance := cache.NewSimpleCache(5 * time.Minute)

// 基本操作
ctx := context.Background()
cacheInstance.Set(ctx, "key", "value", 0)                // 0 = 使用默认 TTL
val, err := cacheInstance.Get(ctx, "key")                // 获取值
cacheInstance.Delete(ctx, "key")                         // 删除值
cacheInstance.Clear(ctx)                                 // 清空缓存
```

### 为什么选择 SimpleCache？

SimpleCache 是 GoAgent 推荐的默认缓存实现，具有以下优势：

| 特性 | SimpleCache | 其他实现 |
|------|-------------|----------|
| **代码复杂度** | 🟢 低（~150 行） | 🔴 高（200-250 行） |
| **性能** | 🟢 优秀（基于 sync.Map） | 🟡 较好（基于 mutex） |
| **并发安全** | 🟢 原生支持 | 🟡 手动锁 |
| **内存管理** | 🟢 TTL 自动清理 | 🔴 需要手动驱逐 |
| **学习曲线** | 🟢 极低 | 🔴 中等到高 |
| **适用场景** | 🟢 90% 使用场景 | 🟡 特殊需求 |

---

## 核心概念

### Cache 接口

所有缓存实现都遵循统一的 `Cache` 接口：

```go
type Cache interface {
    Get(ctx context.Context, key string) (interface{}, error)
    Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    Clear(ctx context.Context) error
    GetStats(ctx context.Context) *CacheStats
}
```

### 缓存键设计

推荐使用 `CacheKeyGenerator` 生成标准化缓存键：

```go
keyGen := cache.NewCacheKeyGenerator("llm")

// 简单键（适用于无参数场景）
key := keyGen.GenerateKeySimple("prompt-text")
// 输出: llm:prompt-text

// 参数键（适用于多参数场景，自动序列化）
key := keyGen.GenerateKey(map[string]interface{}{
    "model": "gpt-4",
    "temperature": 0.7,
})
// 输出: llm:<sha256-hash>
```

**键命名约定**：
- 使用 `namespace:identifier` 格式（例如 `llm:request-123`）
- 避免包含敏感信息（如 API 密钥）
- 控制键长度（建议 < 256 字符）

---

## SimpleCache 推荐用法

### 基本使用

```go
// 创建缓存（5 分钟 TTL）
cache := cache.NewSimpleCache(5 * time.Minute)

// 写入缓存（使用默认 TTL）
err := cache.Set(ctx, "user:123", userData, 0)

// 写入缓存（自定义 TTL）
err := cache.Set(ctx, "temp:token", token, 1*time.Minute)

// 读取缓存
value, err := cache.Get(ctx, "user:123")
if err != nil {
    if err == cache.ErrCacheMiss {
        // 缓存未命中，从数据源加载
        value = loadFromDatabase()
        cache.Set(ctx, "user:123", value, 0)
    } else {
        // 其他错误
        log.Error("Cache error", err)
    }
}
```

### 与工具中间件集成

```go
import (
    "github.com/kart-io/goagent/cache"
    "github.com/kart-io/goagent/tools/middleware"
)

// 创建缓存实例
cacheInstance := cache.NewSimpleCache(10 * time.Minute)

// 配置缓存中间件
cachingMW := middleware.Caching(
    middleware.WithCache(cacheInstance),
    middleware.WithTTL(10 * time.Minute),
)

// 应用到工具
tool := someExpensiveTool()
cachedTool := cachingMW.Apply(tool)

// 第一次调用 - 缓存未命中
result1, _ := cachedTool.Execute(ctx, args)
// output.Metadata["cache_hit"] == false

// 第二次调用（相同参数）- 缓存命中
result2, _ := cachedTool.Execute(ctx, args)
// output.Metadata["cache_hit"] == true
// 不会调用实际工具！
```

### 统计信息监控

```go
// 获取缓存统计
stats := cache.GetStats(ctx)

fmt.Printf("命中率: %.2f%%\n",
    float64(stats.Hits) / float64(stats.Hits + stats.Misses) * 100)
fmt.Printf("总请求: %d (命中: %d, 未命中: %d)\n",
    stats.Hits + stats.Misses, stats.Hits, stats.Misses)

// 典型输出：
// 命中率: 85.50%
// 总请求: 1000 (命中: 855, 未命中: 145)
```

---

## 高级用法

### 自定义缓存键生成

```go
// 方法 1：使用 WithCacheKeyFunc 自定义
customKeyFunc := func(toolName string, args map[string]interface{}) string {
    // 只根据 "query" 参数生成键（忽略其他参数）
    query := args["query"].(string)
    return fmt.Sprintf("search:%s", query)
}

cachingMW := middleware.Caching(
    middleware.WithCache(cacheInstance),
    middleware.WithCacheKeyFunc(customKeyFunc),
)

// 方法 2：使用 CacheKeyGenerator 的高级功能
keyGen := cache.NewCacheKeyGenerator("api")

// 仅基于特定字段生成键
key := keyGen.GenerateKey(map[string]interface{}{
    "endpoint": "/users",
    "query": map[string]string{"page": "1"},
    // "auth_token" 不会影响缓存键
})
```

### 批量操作

```go
// 批量写入
keys := []string{"key1", "key2", "key3"}
values := []interface{}{"val1", "val2", "val3"}

for i, key := range keys {
    cache.Set(ctx, key, values[i], 0)
}

// 批量读取
results := make([]interface{}, len(keys))
for i, key := range keys {
    val, _ := cache.Get(ctx, key)
    results[i] = val
}
```

### 缓存预热

```go
// 应用启动时预加载热数据
func preloadCache(cache cache.Cache) error {
    hotKeys := []string{"config", "feature-flags", "rate-limits"}

    for _, key := range hotKeys {
        data, err := loadFromDatabase(key)
        if err != nil {
            return err
        }

        // 预热缓存，使用较长的 TTL
        if err := cache.Set(context.Background(), key, data, 1*time.Hour); err != nil {
            return err
        }
    }

    log.Info("Cache preloaded successfully")
    return nil
}
```

---

## 性能调优

### TTL 选择指南

| 数据类型 | 推荐 TTL | 理由 |
|---------|----------|------|
| **用户会话** | 15-30 分钟 | 平衡安全性和性能 |
| **API 响应** | 1-5 分钟 | 实时性要求较高 |
| **配置数据** | 30-60 分钟 | 变化频率低 |
| **静态资源** | 24 小时 | 几乎不变 |
| **LLM 响应** | 5-15 分钟 | 成本高，适合缓存 |
| **数据库查询** | 30 秒-5 分钟 | 根据数据新鲜度要求 |

### 性能基准测试

SimpleCache 在并发场景下的性能表现：

```
BenchmarkSimpleCacheGet-8         	10000000	       120 ns/op	       0 B/op	       0 allocs/op
BenchmarkSimpleCacheSet-8         	 5000000	       280 ns/op	      64 B/op	       2 allocs/op
BenchmarkSimpleCacheConcurrent-8  	 3000000	       450 ns/op	      96 B/op	       3 allocs/op
```

**性能优化建议**：
1. **避免存储大对象**：对象 > 1MB 时考虑存储引用而非对象本身
2. **控制缓存大小**：SimpleCache 基于 TTL 自动清理，但仍需合理设置 TTL
3. **使用合适的 TTL**：过短导致频繁加载，过长导致内存浪费

### 监控和告警

```go
// 定期检查缓存健康状态
func monitorCache(cache cache.Cache) {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        stats := cache.GetStats(context.Background())
        hitRate := float64(stats.Hits) / float64(stats.Hits + stats.Misses) * 100

        // 告警：命中率过低
        if hitRate < 50.0 {
            log.Warn("Low cache hit rate",
                "rate", hitRate,
                "hits", stats.Hits,
                "misses", stats.Misses)
        }

        // 指标上报（Prometheus/OpenTelemetry）
        metrics.CacheHitRate.Set(hitRate)
        metrics.CacheTotalHits.Add(float64(stats.Hits))
        metrics.CacheTotalMisses.Add(float64(stats.Misses))
    }
}
```

---

## 最佳实践

### ✅ 推荐做法

```go
// ✅ 1. 使用 SimpleCache 作为默认选择
cache := cache.NewSimpleCache(5 * time.Minute)

// ✅ 2. 合理设置 TTL（根据数据新鲜度要求）
cache.Set(ctx, "hot-data", value, 1*time.Minute)   // 频繁变化
cache.Set(ctx, "cold-data", value, 1*time.Hour)    // 稳定数据

// ✅ 3. 处理缓存未命中（Cache-Aside 模式）
value, err := cache.Get(ctx, key)
if err == cache.ErrCacheMiss {
    value = loadFromDatabase(key)
    cache.Set(ctx, key, value, 0)
}

// ✅ 4. 使用标准化键名
keyGen := cache.NewCacheKeyGenerator("namespace")
key := keyGen.GenerateKeySimple("identifier")

// ✅ 5. 监控缓存命中率
stats := cache.GetStats(ctx)
hitRate := float64(stats.Hits) / float64(stats.Hits + stats.Misses) * 100
```

### ❌ 避免做法

```go
// ❌ 1. 缓存敏感信息
cache.Set(ctx, "user:password", password, 0)  // 错误！

// ❌ 2. 过长或过短的 TTL
cache.Set(ctx, "data", value, 7*24*time.Hour)  // 过长，可能导致陈旧数据
cache.Set(ctx, "data", value, 100*time.Millisecond)  // 过短，失去缓存意义

// ❌ 3. 忽略错误处理
cache.Set(ctx, key, value, 0)  // 缺少错误检查
value, _ := cache.Get(ctx, key)  // 忽略错误

// ❌ 4. 缓存不可序列化的对象
cache.Set(ctx, "conn", databaseConnection, 0)  // 错误！

// ❌ 5. 在循环中逐个操作（应使用批量操作）
for _, item := range items {
    cache.Set(ctx, item.Key, item.Value, 0)  // 效率低
}
```

### 缓存模式选择

#### Cache-Aside（旁路缓存）- 推荐

```go
// 读取流程：先查缓存，未命中则查数据库并回填
func GetUser(ctx context.Context, userID string) (*User, error) {
    // 1. 尝试从缓存获取
    cached, err := cache.Get(ctx, "user:"+userID)
    if err == nil {
        return cached.(*User), nil
    }

    // 2. 缓存未命中，从数据库加载
    user, err := db.GetUser(userID)
    if err != nil {
        return nil, err
    }

    // 3. 回填缓存
    cache.Set(ctx, "user:"+userID, user, 10*time.Minute)

    return user, nil
}
```

#### Write-Through（写穿透）

```go
// 写入流程：同时写入缓存和数据库
func UpdateUser(ctx context.Context, user *User) error {
    // 1. 写入数据库
    if err := db.UpdateUser(user); err != nil {
        return err
    }

    // 2. 更新缓存
    cache.Set(ctx, "user:"+user.ID, user, 10*time.Minute)

    return nil
}
```

#### Write-Behind（写回）

```go
// 写入流程：先写缓存，异步写入数据库（适用于高写入场景）
func UpdateUserAsync(ctx context.Context, user *User) error {
    // 1. 立即更新缓存
    cache.Set(ctx, "user:"+user.ID, user, 10*time.Minute)

    // 2. 异步写入数据库
    go func() {
        if err := db.UpdateUser(user); err != nil {
            log.Error("Failed to persist user", "error", err)
            // 实现重试逻辑
        }
    }()

    return nil
}
```

---

## Deprecated API 迁移

### 迁移时间表

- **当前版本（v1.x）**：Deprecated 函数仍然可用，但不推荐使用
- **v2.0.0（计划中）**：完全移除 Deprecated 函数

### 从 InMemoryCache 迁移

#### 迁移前（Deprecated）

```go
// ❌ Deprecated: 参数复杂，维护成本高
cache := cache.NewInMemoryCache(
    100,           // maxSize
    5*time.Minute, // defaultTTL
    1*time.Minute, // cleanupInterval
)
```

#### 迁移后（推荐）

```go
// ✅ 推荐: 更简洁，性能更好
cache := cache.NewSimpleCache(5 * time.Minute)
```

**迁移差异说明**：

| 参数 | InMemoryCache | SimpleCache | 说明 |
|------|---------------|-------------|------|
| `maxSize` | 必须指定 | ❌ 移除 | SimpleCache 通过 TTL 自动管理 |
| `defaultTTL` | 第 2 个参数 | 唯一参数 | 保持一致 |
| `cleanupInterval` | 必须指定 | ❌ 移除 | 自动优化清理策略 |

### 从 LRUCache 迁移

#### 迁移前（Deprecated）

```go
// ❌ Deprecated: LRU 驱逐策略在实际场景中使用率极低
cache := cache.NewLRUCache(100, 5*time.Minute, 1*time.Minute)
```

#### 迁移后（推荐）

```go
// ✅ 推荐: TTL 驱逐策略更简单有效
cache := cache.NewSimpleCache(5 * time.Minute)
```

**为什么不需要 LRU？**

在实际应用中，LRU（最近最少使用）驱逐策略的使用场景非常有限：
- ✅ **TTL 驱逐更可预测**：数据过期时间明确
- ✅ **实现更简单**：无需维护访问顺序链表
- ✅ **性能更好**：避免 LRU 链表的锁竞争
- ❌ **LRU 仅适用于**：内存严格受限且热点数据明确的场景（极少）

### 从 MultiTierCache 迁移

#### 迁移前（Deprecated）

```go
// ❌ Deprecated: 多级缓存在单进程应用中过于复杂
tier1 := cache.NewInMemoryCache(10, 5*time.Minute, 0)
tier2 := cache.NewInMemoryCache(100, 5*time.Minute, 0)
multiCache := cache.NewMultiTierCache(tier1, tier2)
```

#### 迁移后（推荐）

```go
// ✅ 推荐: 单级缓存满足 90% 场景
cache := cache.NewSimpleCache(5 * time.Minute)

// 如果确实需要多级缓存（例如分布式场景），
// 建议使用外部解决方案（Redis + 本地缓存）
```

**何时需要多级缓存？**

多级缓存仅在以下场景有意义：
- ✅ **分布式系统**：L1 = 本地内存，L2 = Redis/Memcached
- ✅ **不同 TTL 需求**：热数据短 TTL，冷数据长 TTL

对于单进程应用，SimpleCache 已足够。

### 自动化迁移脚本

我们提供了自动化迁移脚本：

```bash
# 扫描并报告 deprecated API 使用
go run tools/migrate-cache.go scan ./...

# 自动替换（推荐先运行 scan 确认）
go run tools/migrate-cache.go replace ./...

# 输出示例：
# Found 3 usages of deprecated cache APIs:
#   - examples/demo.go:42: NewInMemoryCache → NewSimpleCache
#   - pkg/agent/builder.go:78: NewLRUCache → NewSimpleCache
# Run 'replace' command to auto-fix
```

---

## 故障排查

### 问题 1：缓存命中率低

**症状**：`GetStats()` 显示命中率 < 30%

**可能原因**：
1. TTL 过短，数据频繁过期
2. 缓存键不一致（每次生成不同的键）
3. 数据变化频率高，缓存失效快

**解决方案**：
```go
// 1. 检查 TTL 设置
stats := cache.GetStats(ctx)
fmt.Printf("Hits: %d, Misses: %d, Hit Rate: %.2f%%\n",
    stats.Hits, stats.Misses,
    float64(stats.Hits) / float64(stats.Hits + stats.Misses) * 100)

// 2. 验证缓存键一致性
keyGen := cache.NewCacheKeyGenerator("test")
key1 := keyGen.GenerateKey(args)
key2 := keyGen.GenerateKey(args)
assert.Equal(t, key1, key2)  // 必须相同

// 3. 调整 TTL
cache := cache.NewSimpleCache(10 * time.Minute)  // 增加 TTL
```

### 问题 2：内存占用过高

**症状**：应用内存持续增长

**可能原因**：
1. 缓存了大对象（> 1MB）
2. TTL 过长，数据无法及时清理
3. 缓存键空间爆炸（生成了大量不同的键）

**解决方案**：
```go
// 1. 限制缓存对象大小
const maxCacheSize = 1 * 1024 * 1024  // 1MB
if len(serializedData) > maxCacheSize {
    return errors.New("object too large to cache")
}

// 2. 缩短 TTL
cache := cache.NewSimpleCache(2 * time.Minute)  // 减少 TTL

// 3. 分析缓存键分布
stats := cache.GetStats(ctx)
// 使用 pprof 或自定义监控检查键数量
```

### 问题 3：缓存雪崩

**症状**：大量缓存同时过期，导致数据库负载激增

**解决方案**：
```go
// 添加随机抖动到 TTL
func setWithJitter(cache cache.Cache, key string, value interface{}, baseTTL time.Duration) error {
    jitter := time.Duration(rand.Int63n(int64(baseTTL / 10)))  // ±10% 抖动
    ttl := baseTTL + jitter
    return cache.Set(context.Background(), key, value, ttl)
}

// 使用示例
baseTTL := 5 * time.Minute
setWithJitter(cache, "key1", "value1", baseTTL)  // TTL: 5m00s + jitter
setWithJitter(cache, "key2", "value2", baseTTL)  // TTL: 5m15s + jitter
```

### 问题 4：并发竞态条件

**症状**：相同请求触发多次数据库查询（缓存击穿）

**解决方案**：
```go
import "golang.org/x/sync/singleflight"

var sf singleflight.Group

func GetUserSafe(ctx context.Context, userID string) (*User, error) {
    key := "user:" + userID

    // singleflight 确保同一时刻只有一个请求去加载数据
    val, err, _ := sf.Do(key, func() (interface{}, error) {
        // 1. 尝试从缓存获取
        cached, err := cache.Get(ctx, key)
        if err == nil {
            return cached.(*User), nil
        }

        // 2. 从数据库加载
        user, err := db.GetUser(userID)
        if err != nil {
            return nil, err
        }

        // 3. 回填缓存
        cache.Set(ctx, key, user, 10*time.Minute)

        return user, nil
    })

    if err != nil {
        return nil, err
    }
    return val.(*User), nil
}
```

---

## 相关资源

- [工具中间件文档](./TOOL_MIDDLEWARE.md) - 缓存中间件的详细配置
- [架构设计文档](../architecture/CORE_ARCHITECTURE.md) - 缓存在 GoAgent 架构中的位置
- [性能优化指南](../performance/OPTIMIZATION.md) - 缓存性能调优
- [API 文档](../../cache/) - Cache 接口完整定义

---

**最后更新时间**: 2025-12-04
**适用版本**: GoAgent v1.x
**维护者**: GoAgent Team
