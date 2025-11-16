# ReAct Agent Implementation

完整的 ReAct (Reasoning + Acting) Agent 实现，这是 LangChain 中最重要的 Agent 类型之一。

## 概述

ReAct Agent 通过 **思考-行动-观察** 循环来解决问题：

1. **Thought**: 分析当前情况，决定下一步做什么
2. **Action**: 选择并执行一个工具
3. **Observation**: 观察工具执行结果
4. 重复上述过程直到得出最终答案

## 架构

```
┌─────────────────────────────────────────┐
│          ReAct Agent                     │
├─────────────────────────────────────────┤
│                                          │
│  ┌──────────┐       ┌──────────┐       │
│  │   LLM    │◄─────►│  Parser  │       │
│  └──────────┘       └──────────┘       │
│       ▲                   ▲             │
│       │                   │             │
│       ▼                   ▼             │
│  ┌──────────────────────────────┐      │
│  │      Reasoning Loop           │      │
│  │  1. Thought                    │      │
│  │  2. Action (Tool Selection)    │      │
│  │  3. Observation (Tool Result)  │      │
│  └──────────────────────────────┘      │
│       │                                 │
│       ▼                                 │
│  ┌──────────┐                          │
│  │  Tools   │                          │
│  └──────────┘                          │
│                                          │
└─────────────────────────────────────────┘
```

## 特性

### ReAct Agent

- ✅ 完整的 ReAct 循环实现
- ✅ LLM 集成（支持任何 LLM 客户端）
- ✅ 工具调用和管理
- ✅ 智能输出解析
- ✅ 最大步数限制
- ✅ 早停机制
- ✅ 详细的思维链记录
- ✅ 完善的错误处理

### Agent Executor

- ✅ 记忆管理集成
- ✅ 对话历史支持
- ✅ 执行超时控制
- ✅ 批量执行
- ✅ 流式输出
- ✅ 回调系统集成
- ✅ 中间步骤返回

### Output Parser

- ✅ ReAct 格式解析
- ✅ JSON 输出解析
- ✅ 结构化输出解析
- ✅ 列表输出解析
- ✅ 枚举输出解析
- ✅ 布尔输出解析
- ✅ 链式解析器

## 快速开始

### 基本用法

```go
package main

import (
    "context"
    "github.com/kart-io/goagent/agents"
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/tools"
)

func main() {
    // 1. 创建 LLM 客户端
    llmClient := createYourLLMClient()

    // 2. 创建工具
    calculatorTool := createCalculatorTool()
    searchTool := createSearchTool()

    // 3. 创建 ReAct Agent
    agent := agents.NewReActAgent(agents.ReActConfig{
        Name:        "MyAgent",
        Description: "A helpful assistant",
        LLM:         llmClient,
        Tools:       []tools.Tool{calculatorTool, searchTool},
        MaxSteps:    10,
    })

    // 4. 执行任务
    ctx := context.Background()
    input := &core.AgentInput{
        Task: "What is 15 * 7 + 23?",
    }

    output, err := agent.Invoke(ctx, input)
    if err != nil {
        panic(err)
    }

    fmt.Printf("Result: %v\n", output.Result)
}
```

### 使用 Agent Executor

```go
// 创建执行器（带记忆和高级功能）
executor := agents.NewAgentExecutor(agents.ExecutorConfig{
    Agent:             agent,
    Tools:             tools,
    Memory:            memorySystem,
    MaxIterations:     15,
    MaxExecutionTime:  5 * time.Minute,
    ReturnIntermSteps: true,
    Verbose:           true,
})

// 简单执行
result, err := executor.Run(ctx, "What's the weather in Beijing?")

// 或使用完整输入
output, err := executor.Execute(ctx, &core.AgentInput{
    Task:      "Complex task",
    SessionID: "user-123",
})
```

### 使用回调

```go
// 创建回调
callback := &MyCallback{}

// 带回调执行
output, err := executor.ExecuteWithCallbacks(ctx, input, callback)
```

### 流式执行

```go
// 获取流式输出
stream, err := executor.Stream(ctx, input)
if err != nil {
    panic(err)
}

// 处理流
for chunk := range stream {
    if chunk.Error != nil {
        log.Printf("Error: %v", chunk.Error)
        break
    }

    fmt.Printf("Status: %s\n", chunk.Data.Status)

    if chunk.Done {
        fmt.Printf("Final result: %v\n", chunk.Data.Result)
    }
}
```

