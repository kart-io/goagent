# LangChain 设计改进方案

## 实现状态

### ✅ Phase 1: 核心基础设施（已完成）

- **State Management** (`core/state.go`) - ✅ 已实现
- **Runtime & Context** (`core/runtime.go`) - ✅ 已实现
- **Store** (`core/store.go`) - ✅ 已实现
- **Checkpointer** (`core/checkpointer.go`) - ✅ 已实现
- **示例代码** (`example/langchain_phase1/`) - ✅ 已创建

### ✅ Phase 2: 中间件系统（已完成）

- **Middleware Interface** (`core/middleware.go`) - ✅ 已实现
- **Advanced Middleware** (`core/middleware_advanced.go`) - ✅ 已实现
  - DynamicPromptMiddleware - ✅
  - ToolSelectorMiddleware - ✅
  - RateLimiterMiddleware - ✅
  - AuthenticationMiddleware - ✅
  - ValidationMiddleware - ✅
  - TransformMiddleware - ✅
  - CircuitBreakerMiddleware - ✅
  - CacheMiddleware - ✅
- **示例代码** (`example/langchain_phase2/`) - ✅ 已创建

### ✅ Phase 3: 高级特性（已完成）

- **Agent Builder** (`builder/builder.go`) - ✅ 已实现
- **Builder Tests** (`builder/builder_test.go`) - ✅ 已创建
- **完整集成示例** (`example/langchain_complete/`) - ✅ 已创建

## 当前架构优势

✅ **已实现的核心特性：**

1. **Runnable 接口** - 统一的可执行接口（`runnable.go`）

   - `Invoke()` - 单次执行
   - `Stream()` - 流式执行
   - `Batch()` - 批量执行
   - `Pipe()` - 管道连接

2. **组合模式**

   - `RunnableFunc` - 函数包装
   - `RunnablePipe` - 管道组合
   - `RunnableSequence` - 顺序执行

3. **回调系统** - `Callback` 接口支持生命周期钩子

4. **核心抽象**
   - `Agent` - 智能代理
   - `Chain` - 链式处理
   - `Tool` - 工具系统
   - `Memory` - 记忆管理

## 需要补充的 LangChain 特性

### 1. Agent State Management (状态管理)

**LangChain 设计：**

```python
class CustomState(AgentState):
    user_id: str
    user_name: str
    conversation_history: list[Message]
```

**建议实现：**

```go
// core/state.go
package core

import "context"

// State 定义 Agent 的状态接口
//
// 借鉴 LangChain 的 AgentState 设计，提供：
// - 线程安全的状态访问
// - 状态持久化
// - 状态更新追踪
type State interface {
    // Get 获取状态值
    Get(key string) (interface{}, bool)

    // Set 设置状态值
    Set(key string, value interface{})

    // Update 批量更新状态
    Update(updates map[string]interface{})

    // Snapshot 获取状态快照
    Snapshot() map[string]interface{}

    // Clone 克隆状态
    Clone() State
}

// AgentState Agent 状态实现
type AgentState struct {
    state map[string]interface{}
    mu    sync.RWMutex
}

// NewAgentState 创建 Agent 状态
func NewAgentState() *AgentState {
    return &AgentState{
        state: make(map[string]interface{}),
    }
}
```

### 2. Runtime & Context (运行时上下文)

**LangChain 设计：**

```python
@tool
def get_user_info(runtime: ToolRuntime[Context]) -> str:
    user_id = runtime.context.user_id
    store = runtime.store
    state = runtime.state
```

**建议实现：**

```go
// core/runtime.go
package core

// Runtime 定义工具和中间件的运行时环境
//
// 提供对状态、存储、上下文的访问
type Runtime[C, S any] struct {
    // Context 用户自定义上下文
    Context C

    // State Agent 状态
    State S

    // Store 长期存储
    Store Store

    // Checkpointer 会话检查点
    Checkpointer Checkpointer

    // ToolCallID 当前工具调用 ID
    ToolCallID string

    // SessionID 会话 ID
    SessionID string
}

// ToolFunc 工具函数签名，支持运行时访问
type ToolFunc[I, O, C, S any] func(ctx context.Context, input I, runtime *Runtime[C, S]) (O, error)
```

### 3. Store (长期存储)

**LangChain 设计：**

```python
store = InMemoryStore()
store.put(("users",), "user_123", {"name": "John"})
user_info = store.get(("users",), "user_123")
```

**建议实现：**

