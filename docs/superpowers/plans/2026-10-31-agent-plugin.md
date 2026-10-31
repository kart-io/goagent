# Agent 能力包（plugin）实施计划

> **供执行代理：** 必须配合 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 按任务实施。步骤用复选框（`- [ ]`）跟踪。

**目标：** 新增 `goagent/plugin` 包，让插件把工具、提示词、循环钩子、中间件、回调与进程级资源打包装进任意 Agent；删除 `core` 中不与 Agent 集成的旧插件注册中心；在 goagent-examples 中提供四类可运行案例。

**架构：** 插件返回纯数据的 `Contribution`；`Host` 校验依赖、按拓扑顺序启停 `Starter` 并共享 `Resources`；泛型 `Install` 先调用全部所选插件的 `Contribute` 并完成校验，再一次性写入 `builder.AgentBuilder[C, S]`。`builder` 不导入 `plugin`。

**技术栈：** Go 1.25、testify、现有 `builder`/`tools`/`core/durable`，无新依赖。

**设计：** `docs/superpowers/specs/2026-10-31-agent-plugin-design.md`

## 全局约束

- 注释、错误信息、文档、提交信息一律简体中文；标识符沿用英文。
- 依赖方向 `plugin → builder、core、interfaces、tools`；`builder`、`core`、`interfaces`、`llm`、`tools` 不得导入 `plugin`（由 apicheck 强制）。
- 插件名非空、无首尾空白、不含 `/` 与 `@`；版本非空、无首尾空白、不含 `/`。
- 插件 PreStep 身份固定为 `plugin/<名称>@<版本>/<Identity>`。
- 破坏式更改，不保留 `core.Plugin` 等旧接口；CHANGELOG 写迁移说明；提交类型 `feat!`。
- 每个任务结束前运行该包 `go test -race`；全部完成后运行全仓测试、golangci-lint v2.6.2 与 `go run ./scripts/apicheck`。
- 不推送、不合并，等用户指示。不触碰未跟踪的 `.context/` 与 `provider-contracts.jsonl`。

## 文件结构

goagent（分支 `feat/agent-plugin`）：

| 文件 | 职责 |
|---|---|
| `plugin/plugin.go` | `Plugin`、`Starter`、`Dependent`、`PreStep`、`Contribution`，名称与版本校验 |
| `plugin/resources.go` | `Env`、`Resources`、`Lookup` |
| `plugin/host.go` | `Host`：注册校验、拓扑排序、启动回滚、停止、`Plugins` |
| `plugin/install.go` | `InstallOption`、`Only`、`Dispatch`、`Install`、贡献校验与写入 |
| `plugin/testing_test.go` | 测试用插件与脚本模型 |
| `plugin/host_test.go`、`plugin/resources_test.go`、`plugin/install_test.go`、`plugin/agent_test.go` | 单元与集成测试 |
| `core/plugin_registry_enhanced.go`、`core/plugin_registry_enhanced_test.go` | 删除 |
| `scripts/apicheck/main.go`、`scripts/apicheck/main_test.go` | 新增 `plugin` 导入规则 |
| `docs/guides/PLUGIN_SYSTEM_GUIDE.md`、`docs/README.md`、`docs/architecture/ARCHITECTURE.md`、`CHANGELOG.md`、`plugin/README.md` | 文档 |

goagent-examples（分支 `feat/plugin-cases`）：

| 文件 | 职责 |
|---|---|
| `orchestration/plugin/internal/shop/shop.go` | 案例共用插件：订单、物流、退款政策、手机号脱敏 |
| `orchestration/plugin/basic/{main.go,main_test.go,README.md}` | 基础与组合、工具冲突 |
| `orchestration/plugin/hooks/{main.go,main_test.go,README.md}` | 钩子类插件 |
| `orchestration/plugin/resources/{main.go,main_test.go,README.md}` | 资源、依赖与启动回滚 |
| `orchestration/plugin/support-desk/{main.go,main_test.go,README.md}` | 持久运行端到端 |
| `README.md` | 目录表增加一行 |

---

### Task 1：插件类型与共享资源

**Files:**
- Create: `plugin/plugin.go`
- Create: `plugin/resources.go`
- Test: `plugin/resources_test.go`

**Interfaces:**
- Produces: `Plugin`、`Starter`、`Dependent`、`PreStep{Identity string; Hook builder.PreStepHook}`、`Contribution`（字段见下）、`validateName(string) error`、`validateVersion(string) error`、`Env{name string; resources *Resources; starting atomic.Bool}` 及 `Plugin()`/`Resources()`/`Provide(key string, value any) error`、`Resources` 及 `Resolve(key) (any, bool)`/`put`/`clear`、`newResources() *Resources`、`Lookup[T any](r *Resources, key string) (T, error)`、`newEnv(name string, r *Resources) *Env`。

- [ ] **Step 1：写失败的测试** `plugin/resources_test.go`

```go
package plugin

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateNameAndVersion(t *testing.T) {
	for _, bad := range []string{"", " a", "a ", "a/b", "a@b"} {
		require.Error(t, validateName(bad), bad)
	}
	require.NoError(t, validateName("orders"))
	for _, bad := range []string{"", " 1", "1/2"} {
		require.Error(t, validateVersion(bad), bad)
	}
	require.NoError(t, validateVersion("1.2.0"))
}

func TestEnvProvide(t *testing.T) {
	r := newResources()
	env := newEnv("inventory", r)
	require.ErrorContains(t, env.Provide("inventory.db", 1), "只能在 Start 中")

	env.starting.Store(true)
	require.NoError(t, env.Provide("inventory.db", 1))
	require.ErrorContains(t, env.Provide("inventory.db", 2), "已存在")
	require.ErrorContains(t, env.Provide("pricing.db", 1), "须以")
	require.ErrorContains(t, env.Provide("inventory.", 1), "须以")
	require.ErrorContains(t, env.Provide("inventory.nil", nil), "nil")
	env.starting.Store(false)

	n, err := Lookup[int](r, "inventory.db")
	require.NoError(t, err)
	require.Equal(t, 1, n)
	_, err = Lookup[string](r, "inventory.db")
	require.ErrorContains(t, err, "类型")
	_, err = Lookup[int](r, "inventory.none")
	require.ErrorContains(t, err, "不存在")

	r.clear()
	_, ok := r.Resolve("inventory.db")
	require.False(t, ok)
}

func TestResourcesConcurrent(t *testing.T) {
	r := newResources()
	env := newEnv("a", r)
	env.starting.Store(true)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) { defer wg.Done(); _ = env.Provide("a.k"+string(rune('A'+i%26)), i) }(i)
		go func() { defer wg.Done(); r.Resolve("a.kA") }()
	}
	wg.Wait()
}
```

- [ ] **Step 2：运行确认失败**

Run: `go test ./plugin/ -run 'TestValidate|TestEnv|TestResources' -race`
Expected: FAIL，`undefined: validateName` 等编译错误。

- [ ] **Step 3：实现** `plugin/plugin.go`

```go
// Package plugin 提供 Agent 能力包：在 Go 中静态组合工具、提示词片段、循环钩子、中间件、回调与进程级资源，
// 由 Host 管理启停，经 Install 装进 builder.AgentBuilder。
package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
)

// Plugin 是一个 Agent 能力包。
type Plugin interface {
	// Name 在一个 Host 内唯一：非空、无首尾空白、不含 "/" 与 "@"。
	Name() string
	// Version 进入持久运行中 PreStep 的身份：非空、无首尾空白、不含 "/"。
	Version() string
	// Contribute 在每次 Install 时调用，返回要装进本次构建的 Agent 的能力。
	Contribute(ctx context.Context, env *Env) (Contribution, error)
}

// Starter 由持有进程级资源的插件实现：Host.Start 时按依赖顺序调用 Start，Host.Stop 时逆序调用 Stop。
// Start 失败的插件不会再被调用 Stop，须自行释放已打开的部分。
type Starter interface {
	Start(ctx context.Context, env *Env) error
	Stop(ctx context.Context) error
}

// Dependent 声明必须先于本插件启动的插件名。
type Dependent interface {
	Requires() []string
}

// PreStep 是带身份的 PreStep 钩子；持久运行中以 "plugin/<名称>@<版本>/<Identity>" 进入日志，
// 插件升级版本或改变 Identity 后，恢复旧运行以 intent changed 失败。
type PreStep struct {
	Identity string
	Hook     builder.PreStepHook
}

// Contribution 是插件向 Agent 贡献的能力，各字段对应 Builder 的同名扩展点。
type Contribution struct {
	Tools          []interfaces.Tool
	PromptSections []builder.PromptSection
	PromptContexts []builder.PromptContext
	PreSteps       []PreStep
	RequestHooks   []builder.RequestHook
	RequestErrors  []builder.RequestErrorHook
	// MaxRecoveries 只对 RequestErrors 生效；所选插件取最大值，全为 0 时用 Builder 的默认值。
	MaxRecoveries int
	TurnStopping  []builder.TurnStoppingHook
	Middleware    []middleware.Middleware
	Callbacks     []core.Callback
}

func validateName(name string) error {
	if err := validateIdent("插件名", name); err != nil {
		return err
	}
	if strings.Contains(name, "@") {
		return fmt.Errorf("插件名 %q 不能包含 @", name)
	}
	return nil
}

func validateVersion(version string) error { return validateIdent("插件版本", version) }

func validateIdent(kind, v string) error {
	switch {
	case v == "":
		return fmt.Errorf("%s为空", kind)
	case strings.TrimSpace(v) != v:
		return fmt.Errorf("%s %q 含首尾空白", kind, v)
	case strings.Contains(v, "/"):
		return fmt.Errorf("%s %q 不能包含 /", kind, v)
	}
	return nil
}
```

- [ ] **Step 4：实现** `plugin/resources.go`

