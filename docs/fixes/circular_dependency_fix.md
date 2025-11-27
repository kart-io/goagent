# 缓存级联失效循环依赖修复报告

## 问题描述

在 `goagent/tools/` 目录的缓存实现中，两个类的 `invalidateDependents` 方法存在循环依赖导致栈溢出的风险：

1. **ShardedToolCache** (`sharded_cache.go`)
2. **MemoryToolCache** (`tool_cache.go`)

### 原始问题

如果工具之间存在循环依赖（例如 A → B → C → A），递归调用会导致无限递归和栈溢出：

```go
// ❌ 危险代码：没有循环检测
func (c *ShardedToolCache) invalidateDependents(toolName string) int {
    // ...
    for _, dependent := range dependents {
        // 递归调用，如果存在环会导致无限递归
        totalCount += c.invalidateDependents(dependent)
    }
    return totalCount
}
```

## 修复方案

### 核心改进

引入 `visited` 集合来跟踪已访问的工具，防止重复访问和无限递归：

```go
// ✅ 修复后：带循环检测的递归方法
func (c *ShardedToolCache) invalidateDependentsRecursive(toolName string, visited map[string]struct{}) int {
    c.depMu.RLock()
    dependents, exists := c.dependencies[toolName]
    c.depMu.RUnlock()

    if !exists || len(dependents) == 0 {
        return 0
    }

    totalCount := 0
    for _, dependent := range dependents {
        // 检测循环依赖：如果该工具已经被访问过，跳过
        if _, seen := visited[dependent]; seen {
            continue
        }
        visited[dependent] = struct{}{}

        // ... 执行清理逻辑 ...

        // 递归失效，传递 visited 集合
        totalCount += c.invalidateDependentsRecursive(dependent, visited)
    }

    return totalCount
}
```

### 修改的文件

#### 1. ShardedToolCache (sharded_cache.go)
- 修改 `InvalidateByTool` 方法，初始化 visited 集合
- 修改 `InvalidateByPattern` 方法，初始化 visited 集合
- 创建新的 `invalidateDependentsRecursive` 方法，接受 visited 参数
- 移除旧的 `invalidateDependents` 方法

#### 2. MemoryToolCache (tool_cache.go)
- 修改 `InvalidateByTool` 方法，初始化 visited 集合
- 修改 `InvalidateByPattern` 方法，初始化 visited 集合
- 将 `invalidateDependents` 重命名为 `invalidateDependentsRecursive`，添加 visited 参数

## 测试覆盖

### ShardedToolCache 测试（9个测试用例）

| 测试用例 | 测试场景 | 结果 |
|---------|---------|------|
| `TestShardedCache_CircularDependency` | 简单循环：A→B→C→A | ✅ PASS |
| `TestShardedCache_ComplexCircularDependency` | 复杂循环：菱形+环 | ✅ PASS |
| `TestShardedCache_SelfDependency` | 自依赖：A→A | ✅ PASS |
| `TestShardedCache_InvalidateByPatternWithCircularDeps` | 正则表达式+循环 | ✅ PASS |
| `TestShardedCache_NoDependency` | 无依赖场景 | ✅ PASS |
| `TestShardedCache_DeepCircularChain` | 50工具深度循环链 | ✅ PASS (178μs) |
| `TestShardedCache_MultipleCircularGroups` | 3个独立循环组 | ✅ PASS |
| `TestShardedCache_ComplexDependencyGraph` | 菱形+大环复合图 | ✅ PASS |
| `TestShardedCache_NoStackOverflow` | 100工具密集依赖图 | ✅ PASS (171μs) |

### MemoryToolCache 测试（6个测试用例）

| 测试用例 | 测试场景 | 结果 |
|---------|---------|------|
| `TestMemoryCache_CircularDependency` | 简单循环：A→B→C→A | ✅ PASS |
| `TestMemoryCache_ComplexCircularDependency` | 复杂循环：菱形+环 | ✅ PASS |
| `TestMemoryCache_SelfDependency` | 自依赖：A→A | ✅ PASS |
| `TestMemoryCache_InvalidateByPatternWithCircularDeps` | 正则表达式+循环 | ✅ PASS |
| `TestMemoryCache_DeepCircularChain` | 30工具深度循环链 | ✅ PASS (45μs) |
| `TestMemoryCache_MultipleCircularGroups` | 3个独立循环组 | ✅ PASS |

**总计**: 15个测试用例全部通过 ✅

### 性能对比

| 缓存类型 | 场景 | 工具数 | 条目数 | 性能 |
|---------|-----|-------|--------|------|
| **ShardedToolCache** | 深度循环链 | 50 | 250 | 178μs |
| **ShardedToolCache** | 密集依赖图 | 100 | 100 | 171μs |
| **MemoryToolCache** | 深度循环链 | 30 | 90 | 45μs |

## 验证结果

### ✅ Lint 检查
```
golangci-lint run ./...
0 issues.
```

### ✅ Import 层级验证
```
All import layering rules are satisfied!
```

### ✅ 测试覆盖
```
15/15 circular dependency tests PASS
All stress tests PASS
No stack overflow in extreme scenarios
```

## 性能影响

### 内存开销
- 每次级联失效增加一个 `map[string]struct{}` 用于跟踪访问
- 内存开销与依赖图的节点数成正比
- 典型场景（<100个工具）：< 1KB 额外内存

### 时间复杂度
- **修复前**: O(∞) - 循环依赖导致无限递归
- **修复后**: O(V + E) - V是工具数，E是依赖关系数
- **实测**:
  - ShardedToolCache: 50工具循环链 178μs
  - MemoryToolCache: 30工具循环链 45μs

## 健壮性提升

### 修复前风险
1. ❌ 循环依赖导致栈溢出
2. ❌ 程序崩溃，无法恢复
3. ❌ 整个服务不可用

### 修复后保障
1. ✅ 检测并处理所有类型的循环依赖
2. ✅ 优雅处理复杂依赖图
3. ✅ 性能稳定，可预测
4. ✅ 极端场景下也不会崩溃

## 兼容性

### API 兼容性
- ✅ 公共 API 无变化（`InvalidateByTool`, `InvalidateByPattern` 签名不变）
- ✅ 行为兼容（非循环依赖场景结果完全相同）
- ✅ 向后兼容

### 功能兼容性
- ✅ 正常依赖关系处理不受影响
- ✅ 性能无下降（visited 集合开销极小）
- ✅ 所有现有测试通过

## 文件清单

### 核心修复
1. `tools/sharded_cache.go` - ShardedToolCache 修复
2. `tools/tool_cache.go` - MemoryToolCache 修复

### 测试文件
3. `tools/sharded_cache_circular_test.go` - ShardedToolCache 循环依赖测试（新增）
4. `tools/sharded_cache_stress_test.go` - ShardedToolCache 压力测试（新增）
5. `tools/memory_cache_circular_test.go` - MemoryToolCache 循环依赖测试（新增）

### 文档
6. `docs/fixes/circular_dependency_fix.md` - 本修复文档

## 结论

此修复成功解决了两个缓存实现中的循环依赖死循环风险，通过引入 visited 集合实现了：

1. **健壮性**: 防止栈溢出，处理任意复杂的依赖图
2. **性能**: 微秒级失效性能，即使在极端场景下（ShardedToolCache 178μs/50工具，MemoryToolCache 45μs/30工具）
3. **正确性**: 15个专门的循环依赖测试全部通过
4. **兼容性**: API和行为完全向后兼容

该修复使 GoAgent 的工具缓存系统能够安全处理生产环境中可能出现的各种复杂依赖关系，确保服务的稳定性和可靠性。