```go
// core/store.go
package core

// Store 定义长期存储接口
//
// 用于持久化用户数据、偏好设置等
type Store interface {
    // Put 存储数据
    Put(ctx context.Context, namespace []string, key string, value interface{}) error

    // Get 获取数据
    Get(ctx context.Context, namespace []string, key string) (*StoreValue, error)

    // Delete 删除数据
    Delete(ctx context.Context, namespace []string, key string) error

    // Search 搜索数据
    Search(ctx context.Context, namespace []string, filter map[string]interface{}) ([]*StoreValue, error)
}

// StoreValue 存储的值
type StoreValue struct {
    Value    interface{}            `json:"value"`
    Metadata map[string]interface{} `json:"metadata"`
    Created  time.Time              `json:"created"`
    Updated  time.Time              `json:"updated"`
}

// InMemoryStore 内存存储实现
type InMemoryStore struct {
    data map[string]map[string]*StoreValue
    mu   sync.RWMutex
}
```

### 4. Checkpointer (会话持久化)

**LangChain 设计：**

```python
agent = create_agent(
    model="...",
    tools=[...],
    checkpointer=InMemorySaver()
)

agent.invoke(
    {"messages": [...]},
    {"configurable": {"thread_id": "1"}}
)
```

**建议实现：**

```go
// core/checkpointer.go
package core

// Checkpointer 定义检查点接口
//
// 用于保存和恢复会话状态
type Checkpointer interface {
    // Save 保存检查点
    Save(ctx context.Context, threadID string, state State) error

    // Load 加载检查点
    Load(ctx context.Context, threadID string) (State, error)

    // List 列出所有检查点
    List(ctx context.Context) ([]CheckpointInfo, error)

    // Delete 删除检查点
    Delete(ctx context.Context, threadID string) error
}

// CheckpointInfo 检查点信息
type CheckpointInfo struct {
    ThreadID  string    `json:"thread_id"`
    Created   time.Time `json:"created"`
    Updated   time.Time `json:"updated"`
    Metadata  map[string]interface{} `json:"metadata"`
}

// InMemorySaver 内存检查点实现
type InMemorySaver struct {
    checkpoints map[string]State
    mu          sync.RWMutex
}
```

### 5. Middleware (中间件系统)

**LangChain 设计：**

```python
agent = create_agent(
    model="...",
    tools=[...],
    middleware=[
        dynamic_prompt,
        TodoListMiddleware(),
        LLMToolSelectorMiddleware(),
    ]
)
```

**建议实现：**

```go
// core/middleware.go
package core

// Middleware 定义中间件接口
//
// 中间件可以在执行前后拦截和修改请求/响应
type Middleware interface {
    // Name 返回中间件名称
    Name() string

    // OnBefore 执行前钩子
    OnBefore(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error)

    // OnAfter 执行后钩子
    OnAfter(ctx context.Context, response *MiddlewareResponse) (*MiddlewareResponse, error)
}

// MiddlewareRequest 中间件请求
type MiddlewareRequest struct {
    Input   interface{}
    State   State
    Runtime *Runtime[any, any]
}

// MiddlewareResponse 中间件响应
type MiddlewareResponse struct {
    Output interface{}
    State  State
}

// MiddlewareFunc 函数式中间件
type MiddlewareFunc func(ctx context.Context, request *MiddlewareRequest, next Handler) (*MiddlewareResponse, error)

// Handler 处理器函数
type Handler func(ctx context.Context, request *MiddlewareRequest) (*MiddlewareResponse, error)
```

### 6. Agent Builder (创建代理的统一接口)

**LangChain 设计：**

```python
agent = create_agent(
    model="anthropic:claude-sonnet-4-5",
    tools=[get_weather, search],
    system_prompt="You are a helpful assistant",
    checkpointer=InMemorySaver(),
    store=InMemoryStore(),
    context_schema=Context,
    state_schema=CustomState,
    middleware=[...],
)
```

**建议实现：**

