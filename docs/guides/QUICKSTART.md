# GoAgent 快速入门

## 简介

GoAgent 是一个全面的 Go AI Agent 框架。本指南将帮助你快速开始使用 GoAgent 构建智能 Agent。

## 前提条件

- Go 1.25.0 或更高版本
- LLM API 密钥（OpenAI、Gemini、DeepSeek 等）

## 安装

```bash
go get github.com/kart-io/goagent
```

## 基础示例

### 1. 简单 Agent

创建一个基本的对话 Agent：

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

func main() {
    // 创建 LLM 客户端
    client := providers.NewOpenAIClient(
        os.Getenv("OPENAI_API_KEY"),
        "gpt-4",
    )

    // 使用 Builder 创建 Agent
    agent := builder.NewAgentBuilder(client).
        WithSystemPrompt("你是一个有帮助的助手。").
        Build()

    // 创建输入
    input := &interfaces.Input{
        Messages: []interfaces.Message{
            {Role: "user", Content: "你好，请介绍一下自己。"},
        },
    }

    // 调用 Agent
    ctx := context.Background()
    output, err := agent.Invoke(ctx, input)
    if err != nil {
        fmt.Printf("错误: %v\n", err)
        return
    }

    // 输出结果
    for _, msg := range output.Messages {
        fmt.Printf("%s: %s\n", msg.Role, msg.Content)
    }
}
```

### 2. 带工具的 Agent

创建一个可以使用工具的 Agent：

```go
package main

import (
    "context"
    "fmt"
    "os"
    "time"

    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/llm/providers"
    "github.com/kart-io/goagent/tools"
)

// 创建自定义工具
type WeatherTool struct{}

func (t *WeatherTool) Name() string {
    return "get_weather"
}

func (t *WeatherTool) Description() string {
    return "获取指定城市的天气信息"
}

func (t *WeatherTool) ArgsSchema() string {
    return `{
        "type": "object",
        "properties": {
            "city": {"type": "string", "description": "城市名称"}
        },
        "required": ["city"]
    }`
}

func (t *WeatherTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
    city := input.Args["city"].(string)
    // 模拟天气查询
    weather := fmt.Sprintf("%s 今天晴，温度 25°C", city)
    return &interfaces.ToolOutput{
        Result:  weather,
        Success: true,
    }, nil
}

func main() {
    client := providers.NewOpenAIClient(
        os.Getenv("OPENAI_API_KEY"),
        "gpt-4",
    )

    // 创建带工具的 Agent
    agent := builder.NewAgentBuilder(client).
        WithSystemPrompt("你是一个天气助手。").
        WithTools(&WeatherTool{}).
        WithTimeout(30 * time.Second).
        Build()

    input := &interfaces.Input{
        Messages: []interfaces.Message{
            {Role: "user", Content: "北京今天天气怎么样？"},
        },
    }

    ctx := context.Background()
    output, err := agent.Invoke(ctx, input)
    if err != nil {
        fmt.Printf("错误: %v\n", err)
        return
    }

    for _, msg := range output.Messages {
        fmt.Printf("%s: %s\n", msg.Role, msg.Content)
    }
}
```

### 3. 带内存的 Agent

创建一个具有对话记忆的 Agent：

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/llm/providers"
    "github.com/kart-io/goagent/memory"
)

func main() {
    client := providers.NewOpenAIClient(
        os.Getenv("OPENAI_API_KEY"),
        "gpt-4",
    )

    // 创建内存管理器
    memManager := memory.NewDefaultManager()

    // 创建带内存的 Agent
    agent := builder.NewAgentBuilder(client).
        WithSystemPrompt("你是一个有帮助的助手。").
        WithMemory(memManager).
        Build()

    ctx := context.Background()
    sessionID := "session-001"

    // 第一轮对话
    input1 := &interfaces.Input{
        Messages: []interfaces.Message{
            {Role: "user", Content: "我叫小明。"},
        },
        State: interfaces.State{
            "session_id": sessionID,
        },
    }

    output1, _ := agent.Invoke(ctx, input1)
    fmt.Println("回复:", output1.Messages[0].Content)

    // 第二轮对话（Agent 应该记住用户名字）
    input2 := &interfaces.Input{
        Messages: []interfaces.Message{
            {Role: "user", Content: "你还记得我的名字吗？"},
        },
        State: interfaces.State{
            "session_id": sessionID,
        },
    }

    output2, _ := agent.Invoke(ctx, input2)
    fmt.Println("回复:", output2.Messages[0].Content)
}
```

### 4. ReAct Agent

使用 ReAct 推理模式：

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/llm/providers"
)

