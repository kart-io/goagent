# GoAgent 设计概述

本文档描述 GoAgent 框架的核心设计理念、架构决策和关键设计模式。

## 设计原则

### 1. 接口驱动设计

GoAgent 采用接口驱动的设计方式，所有核心组件都通过接口定义规范：

```mermaid
graph TB
    subgraph "接口层 (interfaces/)"
        IA[Agent]
        IR[Runnable]
        IT[Tool]
        IM[MemoryManager]
        IS[Store]
        IC[Checkpointer]
    end

    subgraph "实现层"
        BA[BaseAgent]
        BT[BaseTool]
        DM[DefaultManager]
        MS[MemoryStore]
        MC[MemoryCheckpointer]
    end

    IA --> BA
    IR --> BA
    IT --> BT
    IM --> DM
    IS --> MS
    IC --> MC
```

### 2. 分层架构

框架采用严格的 4 层架构，通过导入规则确保依赖方向单一：

```mermaid
graph TD
    L4["第4层: 应用层<br/>(examples/, tests)"]
    L3["第3层: 实现层<br/>(agents/, tools/, middleware/)"]
    L2["第2层: 业务层<br/>(core/, builder/, llm/, memory/)"]
    L1["第1层: 基础层<br/>(interfaces/, errors/, utils/)"]

    L4 --> L3
    L4 --> L2
    L4 --> L1
    L3 --> L2
    L3 --> L1
    L2 --> L1

    style L1 fill:#e3f2fd
    style L2 fill:#fff8e1
    style L3 fill:#f3e5f5
    style L4 fill:#e8f5e9
```

### 3. 组合优于继承

GoAgent 优先使用组合模式而非继承：

```go
// 组合模式示例
type ConfigurableAgent struct {
    llmClient     llm.Client        // 组合 LLM 客户端
    tools         []Tool            // 组合工具集
    middlewares   []Middleware      // 组合中间件
    memoryManager MemoryManager     // 组合内存管理
}
```

## 核心设计模式

### 1. Builder 模式

AgentBuilder 提供流畅的 API 来构建复杂的 Agent：

```mermaid
sequenceDiagram
    participant User as 用户
    participant Builder as AgentBuilder
    participant Agent as ConfigurableAgent
    participant Runtime as Runtime

    User->>Builder: NewAgentBuilder(llm)
    User->>Builder: WithTools(tools...)
    User->>Builder: WithSystemPrompt(prompt)
    User->>Builder: WithMiddleware(mw...)
    User->>Builder: WithMemoryManager(mm)
    User->>Builder: Build()
    Builder->>Runtime: NewRuntime(ctx, state, store, cp)
    Builder->>Agent: 创建 ConfigurableAgent
    Builder-->>User: 返回 Agent
```

### 2. Chain of Responsibility 模式

中间件系统使用责任链模式处理请求：

```mermaid
flowchart LR
    REQ[请求] --> M1[Logging]
    M1 --> M2[Timing]
    M2 --> M3[Cache]
    M3 --> M4[RateLimit]
    M4 --> H[Handler]
    H --> M4'[RateLimit]
    M4' --> M3'[Cache]
    M3' --> M2'[Timing]
    M2' --> M1'[Logging]
    M1' --> RESP[响应]
```

### 3. Strategy 模式

不同的推理策略（ReAct、CoT、ToT 等）使用策略模式：

```mermaid
classDiagram
    class ReasoningStrategy {
        <<interface>>
        +Process(input) Output
    }

    class ReActStrategy {
        +Think() Thought
        +Act() Action
        +Observe() Observation
    }

    class CoTStrategy {
        +GenerateChain() []Thought
    }

    class ToTStrategy {
        +Explore() Tree
        +Evaluate() Score
    }

    ReasoningStrategy <|.. ReActStrategy
    ReasoningStrategy <|.. CoTStrategy
    ReasoningStrategy <|.. ToTStrategy
```

### 4. Factory 模式

LLM Provider 和 Store 使用工厂模式创建实例：

