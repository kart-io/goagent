# ToolCall 统一任务 - 中期报告

## 任务进度

### 已完成（50%）

1. ✅ core/agent.go
   - 删除 ToolCall 定义
   - 更新 AgentOutput.ToolCalls: []ToolCall → []*interfaces.ToolCall

2. ✅ llm/capabilities.go
   - 使用 llm/common.ToolCallResponse 和 llm/common.ToolChunk

3. ✅ agents/cot/cot.go
   - 更新工具调用构造为 interfaces.ToolCall

4. ✅ performance/cache_pool.go
   - 更新 ToolCalls 类型

### 待完成（50%）

需要更新以下文件：
- agents/base/reasoning_agent.go
- agents/react/react.go
- agents/pot/pot.go
- agents/specialized/*.go (4个文件)
- performance/object_pool.go
- observability/logging.go
- llm/tools.go (需要真正废弃)
- builder/builder.go (需要删除 ToolCall 定义)

### 关键问题

**字段不兼容**：
- core.ToolCall 有：Duration, Success, Input, Output
- interfaces.ToolCall 有：StartTime, EndTime, Args, Result

observability/logging.go 依赖 Duration 和 Success 字段。

## 编译错误

```

# github.com/kart-io/goagent/tools
tools/executor_tool.go:399:28: undefined: agentErrors.Error
tools/executor_tool.go:406:16: undefined: agentErrors.CodeNetworkError
tools/executor_tool.go:407:16: undefined: agentErrors.CodeServiceUnavailable