```go
package plugin

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
)

// Env 是插件可见的宿主环境，每个插件一个。
type Env struct {
	name      string
	resources *Resources
	starting  atomic.Bool
}

func newEnv(name string, r *Resources) *Env { return &Env{name: name, resources: r} }

// Plugin 返回当前插件名。
func (e *Env) Plugin() string { return e.name }

// Resources 返回 Host 内所有插件共享的资源。
func (e *Env) Resources() *Resources { return e.resources }

// Provide 发布本插件的共享资源，只能在本插件 Start 执行期间调用；key 须以 "<插件名>." 开头，避免冒用其他插件的命名空间。
func (e *Env) Provide(key string, value any) error {
	if !e.starting.Load() {
		return fmt.Errorf("插件 %q 只能在 Start 中提供资源 %q", e.name, key)
	}
	prefix := e.name + "."
	if !strings.HasPrefix(key, prefix) || len(key) == len(prefix) {
		return fmt.Errorf("插件 %q 的资源名须以 %q 开头：%q", e.name, prefix, key)
	}
	if value == nil {
		return fmt.Errorf("资源 %q 为 nil", key)
	}
	return e.resources.put(key, value)
}

// Resources 保存插件在 Start 中发布的实例，并发安全；Host 停止后清空。
type Resources struct {
	mu     sync.RWMutex
	values map[string]any
}

func newResources() *Resources { return &Resources{values: map[string]any{}} }

func (r *Resources) put(key string, value any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.values[key]; ok {
		return fmt.Errorf("资源 %q 已存在", key)
	}
	r.values[key] = value
	return nil
}

// Resolve 返回资源及其是否存在。
func (r *Resources) Resolve(key string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.values[key]
	return v, ok
}

func (r *Resources) clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	clear(r.values)
}

// Lookup 按类型取出资源；不存在或类型不符时报错。
func Lookup[T any](r *Resources, key string) (T, error) {
	var zero T
	v, ok := r.Resolve(key)
	if !ok {
		return zero, fmt.Errorf("资源 %q 不存在", key)
	}
	typed, ok := v.(T)
	if !ok {
		return zero, fmt.Errorf("资源 %q 的类型是 %T，不是 %s", key, v, reflect.TypeFor[T]())
	}
	return typed, nil
}
```

- [ ] **Step 5：运行确认通过**

Run: `go test ./plugin/ -run 'TestValidate|TestEnv|TestResources' -race`
Expected: PASS

- [ ] **Step 6：提交**

```bash
git add plugin/plugin.go plugin/resources.go plugin/resources_test.go
git commit -m "feat(plugin): 新增插件类型与共享资源"
```

---

### Task 2：Host 生命周期与依赖

**Files:**
- Create: `plugin/host.go`
- Create: `plugin/testing_test.go`
- Test: `plugin/host_test.go`

**Interfaces:**
- Consumes: Task 1 的全部类型。
- Produces: `NewHost(plugins ...Plugin) (*Host, error)`、`(*Host).Start(ctx) error`、`(*Host).Stop(ctx) error`、`(*Host).Plugins() []string`、内部 `(*Host).selectStarted(cfg installConfig) ([]Plugin, error)`（Task 3 定义 `installConfig` 后补实现，本任务先不写）、字段 `envs map[string]*Env`。测试辅助：`testPlugin`、`lifecyclePlugin`、`recorder`。

- [ ] **Step 1：写测试辅助** `plugin/testing_test.go`

```go
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
```

- [ ] **Step 2：写失败的测试** `plugin/host_test.go`

```go
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
```

- [ ] **Step 3：运行确认失败**

Run: `go test ./plugin/ -run 'TestNewHost|TestHost|TestProvideOnly' -race`
Expected: FAIL，`undefined: NewHost`。

- [ ] **Step 4：实现** `plugin/host.go`

```go
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
```

注意：`TestHostStartCanceled` 第二段中 a 启动后取消，循环里 b 的 `ctx.Err()` 检查触发回滚；若 b 不是 Starter，则由循环后的检查回滚。

- [ ] **Step 5：运行确认通过**

Run: `go test ./plugin/ -race`
Expected: PASS

- [ ] **Step 6：提交**

```bash
git add plugin/host.go plugin/host_test.go plugin/testing_test.go
git commit -m "feat(plugin): Host 按依赖顺序启停插件并在失败时回滚"
```

---

### Task 3：Install 与冲突校验

**Files:**
- Create: `plugin/install.go`
- Modify: `plugin/testing_test.go`（追加脚本模型）
- Test: `plugin/install_test.go`

**Interfaces:**
- Consumes: `Host.order`、`Host.envs`、`Host.state`、`Contribution`、`PreStep`。
- Produces: `type InstallOption func(*installConfig)`、`Only(names ...string) InstallOption`、`Dispatch(cfg tools.DispatchConfig) InstallOption`、`Install[C any, S core.State](ctx context.Context, b *builder.AgentBuilder[C, S], h *Host, opts ...InstallOption) error`。测试辅助：`scriptModel`、`turn`、`answer(text) turn`、`calls(names ...string) turn`、`failing(err) turn`、`newModel(turns ...turn) *scriptModel`、`(*scriptModel).request(i)`、`(*scriptModel).count()`、`toolNamed(name string, runs *atomic.Int32) interfaces.Tool`。

- [ ] **Step 1：追加测试辅助到** `plugin/testing_test.go`

在 import 中加入 `"errors"`、`"slices"`、`"sync/atomic"`、`"github.com/kart-io/goagent/interfaces"`、`"github.com/kart-io/goagent/llm"`、`"github.com/kart-io/goagent/llm/constants"`、`"github.com/kart-io/goagent/tools"`，并追加：

```go
// turn 是一次模型请求的结果：fail 非 nil 时请求失败。
type turn struct {
	text  string
	calls []llm.ToolCall
	fail  error
}

func answer(text string) turn { return turn{text: text} }

func calls(names ...string) turn {
	var cs []llm.ToolCall
	for i, name := range names {
		c := llm.ToolCall{ID: "c" + string(rune('1'+i)), Type: "function"}
		c.Function.Name = name
		c.Function.Arguments = "{}"
		cs = append(cs, c)
	}
	return turn{calls: cs}
}

func failing(err error) turn { return turn{fail: err} }

// scriptModel 按顺序给出回合并记录每次请求。
type scriptModel struct {
	mu       sync.Mutex
	turns    []turn
	requests []*llm.CompletionRequest
}

func newModel(turns ...turn) *scriptModel { return &scriptModel{turns: turns} }

func (m *scriptModel) Provider() constants.Provider { return constants.ProviderCustom }
func (m *scriptModel) IsAvailable() bool            { return true }
func (m *scriptModel) Complete(context.Context, *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return nil, errors.New("测试模型只支持流式")
}
func (m *scriptModel) Chat(context.Context, []llm.Message) (*llm.CompletionResponse, error) {
	return nil, errors.New("测试模型只支持流式")
}
func (m *scriptModel) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	return m.CompleteToolsStream(ctx, req, nil)
}
func (m *scriptModel) ChatStream(ctx context.Context, ms []llm.Message) (<-chan *llm.StreamChunk, error) {
	return m.CompleteToolsStream(ctx, &llm.CompletionRequest{Messages: ms}, nil)
}
func (m *scriptModel) CompleteToolsStream(_ context.Context, req *llm.CompletionRequest, _ []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := *req
	copied.Messages = slices.Clone(req.Messages)
	m.requests = append(m.requests, &copied)
	i := len(m.requests) - 1
	if i >= len(m.turns) {
		return nil, errors.New("发生了多余的模型请求")
	}
	t := m.turns[i]
	if t.fail != nil {
		return nil, t.fail
	}
	chunk := &llm.StreamChunk{Content: t.text, Done: true, ToolCalls: t.calls, FinishReason: "stop", Usage: &llm.Usage{TotalTokens: 2}}
	if len(t.calls) > 0 {
		chunk.FinishReason = "tool_calls"
	}
	ch := make(chan *llm.StreamChunk, 1)
	ch <- chunk
	close(ch)
	return ch, nil
}

func (m *scriptModel) request(i int) *llm.CompletionRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requests[i]
}

func (m *scriptModel) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

// toolNamed 返回成功的工具，每次执行把 runs 加一。
func toolNamed(name string, runs *atomic.Int32) interfaces.Tool {
	return tools.NewBaseTool(name, "测试工具 "+name, `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		if runs != nil {
			runs.Add(1)
		}
		return &interfaces.ToolOutput{Success: true, Result: name + " 的结果"}, nil
	})
}

// startedHost 创建并启动 Host，测试结束时停止。
func startedHost(t interface {
	Helper()
	Cleanup(func())
	Fatalf(string, ...any)
}, plugins ...Plugin) *Host {
	t.Helper()
	h, err := NewHost(plugins...)
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	if err := h.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = h.Stop(context.Background()) })
	return h
}

func contributing(name string, c Contribution) *testPlugin {
	return &testPlugin{name: name, version: "1", contribute: func(context.Context, *Env) (Contribution, error) { return c, nil }}
}
```

如 `tools.NewBaseTool` 的参数与此不符，以 `tools/tool.go:21` 的签名为准（`builder/loop_hooks_test.go:119` 的 `plainTool` 是同样的用法）。

- [ ] **Step 2：写失败的测试** `plugin/install_test.go`

```go
package plugin

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

func section(name, text string) builder.PromptSection {
	return builder.PromptSection{Name: name, Text: func(builder.PromptScope) string { return text }}
}

