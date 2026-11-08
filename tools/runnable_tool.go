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

// InterruptibleTool 声明工具可以在执行中途调用 durable.AwaitInput；ok 为 false 时视为未声明。
// version 是持久 intent 版本，开启时必填。
type InterruptibleTool interface {
	InterruptibleVersion() (version string, ok bool)
}

// DurablePolicyProvider specifies an explicit journal policy for composite calls.
type DurablePolicyProvider interface {
	DurableOperationPolicy() durable.OperationPolicy
}

// DispatchTimeoutProvider 声明工具经 Dispatcher 调用时的超时，正值替代 DispatchConfig.Timeout，0 表示沿用它。
type DispatchTimeoutProvider interface{ DispatchTimeout() time.Duration }

// RunnableToolConfig defines trusted codecs and version identity. ModelInvoking is
// a host declaration, not a mechanism to discover hidden SDK requests.
// CleanupTimeout bounds OwnedRunnable.Close after parent cancellation; 0 means 5s.
// Timeout 经 DispatchTimeoutProvider 交给 Dispatcher，0 表示沿用 Dispatcher 默认值；子 Agent 通常需要显式放宽。
// 直接调用 Invoke 时它不生效，超时由调用方 ctx 决定。
// Resumable 声明子流程的全部效果都记在调用的派生执行键下（同一 Store、稳定作用域），重新进入只会回放：
// 等待（*durable.WaitError，如 DurableGraph.AsChildRunnable 的审批暂停）后可续跑，执行中断后可重新进入。
// 开启后必须经带日志的 Dispatcher 调用，否则在启动子流程前失败。
type RunnableToolConfig[I, O any] struct {
	Name, Description, ArgsSchema, Version string
	Decode                                 func(map[string]any) (I, error)
	Encode                                 func(O) (any, error)
	ModelInvoking                          bool
	Resumable                              bool
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
	if cfg.Name == "" || strings.TrimSpace(cfg.Name) != cfg.Name || cfg.Version == "" || cfg.Decode == nil || cfg.Encode == nil || cfg.MaxDepth < 0 || cfg.CleanupTimeout < 0 || cfg.Timeout < 0 {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "tool requires name, version, codecs, valid depth, cleanup timeout and timeout").
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
		cfg.MaxDepth = 8
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
		return durable.OperationPolicy{MaxAttempts: shared.DefaultResumableToolAttempts, Resumable: true}
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
