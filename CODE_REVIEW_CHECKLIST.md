# Code Review Checklist - 代码审查清单

**审查日期:** 2025-11-26
**审查范围:** 代码质量修复和安全增强
**审查人员:** [待填写]

## 变更概览

### 修改的文件 (2)

1. `llm/providers/base.go` - 重试抖动回退策略修复
2. `tools/practical/database_query.go` - 日志记录和 SQL 注入防护增强

### 新增的文件 (11)

#### 测试文件 (9)

1. `llm/providers/base_retry_test.go` - 重试逻辑测试
2. `llm/providers/base_http_test.go` - HTTP 错误映射测试
3. `llm/providers/base_test.go` - BaseProvider 核心功能测试
4. `llm/providers/utils_test.go` - 工具函数测试
5. `llm/providers/capabilities_test.go` - 能力检查测试
6. `llm/providers/message_conversion_test.go` - 消息转换测试
7. `llm/providers/factory_test.go` - 工厂模式测试
8. `tools/practical/database_query_security_test.go` - SQL 注入防护测试
9. `tools/practical/database_query_enhanced_security_test.go` - 增强安全测试

#### 文档文件 (2)

1. `tools/shell/README.md` - Shell 工具完整文档
2. `tools/practical/README.md` - 数据库工具完整文档（含安全警告）

## 详细审查项

### 1. llm/providers/base.go

#### 问题背景

- **原问题:** 当 crypto/rand 失败时，`secureRandomInt63n` 返回 0，导致所有重试请求同时发生（雷鸣群效应）
- **风险等级:** 🔴 P0 - 高并发场景下可能导致级联故障
- **影响范围:** 所有使用 ExecuteWithRetry 的 LLM 调用

#### 修改内容

**新增代码 (Lines 21-27):**

```go
var (
    insecureRand     *mathrand.Rand
    insecureRandOnce sync.Once
    insecureRandMu   sync.Mutex
)
```

**审查要点:**

- [ ] 全局变量命名清晰（insecure 前缀表明非加密安全）
- [ ] 使用 sync.Once 确保初始化只执行一次
- [ ] 使用 mutex 保护并发访问

**修改函数 (Lines 273-306):**

```go
func secureRandomInt63n(n int64) int64 {
    // ... 保持 crypto/rand 作为首选

    if _, err := rand.Read(b[:]); err != nil {
        // 回退到 math/rand
        insecureRandOnce.Do(func() {
            insecureRand = mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
        })

        insecureRandMu.Lock()
        result := insecureRand.Int63n(n)
        insecureRandMu.Unlock()

        return result
    }
    // ...
}
```

**审查要点:**

- [ ] 优先使用 crypto/rand，只在失败时回退
- [ ] 回退逻辑的文档注释清晰
- [ ] 线程安全性：sync.Once + mutex
- [ ] 性能影响：mutex 锁范围最小化
- [ ] 向后兼容：函数签名未变
- [ ] 测试覆盖：是否有测试验证回退逻辑？

**潜在风险:**

- ⚠️ math/rand 在高并发下可能有竞态条件（已通过 mutex 解决）
- ⚠️ 首次失败后会一直使用 insecureRand（可接受，因为 crypto/rand 失败通常是系统级问题）

**验证建议:**

```bash
# 1. 验证线程安全
go test -race ./llm/providers -run TestSecureRandomInt63n

# 2. 验证随机性
go test -v ./llm/providers -run TestSecureRandomInt63n_Randomness

# 3. 性能测试
go test -bench=BenchmarkMessageConversion ./llm/providers
```

---

### 2. tools/practical/database_query.go

#### 问题背景

- **问题 1:** 使用 `fmt.Printf` 记录错误，无法进行日志聚合
- **问题 2:** SQL 注入防护不够全面，只检查基础模式

#### 修改 1: 日志记录优化

**修改位置:**

- Line 287: `getConnection` - 数据库连接关闭失败
- Line 325: `executeQuery` - 查询结果行关闭失败
- Line 439: `executeTransaction` - 事务回滚失败

**审查要点:**

- [ ] 已添加 `log/slog` 导入
- [ ] 所有 `fmt.Printf` 已替换为 `slog.Error`
- [ ] 结构化字段包含足够的上下文信息
- [ ] 避免了变量遮蔽（closeErr, rollbackErr）
- [ ] 错误返回逻辑未改变

**示例修改:**

