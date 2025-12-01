## 项目上下文摘要（ToolCall 统一任务）
生成时间：2025-12-01

### 1. ToolCall 定义分析

发现了 **7 处** ToolCall 定义，远超预期的 4 处：

**1. interfaces/tool.go:158-192（规范定义，保留）**
```go
type ToolCall struct {
    ID string                      // 唯一标识符
    ToolName string                // 工具名称
    Args map[string]interface{}    // 参数
    Result *ToolOutput             // 结果
    Error string                   // 错误信息
    StartTime int64                // 开始时间戳
    EndTime int64                  // 结束时间戳
    Metadata map[string]interface{} // 元数据
}
```
- **用途**：完整的工具调用记录，用于日志、审计、调试
- **特点**：包含完整的执行上下文（时间、元数据、结果）
- **评估**：这是最全面的定义，应作为规范

**2. llm/tools.go:20-27（待删除）**
```go
type ToolCall struct {
    ID string
    Type string               // "function"
    Function struct {
        Name string
        Arguments string      // JSON string
    }
}
```
- **用途**：LLM 响应中的工具调用格式
- **特点**：Arguments 是 JSON 字符串，不是 map
- **问题**：与 interfaces.ToolCall 结构不同，需要转换函数

**3. llm/common/types.go:20-29（待删除）**
```go
type ToolCall struct {
    ID string
    Type string
    Name string
    Arguments map[string]interface{}
    Function *struct {              // 可选
        Name string
        Arguments string            // JSON string
    }
}
```
- **用途**：LLM common 包的工具调用定义
- **特点**：同时有 Name/Arguments 和 Function 字段，结构混乱
- **问题**：与 llm/tools.go 重复，结构不一致

**4. core/agent.go:126-133（待删除）**
```go
type ToolCall struct {
    ToolName string
    Input map[string]interface{}
    Output interface{}
    Duration time.Duration
    Success bool
    Error string
}
```
- **用途**：Agent 执行过程中的工具调用记录
- **特点**：包含执行统计（Duration, Success）
- **问题**：缺少 ID、时间戳等关键字段

**5. builder/builder.go:421-424（待删除）**
```go
type ToolCall struct {
    Name string
    Input map[string]interface{}
}
```
- **用途**：简化的工具调用表示，用于构建器
- **特点**：最简化的结构，只有名称和输入
- **问题**：信息不完整，无法追踪执行状态

**6. mcp/core/tool.go:129-150（MCP 协议专用，保留）**
```go
type ToolCall struct {
    ID string
    ToolName string
    Input map[string]interface{}
    Context map[string]interface{}
    Timestamp time.Time
    SessionID string
    UserID string
}
```
- **用途**：MCP (Model Context Protocol) 工具调用请求
- **特点**：包含协议所需的上下文信息（SessionID, UserID）
- **评估**：这是 MCP 协议的一部分，应保留独立定义

**7. tools/executor_tool.go:31-43（工具执行器专用，保留）**
```go
type ToolCall struct {
    Tool interfaces.Tool              // 工具实例
    Input *interfaces.ToolInput       // 输入
    ID string                         // 调用标识符
    Dependencies []string             // 依赖关系
}
```
- **用途**：并发工具执行器的调用描述
- **特点**：包含实际的 Tool 对象和依赖关系
- **评估**：用于执行编排，与记录型的 ToolCall 用途不同，应保留

### 2. 统一策略

**保留的定义（3个）：**
1. **interfaces/tool.go** - 规范定义，用于日志、审计
2. **mcp/core/tool.go** - MCP 协议定义，不可修改
3. **tools/executor_tool.go** - 执行器定义，用于编排

**需要删除的定义（4个）：**
1. **llm/tools.go** - 与 common/types.go 重复
2. **llm/common/types.go** - 结构混乱
3. **core/agent.go** - 可用 interfaces.ToolCall 替代
4. **builder/builder.go** - 可用 interfaces.ToolCall 替代

### 3. 迁移方案

**方案A：统一到 interfaces.ToolCall（推荐）**
- llm 包创建转换函数：LLMToolCall -> interfaces.ToolCall
- core/agent.go 直接使用 interfaces.ToolCall
- builder 直接使用 interfaces.ToolCall

**方案B：保留 LLM 专用定义**
- llm 包统一到 llm/common/types.go
- 删除 llm/tools.go
- 其他包使用 interfaces.ToolCall

**选择：方案A**
- 理由：减少重复定义，统一到规范接口
- 成本：需要在 LLM provider 中添加转换逻辑

### 4. 依赖和集成点

**interfaces.ToolCall 的使用者：**
- 未明确使用（被其他重复定义遮蔽）

**llm.ToolCall 的使用者：**
- llm/providers/openai*.go
- llm/providers/comprehensive_test.go
- 需要查找所有 LLM provider

**core.ToolCall 的使用者：**
- core/agent.go 的 AgentOutput.ToolCalls
- agents/base/reasoning_agent.go
- agents/cot/cot.go

**builder.ToolCall 的使用者：**
- builder/builder.go 的 extractToolCalls 和 executeToolCall

### 5. 技术选型理由

**为什么需要统一：**
- 当前有 4 个重复定义，结构不一致
- 导致类型转换复杂，容易出错
- 违反 DRY 原则

**为什么保留 3 个定义：**
- interfaces.ToolCall：规范定义，完整的记录结构
- mcp/core.ToolCall：外部协议要求，不能修改
- tools/executor_tool.ToolCall：执行编排需要，包含 Tool 对象

### 6. 关键风险点

**并发问题：**
- interfaces.ToolCall 包含 map 字段，需要注意并发读写

**边界条件：**
- LLM 响应的 Arguments 是 JSON 字符串，需要解析
- 转换失败时的错误处理

**性能瓶颈：**
- JSON 解析和序列化开销
- 建议在转换层缓存解析结果

**破坏性变更：**
- core.AgentOutput.ToolCalls 字段类型改变
- 所有引用该字段的代码需要更新

### 7. 实施步骤

**步骤 1：创建 LLM 转换函数**
- 在 llm/common 包添加 ToInterfaceToolCall() 转换函数

**步骤 2：更新 LLM providers**
- 查找所有使用 llm.ToolCall 的 provider
- 改为使用 llm/common.ToolCall 和转换函数

**步骤 3：更新 core/agent.go**
- 删除 ToolCall 定义
- 导入 interfaces.ToolCall
- 更新 AgentOutput.ToolCalls 字段类型

**步骤 4：更新 builder/builder.go**
- 删除 ToolCall 定义
- 改为使用 interfaces.ToolCall
- 更新 extractToolCalls 和 executeToolCall 方法

**步骤 5：删除重复定义**
- 删除 llm/tools.go 的 ToolCall
- 删除 llm/common/types.go 的 ToolCall

**步骤 6：验证编译**
- go build ./...
- 确保所有包能编译通过

**步骤 7：运行测试**
- go test ./...
- 确保所有测试通过
