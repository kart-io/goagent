# LangChain-Inspired Improvement Plan for pkg/agent

## Executive Summary

基于 LangChain Python 最新版本的设计理念，为 `pkg/agent/` 目录制定改进计划。当前实现已完成 3 个阶段重构，达到 100% 核心功能覆盖。本计划聚焦于添加 LangChain v1.0 的高级特性。

## 当前状态评估

### ✅ 已实现特性

- **Runnable Pattern** - 统一执行接口
- **Builder Pattern** - 流式 API
- **Basic Agents** - ReAct、ToolCalling、OpenAI Functions
- **Memory Systems** - ConversationBuffer、Summary、KG
- **Store Pattern** - Memory/Redis/PostgreSQL 后端
- **Middleware** - 10+ 中间件类型
- **Streaming** - 基础流式支持

### ❌ 缺失的 LangChain 高级特性

1. **ToolRuntime Pattern** - 工具内访问 agent 状态
2. **LangGraph Store** - 长期记忆存储
3. **多模式 Streaming** - messages/updates/custom 模式
4. **Supervisor Agent** - 多 agent 协调
5. **并行工具执行** - 同时调用多个工具
6. **LLM Tool Selector** - 智能工具选择
7. **动态提示中间件** - 运行时提示生成

## 改进方案

### Phase 1: 核心增强 (1-2 周)

#### 1.1 实现 ToolRuntime Pattern

```go
// tools/runtime.go
package tools

import (
    "context"
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/store"
)

// ToolRuntime provides access to agent state and context from within tools
type ToolRuntime struct {
    State       core.State              // Agent's current state
    Context     context.Context         // Request context
    Store       store.Store            // Long-term memory store
    ToolCallID  string                 // Current tool call ID
    StreamWriter func(interface{})      // Stream custom data
}

// Tool interface with runtime support
type RuntimeTool interface {
    Tool
    ExecuteWithRuntime(ctx context.Context, input interface{}, runtime *ToolRuntime) (interface{}, error)
}

// Example tool implementation
type UserInfoTool struct {
    BaseTool
}

func (t *UserInfoTool) ExecuteWithRuntime(ctx context.Context, input interface{}, runtime *ToolRuntime) (interface{}, error) {
    // Access agent state
    userID := runtime.State.Get("user_id").(string)

    // Access long-term store
    userInfo, err := runtime.Store.Get(ctx, []string{"users"}, userID)
    if err != nil {
        return nil, err
    }

    // Stream progress
    runtime.StreamWriter(map[string]interface{}{
        "status": "Looking up user info",
        "user_id": userID,
    })

    return userInfo, nil
}
```

#### 1.2 添加 LangGraph Store 支持

```go
// store/langgraph_store.go
package store

import (
    "context"
    "time"
)

// StoreValue represents a stored value with metadata
type StoreValue struct {
    Value     interface{}
    Metadata  map[string]interface{}
    Timestamp time.Time
}

// LangGraphStore interface for long-term memory
type LangGraphStore interface {
    // Put stores a value at the specified namespace and key
    Put(ctx context.Context, namespace []string, key string, value interface{}) error

    // Get retrieves a value from the specified namespace and key
    Get(ctx context.Context, namespace []string, key string) (*StoreValue, error)

    // Search performs similarity search within a namespace
    Search(ctx context.Context, namespace []string, query string, limit int) ([]*StoreValue, error)

    // Delete removes a value
    Delete(ctx context.Context, namespace []string, key string) error

    // List returns all keys in a namespace
    List(ctx context.Context, namespace []string) ([]string, error)
}

// InMemoryLangGraphStore for development
type InMemoryLangGraphStore struct {
    data map[string]map[string]*StoreValue
    mu   sync.RWMutex
}

func (s *InMemoryLangGraphStore) Put(ctx context.Context, namespace []string, key string, value interface{}) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    ns := strings.Join(namespace, ":")
    if s.data[ns] == nil {
        s.data[ns] = make(map[string]*StoreValue)
    }

    s.data[ns][key] = &StoreValue{
        Value:     value,
        Timestamp: time.Now(),
        Metadata:  map[string]interface{}{"namespace": namespace},
    }
    return nil
}
```

