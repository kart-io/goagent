package plugin

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// InstallOption 调整 Install 的行为。
type InstallOption func(*installConfig)

type installConfig struct {
	only     []string
	onlySet  bool
	dispatch *tools.DispatchConfig
}

// Only 只安装列出的插件；顺序仍按 Host 的依赖顺序。
func Only(names ...string) InstallOption {
	return func(c *installConfig) {
		c.only = append(c.only, names...)
		c.onlySet = true
	}
}

// Dispatch 把所选插件的全部工具注册进一个 Dispatcher 再交给 Builder；持久运行要求工具经 Dispatcher 记入日志。
// 宿主自己的工具仍由宿主自行包装。
func Dispatch(cfg tools.DispatchConfig) InstallOption {
	return func(c *installConfig) { c.dispatch = &cfg }
}

// Install 把 Host 中插件的贡献装进 b。先调用所选插件的 Contribute 并完成全部校验，
// 全部通过后才写入 b，任一失败时 b 不变。同一个 Builder 只应 Install 一次：重复安装时
// 重名工具会在 Build 时报错，钩子则会重复执行。
// Install 会覆盖此前 WithRequestError 设置的恢复次数上限（所选插件全为 0 时为 Builder 默认值 3）；
// 宿主要自定义上限，须在 Install 之后调用 WithRequestError。
// 多个 goroutine 可同时 Install 同一 Host，插件的 Contribute 须并发安全；Install 不得与 Host.Stop 并发。
func Install[C any, S core.State](ctx context.Context, b *builder.AgentBuilder[C, S], h *Host, opts ...InstallOption) error {
	if b == nil || h == nil {
		return errors.New("plugin.Install 需要非 nil 的 Builder 与 Host")
	}
	var cfg installConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	selected, err := h.selectStarted(cfg)
	if err != nil {
		return err
	}
	contributions := make([]Contribution, len(selected))
	for i, p := range selected {
		c, err := p.Contribute(ctx, h.envs[p.Name()])
		if err != nil {
			return fmt.Errorf("插件 %q 贡献失败：%w", p.Name(), err)
		}
		contributions[i] = c
	}
	if err := validate(selected, contributions); err != nil {
		return err
	}
	toolset, err := collectTools(contributions, cfg.dispatch)
	if err != nil {
		return err
	}
	apply(b, selected, contributions, toolset)
	return nil
}

func (h *Host) selectStarted(cfg installConfig) ([]Plugin, error) {
	h.mu.Lock()
	state := h.state
	h.mu.Unlock()
	if state != hostStarted {
		return nil, errors.New("Host 未启动或已停止，Install 前须先调用 Start")
	}
	if !cfg.onlySet {
		return append([]Plugin(nil), h.order...), nil
	}
	if len(cfg.only) == 0 {
		return nil, errors.New("Only 至少需要一个插件名")
	}
	wanted := map[string]bool{}
	var unknown []error
	for _, name := range cfg.only {
		if _, ok := h.envs[name]; !ok {
			unknown = append(unknown, fmt.Errorf("插件 %q 未注册", name))
		}
		wanted[name] = true
	}
	if len(unknown) > 0 {
		return nil, errors.Join(unknown...)
	}
	var out []Plugin
	for _, p := range h.order {
		if wanted[p.Name()] {
			out = append(out, p)
		}
	}
	return out, nil
}

