package interfaces

import (
	"context"
)

// ReasoningPattern defines the interface for different reasoning strategies.
//
// This interface allows agents to implement various reasoning patterns such as:
// - Chain-of-Thought (CoT): Linear step-by-step reasoning
// - Tree-of-Thought (ToT): Tree-based search with multiple reasoning paths
// - Graph-of-Thought (GoT): Graph-based reasoning with complex dependencies
// - Program-of-Thought (PoT): Code generation and execution for reasoning
// - Skeleton-of-Thought (SoT): Parallel reasoning with skeleton structure
// - Meta-CoT/Self-Ask: Self-questioning and meta-reasoning
type ReasoningPattern interface {
	// Name returns the name of the reasoning pattern.
	Name() string

	// Description returns a description of the reasoning pattern.
	Description() string

	// Process executes the reasoning pattern on the given input.
	Process(ctx context.Context, input *ReasoningInput) (*ReasoningOutput, error)

	// Stream processes with streaming support for incremental results.
	Stream(ctx context.Context, input *ReasoningInput) (<-chan *ReasoningChunk, error)
}

// ReasoningInput represents input for a reasoning pattern.
type ReasoningInput struct {
	// Query is the main question or task to reason about
	Query string `json:"query"`

	// Context provides additional information for reasoning
	Context map[string]interface{} `json:"context,omitempty"`

	// Tools available for the reasoning process
	Tools []Tool `json:"tools,omitempty"`

	// Config contains pattern-specific configuration
	Config map[string]interface{} `json:"config,omitempty"`

	// History of previous reasoning steps (for iterative patterns)
	History []ReasoningStep `json:"history,omitempty"`
}

// ReasoningOutput represents the output from a reasoning pattern.
type ReasoningOutput struct {
	// Answer is the final answer or conclusion
	Answer string `json:"answer"`

	// Steps contains the reasoning steps taken
	Steps []ReasoningStep `json:"steps"`

	// Confidence score (0-1) in the answer
	Confidence float64 `json:"confidence,omitempty"`

	// Metadata contains pattern-specific output data
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// ToolCalls made during reasoning
	ToolCalls []ReasoningToolCall `json:"tool_calls,omitempty"`
}

// ReasoningStep represents a single step in the reasoning process.
type ReasoningStep struct {
	// StepID uniquely identifies this step
	StepID string `json:"step_id"`

	// Type of reasoning step (e.g., "thought", "action", "observation", "branch")
	Type string `json:"type"`

	// Content of the reasoning step
	Content string `json:"content"`

	// Score or evaluation of this step (for search-based patterns)
	Score float64 `json:"score,omitempty"`

	// ParentID links to the parent step (for tree/graph patterns)
	ParentID string `json:"parent_id,omitempty"`

	// ChildrenIDs links to child steps (for tree/graph patterns)
	ChildrenIDs []string `json:"children_ids,omitempty"`

	// Metadata for step-specific data
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ReasoningChunk represents a streaming chunk of reasoning output.
type ReasoningChunk struct {
	// Step is a partial or complete reasoning step
	Step *ReasoningStep `json:"step,omitempty"`

	// PartialAnswer is incremental answer content
	PartialAnswer string `json:"partial_answer,omitempty"`

	// Done indicates if reasoning is complete
	Done bool `json:"done"`

	// Error if something went wrong
	Error error `json:"error,omitempty"`
}

// ReasoningToolCall represents a tool invocation during reasoning.
// This extends the basic ToolCall with reasoning-specific fields.
type ReasoningToolCall struct {
	// ToolName identifies the tool
	ToolName string `json:"tool_name"`

	// Input parameters for the tool
	Input map[string]interface{} `json:"input"`

	// Output from the tool execution
	Output interface{} `json:"output,omitempty"`

	// Error if tool execution failed
	Error string `json:"error,omitempty"`

	// ReasoningStepID links this call to a specific reasoning step
	ReasoningStepID string `json:"reasoning_step_id,omitempty"`
}

// ThoughtNode represents a node in tree/graph-based reasoning patterns.
type ThoughtNode struct {
	// ID uniquely identifies this node
	ID string `json:"id"`

	// Thought content at this node
	Thought string `json:"thought"`

	// Score or evaluation of this thought
	Score float64 `json:"score"`

	// State at this node (for stateful reasoning)
	State map[string]interface{} `json:"state,omitempty"`

	// Parent node reference
	Parent *ThoughtNode `json:"-"`

	// Children nodes
	Children []*ThoughtNode `json:"-"`

	// Visited flag for graph traversal
	Visited bool `json:"visited"`

	// Depth in the tree/graph
	Depth int `json:"depth"`
}

// ProgramCode represents generated code in Program-of-Thought pattern.
type ProgramCode struct {
	// Language of the code (e.g., "python", "javascript")
	Language string `json:"language"`

	// Code content
	Code string `json:"code"`

	// ExecutionResult after running the code
	ExecutionResult interface{} `json:"execution_result,omitempty"`

	// Error if execution failed
	Error string `json:"error,omitempty"`
}

// SkeletonPoint represents a point in the Skeleton-of-Thought pattern.
type SkeletonPoint struct {
	// ID uniquely identifies this skeleton point
	ID string `json:"id"`

	// Question or sub-problem to solve
	Question string `json:"question"`

	// Answer to this skeleton point
	Answer string `json:"answer,omitempty"`

	// Dependencies on other skeleton points
	Dependencies []string `json:"dependencies,omitempty"`

	// Status of this point ("pending", "processing", "completed")
	Status string `json:"status"`
}

// ReasoningStrategy defines different strategies for reasoning patterns.
type ReasoningStrategy string

const (
	// StrategyDepthFirst uses depth-first search for tree/graph patterns
	StrategyDepthFirst ReasoningStrategy = "depth_first"

	// StrategyBreadthFirst uses breadth-first search for tree/graph patterns
	StrategyBreadthFirst ReasoningStrategy = "breadth_first"

	// StrategyBeamSearch uses beam search with limited candidates
	StrategyBeamSearch ReasoningStrategy = "beam_search"

	// StrategyMonteCarlo uses Monte Carlo Tree Search
	StrategyMonteCarlo ReasoningStrategy = "monte_carlo"

	// StrategyGreedy uses greedy selection at each step
	StrategyGreedy ReasoningStrategy = "greedy"
)
