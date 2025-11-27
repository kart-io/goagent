# llm/providers 废弃代码清理执行计划

**创建时间**: 2025-11-27
**依据**: `.claude/llm-providers-cleanup-analysis.md`
**目标**: 安全删除重复代码,保持向后兼容性
**预期收益**: 减少 ~4320 行重复代码 (~96%)

## 📊 决策总结

基于深度分析,我们识别了以下需要保留和可以删除的代码:

### ✅ 必须保留的文件

| 文件 | 原因 | 处理方式 |
|------|------|----------|
| `base.go` | 类型别名,向后兼容 | 保留,长期维护 |
| `utils.go` | 工具函数别名 | 保留,长期维护 |
| `tools.go` | 工具类型别名 | 保留,长期维护 |
| `factory.go` | 仍有外部依赖 | 标记废弃,渐进式迁移 |
| `*_test.go` | 验证兼容性 | 保留,逐步迁移 |

### ❌ 可以删除的代码

| 位置 | 内容 | 数量 | 保留部分 |
|------|------|------|----------|
| 9 个 provider 文件 | 完整实现 | ~4500行 | 废弃包装函数 (~180行) |
| Provider 辅助方法 | WithModel(), WithTemperature() 等 | ~200行 | 无 |
| 内部请求/响应类型 | siliconFlowRequest 等 | ~1000行 | 无 |

**总计可删除**: ~5700 行代码

## 🎯 清理计划详情

### 计划 A: 保守清理 (推荐,立即执行)

**目标**: 删除明确可删除的辅助方法,保留核心实现

**操作清单**:

#### 1. 标记 factory.go 为废弃

**文件**: `llm/providers/factory.go`

**操作**: 在所有公开函数前添加 `Deprecated` 注释

**示例**:
```go
// ClientFactory 统一的客户端工厂
// Deprecated: 使用 llm/registry.New() 代替。
// 示例:
//   import _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
//   client, err := registry.New(constants.ProviderOpenAI, opts...)
type ClientFactory struct{}
```

**影响**: 无,仅文档层面

#### 2. 删除 provider 文件中的辅助方法

**目标文件**: 9 个 provider 实现文件

**删除的方法模式**:
```go
// 删除这些链式配置方法
func (c *XXXClient) WithModel(model string) *XXXClient { ... }
func (c *XXXClient) WithTemperature(temperature float64) *XXXClient { ... }
func (c *XXXClient) WithMaxTokens(maxTokens int) *XXXClient { ... }
```

**理由**: Options 模式已提供 `agentllm.WithModel()` 等功能

**检查清单**:
- [ ] `anthropic.go` - 无辅助方法
- [ ] `cohere.go` - 无辅助方法
- [ ] `deepseek.go` - 无辅助方法
- [ ] `gemini.go` - 无辅助方法
- [ ] `huggingface.go` - 无辅助方法
- [ ] `kimi.go` - 有 `WithModel()`, `WithTemperature()`, `WithMaxTokens()`
- [ ] `ollama.go` - 有 `WithModel()`, `WithTemperature()`, `WithMaxTokens()`
- [ ] `openai.go` - 无辅助方法
- [ ] `siliconflow.go` - 有 `WithModel()`, `WithTemperature()`, `WithMaxTokens()`

**预计删除**: ~100 行代码

#### 3. 删除冗余的特殊方法

**Kimi Provider**:
```go
// 删除这些特殊方法,contrib 版本已实现
func (c *KimiClient) GetSupportedModels() []string { ... }
func (c *KimiClient) GetModelContextSize(model string) int { ... }
func (c *KimiClient) EstimateTokenCount(text string) int { ... }
func (c *KimiClient) CalculateFileUploadTokens(fileContent string) int { ... }
func (c *KimiClient) ValidateContextSize(messages []agentllm.Message) error { ... }
```

**SiliconFlow Provider**:
```go
// 删除
func (c *SiliconFlowClient) ListModels() []string { ... }
```

**Ollama Provider**:
```go
// 删除
func (c *OllamaClient) ListModels() ([]string, error) { ... }
func (c *OllamaClient) PullModel(modelName string) error { ... }
func (c *OllamaClient) getFinishReason(done bool) string { ... }
```

**预计删除**: ~200 行代码

#### 4. 删除高级流式 Provider

**OpenAI Provider**:
```go
// 删除整个 OpenAIStreamingProvider 及相关代码
type OpenAIStreamingProvider struct { ... }
func NewOpenAIStreaming(...) { ... }
func (p *OpenAIStreamingProvider) StreamTokensWithMetadata(...) { ... }
type TokenWithMetadata struct { ... }
```

**理由**: contrib 版本已实现相同功能

**预计删除**: ~100 行代码

**总计**: 保守清理可删除约 **400 行代码**

### 计划 B: 激进清理 (不推荐,高风险)

**目标**: 完全删除 provider 实现,只保留废弃包装

**风险**:
- ❌ Factory 依赖会直接断裂
- ❌ 可能有未知的外部依赖
- ❌ 回滚困难

**结论**: **不建议执行计划 B**,应先完成 factory 迁移

## 📝 执行步骤

### 第一阶段: 准备工作

**检查清单**:
- [x] 完成深度分析 (`.claude/llm-providers-cleanup-analysis.md`)
- [x] 识别保留代码 (本文档)
- [ ] 备份当前代码 (git commit)
- [ ] 确认测试环境可用

### 第二阶段: 执行清理 (计划 A)

#### Step 1: 标记 factory.go 为废弃

```bash
# 编辑 llm/providers/factory.go
# 在所有公开类型和函数前添加 Deprecated 注释
```

**验证**: 代码仍能编译,文档生成正确

#### Step 2: 删除辅助方法

