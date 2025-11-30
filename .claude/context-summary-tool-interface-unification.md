## 项目上下文摘要（Tool接口统一）
生成时间：2025-11-30 16:30:00

### 1. 重复接口分析

**发现的Tool接口定义：**

1. **interfaces/tool.go:18** （标准接口 - 保留）
   - 方法：`Name()`, `Description()`, `Invoke()`, `ArgsSchema()`
   - 使用 `*ToolInput` 和 `*ToolOutput` 类型
   - 已被 tools/ 包广泛使用
   - 文档完善，设计清晰

2. **core/orchestrator.go:136** （重复接口 - 删除）
   - 方法：`Name()`, `Description()`, `Execute()`, `Parameters()`
   - 使用自定义的 `ToolInput`/`ToolOutput` 类型
   - 方法名 `Execute` vs `Invoke`
   - 返回 `[]ToolParameter` 而非 JSON Schema 字符串

3. **mcp/core/tool.go:19** （MCP专用接口 - 保留但重命名）
   - 方法：`Name()`, `Description()`, `Category()`, `Schema()`, `Execute()`, `Validate()`, `RequiresAuth()`, `IsDangerous()`
   - MCP协议专用，包含更多元数据方法
   - 与 interfaces.Tool 语义不同，功能更丰富

4. **examples/advanced/multi-agent-modular/agents/execution.go:18** （示例代码 - 更新）
   - 方法：`Name()`, `Execute()`
   - 简化版接口，仅用于示例
   - 应改为使用 interfaces.Tool

### 2. 接口方法对比

| 方法 | interfaces.Tool | core/orchestrator.Tool | mcp/core.Tool | 示例Tool |
|------|-----------------|------------------------|---------------|----------|
| Name() | ✅ | ✅ | ✅ | ✅ |
| Description() | ✅ | ✅ | ✅ | ❌ |
| Invoke/Execute | Invoke | Execute | Execute | Execute |
| Schema定义 | ArgsSchema() string | Parameters() []ToolParameter | Schema() *ToolSchema | ❌ |
| Category | ❌ | ❌ | ✅ | ❌ |
| Validate | ❌ (ValidatableTool) | ❌ | ✅ | ❌ |
| RequiresAuth | ❌ | ❌ | ✅ | ❌ |
| IsDangerous | ❌ | ❌ | ✅ | ❌ |

### 3. 类型定义对比

**interfaces.ToolInput:**
```go
type ToolInput struct {
    Args      map[string]interface{}
    Context   context.Context
    CallerID  string
    TraceID   string
}
```

**core/orchestrator.ToolInput:**
```go
type ToolInput struct {
    Action     string
    Parameters map[string]interface{}
    Context    map[string]interface{}
}
```

**mcp/core:**
- 直接使用 `map[string]interface{}` 作为输入

### 4. 使用情况分析

**interfaces.Tool 使用者（20+文件）：**
- tools/tool.go - BaseTool实现
- tools/validator.go - 输入验证
- tools/tool_cache.go - 工具缓存
- tools/tool_wrapper.go - 工具包装
- tools/tool_runtime.go - 运行时
- builder/builder_test.go - 构建器测试
- 所有 tools/ 子包

**core/orchestrator.Tool 使用者：**
- core/orchestrator.go:179 - BaseOrchestrator.tools字段
- 未发现其他使用

**mcp/core.Tool 使用者：**
- mcp/tools/network.go - 网络工具实现
- mcp/ 包的其他工具实现

**示例Tool使用者：**
- examples/advanced/multi-agent-modular/agents/ - 仅示例代码

### 5. 项目约定

**命名约定：**
- 接口定义在 interfaces/ 包
- 基础实现在对应的包中（如 tools.BaseTool）
- 使用 `Invoke` 而非 `Execute` 作为执行方法名（interfaces.Tool）

**代码风格：**
- 接口方法有完整的 GoDoc 注释
- 使用 context.Context 作为第一个参数
- 错误处理使用包装错误（tools.ToolError）

**文件组织：**
- interfaces/ - 核心接口定义
- tools/ - 工具实现
- mcp/ - MCP协议实现（独立）
- examples/ - 示例代码

### 6. 统一策略

**保留：**
- `interfaces.Tool` - 作为项目标准Tool接口
- `interfaces.ValidatableTool` - 可选验证接口
- `mcp/core.Tool` - 重命名为 `mcp/core.MCPTool`（避免冲突）

**删除：**
- `core/orchestrator.Tool` - 完全删除
- `core/orchestrator.ToolInput` - 删除
- `core/orchestrator.ToolOutput` - 删除
- `core/orchestrator.ToolParameter` - 删除

**更新：**
- 修改 core/orchestrator.go 中的工具字段类型为 `interfaces.Tool`
- 修改 examples/ 中的Tool接口为 `interfaces.Tool`
- 所有引用 orchestrator.Tool 的地方改为 interfaces.Tool

**MCP包处理：**
- 将 `mcp/core.Tool` 重命名为 `mcp/core.MCPTool`
- 保持MCP包的独立性，不强制统一

### 7. 关键风险点

**并发问题：**
- 修改期间可能影响正在使用的代码
- 需确保所有引用都正确更新

**边界条件：**
- orchestrator 中的工具映射需要类型转换
- 确保没有遗漏的引用

**性能瓶颈：**
- 接口统一不应影响现有性能
- 保持工具缓存和包装器的兼容性

**安全考虑：**
- 无安全风险

### 8. 迁移步骤

1. 重命名 mcp/core.Tool → mcp/core.MCPTool（避免命名冲突）
2. 删除 core/orchestrator.go 中的Tool接口定义
3. 删除 core/orchestrator.go 中的相关类型定义
4. 更新 core/orchestrator.go 的导入和类型引用
5. 更新 examples/ 中的Tool接口引用
6. 运行测试验证
7. 运行构建验证

### 9. 测试策略

**测试文件：**
- tools/tools_test.go - 工具测试
- tools/validator_test.go - 验证器测试
- tools/tool_cache_test.go - 缓存测试
- builder/builder_test.go - 构建器测试

**测试覆盖：**
- 接口兼容性测试
- 类型转换测试
- 现有功能回归测试

**验证命令：**
```bash
go build ./...
go test ./interfaces/...
go test ./tools/...
go test ./core/...
go test ./mcp/...
```
