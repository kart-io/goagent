# 下一步行动建议

## ✅ 已完成的工作

### 1. 测试覆盖率提升
- **cache/** 包: 0% → 89.7% ⭐
- **agents/tot/** 包: 0% → 71.8% ⭐
- 总体覆盖率: 44.3% → 45.7%
- 新增测试代码: 2,694 行

### 2. Context 传递优化
- 减少 context.Background() 使用: 154 → 32 个实例 (-79%)
- 新增 4 个支持 context 的 API
- 零破坏性变更，完全向后兼容

### 3. Qdrant 和 RAG 功能实现
- Qdrant 向量存储: 100% 实现（6个方法）
- RAG 链: 完整集成 LLM
- 多查询检索器: 实现查询变体生成
- Cohere 重排序: 生产环境 API 集成
- 测试覆盖率: 71.9%

---

## 🎯 建议的后续工作

### 优先级 1: 核心包测试覆盖率提升（高优先级）

#### 1. builder/ 包 (42.4% → 80%)
**预计工作量**: 2-3 天

**重点测试内容**:
```bash
# 需要增加的测试用例
- 中间件链错误注入测试
- 运行时初始化的所有路径
- 工具执行循环的边界情况
- 配置验证的错误处理
```

**执行命令**:
```bash
# 查看当前覆盖率
go test -coverprofile=coverage.out ./builder/
go tool cover -func=coverage.out

# 运行测试
go test -v ./builder/
```

#### 2. core/ 包 (53.2% → 80%)
**预计工作量**: 2-3 天

**重点测试内容**:
```bash
# 需要增加的测试用例
- BaseAgent 边界情况测试
- 回调错误场景
- 流式处理边界情况
- 并发操作测试
```

### 优先级 2: Agent 包测试完善（中优先级）

#### 3. agents/cot/ 包 (48.3% → 80%)
**预计工作量**: 1-2 天

**重点测试内容**:
```bash
# 需要增加的测试用例
- 推理路径的更多测试
- 工具集成场景
- 示例解析的边界情况
```

#### 4. agents/pot/ 包 (68.1% → 80%)
**预计工作量**: 1 天

**重点测试内容**:
```bash
# 需要增加的测试用例
- 代码执行场景
- 验证边界情况
- 特定语言路径测试
```

### 优先级 3: 流式处理（低优先级）

#### 5. stream/ 包 (41.1% → 80%)
**预计工作量**: 2-3 天

**重点测试内容**:
```bash
# 需要增加的测试用例
- 异步流式场景
- 多路复用器边界情况
- 缓冲区管理
```

**注意**: 此包需要专门的异步测试方法

---

## 📋 验证清单

在提交任何代码前，请确保:

```bash
# 1. 代码格式化
make fmt

# 2. Lint 检查（必须 0 错误）
make lint

# 3. 导入分层验证
./verify_imports.sh

# 4. 运行所有测试
make test

# 5. 检查覆盖率
go test ./... -coverprofile=coverage.out -covermode=atomic
go tool cover -func=coverage.out | tail -1
```

---

## 🚀 快速开始命令

### 开始提升 builder/ 包覆盖率
```bash
# 1. 查看当前测试
cat builder/builder_test.go | grep "func Test"

# 2. 运行测试并查看覆盖率
go test -v -coverprofile=coverage.out ./builder/
go tool cover -html=coverage.out -o coverage.html

# 3. 在浏览器中查看未覆盖的代码
open coverage.html  # macOS
# 或
xdg-open coverage.html  # Linux

# 4. 添加新测试到 builder/builder_test.go
# 5. 重新运行测试验证
```

### 开始提升 core/ 包覆盖率
```bash
# 1. 查看当前测试
ls -l core/*_test.go

# 2. 运行测试并查看覆盖率
go test -v -coverprofile=coverage.out ./core/
go tool cover -html=coverage.out -o coverage.html

# 3. 查看未覆盖的代码
open coverage.html
```

---

## 📊 预期成果

如果完成上述所有工作:

| 包 | 当前 | 目标 | 改进 |
|---|---|---|---|
| builder/ | 42.4% | 80% | +37.6 点 |
| core/ | 53.2% | 80% | +26.8 点 |
| agents/cot/ | 48.3% | 80% | +31.7 点 |
| agents/pot/ | 68.1% | 80% | +11.9 点 |
| stream/ | 41.1% | 80% | +38.9 点 |
| **总体** | **45.7%** | **~75-80%** | **+30-35 点** |

**总预计工作量**: 8-12 天

---

## 💡 测试编写技巧

### 1. 使用表驱动测试
```go
func TestExample(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {"case 1", "input1", "output1", false},
        {"case 2", "input2", "output2", false},
        {"error case", "bad", "", true},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := SomeFunc(tt.input)
            if tt.wantErr {
                assert.Error(t, err)
                return
            }
            assert.NoError(t, err)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

### 2. Mock LLM 客户端
```go
type mockLLMClient struct {
    response string
    err      error
}

func (m *mockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
    if m.err != nil {
        return nil, m.err
    }
    return &llm.CompletionResponse{Content: m.response}, nil
}
```

### 3. 测试并发操作
```go
func TestConcurrent(t *testing.T) {
    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            // 测试并发操作
        }(i)
    }
    wg.Wait()
}
```

---

## 📚 参考文档

- 完整实现报告: `IMPLEMENTATION_COMPLETE.md`
- Context 迁移指南: `CONTEXT_MIGRATION_REPORT.md`
- Qdrant 使用示例: `retrieval/USAGE_EXAMPLES.md`
- 项目测试规范: `docs/development/TESTING_BEST_PRACTICES.md`
- 架构指南: `CLAUDE.md`

---

## ❓ 需要帮助？

如果在实现过程中遇到问题:

1. 查看已有的高覆盖率包的测试示例（如 `agents/executor/executor_test.go`）
2. 参考 `CLAUDE.md` 中的测试最佳实践
3. 使用 `go test -coverprofile` 查看具体未覆盖的代码行
4. 遵循表驱动测试模式
5. 确保所有测试都有清晰的命名和文档

---

**生成时间**: 2025-11-20
**当前状态**: 所有关键问题已解决，可选择性继续提升覆盖率