func TestInstallRequiresStartedHost(t *testing.T) {
	h, err := NewHost(contributing("a", Contribution{}))
	require.NoError(t, err)
	b := builder.NewSimpleBuilder(newModel())
	require.ErrorContains(t, Install(context.Background(), b, h), "未启动")
	require.NoError(t, h.Start(context.Background()))
	require.NoError(t, h.Stop(context.Background()))
	require.ErrorContains(t, Install(context.Background(), b, h), "未启动")
	var nilBuilder *builder.SimpleAgentBuilder
	require.ErrorContains(t, Install(context.Background(), nilBuilder, h), "非 nil")
}

func TestInstallOnly(t *testing.T) {
	var otherCalled atomic.Bool
	other := &testPlugin{name: "other", version: "1", contribute: func(context.Context, *Env) (Contribution, error) {
		otherCalled.Store(true)
		return Contribution{}, nil
	}}
	var runs atomic.Int32
	orders := contributing("orders", Contribution{Tools: []interfaces.Tool{toolNamed("lookup_order", &runs)}, PromptSections: []builder.PromptSection{section("orders", "退款须提供订单号")}})
	h := startedHost(t, orders, other)

	require.ErrorContains(t, Install(context.Background(), builder.NewSimpleBuilder(newModel()), h, Only("missing")), `"missing"`)
	require.ErrorContains(t, Install(context.Background(), builder.NewSimpleBuilder(newModel()), h, Only()), "至少")

	model := newModel(calls("lookup_order"), answer("已查到"))
	b := builder.NewSimpleBuilder(model).WithMaxIterations(4)
	require.NoError(t, Install(context.Background(), b, h, Only("orders")))
	agent, err := b.Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "订单 A1 呢")
	require.NoError(t, err)
	require.Equal(t, "已查到", out.Result)
	require.EqualValues(t, 1, runs.Load())
	require.False(t, otherCalled.Load())
	require.Contains(t, model.request(0).Messages[0].Content, "退款须提供订单号")
}

func TestInstallRejectsConflictsWithoutChangingBuilder(t *testing.T) {
	noop := func(context.Context, *builder.Step) (builder.PreStepResult, error) { return builder.PreStepResult{}, nil }
	for _, tc := range []struct {
		name string
		bad  Contribution
		want string
	}{
		{"工具重名", Contribution{Tools: []interfaces.Tool{toolNamed("lookup", nil)}}, `插件 "good" 与 "bad" 提供了同名工具 "lookup"`},
		{"片段重名", Contribution{PromptSections: []builder.PromptSection{section("rules", "x")}}, "同名提示词片段"},
		{"上下文重名", Contribution{PromptContexts: []builder.PromptContext{{Name: "ctx", Text: func(builder.PromptScope) string { return "" }}}}, "同名提示词上下文"},
		{"中间件重名", Contribution{Middleware: []middleware.Middleware{middleware.NewBaseMiddleware("audit")}}, "同名中间件"},
		{"缺少身份", Contribution{PreSteps: []PreStep{{Hook: noop}}}, "缺少 Identity"},
		{"nil 钩子", Contribution{TurnStopping: []builder.TurnStoppingHook{nil}}, "为 nil"},
		{"nil 工具", Contribution{Tools: []interfaces.Tool{nil}}, "为 nil"},
		{"负的恢复次数", Contribution{MaxRecoveries: -1}, "MaxRecoveries"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			good := contributing("good", Contribution{
				Tools:          []interfaces.Tool{toolNamed("lookup", nil)},
				PromptSections: []builder.PromptSection{section("rules", "好插件的规则")},
				PromptContexts: []builder.PromptContext{{Name: "ctx", Text: func(builder.PromptScope) string { return "" }}},
				Middleware:     []middleware.Middleware{middleware.NewBaseMiddleware("audit")},
			})
			h := startedHost(t, good, contributing("bad", tc.bad))
			model := newModel(answer("答"))
			b := builder.NewSimpleBuilder(model).WithTools(toolNamed("host_tool", nil))
			require.ErrorContains(t, Install(context.Background(), b, h), tc.want)

			agent, err := b.Build()
			require.NoError(t, err)
			_, err = agent.AsRunnable().Invoke(context.Background(), "问")
			require.NoError(t, err)
			require.NotContains(t, model.request(0).Messages[0].Content, "好插件的规则", "校验失败时 Builder 不变")
		})
	}
}

func TestInstallContributeError(t *testing.T) {
	boom := errors.New("配置缺失")
	h := startedHost(t, &testPlugin{name: "a", version: "1", contribute: func(context.Context, *Env) (Contribution, error) { return Contribution{}, boom }})
	err := Install(context.Background(), builder.NewSimpleBuilder(newModel()), h)
	require.ErrorIs(t, err, boom)
	require.ErrorContains(t, err, `插件 "a" 贡献失败`)
}

func TestInstallMergesRequestErrors(t *testing.T) {
	down := &llm.ProviderError{StatusCode: 503, Err: errors.New("unavailable")}
	var observed, retried atomic.Int32
	observer := contributing("observer", Contribution{
		MaxRecoveries: 1,
		RequestErrors: []builder.RequestErrorHook{func(context.Context, *builder.RequestFailure) (builder.RequestErrorDecision, error) {
			observed.Add(1)
			return builder.RequestErrorDecision{}, nil
		}},
	})
	retry := contributing("retry", Contribution{
		Tools:         []interfaces.Tool{toolNamed("lookup", nil)},
		MaxRecoveries: 2,
		RequestErrors: []builder.RequestErrorHook{func(context.Context, *builder.RequestFailure) (builder.RequestErrorDecision, error) {
			retried.Add(1)
			return builder.RequestErrorDecision{Action: builder.RequestRetry}, nil
		}},
	})
	h := startedHost(t, observer, retry)

	b := builder.NewSimpleBuilder(newModel(failing(down), failing(down), answer("恢复")))
	require.NoError(t, Install(context.Background(), b, h))
	agent, err := b.Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "问")
	require.NoError(t, err)
	require.Equal(t, "恢复", out.Result)
	require.EqualValues(t, 2, observed.Load(), "先安装的插件的钩子先执行")
	require.EqualValues(t, 2, retried.Load())

	b = builder.NewSimpleBuilder(newModel(failing(down), failing(down), failing(down)))
	require.NoError(t, Install(context.Background(), b, h))
	agent, err = b.Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "问")
	require.ErrorIs(t, err, down, "恢复次数取各插件的最大值 2")
}

func TestInstallDispatchWrapsPluginTools(t *testing.T) {
	var runs atomic.Int32
	h := startedHost(t, contributing("orders", Contribution{Tools: []interfaces.Tool{toolNamed("lookup_order", &runs)}}))
	model := newModel(calls("lookup_order"), answer("好"))
	b := builder.NewSimpleBuilder(model).WithMaxIterations(4)
	require.NoError(t, Install(context.Background(), b, h, Dispatch(tools.DispatchConfig{MaxConcurrency: 1})))
	agent, err := b.Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "问")
	require.NoError(t, err)
	require.EqualValues(t, 1, runs.Load())
	var sawResult bool
	for _, m := range model.request(1).Messages {
		sawResult = sawResult || (m.Role == "tool" && strings.Contains(m.Content, "lookup_order 的结果"))
	}
	require.True(t, sawResult)
}
```

在 import 中补上 `"github.com/kart-io/goagent/interfaces"` 与 `"github.com/kart-io/goagent/tools"`。

- [ ] **Step 3：运行确认失败**

Run: `go test ./plugin/ -run TestInstall -race`
Expected: FAIL，`undefined: Install`。

- [ ] **Step 4：实现** `plugin/install.go`

```go
package plugin

import (
	"context"
	"errors"
	"fmt"
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
// 重名工具会在 Build 时报错，钩子则会重复执行。之后再调用 b.WithRequestError 会覆盖插件合并出的恢复次数上限。
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
			if s.Text == nil {
				bad("提示词片段 %q（第 %d 个）的 Text 为 nil", s.Name, j)
			}
			claim("提示词片段", s.Name, name)
		}
		for j, s := range c.PromptContexts {
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
```

import 中补上 `"reflect"`。

- [ ] **Step 5：运行确认通过**

Run: `go test ./plugin/ -race`
Expected: PASS。若 `TestInstallMergesRequestErrors` 中 `observed` 不为 2，先读 `builder/loop_hooks.go` 中 RequestError 钩子的调用顺序（"第一个返回 RequestRetry 的决定生效"）确认断言，再改实现，不改断言语义。

- [ ] **Step 6：提交**

```bash
git add plugin/install.go plugin/install_test.go plugin/testing_test.go
git commit -m "feat(plugin): Install 校验插件贡献后一次性写入 Builder"
```

---

### Task 4：插件驱动 Agent 的集成测试

**Files:**
- Test: `plugin/agent_test.go`

**Interfaces:**
- Consumes: Task 1–3 的全部导出 API 与测试辅助。

- [ ] **Step 1：写测试** `plugin/agent_test.go`

```go
package plugin

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

type countingMiddleware struct {
	*middleware.BaseMiddleware
	calls *atomic.Int32
}

func (m *countingMiddleware) OnBefore(ctx context.Context, r *middleware.MiddlewareRequest) (*middleware.MiddlewareRequest, error) {
	m.calls.Add(1)
	return m.BaseMiddleware.OnBefore(ctx, r)
}

type startCallback struct {
	*core.BaseCallback
	calls *atomic.Int32
}

func (c *startCallback) OnStart(context.Context, interface{}) error {
	c.calls.Add(1)
	return nil
}

// 每类贡献都真正作用于 Agent。
func TestPluginContributionsDriveAgent(t *testing.T) {
	var toolRuns, preSteps, stops, mw, cb atomic.Int32
	p := contributing("orders", Contribution{
		Tools:          []interfaces.Tool{toolNamed("lookup_order", &toolRuns)},
		PromptSections: []builder.PromptSection{section("orders", "退款须提供订单号")},
		PromptContexts: []builder.PromptContext{{Name: "clock", Text: func(builder.PromptScope) string { return "现在是工作时间" }}},
		PreSteps: []PreStep{{Identity: "count", Hook: func(context.Context, *builder.Step) (builder.PreStepResult, error) {
			preSteps.Add(1)
			return builder.PreStepResult{}, nil
		}}},
		RequestHooks: []builder.RequestHook{func(_ context.Context, r *builder.ModelRequest) error {
			r.Request.Temperature = 0.3
			return nil
		}},
		TurnStopping: []builder.TurnStoppingHook{func(_ context.Context, s *builder.Stopping) (string, error) {
			if stops.Add(1) == 1 {
				return "请给出订单号", nil
			}
			return "", nil
		}},
		Middleware: []middleware.Middleware{&countingMiddleware{BaseMiddleware: middleware.NewBaseMiddleware("count"), calls: &mw}},
		Callbacks:  []core.Callback{&startCallback{BaseCallback: core.NewBaseCallback(), calls: &cb}},
	})
	h := startedHost(t, p)
	model := newModel(calls("lookup_order"), answer("已退款"), answer("订单 A1 已退款"))
	b := builder.NewSimpleBuilder(model).WithMaxIterations(6)
	require.NoError(t, Install(context.Background(), b, h))
	agent, err := b.Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "退款")
	require.NoError(t, err)
	require.Equal(t, "订单 A1 已退款", out.Result)

	first := model.request(0)
	require.Contains(t, first.Messages[0].Content, "退款须提供订单号")
	require.Contains(t, first.Messages[len(first.Messages)-1].Content, "现在是工作时间")
	require.Equal(t, 0.3, first.Temperature)
	require.EqualValues(t, 1, toolRuns.Load())
	require.GreaterOrEqual(t, preSteps.Load(), int32(1))
	require.EqualValues(t, 2, stops.Load())
	require.GreaterOrEqual(t, mw.Load(), int32(1))
	require.GreaterOrEqual(t, cb.Load(), int32(1))
}