### Phase 2: 流式增强 (1 周)

#### 2.1 多模式 Streaming

```go
// stream/modes.go
package stream

type StreamMode string

const (
    StreamModeMessages StreamMode = "messages" // Stream LLM tokens
    StreamModeUpdates  StreamMode = "updates"  // Stream state updates
    StreamModeCustom   StreamMode = "custom"   // Stream custom data
    StreamModeValues   StreamMode = "values"   // Stream full state
)

// StreamConfig for multi-mode streaming
type StreamConfig struct {
    Modes    []StreamMode
    Callback func(mode StreamMode, data interface{})
}

// MultiModeStream handles different streaming modes
type MultiModeStream struct {
    config   *StreamConfig
    channels map[StreamMode]chan interface{}
}

func (s *MultiModeStream) Stream(mode StreamMode, data interface{}) {
    if ch, ok := s.channels[mode]; ok {
        select {
        case ch <- data:
        default:
            // Non-blocking send
        }
    }
}

// Agent with multi-mode streaming
func (a *Agent) StreamWithModes(ctx context.Context, input interface{}, config *StreamConfig) (<-chan StreamEvent, error) {
    stream := NewMultiModeStream(config)

    go func() {
        // Stream messages mode
        if contains(config.Modes, StreamModeMessages) {
            for token := range a.llm.StreamTokens(ctx, input) {
                stream.Stream(StreamModeMessages, token)
            }
        }

        // Stream updates mode
        if contains(config.Modes, StreamModeUpdates) {
            for update := range a.state.Updates() {
                stream.Stream(StreamModeUpdates, update)
            }
        }
    }()

    return stream.Output(), nil
}
```

#### 2.2 流式工具调用

```go
// tools/streaming.go
package tools

// StreamingToolCall for progressive tool execution
type StreamingToolCall struct {
    ID       string
    Name     string
    Args     map[string]interface{}
    Chunks   chan ToolChunk
}

type ToolChunk struct {
    Type  string // "name", "args", "output"
    Value interface{}
}

// StreamingTool interface
type StreamingTool interface {
    Tool
    StreamExecute(ctx context.Context, input interface{}) (<-chan ToolChunk, error)
}

// Example streaming tool
type SearchTool struct {
    BaseTool
}

func (t *SearchTool) StreamExecute(ctx context.Context, input interface{}) (<-chan ToolChunk, error) {
    chunks := make(chan ToolChunk, 100)

    go func() {
        defer close(chunks)

        // Stream tool metadata
        chunks <- ToolChunk{Type: "name", Value: t.Name}
        chunks <- ToolChunk{Type: "args", Value: input}

        // Stream search progress
        chunks <- ToolChunk{Type: "output", Value: "Searching databases..."}
        time.Sleep(100 * time.Millisecond)

        chunks <- ToolChunk{Type: "output", Value: "Found 10 results..."}
        time.Sleep(100 * time.Millisecond)

        // Stream final results
        chunks <- ToolChunk{Type: "output", Value: "Complete results here"}
    }()

    return chunks, nil
}
```

### Phase 3: 高级 Agent 模式 (2 周)

#### 3.1 Supervisor Agent Pattern

