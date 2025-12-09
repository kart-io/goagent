# Cache Presets 缓存预设示例

本示例演示如何使用 `ShardedToolCache` 的工厂方法快速创建针对不同场景优化的缓存实例。

## 目录

- [简介](#简介)
- [预设类型](#预设类型)
- [使用方法](#使用方法)
- [代码结构](#代码结构)

## 简介

`ShardedToolCache` 是一个高性能、分片的工具缓存。为了简化配置，GoAgent 提供了两种常用的预设配置：高性能模式和低内存模式。

## 预设类型

### 1. High Performance Cache (高性能)

适用于高并发、对延迟敏感的场景。

- **特点**:
    - 更多的分片数 (32 shards)
    - 更大的容量 (10000 items)
    - 更激进的清理策略
- **创建方式**: `tools.NewHighPerformanceCache()`

### 2. Low Memory Cache (低内存)

适用于资源受限的环境。

- **特点**:
    - 较少的分片数 (4 shards)
    - 较小的容量 (100 items)
    - 启用压缩 (如果支持)
- **创建方式**: `tools.NewLowMemoryCache()`

## 使用方法

### 运行示例

```bash
cd examples/optimization/02-cache-presets
go run main.go
```

### 预期输出

```text
High Performance Cache created
Stats: {Hits:... Misses:...}
Low Memory Cache created
Stats: {Hits:... Misses:...}
HP Cache Get: value1
```

## 代码结构

```text
02-cache-presets/
├── main.go          # 示例入口
└── README.md        # 本文档
```