// 持久运行中插件 PreStep 的身份包含名称与版本：升级版本后恢复旧运行以 intent changed 失败。
func TestPluginVersionEntersDurableIdentity(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "plugin-run"), store)
	var runs atomic.Int32
	run := func(version string, model *scriptModel) (*builder.AgentOutput, error) {
		p := &testPlugin{name: "redact", version: version, contribute: func(context.Context, *Env) (Contribution, error) {
			return Contribution{
				Tools: []interfaces.Tool{toolNamed("lookup_order", &runs)},
				PreSteps: []PreStep{{Identity: "mask", Hook: func(context.Context, *builder.Step) (builder.PreStepResult, error) {
					return builder.PreStepResult{}, nil
				}}},
			}, nil
		}}
		h := startedHost(t, p)
		b := builder.NewSimpleBuilder(model).WithDurableScope("support").WithMaxIterations(4)
		if err := Install(ctx, b, h, Dispatch(tools.DispatchConfig{MaxConcurrency: 1})); err != nil {
			return nil, err
		}
		agent, err := b.Build()
		if err != nil {
			return nil, err
		}
		return agent.AsRunnable().Invoke(ctx, "查订单")
	}

	out, err := run("1", newModel(calls("lookup_order"), answer("已查到")))
	require.NoError(t, err)
	require.Equal(t, "已查到", out.Result)

	replay := newModel()
	out, err = run("1", replay)
	require.NoError(t, err)
	require.Equal(t, "已查到", out.Result)
	require.Zero(t, replay.count(), "同版本恢复只回放")
	require.EqualValues(t, 1, runs.Load())

	_, err = run("2", newModel())
	require.ErrorContains(t, err, "intent changed")
}