```go
// agents/supervisor.go
package agents

import (
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/tools"
)

// SupervisorAgent coordinates multiple sub-agents
type SupervisorAgent struct {
    *core.BaseAgent
    SubAgents map[string]core.Agent
    Router    AgentRouter
}

// AgentRouter decides which sub-agent to use
type AgentRouter interface {
    Route(ctx context.Context, input interface{}) (string, error)
}

// LLMRouter uses an LLM to route requests
type LLMRouter struct {
    llm core.LLM
}

func (r *LLMRouter) Route(ctx context.Context, input interface{}) (string, error) {
    prompt := fmt.Sprintf(`
        Given the user request: %v

        Available agents:
        - calendar_agent: Handles scheduling and calendar operations
        - email_agent: Manages email sending and notifications
        - search_agent: Performs web and database searches

        Which agent should handle this request? Return only the agent name.
    `, input)

    response, err := r.llm.Generate(ctx, prompt)
    if err != nil {
        return "", err
    }

    return strings.TrimSpace(response), nil
}

func (s *SupervisorAgent) Run(ctx context.Context, input interface{}) (*core.AgentOutput, error) {
    // Route to appropriate sub-agent
    agentName, err := s.Router.Route(ctx, input)
    if err != nil {
        return nil, err
    }

    subAgent, ok := s.SubAgents[agentName]
    if !ok {
        return nil, fmt.Errorf("unknown agent: %s", agentName)
    }

    // Wrap sub-agent as tool for unified interface
    agentTool := &AgentTool{
        Agent: subAgent,
        Name:  agentName,
    }

    // Execute sub-agent
    result, err := agentTool.Execute(ctx, input)
    if err != nil {
        return nil, err
    }

    return &core.AgentOutput{
        Result: result,
        Metadata: map[string]interface{}{
            "sub_agent": agentName,
        },
    }, nil
}

// AgentTool wraps an agent as a tool
type AgentTool struct {
    Agent core.Agent
    Name  string
}

func (t *AgentTool) Execute(ctx context.Context, input interface{}) (interface{}, error) {
    output, err := t.Agent.Run(ctx, input)
    if err != nil {
        return nil, err
    }
    return output.Result, nil
}
```

#### 3.2 并行工具执行

```go
// tools/parallel.go
package tools

import (
    "context"
    "sync"
)

// ParallelToolExecutor executes multiple tools concurrently
type ParallelToolExecutor struct {
    MaxConcurrency int
}

// ToolCall represents a single tool invocation
type ToolCall struct {
    Tool   Tool
    Input  interface{}
    ID     string
}

// ToolResult contains the result of a tool execution
type ToolResult struct {
    ID     string
    Output interface{}
    Error  error
}

func (e *ParallelToolExecutor) ExecuteParallel(ctx context.Context, calls []ToolCall) []ToolResult {
    results := make([]ToolResult, len(calls))

    // Use semaphore to limit concurrency
    sem := make(chan struct{}, e.MaxConcurrency)
    var wg sync.WaitGroup

    for i, call := range calls {
        wg.Add(1)
        go func(idx int, tc ToolCall) {
            defer wg.Done()

            sem <- struct{}{}        // Acquire
            defer func() { <-sem }() // Release

            output, err := tc.Tool.Execute(ctx, tc.Input)
            results[idx] = ToolResult{
                ID:     tc.ID,
                Output: output,
                Error:  err,
            }
        }(i, call)
    }

    wg.Wait()
    return results
}

// Agent with parallel tool support
func (a *Agent) RunWithParallelTools(ctx context.Context, input interface{}) (*AgentOutput, error) {
    // LLM generates multiple tool calls
    toolCalls := a.llm.GenerateToolCalls(ctx, input)

    // Check if tools can be executed in parallel
    if a.canRunParallel(toolCalls) {
        executor := &ParallelToolExecutor{MaxConcurrency: 5}
        results := executor.ExecuteParallel(ctx, toolCalls)

        // Process results
        return a.processParallelResults(results)
    }

    // Fall back to sequential execution
    return a.runSequential(ctx, toolCalls)
}
```

### Phase 4: 智能中间件 (1 周)

#### 4.1 LLM Tool Selector Middleware

