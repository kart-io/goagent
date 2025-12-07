# GoAgent 核心 API 参考

本文档提供 GoAgent 框架核心 API 的完整参考。

## 目录

- [Agent 接口](#agent-接口)
- [Runnable 接口](#runnable-接口)
- [Builder API](#builder-api)
- [State 管理](#state-管理)
- [Checkpoint API](#checkpoint-api)

---

## Agent 接口

### interfaces.Agent

Agent 是能够处理输入并产生输出的自主智能体核心接口。

```go
package interfaces

type Agent interface {
    Runnable

    // Name 返回智能体的标识符
    Name() string

    // Description 返回智能体的功能描述
    Description() string

    // Capabilities 返回智能体的能力列表
    Capabilities() []string

    // Plan 为给定输入生成执行计划
    Plan(ctx context.Context, input *Input) (*Plan, error)
}
```

**实现位置：**
- `core.BaseAgent` - 基础实现
- `agents/executor.ExecutorAgent` - 工具执行智能体
- `agents/react.ReactAgent` - ReAct 推理智能体

### interfaces.Input

```go
type Input struct {
    // Messages 是对话历史或输入消息
    Messages []Message `json:"messages"`

    // State 是持久状态数据
    State State `json:"state"`

    // Config 包含运行时配置选项
    Config map[string]interface{} `json:"config,omitempty"`
}
```

### interfaces.Output

```go
type Output struct {
    // Messages 是生成或修改的消息
    Messages []Message `json:"messages"`

    // State 是更新的状态数据
    State State `json:"state"`

    // Metadata 包含关于执行的额外信息
    Metadata map[string]interface{} `json:"metadata,omitempty"`
}
```

### interfaces.Message

```go
type Message struct {
    // Role 标识消息发送者（user、assistant、system、function）
    Role string `json:"role"`

    // Content 是消息的文本内容
    Content string `json:"content"`

    // Name 是发送者的可选名称
    Name string `json:"name,omitempty"`
}
```

### interfaces.Plan

```go
type Plan struct {
    // Steps 是要执行的有序动作列表
    Steps []Step `json:"steps"`

    // Metadata 包含规划元数据
    Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type Step struct {
    // Action 是要执行的动作
    Action string `json:"action"`

    // Input 包含动作的参数
    Input map[string]interface{} `json:"input"`

    // ToolName 是要调用的工具名称
    ToolName string `json:"tool_name,omitempty"`
}
```

---

## Runnable 接口

### interfaces.Runnable

Runnable 是所有可执行组件的基础接口。

```go
type Runnable interface {
    // Invoke 同步执行
    Invoke(ctx context.Context, input *Input) (*Output, error)

    // Stream 流式执行
    Stream(ctx context.Context, input *Input) (<-chan *StreamChunk, error)
}
```

### interfaces.StreamChunk

```go
type StreamChunk struct {
    // Content 是数据块内容
    Content string `json:"content"`

    // Metadata 包含上下文信息
    Metadata map[string]interface{} `json:"metadata,omitempty"`

    // Done 指示是否是最后一个数据块
    Done bool `json:"done"`
}
```

---

## Builder API

### builder.AgentBuilder

AgentBuilder 提供流畅的 API 来构建 Agent。

```go
package builder

// AgentBuilder 泛型构建器
type AgentBuilder[C any, S core.State] struct {
    // 内部字段省略
}

// NewAgentBuilder 创建新的构建器
func NewAgentBuilder[C any, S core.State](llmClient llm.Client) *AgentBuilder[C, S]

// NewSimpleBuilder 创建简化的构建器（推荐）
func NewSimpleBuilder(llmClient llm.Client) *SimpleAgentBuilder
```

### 构建器方法

```go
// WithSystemPrompt 设置系统提示词
func (b *AgentBuilder[C, S]) WithSystemPrompt(prompt string) *AgentBuilder[C, S]

// WithTools 设置工具列表
func (b *AgentBuilder[C, S]) WithTools(tools ...interfaces.Tool) *AgentBuilder[C, S]

// WithMiddleware 添加中间件
func (b *AgentBuilder[C, S]) WithMiddleware(mw ...middleware.Middleware) *AgentBuilder[C, S]

// WithMemoryManager 设置内存管理器
func (b *AgentBuilder[C, S]) WithMemoryManager(manager interfaces.MemoryManager) *AgentBuilder[C, S]

// WithStore 设置存储后端
func (b *AgentBuilder[C, S]) WithStore(store store.Store) *AgentBuilder[C, S]

// WithCheckpointer 设置检查点管理器
func (b *AgentBuilder[C, S]) WithCheckpointer(cp checkpoint.Checkpointer) *AgentBuilder[C, S]

// WithState 设置初始状态
func (b *AgentBuilder[C, S]) WithState(state S) *AgentBuilder[C, S]

// WithContext 设置上下文
func (b *AgentBuilder[C, S]) WithContext(ctx C) *AgentBuilder[C, S]

// WithCallbacks 设置回调函数
func (b *AgentBuilder[C, S]) WithCallbacks(callbacks ...core.Callback) *AgentBuilder[C, S]

// WithErrorHandler 设置错误处理器
func (b *AgentBuilder[C, S]) WithErrorHandler(handler func(error) error) *AgentBuilder[C, S]

// WithMetadata 设置元数据
func (b *AgentBuilder[C, S]) WithMetadata(key string, value interface{}) *AgentBuilder[C, S]

// WithConfig 设置配置
func (b *AgentBuilder[C, S]) WithConfig(config *AgentConfig) *AgentBuilder[C, S]

// Build 构建 Agent
func (b *AgentBuilder[C, S]) Build() (*ConfigurableAgent[C, S], error)
```

### builder.AgentConfig

```go
type AgentConfig struct {
    // MaxIterations 最大迭代次数
    MaxIterations int

    // Timeout 超时时间
    Timeout time.Duration

    // EnableStreaming 启用流式响应
    EnableStreaming bool

    // EnableAutoSave 启用自动保存
    EnableAutoSave bool

    // SaveInterval 保存间隔
    SaveInterval time.Duration

    // MaxTokens 最大 Token 数
    MaxTokens int

    // Temperature 温度参数
    Temperature float64

    // SessionID 会话 ID
    SessionID string

    // Verbose 详细模式
    Verbose bool

    // MaxConversationHistory 最大对话历史数
    MaxConversationHistory int

    // OutputFormat 输出格式
    OutputFormat OutputFormat

    // CustomOutputPrompt 自定义输出提示
    CustomOutputPrompt string
}

// DefaultAgentConfig 返回默认配置
func DefaultAgentConfig() *AgentConfig
```

### builder.OutputFormat

```go
type OutputFormat string

const (
    OutputFormatDefault   OutputFormat = ""
    OutputFormatPlainText OutputFormat = "plain_text"
    OutputFormatMarkdown  OutputFormat = "markdown"
    OutputFormatJSON      OutputFormat = "json"
    OutputFormatCustom    OutputFormat = "custom"
)
```

### 使用示例

```go
import (
    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/llm/providers"
)

// 创建 LLM 客户端
llmClient, _ := providers.NewOpenAIWithOptions(
    providers.WithAPIKey("your-api-key"),
    providers.WithModel("gpt-4"),
)

// 使用 Builder 创建 Agent
agent, err := builder.NewSimpleBuilder(llmClient).
    WithSystemPrompt("你是一个有帮助的助手").
    WithTools(calculatorTool, searchTool).
    WithMiddleware(loggingMW).
    WithConfig(&builder.AgentConfig{
        MaxTokens:   2000,
        Temperature: 0.7,
        SessionID:   "session-123",
    }).
    Build()

if err != nil {
    log.Fatal(err)
}

// 使用 Agent
output, err := agent.Invoke(ctx, &interfaces.Input{
    Messages: []interfaces.Message{
        {Role: "user", Content: "你好"},
    },
})
```

---

## State 管理

### interfaces.State

```go
type State map[string]interface{}
```

### core.AgentState

```go
package core

type AgentState struct {
    data map[string]interface{}
    mu   sync.RWMutex
}

// NewAgentState 创建新的状态
func NewAgentState() *AgentState

// Get 获取值
func (s *AgentState) Get(key string) (interface{}, bool)

// Set 设置值
func (s *AgentState) Set(key string, value interface{})

// Delete 删除值
func (s *AgentState) Delete(key string)

// Keys 返回所有键
func (s *AgentState) Keys() []string

// ToMap 转换为 map
func (s *AgentState) ToMap() map[string]interface{}

// FromMap 从 map 加载
func (s *AgentState) FromMap(data map[string]interface{})
```

---

## Checkpoint API

### interfaces.Checkpointer

```go
type Checkpointer interface {
    // SaveCheckpoint 保存检查点
    SaveCheckpoint(ctx context.Context, checkpoint *Checkpoint) error

    // LoadCheckpoint 加载检查点
    LoadCheckpoint(ctx context.Context, checkpointID string) (*Checkpoint, error)

    // ListCheckpoints 列出检查点
    ListCheckpoints(ctx context.Context, threadID string, limit int) ([]*CheckpointMetadata, error)

    // DeleteCheckpoint 删除检查点
    DeleteCheckpoint(ctx context.Context, checkpointID string) error
}
```

### interfaces.Checkpoint

```go
type Checkpoint struct {
    // ID 检查点唯一标识符
    ID string `json:"id"`

    // ThreadID 线程/会话标识符
    ThreadID string `json:"thread_id"`

    // State 检查点时的状态
    State State `json:"state"`

    // Metadata 额外信息
    Metadata map[string]interface{} `json:"metadata,omitempty"`

    // CreatedAt 创建时间
    CreatedAt time.Time `json:"created_at"`
}
```

### interfaces.CheckpointMetadata

```go
type CheckpointMetadata struct {
    ID        string                 `json:"id"`
    ThreadID  string                 `json:"thread_id"`
    CreatedAt time.Time              `json:"created_at"`
    UpdatedAt time.Time              `json:"updated_at"`
    Metadata  map[string]interface{} `json:"metadata,omitempty"`
    Size      int64                  `json:"size"`
}
```

### 检查点实现

```go
package checkpoint

// NewInMemorySaver 创建内存检查点
func NewInMemorySaver() Checkpointer

// NewRedisCheckpointer 创建 Redis 检查点
func NewRedisCheckpointer(client *redis.Client) Checkpointer

// NewDistributedCheckpointer 创建分布式检查点
func NewDistributedCheckpointer(opts ...Option) Checkpointer
```

### 使用示例

```go
import (
    "github.com/kart-io/goagent/core/checkpoint"
    "github.com/kart-io/goagent/interfaces"
)

// 创建检查点管理器
cp := checkpoint.NewInMemorySaver()

// 保存检查点
err := cp.SaveCheckpoint(ctx, &interfaces.Checkpoint{
    ID:       "ckpt-001",
    ThreadID: "thread-123",
    State: interfaces.State{
        "step": 5,
        "data": "some data",
    },
})

// 加载检查点
loaded, err := cp.LoadCheckpoint(ctx, "ckpt-001")

// 列出检查点
list, err := cp.ListCheckpoints(ctx, "thread-123", 10)
```

---

## 相关文档

- [Tool API 参考](TOOL_API.md)
- [LLM API 参考](LLM_API.md)
- [Middleware API 参考](MIDDLEWARE_API.md)
- [使用指南](../guides/QUICKSTART.md)
