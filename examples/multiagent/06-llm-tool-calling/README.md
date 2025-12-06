# 06-llm-tool-calling LLM 工具调用示例

本示例演示如何在 MultiAgent 系统中使用 LLM 进行工具调用（Tool Calling），展示 LLM 自动选择和执行工具的能力。

## 目录

- [架构设计](#架构设计)
- [核心组件](#核心组件)
- [执行流程](#执行流程)
- [使用方法](#使用方法)
- [应用场景](#应用场景)

## 架构设计

### LLM 工具调用架构

```mermaid
graph TB
    subgraph LLMProviders["LLM 提供商"]
        DeepSeek["DeepSeek API"]
        OpenAI["OpenAI API"]
        Mock["Mock 客户端"]
    end

    subgraph ToolCalling["工具调用层"]
        TC["ToolCallingClient 接口"]
        GWT["GenerateWithTools()"]
        TCR["ToolCallResponse"]
    end

    subgraph Tools["工具层"]
        Calculator["calculator<br/>基础计算"]
        AdvMath["advanced_math<br/>高级数学"]
        Weather["weather<br/>天气查询"]
        Search["search<br/>信息搜索"]
        Time["current_time<br/>时间查询"]
        Timer["timer<br/>计时器"]
        DataFetch["data_fetch<br/>数据获取"]
        DataProcess["data_process<br/>数据处理"]
        DataFormat["data_format<br/>数据格式化"]
    end

    subgraph Agents["Tool Agent 层"]
        Assistant["通用助手"]
        MathExpert["数学专家"]
        InfoExpert["信息专家"]
        TimeExpert["时间专家"]
        Fetcher["数据获取器"]
        Processor["数据处理器"]
        Formatter["数据格式化器"]
    end

    subgraph MAS["MultiAgentSystem"]
        Registry["Agent 注册表"]
        TaskExec["任务执行器"]
    end

    DeepSeek --> TC
    OpenAI --> TC
    Mock --> TC

    TC --> GWT --> TCR

    TCR --> Assistant
    TCR --> MathExpert
    TCR --> InfoExpert
    TCR --> TimeExpert

    Assistant --> Calculator
    Assistant --> Weather
    Assistant --> Search
    Assistant --> Time

    MathExpert --> Calculator
    MathExpert --> AdvMath

    InfoExpert --> Search
    InfoExpert --> Weather

    TimeExpert --> Time
    TimeExpert --> Timer

    Fetcher --> DataFetch
    Processor --> DataProcess
    Formatter --> DataFormat

    Agents --> Registry
    TaskExec --> Agents

    style TC fill:#e3f2fd
    style GWT fill:#e3f2fd
    style Tools fill:#c8e6c9
```

### ToolAgent 结构

```mermaid
classDiagram
    class ToolAgent {
        -base *BaseCollaborativeAgent
        -llmClient Client
        -tools list~Tool~
        -toolMap map~string~Tool
        -systemPrompt string
        +Collaborate(ctx, task) Assignment
        +buildPrompt(userPrompt) string
        +buildPipelinePrompt(input) string
        +executeToolCalls(ctx, toolCalls, prompt) tuple~string, list~string~~
    }

    class BaseCollaborativeAgent {
        -id string
        -description string
        -role Role
        +Name() string
        +GetRole() Role
        +SetRole(role)
    }

    class ToolCallingClient {
        <<interface>>
        +GenerateWithTools(ctx, prompt, tools) ToolCallResponse
        +StreamWithTools(ctx, prompt, tools) chan~ToolChunk~
    }

    class ToolCallResponse {
        +Content string
        +ToolCalls list~ToolCall~
        +Model string
        +TokensUsed int
    }

    class ToolCall {
        +ID string
        +Type string
        +Name string
        +Arguments map~string~any
        +Function struct
    }

    class Tool {
        <<interface>>
        +Name() string
        +Description() string
        +ArgsSchema() string
        +Invoke(ctx, input) tuple~any, error~
    }

    class FunctionTool {
        -name string
        -description string
        -argsSchema string
        -fn func
    }

    ToolAgent *-- BaseCollaborativeAgent : 嵌入
    ToolAgent --> ToolCallingClient : 使用
    ToolAgent --> Tool : 管理
    FunctionTool ..|> Tool : 实现
    ToolCallingClient ..> ToolCallResponse : 返回
    ToolCallResponse *-- ToolCall : 包含
```

### 工具调用决策流程

```mermaid
flowchart TD
    Start["用户问题"] --> Build["构建 Prompt"]
    Build --> Check{"检查 LLM<br/>是否支持工具调用"}

    Check --> |"支持"| GenTools["GenerateWithTools()"]
    Check --> |"不支持"| FallbackChat["降级: Chat()"]

    GenTools --> ParseResp{"解析响应"}

    ParseResp --> |"有 ToolCalls"| ExecTools["执行工具调用"]
    ParseResp --> |"无 ToolCalls"| DirectContent["直接返回内容"]

    ExecTools --> FindTool{"查找工具"}
    FindTool --> |"找到"| ParseArgs["解析参数"]
    FindTool --> |"未找到"| ToolNotFound["工具不存在错误"]

    ParseArgs --> Invoke["tool.Invoke()"]
    Invoke --> CollectResult["收集结果"]

    CollectResult --> MoreTools{"还有更多<br/>工具调用?"}
    MoreTools --> |"是"| FindTool
    MoreTools --> |"否"| FormatResult["格式化结果"]

    FormatResult --> Return["返回 Assignment"]
    DirectContent --> Return
    FallbackChat --> Return
    ToolNotFound --> FormatResult

    style GenTools fill:#c8e6c9
    style ExecTools fill:#fff9c4
    style Invoke fill:#e3f2fd
```

## 核心组件

### 1. ToolAgent 工具代理

将 LLM 工具调用能力与协作 Agent 框架结合的核心组件。

```mermaid
graph LR
    subgraph Input["输入"]
        Task["协作任务"]
        Prompt["系统提示"]
        ToolSet["工具集"]
    end

    subgraph Process["处理流程"]
        Build["构建请求"]
        LLMCall["LLM 调用"]
        Decide["决策工具"]
        Execute["执行工具"]
        Collect["收集结果"]
    end

    subgraph Output["输出"]
        Assignment["Assignment"]
        Response["响应内容"]
        ToolsUsed["使用的工具"]
    end

    Task --> Build
    Prompt --> Build
    ToolSet --> Build
    Build --> LLMCall
    LLMCall --> Decide
    Decide --> Execute
    Execute --> Collect
    Collect --> Assignment
    Collect --> Response
    Collect --> ToolsUsed
```

### 2. 工具定义

```mermaid
graph TB
    subgraph BasicTools["基础工具集"]
        Calc["calculator<br/>加减乘除"]
        Weather["weather<br/>天气查询"]
        Search["search<br/>信息搜索"]
        Time["current_time<br/>当前时间"]
    end

    subgraph MathTools["数学工具集"]
        CalcM["calculator<br/>基础运算"]
        AdvMath["advanced_math<br/>幂/开方/三角函数/对数/π"]
    end

    subgraph InfoTools["信息工具集"]
        SearchI["search<br/>关键词搜索"]
        WeatherI["weather<br/>城市天气"]
    end

    subgraph TimeTools["时间工具集"]
        TimeT["current_time<br/>时区时间"]
        Timer["timer<br/>倒计时/计时器"]
    end

    subgraph DataTools["数据处理工具集"]
        Fetch["data_fetch<br/>获取原始数据"]
        Process["data_process<br/>聚合/过滤/转换"]
        Format["data_format<br/>JSON/CSV/Table"]
    end

    style BasicTools fill:#e3f2fd
    style MathTools fill:#fff9c4
    style InfoTools fill:#c8e6c9
    style TimeTools fill:#f3e5f5
    style DataTools fill:#ffccbc
```

### 3. 三种使用场景

```mermaid
graph TB
    subgraph Scenario1["场景1: 单 Agent 工具调用"]
        direction TB
        Q1["用户问题"]
        A1["通用助手"]
        T1["工具集<br/>(calculator, weather, search, time)"]
        R1["智能选择工具并执行"]

        Q1 --> A1
        A1 --> T1
        T1 --> R1
    end

    subgraph Scenario2["场景2: 多 Agent 专业工具协作"]
        direction TB
        CT["复杂任务"]
        ME["数学专家<br/>(calculator, advanced_math)"]
        IE["信息专家<br/>(search, weather)"]
        TE["时间专家<br/>(current_time, timer)"]
        CR["综合结果"]

        CT --> ME --> CR
        CT --> IE --> CR
        CT --> TE --> CR
    end

    subgraph Scenario3["场景3: 工具链式调用 Pipeline"]
        direction TB
        Input["输入数据"]
        S1["Stage1: data_fetch"]
        S2["Stage2: data_process"]
        S3["Stage3: data_format"]
        Output["格式化输出"]

        Input --> S1 --> S2 --> S3 --> Output
    end
```

## 执行流程

### 场景1: 单 Agent 工具调用

```mermaid
sequenceDiagram
    participant Client as 客户端
    participant MAS as MultiAgentSystem
    participant Agent as ToolAgent
    participant LLM as LLM API
    participant Tool as 工具

    Client->>MAS: 提交问题<br/>"计算 15 乘以 28"

    MAS->>Agent: Collaborate(task)
    Agent->>Agent: buildPrompt(问题)

    Agent->>LLM: GenerateWithTools(prompt, tools)

    Note over LLM: LLM 分析问题<br/>决定调用 calculator

    LLM-->>Agent: ToolCallResponse<br/>{ToolCalls: [{name: "calculator", args: {...}}]}

    Agent->>Agent: executeToolCalls()
    Agent->>Tool: calculator.Invoke({operation: "multiply", a: 15, b: 28})
    Tool-->>Agent: {result: 420}

    Agent->>Agent: 格式化结果

    Agent-->>MAS: Assignment<br/>{response: "[calculator] 420", tools_used: ["calculator"]}

    MAS-->>Client: 任务结果
```

### 场景2: 多 Agent 专业工具协作

```mermaid
sequenceDiagram
    participant Client as 客户端
    participant MAS as MultiAgentSystem
    participant Math as 数学专家
    participant Info as 信息专家
    participant Time as 时间专家
    participant LLM as LLM API

    Client->>MAS: 提交复杂任务
    Note over Client,MAS: 包含数学、信息、时间三类问题

    par 并行执行
        MAS->>Math: Collaborate(math_question)
        Math->>LLM: GenerateWithTools(数学问题, mathTools)
        LLM-->>Math: 调用 advanced_math
        Math->>Math: 执行 π 计算
        Math-->>MAS: π × 100 = 314.159...
    and
        MAS->>Info: Collaborate(info_question)
        Info->>LLM: GenerateWithTools(信息问题, infoTools)
        LLM-->>Info: 调用 weather
        Info->>Info: 执行天气查询
        Info-->>MAS: 上海天气: 晴朗, 22°C
    and
        MAS->>Time: Collaborate(time_question)
        Time->>LLM: GenerateWithTools(时间问题, timeTools)
        LLM-->>Time: 调用 current_time
        Time->>Time: 执行时间查询
        Time-->>MAS: 当前时间: 14:30:00
    end

    MAS->>MAS: 汇总各专家结果
    MAS-->>Client: 综合任务结果
```

### 场景3: 工具链式调用 (Pipeline)

```mermaid
sequenceDiagram
    participant Client as 客户端
    participant MAS as MultiAgentSystem
    participant Fetch as 数据获取器
    participant Process as 数据处理器
    participant Format as 数据格式化器
    participant Tools as 工具

    Client->>MAS: 提交 Pipeline 任务
    Note over Client,MAS: 获取 → 处理 → 格式化

    rect rgb(230, 245, 255)
        Note over Fetch: Stage 1: 数据获取
        MAS->>Fetch: Collaborate(stage1)
        Fetch->>Tools: data_fetch.Invoke({source: "sales_2024"})
        Tools-->>Fetch: {data: [...], fetched_at: "..."}
        Fetch-->>MAS: 原始数据
    end

    rect rgb(255, 245, 230)
        Note over Process: Stage 2: 数据处理
        MAS->>Process: Collaborate(stage2 + 上阶段输出)
        Process->>Tools: data_process.Invoke({operation: "aggregate"})
        Tools-->>Process: {total: 450, average: 150, count: 3}
        Process-->>MAS: 处理后数据
    end

    rect rgb(230, 255, 230)
        Note over Format: Stage 3: 数据格式化
        MAS->>Format: Collaborate(stage3 + 上阶段输出)
        Format->>Tools: data_format.Invoke({output_type: "json"})
        Tools-->>Format: {format: "json", output: "..."}
        Format-->>MAS: 格式化结果
    end

    MAS-->>Client: Pipeline 完成
```

### 工具调用内部流程

```mermaid
flowchart TD
    subgraph ToolAgent["ToolAgent.Collaborate()"]
        Start["接收任务"] --> Parse["解析输入"]
        Parse --> Build["构建 Prompt"]
        Build --> Check{"LLM 支持<br/>工具调用?"}

        Check --> |"是"| GenTools["llm.AsToolCaller()"]
        Check --> |"否"| Fallback["降级到 Chat()"]

        GenTools --> Call["GenerateWithTools()"]
        Call --> HasTools{"响应包含<br/>ToolCalls?"}

        HasTools --> |"是"| Loop["遍历 ToolCalls"]
        HasTools --> |"否"| Direct["直接使用 Content"]

        Loop --> Find["从 toolMap 查找工具"]
        Find --> Found{"找到工具?"}

        Found --> |"是"| ParseArgs["解析 Arguments"]
        Found --> |"否"| NotFound["记录工具不存在"]

        ParseArgs --> Invoke["tool.Invoke(ctx, args)"]
        Invoke --> Success{"执行成功?"}

        Success --> |"是"| AddResult["添加到结果列表"]
        Success --> |"否"| AddError["添加错误信息"]

        AddResult --> More{"更多工具?"}
        AddError --> More

        More --> |"是"| Loop
        More --> |"否"| Format["格式化所有结果"]

        NotFound --> More
        Direct --> Format
        Fallback --> Format

        Format --> Return["返回 Assignment"]
    end

    style GenTools fill:#c8e6c9
    style Invoke fill:#fff9c4
    style Return fill:#e3f2fd
```

## 使用方法

### 环境配置

```bash
# 使用 DeepSeek（推荐，中文能力强）
export DEEPSEEK_API_KEY="your-api-key"

# 或使用 OpenAI
export OPENAI_API_KEY="your-api-key"

# 未配置 API Key 时将使用 Mock 客户端
```

### 运行示例

```bash
cd examples/multiagent/06-llm-tool-calling
go run main.go
```

### 预期输出

```text
╔════════════════════════════════════════════════════════════════╗
║          LLM Tool Calling 多智能体协作示例                     ║
║   展示如何让 LLM Agent 调用工具完成任务                        ║
╚════════════════════════════════════════════════════════════════╝

LLM 提供商: deepseek

【场景 1】单 Agent 工具调用
════════════════════════════════════════════════════════════════

场景描述: 单个 Agent 配备多个工具，根据用户问题自动选择合适的工具

可用工具:
  - calculator: 执行基本数学计算（加减乘除）
  - weather: 查询指定城市的天气信息
  - search: 搜索相关信息和资料
  - current_time: 获取当前时间
✓ 注册工具 Agent: assistant

问题 1: 计算 15 乘以 28 等于多少？
────────────────────────────────────────
回答: [calculator] map[a:15 b:28 operation:multiply result:420]
使用的工具: [calculator]

问题 2: 今天北京的天气怎么样？
────────────────────────────────────────
回答: [weather] map[aqi:45 city:北京 condition:晴朗 humidity:65 ...]
使用的工具: [weather]

问题 3: 搜索关于 Go 语言并发编程的资料
────────────────────────────────────────
回答: [search] map[count:3 query:Go 语言并发编程 results:[...]]
使用的工具: [search]

问题 4: 现在是几点钟？
────────────────────────────────────────
回答: [current_time] map[date:2024-12-05 datetime:... time:14:30:00 ...]
使用的工具: [current_time]

【场景 2】多 Agent 专业工具协作
════════════════════════════════════════════════════════════════

场景描述: 多个专业 Agent 各自拥有不同的工具集，协作完成复杂任务

注册的专家 Agent:
  ✓ math-expert: 数学专家 (calculator, advanced_math)
  ✓ info-expert: 信息专家 (search, weather)
  ✓ time-expert: 时间专家 (current_time, timer)

执行综合任务...

各专家执行结果:
────────────────────────────────────────

【math-expert】
  响应: [advanced_math] map[input:0 operation:pi result:3.141592653589793]
  使用工具: [advanced_math]

【info-expert】
  响应: [weather] map[city:上海 condition:晴朗 temperature:22 ...]
  使用工具: [weather]

【time-expert】
  响应: [current_time] map[datetime:2024-12-05 14:30:00 timezone:Asia/Shanghai ...]
  使用工具: [current_time]

【场景 3】工具链式调用（Pipeline）
════════════════════════════════════════════════════════════════

场景描述: 工具的输出作为下一个工具的输入，形成处理管道

Pipeline 阶段:
  Stage 1: data-fetcher  [data_fetch]
  Stage 2: data-processor [data_process]
  Stage 3: data-formatter [data_format]

执行数据处理管道...
✓ Pipeline 完成，状态: completed

各阶段结果:
  fetch: map[data:[...] fetched_at:2024-12-05T14:30:00Z source:sales_2024]
  process: map[operation:aggregate processed:map[average:150 count:3 total:450] ...]
  format: map[format:json formatted_at:2024-12-05T14:30:01Z output:数据已格式化为 json 格式]

╔════════════════════════════════════════════════════════════════╗
║                        示例完成                                ║
╚════════════════════════════════════════════════════════════════╝
```

### 关键代码示例

#### 创建工具

```go
// 使用 tools.NewFunctionTool 创建工具
calculatorTool := tools.NewFunctionTool(
    "calculator",
    "执行基本数学计算（加减乘除）",
    `{
        "type": "object",
        "properties": {
            "operation": {"type": "string", "enum": ["add", "subtract", "multiply", "divide"]},
            "a": {"type": "number"},
            "b": {"type": "number"}
        },
        "required": ["operation", "a", "b"]
    }`,
    func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        op := args["operation"].(string)
        a := args["a"].(float64)
        b := args["b"].(float64)

        var result float64
        switch op {
        case "add":
            result = a + b
        case "multiply":
            result = a * b
        // ...
        }
        return map[string]interface{}{"result": result}, nil
    },
)
```

#### 创建 ToolAgent

```go
agent := NewToolAgent(
    "assistant",           // ID
    "通用助手",            // 描述
    multiagent.RoleWorker, // 角色
    system,                // MultiAgentSystem
    llmClient,             // LLM 客户端
    toolSet,               // 工具集
    "你是一个智能助手，可以使用工具来帮助用户完成任务。", // 系统提示
)
```

#### 实现 Collaborate 方法

```go
func (a *ToolAgent) Collaborate(ctx context.Context, task *multiagent.CollaborativeTask) (*multiagent.Assignment, error) {
    // 尝试使用工具调用
    toolCaller := llm.AsToolCaller(a.llmClient)
    if toolCaller != nil && len(a.tools) > 0 {
        result, err := toolCaller.GenerateWithTools(ctx, a.buildPrompt(userPrompt), a.tools)
        if err == nil && len(result.ToolCalls) > 0 {
            response, toolsUsed = a.executeToolCalls(ctx, result.ToolCalls, userPrompt)
        }
    }

    return &multiagent.Assignment{
        AgentID: a.Name(),
        Result: map[string]interface{}{
            "response":   response,
            "tools_used": toolsUsed,
        },
        Status: multiagent.TaskStatusCompleted,
    }, nil
}
```

## 应用场景

### LLM 工具调用应用矩阵

```mermaid
mindmap
    root((LLM 工具调用))
        智能助手
            问答系统
            任务执行
            信息检索
            计算服务
        数据处理
            数据获取
            数据转换
            数据分析
            报表生成
        业务自动化
            订单处理
            库存查询
            客户服务
            流程编排
        开发辅助
            代码生成
            文档查询
            API 调用
            测试执行
```

### 工具调用模式对比

| 模式 | 说明 | 适用场景 | Agent 数量 |
|------|------|---------|-----------|
| **单 Agent 多工具** | 一个 Agent 配备多种工具 | 通用助手、问答系统 | 1 |
| **多 Agent 专业工具** | 每个 Agent 专注特定工具集 | 专家协作、复杂分析 | N |
| **Pipeline 工具链** | 工具按顺序链式执行 | 数据处理、流程编排 | N |

### 与其他示例的关系

```mermaid
graph LR
    subgraph Foundation["基础"]
        E01["01-basic-system<br/>基础系统"]
        E02["02-collaboration-types<br/>协作类型"]
    end

    subgraph Advanced["进阶"]
        E03["03-team-management<br/>团队管理"]
        E04["04-specialized-agents<br/>专业化 Agent"]
    end

    subgraph LLM["LLM 集成"]
        E05["05-llm-collaborative<br/>LLM 协作"]
        E06["06-llm-tool-calling<br/>LLM 工具调用"]
    end

    E01 --> E02
    E02 --> E03
    E02 --> E04
    E04 --> E05
    E05 --> E06

    style E06 fill:#c8e6c9,stroke:#2e7d32,stroke-width:2px
```

## 扩展阅读

- [01-basic-system](../01-basic-system/) - 基础系统示例
- [02-collaboration-types](../02-collaboration-types/) - 协作类型示例
- [05-llm-collaborative-agents](../05-llm-collaborative-agents/) - LLM 协作 Agent 示例
- [llm 包文档](../../../llm/) - LLM 客户端使用指南
- [tools 包文档](../../../tools/) - 工具定义与实现
- [interfaces 包文档](../../../interfaces/) - 工具接口定义
