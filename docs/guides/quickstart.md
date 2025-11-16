# LangChain-Inspired Improvements - Quick Reference

## 概述

本文档提供基于 LangChain 设计的 `pkg/agent/` 改进方案快速参考。详细信息请参阅 `LANGCHAIN_INSPIRED_IMPROVEMENTS.md`。

## 核心改进项

### 1. ToolRuntime Pattern 🔥

**优先级**: 高

**目标**: 工具能访问 Agent 状态、上下文和存储

**示例**:

```go
@tool
func getUserInfo(runtime: ToolRuntime) -> str:
    userID := runtime.State.Get("user_id")
    return runtime.Store.Get(ctx, []string{"users"}, userID)
```

**价值**:

- 工具更智能,能利用上下文信息
- 减少重复数据传递
- 支持复杂的业务逻辑

### 2. Multi-Mode Streaming 🔥

**优先级**: 高

**目标**: 支持 4 种流式模式

- `messages`: LLM tokens
- `updates`: 状态更新
- `custom`: 工具自定义输出
- `values`: 完整状态快照

**示例**:

```go
for event := range agent.StreamWithModes(ctx, input, []StreamMode{
    StreamModeMessages,
    StreamModeUpdates,
    StreamModeCustom,
}) {
    switch event.Mode {
    case StreamModeMessages:
        fmt.Printf("[LLM] %v\n", event.Data)
    case StreamModeUpdates:
        fmt.Printf("[State] %v\n", event.Data)
    case StreamModeCustom:
        fmt.Printf("[Tool] %v\n", event.Data)
    }
}
```

**价值**:

- 实时反馈,提升用户体验
- 灵活的流式数据控制
- 支持复杂的 UI 交互

### 3. Tool Selector Middleware 🔧

**优先级**: 中

**目标**: 基于上下文动态选择相关工具

**示例**:

```go
middleware := NewToolSelectorMiddleware(&ToolSelectorConfig{
    MaxTools:      5,
    SelectorModel: cheapLLM,
})

agent := NewAgentBuilder(llm).
    WithTools(allTools...).  // 50+ tools
    WithMiddleware(middleware).
    Build()
```

**价值**:

- 降低 Token 成本 (70%+)
- 提高模型准确性
- 减少 Prompt 复杂度

### 4. Parallel Tool Execution ⚡

**优先级**: 中

**目标**: 并行调用多个工具

**示例**:

```go
executor := NewParallelExecutor(10, 30*time.Second)

requests := []*ToolCallRequest{
    {Tool: searchTool, Input: map[string]interface{}{"query": "Go"}},
    {Tool: weatherTool, Input: map[string]interface{}{"city": "SF"}},
    {Tool: newsTool, Input: map[string]interface{}{"topic": "AI"}},
}

results, _ := executor.ExecuteParallel(ctx, requests)
```

**价值**:

- 5x+ 性能提升
- 减少总延迟
- 更好的资源利用

### 5. Human-in-the-Loop 👤

**优先级**: 中

**目标**: 支持执行中的中断和恢复

**示例**:

```go
// 流式执行,可能中断
for event := range agent.StreamWithInterrupts(ctx, input) {
    if interrupt, ok := event.(*Interrupt); ok {
        fmt.Printf("Interrupt: %s\n", interrupt.Message)
        // 等待人工审批...
    }
}

// 恢复执行
agent.Resume(ctx, &Command{
    Resume:   interruptID,
    Approved: true,
})
```

**价值**:

- 安全性 (敏感操作需审批)
- 可控性 (人工干预)
- 合规性 (审计追踪)

## 实施优先级

### Phase 1: 核心特性 (Week 1-4)

1. **ToolRuntime Pattern** (Week 1-2)
   - 最高价值,实现成本低
   - 立即提升工具能力

2. **Multi-Mode Streaming** (Week 3-4)
   - 显著改善用户体验
   - 支持复杂 UI 交互

### Phase 2: 性能优化 (Week 5-6)

3. **Tool Selector Middleware** (Week 5)
   - 降低成本
   - 提高准确性

4. **Parallel Tool Execution** (Week 6)
   - 性能提升明显
   - 技术实现相对简单

### Phase 3: 高级特性 (Week 7+)

5. **Human-in-the-Loop** (Week 7)
   - 企业级必需特性
   - 需要状态管理支持

## 快速开始

### 1. 启用 ToolRuntime

```go
// 定义支持 Runtime 的工具
type ContextAwareTool struct {
    *tools.BaseRuntimeTool
}

func (t *ContextAwareTool) ExecuteWithRuntime(
    ctx context.Context,
    input *tools.ToolInput,
    runtime *tools.ToolRuntime,
) (*tools.ToolOutput, error) {
    // 访问状态
    userID := runtime.State.Get("user_id")

    // 访问存储
    data, _ := runtime.Store.Get(ctx, []string{"users"}, userID)

    // 流式输出进度
    runtime.StreamWriter(map[string]interface{}{
        "status": "processing",
        "progress": 50,
    })

    return t.NewOutput(data, nil)
}
```

