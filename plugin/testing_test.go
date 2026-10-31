package plugin

import (
	"context"
	"sync"
)

// testPlugin 只实现 Plugin。
type testPlugin struct {
	name, version string
	contribute    func(context.Context, *Env) (Contribution, error)
}

func (p *testPlugin) Name() string    { return p.name }
func (p *testPlugin) Version() string { return p.version }
func (p *testPlugin) Contribute(ctx context.Context, env *Env) (Contribution, error) {
	if p.contribute == nil {
		return Contribution{}, nil
	}
	return p.contribute(ctx, env)
}

// lifecyclePlugin 额外实现 Starter 与 Dependent。
type lifecyclePlugin struct {
	testPlugin
	requires []string
	start    func(context.Context, *Env) error
	stop     func(context.Context) error
}

func (p *lifecyclePlugin) Requires() []string { return p.requires }
func (p *lifecyclePlugin) Start(ctx context.Context, env *Env) error {
	if p.start == nil {
		return nil
	}
	return p.start(ctx, env)
}
func (p *lifecyclePlugin) Stop(ctx context.Context) error {
	if p.stop == nil {
		return nil
	}
	return p.stop(ctx)
}

// recorder 记录启停顺序。
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(e string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

// tracked 返回会把启停写入 rec 的插件；startErr/stopErr 非 nil 时对应操作失败。
func tracked(rec *recorder, name string, requires []string, startErr, stopErr error) *lifecyclePlugin {
	return &lifecyclePlugin{
		testPlugin: testPlugin{name: name, version: "1"},
		requires:   requires,
		start: func(context.Context, *Env) error {
			rec.add("start " + name)
			return startErr
		},
		stop: func(context.Context) error {
			rec.add("stop " + name)
			return stopErr
		},
	}
}