## 创建自定义工具

```go
func createCustomTool() tools.Tool {
    return tools.NewBaseTool(
        "my_tool",
        "Description of what this tool does",
        `{
            "type": "object",
            "properties": {
                "param1": {
                    "type": "string",
                    "description": "First parameter"
                }
            },
            "required": ["param1"]
        }`,
        func(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
            param1 := input.Args["param1"].(string)

            // 执行工具逻辑
            result := doSomething(param1)

            return &tools.ToolOutput{
                Result:  result,
                Success: true,
            }, nil
        },
    )
}
```

## ReAct Prompt 格式

默认的 ReAct prompt 格式如下：

```
Answer the following questions as best you can. You have access to the following tools:

- calculator: Useful for mathematical calculations
- search: Search for information on the internet

Use the following format:

Thought: you should always think about what to do
Action: the action to take, should be one of [calculator, search]
Action Input: the input to the action
Observation: the result of the action
... (this Thought/Action/Action Input/Observation can repeat N times)
Thought: I now know the final answer
Final Answer: the final answer to the original input question

Begin!

Question: What is 15 * 7?
Thought:
```

### 自定义 Prompt

```go
agent := agents.NewReActAgent(agents.ReActConfig{
    Name:  "CustomAgent",
    LLM:   llmClient,
    Tools: tools,

    // 自定义 prompt 前缀
    PromptPrefix: `You are a helpful assistant...`,

    // 自定义 prompt 后缀
    PromptSuffix: `Task: {input}\nLet's think step by step.`,

    // 自定义格式说明
    FormatInstr: `Your custom format instructions...`,
})
```

## 配置选项

### ReActConfig

| 字段         | 类型         | 说明         | 默认值            |
| ------------ | ------------ | ------------ | ----------------- |
| Name         | string       | Agent 名称   | 必需              |
| Description  | string       | Agent 描述   | 必需              |
| LLM          | llm.Client   | LLM 客户端   | 必需              |
| Tools        | []tools.Tool | 可用工具列表 | 必需              |
| MaxSteps     | int          | 最大步数     | 10                |
| StopPattern  | []string     | 停止模式     | ["Final Answer:"] |
| PromptPrefix | string       | Prompt 前缀  | 默认 ReAct 格式   |
| PromptSuffix | string       | Prompt 后缀  | 默认格式          |
| FormatInstr  | string       | 格式说明     | 默认说明          |

### ExecutorConfig

| 字段                | 类型          | 说明         | 默认值  |
| ------------------- | ------------- | ------------ | ------- |
| Agent               | core.Agent    | Agent 实例   | 必需    |
| Tools               | []tools.Tool  | 工具列表     | 可选    |
| Memory              | memory.Memory | 记忆系统     | nil     |
| MaxIterations       | int           | 最大迭代次数 | 15      |
| MaxExecutionTime    | time.Duration | 最大执行时间 | 5 分钟  |
| EarlyStoppingMethod | string        | 早停方法     | "force" |
| HandleParsingErrors | bool          | 处理解析错误 | false   |
| ReturnIntermSteps   | bool          | 返回中间步骤 | false   |
| Verbose             | bool          | 详细输出     | false   |

## 输出结构

```go
type AgentOutput struct {
    // 执行结果
    Result  interface{} // 最终结果
    Status  string      // "success", "failed", "partial"
    Message string      // 结果消息

    // 推理过程
    ReasoningSteps []ReasoningStep // 推理步骤
    ToolCalls      []ToolCall      // 工具调用记录

    // 元数据
    Latency   time.Duration          // 执行延迟
    Timestamp time.Time              // 时间戳
    Metadata  map[string]interface{} // 额外元数据
}
```

## 示例

完整示例见 `examples/react_example/main.go`：

```bash
cd examples/react_example
go run main.go
```

输出示例：

```
=== ReAct Agent Example ===

[LLM START] Model: , Prompts: 1
[LLM END] Tokens: 30
Output: Thought: I need to search for the current weather in Beijing
Action: weather
Action Input: {"city": "Beijing"}

[TOOL START] weather
  Input: map[city:Beijing]
