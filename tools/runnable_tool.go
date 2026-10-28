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

// RunnableToolConfig defines trusted codecs and version identity. ModelInvoking is
// a host declaration, not a mechanism to discover hidden SDK requests.
// CleanupTimeout bounds OwnedRunnable.Close after parent cancellation; 0 means 5s.
type RunnableToolConfig[I, O any] struct {
	Name, Description, ArgsSchema, Version string
	Decode                                 func(map[string]any) (I, error)
	Encode                                 func(O) (any, error)
	ModelInvoking                          bool
	MaxDepth                               int
	CleanupTimeout                         time.Duration
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
	if cfg.Name == "" || strings.TrimSpace(cfg.Name) != cfg.Name || cfg.Version == "" || cfg.Decode == nil || cfg.Encode == nil || cfg.MaxDepth < 0 || cfg.CleanupTimeout < 0 {
		return nil, fmt.Errorf("tool requires name, version, codecs, valid depth and cleanup timeout")
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
	return durable.OperationPolicy{MaxAttempts: 1}
}
func (t *runnableTool[I, O]) Invoke(ctx context.Context, input *interfaces.ToolInput) (out *interfaces.ToolOutput, err error) {
	defer func() {
		if v := recover(); v != nil {
			out = nil
			err = errors.Join(err, fmt.Errorf("runnable tool panicked: %v", v))
		}
	}()
	ctx, err = enterRunnable(ctx, t.config.MaxDepth, t.config.ModelInvoking)
	if err != nil {
		return nil, err
	}
	if input == nil || input.Args == nil {
		return nil, fmt.Errorf("tool object arguments required")
	}
	// 参数问题在启动子会话之前发现，可交还模型改正；子会话自身的失败不在此列。
	if err = t.schema.Validate(input.Args); err != nil {
		return nil, &ArgumentError{Message: err.Error(), Err: err}
	}
	value, err := t.config.Decode(input.Args)
	if err != nil {
		return nil, &ArgumentError{Message: err.Error(), Err: err}
	}
	run := t.run
	if t.factory != nil {
		owned, e := t.factory(ctx)
		if owned.Close != nil {
			defer func() {
				cleanCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), t.config.CleanupTimeout)
				defer cancel()
				err = errors.Join(err, owned.Close(cleanCtx))
				if err != nil {
					out = nil
				}
			}()
		}
		if e != nil {
			return nil, e
		}
		if owned.Close == nil || nilRunnable(owned.Runnable) {
			return nil, fmt.Errorf("owned runnable and cleanup required")
		}
		run = owned.Runnable
	}
	output, err := run.Invoke(ctx, value)
	if err != nil {
		return nil, err
	}
	encoded, err := t.config.Encode(output)
	if err != nil {
		return nil, err
	}
	return &interfaces.ToolOutput{Success: true, Result: encoded}, nil
}
