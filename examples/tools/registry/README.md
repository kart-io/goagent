# 工具注册与执行示例

本示例演示 tools 包的核心功能，包括工具注册表管理、自定义工具创建、工具执行器和工具组合。

## 目录

- [架构设计](#架构设计)
- [核心组件](#核心组件)
- [执行流程](#执行流程)
- [使用方法](#使用方法)
- [API 参考](#api-参考)

## 架构设计

### 工具系统架构

```mermaid
graph TB
    subgraph Registry["工具注册表"]
        Reg["Registry"]
        Tools["工具存储<br/>map~string~Tool"]
    end

    subgraph ToolTypes["工具类型"]
        FuncTool["FunctionTool<br/>函数式工具"]
        BaseTool["BaseTool<br/>基础工具"]
        MiddlewareTool["MiddlewareTool<br/>中间件工具"]
    end

    subgraph Executor["工具执行器"]
        Exec["ToolExecutor"]
        Invoke["Invoke()"]
        Result["ToolResult"]
    end

    subgraph Middleware["中间件层"]
        Logging["日志中间件"]
        Caching["缓存中间件"]
        RateLimit["限流中间件"]
    end

    ToolTypes --> Reg
    Reg --> Tools
    Tools --> Exec
    Exec --> Invoke
    Invoke --> Result

    Middleware --> Invoke

    style Reg fill:#e3f2fd
    style Exec fill:#c8e6c9
```

### 工具接口体系

```mermaid
classDiagram
    class Tool {
        <<interface>>
        +Name() string
        +Description() string
        +ArgsSchema() string
        +Invoke(ctx, input) ToolOutput
    }

    class ValidatableTool {
        <<interface>>
        +Validate(ctx, input) error
    }

    class ToolExecutor {
        <<interface>>
        +ExecuteTool(ctx, name, args) ToolResult
        +ListTools() list~Tool~
    }

    class FunctionTool {
        -name string
        -description string
        -argsSchema string
        -fn func
    }

    class BaseTool {
        -name string
        -description string
        -argsSchema string
        -runFunc func
    }

    class Registry {
        -tools map~string~Tool
        -mu RWMutex
        +Register(tool) error
        +Get(name) Tool
        +List() list~Tool~
        +Names() list~string~
        +Clear()
        +Size() int
    }

    Tool <|.. FunctionTool : 实现
    Tool <|.. BaseTool : 实现
    Tool <|-- ValidatableTool : 扩展
    Registry --> Tool : 管理
    ToolExecutor --> Tool : 执行
```

### 工具执行流程

```mermaid
sequenceDiagram
    participant App as 应用
    participant Exec as ToolExecutor
    participant Reg as Registry
    participant Tool as Tool
    participant MW as Middleware

    App->>Exec: ExecuteTool(ctx, name, args)
    Exec->>Reg: Get(name)
    Reg-->>Exec: Tool

    alt 找到工具
        Exec->>MW: 前置处理
        MW->>Tool: Invoke(ctx, input)
        Tool-->>MW: ToolOutput
        MW->>Exec: 后置处理
        Exec-->>App: ToolResult
    else 未找到工具
        Exec-->>App: 错误
    end
```

## 核心组件

### 1. Registry 工具注册表

```mermaid
graph LR
    subgraph Registry["Registry 操作"]
        Register["Register(tool)<br/>注册工具"]
        Get["Get(name)<br/>获取工具"]
        List["List()<br/>列出工具"]
        Names["Names()<br/>工具名称"]
        Clear["Clear()<br/>清空"]
        Size["Size()<br/>数量"]
    end

    subgraph Storage["存储"]
        Map["map~string~Tool"]
        Lock["sync.RWMutex"]
    end

    Register --> Map
    Get --> Map
    List --> Map
    Map --> Lock

    style Register fill:#c8e6c9
    style Get fill:#e3f2fd
```

### 2. FunctionTool 函数式工具

```mermaid
graph TB
    subgraph Definition["工具定义"]
        Name["名称: calculator"]
        Desc["描述: 数学计算"]
        Schema["参数 Schema"]
        Func["执行函数"]
    end

    subgraph Execution["执行过程"]
        Parse["解析参数"]
        Validate["验证输入"]
        Execute["执行函数"]
        Return["返回结果"]
    end

    Definition --> Parse
    Parse --> Validate
    Validate --> Execute
    Execute --> Return

    style Execute fill:#fff9c4
```

### 3. 工具组合模式

```mermaid
graph TB
    subgraph Pipeline["Pipeline 模式"]
        P1["Step 1"] --> P2["Step 2"]
        P2 --> P3["Step 3"]
    end

    subgraph Parallel["Parallel 模式"]
        PA["工具 A"]
        PB["工具 B"]
        PC["工具 C"]
        PA --> Merge["合并结果"]
        PB --> Merge
        PC --> Merge
    end

    subgraph Conditional["Conditional 模式"]
        Cond{{"条件"}}
        CA["工具 A"]
        CB["工具 B"]
        Cond --> |"true"| CA
        Cond --> |"false"| CB
    end

    style P1 fill:#e3f2fd
    style P2 fill:#e3f2fd
    style P3 fill:#e3f2fd
```

## 执行流程

### 工具注册流程

```mermaid
flowchart TD
    Start["创建工具"] --> Check{{"工具名已存在?"}}
    Check --> |"是"| Error["返回错误"]
    Check --> |"否"| Lock["获取写锁"]
    Lock --> Store["存储到 map"]
    Store --> Unlock["释放锁"]
    Unlock --> Success["注册成功"]

    style Success fill:#c8e6c9
    style Error fill:#ffcdd2
```

### 工具执行流程

```mermaid
flowchart TD
    Start["ExecuteTool(name, args)"] --> Get["获取工具"]
    Get --> Found{{"工具存在?"}}

    Found --> |"否"| NotFound["返回错误"]
    Found --> |"是"| CreateInput["创建 ToolInput"]

    CreateInput --> HasMiddleware{{"有中间件?"}}

    HasMiddleware --> |"是"| Before["OnBeforeInvoke"]
    HasMiddleware --> |"否"| Invoke["Invoke()"]

    Before --> Invoke
    Invoke --> Success{{"执行成功?"}}

    Success --> |"否"| OnError["OnError"]
    Success --> |"是"| After["OnAfterInvoke"]

    After --> CreateResult["创建 ToolResult"]
    OnError --> CreateResult
    CreateResult --> Return["返回结果"]

    style Return fill:#c8e6c9
    style NotFound fill:#ffcdd2
```

## 使用方法

### 运行示例

```bash
cd examples/tools/registry
go run main.go
```

### 预期输出

```text
╔════════════════════════════════════════════════════════════════╗
║          工具注册与执行示例                                     ║
║   展示工具注册表、自定义工具和执行器的使用                       ║
╚════════════════════════════════════════════════════════════════╝

【场景 1】工具注册表管理
════════════════════════════════════════════════════════════════

✓ 创建工具注册表

注册工具:
────────────────────────────────────────
  ✓ calculator: 执行基本数学计算
  ✓ text_processor: 处理文本
  ✓ datetime: 获取当前日期和时间
  ✓ random: 生成随机数

注册表状态:
────────────────────────────────────────
  已注册工具数: 4
  工具列表: [calculator text_processor datetime random]

【场景 2】自定义工具创建
════════════════════════════════════════════════════════════════
  ✓ 调用成功: map[greeting:你好，张三！ language:zh]
  ✓ 文本反转: map[operation:reverse result:dlroW olleH]
  ✓ 除零错误被正确捕获: 除数不能为零

【场景 3】工具执行器
════════════════════════════════════════════════════════════════
  ✓ calculator: map[a:7 b:8 operation:multiply result:56] (耗时: 0ms)
  ✓ text_processor: map[action:uppercase input:hello world output:HELLO WORLD] (耗时: 0ms)
```

## API 参考

### Registry API

| 方法 | 说明 | 返回值 |
|------|------|--------|
| `NewRegistry()` | 创建注册表 | `*Registry` |
| `Register(tool)` | 注册工具 | `error` |
| `Get(name)` | 获取工具 | `Tool` |
| `List()` | 列出所有工具 | `[]Tool` |
| `Names()` | 获取工具名称 | `[]string` |
| `Clear()` | 清空注册表 | - |
| `Size()` | 获取工具数量 | `int` |

### FunctionTool API

```go
// 创建函数式工具
tool := tools.NewFunctionTool(
    "name",           // 工具名称
    "description",    // 工具描述
    argsSchema,       // JSON Schema
    func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        // 工具逻辑
        return result, nil
    },
)
```

### BaseTool API

```go
// 创建基础工具
tool := tools.NewBaseTool(
    "name",
    "description",
    argsSchema,
    func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
        // 工具逻辑
        return &interfaces.ToolOutput{
            Result:  result,
            Success: true,
        }, nil
    },
)
```

### 代码示例

#### 创建和注册工具

```go
// 创建注册表
registry := tools.NewRegistry()

// 创建工具
calcTool := tools.NewFunctionTool(
    "calculator",
    "执行数学计算",
    `{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}}}`,
    func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        a := args["a"].(float64)
        b := args["b"].(float64)
        return a + b, nil
    },
)

// 注册工具
if err := registry.Register(calcTool); err != nil {
    log.Fatal(err)
}
```

#### 执行工具

```go
// 获取工具
tool := registry.Get("calculator")
if tool == nil {
    log.Fatal("工具未找到")
}

// 执行工具
output, err := tool.Invoke(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "a": 10.0,
        "b": 20.0,
    },
})

fmt.Printf("结果: %v\n", output.Result)
```

#### 工具组合

```go
// Pipeline 执行
tools := []interfaces.Tool{tool1, tool2, tool3}
var result interface{}

for _, tool := range tools {
    output, _ := tool.Invoke(ctx, &interfaces.ToolInput{
        Args: map[string]interface{}{"input": result},
    })
    result = output.Result
}
```

## 扩展阅读

- [LLM 工具调用示例](../../multiagent/06-llm-tool-calling/)
- [LLM 高级用法示例](../llm/advanced/)
- [中间件与可观测性示例](../middleware/)
- [tools 包文档](../../../tools/) - 工具 API 参考
