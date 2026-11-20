package tools

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/kart-io/goagent/codec/json"
	shared "github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools/toolargs"
)

// DurableIntentVersioner versions otherwise opaque tool implementation semantics.
type DurableIntentVersioner interface{ DurableIntentVersion() string }

// InterruptibleTool declares that the tool may call durable.AwaitInput
// mid-execution; ok being false means it is not declared. version is the
// durable intent version and is required when enabled.
type InterruptibleTool interface {
	InterruptibleVersion() (version string, ok bool)
}

// DurablePolicyProvider specifies an explicit journal policy for composite calls.
type DurablePolicyProvider interface {
	DurableOperationPolicy() durable.OperationPolicy
}

// DispatchTimeoutProvider declares the tool's timeout when called through the
// Dispatcher; a positive value replaces DispatchConfig.Timeout, and 0 keeps it.
type DispatchTimeoutProvider interface{ DispatchTimeout() time.Duration }

// RunnableToolConfig defines trusted codecs and version identity. ModelInvoking is
// a host declaration, not a mechanism to discover hidden SDK requests.
// CleanupTimeout bounds OwnedRunnable.Close after parent cancellation; 0 means 5s.
// Timeout is passed to the Dispatcher via DispatchTimeoutProvider; 0 keeps the
// Dispatcher default. Sub-agents usually need it raised explicitly. It has no
// effect when Invoke is called directly; the timeout is then determined by the
// caller's ctx.
// Resumable declares that all effects of the subflow are recorded under the
// call's derived execution key (same Store, stable scope), so re-entering only
// replays: after a wait (*durable.WaitError, such as an approval pause in
// DurableGraph.AsChildRunnable) it can resume, and after an interrupted
// execution it can be re-entered. When enabled, it must be called through a
// journaled Dispatcher, otherwise it fails before starting the subflow.
// MaxAttempts bounds how many times a Resumable call is entered in total,
// counting the first, like DispatchConfig.MaxAttempts: 0 means
// constants.DefaultResumableToolAttempts (3) and 1 means an interrupted call
// is not re-entered. It is a persisted protocol value rather than a caller
// limit: the journal records it and replay rejects a changed value, so its
// default cannot move without failing calls journaled before an upgrade.
// It is ignored when Resumable is false. Negative values are rejected.
// MaxDepth caps nested runnable tools. It is an anti-abuse cap rather than a
// policy limit, because recursive delegation would otherwise never stop: 0
// means constants.DefaultRunnableToolMaxDepth and UnlimitedRunnableDepth removes the cap.
type RunnableToolConfig[I, O any] struct {
	Name, Description, ArgsSchema, Version string
	Decode                                 func(map[string]any) (I, error)
	Encode                                 func(O) (any, error)
	ModelInvoking                          bool
	Resumable                              bool
	MaxAttempts                            int
	MaxDepth                               int
	CleanupTimeout                         time.Duration
	Timeout                                time.Duration
}
type runnableTool[I, O any] struct {
	config  RunnableToolConfig[I, O]
	run     core.Runnable[I, O]
	schema  *jsonschema.Resolved
	factory func(context.Context) (OwnedRunnable[I, O], error)
}

// UnlimitedRunnableDepth passed as MaxDepth removes the nesting cap.
const UnlimitedRunnableDepth = -1

// NewRunnableTool adapts a fixed runnable without taking ownership of its resources.
func NewRunnableTool[I, O any](run core.Runnable[I, O], cfg RunnableToolConfig[I, O]) (interfaces.Tool, error) {
	if nilRunnable(run) {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "runnable required").
			WithComponent("runnable_tool").
			WithOperation("initialize").
			WithContext("tool_name", cfg.Name)
	}
	t, err := newRunnableTool(cfg)
	if err != nil {
		return nil, err
	}
	t.run = run
	return t, nil
}

// OwnedRunnable transfers only per-call resource ownership to the adapter.
type OwnedRunnable[I, O any] struct {
	Runnable core.Runnable[I, O]
	Close    func(context.Context) error
}

// NewRunnableToolFactory creates a new child session for each actual invocation.
func NewRunnableToolFactory[I, O any](factory func(context.Context) (OwnedRunnable[I, O], error), cfg RunnableToolConfig[I, O]) (interfaces.Tool, error) {
	if factory == nil {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "runnable factory required").
			WithComponent("runnable_tool").
			WithOperation("initialize").
			WithContext("tool_name", cfg.Name)
	}
	t, err := newRunnableTool(cfg)
	if err != nil {
		return nil, err
	}
	t.factory = factory
	return t, nil
}

func nilRunnable(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		return v.IsNil()
	}
	return false
}

