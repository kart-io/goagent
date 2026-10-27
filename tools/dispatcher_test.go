package tools_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

type dispatchTestTool struct {
	name, schema string
	run          func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error)
	calls        atomic.Int32
}

func (t *dispatchTestTool) Name() string        { return t.name }
func (t *dispatchTestTool) Description() string { return "test tool" }
func (t *dispatchTestTool) ArgsSchema() string  { return t.schema }
func (t *dispatchTestTool) Invoke(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	t.calls.Add(1)
	return t.run(ctx, in)
}

const strictOrderSchema = `{"type":"object","properties":{"order":{"type":"object","properties":{"id":{"type":"string","minLength":1}},"required":["id"],"additionalProperties":false},"amount":{"type":"integer","minimum":1}},"required":["order","amount"],"additionalProperties":false}`

func validToolInput() *interfaces.ToolInput {
	return &interfaces.ToolInput{Args: map[string]any{"order": map[string]any{"id": "order-1"}, "amount": int64(9007199254740993)}, CallerID: "trusted-user"}
}
func newTestDispatcher(t *testing.T, tool interfaces.Tool, cfg tools.DispatchConfig) *tools.Dispatcher {
	t.Helper()
	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(tool))
	d, err := tools.NewDispatcher(registry, cfg)
	require.NoError(t, err)
	return d
}
func TestDispatcherValidationAuthorizationAndEffectKey(t *testing.T) {
	ctx := durable.WithExecutionKey(context.Background(), "node-key")
	authorized := 0
	tool := &dispatchTestTool{name: "refund", schema: strictOrderSchema, run: func(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		if in.Context != ctx || in.ExecutionKey != "node-key" || in.CallerID != "trusted-user" {
			return nil, errors.New("host metadata lost")
		}
		if amount, ok := in.Args["amount"].(int64); !ok || amount != 9007199254740993 {
			return nil, errors.New("integer precision lost")
		}
		return &interfaces.ToolOutput{Success: true, Result: "receipt"}, nil
	}}
	d := newTestDispatcher(t, tool, tools.DispatchConfig{Authorize: func(_ context.Context, _ string, in *interfaces.ToolInput) error {
		authorized++
		if in.CallerID != "trusted-user" {
			return errors.New("denied")
		}
		return nil
	}})
	for _, change := range []func(*interfaces.ToolInput){
		func(in *interfaces.ToolInput) { in.Args["extra"] = "no" },
		func(in *interfaces.ToolInput) { in.Args["order"] = map[string]any{"id": ""} },
		func(in *interfaces.ToolInput) { in.Args["order"] = map[string]any{"id": "ok", "extra": true} },
		func(in *interfaces.ToolInput) { in.Args["amount"] = 1.5 },
		func(in *interfaces.ToolInput) { delete(in.Args, "amount") },
	} {
		in := validToolInput()
		change(in)
		_, err := d.Invoke(ctx, tools.ToolRequest{Name: "refund", Input: in})
		require.Error(t, err)
	}
	require.Zero(t, tool.calls.Load())
	require.Zero(t, authorized)
	in := validToolInput()
	in.CallerID = "attacker"
	_, err := d.Invoke(ctx, tools.ToolRequest{Name: "refund", Input: in})
	require.ErrorContains(t, err, "denied")
	require.Zero(t, tool.calls.Load())
	in = validToolInput()
	in.ExecutionKey = "untrusted-key"
	out, err := d.AsRunnable().Invoke(ctx, tools.ToolRequest{Name: "refund", Input: in})
	require.NoError(t, err)
	require.Equal(t, "receipt", out.Result)
	out, err = d.Tools()[0].Invoke(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "receipt", out.Result)
	require.EqualValues(t, 2, tool.calls.Load())
	require.Equal(t, "untrusted-key", in.ExecutionKey, "input metadata must not be overwritten")
}

func TestDispatcherFailuresNeverBecomeSuccessOrRetry(t *testing.T) {
	for _, mode := range []string{"error", "nil", "failed", "error-field", "panic", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			tool := &dispatchTestTool{name: "op", schema: `{"type":"object"}`, run: func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				switch mode {
				case "error":
					return nil, errors.New("upstream failure")
				case "nil":
					return nil, nil
				case "failed":
					return &interfaces.ToolOutput{Success: false}, nil
				case "error-field":
					return &interfaces.ToolOutput{Success: true, Error: "failed"}, nil
				case "panic":
					panic("private implementation details")
				default:
					cancel()
					return &interfaces.ToolOutput{Success: true}, nil
				}
			}}
			d := newTestDispatcher(t, tool, tools.DispatchConfig{})
			out, err := d.Invoke(ctx, tools.ToolRequest{Name: "op", Input: &interfaces.ToolInput{Args: map[string]any{}}})
			require.Error(t, err)
			require.Nil(t, out)
			require.EqualValues(t, 1, tool.calls.Load())
			require.NotContains(t, err.Error(), "private implementation details")
		})
	}
}

func TestDispatcherQueueCancellationAndTimeout(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	tool := &dispatchTestTool{name: "block", schema: `{"type":"object"}`, run: func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		entered <- struct{}{}
		select {
		case <-release:
			return &interfaces.ToolOutput{Success: true, Result: "ok"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	d := newTestDispatcher(t, tool, tools.DispatchConfig{MaxConcurrency: 1, Timeout: 2 * time.Second})
	req := tools.ToolRequest{Name: "block", Input: &interfaces.ToolInput{Args: map[string]any{}}}
	done := make(chan error, 1)
	go func() { _, err := d.Invoke(context.Background(), req); done <- err }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := d.Invoke(ctx, req)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	close(release)
	require.NoError(t, <-done)
	require.EqualValues(t, 1, tool.calls.Load())
	// Timeout during active work is also propagated, with no detached goroutine.
	d2 := newTestDispatcher(t, &dispatchTestTool{name: "timeout", schema: `{"type":"object"}`, run: func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}, tools.DispatchConfig{Timeout: 20 * time.Millisecond})
	_, err = d2.Invoke(context.Background(), tools.ToolRequest{Name: "timeout", Input: req.Input})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestDispatcherRegistrySnapshotAndBadSchema(t *testing.T) {
	registry := tools.NewRegistry()
	var nilTool *dispatchTestTool
	require.Error(t, registry.Register(nilTool))
	require.Error(t, registry.Register(&dispatchTestTool{name: " "}))
	tool := &dispatchTestTool{name: "original", schema: `{"type":"object"}`, run: func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true}, nil
	}}
	require.NoError(t, registry.Register(tool))
	d, err := tools.NewDispatcher(registry, tools.DispatchConfig{})
	require.NoError(t, err)
	registry.Clear()
	require.Len(t, d.Tools(), 1)
	_, err = d.Invoke(context.Background(), tools.ToolRequest{Name: "missing", Input: validToolInput()})
	require.ErrorContains(t, err, "unknown tool")
	require.Zero(t, tool.calls.Load())
	for _, schema := range []string{`null`, `{`, `{"type":"string"}`, `{"type":"object","$ref":"https://invalid.example/schema"}`} {
		r := tools.NewRegistry()
		require.NoError(t, r.Register(&dispatchTestTool{name: "bad", schema: schema}))
		_, err := tools.NewDispatcher(r, tools.DispatchConfig{})
		require.Error(t, err)
	}
}