```go
// 修改前
fmt.Printf("failed to close database connection: %v", err)

// 修改后
slog.Error("failed to close database connection",
    "error", closeErr,
    "connection_id", id,
    "driver", config.Driver,
    "component", "database_query_tool",
    "operation", "getConnection")
```

**审查要点:**

- [ ] 日志级别正确（Error 用于错误情况）
- [ ] 字段名称一致（error, component, operation）
- [ ] 敏感信息已脱敏（DSN 不应记录）

#### 修改 2: SQL 注入防护增强

**修改函数:** `sanitizeQuery` (Lines 21-69)

**新增检查:**

1. **UNION 注入检测**

```go
if strings.Contains(upperQuery, " UNION ") ||
   strings.Contains(upperQuery, " UNION ALL ") {
    return agentErrors.New(...)
}
```

**审查要点:**

- [ ] 大小写不敏感检测（使用 upperQuery）
- [ ] 检测空格包围的 UNION（避免误判 REUNION 等词）
- [ ] 错误消息清晰

2. **布尔表达式注入检测**

```go
dangerousPatterns := []string{
    " OR 1=1", " OR '1'='1'", " OR \"1\"=\"1\"",
    " AND 1=1", " AND '1'='1'", " OR TRUE", " AND TRUE",
    " OR `1`=`1`", " AND `1`=`1`",
}
```

**审查要点:**

- [ ] 覆盖常见注入模式
- [ ] 考虑不同引号类型（单引号、双引号、反引号）
- [ ] 空格包围避免误判
- [ ] 是否有假阳性风险？

**潜在假阳性场景:**

```sql
-- 这些合法查询可能被误判
SELECT * FROM products WHERE price > 100 OR category = 'premium'  -- 包含 " OR "
SELECT * FROM logs WHERE level = 'INFO' AND processed = TRUE       -- 包含 " AND TRUE"
```

**缓解措施:**

- ✅ 文档明确说明必须配合参数化查询使用
- ✅ 合法的 OR/AND 应该使用参数化查询
- ⚠️ 考虑添加白名单模式（下一阶段）

**文档改进:**

```go
// sanitizeQuery performs basic SQL query sanitization checks.
// WARNING: This is NOT a complete SQL injection prevention solution.
// ALWAYS use parameterized queries for user inputs.
```

**审查要点:**

- [ ] 警告信息足够明确
- [ ] 说明了局限性
- [ ] 指导正确使用方法

---

### 3. 测试文件审查

#### llm/providers 测试文件 (7 个)

**覆盖率目标:** 从 46.6% 提升到 55.2%（已达成）

**审查清单:**

- [ ] **base_retry_test.go**
  - [ ] 测试成功场景、重试场景、最大重试
  - [ ] 测试 Context 取消
  - [ ] 测试随机数生成的范围和随机性
  - [ ] 测试使用 `context.WithValue` 设置测试延迟

- [ ] **base_http_test.go**
  - [ ] 覆盖所有 HTTP 状态码（400, 401, 403, 404, 429, 500+）
  - [ ] 测试 Retry-After 头解析
  - [ ] 测试自定义错误解析器

- [ ] **base_test.go**
  - [ ] 测试配置创建和默认值
  - [ ] 测试 Get* 方法的 fallback 逻辑
  - [ ] 测试 HTTP 客户端创建

- [ ] **utils_test.go**
  - [ ] 测试 parseRetryAfter（整数、RFC1123、无效格式）
  - [ ] 测试 generateCallID 的唯一性
  - [ ] 测试 isRetryable 的错误类型判断

- [ ] **capabilities_test.go**
  - [ ] 测试能力添加、检查、列出

- [ ] **message_conversion_test.go**
  - [ ] 测试各种消息转换场景
  - [ ] 测试边界情况（空消息列表等）

- [ ] **factory_test.go**
  - [ ] 测试所有 Provider 的创建
  - [ ] 测试生产/开发环境客户端

**通用审查要点:**

- [ ] 所有测试函数有清晰的命名
- [ ] 使用 table-driven tests（where appropriate）
- [ ] 测试独立，不依赖执行顺序
- [ ] 适当使用 t.Helper()
- [ ] 错误消息清晰，便于调试

#### tools/practical 安全测试 (2 个)

**覆盖率目标:** 从 36.7% 提升到 39.9%（已达成）

**审查清单:**

- [ ] **database_query_security_test.go**
  - [ ] 测试各种 SQL 注入攻击模式
  - [ ] 测试参数化查询 vs 字符串拼接
  - [ ] 测试边界情况
  - [ ] 测试真实数据库场景（SQLite in-memory）

