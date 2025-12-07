# GoAgent Tool API 参考

本文档提供 GoAgent 工具系统的完整 API 参考。

## 目录

- [Tool 接口](#tool-接口)
- [BaseTool](#basetool)
- [FunctionTool](#functiontool)
- [Tool Registry](#tool-registry)
- [Tool Executor](#tool-executor)
- [内置工具](#内置工具)

---

## Tool 接口

### interfaces.Tool

```go
package interfaces

// Tool 表示智能体可以调用的可执行工具
type Tool interface {
    // Name 返回工具标识符（唯一）
    Name() string

    // Description 返回工具的功能描述（供 LLM 理解）
    Description() string

    // Invoke 执行工具
    Invoke(ctx context.Context, input *ToolInput) (*ToolOutput, error)

    // ArgsSchema 返回参数 JSON Schema
    ArgsSchema() string
}
```

### interfaces.ValidatableTool

```go
// ValidatableTool 支持自定义验证的工具
type ValidatableTool interface {
    Tool

    // Validate 在执行前验证输入
    Validate(ctx context.Context, input *ToolInput) error
}
```

### interfaces.ToolInput

```go
type ToolInput struct {
    // Args 包含工具的输入参数
    Args map[string]interface{} `json:"args"`

    // Context 是执行上下文
    Context context.Context `json:"-"`

    // CallerID 调用者标识
    CallerID string `json:"caller_id,omitempty"`

    // TraceID 追踪 ID
    TraceID string `json:"trace_id,omitempty"`
}
```

### interfaces.ToolOutput

```go
type ToolOutput struct {
    // Result 工具输出数据
    Result interface{} `json:"result"`

    // Success 执行是否成功
    Success bool `json:"success"`

    // Error 错误消息
    Error string `json:"error,omitempty"`

    // Metadata 额外信息
    Metadata map[string]interface{} `json:"metadata,omitempty"`
}
```

### interfaces.ToolResult

```go
type ToolResult struct {
    // ToolName 执行的工具名称
    ToolName string `json:"tool_name"`

    // Output 工具输出
    Output *ToolOutput `json:"output"`

    // ExecutionTime 执行时长（毫秒）
    ExecutionTime int64 `json:"execution_time,omitempty"`
}
```

### interfaces.ToolCall

```go
type ToolCall struct {
    // ID 工具调用唯一标识符
    ID string `json:"id"`

    // ToolName 工具名称
    ToolName string `json:"tool_name"`

    // Args 参数
    Args map[string]interface{} `json:"args"`

    // Result 输出结果
    Result *ToolOutput `json:"result,omitempty"`

    // Error 错误信息
    Error string `json:"error,omitempty"`

    // StartTime 开始时间
    StartTime int64 `json:"start_time"`

    // EndTime 结束时间
    EndTime int64 `json:"end_time,omitempty"`

    // Metadata 元数据
    Metadata map[string]interface{} `json:"metadata,omitempty"`
}
```

---

## BaseTool

### tools.BaseTool

```go
package tools

type BaseTool struct {
    name        string
    description string
    argsSchema  string
    runFunc     func(context.Context, *ToolInput) (*ToolOutput, error)
}

// NewBaseTool 创建基础工具
func NewBaseTool(
    name string,
    description string,
    argsSchema string,
    runFunc func(context.Context, *ToolInput) (*ToolOutput, error),
) *BaseTool

// Name 返回工具名称
func (t *BaseTool) Name() string

// Description 返回工具描述
func (t *BaseTool) Description() string

// ArgsSchema 返回参数 Schema
func (t *BaseTool) ArgsSchema() string

// Invoke 执行工具
func (t *BaseTool) Invoke(ctx context.Context, input *ToolInput) (*ToolOutput, error)
```

### 使用示例

```go
import "github.com/kart-io/goagent/tools"

// 创建自定义工具
myTool := tools.NewBaseTool(
    "my_tool",
    "执行自定义操作",
    `{
        "type": "object",
        "properties": {
            "input": {"type": "string", "description": "输入参数"}
        },
        "required": ["input"]
    }`,
    func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
        inputStr := input.Args["input"].(string)
        result := processInput(inputStr)
        return &interfaces.ToolOutput{
            Result:  result,
            Success: true,
        }, nil
    },
)
```

---

## FunctionTool

### tools.FunctionTool

```go
package tools

type FunctionTool struct {
    *BaseTool
    fn func(context.Context, map[string]interface{}) (interface{}, error)
}

// NewFunctionTool 创建函数工具
func NewFunctionTool(
    name string,
    description string,
    argsSchema string,
    fn func(context.Context, map[string]interface{}) (interface{}, error),
) *FunctionTool

// NewSimpleFunctionTool 创建简单函数工具
func NewSimpleFunctionTool(name string, description string, fn SimpleFunction) *FunctionTool

// NewTypedFunctionTool 创建类型安全的函数工具（泛型）
func NewTypedFunctionTool[I, O any](
    name string,
    description string,
    argsSchema string,
    fn TypedFunction[I, O],
) *FunctionTool
```

### tools.FunctionToolBuilder

```go
type FunctionToolBuilder struct {
    name        string
    description string
    argsSchema  string
    fn          func(context.Context, map[string]interface{}) (interface{}, error)
}

// NewFunctionToolBuilder 创建函数工具构建器
func NewFunctionToolBuilder(name string) *FunctionToolBuilder

// WithDescription 设置描述
func (b *FunctionToolBuilder) WithDescription(description string) *FunctionToolBuilder

// WithArgsSchema 设置参数 Schema
func (b *FunctionToolBuilder) WithArgsSchema(schema string) *FunctionToolBuilder

// WithFunction 设置执行函数
func (b *FunctionToolBuilder) WithFunction(fn func(context.Context, map[string]interface{}) (interface{}, error)) *FunctionToolBuilder

// Build 构建工具
func (b *FunctionToolBuilder) Build() (*FunctionTool, error)

// MustBuild 构建工具（失败时 panic）
func (b *FunctionToolBuilder) MustBuild() *FunctionTool
```

### 使用示例

```go
import "github.com/kart-io/goagent/tools"

// 方式 1: 使用 NewFunctionTool
addTool := tools.NewFunctionTool(
    "add",
    "加法计算",
    `{"type": "object", "properties": {"a": {"type": "number"}, "b": {"type": "number"}}, "required": ["a", "b"]}`,
    func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        a := args["a"].(float64)
        b := args["b"].(float64)
        return a + b, nil
    },
)

// 方式 2: 使用 Builder
searchTool := tools.NewFunctionToolBuilder("search").
    WithDescription("搜索信息").
    WithArgsSchema(`{"type": "object", "properties": {"query": {"type": "string"}}, "required": ["query"]}`).
    WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        query := args["query"].(string)
        return search(query), nil
    }).
    MustBuild()

// 方式 3: 使用泛型（类型安全）
type AddInput struct {
    A float64 `json:"a"`
    B float64 `json:"b"`
}

typedAddTool := tools.NewTypedFunctionTool[AddInput, float64](
    "typed_add",
    "类型安全的加法",
    `{"type": "object", "properties": {"a": {"type": "number"}, "b": {"type": "number"}}, "required": ["a", "b"]}`,
    func(ctx context.Context, input AddInput) (float64, error) {
        return input.A + input.B, nil
    },
)
```

---

## Tool Registry

### tools.Registry

```go
package tools

type Registry struct {
    tools map[string]Tool
    mu    sync.RWMutex
}

// NewRegistry 创建工具注册表
func NewRegistry() *Registry

// Register 注册工具
func (r *Registry) Register(tool Tool) error

// Get 获取工具
func (r *Registry) Get(name string) Tool

// List 列出所有工具
func (r *Registry) List() []Tool

// Names 返回所有工具名称
func (r *Registry) Names() []string

// Clear 清空注册表
func (r *Registry) Clear()

// Size 返回工具数量
func (r *Registry) Size() int
```

### 使用示例

```go
import "github.com/kart-io/goagent/tools"

// 创建注册表
registry := tools.NewRegistry()

// 注册工具
registry.Register(calculatorTool)
registry.Register(searchTool)
registry.Register(shellTool)

// 获取工具
tool := registry.Get("calculator")

// 列出所有工具
allTools := registry.List()

// 获取工具名称
names := registry.Names() // ["calculator", "search", "shell"]
```

---

## Tool Executor

### interfaces.ToolExecutor

```go
type ToolExecutor interface {
    // ExecuteTool 执行指定工具
    ExecuteTool(ctx context.Context, toolName string, args map[string]interface{}) (*ToolResult, error)

    // ListTools 列出可用工具
    ListTools() []Tool
}
```

### tools.ToolExecutor

```go
package tools

type ToolExecutor struct {
    maxConcurrency int
    timeout        time.Duration
    retryPolicy    *RetryPolicy
    errorHandler   ErrorHandler
}

// NewToolExecutor 创建工具执行器
func NewToolExecutor(opts ...ExecutorOption) *ToolExecutor

// ExecutorOption 执行器选项
type ExecutorOption func(*ToolExecutor)

// WithMaxConcurrency 设置最大并发数
func WithMaxConcurrency(max int) ExecutorOption

// WithTimeout 设置超时时间
func WithTimeout(timeout time.Duration) ExecutorOption

// WithRetryPolicy 设置重试策略
func WithRetryPolicy(policy *RetryPolicy) ExecutorOption

// WithErrorHandler 设置错误处理器
func WithErrorHandler(handler ErrorHandler) ExecutorOption
```

### tools.RetryPolicy

```go
type RetryPolicy struct {
    // MaxRetries 最大重试次数
    MaxRetries int

    // InitialDelay 初始延迟
    InitialDelay time.Duration

    // MaxDelay 最大延迟
    MaxDelay time.Duration

    // Multiplier 延迟倍数
    Multiplier float64

    // RetryableErrors 可重试的错误类型
    RetryableErrors []string
}
```

### 使用示例

```go
import "github.com/kart-io/goagent/tools"

// 创建执行器
executor := tools.NewToolExecutor(
    tools.WithMaxConcurrency(10),
    tools.WithTimeout(30 * time.Second),
    tools.WithRetryPolicy(&tools.RetryPolicy{
        MaxRetries:   3,
        InitialDelay: 100 * time.Millisecond,
        MaxDelay:     5 * time.Second,
        Multiplier:   2.0,
    }),
)

// 执行工具
result, err := executor.ExecuteTool(ctx, "calculator", map[string]interface{}{
    "expression": "2 + 2",
})
```

---

## 内置工具

### Shell 工具

```go
package shell

type ShellTool struct {
    allowedCommands []string
    timeout         time.Duration
    workDir         string
}

// NewShellTool 创建 Shell 工具
func NewShellTool(opts ...ShellOption) *ShellTool

// ShellOption 选项
func WithAllowedCommands(cmds []string) ShellOption
func WithShellTimeout(timeout time.Duration) ShellOption
func WithWorkDir(dir string) ShellOption
```

### Calculator 工具

```go
package compute

type Calculator struct {
    precision int
}

// NewCalculator 创建计算器工具
func NewCalculator(opts ...CalculatorOption) *Calculator
```

### HTTP/API 工具

```go
package http

type APITool struct {
    baseURL string
    headers map[string]string
    timeout time.Duration
}

// NewAPITool 创建 API 工具
func NewAPITool(opts ...APIOption) *APITool
```

### Search 工具

```go
package search

type SearchTool struct {
    provider SearchProvider
    maxResults int
}

// NewSearchTool 创建搜索工具
func NewSearchTool(provider SearchProvider, opts ...SearchOption) *SearchTool
```

### Web Scraper 工具

```go
package practical

type WebScraper struct {
    userAgent string
    timeout   time.Duration
}

// NewWebScraper 创建网页爬取工具
func NewWebScraper(opts ...ScraperOption) *WebScraper
```

### File Operations 工具

```go
package practical

type FileOperations struct {
    allowedPaths []string
    maxFileSize  int64
}

// NewFileOperations 创建文件操作工具
func NewFileOperations(opts ...FileOption) *FileOperations
```

### Database Query 工具

```go
package practical

type DatabaseQuery struct {
    driver     string
    datasource string
    maxRows    int
}

// NewDatabaseQuery 创建数据库查询工具
func NewDatabaseQuery(driver, datasource string, opts ...DBOption) *DatabaseQuery
```

### 使用示例

```go
import (
    "github.com/kart-io/goagent/tools/shell"
    "github.com/kart-io/goagent/tools/compute"
    "github.com/kart-io/goagent/tools/http"
    "github.com/kart-io/goagent/tools/practical"
)

// 创建各种内置工具
shellTool := shell.NewShellTool(
    shell.WithAllowedCommands([]string{"ls", "cat", "grep"}),
    shell.WithShellTimeout(10 * time.Second),
)

calculator := compute.NewCalculator()

apiTool := http.NewAPITool(
    http.WithBaseURL("https://api.example.com"),
    http.WithHeaders(map[string]string{"Authorization": "Bearer token"}),
)

webScraper := practical.NewWebScraper(
    practical.WithUserAgent("GoAgent/1.0"),
)

fileOps := practical.NewFileOperations(
    practical.WithAllowedPaths([]string{"/tmp", "/data"}),
)

dbQuery := practical.NewDatabaseQuery(
    "postgres",
    "postgres://user:pass@localhost/db",
    practical.WithMaxRows(1000),
)
```

---

## 工具中间件

### tools/middleware

```go
package middleware

// ToolMiddleware 工具中间件接口
type ToolMiddleware interface {
    Before(ctx context.Context, input *ToolInput) (*ToolInput, error)
    After(ctx context.Context, output *ToolOutput) (*ToolOutput, error)
    OnError(ctx context.Context, err error) error
}

// NewLoggingMiddleware 日志中间件
func NewLoggingMiddleware() ToolMiddleware

// NewRateLimitMiddleware 速率限制中间件
func NewRateLimitMiddleware(rps float64) *RateLimitMiddleware

// NewCachingMiddleware 缓存中间件
func NewCachingMiddleware(ttl time.Duration) *CachingMiddleware
```

---

## 相关文档

- [核心 API 参考](CORE_API.md)
- [LLM API 参考](LLM_API.md)
- [工具中间件指南](../guides/TOOL_MIDDLEWARE.md)
