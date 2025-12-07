# GoAgent 组件关系图

本文档展示 GoAgent 框架中各组件之间的关系和依赖。

## 整体架构图

```mermaid
graph TB
    subgraph "用户应用层"
        APP[应用程序]
        CLI[命令行工具]
    end

    subgraph "GoAgent 框架"
        subgraph "第4层 - 应用层"
            EXAMPLES[examples/]
            TESTS[*_test.go]
        end

        subgraph "第3层 - 实现层"
            AGENTS[agents/]
            TOOLS[tools/]
            MW[middleware/]
            STREAM[stream/]
            MULTI[multiagent/]
            DIST[distributed/]
            MCP[mcp/]
            PARSERS[parsers/]
        end

        subgraph "第2层 - 业务逻辑层"
            CORE[core/]
            BUILDER[builder/]
            LLM[llm/]
            MEMORY[memory/]
            STORE[store/]
            OBS[observability/]
            PLANNING[planning/]
            RETRIEVAL[retrieval/]
        end

        subgraph "第1层 - 基础层"
            INTERFACES[interfaces/]
            ERRORS[errors/]
            CACHE[cache/]
            UTILS[utils/]
            OPTIONS[options/]
        end
    end

    subgraph "外部服务"
        OPENAI[OpenAI API]
        ANTHROPIC[Anthropic API]
        GEMINI[Google Gemini]
        DEEPSEEK[DeepSeek API]
        OLLAMA[Ollama 本地]
        REDIS[(Redis)]
        PG[(PostgreSQL)]
        MYSQL[(MySQL)]
        NATS[NATS 消息]
    end

    APP --> BUILDER
    APP --> AGENTS
    CLI --> BUILDER

    BUILDER --> CORE
    BUILDER --> AGENTS
    BUILDER --> LLM
    BUILDER --> TOOLS
    BUILDER --> MEMORY
    BUILDER --> STORE

    AGENTS --> CORE
    AGENTS --> LLM
    AGENTS --> TOOLS

    TOOLS --> INTERFACES
    TOOLS --> ERRORS

    MULTI --> AGENTS
    MULTI --> NATS

    LLM --> OPENAI
    LLM --> ANTHROPIC
    LLM --> GEMINI
    LLM --> DEEPSEEK
    LLM --> OLLAMA

    STORE --> REDIS
    STORE --> PG
    STORE --> MYSQL

    CORE --> INTERFACES
    CORE --> ERRORS
    LLM --> INTERFACES
    MEMORY --> INTERFACES
    STORE --> INTERFACES
```

## 核心模块依赖图

```mermaid
graph LR
    subgraph "基础层 (L1)"
        I[interfaces]
        E[errors]
        U[utils]
        C[cache]
        O[options]
    end

    subgraph "业务层 (L2)"
        CO[core]
        B[builder]
        L[llm]
        M[memory]
        S[store]
        OB[observability]
    end

    subgraph "实现层 (L3)"
        A[agents]
        T[tools]
        MW[middleware]
        MA[multiagent]
        P[parsers]
        ST[stream]
    end

    CO --> I
    CO --> E
    CO --> C

    B --> CO
    B --> L
    B --> T
    B --> M
    B --> S

    L --> I
    L --> E
    L --> U

    M --> I
    M --> E
    M --> C

    S --> I
    S --> E

    A --> CO
    A --> L
    A --> T
    A --> I

    T --> I
    T --> E

    MW --> CO
    MW --> I

    MA --> A
    MA --> CO
    MA --> ST

    P --> I
    P --> U

    ST --> I
    ST --> E
```

## Agent 类型关系图