func newRunnableTool[I, O any](cfg RunnableToolConfig[I, O]) (*runnableTool[I, O], error) {
	if cfg.Name == "" || strings.TrimSpace(cfg.Name) != cfg.Name || cfg.Version == "" || cfg.Decode == nil || cfg.Encode == nil || cfg.MaxAttempts < 0 || cfg.MaxDepth < UnlimitedRunnableDepth || cfg.CleanupTimeout < 0 || cfg.Timeout < 0 {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "tool requires name, version, codecs, valid attempts, depth, cleanup timeout and timeout").
			WithComponent("runnable_tool").
			WithOperation("initialize").
			WithContext("tool_name", cfg.Name)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal([]byte(cfg.ArgsSchema), &schema); err != nil {
		return nil, err
	}
	if schema.Type != "object" {
		return nil, agentErrors.Newf(agentErrors.CodeToolValidation, "tool schema must be object").
			WithComponent("runnable_tool").
			WithOperation("initialize").
			WithContext("tool_name", cfg.Name)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, err
	}
	if cfg.MaxDepth == 0 {
		cfg.MaxDepth = shared.DefaultRunnableToolMaxDepth
	}
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = shared.DefaultResumableToolAttempts
	}
	if cfg.CleanupTimeout == 0 {
		cfg.CleanupTimeout = shared.DefaultSessionCleanupTimeout
	}
	return &runnableTool[I, O]{config: cfg, schema: resolved}, nil
}
func (t *runnableTool[I, O]) Name() string                 { return t.config.Name }
func (t *runnableTool[I, O]) Description() string          { return t.config.Description }
func (t *runnableTool[I, O]) ArgsSchema() string           { return t.config.ArgsSchema }
func (t *runnableTool[I, O]) DurableIntentVersion() string { return t.config.Version }
func (t *runnableTool[I, O]) DurableOperationPolicy() durable.OperationPolicy {
	if t.config.Resumable {
		return durable.OperationPolicy{MaxAttempts: t.config.MaxAttempts, Resumable: true}
	}
	return durable.OperationPolicy{MaxAttempts: 1}
}

func (t *runnableTool[I, O]) DispatchTimeout() time.Duration { return t.config.Timeout }
func (t *runnableTool[I, O]) Invoke(ctx context.Context, input *interfaces.ToolInput) (out *interfaces.ToolOutput, err error) {
	defer func() {
		if v := recover(); v != nil {
			out = nil
			err = errors.Join(err, agentErrors.NewPanicError("Runnable 工具", v))
		}
	}()
	ctx, err = enterRunnable(ctx, t.config.MaxDepth, t.config.ModelInvoking)
	if err != nil {
		return nil, err
	}
	if t.config.Resumable && !durable.HasJournal(ctx) {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "resumable tool %q requires a journaled dispatcher call", t.config.Name).
			WithComponent("runnable_tool").
			WithOperation("invoke").
			WithContext("tool_name", t.config.Name)
	}
	if input == nil || input.Args == nil {
		return nil, agentErrors.Newf(agentErrors.CodeToolValidation, "tool object arguments required").
			WithComponent("runnable_tool").
			WithOperation("invoke").
			WithContext("tool_name", t.config.Name)
	}
	// 参数问题在启动子会话之前发现，可交还模型改正；Decode 只有返回 *ArgumentError 才可恢复。
	// 子会话及其内层工具的错误一律定性为本工具的执行失败，不能被外层当作参数错误。
	if err = toolargs.Validate(t.schema, input.Args); err != nil {
		message := err.Error()
		if detail, ok := err.(*agentErrors.AgentError); ok {
			message = detail.Message
		}
		return nil, &ArgumentError{Message: message, Err: err}
	}
	value, err := t.config.Decode(input.Args)
	if err != nil {
		return nil, err
	}
	run := t.run
	if t.factory != nil {
		owned, e := t.factory(ctx)
		if owned.Close != nil {
			defer func() {
				cleanCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), t.config.CleanupTimeout)
				defer cancel()
				err = errors.Join(err, Settle(ctx, owned.Close(cleanCtx)))
				if err != nil {
					out = nil
				}
			}()
		}
		if e != nil {
			return nil, Settle(ctx, e)
		}
		if owned.Close == nil || nilRunnable(owned.Runnable) {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidOutput, "owned runnable and cleanup required").
				WithComponent("runnable_tool").
				WithOperation("invoke").
				WithContext("tool_name", t.config.Name)
		}
		run = owned.Runnable
	}
	output, err := run.Invoke(ctx, value)
	if err != nil {
		return nil, Settle(ctx, err)
	}
	encoded, err := t.config.Encode(output)
	if err != nil {
		return nil, Settle(ctx, err)
	}
	return &interfaces.ToolOutput{Success: true, Result: encoded}, nil
}
