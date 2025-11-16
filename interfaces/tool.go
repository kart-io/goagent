package interfaces

import "context"

// Tool represents an executable tool that agents can invoke.
//
// All tool implementations should implement this interface.
//
// Implementation locations:
//   - tools.BaseTool - Base implementation with common functionality
//   - tools/compute/* - Computation tools
//   - tools/http/* - HTTP request tools
//   - tools/search/* - Search tools
//   - tools/shell/* - Shell execution tools
//   - tools/practical/* - Practical utility tools
//
// See: pkg/agent/tools/tool.go for the base implementation
type Tool interface {
	// Name returns the tool identifier.
	//
	// The name should be unique within a tool registry and follow
	// naming conventions (lowercase, underscores for separators).
	Name() string

	// Description returns what the tool does.
	//
	// This description is used by LLMs to understand when and how
	// to use the tool. It should be clear and concise.
	Description() string

	// Invoke executes the tool with given input.
	//
	// The tool should process the input arguments and return results
	// or an error if execution fails.
	Invoke(ctx context.Context, input *ToolInput) (*ToolOutput, error)

	// ArgsSchema returns the tool's input schema (JSON Schema format).
	//
	// This schema defines the structure of arguments the tool accepts.
	// It's used by LLMs to generate valid tool calls.
	//
	// Example schema:
	//   {
	//     "type": "object",
	//     "properties": {
	//       "query": {"type": "string", "description": "Search query"}
	//     },
	//     "required": ["query"]
	//   }
	ArgsSchema() string
}

// ToolExecutor represents a component that can execute tools.
//
// This interface is implemented by components that need to run tools,
// such as agents and workflow engines.
//
// Implementation locations:
//   - agents/executor/executor_agent.go - Executor agent implementation
//   - tools/registry.go - Tool registry with execution capabilities
type ToolExecutor interface {
	// ExecuteTool executes a tool by name with the given arguments.
	//
	// The executor is responsible for:
	//   - Looking up the tool by name
	//   - Validating arguments against the tool's schema
	//   - Executing the tool
	//   - Handling errors and timeouts
	ExecuteTool(ctx context.Context, toolName string, args map[string]interface{}) (*ToolResult, error)

	// ListTools returns all available tools.
	//
	// This is useful for agents that need to discover what tools
	// they can use.
	ListTools() []Tool
}

// ToolInput represents tool execution input.
//
// This structure contains all necessary information to execute a tool,
// including arguments and metadata for tracing and debugging.
type ToolInput struct {
	// Args contains the tool's input parameters.
	//
	// The structure of Args should match the tool's ArgsSchema.
	Args map[string]interface{} `json:"args"`

	// Context is the execution context (not serialized).
	//
	// This is used for cancellation, timeouts, and passing
	// request-scoped values.
	Context context.Context `json:"-"`

	// CallerID identifies who is invoking the tool.
	//
	// Optional. Used for authorization and auditing.
	CallerID string `json:"caller_id,omitempty"`

	// TraceID is used for distributed tracing.
	//
	// Optional. Helps track tool execution across systems.
	TraceID string `json:"trace_id,omitempty"`
}

// ToolOutput represents tool execution output.
//
// This structure contains the result of tool execution along with
// status information and metadata.
type ToolOutput struct {
	// Result contains the tool's output data.
	//
	// The type depends on the specific tool. Common types:
	//   - string: Text output
	//   - map[string]interface{}: Structured data
	//   - []byte: Binary data
	Result interface{} `json:"result"`

	// Success indicates whether the tool executed successfully.
	//
	// True if the tool completed without errors, false otherwise.
	Success bool `json:"success"`

	// Error contains the error message if Success is false.
	//
	// Empty string if Success is true.
	Error string `json:"error,omitempty"`

	// Metadata contains additional information about execution.
	//
	// Optional. May include:
	//   - execution_time: How long the tool took to run
	//   - retries: Number of retry attempts
	//   - cost: API call cost (for external services)
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ToolResult represents the result of a tool execution by a ToolExecutor.
//
// This is different from ToolOutput in that it includes additional
// information that executors track, such as the tool that was run.
type ToolResult struct {
	// ToolName is the name of the tool that was executed.
	ToolName string `json:"tool_name"`

	// Output contains the tool's execution output.
	Output *ToolOutput `json:"output"`

	// ExecutionTime is how long the tool took to execute (in milliseconds).
	//
	// Optional. Useful for performance monitoring.
	ExecutionTime int64 `json:"execution_time,omitempty"`
}

// ToolCall represents a record of a tool invocation.
//
// This is used for logging, auditing, and debugging purposes.
// It captures the complete context of a tool call.
type ToolCall struct {
	// ID is a unique identifier for this tool call.
	//
	// Useful for correlating calls across logs and traces.
	ID string `json:"id"`

	// ToolName is the name of the tool that was called.
	ToolName string `json:"tool_name"`

	// Args are the arguments passed to the tool.
	Args map[string]interface{} `json:"args"`

	// Result is the output from the tool.
	//
	// May be nil if the call is still in progress.
	Result *ToolOutput `json:"result,omitempty"`

	// Error contains error information if the call failed.
	//
	// Empty string if the call succeeded.
	Error string `json:"error,omitempty"`

	// StartTime is when the tool call started (Unix timestamp).
	StartTime int64 `json:"start_time"`

	// EndTime is when the tool call completed (Unix timestamp).
	//
	// Zero if the call is still in progress.
	EndTime int64 `json:"end_time,omitempty"`

	// Metadata contains additional context about the call.
	//
	// May include caller information, trace IDs, etc.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}