```mermaid
classDiagram
    class Runnable {
        <<interface>>
        +Invoke(ctx, input) Output
        +Stream(ctx, input) chan
    }

    class Agent {
        <<interface>>
        +Name() string
        +Description() string
        +Capabilities() []string
        +Plan(ctx, input) Plan
    }

    class BaseAgent {
        -name string
        -description string
        -capabilities []string
        +Invoke(ctx, input) Output
        +InvokeFast(ctx, input) Output
        +Stream(ctx, input) chan
        +Batch(ctx, inputs) []Output
    }

    class ChainableAgent {
        -agents []Agent
        +Invoke(ctx, input) Output
    }

    class ConfigurableAgent {
        -llmClient Client
        -tools []Tool
        -middlewares []Middleware
        -memoryManager MemoryManager
        +Initialize(ctx) error
        +Invoke(ctx, input) Output
    }

    class ReActAgent {
        -llm Client
        -tools []Tool
        -maxIterations int
        +Think(ctx, input) Thought
        +Act(ctx, thought) Action
        +Observe(ctx, action) Observation
    }

    class CoTAgent {
        -llm Client
        -chainLength int
        +GenerateChain(ctx, input) []Thought
    }

    class ToTAgent {
        -llm Client
        -branchFactor int
        -maxDepth int
        +Explore(ctx, input) Tree
    }

    class ExecutorAgent {
        -tools []Tool
        -executor ToolExecutor
        +ExecuteTool(ctx, name, args) Result
    }

    Runnable <|-- Agent
    Agent <|.. BaseAgent
    BaseAgent <|-- ChainableAgent
    BaseAgent <|-- ConfigurableAgent
    BaseAgent <|-- ReActAgent
    BaseAgent <|-- CoTAgent
    BaseAgent <|-- ToTAgent
    BaseAgent <|-- ExecutorAgent
```

## Tool 系统类图

```mermaid
classDiagram
    class Tool {
        <<interface>>
        +Name() string
        +Description() string
        +Invoke(ctx, input) ToolOutput
        +ArgsSchema() string
    }

    class ValidatableTool {
        <<interface>>
        +Validate(ctx, input) error
    }

    class ToolExecutor {
        <<interface>>
        +ExecuteTool(ctx, name, args) ToolResult
        +ListTools() []Tool
    }

    class BaseTool {
        -name string
        -description string
        -argsSchema string
        -runFunc func
        +Name() string
        +Description() string
        +Invoke(ctx, input) ToolOutput
        +ArgsSchema() string
    }

    class FunctionTool {
        -fn func
        +Invoke(ctx, input) ToolOutput
    }

    class Registry {
        -tools map
        -mu RWMutex
        +Register(tool) error
        +Get(name) Tool
        +List() []Tool
        +Names() []string
    }

    class ShellTool {
        +Execute(ctx, cmd) Result
    }

    class Calculator {
        +Calculate(ctx, expr) Result
    }

    class WebScraper {
        +Scrape(ctx, url) Result
    }

    class APICaller {
        +Call(ctx, request) Response
    }

    class DatabaseQuery {
        +Query(ctx, sql) Result
    }

    Tool <|-- ValidatableTool
    Tool <|.. BaseTool
    BaseTool <|-- FunctionTool
    BaseTool <|-- ShellTool
    BaseTool <|-- Calculator
    BaseTool <|-- WebScraper
    BaseTool <|-- APICaller
    BaseTool <|-- DatabaseQuery
    ToolExecutor o-- Tool
    Registry o-- Tool
```

## LLM Provider 类图

```mermaid
classDiagram
    class Client {
        <<interface>>
        +Complete(ctx, req) Response
        +Chat(ctx, messages) Response
        +Provider() Provider
        +IsAvailable() bool
    }

    class BaseProvider {
        #apiKey string
        #baseURL string
        #model string
        #httpClient *http.Client
        +Complete(ctx, req) Response
        +Chat(ctx, messages) Response
    }

    class OpenAIProvider {
        -client *openai.Client
        +Complete(ctx, req) Response
        +Chat(ctx, messages) Response
        +Provider() Provider
    }

    class AnthropicProvider {
        -client *anthropic.Client
        +Complete(ctx, req) Response
        +Provider() Provider
    }

    class GeminiProvider {
        -client *genai.Client
        +Complete(ctx, req) Response
        +Provider() Provider
    }

    class OllamaProvider {
        -baseURL string
        +Complete(ctx, req) Response
        +Provider() Provider
    }

    class DeepSeekProvider {
        +Complete(ctx, req) Response
        +Provider() Provider
    }

    class CohereProvider {
        +Complete(ctx, req) Response
        +Provider() Provider
    }

    class KimiProvider {
        +Complete(ctx, req) Response
        +Provider() Provider
    }

    class SiliconFlowProvider {
        +Complete(ctx, req) Response
        +Provider() Provider
    }

    Client <|.. BaseProvider
    BaseProvider <|-- OpenAIProvider
    BaseProvider <|-- AnthropicProvider
    BaseProvider <|-- GeminiProvider
    BaseProvider <|-- OllamaProvider
    BaseProvider <|-- DeepSeekProvider
    BaseProvider <|-- CohereProvider
    BaseProvider <|-- KimiProvider
    BaseProvider <|-- SiliconFlowProvider
```