```go
// builder/builder.go
package builder

// AgentBuilder Agent 构建器
//
// 提供流式的构建 API
type AgentBuilder[C, S any] struct {
    model          LLMClient
    tools          []Tool
    systemPrompt   string
    checkpointer   Checkpointer
    store          Store
    contextSchema  C
    stateSchema    S
    middleware     []Middleware
    callbacks      []Callback
}

// NewAgentBuilder 创建 Agent 构建器
func NewAgentBuilder[C, S any](model LLMClient) *AgentBuilder[C, S] {
    return &AgentBuilder[C, S]{
        model:      model,
        tools:      []Tool{},
        middleware: []Middleware{},
        callbacks:  []Callback{},
    }
}

// WithTools 设置工具
func (b *AgentBuilder[C, S]) WithTools(tools ...Tool) *AgentBuilder[C, S] {
    b.tools = append(b.tools, tools...)
    return b
}

// WithSystemPrompt 设置系统提示词
func (b *AgentBuilder[C, S]) WithSystemPrompt(prompt string) *AgentBuilder[C, S] {
    b.systemPrompt = prompt
    return b
}

// WithCheckpointer 设置检查点
func (b *AgentBuilder[C, S]) WithCheckpointer(cp Checkpointer) *AgentBuilder[C, S] {
    b.checkpointer = cp
    return b
}

// WithStore 设置存储
func (b *AgentBuilder[C, S]) WithStore(store Store) *AgentBuilder[C, S] {
    b.store = store
    return b
}

// WithMiddleware 添加中间件
func (b *AgentBuilder[C, S]) WithMiddleware(mw ...Middleware) *AgentBuilder[C, S] {
    b.middleware = append(b.middleware, mw...)
    return b
}

// Build 构建 Agent
func (b *AgentBuilder[C, S]) Build() (Agent, error) {
    return NewConfigurableAgent(b)
}
```

## 实现优先级

### Phase 1: 核心基础设施（高优先级）

1. **State Management** (`core/state.go`)

   - AgentState 实现
   - 线程安全的状态访问

2. **Runtime** (`core/runtime.go`)

   - Runtime 结构定义
   - 工具函数签名更新

3. **Store** (`core/store.go`)
   - Store 接口
   - InMemoryStore 实现

### Phase 2: 持久化和中间件（中优先级）

4. **Checkpointer** (`core/checkpointer.go`)

   - Checkpointer 接口
   - InMemorySaver 实现

5. **Middleware** (`core/middleware.go`)
   - Middleware 接口
   - 常用中间件实现

### Phase 3: 高级特性（中低优先级）

6. **Agent Builder** (`builder/builder.go`)

   - 统一的构建接口
   - 链式 API

7. **内置中间件** (`middleware/`)
   - DynamicPromptMiddleware
   - ToolSelectorMiddleware
   - TodoListMiddleware

## 使用示例

### 示例 1: 带状态管理的 Agent

```go
package main

import (
    "context"
    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/core"
)

// 自定义上下文
type MyContext struct {
    UserID string
}

// 自定义状态
type MyState struct {
    *core.AgentState
    UserName string
}

// 定义工具
func getUserInfo(ctx context.Context, input string, runtime *core.Runtime[MyContext, MyState]) (string, error) {
    userID := runtime.Context.UserID

    // 从 Store 获取用户信息
    value, err := runtime.Store.Get(ctx, []string{"users"}, userID)
    if err != nil {
        return "", err
    }

    return value.Value.(string), nil
}

func main() {
    // 创建 Agent
    agent, err := builder.NewAgentBuilder[MyContext, MyState](llmClient).
        WithTools(getUserInfo).
        WithSystemPrompt("You are a helpful assistant").
        WithCheckpointer(core.NewInMemorySaver()).
        WithStore(core.NewInMemoryStore()).
        Build()

    // 执行
    result, err := agent.Invoke(ctx, &core.AgentInput{
        Task: "Get user information",
        Context: MyContext{UserID: "user_123"},
    })
}
```

### 示例 2: 带中间件的 Agent

```go
// 动态提示词中间件
dynamicPrompt := middleware.NewDynamicPromptMiddleware(func(req *core.MiddlewareRequest) string {
    userName := req.State.Get("user_name")
    return fmt.Sprintf("You are assisting %s", userName)
})

// 创建 Agent
agent, _ := builder.NewAgentBuilder[MyContext, MyState](llmClient).
    WithMiddleware(dynamicPrompt).
    WithMiddleware(middleware.NewToolSelectorMiddleware(3)). // 限制 3 个工具
    Build()
```

## 兼容性

所有新特性都向后兼容现有代码：

- 现有的 `Agent`、`Chain`、`Tool` 接口保持不变
- 新特性通过可选参数提供
- 可以逐步迁移到新 API

## 参考资料

- LangChain Python: https://docs.langchain.com/oss/python/langchain
- LangChain Agents: https://docs.langchain.com/oss/python/langchain/agents
- LangChain Memory: https://docs.langchain.com/oss/python/langchain/short-term-memory
- LangGraph Store: https://docs.langchain.com/oss/python/langchain/long-term-memory