var _ llm.ToolStreamClient = (*scriptModel)(nil)
```

- [ ] **Step 2：运行**

Run: `go test ./plugin/ -run 'TestPlugin' -race -v`
Expected: PASS。若中间件计数为 0，读 `builder/events.go:106` 附近确认原生流程使用中间件链的条件；若 `intent changed` 文本不同，读 `builder/loop_hooks.go:174` 附近的错误信息并以实际文本断言。只改断言中的文本，不放宽语义。

- [ ] **Step 3：提交**

```bash
git add plugin/agent_test.go
git commit -m "test(plugin): 验证各类贡献作用于 Agent 且版本进入持久身份"
```

---

### Task 5：删除旧插件注册中心、导入规则与文档

**Files:**
- Delete: `core/plugin_registry_enhanced.go`、`core/plugin_registry_enhanced_test.go`
- Modify: `scripts/apicheck/main.go`（`forbidden`）、`scripts/apicheck/main_test.go`
- Create: `plugin/README.md`
- Modify: `docs/guides/PLUGIN_SYSTEM_GUIDE.md`、`docs/README.md`、`docs/architecture/ARCHITECTURE.md:92`、`CHANGELOG.md`

- [ ] **Step 1：确认没有其他引用后删除**

Run: `rtk proxy grep -rn 'EnhancedPluginRegistry\|NewBasePlugin\|BasePlugin\b\|PluginConfig\|GlobalEnhancedPluginRegistry\|core\.Container\|NewContainer\|ResolveTyped' --include='*.go' . ../goagent-examples ../goagent-contrib | grep -v '^./core/plugin_registry_enhanced'`
Expected: 无输出（`core/plugin_bridge.go` 的 `PluginRegistry`、`PluginMetadata` 不在此列）。

```bash
git rm core/plugin_registry_enhanced.go core/plugin_registry_enhanced_test.go
go build ./... && go vet ./core/
```

若 `core` 中有只被删除文件使用的辅助函数（如 `DefaultLifecycleManager` 以外的私有函数）导致 unused 告警，一并删除。

- [ ] **Step 2：apicheck 写失败的测试**

在 `scripts/apicheck/main_test.go` 追加：

```go
func TestPluginIsTopLevel(t *testing.T) {
	for _, file := range []string{"builder/a.go", "core/a.go", "interfaces/a.go", "llm/a.go", "tools/a.go"} {
		require.True(t, forbidden(file, module+"plugin"), file)
	}
	for _, file := range []string{"plugin/install.go", "execution/scope.go", "cmd/host/main.go"} {
		require.False(t, forbidden(file, module+"plugin"), file)
	}
}
```

Run: `go test ./scripts/apicheck/ -run TestPluginIsTopLevel`
Expected: FAIL。

- [ ] **Step 3：实现规则**

在 `scripts/apicheck/main.go` 的 `forbidden` 中，`if dest == "execution" {` 块之后加入：

```go
	// plugin 在 builder 之上组合能力，执行机制不得反向依赖它。
	if dest == "plugin" {
		switch source {
		case "interfaces", "errors", "utils", "cache", "core", "builder", "tools", "parsers", "llm", "workflow", "memory", "store", "middleware", "observability":
			return true
		}
	}
```

Run: `go test ./scripts/apicheck/ && go run ./scripts/apicheck`
Expected: PASS，`imports: 0 findings`。

- [ ] **Step 4：文档**

`plugin/README.md`（新建）：

```markdown
# plugin

Agent 能力包：在 Go 中静态组合工具、提示词片段、循环钩子、中间件、回调与进程级资源，由 `Host` 管理启停，经 `Install` 装进任意 `builder.AgentBuilder`。

| 文件 | 内容 |
|---|---|
| `plugin.go` | `Plugin`、`Starter`、`Dependent`、`PreStep`、`Contribution` |
| `resources.go` | `Env`、`Resources`、`Lookup` |
| `host.go` | `Host`：依赖排序、启动回滚、停止 |
| `install.go` | `Install`、`Only`、`Dispatch` |

使用方式与规则见 [插件指南](../docs/guides/PLUGIN_SYSTEM_GUIDE.md)。
```

`docs/guides/PLUGIN_SYSTEM_GUIDE.md`：把标题改为"插件"，开头一段改为"Agent 能力包见第一节；`core` 的 Runnable 注册表见最后一节"。删除"带版本与生命周期的插件"整节与表格中 `EnhancedPluginRegistry`、`Plugin / BasePlugin`、`Container` 三行及 `GlobalEnhancedPluginRegistry` 的说明，新增第一节"Agent 能力包"，内容为：

````markdown
## Agent 能力包

插件把工具、提示词片段、循环钩子、中间件、回调与进程级资源打包，经 `plugin.Install` 装进 Agent。`builder` 不依赖 `plugin`；goagent-harness 的 preset 组件可以直接包装插件。

```go
type Orders struct{ cfg OrdersConfig }

func (*Orders) Name() string    { return "orders" }
func (*Orders) Version() string { return "1.0.0" }
func (o *Orders) Contribute(ctx context.Context, env *plugin.Env) (plugin.Contribution, error) {
    db, err := plugin.Lookup[*sql.DB](env.Resources(), "database.primary")
    if err != nil { return plugin.Contribution{}, err }
    return plugin.Contribution{
        Tools:          []interfaces.Tool{newLookupOrder(db)},
        PromptSections: []builder.PromptSection{{Name: "orders", Text: func(builder.PromptScope) string { return "退款前先查询订单" }}},
    }, nil
}
func (*Orders) Requires() []string { return []string{"database"} }

host, err := plugin.NewHost(database, &Orders{cfg: cfg}, redact)
if err != nil { return err }
if err := host.Start(ctx); err != nil { return err }
defer host.Stop(context.Background())

b := builder.NewSimpleBuilder(client).WithDurableScope("support")
if err := plugin.Install(ctx, b, host, plugin.Dispatch(tools.DispatchConfig{MaxConcurrency: 4})); err != nil { return err }
agent, err := b.Build()
```

规则（以代码为准）：

- 插件名在 Host 内唯一：非空、无首尾空白、不含 `/` 与 `@`；版本非空、无首尾空白、不含 `/`。`NewHost` 一次报告全部问题，包括依赖未注册与循环依赖。
- `Host.Start` 按依赖拓扑顺序（同层保持注册顺序）调用 `Starter.Start`；任一失败或 ctx 结束时逆序停止已启动的插件，失败的插件自身不调用 `Stop`。Host 不可重启，`Stop` 逆序停止并合并错误，重复调用返回 nil。
- 资源只能在本插件 `Start` 中经 `env.Provide("<插件名>.<名称>", v)` 发布；其他插件用 `plugin.Lookup[T]` 取出。Host 停止后资源清空，须先停用 Agent 再停止 Host。
- `Install` 要求 Host 已启动；先调用所选插件的 `Contribute` 并校验（工具、提示词片段、提示词上下文、中间件重名，PreStep 缺少 Identity，nil 钩子），全部通过后才写入 Builder。`Only(names...)` 选择插件；`Dispatch(cfg)` 把插件工具统一经一个 Dispatcher 包装，持久运行必须使用。
- 插件 PreStep 以 `plugin/<名称>@<版本>/<Identity>` 进入持久日志，升级版本后恢复旧运行以 `intent changed` 失败。
- 所有插件的 `RequestErrors` 合并后注册一次，恢复次数取各插件 `MaxRecoveries` 的最大值；宿主之后再调用 `WithRequestError` 会覆盖它。
- 同一个 Builder 只应 Install 一次。

案例见 goagent-examples 的 `orchestration/plugin/`。
````

`docs/README.md`：在包索引中加入 `plugin`（"Agent 能力包与插件生命周期"），并把指向 `PLUGIN_SYSTEM_GUIDE.md` 的描述改为"插件：Agent 能力包与 Runnable 注册表"。
`docs/architecture/ARCHITECTURE.md:92`：`core` 行去掉"与插件注册"，改为"生命周期与 Runnable 注册表"；在包表中 `builder` 之后加一行 `| \`plugin\` | Agent 能力包：Host 启停、共享资源、Install 装进 Builder |`。

`CHANGELOG.md` 在文件头之后加入：

```markdown
## [Unreleased]

### Added

- `plugin` 包：Agent 能力包。插件返回工具、提示词片段、循环钩子、中间件与回调；`Host` 按依赖顺序启停持有资源的插件，启动失败时回滚，经 `Env.Provide`/`plugin.Lookup` 共享资源；`plugin.Install` 校验冲突后装进任意 `AgentBuilder`，支持 `Only` 与 `Dispatch`。插件 PreStep 的持久身份含插件名与版本。

### Removed

- `core.Plugin`、`core.BasePlugin`、`core.EnhancedPluginRegistry`、`core.GlobalEnhancedPluginRegistry`、`core.PluginConfig`、`core.Container`、`core.ResolveTyped`：插件提供的 `DynamicRunnable` 无法被 Agent 使用。迁移：把插件改为实现 `plugin.Plugin`，`GetTools` 返回的能力改为 `Contribution.Tools` 中的 `interfaces.Tool`，`Init`/`Start`/`Stop` 改为 `plugin.Starter`，`Container.Register` 改为在 `Start` 中 `env.Provide`，`ResolveTyped` 改为 `plugin.Lookup`。`core.PluginRegistry` 等 Runnable 注册表不变。
```

- [ ] **Step 5：全量验证**

```bash
gofmt -l . | grep -v '^.context' ; go vet ./... && go test -race ./plugin/ ./core/ ./scripts/apicheck/ && go test ./...
GOTOOLCHAIN=go1.25.0 GOFLAGS=-mod=mod go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.6.2 run --timeout=5m ./...
go run ./scripts/apicheck
cd $CLAUDE_JOB_DIR/tmp/doccheck && go run main.go /home/hellotalk/code/go/src/github.com/kart-io/goagent
```

Expected: gofmt 无输出；测试全部通过；lint `0 issues`；apicheck `0 findings`；doccheck 不报告新文档中的失效标识符（如工具不存在则跳过，并在报告中说明）。

- [ ] **Step 6：提交**

```bash
git add -A core scripts/apicheck plugin/README.md docs CHANGELOG.md
git commit -m "feat(plugin)!: 以 plugin 包取代 core 中不与 Agent 集成的插件注册中心

core.Plugin 提供的 DynamicRunnable 与 interfaces.Tool 不兼容，注册的插件无法被 Agent 使用。
新的 plugin 包让插件返回贡献清单，由 Install 校验后装进 Builder，Host 管理依赖与资源。
迁移说明见 CHANGELOG。"
```

---

### Task 6：案例共用插件与基础案例（goagent-examples）

在 goagent-examples 中 `git checkout -b feat/plugin-cases`，`make work` 生成指向 `../goagent` 的 `go.work`（已被忽略）。

**Files:**
- Create: `orchestration/plugin/internal/shop/shop.go`
- Create: `orchestration/plugin/basic/main.go`、`main_test.go`、`README.md`

**Interfaces:**
- Produces（`shop` 包）：`Orders(runs *atomic.Int32) plugin.Plugin`（名称 `orders`，工具 `lookup_order`，片段 `orders`）、`Shipping() plugin.Plugin`（`shipping`，工具 `track_parcel`）、`RefundPolicy() plugin.Plugin`（`refund-policy`，片段 `refund-policy`）、`Redact(version string) plugin.Plugin`（`redact`，PreStep 身份 `mask-phone`）、`MaskPhone(string) string`、`Tool(name, result string, runs *atomic.Int32) interfaces.Tool`、`Static(name, version string, c plugin.Contribution) plugin.Plugin`。
- Produces（各案例）：`run(...)` 与 `result` 结构体，供 `main_test.go` 使用。

- [ ] **Step 1：写** `orchestration/plugin/internal/shop/shop.go`

```go
// Package shop 是插件案例共用的电商插件：订单、物流、退款政策与手机号脱敏。
package shop

import (
	"context"
	"regexp"
	"sync/atomic"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/plugin"
	"github.com/kart-io/goagent/tools"
)

type static struct {
	name, version string
	c             plugin.Contribution
}

func (s *static) Name() string    { return s.name }
func (s *static) Version() string { return s.version }
func (s *static) Contribute(context.Context, *plugin.Env) (plugin.Contribution, error) {
	return s.c, nil
}

// Static 返回固定贡献的插件。
func Static(name, version string, c plugin.Contribution) plugin.Plugin {
	return &static{name: name, version: version, c: c}
}

// Tool 返回无参数工具，执行时把 runs 加一（runs 可为 nil）。
func Tool(name, result string, runs *atomic.Int32) interfaces.Tool {
	return tools.NewBaseTool(name, name, `{"type":"object","properties":{},"additionalProperties":false}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		if runs != nil {
			runs.Add(1)
		}
		return &interfaces.ToolOutput{Success: true, Result: result}, nil
	})
}

func text(s string) func(builder.PromptScope) string { return func(builder.PromptScope) string { return s } }

// Orders 提供订单查询工具与使用说明。
func Orders(runs *atomic.Int32) plugin.Plugin {
	return Static("orders", "1.0.0", plugin.Contribution{
		Tools:          []interfaces.Tool{Tool("lookup_order", "订单 A100：已发货，金额 199 元，下单 5 天", runs)},
		PromptSections: []builder.PromptSection{{Name: "orders", Text: text("回答订单问题前先调用 lookup_order，回答中给出订单号。")}},
	})
}

// Shipping 提供物流查询工具。
func Shipping() plugin.Plugin {
	return Static("shipping", "1.0.0", plugin.Contribution{
		Tools: []interfaces.Tool{Tool("track_parcel", "包裹在上海转运中心，预计明天送达", nil)},
	})
}

// RefundPolicy 只提供退款政策提示词。
func RefundPolicy() plugin.Plugin {
	return Static("refund-policy", "1.0.0", plugin.Contribution{
		PromptSections: []builder.PromptSection{{Name: "refund-policy", Order: 1, Text: text("签收 7 天内可无理由退款。")}},
	})
}

var phone = regexp.MustCompile(`1[3-9]\d{9}`)

// MaskPhone 把手机号中间四位替换为星号。
func MaskPhone(s string) string {
	return phone.ReplaceAllStringFunc(s, func(p string) string { return p[:3] + "****" + p[7:] })
}

// Redact 在每次模型请求前脱敏用户消息中的手机号。version 进入持久身份。
func Redact(version string) plugin.Plugin {
	return Static("redact", version, plugin.Contribution{
		PreSteps: []plugin.PreStep{{Identity: "mask-phone", Hook: func(_ context.Context, s *builder.Step) (builder.PreStepResult, error) {
			for i, m := range s.Messages {
				if m.Role == "user" {
					s.Messages[i].Content = MaskPhone(m.Content)
				}
			}
			return builder.PreStepResult{}, nil
		}}},
	})
}
```


- [ ] **Step 2：写失败的测试** `orchestration/plugin/basic/main_test.go`

```go
package main

import (
	"context"
	"testing"

	"github.com/kart-io/goagent-examples/internal/livemodel/livemodeltest"
	"github.com/stretchr/testify/require"
)

func TestBasicComposesPlugins(t *testing.T) {
	out, err := run(context.Background(), demoModel())
	require.NoError(t, err)
	require.Equal(t, []string{"orders@1.0.0", "shipping@1.0.0"}, out.Plugins)
	require.EqualValues(t, 1, out.OrderLookups)
	require.Contains(t, out.Answer, "A100")
	require.Contains(t, out.Answer, "上海")
}

func TestBasicConflict(t *testing.T) {
	err := conflict(context.Background())
	require.ErrorContains(t, err, `插件 "orders" 与 "orders-copy" 提供了同名工具 "lookup_order"`)
}

