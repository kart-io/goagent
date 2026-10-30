package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// ErrToolWorkflowWaiting prevents host-required continuation from becoming an answer.
var ErrToolWorkflowWaiting = errors.New("tool workflow requires host continuation")

// DurableIntentVersioner versions otherwise opaque tool implementation semantics.
type DurableIntentVersioner interface{ DurableIntentVersion() string }

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
		return nil, fmt.Errorf("runnable required")
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
		return nil, fmt.Errorf("runnable factory required")
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
		return nil, fmt.Errorf("tool requires name, version, codecs, valid depth, cleanup timeout and timeout")
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal([]byte(cfg.ArgsSchema), &schema); err != nil {
		return nil, err
	}
	if schema.Type != "object" {
		return nil, fmt.Errorf("tool schema must be object")
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, err
	}
	if cfg.MaxDepth == 0 {
		cfg.MaxDepth = 8
	}
	if cfg.CleanupTimeout == 0 {
		cfg.CleanupTimeout = 5 * time.Second
	}
	return &runnableTool[I, O]{config: cfg, schema: resolved}, nil
}
func (t *runnableTool[I, O]) Name() string                 { return t.config.Name }
func (t *runnableTool[I, O]) Description() string          { return t.config.Description }
func (t *runnableTool[I, O]) ArgsSchema() string           { return t.config.ArgsSchema }
func (t *runnableTool[I, O]) DurableIntentVersion() string { return t.config.Version }
func (t *runnableTool[I, O]) DurableOperationPolicy() durable.OperationPolicy {
	if t.config.Resumable {
		return durable.OperationPolicy{MaxAttempts: resumableAttempts, Resumable: true}
	}
	return durable.OperationPolicy{MaxAttempts: 1}
}

// resumableAttempts 限制可续跑工具在执行中断后重新进入的次数；重新进入只回放已完成的步骤，
// 但反复中断说明需要宿主介入。从等待中续跑不计次数。
const resumableAttempts = 3

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
		return nil, fmt.Errorf("resumable tool %q requires a journaled dispatcher call", t.config.Name)
	}
	if input == nil || input.Args == nil {
		return nil, fmt.Errorf("tool object arguments required")
	}
	// 参数问题在启动子会话之前发现，可交还模型改正；Decode 只有返回 *ArgumentError 才可恢复。
	// 子会话及其内层工具的错误一律定性为本工具的执行失败，不能被外层当作参数错误。
	if err = t.schema.Validate(input.Args); err != nil {
		return nil, &ArgumentError{Message: err.Error(), Err: err}
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
			return nil, fmt.Errorf("owned runnable and cleanup required")
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
