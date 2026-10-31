package plugin

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewHostValidates(t *testing.T) {
	for _, tc := range []struct {
		plugins []Plugin
		want    string
	}{
		{[]Plugin{nil}, "为 nil"},
		{[]Plugin{&testPlugin{name: "", version: "1"}}, "插件名为空"},
		{[]Plugin{&testPlugin{name: "a@b", version: "1"}}, "不能包含 @"},
		{[]Plugin{&testPlugin{name: "a", version: ""}}, "插件版本为空"},
		{[]Plugin{&testPlugin{name: "a", version: "1"}, &testPlugin{name: "a", version: "2"}}, "重复"},
		{[]Plugin{&lifecyclePlugin{testPlugin: testPlugin{name: "a", version: "1"}, requires: []string{"x"}}}, `依赖的 "x" 未注册`},
		{[]Plugin{
			&lifecyclePlugin{testPlugin: testPlugin{name: "a", version: "1"}, requires: []string{"b"}},
			&lifecyclePlugin{testPlugin: testPlugin{name: "b", version: "1"}, requires: []string{"a"}},
		}, "循环依赖"},
		{[]Plugin{&lifecyclePlugin{testPlugin: testPlugin{name: "a", version: "1"}, requires: []string{"a"}}}, "循环依赖"},
	} {
		_, err := NewHost(tc.plugins...)
		require.ErrorContains(t, err, tc.want)
	}

	// 多个问题一次报告。
	_, err := NewHost(&testPlugin{name: "", version: "1"}, &testPlugin{name: "b", version: ""})
	require.ErrorContains(t, err, "插件名为空")
	require.ErrorContains(t, err, "插件版本为空")
}

func TestNewHostOrdersByDependency(t *testing.T) {
	h, err := NewHost(
		&lifecyclePlugin{testPlugin: testPlugin{name: "c", version: "1"}, requires: []string{"a"}},
		&testPlugin{name: "a", version: "1"},
		&lifecyclePlugin{testPlugin: testPlugin{name: "b", version: "2"}, requires: []string{"c"}},
		&testPlugin{name: "d", version: "1"},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"a@1", "c@1", "b@2", "d@1"}, h.Plugins())
}

func TestHostStartStopOrder(t *testing.T) {
	rec := &recorder{}
	h, err := NewHost(tracked(rec, "b", []string{"a"}, nil, nil), tracked(rec, "a", nil, nil, nil), &testPlugin{name: "plain", version: "1"})
	require.NoError(t, err)
	require.NoError(t, h.Start(context.Background()))
	require.ErrorContains(t, h.Start(context.Background()), "只能启动一次")
	require.NoError(t, h.Stop(context.Background()))
	require.NoError(t, h.Stop(context.Background()), "重复 Stop 返回 nil")
	require.Equal(t, []string{"start a", "start b", "stop b", "stop a"}, rec.list())
}

func TestHostStartRollback(t *testing.T) {
	rec := &recorder{}
	boom, stopFail := errors.New("连接失败"), errors.New("关闭失败")
	h, err := NewHost(tracked(rec, "a", nil, nil, stopFail), tracked(rec, "b", []string{"a"}, boom, nil), tracked(rec, "c", []string{"b"}, nil, nil))
	require.NoError(t, err)
	err = h.Start(context.Background())
	require.ErrorIs(t, err, boom)
	require.ErrorIs(t, err, stopFail)
	require.ErrorContains(t, err, `插件 "b" 启动失败`)
	require.Equal(t, []string{"start a", "start b", "stop a"}, rec.list(), "失败的插件不调用 Stop，未启动的插件不受影响")
	require.ErrorContains(t, h.Start(context.Background()), "只能启动一次")
	require.NoError(t, h.Stop(context.Background()))
}

func TestHostStartCanceled(t *testing.T) {
	rec := &recorder{}
	h, err := NewHost(tracked(rec, "a", nil, nil, nil))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, h.Start(ctx), context.Canceled)
	require.Empty(t, rec.list())

	// 第一个插件启动后 ctx 被取消：不再启动后续插件，并回滚已启动的。
	rec = &recorder{}
	ctx, cancel = context.WithCancel(context.Background())
	first := tracked(rec, "a", nil, nil, nil)
	first.start = func(context.Context, *Env) error { rec.add("start a"); cancel(); return nil }
	h, err = NewHost(first, tracked(rec, "b", []string{"a"}, nil, nil))
	require.NoError(t, err)
	require.ErrorIs(t, h.Start(ctx), context.Canceled)
	require.Equal(t, []string{"start a", "stop a"}, rec.list())
}

func TestHostStopJoinsErrorsAndClearsResources(t *testing.T) {
	e1, e2 := errors.New("一"), errors.New("二")
	rec := &recorder{}
	a := tracked(rec, "a", nil, nil, e1)
	a.start = func(_ context.Context, env *Env) error { return env.Provide("a.conn", "连接") }
	h, err := NewHost(a, tracked(rec, "b", nil, nil, e2))
	require.NoError(t, err)
	require.NoError(t, h.Start(context.Background()))
	_, ok := h.envs["a"].Resources().Resolve("a.conn")
	require.True(t, ok)
	err = h.Stop(context.Background())
	require.ErrorIs(t, err, e1)
	require.ErrorIs(t, err, e2)
	_, ok = h.envs["a"].Resources().Resolve("a.conn")
	require.False(t, ok)
}

func TestProvideOnlyDuringOwnStart(t *testing.T) {
	var leaked *Env
	a := &lifecyclePlugin{testPlugin: testPlugin{name: "a", version: "1"}, start: func(_ context.Context, env *Env) error {
		leaked = env
		return nil
	}}
	h, err := NewHost(a)
	require.NoError(t, err)
	require.NoError(t, h.Start(context.Background()))
	require.ErrorContains(t, leaked.Provide("a.late", 1), "只能在 Start 中")
	require.NoError(t, h.Stop(context.Background()))
}