func TestBasicLive(t *testing.T) {
	client := livemodeltest.Require(t)
	out, err := run(context.Background(), client)
	require.NoError(t, err)
	require.GreaterOrEqual(t, out.OrderLookups, int32(1), "真实模型应调用订单工具")
}
```

Run: `go test ./orchestration/plugin/basic/`
Expected: FAIL，`undefined: run`。

- [ ] **Step 3：写** `orchestration/plugin/basic/main.go`

```go
// 基础与组合：订单插件与物流插件装进同一个 Agent；两个插件提供同名工具时 Install 报错。
// 默认使用离线模型；GOAGENT_LIVE=1 时使用真实模型（见 internal/livemodel）。
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"github.com/kart-io/goagent-examples/internal/livemodel"
	"github.com/kart-io/goagent-examples/orchestration/internal/demomodel"
	"github.com/kart-io/goagent-examples/orchestration/plugin/internal/shop"
	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/plugin"
)

type result struct {
	Answer       string
	Plugins      []string
	OrderLookups int32
}

// demoModel 第一轮同时调用两个工具，第二轮用工具结果作答。
func demoModel() llm.ToolStreamClient {
	var calls atomic.Int64
	return &demomodel.Model{Calls: &calls, Respond: func(r *llm.CompletionRequest) llm.StreamChunk {
		var results []string
		for _, m := range r.Messages {
			if m.Role == "tool" {
				results = append(results, m.Content)
			}
		}
		if len(results) == 0 {
			return llm.StreamChunk{ToolCalls: []llm.ToolCall{demomodel.Call("c1", "lookup_order"), demomodel.Call("c2", "track_parcel")}}
		}
		return llm.StreamChunk{Content: "订单 A100 已发货；" + strings.Join(results, "；")}
	}}
}

func run(ctx context.Context, model llm.ToolStreamClient) (result, error) {
	var lookups atomic.Int32
	host, err := plugin.NewHost(shop.Orders(&lookups), shop.Shipping())
	if err != nil {
		return result{}, err
	}
	if err := host.Start(ctx); err != nil {
		return result{}, err
	}
	defer func() { _ = host.Stop(context.Background()) }()

	b := builder.NewSimpleBuilder(model).WithSystemPrompt("你是电商客服。").WithMaxIterations(4)
	if err := plugin.Install(ctx, b, host); err != nil {
		return result{}, err
	}
	agent, err := b.Build()
	if err != nil {
		return result{}, err
	}
	out, err := agent.AsRunnable().Invoke(ctx, "我的订单 A100 到哪了？")
	if err != nil {
		return result{}, err
	}
	return result{Answer: out.Result, Plugins: host.Plugins(), OrderLookups: lookups.Load()}, nil
}

// conflict 演示两个插件提供同名工具：Install 在写入 Builder 前报错。
func conflict(ctx context.Context) error {
	host, err := plugin.NewHost(shop.Orders(nil), shop.Static("orders-copy", "1.0.0", plugin.Contribution{
		Tools: []interfaces.Tool{shop.Tool("lookup_order", "另一份订单数据", nil)},
	}))
	if err != nil {
		return err
	}
	if err := host.Start(ctx); err != nil {
		return err
	}
	defer func() { _ = host.Stop(context.Background()) }()
	return plugin.Install(ctx, builder.NewSimpleBuilder(demoModel()), host)
}

func main() {
	ctx := context.Background()
	var model llm.ToolStreamClient = demoModel()
	if os.Getenv("GOAGENT_LIVE") == "1" {
		client, err := livemodel.FromEnv()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		model = client
	}
	out, err := run(ctx, model)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("插件：%v\n订单查询次数：%d\n回答：%s\n", out.Plugins, out.OrderLookups, out.Answer)
	fmt.Println("同名工具冲突：", conflict(ctx))
}
```

- [ ] **Step 4：README** `orchestration/plugin/basic/README.md`

```markdown
# 插件：基础与组合

订单插件提供 `lookup_order` 工具与使用说明，物流插件提供 `track_parcel`，两者经 `plugin.Install` 装进同一个 Agent。另一个插件也提供 `lookup_order` 时，`Install` 在写入 Builder 前报告冲突双方。

```bash
go run ./orchestration/plugin/basic                 # 离线模型
GOAGENT_LIVE=1 DEEPSEEK_API_KEY=... go run ./orchestration/plugin/basic
go test ./orchestration/plugin/basic
```
```

- [ ] **Step 5：运行确认通过**

Run: `go test -race ./orchestration/plugin/... && go run ./orchestration/plugin/basic`
Expected: 测试 PASS（`TestBasicLive` 跳过）；输出插件列表、查询次数 1 与冲突错误。

- [ ] **Step 6：提交**

```bash
git add orchestration/plugin
git commit -m "feat(plugin): 新增插件基础与组合案例"
```

---

### Task 7：钩子类插件案例

**Files:**
- Create: `orchestration/plugin/hooks/main.go`、`main_test.go`、`README.md`

**Interfaces:**
- Consumes: `shop.Orders`、`shop.Redact`、`shop.Static`、`shop.MaskPhone`。
- Produces: `run(ctx, model llm.ToolStreamClient) (result, error)`，`result{Answer string; Retries, Continued, Audited int32}`；`demoModel() (llm.ToolStreamClient, func() bool)`，第二个返回值报告模型是否见过完整手机号；`flaky` 客户端。

- [ ] **Step 1：写失败的测试** `orchestration/plugin/hooks/main_test.go`

```go
package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHookPlugins(t *testing.T) {
	model, seen := demoModel()
	out, err := run(context.Background(), &flaky{ToolStreamClient: model, failures: 1})
	require.NoError(t, err)
	require.EqualValues(t, 1, out.Retries, "首个请求 503 后由限流重试插件重试")
	require.EqualValues(t, 1, out.Continued, "答案缺少订单号时续跑一次")
	require.GreaterOrEqual(t, out.Audited, int32(1))
	require.Contains(t, out.Answer, "A100")
	require.False(t, seen(), "模型从未看到完整手机号")
}
```

- [ ] **Step 2：运行确认失败**

Run: `go test ./orchestration/plugin/hooks/`
Expected: FAIL，`undefined: run`。

- [ ] **Step 3：写** `orchestration/plugin/hooks/main.go`

```go
// 钩子类插件：手机号脱敏（PreStep）、限流重试（RequestError）、答案校验续跑（TurnStopping）、审计中间件。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent-examples/internal/livemodel"
	"github.com/kart-io/goagent-examples/orchestration/internal/demomodel"
	"github.com/kart-io/goagent-examples/orchestration/plugin/internal/shop"
	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/plugin"
)

const question = "我是 13800138000，订单什么时候到？"

type result struct {
	Answer                      string
	Retries, Continued, Audited int32
}

// flaky 让前 failures 次请求以 503 失败，模拟上游限流。
type flaky struct {
	llm.ToolStreamClient
	failures int32
	n        atomic.Int32
}

func (f *flaky) CompleteToolsStream(ctx context.Context, r *llm.CompletionRequest, ts []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	if f.n.Add(1) <= f.failures {
		return nil, &llm.ProviderError{StatusCode: 503, Err: errors.New("服务暂时不可用")}
	}
	return f.ToolStreamClient.CompleteToolsStream(ctx, r, ts)
}

// demoModel 先调用订单工具，第一次作答故意不给订单号；seen 报告模型是否见过完整手机号。
func demoModel() (llm.ToolStreamClient, func() bool) {
	var calls atomic.Int64
	var sawPhone atomic.Bool
	var answers atomic.Int32
	m := &demomodel.Model{Calls: &calls, Respond: func(r *llm.CompletionRequest) llm.StreamChunk {
		hasTool := false
		for _, msg := range r.Messages {
			sawPhone.Store(sawPhone.Load() || strings.Contains(msg.Content, "13800138000"))
			hasTool = hasTool || msg.Role == "tool"
		}
		if !hasTool {
			return llm.StreamChunk{ToolCalls: []llm.ToolCall{demomodel.Call("c1", "lookup_order")}}
		}
		if answers.Add(1) == 1 {
			return llm.StreamChunk{Content: "您的订单已发货。"}
		}
		return llm.StreamChunk{Content: "订单 A100 已发货，预计明天送达。"}
	}}
	return m, sawPhone.Load
}

var orderID = regexp.MustCompile(`A\d+`)

type audit struct {
	*middleware.BaseMiddleware
	calls *atomic.Int32
}

func (a *audit) OnBefore(ctx context.Context, r *middleware.MiddlewareRequest) (*middleware.MiddlewareRequest, error) {
	a.calls.Add(1)
	return a.BaseMiddleware.OnBefore(ctx, r)
}

func run(ctx context.Context, model llm.ToolStreamClient) (result, error) {
	var retries, continued, audited atomic.Int32
	retry := builder.RetryTransient(2, func(int) time.Duration { return 10 * time.Millisecond })
	host, err := plugin.NewHost(
		shop.Orders(nil),
		shop.Redact("1.0.0"),
		shop.Static("retry", "1.0.0", plugin.Contribution{
			MaxRecoveries: 2,
			RequestErrors: []builder.RequestErrorHook{func(ctx context.Context, f *builder.RequestFailure) (builder.RequestErrorDecision, error) {
				d, err := retry(ctx, f)
				if d.Action == builder.RequestRetry {
					retries.Add(1)
				}
				return d, err
			}},
		}),
		shop.Static("verify", "1.0.0", plugin.Contribution{
			TurnStopping: []builder.TurnStoppingHook{func(_ context.Context, s *builder.Stopping) (string, error) {
				if orderID.MatchString(s.Answer) {
					return "", nil
				}
				continued.Add(1)
				return "回答中必须给出订单号。", nil
			}},
		}),
		shop.Static("audit", "1.0.0", plugin.Contribution{
			Middleware: []middleware.Middleware{&audit{BaseMiddleware: middleware.NewBaseMiddleware("audit"), calls: &audited}},
		}),
	)
	if err != nil {
		return result{}, err
	}
	if err := host.Start(ctx); err != nil {
		return result{}, err
	}
	defer func() { _ = host.Stop(context.Background()) }()
	b := builder.NewSimpleBuilder(model).WithSystemPrompt("你是电商客服。").WithMaxIterations(6)
	if err := plugin.Install(ctx, b, host); err != nil {
		return result{}, err
	}
	agent, err := b.Build()
	if err != nil {
		return result{}, err
	}
	out, err := agent.AsRunnable().Invoke(ctx, question)
	if err != nil {
		return result{}, err
	}
	return result{Answer: out.Result, Retries: retries.Load(), Continued: continued.Load(), Audited: audited.Load()}, nil
}