- [ ] **database_query_enhanced_security_test.go**
  - [ ] 63 个安全测试用例
  - [ ] 覆盖所有 sanitizeQuery 的检查逻辑
  - [ ] 测试大小写变体
  - [ ] 测试组合注入

**安全测试要点:**

- [ ] 测试用例包含真实的攻击模式
- [ ] 测试既有阻止（应该失败的）也有允许（应该成功的）
- [ ] 测试不会泄露敏感信息到日志
- [ ] 测试消息清晰，说明为什么被阻止

---

### 4. 文档审查

#### tools/shell/README.md

**审查清单:**

- [ ] 遵循 MarkdownLint 规则
- [ ] 包含所有章节：
  - [ ] 概述和特性
  - [ ] 安全特性详解
  - [ ] 使用示例（基本、Builder、工作目录、超时、脚本、管道）
  - [ ] API 参考
  - [ ] 安全最佳实践
  - [ ] 常见问题
- [ ] 代码示例可运行
- [ ] 安全警告突出显示
- [ ] 中文内容准确流畅

**特别关注:**

- [ ] 危险字符列表完整
- [ ] 白名单机制说明清楚
- [ ] 示例代码没有安全漏洞

#### tools/practical/README.md

**审查清单:**

- [ ] ⚠️ 安全警告在最前面且醒目
- [ ] 正确用法 vs 错误用法对比清晰
- [ ] 包含所有章节：
  - [ ] 安全警告（最前面）
  - [ ] 概述
  - [ ] 支持的数据库
  - [ ] 安全特性
  - [ ] 使用示例
  - [ ] API 参考
  - [ ] 安全最佳实践
  - [ ] SQL 注入攻击示例
  - [ ] 常见问题
- [ ] SQL 注入示例准确且有教育意义
- [ ] 强调参数化查询的重要性

**特别关注:**

- [ ] 攻击示例不会教导恶意使用
- [ ] 防护建议具体可行
- [ ] DSN 示例不包含真实密码
- [ ] 表名/列名白名单建议清楚

---

## 质量验证

### Lint 检查

```bash
make lint
# 期望：0 issues ✅
```

**审查要点:**

- [ ] 无 ineffassign 警告
- [ ] 无 errcheck 警告
- [ ] 无 staticcheck 警告
- [ ] 无 unused 警告

### 测试验证

```bash
# 运行所有测试
go test ./...

# 运行修改模块的测试
go test ./llm/providers ./tools/practical ./tools/shell -v -race
```

**审查要点:**

- [ ] 所有测试通过
- [ ] 无 race condition
- [ ] 无 panic 或 unexpected error
- [ ] 测试执行时间合理（< 2 分钟）

### 覆盖率验证

```bash
# 生成覆盖率报告
go test -coverprofile=coverage.out ./llm/providers ./tools/practical ./tools/shell
go tool cover -html=coverage.out -o coverage.html
```

**审查要点:**

- [ ] llm/providers: 55.2%（从 46.6%）
- [ ] tools/practical: 39.9%（从 36.7%）
- [ ] tools/shell: 97.1%（保持）
- [ ] 核心文件覆盖率：
  - [ ] base.go: 91.3%
  - [ ] factory.go: 99.4%
  - [ ] sanitizeQuery: 100%

### 架构验证

```bash
./verify_imports.sh
```

**审查要点:**

- [ ] 所有导入层级规则通过
- [ ] 无循环依赖
- [ ] 新增测试文件符合规范

---

## 性能影响评估

### 重试逻辑性能

```bash
# 运行性能测试
go test -bench=BenchmarkMessageConversion ./llm/providers
```

**审查要点:**

- [ ] 对象池性能未退化
- [ ] 随机数生成性能可接受
- [ ] mutex 锁竞争不明显

### SQL 检查性能

```bash
# 基准测试 sanitizeQuery
go test -bench=BenchmarkSanitizeQuery ./tools/practical
```

**审查要点:**

- [ ] 检查时间 < 1μs
- [ ] 无明显的性能退化
- [ ] 内存分配合理

---

## 安全性评估

### 潜在风险

1. **重试抖动回退**
   - ⚠️ math/rand 非加密安全（已明确标注）
   - ✅ 仅用于重试延迟，不涉及安全决策
   - ✅ crypto/rand 是首选，只在失败时回退

