package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

type hostState int

const (
	hostNew hostState = iota
	hostStarted
	hostStopped
)

// Host 管理一组插件：注册时校验名称、版本与依赖，Start 按依赖顺序启动 Starter，Stop 逆序停止。
// Host 不可重启；停止后应先停用由它安装的 Agent，因为插件贡献的工具可能持有已释放的资源。
type Host struct {
	order     []Plugin
	envs      map[string]*Env
	resources *Resources

	mu      sync.Mutex
	state   hostState
	started []Starter
	names   []string // 与 started 对应，用于错误信息
}

// NewHost 校验并按依赖拓扑排序插件，同层保持注册顺序；所有问题一次报告。
func NewHost(plugins ...Plugin) (*Host, error) {
	var errs []error
	byName := map[string]Plugin{}
	var names []string
	for i, p := range plugins {
		if p == nil {
			errs = append(errs, fmt.Errorf("第 %d 个插件为 nil", i))
			continue
		}
		name := p.Name()
		if err := validateName(name); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := validateVersion(p.Version()); err != nil {
			errs = append(errs, fmt.Errorf("插件 %q：%w", name, err))
		}
		if _, dup := byName[name]; dup {
			errs = append(errs, fmt.Errorf("插件名 %q 重复", name))
			continue
		}
		byName[name] = p
		names = append(names, name)
	}
	for _, name := range names {
		for _, req := range requires(byName[name]) {
			if _, ok := byName[req]; !ok {
				errs = append(errs, fmt.Errorf("插件 %q 依赖的 %q 未注册", name, req))
			}
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	order, err := topoSort(names, byName)
	if err != nil {
		return nil, err
	}
	h := &Host{order: order, envs: map[string]*Env{}, resources: newResources()}
	for _, p := range order {
		h.envs[p.Name()] = newEnv(p.Name(), h.resources)
	}
	return h, nil
}

func requires(p Plugin) []string {
	if d, ok := p.(Dependent); ok {
		return d.Requires()
	}
	return nil
}

// topoSort 每轮取注册顺序中第一个依赖已全部就位的插件，结果稳定；取不到时剩余插件构成环。
func topoSort(names []string, byName map[string]Plugin) ([]Plugin, error) {
	placed := map[string]bool{}
	order := make([]Plugin, 0, len(names))
	for len(order) < len(names) {
		progressed := false
		for _, name := range names {
			if placed[name] {
				continue
			}
			ready := true
			for _, req := range requires(byName[name]) {
				if !placed[req] {
					ready = false
					break
				}
			}
			if ready {
				placed[name] = true
				order = append(order, byName[name])
				progressed = true
				break
			}
		}
		if !progressed {
			var rest []string
			for _, name := range names {
				if !placed[name] {
					rest = append(rest, name)
				}
			}
			return nil, fmt.Errorf("插件存在循环依赖：%s", strings.Join(rest, "、"))
		}
	}
	return order, nil
}

// Plugins 返回拓扑顺序的 "名称@版本"。
func (h *Host) Plugins() []string {
	out := make([]string, len(h.order))
	for i, p := range h.order {
		out[i] = p.Name() + "@" + p.Version()
	}
	return out
}

// Start 按依赖顺序启动 Starter。任一失败或 ctx 结束时逆序停止已启动的插件，Host 进入已停止状态。
// ctx 在开始前已结束时不启动任何插件，Host 仍可再次 Start。
func (h *Host) Start(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state != hostNew {
		return errors.New("Host 只能启动一次")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, p := range h.order {
		s, ok := p.(Starter)
		if !ok {
			continue
		}
		err := ctx.Err()
		if err == nil {
			env := h.envs[p.Name()]
			env.starting.Store(true)
			err = s.Start(ctx, env)
			env.starting.Store(false)
			if err != nil {
				err = fmt.Errorf("插件 %q 启动失败：%w", p.Name(), err)
			}
		}
		if err != nil {
			h.state = hostStopped
			// 回滚不受启动 ctx 取消的影响，否则已打开的资源无法释放。
			return errors.Join(err, h.stopStarted(context.WithoutCancel(ctx)))
		}
		h.started = append(h.started, s)
		h.names = append(h.names, p.Name())
	}
	if err := ctx.Err(); err != nil {
		h.state = hostStopped
		return errors.Join(err, h.stopStarted(context.WithoutCancel(ctx)))
	}
	h.state = hostStarted
	return nil
}

// Stop 逆序停止已启动的插件，单个失败不中断其余，错误合并返回；重复调用返回 nil。
func (h *Host) Stop(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state == hostStopped {
		return nil
	}
	h.state = hostStopped
	return h.stopStarted(ctx)
}

func (h *Host) stopStarted(ctx context.Context) error {
	var errs []error
	for i := len(h.started) - 1; i >= 0; i-- {
		if err := h.started[i].Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("插件 %q 停止失败：%w", h.names[i], err))
		}
	}
	h.started, h.names = nil, nil
	h.resources.clear()
	return errors.Join(errs...)
}