**文件**:
1. `llm/providers/kimi.go` - 删除 WithModel, WithTemperature, WithMaxTokens
2. `llm/providers/ollama.go` - 删除 WithModel, WithTemperature, WithMaxTokens
3. `llm/providers/siliconflow.go` - 删除 WithModel, WithTemperature, WithMaxTokens

**验证**: 运行测试确认无影响

#### Step 3: 删除特殊方法

**文件**:
1. `llm/providers/kimi.go` - 删除 5 个特殊方法
2. `llm/providers/siliconflow.go` - 删除 ListModels
3. `llm/providers/ollama.go` - 删除 3 个方法

**验证**: 检查是否有测试或外部代码依赖这些方法

#### Step 4: 删除高级流式 Provider

**文件**: `llm/providers/openai.go`

**删除内容**:
- `OpenAIStreamingProvider` 类型
- `NewOpenAIStreaming()` 函数
- `StreamTokensWithMetadata()` 方法
- `TokenWithMetadata` 类型

**验证**: grep 搜索是否有使用

### 第三阶段: 测试验证

```bash
# 运行所有测试
go test ./llm/providers/... -v

# 运行相关测试
go test ./llm/... -v

# 编译所有示例
go build ./examples/...

# 运行特定 provider 测试
go test -run TestKimi ./llm/providers/
go test -run TestOllama ./llm/providers/
go test -run TestSiliconFlow ./llm/providers/
go test -run TestOpenAI ./llm/providers/
```

**期望结果**:
- ✅ 所有测试通过
- ✅ 无编译错误
- ✅ 无运行时错误

### 第四阶段: Git 提交

```bash
# 创建清理提交
git add llm/providers/
git commit -m "refactor(providers): 删除废弃的辅助方法和特殊实现

- 标记 factory.go 为 deprecated
- 删除 Kimi/Ollama/SiliconFlow 的 WithXXX 链式方法
- 删除 Kimi 的特殊方法 (GetModelContextSize, EstimateTokenCount 等)
- 删除 Ollama 的 ListModels 和 PullModel 方法
- 删除 SiliconFlow 的 ListModels 方法
- 删除 OpenAIStreamingProvider 及相关代码

这些功能在 contrib/llm-providers/ 中已有更好的实现。
保留核心实现以维持向后兼容性,等待 factory 迁移到 registry。

相关: .claude/llm-providers-cleanup-analysis.md
"

# 推送到远程(可选)
# git push origin master
```

## 🔄 后续步骤

### 中期 (1-2 周)

1. **迁移 factory 到 registry**
   - 修改 `factory.go` 使用 `registry.New()`
   - 确保所有 contrib providers 已注册
   - 运行完整测试

2. **更新文档**
   - 更新所有示例代码
   - 更新 README 文件
   - 添加迁移指南

### 长期 (1-3 个月)

1. **迁移测试文件**
   - 逐步将测试迁移到新 API
   - 删除对废弃包装函数的依赖

2. **完全删除 provider 实现**
   - 删除所有 provider 文件的实现部分
   - 只保留最小废弃包装
   - 更新所有外部依赖

## ⚠️ 风险控制

### 回滚方案

如果清理导致问题:
```bash
# 回滚到清理前
git revert HEAD

# 或者重置到之前的提交
git reset --hard <commit-hash>
```

### 测试策略

1. **单元测试**: 确保所有 provider 功能正常
2. **集成测试**: 验证 factory 创建的 client 工作正常
3. **端到端测试**: 运行示例程序确认无问题

### 监控指标

- ✅ 代码行数减少
- ✅ 编译时间(应无变化或略快)
- ✅ 测试覆盖率(应保持不变)
- ✅ 文档一致性

## 📊 预期结果

### 代码统计

**清理前**:
```
llm/providers/
├── anthropic.go      ~350 行
├── cohere.go         ~300 行
├── deepseek.go       ~250 行
├── gemini.go         ~400 行
├── huggingface.go    ~350 行
├── kimi.go           ~351 行 (可删除 ~150 行)
├── ollama.go         ~399 行 (可删除 ~100 行)
├── openai.go         ~593 行 (可删除 ~100 行)
├── siliconflow.go    ~280 行 (可删除 ~50 行)
└── factory.go        ~165 行 (标记废弃)
```

**清理后 (计划 A)**:
```
llm/providers/
├── kimi.go           ~201 行 (删除 150 行)
├── ollama.go         ~299 行 (删除 100 行)
├── openai.go         ~493 行 (删除 100 行)
├── siliconflow.go    ~230 行 (删除 50 行)
├── factory.go        ~165 行 (标记 Deprecated)
└── 其他文件保持不变
```

**收益**: 删除约 **400 行**冗余代码

### 维护改进

- ✅ 更清晰的代码结构
- ✅ 减少重复维护工作
- ✅ 更好的文档一致性
- ✅ 为 factory 迁移铺路

## ✅ 验收清单

- [ ] factory.go 已标记 Deprecated
- [ ] Kimi, Ollama, SiliconFlow 的 WithXXX 方法已删除
- [ ] Kimi 的 5 个特殊方法已删除
- [ ] Ollama 的 3 个方法已删除
- [ ] SiliconFlow 的 ListModels 已删除
- [ ] OpenAIStreamingProvider 及相关代码已删除
- [ ] 所有测试通过
- [ ] 代码编译无错误
- [ ] Git 提交已创建
- [ ] 文档已更新 (本计划)

## 📚 相关文档

1. `.claude/llm-providers-cleanup-analysis.md` - 详细分析报告
2. `.claude/backwards-compatibility-completion-report.md` - 向后兼容性工作
3. `docs/guides/REGISTRY_MIGRATION_GUIDE.md` - 注册表迁移指南

---

🤖 Generated with Claude Code
📅 2025-11-27