## 中间件系统类图

```mermaid
classDiagram
    class Middleware {
        <<interface>>
        +Name() string
        +OnBefore(ctx, req) req
        +OnAfter(ctx, resp) resp
        +OnError(ctx, err) error
    }

    class MiddlewareChain {
        -middlewares []Middleware
        -handler Handler
        +Use(mw ...Middleware) Chain
        +Execute(ctx, req) Response
        +Size() int
    }

    class BaseMiddleware {
        -name string
        +Name() string
        +OnBefore(ctx, req) req
        +OnAfter(ctx, resp) resp
        +OnError(ctx, err) error
    }

    class LoggingMiddleware {
        -logger func
        +OnBefore(ctx, req) req
        +OnAfter(ctx, resp) resp
    }

    class TimingMiddleware {
        -timings map
        +OnBefore(ctx, req) req
        +OnAfter(ctx, resp) resp
        +GetTimings() map
        +GetAverageLatency() Duration
    }

    class RetryMiddleware {
        -maxRetries int
        -backoff Duration
        -condition func
        +OnError(ctx, err) error
    }

    class CacheMiddleware {
        -cache *ShardedCache
        -ttl Duration
        +OnBefore(ctx, req) req
        +OnAfter(ctx, resp) resp
        +Clear()
        +Size() int
    }

    class RateLimitMiddleware {
        -limiter *rate.Limiter
        +OnBefore(ctx, req) req
    }

    Middleware <|.. BaseMiddleware
    BaseMiddleware <|-- LoggingMiddleware
    BaseMiddleware <|-- TimingMiddleware
    BaseMiddleware <|-- RetryMiddleware
    BaseMiddleware <|-- CacheMiddleware
    BaseMiddleware <|-- RateLimitMiddleware
    MiddlewareChain o-- Middleware
```

## Memory 和 Store 系统类图

```mermaid
classDiagram
    class MemoryManager {
        <<interface>>
        +AddConversation(ctx, conv) error
        +GetConversationHistory(ctx, sessionID, limit) []Conversation
        +ClearConversation(ctx, sessionID) error
        +AddCase(ctx, case) error
        +SearchSimilarCases(ctx, query, limit) []Case
        +Store(ctx, key, value) error
        +Retrieve(ctx, key) interface
        +Delete(ctx, key) error
        +Clear(ctx) error
    }

    class Store {
        <<interface>>
        +Get(ctx, key) interface
        +Set(ctx, key, value) error
        +Delete(ctx, key) error
        +Clear(ctx) error
    }

    class VectorStore {
        <<interface>>
        +SimilaritySearch(ctx, query, topK) []Document
        +SimilaritySearchWithScore(ctx, query, topK) []Document
        +AddDocuments(ctx, docs) error
        +Delete(ctx, ids) error
    }

    class Checkpointer {
        <<interface>>
        +SaveCheckpoint(ctx, checkpoint) error
        +LoadCheckpoint(ctx, checkpointID) Checkpoint
        +ListCheckpoints(ctx, threadID, limit) []Metadata
        +DeleteCheckpoint(ctx, checkpointID) error
    }

    class DefaultManager {
        -conversationStore ConversationMemory
        -caseStore CaseMemory
        -kvStore Store
    }

    class MemoryStore {
        -data map
        -mu RWMutex
    }

    class RedisStore {
        -client *redis.Client
        -prefix string
    }

    class PostgresStore {
        -db *gorm.DB
    }

    class MemoryCheckpointer {
        -checkpoints map
        -mu RWMutex
    }

    class RedisCheckpointer {
        -client *redis.Client
    }

    MemoryManager <|.. DefaultManager
    Store <|.. MemoryStore
    Store <|.. RedisStore
    Store <|.. PostgresStore
    Checkpointer <|.. MemoryCheckpointer
    Checkpointer <|.. RedisCheckpointer
    DefaultManager o-- Store
    DefaultManager o-- VectorStore
```

