## 项目上下文摘要（删除 DataPool 过度优化）
生成时间：2025-11-30

### 1. 任务背景
根据性能审查报告（REVIEW_REPORT_V2.md），performance/datapool.go（636行）导致性能下降39%。
- Benchmark 证据：WithPool 68.71 ns/op vs WithoutPool 49.40 ns/op
- 结论：对象池优化适得其反，增加了同步开销

### 2. 需要删除的文件
- `/Users/costalong/code/go/src/github.com/kart/goagent/performance/datapool.go` (主实现)
- `/Users/costalong/code/go/src/github.com/kart/goagent/performance/datapool_test.go` (测试文件)

### 3. 依赖该对象池的文件
**示例代码**：
- `examples/basic/10-object-pooling/main.go` (使用 performance.GetAgentInput/Output 等函数)
- `examples/basic/10-object-pooling/README.md` (文档)

**文档引用**：
- `performance/OBJECT_POOLING.md` (文档)
- `performance/README.md` (可能有引用)
- `examples/advanced/pool-decoupled-architecture/CLEANUP_SUMMARY.md` (历史记录)

### 4. 迁移策略
**原代码模式**：
```go
// 旧：使用对象池
input := performance.GetAgentInput()
defer performance.PutAgentInput(input)
```

**新代码模式**：
```go
// 新：直接创建
input := &core.AgentInput{
    Context: make(map[string]interface{}),
}
```

**AgentOutput 同理**：
```go
// 旧
output := performance.GetAgentOutput()
defer performance.PutAgentOutput(output)

// 新
output := &core.AgentOutput{
    ReasoningSteps: make([]core.ReasoningStep, 0),
    ToolCalls:      make([]core.ToolCall, 0),
    Metadata:       make(map[string]interface{}),
}
```

### 5. 项目约定
- **命名约定**：Go 标准风格，驼峰命名
- **文件组织**：`performance/` 包专注性能优化
- **导入顺序**：标准库 → 第三方库 → 本地包
- **代码风格**：`goimports` 格式化

### 6. 测试策略
- **测试框架**：Go 标准 testing 包
- **测试模式**：
  - 单元测试：`Test*` 函数
  - 基准测试：`Benchmark*` 函数
- **覆盖要求**：删除后确保 `go test ./performance/...` 通过

### 7. 执行步骤
1. 删除 `performance/datapool.go`
2. 删除 `performance/datapool_test.go`
3. 删除示例代码 `examples/basic/10-object-pooling/`
4. 更新文档引用（删除或标记为已弃用）
5. 运行测试验证：
   - `go build ./performance/...`
   - `go test ./performance/...`
   - `go build ./examples/...`

### 8. 关键风险点
- **依赖清理**：确保没有遗漏的导入
- **编译检查**：删除后必须编译通过
- **测试覆盖**：其他测试文件可能依赖 DataPool
- **文档一致性**：删除所有对象池相关文档和示例

### 9. 验证标准
✅ `go build ./performance/...` 成功
✅ `go test ./performance/...` 全部通过
✅ `go build ./examples/...` 成功（或删除示例）
✅ 无残留的 `performance.DataPool` 导入
✅ 文档更新完成
