package adapters

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
)

type observed struct {
	mu     sync.Mutex
	tasks  []string
	events []builder.AgentEvent
}

func (o *observed) observe(_ context.Context, task mg.Task, ev builder.AgentEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.tasks = append(o.tasks, task.ID)
	o.events = append(o.events, ev)
}

func (o *observed) deltas() (out []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, ev := range o.events {
		if ev.Type == "text" {
			out = append(out, ev.Delta)
		}
	}
	return out
}

func (o *observed) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.events)
}

func observeFactory(observe func(context.Context, mg.Task, builder.AgentEvent)) NativeFactory {
	return NativeFactory{
		Check: func(context.Context, mg.Version) error { return nil },
		BuildAgent: func(context.Context, mg.Version, mg.Resources, tools.Approver) (*builder.SimpleAgentBuilder, func(context.Context) error, error) {
			model := mocks.NewMockStreamingLLMClient()
			model.SetStreamChunks("a", "b")
			return builder.NewSimpleBuilder(model), nil, nil
		},
		Observe: observe,
	}
}

func TestNativeObserveReceivesDeltasInOrder(t *testing.T) {
	ctx, r := adapterResources(t)
	var o observed
	binding, err := observeFactory(o.observe).Build(ctx, mg.Version{AgentID: "agent", Version: "v1", ConfigHash: "config"}, r)
	require.NoError(t, err)
	result, err := binding.Runtime.Run(ctx, mg.RunSpec{Task: r.Task})
	require.NoError(t, err)
	require.Equal(t, "completed", result.Outcome)
	require.JSONEq(t, `"ab"`, string(result.Output))
	require.Equal(t, []string{"a", "b"}, o.deltas())
	for _, id := range o.tasks {
		require.Equal(t, r.Task.ID, id)
	}
}

func TestNativeInspectDoesNotObserve(t *testing.T) {
	ctx, r := adapterResources(t)
	var o observed
	f := observeFactory(o.observe)
	v := mg.Version{AgentID: "agent", Version: "v1", ConfigHash: "config"}
	binding, err := f.Build(ctx, v, r)
	require.NoError(t, err)
	spec := mg.RunSpec{Task: r.Task}
	_, err = binding.Runtime.Run(ctx, spec)
	require.NoError(t, err)
	before := o.count()
	require.NotZero(t, before)
	binding, err = f.Build(ctx, v, r)
	require.NoError(t, err)
	evidence, err := binding.Runtime.Inspect(ctx, spec)
	require.NoError(t, err)
	require.Equal(t, "completed", evidence.State)
	require.Equal(t, before, o.count())
}

func TestNativeObservePanicDoesNotFailTask(t *testing.T) {
	ctx, r := adapterResources(t)
	binding, err := observeFactory(func(context.Context, mg.Task, builder.AgentEvent) { panic("boom") }).Build(ctx, mg.Version{AgentID: "agent", Version: "v1", ConfigHash: "config"}, r)
	require.NoError(t, err)
	result, err := binding.Runtime.Run(ctx, mg.RunSpec{Task: r.Task})
	require.NoError(t, err)
	require.Equal(t, "completed", result.Outcome)
}
