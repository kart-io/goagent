package builder

import (
	"fmt"
	"time"

	"github.com/kart-io/goagent/builder/contextpolicy"
	shared "github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/logging"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/memory"
)

// Runtime 配置方法
// 本文件包含 AgentBuilder 的运行时配置方法
//
// API 分层说明：
// - [Simple]: 日常 API，覆盖 80% 使用场景（常用配置）
// - [Core]: 标准 API，覆盖 95% 使用场景（性能调优）
// - [Advanced]: 完整 API，覆盖所有场景（细粒度控制）

// OutputFormat defines the LLM output format type.
type OutputFormat string

const (
	// OutputFormatDefault specifies no format; the LLM decides.
	OutputFormatDefault OutputFormat = ""

	// OutputFormatPlainText is plain text without Markdown syntax.
	OutputFormatPlainText OutputFormat = "plain_text"

	// OutputFormatMarkdown is Markdown format.
	OutputFormatMarkdown OutputFormat = "markdown"

	// OutputFormatJSON is JSON format.
	OutputFormatJSON OutputFormat = "json"

	// OutputFormatCustom is a custom format (used with CustomOutputPrompt).
	OutputFormatCustom OutputFormat = "custom"
)

// outputFormatPrompts 存储各格式对应的提示词
var outputFormatPrompts = map[OutputFormat]string{
	OutputFormatPlainText: "请使用纯文本格式回复，不要使用 Markdown 语法。",
	OutputFormatMarkdown:  "请使用 Markdown 格式回复。",
	OutputFormatJSON:      "请使用 JSON 格式回复。",
}

// AgentConfig holds agent configuration options.
type AgentConfig struct {
	OutputValidators []OutputValidator      `json:"-"`
	Reasoning        *usage.ReasoningConfig `json:"reasoning,omitzero"`
	UsageHook        budget.UsageHook       `json:"-"`
	AgentID          string                 `json:",omitempty"`
	// TokenEstimator is the host's conservative token estimator; it is not
	// written to the durable configuration.
	TokenEstimator TokenEstimator `json:"-"`
	// Logger receives run lifecycle records and the Builder's own warnings;
	// see WithLogger. It is not written to the durable configuration.
	Logger logging.Logger `json:"-"`

	// DurableScope identifies one logical Agent invocation within a durable node.
	// It must be unique among siblings and stable across restarts.
	DurableScope string

	// Offloads are the result offloads installed by WithOffload; they apply
	// only to the native tool stream.
	Offloads []*contextpolicy.Offload `json:"-"`

	// LongTermMemory is the long-term memory attached by WithLongTermMemory.
	LongTermMemory *memory.Memory `json:"-"`

	// ModelCache is the model call cache configured by WithModelCache.
	ModelCache *ModelCache `json:"-"`

	// ReturnDirectTools lets a single tool call provide the final answer
	// directly; it applies only to the native tool stream.
	ReturnDirectTools []string

	// EmptyAnswerRetries is the number of times to re-ask when the model
	// finishes normally but returns an empty answer; it applies only to the
	// native tool stream. 0 means no re-asking.
	EmptyAnswerRetries int

	// ParallelToolCalls is the maximum number of tool calls in one round that
	// may run concurrently; below 2 (default 0) they run sequentially.
	ParallelToolCalls int

	// ToolErrorFeedback hands recoverable tool failures to the model as tool
	// results; it applies only to the native tool stream. See
	// WithToolErrorFeedback.
	ToolErrorFeedback bool
	// ReplayablePartialOutput declares that the caller can discard deltas
	// already emitted by a failed request; see WithReplayablePartialOutput.
	ReplayablePartialOutput bool

	// Loop extension points and prompt assembly; see loop_hooks.go. They apply
	// only to the native tool stream.
	PreStep              []preStep          `json:"-"`
	RequestHooks         []RequestHook      `json:"-"`
	RequestErrorHooks    []RequestErrorHook `json:"-"`
	MaxRequestRecoveries int
	TurnStopping         []TurnStoppingHook `json:"-"`
	PromptSections       []PromptSection    `json:"-"`
	PromptContexts       []PromptContext    `json:"-"`
	SessionLog           session.Store      `json:"-"`
	// SessionState: see WithSessionState.
	SessionState bool

	// ResendInterruptedModel: see WithInterruptedModelResend.
	ResendInterruptedModel bool

	// MaxIterations limits the maximum number of reasoning steps.
	MaxIterations int

	// Timeout sets the agent execution timeout.
	Timeout time.Duration

	// MaxTokens limits the maximum number of tokens in the LLM response; 0
	// means it is not sent and the LLM client configuration decides.
	MaxTokens int

	// Temperature controls the randomness of LLM sampling; it is sent only when
	// TemperatureSet is true.
	Temperature float64

	// TemperatureSet reports whether Temperature was set explicitly; when
	// false, the LLM client configuration decides.
	TemperatureSet bool

	// SessionID is the default session ID for the session log; a session ID
	// supplied in the request context via tools.WithSessionID takes precedence.
	SessionID string

	// Verbose enables verbose logging.
	Verbose bool

	// OutputFormat specifies the LLM output format. Supported values:
	// OutputFormatDefault (default), OutputFormatPlainText,
	// OutputFormatMarkdown, OutputFormatJSON, and OutputFormatCustom.
	OutputFormat OutputFormat

	// CustomOutputPrompt is the custom output format prompt. It takes effect
	// only when OutputFormat is OutputFormatCustom.
	CustomOutputPrompt string
}

