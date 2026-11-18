package interfaces

import "context"

// Tool represents an executable tool that an agent can call.
//
// All tool implementations should implement this interface.
//
// Implementations:
//   - tools.BaseTool - base implementation with common functionality
//   - tools/compute/* - compute tools
//   - tools/http/* - HTTP request tools
//   - tools/search/* - search tools
//   - tools/shell/* - shell execution tools
//
// See tools/tool.go for the base implementation.
type Tool interface {
	// Name 返回工具标识符
	//
	// 名称应在工具注册表中唯一，并遵循命名约定
	// （小写字母，下划线分隔）
	Name() string

	// Description 返回工具的功能描述
	//
	// 此描述供 LLM 理解何时以及如何使用工具
	// 应当清晰简洁
	Description() string

	// Invoke 使用给定输入执行工具
	//
	// 工具应处理输入参数并返回结果
	// 如果执行失败则返回错误
	Invoke(ctx context.Context, input *ToolInput) (*ToolOutput, error)

	// ArgsSchema 返回工具的输入模式（JSON Schema 格式）
	//
	// 此模式定义工具接受的参数结构
	// LLM 使用它生成有效的工具调用
	//
	// 示例模式：
	//   {
	//     "type": "object",
	//     "properties": {
	//       "query": {"type": "string", "description": "搜索查询"}
	//     },
	//     "required": ["query"]
	//   }
	ArgsSchema() string
}

// DurableCallTool declares that Invoke journals ToolInput.CallID under the host
// execution key and store, preserving child keys and replaying completed results.
// Dispatcher.Tools implements this contract. A bare Tool does not imply recovery.
type DurableCallTool interface {
	Tool
	SupportsDurableCalls() bool
}

// ArgumentErrorTool declares that Invoke returns invalid model-supplied
// arguments to the model as a result for correction, so the host need not
// reject them beforehand. Dispatcher.Tools implements this contract; wrappers
// must forward it explicitly, otherwise the host still validates arguments
// before the call.
type ArgumentErrorTool interface {
	Tool
	ReturnsArgumentErrors() bool
}

// ConcurrentCallTool declares that Invoke may be executed concurrently by
// multiple calls in the same turn and that the implementation limits its own
// concurrency. Dispatcher.Tools implements this contract; wrappers must forward
// it explicitly, otherwise Builder executes the turn's calls sequentially. The
// declaration only permits concurrency; Builder also needs
// WithParallelToolCalls(n>=2) to execute concurrently.
type ConcurrentCallTool interface {
	Tool
	SupportsConcurrentCalls() bool
}

// ValidatableTool is an optional interface a tool can implement to provide
// custom input validation logic.
//
// If a tool implements this interface, the validator calls Validate before
// executing the tool, allowing more complex validation than a JSON schema
// alone.
//
// In the example implementation, agentErrors is the import alias for
// github.com/kart-io/goagent/errors:
//
//	func (t *MyTool) Validate(ctx context.Context, input *ToolInput) error {
//	    // Custom validation logic
//	    if val, ok := input.Args["amount"].(float64); ok && val < 0 {
//	        return agentErrors.NewToolValidationError(t.Name(), "amount must be non-negative")
//	    }
//	    return nil
//	}
type ValidatableTool interface {
	Tool

	// Validate 在执行前验证工具输入
	//
	// 如果输入无效则返回错误，错误消息应清楚描述输入的问题
	Validate(ctx context.Context, input *ToolInput) error
}

// ToolInput represents tool execution input.
//
// It contains all the information needed to execute a tool, including the
// arguments and metadata for tracing and debugging.
type ToolInput struct {
	// CallID is a host-owned stable child operation ID. Multiple effects in one
	// durable node require distinct IDs; never read this from model arguments.
	CallID string `json:"call_id,omitempty"`
	// ExecutionKey is supplied by the host/execution context, never inferred from
	// model arguments. Effect services must implement idempotency with this key
	// or a stronger business ID. Empty means execution is not durably identified.
	ExecutionKey string `json:"execution_key,omitempty"`

	// Args contains the tool's input arguments.
	//
	// The structure of Args should match the tool's ArgsSchema. Numbers from the
	// model are generally float64; large integers and high-precision decimals that
	// float64 cannot represent are json.Number (see tools/toolargs).
	Args map[string]interface{} `json:"args"`

	// CallerID identifies the caller of the tool.
	//
	// Optional; used for authorization and auditing.
	CallerID string `json:"caller_id,omitempty"`

	// TraceID is used for distributed tracing.
	//
	// Optional; helps track tool execution across systems.
	TraceID string `json:"trace_id,omitempty"`
}

// ToolOutput represents tool execution output.
//
// It contains the tool's execution result along with status information and
// metadata.
type ToolOutput struct {
	// Result contains the tool's output data.
	//
	// The type depends on the tool; common types:
	//   - string: text output
	//   - map[string]interface{}: structured data
	//   - []byte: binary data
	Result interface{} `json:"result"`

	// Success reports whether the tool executed successfully.
	//
	// It is true if the tool completed without error, false otherwise.
	Success bool `json:"success"`

	// Error contains the error message (when Success is false).
	//
	// It is an empty string when Success is true.
	Error string `json:"error,omitempty"`

	// Metadata contains additional information about the execution.
	//
	// Optional; it may include:
	//   - execution_time: tool run duration
	//   - retries: number of retries
	//   - cost: API call cost (for external services)
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// AdditionalContext is appended by post-call hooks; Builder injects it into the
	// model context as a user message after this batch of tool results.
	AdditionalContext []string `json:"additional_context,omitempty"`

	// Labels is reserved for data source and trust labels (such as source=web,
	// trust=untrusted) that policies can act on; it is not propagated yet.
	Labels map[string]string `json:"labels,omitempty"`
}

// ToolCall represents a record of a tool call.
//
// It is used for logging, auditing, and debugging, and captures the full
// context of a tool call.
type ToolCall struct {
	// ID is the unique identifier of this tool call.
	//
	// It is used to correlate calls in logs and traces.
	ID string `json:"id"`

	// ToolName is the name of the called tool.
	ToolName string `json:"tool_name"`

	// Args are the arguments passed to the tool.
	Args map[string]interface{} `json:"args"`

	// Result is the tool's output.
	//
	// It may be nil if the call is still in progress.
	Result *ToolOutput `json:"result,omitzero"`

	// Error contains the error information when the call fails.
	//
	// It is an empty string when the call succeeds.
	Error string `json:"error,omitempty"`

	// StartTime is when the tool call started (Unix timestamp).
	StartTime int64 `json:"start_time"`

	// EndTime is when the tool call completed (Unix timestamp).
	//
	// It is zero if the call is still in progress.
	EndTime int64 `json:"end_time,omitzero"`

	// Metadata contains additional context about the call.
	//
	// It may include caller information, trace IDs, and so on.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}