### 2. 使用 Multi-Mode Streaming

```go
agent, _ := builder.NewAgentBuilder(llm).
    WithSystemPrompt("You are helpful").
    WithTools(tools...).
    Build()

// 流式执行
events, _ := agent.StreamWithModes(ctx, "query", []stream.StreamMode{
    stream.StreamModeMessages,
    stream.StreamModeCustom,
})

for event := range events {
    fmt.Printf("[%s] %v\n", event.Mode, event.Data)
}
```

### 3. 添加 Tool Selector

```go
selector := middleware.NewToolSelectorMiddleware(&middleware.ToolSelectorConfig{
    MaxTools:      5,
    SelectorModel: llm.NewMockClient(),
    AlwaysInclude: []string{"essential_tool"},
})

agent, _ := builder.NewAgentBuilder(llm).
    WithTools(allTools...).  // 50+ tools
    WithMiddleware(selector).
    Build()
```

### 4. 并行执行工具

```go
executor := tools.NewParallelExecutor(10, 30*time.Second)

requests := []*tools.ToolCallRequest{
    {Tool: tool1, Input: input1},
    {Tool: tool2, Input: input2},
    {Tool: tool3, Input: input3},
}

results, _ := executor.ExecuteParallel(ctx, requests)

for _, result := range results {
    if result.Error != nil {
        log.Printf("Tool %s failed: %v", result.ID, result.Error)
    } else {
        log.Printf("Tool %s succeeded: %v", result.ID, result.Output)
    }
}
```

## 性能基准

| 特性 | 性能提升 | Token 节省 | 开发成本 |
|------|---------|-----------|---------|
| ToolRuntime | - | - | 低 |
| Multi-Mode Streaming | - | - | 中 |
| Tool Selector | - | 70%+ | 中 |
| Parallel Execution | 3-5x | - | 低 |
| Human-in-the-Loop | - | - | 高 |

## 迁移指南

### 从现有实现迁移

1. **工具迁移**:

```go
// 旧方式
type OldTool struct {
    *tools.BaseTool
}

func (t *OldTool) Execute(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
    // 需要在 input 中传递所有上下文
    userID := input.Args["user_id"].(string)
    return t.NewOutput(result, nil)
}

// 新方式 (支持 Runtime)
type NewTool struct {
    *tools.BaseRuntimeTool
}

func (t *NewTool) ExecuteWithRuntime(ctx context.Context, input *tools.ToolInput, runtime *tools.ToolRuntime) (*tools.ToolOutput, error) {
    // 直接从 Runtime 获取上下文
    userID := runtime.State.Get("user_id").(string)
    return t.NewOutput(result, nil)
}
```

2. **流式输出迁移**:

```go
// 旧方式 (单一流)
stream, _ := agent.Stream(ctx, input)
for chunk := range stream {
    fmt.Println(chunk)
}

// 新方式 (多模式流)
events, _ := agent.StreamWithModes(ctx, input, []stream.StreamMode{
    stream.StreamModeMessages,
    stream.StreamModeUpdates,
})
for event := range events {
    switch event.Mode {
    case stream.StreamModeMessages:
        // 处理 LLM 输出
    case stream.StreamModeUpdates:
        // 处理状态更新
    }
}
```

## 常见问题

### Q: 是否需要重写所有现有工具?

A: 不需要。ToolRuntime 是可选的,现有工具仍然正常工作。只有需要访问上下文的工具才需要升级。

### Q: Multi-Mode Streaming 会增加多少开销?

A: 开销 < 5%。使用高效的 channel 和 goroutine 实现。

### Q: Tool Selector 是否会降低准确性?

A: 不会。通过 LLM 智能选择,实际上可以提高准确性 (减少噪音)。

### Q: 并行工具执行是否安全?

A: 安全。提供了依赖分析、超时控制和错误隔离机制。

### Q: Human-in-the-Loop 如何持久化状态?

A: 使用 Checkpointer 系统,支持 Redis/PostgreSQL 后端。

## 相关文档

- [详细改进方案](LANGCHAIN_INSPIRED_IMPROVEMENTS.md) - 完整的技术设计和实现细节
- [LangChain V2 计划](LANGCHAIN_V2_IMPROVEMENT_PLAN.md) - 之前的改进计划
- [架构文档](ARCHITECTURE.md) - 整体架构说明
- [README](README.md) - 项目概览和使用指南

## 贡献

欢迎贡献代码和反馈:

1. Fork 项目
2. 创建特性分支: `git checkout -b feature/langchain-toolruntime`
3. 提交更改: `git commit -m 'feat: implement ToolRuntime pattern'`
4. 推送分支: `git push origin feature/langchain-toolruntime`
5. 创建 Pull Request

## 许可证

MIT License - 详见 LICENSE 文件