```mermaid
flowchart TB
    subgraph "LLM Factory"
        LF[ProviderFactory]
        LF --> |OpenAI| OP[OpenAIProvider]
        LF --> |Anthropic| AP[AnthropicProvider]
        LF --> |Gemini| GP[GeminiProvider]
        LF --> |Ollama| OL[OllamaProvider]
    end

    subgraph "Store Factory"
        SF[StoreFactory]
        SF --> |memory| MS[MemoryStore]
        SF --> |redis| RS[RedisStore]
        SF --> |postgres| PS[PostgresStore]
    end
```

### 5. Decorator 模式

工具中间件使用装饰器模式增强功能：

```mermaid
flowchart LR
    T[BaseTool]
    T --> L[LoggingDecorator]
    L --> C[CachingDecorator]
    C --> R[RetryDecorator]
    R --> V[ValidationDecorator]
    V --> FT[最终工具]
```

## 关键流程设计

### Agent 执行流程

```mermaid
flowchart TB
    START([开始]) --> INPUT[接收输入]
    INPUT --> INIT[初始化上下文]
    INIT --> LOAD[加载历史对话]
    LOAD --> BUILD[构建消息列表]
    BUILD --> MW_BEFORE[执行前置中间件]
    MW_BEFORE --> LLM[调用 LLM]
    LLM --> PARSE[解析响应]
    PARSE --> TOOL{需要调用工具?}
    TOOL -->|是| EXEC[执行工具]
    EXEC --> OBS[观察结果]
    OBS --> BUILD
    TOOL -->|否| MW_AFTER[执行后置中间件]
    MW_AFTER --> SAVE[保存对话]
    SAVE --> CHECKPOINT{自动保存?}
    CHECKPOINT -->|是| CP[保存检查点]
    CHECKPOINT -->|否| OUTPUT
    CP --> OUTPUT[返回输出]
    OUTPUT --> END([结束])
```

### Tool 执行流程

```mermaid
flowchart TB
    START([开始]) --> RECEIVE[接收工具调用]
    RECEIVE --> LOOKUP[查找工具]
    LOOKUP --> FOUND{找到工具?}
    FOUND -->|否| ERROR1[返回错误]
    FOUND -->|是| VALIDATE[验证参数]
    VALIDATE --> VALID{参数有效?}
    VALID -->|否| ERROR2[返回验证错误]
    VALID -->|是| MW[执行工具中间件]
    MW --> EXEC[执行工具]
    EXEC --> SUCCESS{执行成功?}
    SUCCESS -->|否| RETRY{可重试?}
    RETRY -->|是| MW
    RETRY -->|否| ERROR3[返回执行错误]
    SUCCESS -->|是| RESULT[返回结果]
    ERROR1 --> END([结束])
    ERROR2 --> END
    ERROR3 --> END
    RESULT --> END
```

### Memory 管理流程

```mermaid
flowchart TB
    subgraph "对话管理"
        ADD[添加对话] --> CONV_STORE[(对话存储)]
        GET[获取历史] --> CONV_STORE
        CLEAR[清除对话] --> CONV_STORE
    end

    subgraph "案例管理"
        ADD_CASE[添加案例] --> EMBED[生成向量]
        EMBED --> CASE_STORE[(案例存储)]
        SEARCH[搜索案例] --> QUERY_EMBED[查询向量化]
        QUERY_EMBED --> SIMILARITY[相似度计算]
        SIMILARITY --> CASE_STORE
    end

    subgraph "KV 存储"
        STORE[存储] --> KV_STORE[(KV 存储)]
        RETRIEVE[检索] --> KV_STORE
        DELETE[删除] --> KV_STORE
    end
```

## 状态管理设计

### State 生命周期

```mermaid
stateDiagram-v2
    [*] --> Uninitialized
    Uninitialized --> Initialized: Init()
    Initialized --> Running: Start()
    Running --> Running: Process()
    Running --> Checkpointed: SaveCheckpoint()
    Checkpointed --> Running: Continue
    Running --> Stopped: Stop()
    Stopped --> Running: Resume()
    Running --> Failed: Error
    Failed --> Running: Recover()
    Stopped --> [*]
```

### Checkpoint 机制

```mermaid
sequenceDiagram
    participant A as Agent
    participant S as State
    participant CP as Checkpointer
    participant ST as Storage

    A->>S: 更新状态
    A->>CP: SaveCheckpoint(state)
    CP->>ST: 持久化状态
    ST-->>CP: 确认保存
    CP-->>A: 返回检查点 ID

    Note over A,ST: 恢复流程

    A->>CP: LoadCheckpoint(id)
    CP->>ST: 读取状态
    ST-->>CP: 返回状态数据
    CP-->>A: 返回检查点
    A->>S: 恢复状态
```