2. **SQL 注入防护**
   - ⚠️ 基础检查不能防止所有注入
   - ✅ 文档明确说明必须配合参数化查询
   - ⚠️ 可能有假阳性（合法查询被拒绝）
   - ✅ 提供了详细的使用指南

3. **日志记录**
   - ⚠️ 确保不记录敏感信息（密码、API key、DSN）
   - ✅ 使用结构化日志便于审计

### 安全检查清单

- [ ] 没有硬编码密码或 API key
- [ ] 敏感信息已脱敏或不记录
- [ ] 错误消息不泄露内部实现细节
- [ ] 所有用户输入都经过验证
- [ ] 文档包含安全警告和最佳实践

---

## 向后兼容性

### API 变更

- [ ] llm/providers/base.go
  - [ ] ✅ 函数签名未变
  - [ ] ✅ 只添加了内部实现
  - [ ] ✅ 完全向后兼容

- [ ] tools/practical/database_query.go
  - [ ] ✅ 函数签名未变
  - [ ] ⚠️ sanitizeQuery 检查更严格，可能拒绝之前能执行的查询
  - [ ] ⚠️ 需要在 CHANGELOG 中说明

### 迁移指南

**如果遇到 sanitizeQuery 拒绝合法查询:**

```go
// 之前可能可以执行
query := "SELECT * FROM users WHERE status = 'active' OR role = 'admin'"

// 现在被拒绝（包含 " OR "）
// 解决方案：使用参数化查询
query := "SELECT * FROM users WHERE status = ? OR role = ?"
params := []interface{}{"active", "admin"}
```

---

## 文档更新需求

### 需要更新的文档

- [ ] CHANGELOG.md - 记录所有变更
- [ ] docs/guides/SECURITY.md - 添加新的安全特性
- [ ] docs/guides/MIGRATION_GUIDE.md - 添加迁移说明（如有破坏性变更）

### CHANGELOG 建议内容

```markdown
## [Unreleased]

### Fixed
- 修复重试抖动失败回退策略，防止雷鸣群效应 (#issue-number)
- 优化错误日志记录，使用结构化日志 (#issue-number)

### Security
- 加强 SQL 注入防护，新增 UNION 和布尔表达式检测 (#issue-number)
- 添加 63 个 SQL 注入防护测试用例

### Added
- 为 llm/providers 添加 7 个测试文件，覆盖率从 46.6% 提升到 55.2%
- 为 tools/practical 添加安全测试，覆盖率从 36.7% 提升到 39.9%
- 新增 tools/shell/README.md 完整文档
- 新增 tools/practical/README.md 完整文档（含安全警告）

### Changed
- sanitizeQuery 检查更严格，可能拒绝包含 OR/AND 的合法查询
  建议使用参数化查询替代字符串拼接
```

---

## 审查决策

### 通过条件

- [ ] 所有审查项都已检查
- [ ] Lint: 0 issues
- [ ] 测试: 100% 通过
- [ ] 覆盖率: 达到目标
- [ ] 架构: 通过验证
- [ ] 安全: 无高风险问题
- [ ] 文档: 完整准确
- [ ] 向后兼容: 已评估

### 审查结果

- [ ] ✅ **批准合并** - 所有检查通过
- [ ] ⚠️ **有条件批准** - 需要修复小问题
- [ ] ❌ **需要修改** - 存在重大问题

### 审查意见

**优点:**

1.
2.
3.

**需要改进:**

1.
2.
3.

**阻塞问题（必须修复）:**

1.
2.

---

## 审查签名

| 审查项 | 审查人 | 日期 | 签名 |
|--------|--------|------|------|
| 代码逻辑 | | | |
| 测试覆盖 | | | |
| 安全性 | | | |
| 性能 | | | |
| 文档 | | | |
| 架构 | | | |

**最终批准:**

- [ ] Tech Lead: ________________  日期: ________
- [ ] Security: ________________  日期: ________

---

## 附录

### 相关 Issue/PR

- Issue #XXX: 重试抖动失败回退策略问题
- Issue #XXX: SQL 注入防护增强
- Issue #XXX: 测试覆盖率提升
- PR #XXX: 代码审查修复

### 参考资料

- [GoAgent 架构设计](../../docs/architecture/ARCHITECTURE.md)
- [导入层级规范](../../docs/architecture/IMPORT_LAYERING.md)
- [测试最佳实践](../../docs/development/TESTING_BEST_PRACTICES.md)
- [安全指南](../../docs/guides/SECURITY.md)