func validate(selected []Plugin, cs []Contribution) error {
	var errs []error
	owners := map[string]string{}
	claim := func(kind, key, plugin string) {
		k := kind + "\x00" + key
		if prev, ok := owners[k]; ok {
			errs = append(errs, fmt.Errorf("插件 %q 与 %q 提供了同名%s %q", prev, plugin, kind, key))
			return
		}
		owners[k] = plugin
	}
	for i, p := range selected {
		name, c := p.Name(), cs[i]
		bad := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf("插件 %q 贡献无效：%s", name, fmt.Sprintf(format, args...)))
		}
		for j, t := range c.Tools {
			if t == nil {
				bad("第 %d 个工具为 nil", j)
				continue
			}
			claim("工具", t.Name(), name)
		}
		for j, s := range c.PromptSections {
			if s.Name == "" {
				bad("提示词片段（第 %d 个）缺少 Name", j)
				continue
			}
			if s.Text == nil {
				bad("提示词片段 %q（第 %d 个）的 Text 为 nil", s.Name, j)
			}
			claim("提示词片段", s.Name, name)
		}
		for j, s := range c.PromptContexts {
			if s.Name == "" {
				bad("提示词上下文（第 %d 个）缺少 Name", j)
				continue
			}
			if s.Text == nil {
				bad("提示词上下文 %q（第 %d 个）的 Text 为 nil", s.Name, j)
			}
			claim("提示词上下文", s.Name, name)
		}
		for j, s := range c.PreSteps {
			if strings.TrimSpace(s.Identity) == "" {
				bad("第 %d 个 PreStep 缺少 Identity", j)
			}
			if s.Hook == nil {
				bad("第 %d 个 PreStep 的 Hook 为 nil", j)
			}
		}
		for j, m := range c.Middleware {
			if m == nil {
				bad("第 %d 个中间件为 nil", j)
				continue
			}
			claim("中间件", m.Name(), name)
		}
		nilHooks(bad, "RequestHook", c.RequestHooks)
		nilHooks(bad, "RequestError", c.RequestErrors)
		nilHooks(bad, "TurnStopping", c.TurnStopping)
		for j, cb := range c.Callbacks {
			if cb == nil {
				bad("第 %d 个回调为 nil", j)
			}
		}
		if c.MaxRecoveries < 0 {
			bad("MaxRecoveries 不能为负数：%d", c.MaxRecoveries)
		}
	}
	return errors.Join(errs...)
}

func nilHooks[F any](bad func(string, ...any), kind string, hooks []F) {
	for j, h := range hooks {
		if isNilFunc(h) {
			bad("第 %d 个 %s 钩子为 nil", j, kind)
		}
	}
}

// isNilFunc 判断函数类型的值是否为 nil；钩子都是函数类型，装进 any 后不能直接与 nil 比较。
func isNilFunc[F any](f F) bool {
	v := reflect.ValueOf(f)
	return v.Kind() == reflect.Func && v.IsNil()
}

func collectTools(cs []Contribution, dispatch *tools.DispatchConfig) ([]interfaces.Tool, error) {
	var all []interfaces.Tool
	for _, c := range cs {
		all = append(all, c.Tools...)
	}
	if dispatch == nil || len(all) == 0 {
		return all, nil
	}
	reg := tools.NewRegistry()
	for _, t := range all {
		if err := reg.Register(t); err != nil {
			return nil, fmt.Errorf("注册插件工具 %q：%w", t.Name(), err)
		}
	}
	d, err := tools.NewDispatcher(reg, *dispatch)
	if err != nil {
		return nil, fmt.Errorf("创建插件工具的 Dispatcher：%w", err)
	}
	return d.Tools(), nil
}

func apply[C any, S core.State](b *builder.AgentBuilder[C, S], selected []Plugin, cs []Contribution, toolset []interfaces.Tool) {
	b.WithTools(toolset...)
	var recoveries []builder.RequestErrorHook
	maxRecoveries := 0
	for i, p := range selected {
		c := cs[i]
		prefix := "plugin/" + p.Name() + "@" + p.Version() + "/"
		b.WithPromptSections(c.PromptSections...)
		b.WithPromptContexts(c.PromptContexts...)
		for _, s := range c.PreSteps {
			b.WithIdentifiedPreStep(prefix+s.Identity, s.Hook)
		}
		b.WithRequestHook(c.RequestHooks...)
		b.WithTurnStopping(c.TurnStopping...)
		b.WithMiddleware(c.Middleware...)
		b.WithCallbacks(c.Callbacks...)
		recoveries = append(recoveries, c.RequestErrors...)
		maxRecoveries = max(maxRecoveries, c.MaxRecoveries)
	}
	if len(recoveries) > 0 {
		b.WithRequestError(maxRecoveries, recoveries...)
	}
}
