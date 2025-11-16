package interfaces

import "context"

// Agent represents an autonomous agent that can process inputs and produce outputs.
//
// All agent implementations (react, executor, specialized) should implement this interface.
// This is the canonical definition; all other references should use type aliases.
//
// The Agent interface extends Runnable and adds agent-specific methods for:
//   - Identifying the agent (Name, Description)
//   - Generating execution plans
//
// Implementations:
//   - core.BaseAgent - Base implementation with Runnable support
//   - agents/executor.ExecutorAgent - Tool execution agent
//   - agents/react.ReactAgent - ReAct reasoning agent
//   - agents/specialized.SpecializedAgent - Domain-specific agents
type Agent interface {
	Runnable

	// Name returns the agent's identifier.
	Name() string

	// Description returns what the agent does.
	Description() string

	// Plan generates an execution plan for the given input.
	// This is optional and may return nil if the agent doesn't support planning.
	Plan(ctx context.Context, input *Input) (*Plan, error)
}

// Runnable represents any component that can be invoked with input to produce output.
//
// This is the foundation interface implemented by agents, chains, and tools.
// It provides core execution capabilities including:
//   - Synchronous execution (Invoke)
//   - Streaming execution (Stream)
//
// The Runnable interface enables composition and chaining of components through
// standard execution patterns.
//
// Implementations:
//   - core.BaseRunnable - Base implementation with callbacks
//   - core.BaseAgent - Agent-specific implementation
//   - core.Chain - Chain of runnables
type Runnable interface {
	// Invoke executes the runnable with the given input.
	// This is the primary execution method for synchronous processing.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control
	//   - input: Input data for the runnable
	//
	// Returns:
	//   - output: Result of the execution
	//   - error: Error if execution fails
	Invoke(ctx context.Context, input *Input) (*Output, error)

	// Stream executes with streaming output support.
	// Allows processing output as it becomes available.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control
	//   - input: Input data for the runnable
	//
	// Returns:
	//   - chan: Channel of stream chunks
	//   - error: Error if stream setup fails
	Stream(ctx context.Context, input *Input) (<-chan *StreamChunk, error)
}

// Input represents standardized input to a runnable.
//
// Input provides a flexible structure for passing data to runnables:
//   - Messages: Conversation history for LLM-based processing
//   - State: Persistent state data
//   - Config: Runtime configuration options
type Input struct {
	// Messages is the conversation history or input messages.
	Messages []Message `json:"messages"`

	// State is the persistent state data.
	// Can store arbitrary key-value pairs for state management.
	State State `json:"state"`

	// Config contains runtime configuration options.
	// Can include model settings, tool settings, etc.
	Config map[string]interface{} `json:"config,omitempty"`
}

// Output represents standardized output from a runnable.
//
// Output provides a structured way to return results:
//   - Messages: Generated or modified messages
//   - State: Updated state data
//   - Metadata: Additional information about execution
type Output struct {
	// Messages is the generated or modified messages.
	Messages []Message `json:"messages"`

	// State is the updated state data.
	State State `json:"state"`

	// Metadata contains additional information about the execution.
	// Can include timing, model info, tool calls, etc.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Message represents a single message in a conversation.
//
// Messages are used for:
//   - Chat history
//   - LLM interactions
//   - Agent communication
//
// The Role field indicates the message sender:
//   - "user": User input
//   - "assistant": AI/Agent response
//   - "system": System instructions
//   - "function": Function/Tool output
type Message struct {
	// Role identifies the message sender (user, assistant, system, function).
	Role string `json:"role"`

	// Content is the message text content.
	Content string `json:"content"`

	// Name is the optional name of the sender (for function/tool messages).
	Name string `json:"name,omitempty"`
}

// StreamChunk represents a chunk of streaming output.
//
// StreamChunk allows incremental processing of output:
//   - Content: Partial or complete output
//   - Metadata: Contextual information
//   - Done: Flag indicating stream completion
type StreamChunk struct {
	// Content is the chunk data (can be partial output).
	Content string `json:"content"`

	// Metadata contains contextual information about the chunk.
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// Done indicates if this is the final chunk.
	Done bool `json:"done"`
}

// Plan represents an agent's execution plan.
//
// Plan describes the steps an agent will take to accomplish a task:
//   - Steps: Ordered list of actions
//   - Metadata: Planning metadata (confidence, reasoning, etc.)
//
// Plans are generated by Agent.Plan() and used for:
//   - Transparency (showing what the agent will do)
//   - Validation (checking if plan is acceptable)
//   - Optimization (modifying plan before execution)
type Plan struct {
	// Steps is the ordered list of actions to execute.
	Steps []Step `json:"steps"`

	// Metadata contains planning metadata (confidence, reasoning, etc.).
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Step represents a single step in an execution plan.
//
// Each step describes:
//   - Action: What to do
//   - Input: Parameters for the action
//   - ToolName: Tool to invoke (if applicable)
type Step struct {
	// Action is the action to perform (e.g., "search", "analyze", "format").
	Action string `json:"action"`

	// Input contains parameters for the action.
	Input map[string]interface{} `json:"input"`

	// ToolName is the tool to invoke (if this is a tool call step).
	ToolName string `json:"tool_name,omitempty"`
}

// State represents agent state that can be persisted.
//
// State is a flexible key-value store for:
//   - Agent memory
//   - Conversation context
//   - Intermediate results
//   - Configuration data
//
// State is preserved across invocations when using checkpointing.
type State map[string]interface{}