## Builder 模式类图

```mermaid
classDiagram
    class AgentBuilder~C,S~ {
        -llmClient Client
        -tools []Tool
        -systemPrompt string
        -state S
        -store Store
        -checkpointer Checkpointer
        -middlewares []Middleware
        -memoryManager MemoryManager
        -config *AgentConfig
        -callbacks []Callback
        +WithTools(tools) Builder
        +WithSystemPrompt(prompt) Builder
        +WithMiddleware(mw) Builder
        +WithMemoryManager(manager) Builder
        +WithStore(store) Builder
        +WithCheckpointer(cp) Builder
        +WithConfig(config) Builder
        +Build() ConfigurableAgent
    }

    class SimpleAgentBuilder {
        <<type alias>>
        AgentBuilder~any, *AgentState~
    }

    class AgentConfig {
        +MaxIterations int
        +Timeout Duration
        +EnableStreaming bool
        +EnableAutoSave bool
        +SaveInterval Duration
        +MaxTokens int
        +Temperature float64
        +SessionID string
        +Verbose bool
        +MaxConversationHistory int
        +OutputFormat OutputFormat
    }

    class ConfigurableAgent~C,S~ {
        -llmClient Client
        -tools []Tool
        -systemPrompt string
        -runtime *Runtime
        -chain *MiddlewareChain
        -config *AgentConfig
        -memoryManager MemoryManager
        +Initialize(ctx) error
        +Invoke(ctx, input) Output
        +Stream(ctx, input) chan
    }

    AgentBuilder --> ConfigurableAgent : creates
    AgentBuilder --> AgentConfig : uses
    SimpleAgentBuilder --|> AgentBuilder
```

## 数据流图

```mermaid
flowchart LR
    subgraph Input
        UI[用户输入]
        API[API 请求]
    end

    subgraph Processing
        B[Builder]
        A[Agent]
        MW[Middleware]
        LLM[LLM Client]
        T[Tools]
    end

    subgraph Storage
        M[Memory]
        S[Store]
        CP[Checkpoint]
    end

    subgraph Output
        R[响应]
        ST[流式输出]
    end

    UI --> B
    API --> B
    B --> A
    A --> MW
    MW --> LLM
    MW --> T
    LLM --> MW
    T --> MW
    MW --> A
    A --> M
    A --> S
    A --> CP
    M --> A
    S --> A
    CP --> A
    A --> R
    A --> ST
```

## 包导入层级图

```mermaid
graph TD
    L4[第4层: examples/, tests] --> L3
    L4 --> L2
    L4 --> L1

    L3[第3层: agents/, tools/, middleware/, multiagent/] --> L2
    L3 --> L1

    L2[第2层: core/, builder/, llm/, memory/, store/] --> L1

    L1[第1层: interfaces/, errors/, utils/, cache/]

    style L1 fill:#e1f5fe,stroke:#01579b
    style L2 fill:#fff3e0,stroke:#e65100
    style L3 fill:#f3e5f5,stroke:#4a148c
    style L4 fill:#e8f5e9,stroke:#1b5e20
```

## 相关文档

- [架构概述](ARCHITECTURE.md)
- [导入层级说明](IMPORT_LAYERING.md)
- [设计概述](../design/DESIGN_OVERVIEW.md)