func main() {
	ctx := context.Background()
	model, seen := demoModel()
	var client llm.ToolStreamClient = &flaky{ToolStreamClient: model, failures: 1}
	if os.Getenv("GOAGENT_LIVE") == "1" {
		live, err := livemodel.FromEnv()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		client, seen = live, func() bool { return false }
	}
	out, err := run(ctx, client)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("重试 %d 次，续跑 %d 次，审计 %d 次，模型见过完整手机号：%v\n回答：%s\n", out.Retries, out.Continued, out.Audited, seen(), out.Answer)
}
```

- [ ] **Step 4：README** `orchestration/plugin/hooks/README.md`

```markdown
# 插件：循环钩子

| 插件 | 扩展点 | 作用 |
|---|---|---|
| `redact` | PreStep（身份 `plugin/redact@1.0.0/mask-phone`） | 模型请求前把手机号中间四位替换为星号 |
| `retry` | RequestError（`builder.RetryTransient`） | 上游 503 时重试，最多 2 次 |
| `verify` | TurnStopping | 答案缺少订单号时要求补充 |
| `audit` | 中间件 | 记录每次运行 |

离线模型的第一个请求返回 503、第一次作答缺少订单号，用来触发重试与续跑。

```bash
go run ./orchestration/plugin/hooks
go test ./orchestration/plugin/hooks
```
```

- [ ] **Step 5：运行确认通过**

Run: `go test -race ./orchestration/plugin/hooks/ && go run ./orchestration/plugin/hooks`
Expected: PASS；输出"重试 1 次，续跑 1 次"。若 `Retries` 为 0，确认 `RetryTransient` 对 503 判为可重试（`llm.ClassifyError`）且 `flaky` 的错误在原生工具流中触发 RequestError，再调整案例，不改断言语义。

- [ ] **Step 6：提交**

```bash
git add orchestration/plugin/hooks
git commit -m "feat(plugin): 新增钩子类插件案例"
```

---

### Task 8：资源、依赖与启动回滚案例

**Files:**
- Create: `orchestration/plugin/resources/main.go`、`main_test.go`、`README.md`

**Interfaces:**
- Produces: `inventory` 类型（`Stock(sku) int`、`Closed() bool`）、`inventoryPlugin`、`pricingPlugin`、`run(ctx, model, failAudit bool) (result, error)`，`result{Answer string; Events []string; InventoryClosed bool}`。

- [ ] **Step 1：写失败的测试** `orchestration/plugin/resources/main_test.go`

```go
package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResourcesSharedAcrossPlugins(t *testing.T) {
	out, err := run(context.Background(), demoModel(), false)
	require.NoError(t, err)
	require.Contains(t, out.Answer, "库存 12")
	require.Contains(t, out.Answer, "单价 99")
	require.Equal(t, []string{"打开库存", "定价就绪", "定价停止", "关闭库存"}, out.Events)
	require.True(t, out.InventoryClosed)
}

func TestStartFailureRollsBack(t *testing.T) {
	out, err := run(context.Background(), demoModel(), true)
	require.ErrorContains(t, err, `插件 "audit-log" 启动失败`)
	require.Equal(t, []string{"打开库存", "定价就绪", "定价停止", "关闭库存"}, out.Events)
	require.True(t, out.InventoryClosed, "已打开的库存被回滚关闭")
}
```

- [ ] **Step 2：运行确认失败**

Run: `go test ./orchestration/plugin/resources/`
Expected: FAIL，`undefined: run`。

- [ ] **Step 3：写** `orchestration/plugin/resources/main.go`

```go
// 资源与依赖：库存插件在 Start 中打开库存并发布为共享资源，定价插件声明依赖并取出使用；
// 审计插件启动失败时，已启动的插件按逆序停止。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/kart-io/goagent-examples/internal/livemodel"
	"github.com/kart-io/goagent-examples/orchestration/internal/demomodel"
	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/plugin"
	"github.com/kart-io/goagent/tools"
)

type result struct {
	Answer          string
	Events          []string
	InventoryClosed bool
}

type events struct {
	mu   sync.Mutex
	list []string
}

func (e *events) add(s string) { e.mu.Lock(); defer e.mu.Unlock(); e.list = append(e.list, s) }
func (e *events) all() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.list...)
}

// inventory 模拟需要打开与关闭的连接。
type inventory struct {
	stock  map[string]int
	closed atomic.Bool
}

func (i *inventory) Stock(sku string) (int, error) {
	if i.closed.Load() {
		return 0, errors.New("库存连接已关闭")
	}
	return i.stock[sku], nil
}

type inventoryPlugin struct {
	ev  *events
	inv *inventory
}

func (*inventoryPlugin) Name() string    { return "inventory" }
func (*inventoryPlugin) Version() string { return "1.0.0" }
func (p *inventoryPlugin) Start(_ context.Context, env *plugin.Env) error {
	p.inv = &inventory{stock: map[string]int{"SKU-1": 12}}
	p.ev.add("打开库存")
	return env.Provide("inventory.store", p.inv)
}
func (p *inventoryPlugin) Stop(context.Context) error {
	p.inv.closed.Store(true)
	p.ev.add("关闭库存")
	return nil
}
func (p *inventoryPlugin) Contribute(context.Context, *plugin.Env) (plugin.Contribution, error) {
	return plugin.Contribution{Tools: []interfaces.Tool{tools.NewBaseTool("check_stock", "查询 SKU-1 库存", `{"type":"object","properties":{},"additionalProperties":false}`,
		func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			n, err := p.inv.Stock("SKU-1")
			if err != nil {
				return nil, err
			}
			return &interfaces.ToolOutput{Success: true, Result: fmt.Sprintf("SKU-1 库存 %d", n)}, nil
		})}}, nil
}

type pricingPlugin struct{ ev *events }

func (*pricingPlugin) Name() string       { return "pricing" }
func (*pricingPlugin) Version() string    { return "1.0.0" }
func (*pricingPlugin) Requires() []string { return []string{"inventory"} }
func (p *pricingPlugin) Start(_ context.Context, env *plugin.Env) error {
	if _, err := plugin.Lookup[*inventory](env.Resources(), "inventory.store"); err != nil {
		return err
	}
	p.ev.add("定价就绪")
	return nil
}
func (p *pricingPlugin) Stop(context.Context) error { p.ev.add("定价停止"); return nil }
func (p *pricingPlugin) Contribute(_ context.Context, env *plugin.Env) (plugin.Contribution, error) {
	inv, err := plugin.Lookup[*inventory](env.Resources(), "inventory.store")
	if err != nil {
		return plugin.Contribution{}, err
	}
	return plugin.Contribution{Tools: []interfaces.Tool{tools.NewBaseTool("quote_price", "按库存给出 SKU-1 报价", `{"type":"object","properties":{},"additionalProperties":false}`,
		func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			n, err := inv.Stock("SKU-1")
			if err != nil {
				return nil, err
			}
			price := 99
			if n < 5 {
				price = 129
			}
			return &interfaces.ToolOutput{Success: true, Result: fmt.Sprintf("SKU-1 单价 %d 元", price)}, nil
		})}}, nil
}

// auditLog 依赖定价；fail 为 true 时模拟日志服务不可达。
type auditLog struct{ fail bool }

func (*auditLog) Name() string       { return "audit-log" }
func (*auditLog) Version() string    { return "1.0.0" }
func (*auditLog) Requires() []string { return []string{"pricing"} }
func (a *auditLog) Start(context.Context, *plugin.Env) error {
	if a.fail {
		return errors.New("日志服务不可达")
	}
	return nil
}
func (*auditLog) Stop(context.Context) error { return nil }
func (*auditLog) Contribute(context.Context, *plugin.Env) (plugin.Contribution, error) {
	return plugin.Contribution{}, nil
}

func demoModel() llm.ToolStreamClient {
	var calls atomic.Int64
	return &demomodel.Model{Calls: &calls, Respond: func(r *llm.CompletionRequest) llm.StreamChunk {
		var results []string
		for _, m := range r.Messages {
			if m.Role == "tool" {
				results = append(results, m.Content)
			}
		}
		if len(results) == 0 {
			return llm.StreamChunk{ToolCalls: []llm.ToolCall{demomodel.Call("c1", "check_stock"), demomodel.Call("c2", "quote_price")}}
		}
		return llm.StreamChunk{Content: fmt.Sprint(results)}
	}}
}

func run(ctx context.Context, model llm.ToolStreamClient, failAudit bool) (result, error) {
	ev := &events{}
	inv := &inventoryPlugin{ev: ev}
	// 注册顺序与依赖顺序相反，Host 按依赖排序。
	host, err := plugin.NewHost(&auditLog{fail: failAudit}, &pricingPlugin{ev: ev}, inv)
	if err != nil {
		return result{}, err
	}
	if err := host.Start(ctx); err != nil {
		return result{Events: ev.all(), InventoryClosed: inv.inv != nil && inv.inv.closed.Load()}, err
	}
	b := builder.NewSimpleBuilder(model).WithSystemPrompt("你是商品顾问。").WithMaxIterations(4)
	if err := plugin.Install(ctx, b, host); err != nil {
		return result{}, errors.Join(err, host.Stop(context.Background()))
	}
	agent, err := b.Build()
	if err != nil {
		return result{}, errors.Join(err, host.Stop(context.Background()))
	}
	out, err := agent.AsRunnable().Invoke(ctx, "SKU-1 还有货吗，多少钱？")
	// 先停用 Agent（本例中运行已结束），再停止 Host。
	stopErr := host.Stop(context.Background())
	if err = errors.Join(err, stopErr); err != nil {
		return result{}, err
	}
	return result{Answer: out.Result, Events: ev.all(), InventoryClosed: inv.inv.closed.Load()}, nil
}