## 并发设计

### 并行工具执行

```mermaid
flowchart TB
    INPUT[工具调用列表] --> ANALYZE[分析依赖]
    ANALYZE --> PARALLEL{可并行?}
    PARALLEL -->|是| CONCURRENT[并发执行]
    PARALLEL -->|否| SEQUENTIAL[顺序执行]

    subgraph "并发执行"
        CONCURRENT --> G1[Goroutine 1]
        CONCURRENT --> G2[Goroutine 2]
        CONCURRENT --> G3[Goroutine N]
        G1 --> COLLECT[收集结果]
        G2 --> COLLECT
        G3 --> COLLECT
    end

    SEQUENTIAL --> RESULT[返回结果]
    COLLECT --> RESULT
```

### 线程安全设计

```mermaid
classDiagram
    class Registry {
        -tools map[string]Tool
        -mu sync.RWMutex
        +Register(tool) error
        +Get(name) Tool
        +List() []Tool
    }

    class ShardedCache {
        -shards []*shard
        -numShards uint32
        +Get(key) value
        +Set(key, value)
        +Delete(key)
    }

    class shard {
        -data map[string]entry
        -mu sync.RWMutex
    }

    ShardedCache o-- shard
```

## 扩展性设计

### 插件架构

```mermaid
flowchart TB
    subgraph "核心框架"
        CORE[GoAgent Core]
    end

    subgraph "可插拔组件"
        LLM[LLM Providers]
        TOOL[Tools]
        STORE[Stores]
        MW[Middlewares]
        REASON[Reasoning Strategies]
    end

    subgraph "扩展点"
        CUSTOM_LLM[自定义 LLM]
        CUSTOM_TOOL[自定义工具]
        CUSTOM_STORE[自定义存储]
        CUSTOM_MW[自定义中间件]
    end

    CORE --> LLM
    CORE --> TOOL
    CORE --> STORE
    CORE --> MW
    CORE --> REASON

    LLM -.-> CUSTOM_LLM
    TOOL -.-> CUSTOM_TOOL
    STORE -.-> CUSTOM_STORE
    MW -.-> CUSTOM_MW
```

### 接口扩展点

| 扩展点 | 接口 | 说明 |
|-------|------|------|
| LLM Provider | `llm.Client` | 添加新的 LLM 提供商 |
| Tool | `interfaces.Tool` | 添加自定义工具 |
| Store | `interfaces.Store` | 添加新的存储后端 |
| Middleware | `middleware.Middleware` | 添加请求处理中间件 |
| Memory | `interfaces.MemoryManager` | 自定义内存管理 |
| Checkpointer | `interfaces.Checkpointer` | 自定义检查点存储 |
| Parser | `parsers.OutputParser` | 自定义输出解析器 |

## 性能优化设计

### 对象池

```mermaid
flowchart LR
    subgraph "对象池系统"
        REQ_POOL[RequestPool]
        RESP_POOL[ResponsePool]
        INPUT_POOL[InputPool]
        OUTPUT_POOL[OutputPool]
    end

    GET[Get()] --> REQ_POOL
    REQ_POOL --> USE[使用对象]
    USE --> PUT[Put()]
    PUT --> REQ_POOL
```

### 缓存策略

```mermaid
flowchart TB
    REQ[请求] --> CACHE{缓存命中?}
    CACHE -->|是| HIT[返回缓存]
    CACHE -->|否| EXEC[执行请求]
    EXEC --> STORE[存储到缓存]
    STORE --> RESP[返回响应]
    HIT --> RESP

    subgraph "缓存配置"
        TTL[TTL 过期]
        LRU[LRU 淘汰]
        SHARD[分片存储]
    end
```

## 相关文档

- [时序图详解](SEQUENCE_DIAGRAMS.md)
- [流程图详解](FLOW_DIAGRAMS.md)
- [架构概述](../architecture/ARCHITECTURE.md)
- [组件关系图](../architecture/COMPONENT_DIAGRAM.md)
