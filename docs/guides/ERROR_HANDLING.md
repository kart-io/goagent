# GoAgent 错误处理最佳实践与迁移指南

## 目录

- [概述](#概述)
- [错误处理架构](#错误处理架构)
- [快速开始](#快速开始)
- [错误代码参考](#错误代码参考)
- [迁移指南](#迁移指南)
- [最佳实践](#最佳实践)
- [常见场景](#常见场景)
- [性能考量](#性能考量)
- [调试技巧](#调试技巧)

## 概述

GoAgent 使用统一的 `errors` 包来处理所有错误，提供：

- **结构化错误**：包含错误代码、组件、操作、上下文等信息
- **错误链支持**：完整的 `errors.Is()` 和 `errors.As()` 支持
- **堆栈跟踪**：自动捕获错误发生位置
- **类型安全**：通过错误代码进行分类和检查
- **可观测性**：结构化元数据便于日志和监控

### 为什么要迁移

**当前问题（使用 fmt.Errorf）：**

```go
// ❌ 问题：缺少类型信息，难以分类
return nil, fmt.Errorf("LLM request failed: %w", err)

// ❌ 问题：字符串比较脆弱
if err != nil && strings.Contains(err.Error(), "not found") {
    // 处理逻辑
}

// ❌ 问题：缺少结构化上下文
return nil, fmt.Errorf("rate limit exceeded: %d requests", count)
```

**使用 errors 包的优势：**

```go
// ✅ 优势：类型安全的错误代码
return nil, errors.NewLLMRequestError(provider, model, err)

// ✅ 优势：健壮的类型检查
if errors.IsCode(err, errors.CodeDocumentNotFound) {
    // 处理逻辑
}

// ✅ 优势：结构化上下文
return nil, errors.New(errors.CodeLLMRateLimit, "rate limit exceeded").
    WithContext("count", count).
    WithContext("limit", limit)
```

## 错误处理架构

### AgentError 结构

```go
type AgentError struct {
    Code      ErrorCode              // 错误分类代码
    Message   string                 // 人类可读消息
    Operation string                 // 操作标识（如 "request", "parse"）
    Component string                 // 组件标识（如 "llm", "tool"）
    Context   map[string]interface{} // 结构化元数据
    Cause     error                  // 底层错误（支持错误链）
    Stack     []StackFrame           // 堆栈跟踪
}
```

### 错误代码体系

错误代码按功能域分类（共 48 个）：

| 域 | 错误代码数量 | 示例 |
|----|------------|------|
| Agent | 4 | `CodeAgentExecution`, `CodeAgentValidation` |
| Tool | 5 | `CodeToolExecution`, `CodeToolTimeout` |
| Middleware | 3 | `CodeMiddlewareExecution`, `CodeMiddlewareChain` |
| State | 4 | `CodeStateLoad`, `CodeStateSave` |
| Stream | 4 | `CodeStreamRead`, `CodeStreamWrite` |
| LLM | 4 | `CodeLLMRequest`, `CodeLLMRateLimit` |
| Context | 2 | `CodeContextCanceled`, `CodeContextTimeout` |
| Distributed | 3 | `CodeDistributedConnection`, `CodeDistributedSerialization` |
| Retrieval | 4 | `CodeRetrievalSearch`, `CodeDocumentNotFound` |
| Planning | 4 | `CodePlanningFailed`, `CodePlanExecutionFailed` |
| Parser | 3 | `CodeParserFailed`, `CodeParserInvalidJSON` |
| MultiAgent | 3 | `CodeMultiAgentRegistration`, `CodeMultiAgentConsensus` |
| Store | 3 | `CodeStoreConnection`, `CodeStoreNotFound` |
| Router | 3 | `CodeRouterNoMatch`, `CodeRouterFailed` |
| General | 4 | `CodeInvalidInput`, `CodeInvalidConfig` |

## 快速开始

### 1. 创建新错误

**使用辅助函数（推荐）：**

```go
import "github.com/kart-io/goagent/errors"

// LLM 错误
err := errors.NewLLMRequestError("openai", "gpt-4", originalErr)

// Tool 错误
err := errors.NewToolExecutionError("calculator", "execute", originalErr)

// 文档未找到
err := errors.NewDocumentNotFoundError("doc-123")
```

**使用核心 API（高级）：**

```go
// 创建简单错误
err := errors.New(errors.CodeAgentExecution, "execution failed")

// 包装已有错误
err := errors.Wrap(originalErr, errors.CodeLLMRequest, "LLM call failed")

// 链式添加上下文
err := errors.New(errors.CodeToolTimeout, "tool timed out").
    WithComponent("tool").
    WithOperation("execute").
    WithContext("tool_name", "web_scraper").
    WithContext("timeout_seconds", 30)
```

### 2. 检查错误类型

```go
// 检查特定错误代码
if errors.IsCode(err, errors.CodeLLMRateLimit) {
    // 实现重试逻辑
    time.Sleep(time.Duration(retryAfter) * time.Second)
    return retry()
}

// 提取错误代码
code := errors.GetCode(err)
switch code {
case errors.CodeDocumentNotFound:
    return nil, ErrNotFound
case errors.CodeLLMTimeout:
    return nil, ErrTimeout
default:
    return nil, err
}

// 使用标准 errors.Is
var agentErr *errors.AgentError
if stderrors.As(err, &agentErr) {
    log.Printf("Component: %s, Operation: %s", agentErr.Component, agentErr.Operation)
}
```

### 3. 提取错误信息

```go
// 提取组件
component := errors.GetComponent(err) // "llm"

// 提取操作
operation := errors.GetOperation(err) // "request"

// 提取上下文
ctx := errors.GetContext(err)
if provider, ok := ctx["provider"]; ok {
    log.Printf("Provider: %v", provider)
}

// 获取错误链
chain := errors.ErrorChain(err)
for i, e := range chain {
    log.Printf("Error %d: %v", i, e)
}

// 获取根因
root := errors.RootCause(err)
```

## 错误代码参考

### Agent 错误

```go
// 执行失败
errors.NewAgentExecutionError("my-agent", "run", cause)
// [AGENT_EXECUTION] [agent] operation=run: agent execution failed: <cause>

// 验证失败
errors.NewAgentValidationError("my-agent", "missing required input")
// [AGENT_VALIDATION] [agent] operation=validation: agent validation failed: missing required input

// Agent 未找到
errors.NewAgentNotFoundError("unknown-agent")
// [AGENT_NOT_FOUND] [agent] operation=lookup: agent not found: unknown-agent

// 初始化失败
errors.NewAgentInitializationError("my-agent", cause)
// [AGENT_INITIALIZATION] [agent] operation=initialize: agent initialization failed: <cause>
```

### Tool 错误

```go
// 工具执行失败
errors.NewToolExecutionError("calculator", "compute", cause)
// [TOOL_EXECUTION] [tool] operation=compute: tool execution failed: <cause>

// 工具未找到
errors.NewToolNotFoundError("unknown-tool")
// [TOOL_NOT_FOUND] [tool] operation=lookup: tool not found: unknown-tool

// 工具超时
errors.NewToolTimeoutError("web_scraper", 30)
// [TOOL_TIMEOUT] [tool] operation=execute: tool execution timed out after 30 seconds

// 重试耗尽
errors.NewToolRetryExhaustedError("api_caller", 3, lastErr)
// [TOOL_RETRY_EXHAUSTED] [tool] operation=execute_with_retry: tool retry exhausted after 3 attempts: <lastErr>
```

### LLM 错误

```go
// 请求失败
errors.NewLLMRequestError("openai", "gpt-4", cause)
// [LLM_REQUEST] [llm] operation=request: LLM request failed: <cause>
// Context: provider=openai, model=gpt-4

// 响应解析错误
errors.NewLLMResponseError("openai", "gpt-4", "no completion returned")
// [LLM_RESPONSE] [llm] operation=parse_response: LLM response error: no completion returned

// 请求超时
errors.NewLLMTimeoutError("openai", "gpt-4", 30)
// [LLM_TIMEOUT] [llm] operation=request: LLM request timed out after 30 seconds

// 速率限制
errors.NewLLMRateLimitError("openai", "gpt-4", 60)
// [LLM_RATE_LIMIT] [llm] operation=request: LLM rate limit exceeded
// Context: retry_after_seconds=60
```

### Retrieval/RAG 错误

```go
// 搜索失败
errors.NewRetrievalSearchError("user query", cause)
// [RETRIEVAL_SEARCH] [retrieval] operation=search: retrieval search failed: <cause>

// Embedding 生成失败
errors.NewRetrievalEmbeddingError("long text...", cause)
// [RETRIEVAL_EMBEDDING] [retrieval] operation=generate_embedding: retrieval embedding failed: <cause>

// 文档未找到
errors.NewDocumentNotFoundError("doc-123")
// [DOCUMENT_NOT_FOUND] [retrieval] operation=get_document: document not found: doc-123

// 向量维度不匹配
errors.NewVectorDimMismatchError(512, 768)
// [VECTOR_DIM_MISMATCH] [retrieval] operation=validate_vector: vector dimension mismatch: expected 512, got 768
```

### State 错误

```go
// 状态加载失败
errors.NewStateLoadError("session-123", cause)
// [STATE_LOAD] [state] operation=load: failed to load state: <cause>

// 状态保存失败
errors.NewStateSaveError("session-123", cause)
// [STATE_SAVE] [state] operation=save: failed to save state: <cause>

// Checkpoint 失败
errors.NewStateCheckpointError("session-123", "save", cause)
// [STATE_CHECKPOINT] [checkpoint] operation=save: checkpoint save failed: <cause>
```

### Store 错误

```go
// 连接失败
errors.NewStoreConnectionError("redis", "localhost:6379", cause)
// [STORE_CONNECTION] [store] operation=connect: store connection failed: <cause>

// 序列化失败
errors.NewStoreSerializationError("session:123", cause)
// [STORE_SERIALIZATION] [store] operation=serialize: store serialization failed: <cause>

// 项未找到
errors.NewStoreNotFoundError([]string{"memory", "session"}, "key-123")
// [STORE_NOT_FOUND] [store] operation=get: store item not found: key-123
```

### Planning 错误

```go
// 规划失败
errors.NewPlanningError("solve complex problem", cause)
// [PLANNING_FAILED] [planning] operation=create_plan: planning failed: <cause>

// 计划验证失败
errors.NewPlanValidationError("plan-123", "missing required step")
// [PLAN_VALIDATION] [planning] operation=validate_plan: plan validation failed: missing required step

// 计划执行失败
errors.NewPlanExecutionError("plan-123", "step-5", cause)
// [PLAN_EXECUTION_FAILED] [planning] operation=execute_plan: plan execution failed: <cause>

// 计划未找到
errors.NewPlanNotFoundError("plan-123")
// [PLAN_NOT_FOUND] [planning] operation=get_plan: plan not found: plan-123
```

### Parser 错误

```go
// 解析失败
errors.NewParserError("json", content, cause)
// [PARSER_FAILED] [parser] operation=parse: parser failed: <cause>

// JSON 无效
errors.NewParserInvalidJSONError(content, cause)
// [PARSER_INVALID_JSON] [parser] operation=parse_json: invalid JSON: <cause>

// 缺少字段
errors.NewParserMissingFieldError("action")
// [PARSER_MISSING_FIELD] [parser] operation=validate_fields: missing required field: action
```

### MultiAgent 错误

```go
// 注册失败
errors.NewMultiAgentRegistrationError("agent-123", cause)
// [MULTIAGENT_REGISTRATION] [multiagent] operation=register: agent registration failed: <cause>

// 共识失败
votes := map[string]bool{"agent-1": true, "agent-2": false, "agent-3": true}
errors.NewMultiAgentConsensusError(votes)
// [MULTIAGENT_CONSENSUS] [multiagent] operation=consensus: consensus not reached: 2 yes, 1 no

// 消息传递失败
errors.NewMultiAgentMessageError("agent.task", cause)
// [MULTIAGENT_MESSAGE] [multiagent] operation=send_message: message passing failed: <cause>
```

### Router 错误

```go
// 无匹配路由
errors.NewRouterNoMatchError("user.login", "/api/*")
// [ROUTER_NO_MATCH] [router] operation=route: no route matched for topic: user.login (pattern: /api/*)

// 路由失败
errors.NewRouterFailedError("semantic", cause)
// [ROUTER_FAILED] [router] operation=execute: router failed: <cause>

// 路由过载
errors.NewRouterOverloadError(100, 150)
// [ROUTER_OVERLOAD] [router] operation=queue: router overloaded: 150/100 requests
```

### Distributed 错误

```go
// 连接失败
errors.NewDistributedConnectionError("http://node-2:8080", cause)
// [DISTRIBUTED_CONNECTION] [distributed] operation=connect: distributed connection failed: <cause>

// 序列化失败
errors.NewDistributedSerializationError("AgentInput", cause)
// [DISTRIBUTED_SERIALIZATION] [distributed] operation=serialize: distributed serialization failed: <cause>

// 协调失败
errors.NewDistributedCoordinationError("elect_leader", cause)
// [DISTRIBUTED_COORDINATION] [distributed] operation=elect_leader: distributed coordination failed: <cause>
```

### General 错误

```go
// 无效输入
errors.NewInvalidInputError("agent", "prompt", "prompt cannot be empty")
// [INVALID_INPUT] [agent] operation=validate_input: invalid input: prompt cannot be empty

// 无效配置
errors.NewInvalidConfigError("llm", "api_key", "API key is required")
// [INVALID_CONFIG] [llm] operation=validate_config: invalid configuration: API key is required

// 未实现
errors.NewNotImplementedError("agent", "stream mode")
// [NOT_IMPLEMENTED] [agent]: feature not implemented: stream mode

// 内部错误
errors.NewInternalError("agent", "execute", cause)
// [INTERNAL_ERROR] [agent] operation=execute: internal error occurred: <cause>
```

## 迁移指南

### 迁移步骤

#### 步骤 1：识别错误创建模式

**模式 A：简单错误消息**

```go
// ❌ 旧代码
return nil, fmt.Errorf("checkpoint not found for thread: %s", threadID)

// ✅ 新代码
return nil, errors.NewStateLoadError(threadID, stderrors.New("checkpoint not found"))
```

**模式 B：错误包装**

```go
// ❌ 旧代码
return nil, fmt.Errorf("failed to connect to Redis: %w", err)

// ✅ 新代码
return nil, errors.Wrap(err, errors.CodeStateLoad, "failed to connect to Redis")
// 或使用专门的辅助函数
return nil, errors.NewStoreConnectionError("redis", "localhost:6379", err)
```

**模式 C：复杂上下文**

```go
// ❌ 旧代码
return nil, fmt.Errorf("rate limit exceeded: %d requests in %v", maxRequests, windowSize)

// ✅ 新代码
return nil, errors.New(errors.CodeLLMRateLimit, "rate limit exceeded").
    WithComponent("middleware").
    WithContext("max_requests", maxRequests).
    WithContext("window_size", windowSize)
```

#### 步骤 2：替换错误检查

**字符串比较 → 类型检查**

```go
// ❌ 旧代码（脆弱）
if err != nil && strings.Contains(err.Error(), "not found") {
    return handleNotFound()
}

// ✅ 新代码（健壮）
if errors.IsCode(err, errors.CodeDocumentNotFound) {
    return handleNotFound()
}

// 或使用 switch
switch errors.GetCode(err) {
case errors.CodeDocumentNotFound:
    return handleNotFound()
case errors.CodeLLMRateLimit:
    return handleRateLimit()
default:
    return err
}
```

#### 步骤 3：添加结构化上下文

**替换字符串插值为结构化字段**

```go
// ❌ 旧代码
return fmt.Errorf("tool %s timed out after %d seconds", toolName, timeout)

// ✅ 新代码
return errors.NewToolTimeoutError(toolName, timeout)
// 自动添加 tool_name 和 timeout_seconds 到 Context
```

### 迁移示例

#### 示例 1：LLM Provider

**旧代码 (llm/providers/openai.go)：**

```go
func (c *OpenAIClient) Complete(ctx context.Context, prompt string) (*Response, error) {
    if c.apiKey == "" {
        return nil, fmt.Errorf("OpenAI API key is required")
    }

    resp, err := c.client.CreateCompletion(ctx, req)
    if err != nil {
        return nil, fmt.Errorf("OpenAI completion failed: %w", err)
    }

    if len(resp.Choices) == 0 {
        return nil, fmt.Errorf("no completion choices returned")
    }

    return &Response{Text: resp.Choices[0].Text}, nil
}
```

**新代码：**

```go
import "github.com/kart-io/goagent/errors"

func (c *OpenAIClient) Complete(ctx context.Context, prompt string) (*Response, error) {
    if c.apiKey == "" {
        return nil, errors.NewInvalidConfigError("llm", "api_key", "OpenAI API key is required")
    }

    resp, err := c.client.CreateCompletion(ctx, req)
    if err != nil {
        // 检查特定错误类型
        if isRateLimitError(err) {
            return nil, errors.NewLLMRateLimitError("openai", c.model, 60)
        }
        return nil, errors.NewLLMRequestError("openai", c.model, err)
    }

    if len(resp.Choices) == 0 {
        return nil, errors.NewLLMResponseError("openai", c.model, "no completion choices returned")
    }

    return &Response{Text: resp.Choices[0].Text}, nil
}
```

#### 示例 2：Tool Execution

**旧代码 (tools/practical/file_operations.go)：**

```go
func (t *FileReadTool) Execute(ctx context.Context, input map[string]interface{}) (interface{}, error) {
    path, ok := input["path"].(string)
    if !ok {
        return nil, fmt.Errorf("path is required")
    }

    content, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return nil, fmt.Errorf("file not found: %s", path)
        }
        return nil, fmt.Errorf("failed to read file: %w", err)
    }

    return string(content), nil
}
```

**新代码：**

```go
import (
    "github.com/kart-io/goagent/errors"
    stderrors "errors"
)

func (t *FileReadTool) Execute(ctx context.Context, input map[string]interface{}) (interface{}, error) {
    path, ok := input["path"].(string)
    if !ok {
        return nil, errors.NewToolValidationError(t.Name(), "path is required")
    }

    content, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return nil, errors.NewDocumentNotFoundError(path).
                WithComponent("tool").
                WithContext("tool_name", t.Name())
        }
        return nil, errors.NewToolExecutionError(t.Name(), "read_file", err).
            WithContext("path", path)
    }

    return string(content), nil
}
```

#### 示例 3：State Checkpoint

**旧代码 (core/checkpoint/redis.go)：**

```go
func (s *RedisCheckpoint) Save(ctx context.Context, threadID string, state State) error {
    data, err := json.Marshal(state)
    if err != nil {
        return fmt.Errorf("failed to marshal state: %w", err)
    }

    key := fmt.Sprintf("checkpoint:%s", threadID)
    err = s.client.Set(ctx, key, data, 0).Err()
    if err != nil {
        return fmt.Errorf("failed to save checkpoint to Redis: %w", err)
    }

    return nil
}
```

**新代码：**

```go
import "github.com/kart-io/goagent/errors"

func (s *RedisCheckpoint) Save(ctx context.Context, threadID string, state State) error {
    data, err := json.Marshal(state)
    if err != nil {
        return errors.NewStoreSerializationError(threadID, err).
            WithComponent("checkpoint")
    }

    key := fmt.Sprintf("checkpoint:%s", threadID)
    err = s.client.Set(ctx, key, data, 0).Err()
    if err != nil {
        return errors.NewStateCheckpointError(threadID, "save", err).
            WithContext("backend", "redis").
            WithContext("key", key)
    }

    return nil
}
```

### 迁移检查清单

在提交 PR 前，确保：

- [ ] 所有 `fmt.Errorf` 已替换为 `errors` 包函数
- [ ] 错误检查使用 `errors.IsCode()` 而非字符串比较
- [ ] 所有新错误包含适当的 Component 和 Operation
- [ ] 关键上下文信息添加到 Context 字段
- [ ] 更新了相关单元测试
- [ ] 错误消息清晰且对用户友好
- [ ] 导入分层规则合规（`./verify_imports.sh`）

## 最佳实践

### 1. 选择合适的错误代码

**规则：** 始终选择最具体的错误代码

```go
// ❌ 不好：使用通用代码
return errors.New(errors.CodeInternal, "document not found")

// ✅ 好：使用专门代码
return errors.NewDocumentNotFoundError(docID)
```

### 2. 添加有意义的上下文

**规则：** 添加调试和监控所需的关键信息

```go
// ❌ 不好：缺少上下文
return errors.New(errors.CodeLLMRequest, "request failed")

// ✅ 好：包含关键信息
return errors.NewLLMRequestError(provider, model, err).
    WithContext("prompt_length", len(prompt)).
    WithContext("temperature", temperature)
```

### 3. 正确包装错误

**规则：** 使用 `Wrap()` 保留错误链

```go
// ❌ 不好：丢失原始错误
return errors.New(errors.CodeToolExecution, err.Error())

// ✅ 好：保留错误链
return errors.Wrap(err, errors.CodeToolExecution, "tool execution failed")
// 或
return errors.NewToolExecutionError(toolName, "execute", err)
```

### 4. 避免过度包装

**规则：** 不要在同一层级重复包装

```go
// ❌ 不好：重复包装
err := doSomething()
if err != nil {
    err = errors.Wrap(err, errors.CodeInternal, "step 1 failed")
    err = errors.Wrap(err, errors.CodeInternal, "step 2 failed") // 重复！
    return err
}

// ✅ 好：在适当的层级包装一次
err := doSomething()
if err != nil {
    return errors.Wrap(err, errors.CodeAgentExecution, "agent step failed")
}
```

### 5. 使用辅助函数

**规则：** 优先使用 `NewXxxError` 辅助函数

```go
// ❌ 可以，但繁琐
return errors.New(errors.CodeLLMRequest, "LLM request failed").
    WithComponent("llm").
    WithOperation("request").
    WithContext("provider", provider).
    WithContext("model", model)

// ✅ 更好：使用辅助函数
return errors.NewLLMRequestError(provider, model, err)
```

### 6. 错误检查模式

**规则：** 使用类型检查而非字符串比较

```go
// ❌ 脆弱的字符串检查
if strings.Contains(err.Error(), "rate limit") {
    // 可能误匹配
}

// ✅ 类型安全的检查
if errors.IsCode(err, errors.CodeLLMRateLimit) {
    retryAfter := errors.GetContext(err)["retry_after_seconds"].(int)
    time.Sleep(time.Duration(retryAfter) * time.Second)
    return retry()
}
```

### 7. 日志记录

**规则：** 利用结构化错误进行日志记录

```go
if err != nil {
    var agentErr *errors.AgentError
    if stderrors.As(err, &agentErr) {
        log.WithFields(log.Fields{
            "error_code": agentErr.Code,
            "component":  agentErr.Component,
            "operation":  agentErr.Operation,
            "context":    agentErr.Context,
        }).Error("operation failed")
    }
    return err
}
```

## 常见场景

### 场景 1：重试逻辑

```go
func executeWithRetry(ctx context.Context, tool Tool, input map[string]interface{}) (interface{}, error) {
    const maxAttempts = 3
    var lastErr error

    for attempt := 1; attempt <= maxAttempts; attempt++ {
        result, err := tool.Execute(ctx, input)
        if err == nil {
            return result, nil
        }

        lastErr = err

        // 只在特定错误时重试
        if errors.IsCode(err, errors.CodeLLMRateLimit) {
            retryAfter := errors.GetContext(err)["retry_after_seconds"].(int)
            time.Sleep(time.Duration(retryAfter) * time.Second)
            continue
        }

        if errors.IsCode(err, errors.CodeLLMTimeout) {
            time.Sleep(time.Second * time.Duration(attempt))
            continue
        }

        // 其他错误立即返回
        return nil, err
    }

    return nil, errors.NewToolRetryExhaustedError(tool.Name(), maxAttempts, lastErr)
}
```

### 场景 2：降级处理

```go
func getDocument(ctx context.Context, docID string) (*Document, error) {
    // 尝试主存储
    doc, err := primaryStore.Get(ctx, docID)
    if err == nil {
        return doc, nil
    }

    // 检查是否是连接错误，如果是则尝试备份存储
    if errors.IsCode(err, errors.CodeStoreConnection) {
        log.Warn("Primary store unavailable, trying backup", "error", err)
        doc, backupErr := backupStore.Get(ctx, docID)
        if backupErr == nil {
            return doc, nil
        }
    }

    // 所有尝试失败
    if errors.IsCode(err, errors.CodeStoreNotFound) {
        return nil, errors.NewDocumentNotFoundError(docID)
    }

    return nil, err
}
```

### 场景 3：错误转换

```go
// API 层：转换内部错误为 HTTP 状态码
func toHTTPError(err error) (int, string) {
    code := errors.GetCode(err)

    switch code {
    case errors.CodeDocumentNotFound, errors.CodeAgentNotFound, errors.CodeToolNotFound:
        return http.StatusNotFound, err.Error()
    case errors.CodeInvalidInput, errors.CodeInvalidConfig:
        return http.StatusBadRequest, err.Error()
    case errors.CodeLLMRateLimit:
        return http.StatusTooManyRequests, err.Error()
    case errors.CodeLLMTimeout, errors.CodeToolTimeout:
        return http.StatusGatewayTimeout, err.Error()
    default:
        return http.StatusInternalServerError, "Internal server error"
    }
}
```

### 场景 4：错误聚合

```go
func processBatch(ctx context.Context, items []Item) ([]Result, error) {
    results := make([]Result, 0, len(items))
    var errs []error

    for i, item := range items {
        result, err := processItem(ctx, item)
        if err != nil {
            // 为每个错误添加批次位置上下文
            if agentErr, ok := err.(*errors.AgentError); ok {
                agentErr.WithContext("batch_index", i).WithContext("item_id", item.ID)
            }
            errs = append(errs, err)
            continue
        }
        results = append(results, result)
    }

    if len(errs) > 0 {
        // 创建聚合错误
        return results, errors.New(errors.CodeInternal, "batch processing partially failed").
            WithContext("total_items", len(items)).
            WithContext("failed_items", len(errs)).
            WithContext("errors", errs)
    }

    return results, nil
}
```

## 性能考量

### 堆栈捕获

堆栈捕获有性能开销（~1-2μs）。对于高频路径，可以考虑：

```go
// 不需要堆栈的情况（如输入验证）
if input == "" {
    // 使用简单错误，堆栈仍会捕获但不必担心
    return errors.NewInvalidInputError("agent", "input", "input cannot be empty")
}
```

### 上下文大小

避免在 Context 中添加大对象：

```go
// ❌ 不好：添加大对象
return err.WithContext("full_document", largeDocument) // 可能数MB

// ✅ 好：添加摘要信息
return err.WithContext("document_id", doc.ID).
    WithContext("document_size", len(doc.Content))
```

### 基准测试

```bash
# 运行 errors 包的基准测试
cd errors
go test -bench=. -benchmem

# 典型结果：
# BenchmarkNew-8           2000000    ~800 ns/op   ~400 B/op   ~10 allocs/op
# BenchmarkWrap-8          2000000    ~900 ns/op   ~450 B/op   ~12 allocs/op
# BenchmarkWithContext-8  20000000     ~50 ns/op    ~48 B/op    ~1 allocs/op
```

## 调试技巧

### 1. 打印完整错误链

```go
func printErrorChain(err error) {
    chain := errors.ErrorChain(err)
    for i, e := range chain {
        fmt.Printf("  %d: %v\n", i, e)
        if agentErr, ok := e.(*errors.AgentError); ok {
            fmt.Printf("     Code: %s, Component: %s, Operation: %s\n",
                agentErr.Code, agentErr.Component, agentErr.Operation)
            fmt.Printf("     Context: %+v\n", agentErr.Context)
        }
    }
}
```

### 2. 提取堆栈跟踪

```go
if agentErr, ok := err.(*errors.AgentError); ok {
    fmt.Println(agentErr.FormatStack())
    // 输出：
    // Stack trace:
    //   /path/to/file.go:123 github.com/kart-io/goagent/pkg.function
    //   /path/to/file.go:456 github.com/kart-io/goagent/pkg.caller
    //   ...
}
```

### 3. JSON 序列化

```go
// AgentError 可以直接 JSON 序列化用于 API 响应或日志
func errorToJSON(err error) string {
    if agentErr, ok := err.(*errors.AgentError); ok {
        data, _ := json.MarshalIndent(map[string]interface{}{
            "code":      agentErr.Code,
            "message":   agentErr.Message,
            "component": agentErr.Component,
            "operation": agentErr.Operation,
            "context":   agentErr.Context,
        }, "", "  ")
        return string(data)
    }
    return err.Error()
}
```

## 进一步阅读

- [架构文档](../architecture/ARCHITECTURE.md) - GoAgent 整体架构
- [导入分层规则](../architecture/IMPORT_LAYERING.md) - 严格的 4 层导入规范
- [测试最佳实践](../development/TESTING_BEST_PRACTICES.md) - 错误处理测试
- [生产部署指南](PRODUCTION_DEPLOYMENT.md) - 错误监控和告警

## 贡献

改进此文档或 errors 包？请提交 PR：

1. 在 `errors/` 中添加新的错误代码（如果需要）
2. 更新 `errors/helpers.go` 添加辅助函数
3. 添加对应的测试到 `errors/errors_test.go`
4. 更新本文档的错误代码参考部分
5. 运行 `./verify_imports.sh` 确保合规

---

**最后更新：** 2025-11-17
**版本：** v1.0.0
**维护者：** GoAgent Team