func main() {
	var model llm.ToolStreamClient = demoModel()
	if os.Getenv("GOAGENT_LIVE") == "1" {
		live, err := livemodel.FromEnv()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		model = live
	}
	out, err := run(context.Background(), model, false)
	fmt.Printf("回答：%s\n启停：%v\n错误：%v\n", out.Answer, out.Events, err)
	out, err = run(context.Background(), model, true)
	fmt.Printf("审计插件启动失败时的启停：%v\n错误：%v\n", out.Events, err)
}
```


- [ ] **Step 4：README** `orchestration/plugin/resources/README.md`

```markdown
# 插件：资源与依赖

`inventory` 在 `Start` 中打开库存并以 `inventory.store` 发布；`pricing` 声明依赖 `inventory`，在 `Start` 与 `Contribute` 中用 `plugin.Lookup` 取出；`audit-log` 依赖 `pricing`。插件按相反顺序注册，Host 按依赖启动、逆序停止。`audit-log` 启动失败时，已启动的 `pricing`、`inventory` 被逆序停止，库存连接关闭。

```bash
go run ./orchestration/plugin/resources
go test ./orchestration/plugin/resources
```
```

- [ ] **Step 5：运行确认通过**

Run: `go test -race ./orchestration/plugin/resources/ && go run ./orchestration/plugin/resources`
Expected: PASS。

- [ ] **Step 6：提交**

```bash
git add orchestration/plugin/resources
git commit -m "feat(plugin): 新增资源共享与启动回滚案例"
```

---

### Task 9：客服端到端案例（持久运行与恢复）

**Files:**
- Create: `orchestration/plugin/support-desk/main.go`、`main_test.go`、`README.md`
- Modify: `README.md`（目录表）

**Interfaces:**
- Consumes: `shop.Orders`、`shop.RefundPolicy`、`shop.Redact`。
- Produces: `options{Dir, RedactVersion string; CrashBeforeAnswer bool}`、`run(ctx, opts) (result, error)`，`result{Answer string; ModelCalls int64; OrderLookups int32}`。

- [ ] **Step 1：写失败的测试** `orchestration/plugin/support-desk/main_test.go`

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// 子进程在第二次模型请求时退出，模拟工具结果已记入日志后崩溃。
func TestSupportDeskCrashAndResume(t *testing.T) {
	if dir := os.Getenv("GOAGENT_PLUGIN_CRASH_DIR"); dir != "" {
		_, err := run(context.Background(), options{Dir: dir, RedactVersion: "1.0.0", CrashBeforeAnswer: true})
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSupportDeskCrashAndResume$")
	cmd.Env = append(os.Environ(), "GOAGENT_PLUGIN_CRASH_DIR="+dir)
	err := cmd.Run()
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, crashCode, exit.ExitCode())

	out, err := run(context.Background(), options{Dir: dir, RedactVersion: "1.0.0"})
	require.NoError(t, err)
	require.Contains(t, out.Answer, "7 天")
	require.Zero(t, out.OrderLookups, "恢复时回放工具结果，不重复查询订单")
	require.EqualValues(t, 1, out.ModelCalls, "只补发崩溃前未完成的那次请求")

	_, err = run(context.Background(), options{Dir: dir, RedactVersion: "2.0.0"})
	require.ErrorContains(t, err, "intent changed", "脱敏插件升级后不能按旧日志恢复")
}
```

- [ ] **Step 2：运行确认失败**

Run: `go test ./orchestration/plugin/support-desk/`
Expected: FAIL，`undefined: run`。

- [ ] **Step 3：写** `orchestration/plugin/support-desk/main.go`

```go
// 客服端到端：订单、退款政策与脱敏插件组成持久运行的客服 Agent。进程在工具结果记入日志后崩溃，
// 重启后回放已完成的模型回合与工具结果；脱敏插件升级版本后，旧运行不能恢复。
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"github.com/kart-io/goagent-examples/orchestration/internal/demomodel"
	"github.com/kart-io/goagent-examples/orchestration/plugin/internal/shop"
	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/plugin"
	"github.com/kart-io/goagent/tools"
)

const crashCode = 3

type options struct {
	Dir, RedactVersion string
	CrashBeforeAnswer  bool
}

type result struct {
	Answer       string
	ModelCalls   int64
	OrderLookups int32
}

func run(ctx context.Context, opts options) (result, error) {
	store, err := durable.NewFileStore(opts.Dir)
	if err != nil {
		return result{}, err
	}
	ctx = durable.WithJournal(durable.WithExecutionKey(ctx, "ticket/1001"), store)

	var lookups atomic.Int32
	host, err := plugin.NewHost(shop.Orders(&lookups), shop.RefundPolicy(), shop.Redact(opts.RedactVersion))
	if err != nil {
		return result{}, err
	}
	if err := host.Start(ctx); err != nil {
		return result{}, err
	}
	defer func() { _ = host.Stop(context.Background()) }()

	var calls atomic.Int64
	model := &demomodel.Model{Calls: &calls, Respond: func(r *llm.CompletionRequest) llm.StreamChunk {
		var evidence string
		for _, m := range r.Messages {
			if m.Role == "tool" {
				evidence = m.Content
			}
		}
		if evidence == "" {
			return llm.StreamChunk{ToolCalls: []llm.ToolCall{demomodel.Call("c1", "lookup_order")}}
		}
		if opts.CrashBeforeAnswer {
			os.Exit(crashCode)
		}
		return llm.StreamChunk{Content: "订单 A100 签收 5 天，符合 7 天无理由退款。" + strings.TrimSpace(evidence)}
	}}
	b := builder.NewSimpleBuilder(model).WithSystemPrompt("你是电商客服。").WithDurableScope("support-desk").WithMaxIterations(4)
	if err := plugin.Install(ctx, b, host, plugin.Dispatch(tools.DispatchConfig{MaxConcurrency: 1})); err != nil {
		return result{}, err
	}
	agent, err := b.Build()
	if err != nil {
		return result{}, err
	}
	out, err := agent.AsRunnable().Invoke(ctx, "我是 13800138000，订单 A100 想退款")
	if err != nil {
		return result{}, err
	}
	return result{Answer: out.Result, ModelCalls: calls.Load(), OrderLookups: lookups.Load()}, nil
}

func main() {
	dir := "support-desk-journal"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	out, err := run(context.Background(), options{Dir: dir, RedactVersion: "1.0.0"})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("回答：%s\n本进程模型请求 %d 次，订单查询 %d 次（重复运行同一目录时均为 0）\n", out.Answer, out.ModelCalls, out.OrderLookups)
}
```

`demomodel.Model.Calls` 只在真正进入传输层时计数，回放不计入（见 `orchestration/internal/demomodel`）。

- [ ] **Step 4：README 与目录表**

`orchestration/plugin/support-desk/README.md`：

```markdown
# 插件：客服端到端

`orders`、`refund-policy`、`redact` 三个插件组成持久运行的客服 Agent，插件工具经 `plugin.Dispatch` 记入日志。测试先让子进程在工具结果记入日志后退出，再在同一日志目录恢复：已完成的模型回合与订单查询被回放，只补发崩溃前未完成的请求。脱敏插件升级为 2.0.0 后，PreStep 身份 `plugin/redact@2.0.0/mask-phone` 与日志不符，恢复以 `intent changed` 失败。

```bash
go run ./orchestration/plugin/support-desk /tmp/desk   # 第二次运行同一目录只回放
go test ./orchestration/plugin/support-desk
```
```

根 `README.md` 目录表在 `orchestration/ops` 之后加一行：

```markdown
| [orchestration/plugin](orchestration/plugin/basic/README.md) | Agent 能力包：组合与冲突、循环钩子、资源与依赖、持久运行的客服端到端 |
```

- [ ] **Step 5：运行确认通过**

Run: `go test -race ./orchestration/plugin/... && go vet ./...`
Expected: PASS。若恢复后 `ModelCalls` 不为 1，读 `orchestration/composed-order/main_test.go` 的 `TestComposedOrderProcessRecovery` 核对崩溃点与计数方式，再调整断言说明，不放宽"订单不重复查询"。

- [ ] **Step 6：提交**

```bash
git add orchestration/plugin/support-desk README.md
git commit -m "feat(plugin): 新增持久运行的客服插件案例"
```

---

### Task 10：发布与依赖升级（需用户指示）

- [ ] goagent：用户确认后合并 `feat/agent-plugin` 到 master 并推送；自动打出 v0.32.0 后把 CHANGELOG 的 `[Unreleased]` 改为 `[0.32.0] - <日期>`。
- [ ] goagent-examples：`GOWORK=off go get github.com/kart-io/goagent@v0.32.0 && go mod tidy`，运行 `make test-remote`（依赖已发布版本）通过后提交 `chore(deps): 升级 goagent 到 v0.32.0`，用户确认后合并到 main。

## 依赖与实施

Task 1→2→3→4→5 在 goagent 中顺序执行；Task 6–9 依赖 Task 5 完成的 API，可在 `go.work` 下开发，彼此之间只共享 `shop` 包（Task 6 创建），7–9 可并行。Task 10 只在用户指示后执行。
