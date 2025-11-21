# DeepSeek 简单示例

这是一个使用 DeepSeek LLM 的简单 GoAgent 示例，演示了最基本的使用方法。

## 功能特点

- **基础对话**: 使用 DeepSeek Provider 进行简单的对话交互
- **Agent 构建**: 使用 Builder 模式创建配置化的 Agent
- **结构化输出**: 演示如何让 DeepSeek 生成 JSON 等结构化数据
- **错误处理**: 展示基本的错误处理流程
- **Token 统计**: 显示 API 调用的 Token 使用情况

## 前置要求

1. Go 1.25.0 或更高版本
2. DeepSeek API Key

## 获取 API Key

访问 [DeepSeek Platform](https://platform.deepseek.com/) 注册并获取 API Key。

## 配置步骤

### 1. 设置环境变量

```bash
export DEEPSEEK_API_KEY=your-deepseek-api-key
```

### 2. 安装依赖

```bash
cd /home/hellotalk/code/go/src/github.com/kart-io/goagent
go mod tidy
```

### 3. 运行示例

```bash
cd examples/basic/09-deepseek-simple
go run main.go
```

## 代码说明

### 示例 1: Provider 直接对话

```go
// 创建 DeepSeek 配置
config := &llm.Config{
    APIKey:      apiKey,
    Model:       "deepseek-chat",
    Temperature: 0.7,
    MaxTokens:   1000,
    Timeout:     30,
}

// 创建 Provider
client, err := providers.NewDeepSeek(config)

// 发送消息
response, err := client.Chat(ctx, messages)
```

这种方式直接使用 DeepSeek Provider，适合简单的对话场景。

### 示例 2: Builder 构建 Agent

```go
// 使用 Builder 构建 Agent（需要指定泛型参数）
agent, err := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
    WithSystemPrompt("你是一个专业的 Go 语言顾问").
    WithMetadata("name", "Go-Advisor").
    WithMetadata("description", "Go 语言专业顾问").
    Build()

// 执行任务
output, err := agent.Execute(ctx, "请列举 Go 语言的三个主要特点。")
```

这种方式使用 Builder 模式，适合需要更多配置和功能的场景。注意：
- 必须显式指定泛型参数 `[any, *agentcore.AgentState]`
- 使用 `Execute` 方法执行任务
- 使用 `WithMetadata` 设置元数据

### 示例 3: 输出结构化数据

```go
// 使用 Builder 构建 Agent，配置为输出 JSON 格式
agent, err := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
    WithSystemPrompt(`你是一个数据分析助手。你的任务是根据用户的要求，生成结构化的 JSON 数据。
请确保输出的是有效的 JSON 格式，不要包含任何其他文字说明。`).
    WithMetadata("name", "DataGenerator").
    Build()

// 执行任务，生成结构化数据
task := `请生成一个包含 3 个用户信息的 JSON 数组...`
output, err := agent.Execute(ctx, task)
```

这种方式展示如何让 DeepSeek 输出结构化的 JSON 数据，适合：
- 数据生成场景
- API 接口开发
- 测试数据准备
- 结构化内容生成

**关键技巧**：
- 使用较低的 `Temperature` (如 0.3) 获得更稳定的输出
- 在系统提示中明确要求输出格式
- 在任务描述中详细说明数据结构

## 配置选项

### DeepSeek 配置参数

| 参数 | 类型 | 说明 | 默认值 |
|------|------|------|--------|
| `APIKey` | string | DeepSeek API Key | 必填 |
| `Model` | string | 模型名称 | `deepseek-chat` |
| `Temperature` | float64 | 温度参数 (0-1) | `0.7` |
| `MaxTokens` | int | 最大 Token 数 | `2000` |
| `Timeout` | int | 超时时间（秒） | `30` |

### 可用模型

- `deepseek-chat`: 通用对话模型
- `deepseek-coder`: 代码生成和理解模型

## 输出示例

```text
GoAgent + DeepSeek 简单示例
==============================

示例 1: DeepSeek Provider 基础对话
-----------------------------------
发送请求到 DeepSeek...

回复: AI Agent 是一种能够感知环境、自主决策并执行任务的智能程序。

Token 使用统计:
  - 输入 Tokens: 45
  - 输出 Tokens: 28
  - 总计 Tokens: 73
  - 模型: deepseek-chat

示例 2: 使用 Builder 创建 DeepSeek Agent
--------------------------------------
任务: 请列举 Go 语言的三个主要特点。

Agent 正在思考...

结果:
1. 并发性强：内置 goroutine 和 channel
2. 编译速度快：静态编译，生成独立可执行文件
3. 简洁高效：语法简单，性能接近 C/C++

执行时间: 1.234s

示例 3: 输出结构化数据（JSON 格式）
-------------------------------------
任务: 生成结构化用户数据（JSON 格式）

Agent 正在生成数据...

生成的 JSON 数据:
------------------
[
  {
    "id": 1,
    "name": "张三",
    "email": "zhangsan@example.com",
    "age": 28,
    "skills": ["Go", "Python", "Docker"]
  },
  {
    "id": 2,
    "name": "李四",
    "email": "lisi@example.com",
    "age": 32,
    "skills": ["Java", "Spring", "Kubernetes"]
  },
  {
    "id": 3,
    "name": "王五",
    "email": "wangwu@example.com",
    "age": 25,
    "skills": ["JavaScript", "React", "Node.js"]
  }
]

执行时间: 1.567s

示例 3.2: 生成产品信息
------------------------
Agent 正在生成产品数据...

生成的产品 JSON 数据:
---------------------
{
  "product_id": "PROD-2024-001",
  "name": "智能手表",
  "description": "高性能运动智能手表，支持心率监测、GPS 定位等功能",
  "price": 1299.99,
  "category": "智能穿戴",
  "tags": ["智能手表", "运动", "健康监测"],
  "in_stock": true,
  "specifications": {
    "weight": "45g",
    "dimensions": "44mm x 38mm x 10.7mm",
    "color": "深空灰",
    "battery_life": "18小时",
    "water_resistance": "50米防水"
  }
}

执行时间: 1.823s
```

## 常见问题

### 1. API Key 错误

```text
错误: 请设置 DEEPSEEK_API_KEY 环境变量
```

**解决方案**: 确保已设置环境变量

```bash
export DEEPSEEK_API_KEY=your-api-key
```

### 2. 网络连接问题

```text
LLM 请求失败: context deadline exceeded
```

**解决方案**:
- 检查网络连接
- 增加 Timeout 配置
- 检查代理设置

### 3. API 配额超限

```text
API returned status 429: Rate limit exceeded
```

**解决方案**:
- 检查 API 使用配额
- 添加请求重试逻辑
- 使用速率限制中间件

## 下一步

学习更多功能，请参考：

- **InvokeFast 性能优化**: [invokefast](./invokefast) - 自动提升 Agent 性能
- **工具调用**: [examples/basic/08-deepseek-agent](../08-deepseek-agent)
- **流式输出**: [examples/advanced/streaming](../../advanced/streaming)
- **多 Agent 协作**: [examples/advanced/multi-agent-collaboration](../../advanced/multi-agent-collaboration)

## 相关资源

- [DeepSeek 官方文档](https://platform.deepseek.com/docs)
- [GoAgent 完整文档](../../../docs/README.md)
- [LLM Providers 指南](../../../docs/guides/LLM_PROVIDERS.md)
