## 项目上下文摘要（缓存系统简化）
生成时间：2025-11-30 22:40:00

### 1. 相似实现分析

**实现1**: tools/simple_tool_cache.go:24-183
- 模式：基于 cache.SimpleCache (sync.Map + TTL) 的简化实现
- 可复用：NewSimpleToolCache、CachedTool 包装器
- 需注意：依赖 cache.SimpleCache，无 LRU/分片/版本号/依赖管理

**实现2**: tools/tool_cache.go:40-594
- 模式：基于 container/list 的 LRU 缓存实现
- 可复用：MemoryToolCache、版本号失效、依赖级联失效
- 需注意：包含复杂的依赖管理、正则失效、版本号机制

**实现3**: tools/sharded_cache.go:15-100
- 模式：32分片 + FNV-1a哈希 + 自动调优
- 可复用：ShardedToolCache（过度设计）
- 需注意：包含大量过度设计特性（自动调优、压缩、负载均衡等）

### 2. 项目约定
- **命名约定**: ToolCache 接口、MemoryToolCache/ShardedToolCache/SimpleToolCache 实现
- **文件组织**: tools/*.go 工具相关、cache/*.go 通用缓存
- **导入顺序**: 标准库 → 第三方库 → 本项目包
- **代码风格**: goimports格式化、注释使用简体中文

### 3. 可复用组件清单
- `cache/simple_cache.go`: 基础 sync.Map + TTL 缓存
- `tools/simple_tool_cache.go`: 简化工具缓存（保留）
- `interfaces/tool.go`: Tool、ToolInput、ToolOutput 接口定义

### 4. 测试策略
- **测试框架**: Go testing
- **测试模式**: 单元测试 + 压测 + 循环依赖测试
- **参考文件**:
  - tools/cache_test.go: 基础缓存测试
  - tools/sharded_cache_test.go: 分片缓存测试（待删除）
  - tools/sharded_cache_bench_test.go: 性能测试（待删除）
- **覆盖要求**: Get/Set/Delete/Clear + TTL过期 + 并发安全

### 5. 依赖和集成点
- **外部依赖**: 无
- **内部依赖**:
  - cache.SimpleCache: 底层通用缓存
  - interfaces.ToolOutput: 工具输出类型
  - errors: 错误包装
- **集成方式**: 工具包装器（CachedTool）
- **配置来源**: 代码配置（ttl参数）

### 6. 技术选型理由
- **为什么用这个方案**:
  - simple_tool_cache.go: 满足基本需求，简单可维护
  - sharded_cache.go: 过度设计，实际无需32分片和自动调优
- **优势**: SimpleToolCache 足够简单，基于 sync.Map 性能已经很好
- **劣势和风险**: MemoryToolCache 包含复杂的依赖管理和版本号机制

### 7. 关键风险点
- **并发问题**: sync.Map 已处理，无需额外分片
- **边界条件**: TTL过期、缓存驱逐
- **性能瓶颈**: 对于工具调用场景，SimpleCache 性能足够
- **安全考虑**: 无

### 8. 删除文件清单
**分片缓存相关（过度设计）**:
- tools/sharded_cache.go (20KB)
- tools/sharded_cache_options.go (12KB)
- tools/sharded_cache_examples.go (7.2KB)
- tools/sharded_cache_test.go (8.9KB)
- tools/sharded_cache_bench_test.go (5.4KB)
- tools/sharded_cache_options_test.go (11KB)
- tools/sharded_cache_stress_test.go (6.8KB)
- tools/sharded_cache_circular_test.go (6.9KB)
- tools/memory_cache_circular_test.go (7.6KB)

**保留文件**:
- tools/simple_tool_cache.go (4.3KB) - 唯一缓存实现
- tools/tool_cache.go (15KB) - 包含 ToolCache 接口和 MemoryToolCache（需评估）
- tools/cache_test.go - 基础测试
- tools/cache_race_test.go - 竞态测试

### 9. 迁移策略
1. 删除所有分片缓存相关文件
2. 评估 tool_cache.go 中的 MemoryToolCache 是否需要保留
3. 确保 SimpleToolCache 作为唯一实现
4. 更新所有引用分片缓存的代码
5. 运行测试验证