```go
// middleware/tool_selector.go
package middleware

import (
    "context"
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/tools"
)

// LLMToolSelectorMiddleware intelligently selects relevant tools
type LLMToolSelectorMiddleware struct {
    Model         core.LLM    // Cheaper model for selection
    MaxTools      int         // Maximum tools to select
    AlwaysInclude []string    // Tools to always include
}

func (m *LLMToolSelectorMiddleware) Process(ctx context.Context, state core.State) (core.State, error) {
    // Get all available tools
    allTools := state.Get("tools").([]tools.Tool)
    userQuery := state.Get("query").(string)

    // Build selection prompt
    prompt := m.buildSelectionPrompt(userQuery, allTools)

    // Use cheaper model to select tools
    selectedNames, err := m.Model.Generate(ctx, prompt)
    if err != nil {
        return state, err
    }

    // Filter tools based on selection
    selectedTools := m.filterTools(allTools, selectedNames)

    // Always include specified tools
    selectedTools = m.ensureAlwaysIncluded(selectedTools)

    // Update state with selected tools
    state.Set("tools", selectedTools)
    return state, nil
}

func (m *LLMToolSelectorMiddleware) buildSelectionPrompt(query string, tools []tools.Tool) string {
    toolDescriptions := []string{}
    for _, tool := range tools {
        toolDescriptions = append(toolDescriptions, fmt.Sprintf(
            "- %s: %s", tool.Name(), tool.Description(),
        ))
    }

    return fmt.Sprintf(`
        User Query: %s

        Available Tools:
        %s

        Select up to %d most relevant tools for this query.
        Return tool names as comma-separated list.
    `, query, strings.Join(toolDescriptions, "\n"), m.MaxTools)
}
```

#### 4.2 Dynamic Prompt Middleware

```go
// middleware/dynamic_prompt.go
package middleware

// DynamicPromptMiddleware generates prompts at runtime
type DynamicPromptMiddleware struct {
    PromptGenerator func(ctx context.Context, state core.State) string
}

func (m *DynamicPromptMiddleware) Process(ctx context.Context, state core.State) (core.State, error) {
    // Generate dynamic prompt based on current state
    prompt := m.PromptGenerator(ctx, state)

    // Update system prompt
    state.Set("system_prompt", prompt)

    return state, nil
}

// Example usage
func NewPersonalizedPromptMiddleware() *DynamicPromptMiddleware {
    return &DynamicPromptMiddleware{
        PromptGenerator: func(ctx context.Context, state core.State) string {
            userName := state.Get("user_name").(string)
            userPreferences := state.Get("preferences").(map[string]interface{})

            return fmt.Sprintf(`
                You are a helpful assistant for %s.
                User preferences:
                - Language: %v
                - Timezone: %v
                - Style: %v

                Adjust your responses accordingly.
            `, userName,
                userPreferences["language"],
                userPreferences["timezone"],
                userPreferences["style"],
            )
        },
    }
}
```

### Phase 5: LLM 提供者集成 (1 周)

#### 5.1 统一 LLM 接口

```go
// llm/interface.go
package llm

import (
    "context"
    "github.com/kart-io/goagent/tools"
)

// LLM unified interface
type LLM interface {
    // Basic generation
    Generate(ctx context.Context, prompt string) (string, error)

    // Streaming generation
    Stream(ctx context.Context, prompt string) (<-chan string, error)

    // Tool calling
    GenerateWithTools(ctx context.Context, prompt string, tools []tools.Tool) (*ToolCallResponse, error)

    // Embeddings
    Embed(ctx context.Context, text string) ([]float64, error)

    // Model info
    ModelName() string
    MaxTokens() int
}

// ToolCallResponse from LLM
type ToolCallResponse struct {
    Content   string
    ToolCalls []ToolCall
}

type ToolCall struct {
    ID       string
    Name     string
    Arguments map[string]interface{}
}
```

#### 5.2 OpenAI Provider