// WithDurableScope sets a host-owned stable invocation name. Durable execution
// requires it; different agents/calls in the same node must use different scopes.
// Reusing the same scope means replaying the same logical invocation.
func (b *AgentBuilder[S]) WithDurableScope(scope string) *AgentBuilder[S] {
	b.config.DurableScope = scope
	return b
}

// WithInterruptedModelResend makes a resumed durable run resend model requests
// that were interrupted by a process stop, crash, or deadline, sending at most
// 3 times per turn. Resending is off by default because an interrupted request
// may already have been billed: enabling it may cause duplicate charges, each
// resend takes its own budget share, and nothing is sent when the quota is
// insufficient. Committed turns, requests the model explicitly failed, and tool
// calls are unaffected. The switch must stay the same for the entire lifetime
// of a run; otherwise resuming reports a policy change.
func (b *AgentBuilder[S]) WithInterruptedModelResend() *AgentBuilder[S] {
	b.config.ResendInterruptedModel = true
	return b
}

// WithReturnDirectTools names terminal tools whose results can be shown
// directly, skipping the model's summarization request. It takes effect in
// ExecuteStream/AsRunnable and requires a ToolStreamClient. Each call replaces
// the list. When a round has multiple tool calls, the full model loop still
// runs; string results are output as-is and other results are encoded as JSON.
// Whoever enables it is responsible for the format, completeness, and
// suitability of the tool results; model output format instructions are no
// longer applied.
func (b *AgentBuilder[S]) WithReturnDirectTools(names ...string) *AgentBuilder[S] {
	b.config.ReturnDirectTools = append([]string(nil), names...)
	return b
}

// WithEmptyAnswerRetries sets how many times to re-ask after an empty answer
// (default 0, no re-asking).
//
// [Advanced] When the model stops with neither content nor tool calls, no empty
// assistant message is appended; instead a fixed user message asking it to
// answer directly or call a tool is appended and the next round is requested.
// Each re-ask uses one MaxIterations slot and consumes one budget share; after
// n re-asks, or on the last round, it errors as before. In durable mode an
// empty turn is recorded as completed and replayed on resume. Negative values
// are treated as 0.
func (b *AgentBuilder[S]) WithEmptyAnswerRetries(n int) *AgentBuilder[S] {
	b.config.EmptyAnswerRetries = max(n, 0)
	return b
}

// WithParallelToolCalls sets the concurrency limit for tool calls within one
// round (default 0, sequential).
//
// [Advanced] It applies only to the native tool stream. With n>=2, when a round
// has multiple calls and every called tool implements
// interfaces.ConcurrentCallTool (such as Dispatcher.Tools), up to n calls run
// concurrently; the actual peak is also bounded by the tool's own throttling
// (Dispatcher.MaxConcurrency). Pass math.MaxInt for no extra limit. n<2 runs
// sequentially and negative values are treated as 0; if any tool does not
// declare concurrency support, the whole round runs sequentially.
//
// With concurrency, execution order is not guaranteed to match the order the
// model gave, even with Dispatcher.MaxConcurrency=1; tools must not assign
// numbers by execution order or depend on the ordering of calls within the same
// round. Results are still returned to the model in the model's order:
// tool_start is emitted before dispatch and tool_end after all calls finish,
// both in that order, and the Dispatcher's OnResult also fires in that order
// after all calls finish. A failed call does not cancel the others; after all
// finish, the earliest error in order is returned.
func (b *AgentBuilder[S]) WithParallelToolCalls(n int) *AgentBuilder[S] {
	b.config.ParallelToolCalls = max(n, 0)
	return b
}

// WithToolErrorFeedback lets the model see recoverable tool failures and
// correct itself instead of aborting the run (off by default).
//
// [Advanced] It applies only to the native tool stream. When enabled, the
// following are handed to the model as business rejections (tools.Rejected) and
// use the normal iteration quota:
//   - Calls to tools not offered this round, or arguments that are not a JSON
//     object: these depend on model output and also apply in durable runs.
//   - Tools returning an error: non-durable runs only. Durable runs cannot
//     record a failed result and would re-execute on resume, so they still
//     abort as before.
//
// A finished run ctx, budget exhaustion, panics, and durable waits always abort
// the run.
func (b *AgentBuilder[S]) WithToolErrorFeedback() *AgentBuilder[S] {
	b.config.ToolErrorFeedback = true
	return b
}

