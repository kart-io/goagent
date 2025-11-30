# 缓存系统简化 - 操作日志

**任务**: 简化项目中过度工程化的缓存设计
**时间**: 2025-11-30
**执行者**: Claude Code (Golang Pro)

---

## 编码前检查 - 缓存简化

时间: 2025-11-30

□ 已完成上下文收集 (7步检索清单) ✅
□ 已生成上下文摘要文件 ⏳
□ 分析了至少3个相似实现 ✅
  - cache/base.go:79-310 (InMemoryCache)
  - tools/tool_cache.go:47-565 (MemoryToolCache)
  - tools/sharded_cache.go:15-864 (ShardedToolCache)
□ 确认项目约定 ✅
  - 命名: 驼峰命名,接口不带I前缀
  - 风格: goimports格式化,中文注释
  - 生命周期: Close()方法 + context
□ 确认不重复造轮子 ✅
  - 使用标准库sync.Map而非自建
□ 理解集成点 ✅
  - 影响范围: tools/, agents/, llm/, performance/

## 上下文收集摘要

### 当前缓存架构复杂度分析

**文件统计**:
- cache/base.go: 575行 (4种实现)
- tools/tool_cache.go: 797行 (LRU + 版本号)
- tools/sharded_cache.go: 864行 (分片 + 自动调优)
- performance/cache_pool.go: 405行 (Agent缓存)
- llm/cache/: 语义缓存 (另外的实现)

**抽象层次统计** (过度设计证据):
1. cache/base.go
   - Cache接口 (7个方法)
   - InMemoryCache (sync.RWMutex + map)
   - LRUCache (继承InMemoryCache)
   - MultiTierCache (多层回填)
   - NoOpCache (禁用)
   - CacheKeyGenerator (键生成)

2. tools/tool_cache.go
   - ToolCache接口 (6个方法)
   - MemoryToolCache (container/list LRU)
   - CachedTool包装器
   - 版本号失效机制
   - 依赖关系管理
   - 正则模式失效

3. tools/sharded_cache.go
   - ShardedToolCache (32分片)
   - 自定义双向链表LRU
   - FNV-1a哈希算法
   - 自动调优 (autoTune)
   - 自适应清理 (adaptiveCleanup)
   - 负载均衡策略
   - 工作负载类型
   - 压缩阈值
   - 内存限制

**锁竞争分析**:
- sync.RWMutex: 6处 (cache/base.go, tools/tool_cache.go, tools/sharded_cache.go)
- 每次Get/Set都需要加锁
- 分片缓存试图缓解锁竞争,但增加了复杂度

**实际使用场景**:
- 单进程应用
- 不需要分布式缓存
- 不需要多层缓存
- 不需要LRU (大部分缓存未满)
- 简单的Get/Set/Delete/TTL即可

**简化方案**:
使用sync.Map + TTL的轻量级实现,删除所有过度抽象

---

## 决策记录

### 决策1: 使用sync.Map替代所有自定义缓存实现
**时间**: 2025-11-30
**理由**:
- sync.Map专为读多写少场景优化 (符合缓存特性)
- 零锁竞争 (使用atomic + 读写分离)
- 标准库,无需维护
- 删除2236行自定义实现

**对比分析**:
| 特性 | 当前实现 | sync.Map方案 |
|------|---------|-------------|
| 代码行数 | 2236行 | ~150行 |
| 锁机制 | RWMutex | atomic (无锁) |
| LRU | 3套实现 | 删除 (实际不需要) |
| 分片 | 32分片 + 哈希 | sync.Map内部优化 |
| 自动调优 | 860行代码 | 删除 (过度设计) |
| 依赖管理 | 级联失效 | 简化为简单失效 |

### 决策2: 统一缓存接口
**时间**: 2025-11-30
**当前问题**: 3套接口(Cache/ToolCache/SemanticCache)
**解决方案**: 统一为单一SimpleCache
**理由**:
- 避免接口膨胀
- 减少抽象层次
- 符合YAGNI原则

### 决策3: 保留TTL和基础统计,删除高级特性
**保留**:
- Get/Set/Delete/Clear
- TTL过期
- 命中/未命中统计

**删除**:
- LRU驱逐 (实际容量未满)
- 多层缓存回填
- 版本号失效
- 依赖级联失效
- 正则模式失效
- 自动调优
- 负载均衡
- 压缩阈值
- 内存限制

---

## 下一步: 实施简化
⏳ 创建cache/simple_cache.go
⏳ 更新所有使用处
⏳ 删除旧实现
⏳ 运行测试验证