```go
// llm/providers/openai.go
package providers

import (
    "github.com/sashabaranov/go-openai"
    "github.com/kart-io/goagent/llm"
)

type OpenAIProvider struct {
    client *openai.Client
    model  string
}

func NewOpenAI(apiKey string, model string) *OpenAIProvider {
    return &OpenAIProvider{
        client: openai.NewClient(apiKey),
        model:  model,
    }
}

func (p *OpenAIProvider) Generate(ctx context.Context, prompt string) (string, error) {
    resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
        Model: p.model,
        Messages: []openai.ChatCompletionMessage{
            {Role: openai.ChatMessageRoleUser, Content: prompt},
        },
    })
    if err != nil {
        return "", err
    }

    return resp.Choices[0].Message.Content, nil
}

func (p *OpenAIProvider) GenerateWithTools(ctx context.Context, prompt string, tools []tools.Tool) (*llm.ToolCallResponse, error) {
    // Convert tools to OpenAI function format
    functions := p.convertToolsToFunctions(tools)

    resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
        Model:     p.model,
        Messages:  []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: prompt}},
        Functions: functions,
    })
    if err != nil {
        return nil, err
    }

    // Parse tool calls from response
    return p.parseToolCalls(resp), nil
}

func (p *OpenAIProvider) Stream(ctx context.Context, prompt string) (<-chan string, error) {
    tokens := make(chan string, 100)

    stream, err := p.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
        Model:    p.model,
        Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: prompt}},
        Stream:   true,
    })
    if err != nil {
        return nil, err
    }

    go func() {
        defer close(tokens)
        for {
            response, err := stream.Recv()
            if err != nil {
                break
            }
            if len(response.Choices) > 0 {
                tokens <- response.Choices[0].Delta.Content
            }
        }
    }()

    return tokens, nil
}
```

## 实施计划

### Week 1-2: 核心增强

- [ ] 实现 ToolRuntime pattern
- [ ] 添加 LangGraph Store
- [ ] 集成到现有 agent 架构

### Week 3: 流式增强

- [ ] 实现多模式 streaming
- [ ] 添加流式工具调用
- [ ] 测试和优化性能

### Week 4-5: 高级 Agent

- [ ] 实现 Supervisor Agent
- [ ] 添加并行工具执行
- [ ] 创建示例应用

### Week 6: 智能中间件

- [ ] 实现 Tool Selector
- [ ] 添加 Dynamic Prompt
- [ ] 集成测试

### Week 7: LLM 提供者

- [ ] 实现统一接口
- [ ] 添加 OpenAI provider
- [ ] 添加 Gemini provider
- [ ] 添加 DeepSeek provider

## 测试策略

### 单元测试

```go
// tools/runtime_test.go
func TestToolRuntimeStateAccess(t *testing.T) {
    runtime := &ToolRuntime{
        State: core.NewState(),
    }
    runtime.State.Set("user_id", "123")

    tool := &UserInfoTool{}
    result, err := tool.ExecuteWithRuntime(context.Background(), nil, runtime)

    assert.NoError(t, err)
    assert.NotNil(t, result)
}
```

### 集成测试

```go
// pkg/agent/integration_test.go
func TestSupervisorAgentIntegration(t *testing.T) {
    supervisor := NewSupervisorAgent(
        WithSubAgents(map[string]Agent{
            "calendar": NewCalendarAgent(),
            "email": NewEmailAgent(),
        }),
        WithRouter(NewLLMRouter()),
    )

    output, err := supervisor.Run(context.Background(), map[string]interface{}{
        "query": "Schedule a meeting tomorrow at 2pm and send invites",
    })

    assert.NoError(t, err)
    assert.Contains(t, output.Metadata, "sub_agent")
}
```

## 性能目标

- **Streaming 延迟**: < 50ms 首个 token
- **并行工具执行**: 5x 速度提升
- **Tool Selection**: 减少 70% token 使用
- **Memory 查询**: < 10ms (Redis backend)

## 文档更新

需要更新的文档：

1. `README.md` - 添加新特性说明
2. `ARCHITECTURE.md` - 更新架构图
3. `examples/` - 添加新示例代码
4. `docs/api/` - 完整 API 参考

## 兼容性保证

所有改进保持向后兼容：

- 现有 API 不变
- 新特性通过选项启用
- 渐进式迁移路径

## 总结

这个改进计划将使 `pkg/agent/` 达到与 LangChain Python v1.0 功能对等，同时保持 Go 的性能优势。重点是：

1. **生产就绪**: 添加关键的生产特性
2. **开发体验**: 提升 API 易用性
3. **性能优化**: 保持 10-100x Python 性能优势
4. **完整功能**: 覆盖所有 LangChain 核心模式

预计总工期 7 周，可以并行开发多个模块。