[TOOL END] weather
  Output: map[city:Beijing condition:sunny humidity:60 temperature:25 unit:celsius]

...

=== Execution Result ===
Status: success
Result: The current weather in Beijing is 25°C (77°F) with sunny skies.
Latency: 15.234ms
Steps: 6
Tool Calls: 2

=== Reasoning Steps ===
1. [Thought] I need to search for the current weather in Beijing ->  (1.23ms)
2. [Action] Tool: weather -> map[city:Beijing ...] (8.45ms)
...

=== Tool Calls ===
1. weather [SUCCESS] (8.45ms)
   Input: map[city:Beijing]
   Output: map[city:Beijing condition:sunny ...]
2. calculator [SUCCESS] (2.13ms)
   Input: map[expression:25 * 9/5 + 32]
   Output: 77
```

## 测试

运行测试：

```bash
cd agents
go test -v
```

运行基准测试：

```bash
go test -bench=. -benchmem
```

## 最佳实践

### 1. 工具设计

- 每个工具应该有单一职责
- 提供清晰的描述和 JSON Schema
- 返回结构化的结果
- 妥善处理错误

### 2. Prompt 工程

- 清晰地描述每个工具的用途
- 提供具体的使用示例
- 设置合理的停止模式
- 根据任务调整格式说明

### 3. 性能优化

- 设置合理的 MaxSteps 避免无限循环
- 使用 MaxExecutionTime 防止超时
- 启用 EarlyStoppingMethod
- 考虑使用流式输出

### 4. 错误处理

- 实现自定义回调监控执行
- 启用 HandleParsingErrors
- 检查工具调用结果
- 设置合理的重试策略

### 5. 记忆管理

- 使用 Memory 系统保持上下文
- 定期清理旧的对话历史
- 控制历史长度避免 token 超限

## 常见问题

### Q: Agent 陷入循环怎么办？

A: 设置合理的 `MaxSteps` 和 `MaxExecutionTime`：

```go
agent := agents.NewReActAgent(agents.ReActConfig{
    MaxSteps: 5, // 限制最大步数
    // ...
})

executor := agents.NewAgentExecutor(agents.ExecutorConfig{
    MaxIterations:    10,
    MaxExecutionTime: 30 * time.Second,
    // ...
})
```

### Q: 如何处理解析错误？

A: 启用解析错误处理并提供清晰的格式说明：

```go
executor := agents.NewAgentExecutor(agents.ExecutorConfig{
    HandleParsingErrors: true,
    // ...
})
```

### Q: 如何调试 Agent 执行过程？

A: 使用 Callback 系统：

```go
type DebugCallback struct {
    core.BaseCallback
}

func (d *DebugCallback) OnLLMEnd(ctx context.Context, output string, tokens int) error {
    fmt.Printf("LLM Output:\n%s\n", output)
    return nil
}

func (d *DebugCallback) OnToolEnd(ctx context.Context, toolName string, output interface{}) error {
    fmt.Printf("Tool %s returned: %v\n", toolName, output)
    return nil
}

// 使用
executor.ExecuteWithCallbacks(ctx, input, &DebugCallback{})
```

### Q: 如何与现有系统集成？

A: 实现自定义 Tool 接口：

```go
type MySystemTool struct {
    client *MySystemClient
}

func (m *MySystemTool) Invoke(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
    // 调用现有系统
    result := m.client.DoSomething(input.Args)
    return &tools.ToolOutput{Result: result, Success: true}, nil
}
```

## 扩展

### 添加新的 Agent 类型

可以基于 `core.BaseAgent` 创建新的 Agent 类型：

```go
type MyCustomAgent struct {
    *core.BaseAgent
    // 自定义字段
}

func (a *MyCustomAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
    // 自定义实现
}
```

### 自定义 Parser

实现 `parsers.OutputParser` 接口：

```go
type MyParser struct {
    *parsers.BaseOutputParser[MyOutput]
}

func (p *MyParser) Parse(ctx context.Context, text string) (MyOutput, error) {
    // 自定义解析逻辑
}
```

## 参考

- [LangChain ReAct](https://python.langchain.com/docs/modules/agents/agent_types/react)
- [ReAct Paper](https://arxiv.org/abs/2210.03629)
- [Tool Calling Best Practices](https://platform.openai.com/docs/guides/function-calling)

## License

MIT License