// WithReplayablePartialOutput declares that the caller uses only the final
// result, or can discard deltas already emitted by a failed request (off by
// default). When enabled and a model request fails midway, the RequestError
// hook always sees Emitted as false, and RetryTransient resends as usual. It
// suits callers that only take the final reply, such as AsRunnable().Invoke:
// when the server is overloaded, the stream often emits a few bytes and then
// stalls, and under the default rule such a failure is not retried. For callers
// that display output incrementally through ExecuteStream or Stream, enabling
// it makes retries show already-displayed fragments a second time.
func (b *AgentBuilder[S]) WithReplayablePartialOutput() *AgentBuilder[S] {
	b.config.ReplayablePartialOutput = true
	return b
}

// DefaultAgentConfig returns the default configuration.
func DefaultAgentConfig() *AgentConfig {
	return &AgentConfig{
		MaxIterations: shared.DefaultAgentMaxIterations,
		Timeout:       shared.DefaultAgentExecutionTimeout,
		SessionID:     fmt.Sprintf("session-%d", time.Now().UnixNano()),
		Verbose:       false,
	}
}

// WithMaxIterations sets the maximum number of iterations.
//
// [Simple] Common option that controls the maximum number of agent reasoning
// steps (default 10). Adjust it to task complexity: 5-10 for simple tasks,
// 15-30 for complex ones.
func (b *AgentBuilder[S]) WithMaxIterations(max int) *AgentBuilder[S] {
	if max > 0 {
		b.config.MaxIterations = max
	}
	return b
}

// WithTimeout sets the timeout.
//
// [Core] Standard option that prevents agent execution from running too long
// (default 5 minutes).
func (b *AgentBuilder[S]) WithTimeout(timeout time.Duration) *AgentBuilder[S] {
	if timeout > 0 {
		b.config.Timeout = timeout
	}
	return b
}

// WithMaxTokens sets the maximum number of tokens.
//
// [Core] Standard option that limits the maximum number of tokens in the LLM
// response (unset by default, deferring to the LLM client configuration). Use
// it to control cost and response length.
func (b *AgentBuilder[S]) WithMaxTokens(max int) *AgentBuilder[S] {
	if max > 0 {
		b.config.MaxTokens = max
	}
	return b
}

// WithTemperature sets the temperature (controls randomness).
//
// [Simple] Common option that controls the creativity of LLM output (unset by
// default, deferring to the LLM client configuration).
// - 0.0-0.3: precise, deterministic (fact lookup, code generation)
// - 0.4-0.7: balanced (general conversation)
// - 0.8-1.0: creative (writing, brainstorming)
func (b *AgentBuilder[S]) WithTemperature(temp float64) *AgentBuilder[S] {
	if temp >= 0 && temp <= 2.0 {
		b.config.Temperature, b.config.TemperatureSet = temp, true
	}
	return b
}

// WithSessionID sets the session ID.
//
// [Advanced] Used for checkpointing and session recovery (auto-generated by
// default).
func (b *AgentBuilder[S]) WithSessionID(sessionID string) *AgentBuilder[S] {
	if sessionID != "" {
		b.config.SessionID = sessionID
	}
	return b
}

// WithVerbose sets whether verbose logging is enabled.
//
// [Core] Standard option for debugging and development (default false).
func (b *AgentBuilder[S]) WithVerbose(verbose bool) *AgentBuilder[S] {
	b.config.Verbose = verbose
	return b
}

// WithOutputFormat sets the LLM output format.
//
// [Simple] Common option that controls the output format of LLM responses.
// Supported formats:
//   - OutputFormatDefault: no format specified; the LLM decides
//   - OutputFormatPlainText: plain text without Markdown syntax (suits terminal
//     display)
//   - OutputFormatMarkdown: Markdown (suits rich text display)
//   - OutputFormatJSON: JSON (suits programmatic parsing)
//
// Example:
//
//	agent, err := builder.NewSimpleBuilder(llmClient).
//	    WithSystemPrompt("You are an assistant").
//	    WithOutputFormat(builder.OutputFormatPlainText).
//	    Build()
func (b *AgentBuilder[S]) WithOutputFormat(format OutputFormat) *AgentBuilder[S] {
	b.config.OutputFormat = format
	return b
}

// WithCustomOutputFormat sets a custom output format prompt.
//
// [Simple] Common option that lets the user specify an arbitrary format prompt.
// It automatically sets OutputFormat to OutputFormatCustom.
//
// Example:
//
//	agent, err := builder.NewSimpleBuilder(llmClient).
//	    WithSystemPrompt("You are an assistant").
//	    WithCustomOutputFormat("Reply in a table, one item per row").
//	    Build()
func (b *AgentBuilder[S]) WithCustomOutputFormat(prompt string) *AgentBuilder[S] {
	if prompt != "" {
		b.config.OutputFormat = OutputFormatCustom
		b.config.CustomOutputPrompt = prompt
	}
	return b
}

// GetOutputFormatPrompt returns the prompt for the given output format. It
// returns an empty string for Default or undefined formats.
func GetOutputFormatPrompt(format OutputFormat) string {
	if prompt, ok := outputFormatPrompts[format]; ok {
		return prompt
	}
	return ""
}