func main() {
    client := providers.NewOpenAIClient(
        os.Getenv("OPENAI_API_KEY"),
        "gpt-4",
    )

    // 创建 ReAct Agent
    agent := builder.NewAgentBuilder(client).
        WithReAct().
        WithTools(/* 添加工具 */).
        Build()

    input := &interfaces.Input{
        Messages: []interfaces.Message{
            {Role: "user", Content: "请帮我分析这个问题..."},
        },
    }

    ctx := context.Background()
    output, err := agent.Invoke(ctx, input)
    if err != nil {
        fmt.Printf("错误: %v\n", err)
        return
    }

    fmt.Println(output.Messages[0].Content)
}
```

### 5. 思维链 Agent

使用 Chain-of-Thought 推理：

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/llm/providers"
)

func main() {
    client := providers.NewOpenAIClient(
        os.Getenv("OPENAI_API_KEY"),
        "gpt-4",
    )

    // 创建零样本 CoT Agent
    agent := builder.NewAgentBuilder(client).
        WithZeroShotCoT().
        Build()

    input := &interfaces.Input{
        Messages: []interfaces.Message{
            {Role: "user", Content: "如果一个农场有 5 只鸡，每只鸡每天下 2 个蛋，一周能收集多少鸡蛋？"},
        },
    }

    ctx := context.Background()
    output, _ := agent.Invoke(ctx, input)
    fmt.Println(output.Messages[0].Content)
}
```

### 6. 流式输出

使用流式响应：

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/llm/providers"
)

func main() {
    client := providers.NewOpenAIClient(
        os.Getenv("OPENAI_API_KEY"),
        "gpt-4",
    )

    agent := builder.NewAgentBuilder(client).
        WithSystemPrompt("你是一个有帮助的助手。").
        Build()

    input := &interfaces.Input{
        Messages: []interfaces.Message{
            {Role: "user", Content: "请写一首关于春天的诗。"},
        },
    }

    ctx := context.Background()
    stream, err := agent.Stream(ctx, input)
    if err != nil {
        fmt.Printf("错误: %v\n", err)
        return
    }

    // 处理流式响应
    for chunk := range stream {
        if chunk.Done {
            fmt.Println("\n--- 完成 ---")
            break
        }
        fmt.Print(chunk.Content)
    }
}
```

## 使用不同的 LLM 提供商

### OpenAI

```go
client := providers.NewOpenAIClient(
    os.Getenv("OPENAI_API_KEY"),
    "gpt-4",
)
```

### Anthropic

```go
client := providers.NewAnthropicClient(
    os.Getenv("ANTHROPIC_API_KEY"),
    "claude-3-sonnet-20240229",
)
```

### DeepSeek

```go
client := providers.NewDeepSeekClient(
    os.Getenv("DEEPSEEK_API_KEY"),
    "deepseek-chat",
)
```

### Ollama（本地）

```go
client := providers.NewOllamaClient(
    "http://localhost:11434",
    "llama2",
)
```

更多提供商配置请参考 [LLM 提供商指南](LLM_PROVIDERS.md)。

## 项目结构建议

```text
my-agent-project/
├── cmd/
│   └── main.go           # 入口点
├── internal/
│   ├── agents/           # 自定义 Agent
│   ├── tools/            # 自定义工具
│   └── config/           # 配置
├── go.mod
└── README.md
```

## 环境变量

常用环境变量：

```bash
# LLM API 密钥
export OPENAI_API_KEY="your-api-key"
export ANTHROPIC_API_KEY="your-api-key"
export DEEPSEEK_API_KEY="your-api-key"

# 可选配置
export LOG_LEVEL="debug"
export LOG_FORMAT="json"
```

## 下一步

- 阅读 [架构概述](../architecture/ARCHITECTURE.md) 了解框架设计
- 查看 [LLM 提供商指南](LLM_PROVIDERS.md) 了解更多提供商配置
- 探索 `examples/` 目录中的更多示例
- 阅读 [测试最佳实践](../development/TESTING_BEST_PRACTICES.md)

## 常见问题

### Agent 没有使用工具

确保：

1. 工具已正确注册到 Agent
2. 系统提示中明确说明可以使用工具
3. LLM 模型支持函数调用

### 内存没有保存

确保：

1. 使用相同的 sessionID
2. MemoryManager 已正确配置
3. 调用 `AddConversation` 保存对话

### 流式输出不工作

确保：

1. LLM 客户端支持流式输出
2. 正确处理 channel 和 Done 标志

## 相关文档

- [架构概述](../architecture/ARCHITECTURE.md)
- [LLM 提供商指南](LLM_PROVIDERS.md)
- [测试最佳实践](../development/TESTING_BEST_PRACTICES.md)
