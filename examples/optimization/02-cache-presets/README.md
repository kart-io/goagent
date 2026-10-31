# Cache Presets 缓存预设示例

演示 `tools.ShardedToolCache` 的两个预设工厂，以及一次 `Set`/`Get`。不需要 API Key。

## 预设

配置见 `tools/sharded_cache.go`：

| 工厂 | 分片 | 容量 | 默认 TTL | 清理间隔 | 其他 |
| --- | --- | --- | --- | --- | --- |
| `tools.NewHighPerformanceCache()` | 64 | 100000 | 10m | 30s | 启用 `AutoTuning`、`MetricsEnabled` |
| `tools.NewLowMemoryCache()` | 8 | 1000 | 2m | 1m | `CompressionThreshold: 1024` |

两者都返回 `*ShardedToolCache`，用完需 `Close()`。

## 运行

```bash
go run ./examples/optimization/02-cache-presets
```

输出依次为两个缓存的 `GetStats()`（全新缓存，计数均为 0），以及从高性能缓存读回的值：

```text
High Performance Cache created
Stats: {Hits:... Misses:... Evicts:... Invalidations:...}
Low Memory Cache created
Stats: {Hits:... Misses:... Evicts:... Invalidations:...}
HP Cache Get: value1
```

`CacheStats` 的字段是原子计数器，直接 `%+v` 打印会带出内部结构；需要数值时用 `GetStatsValues()`。
