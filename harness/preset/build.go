package preset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/coding"
	"github.com/kart-io/goagent/tools/policy"
	"github.com/kart-io/goagent/tools/spill"
)

// Host 是宿主提供给 Build 的环境。模型、SessionID、持久化作用域与预算由宿主在 Builder 上配置。
type Host struct {
	// Root 是工作区根目录，workspace 组件需要；也是信任授权的项目标识。
	Root string
	// Approver 处理 Ask；为 nil 时 Ask 的结果为 Unavailable，按拒绝处理。
	Approver tools.Approver
	// Trust 记录扩权项的授权；为 nil 时含扩权项的 preset 一律返回 *TrustRequiredError。
	Trust TrustStore
	// ArtifactDir 存放溢出文件等宿主产物，默认在用户缓存目录下的 goagent/artifacts。
	ArtifactDir string
	// Secret 解析秘密引用，默认读取环境变量。
	Secret func(name string) (string, bool)
	// Dispatch 是 Dispatcher 的基础配置；Pipeline 由 Build 设置，ReturnArgumentErrors 总是开启。
	Dispatch tools.DispatchConfig
	// RunAgent 运行子 Agent 并返回最终答复，subagent 组件需要它；模型由宿主选择，可用 BuilderRunner。
	RunAgent func(ctx context.Context, asm *Assembly, task string) (string, error)
	// CompactionModel 生成对话摘要，compaction 组件需要它。
	CompactionModel llm.Client
}

// buildOptions 是作为子 Agent 装配时的约束。
type buildOptions struct {
	depth  int
	parent *parentScope
	filter *ToolFilter
}

// parentScope 是父 Agent 的权限：子 Agent 的调用同样要经过父 Agent 的调用前钩子与守卫，权限因此不超过父 Agent。
type parentScope struct {
	pre          []tools.PreHook
	guards       []tools.Guard
	sandbox      Sandbox
	denyApproval bool
}

// WorkspaceInfo 描述 workspace 组件创建的工作区及其沙箱。
type WorkspaceInfo struct {
	Root     string  `json:"root"`
	Sandbox  Sandbox `json:"sandbox"`
	Executor string  `json:"executor"`
	Image    string  `json:"image,omitempty"`
	Network  bool    `json:"network"`
	// CommandsEnabled 表示执行器能兑现沙箱模式，可以提供执行命令的工具。
	CommandsEnabled bool `json:"commands_enabled"`
}

// Assembler 收集组件的贡献。提示词段落、工具与钩子按组件在列表中的顺序排列，与初始化顺序无关。
type Assembler struct {
	host     Host
	preset   *Resolved
	cur      int
	contribs []contribution

	rules        []policy.Rule
	checkedRules []policy.Rule
	sessionTools []string
	denyApproval bool
	closers      []func() error

	workspace     *coding.Workspace
	workspaceInfo WorkspaceInfo

	opts         buildOptions
	afterPublish []func(*tools.Pipeline) error
}

type contribution struct {
	info    map[string]string
	prompts []string
	tools   []interfaces.Tool
	pre     []tools.PreHook
	guards  []tools.Guard
	around  []tools.Around
	post    []tools.PostHook
	loop    Loop
}

func (a *Assembler) slot() *contribution { return &a.contribs[a.cur] }

// ComponentID 返回正在初始化的组件行的 id。
func (a *Assembler) ComponentID() string { return a.preset.Rows[a.cur].ID }

// SetInfo 在装配清单中记录本组件的运行时信息（如 MCP 服务的名称与版本），信息变化会改变清单摘要。
func (a *Assembler) SetInfo(key, value string) {
	if a.slot().info == nil {
		a.slot().info = map[string]string{}
	}
	a.slot().info[key] = value
}

// Root 返回规范化后的工作区根目录，未设置时为空串。
func (a *Assembler) Root() string { return a.host.Root }

// ArtifactDir 返回宿主产物目录。
func (a *Assembler) ArtifactDir() string { return a.host.ArtifactDir }

// AddPrompt 追加一个系统提示词段落。
func (a *Assembler) AddPrompt(text string) { a.slot().prompts = append(a.slot().prompts, text) }

// AddTools 追加工具；Build 结束时检查工具名全局唯一。
func (a *Assembler) AddTools(ts ...interfaces.Tool) { a.slot().tools = append(a.slot().tools, ts...) }

// AddPreHook 追加调用前钩子。
func (a *Assembler) AddPreHook(h tools.PreHook) { a.slot().pre = append(a.slot().pre, h) }

// AddGuard 追加守卫。
func (a *Assembler) AddGuard(g tools.Guard) { a.slot().guards = append(a.slot().guards, g) }

// AddAround 追加执行包装。
func (a *Assembler) AddAround(w tools.Around) { a.slot().around = append(a.slot().around, w) }

// AddPostHook 追加调用后钩子。
func (a *Assembler) AddPostHook(h tools.PostHook) { a.slot().post = append(a.slot().post, h) }

// AddRules 追加权限规则，全部组件的规则合并为一个策略（Deny > Ask > Allow）。
// checked 为真时 Build 检查规则引用的工具存在，用于用户书写的规则。
func (a *Assembler) AddRules(rules []policy.Rule, sessionTools []string, checked bool) {
	a.rules = append(a.rules, rules...)
	if checked {
		a.checkedRules = append(a.checkedRules, rules...)
	}
	a.sessionTools = append(a.sessionTools, sessionTools...)
}

// DenyApprovals 让全部 Ask 自动拒绝（approval: never）。
func (a *Assembler) DenyApprovals() { a.denyApproval = true }

// OnClose 登记 Assembly.Close 或 Build 失败时要关闭的资源，按登记的逆序关闭。
func (a *Assembler) OnClose(fn func() error) { a.closers = append(a.closers, fn) }

// SetWorkspace 由 workspace 组件调用。
func (a *Assembler) SetWorkspace(w *coding.Workspace, info WorkspaceInfo) error {
	if a.workspace != nil {
		return errors.New("工作区已经设置")
	}
	a.workspace, a.workspaceInfo = w, info
	return nil
}

// Workspace 返回 workspace 组件创建的工作区。
func (a *Assembler) Workspace() (*coding.Workspace, WorkspaceInfo, error) {
	if a.workspace == nil {
		return nil, WorkspaceInfo{}, errors.New("需要 workspace 组件")
	}
	return a.workspace, a.workspaceInfo, nil
}

// Secret 解析秘密引用，缺失时报错；错误信息只含名称。
func (a *Assembler) Secret(s Secret) (string, error) {
	v, ok := a.host.Secret(s.Name)
	if !ok {
		return "", fmt.Errorf("秘密 %s 未设置", s.Name)
	}
	return v, nil
}

// Value 解析字面量或秘密引用。
func (a *Assembler) Value(v Value) (string, error) {
	if v.Secret != nil {
		return a.Secret(*v.Secret)
	}
	return v.Literal, nil
}

// Assembly 是 Build 的结果，用 Configure 交给 Builder。
type Assembly struct {
	PresetID     string
	SystemPrompt string
	// Loop 是组件贡献的循环扩展点，只作用于原生工具流。
	Loop Loop
	// Tools 已经过 Dispatcher 与 Pipeline。
	Tools      []interfaces.Tool
	Dispatcher *tools.Dispatcher
	Pipeline   *tools.Pipeline
	// Workspace 在没有 workspace 组件时为 nil。
	Workspace *coding.Workspace
	Manifest  Manifest

	closeOnce sync.Once
	closers   []func() error
	closeErr  error
}

// Close 按初始化的逆序关闭资源，可重复调用。
func (a *Assembly) Close() error {
	a.closeOnce.Do(func() { a.closeErr = closeAll(a.closers) })
	return a.closeErr
}

func closeAll(closers []func() error) error {
	var errs []error
	for i := len(closers) - 1; i >= 0; i-- {
		errs = append(errs, closers[i]())
	}
	return errors.Join(errs...)
}

// Build 按依赖顺序初始化组件并装配。含未授权扩权项时返回 *TrustRequiredError；任一组件失败时已创建的资源按逆序关闭。
func Build(ctx context.Context, p *Resolved, host Host) (*Assembly, error) {
	return build(ctx, p, host, buildOptions{})
}

func build(ctx context.Context, p *Resolved, host Host, opts buildOptions) (asm *Assembly, err error) {
	if p == nil {
		return nil, errors.New("preset is required")
	}
	if host.Root != "" {
		abs, err := filepath.Abs(host.Root)
		if err != nil {
			return nil, err
		}
		if host.Root, err = filepath.EvalSymlinks(abs); err != nil {
			return nil, err
		}
	}
	if host.ArtifactDir == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("Host.ArtifactDir 未设置且无法取得用户缓存目录: %w", err)
		}
		host.ArtifactDir = filepath.Join(dir, "goagent", "artifacts")
	}
	if host.Secret == nil {
		host.Secret = os.LookupEnv
	}
	cfgs := make([]any, len(p.Rows))
	contentNeeds := make([]Needs, len(p.Rows))
	for i, r := range p.Rows {
		cfgs[i] = r.config
		if r.comp.resolve != nil {
			if cfgs[i], contentNeeds[i], err = r.comp.resolve(host.Root, r.config); err != nil {
				return nil, fmt.Errorf("组件 %s: %w", r.ID, err)
			}
		}
	}
	if expansions := p.expansions(cfgs, contentNeeds, host.Root); len(expansions) > 0 {
		digest, err := expansionDigest(host.Root, p, expansions, cfgs)
		if err != nil {
			return nil, err
		}
		trusted := false
		if host.Trust != nil {
			if trusted, err = host.Trust.IsTrusted(host.Root, digest); err != nil {
				return nil, err
			}
		}
		if !trusted {
			return nil, &TrustRequiredError{Preset: p.ID, Root: host.Root, Digest: digest, Items: expansions}
		}
	}
	order, err := buildOrder(p.Rows)
	if err != nil {
		return nil, err
	}
	if opts.filter != nil {
		for i, r := range p.Rows {
			if r.comp.filterTools == nil {
				continue
			}
			var keep bool
			if cfgs[i], keep = r.comp.filterTools(cfgs[i], r.ID, *opts.filter); !keep {
				cfgs[i] = nil
			}
		}
	}
	a := &Assembler{host: host, preset: p, contribs: make([]contribution, len(p.Rows)), opts: opts}
	defer func() {
		if err != nil {
			err = errors.Join(err, closeAll(a.closers))
		}
	}()
	for _, i := range order {
		// 过滤后不提供任何工具的组件不初始化，也就不启动进程。
		if cfgs[i] == nil {
			continue
		}
		a.cur = i
		if err := p.Rows[i].comp.build(ctx, a, cfgs[i]); err != nil {
			return nil, fmt.Errorf("组件 %s: %w", p.Rows[i].ID, err)
		}
	}
	return a.publish(cfgs)
}

// AfterPublish 登记在本 Agent 的 Pipeline 建成后执行的函数（如以它为父权限装配子 Agent），失败时 Build 失败。
func (a *Assembler) AfterPublish(fn func(parent *tools.Pipeline) error) {
	a.afterPublish = append(a.afterPublish, fn)
}

// Depth 返回嵌套深度：顶层 Agent 为 0，其子 Agent 为 1。
func (a *Assembler) Depth() int { return a.opts.depth }

// buildOrder 返回初始化顺序：被依赖的组件先于依赖它的组件，其余保持列表顺序。
func buildOrder(rows []*Row) ([]int, error) {
	state := make([]int, len(rows)) // 0 未访问，1 访问中，2 完成
	var order []int
	var visit func(int) error
	visit = func(i int) error {
		switch state[i] {
		case 1:
			return fmt.Errorf("组件 %s 的依赖有环", rows[i].ID)
		case 2:
			return nil
		}
		state[i] = 1
		for _, req := range rows[i].comp.requires {
			for j, r := range rows {
				if r.Use == req {
					if err := visit(j); err != nil {
						return err
					}
				}
			}
		}
		state[i] = 2
		order = append(order, i)
		return nil
	}
	for i := range rows {
		if err := visit(i); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func (a *Assembler) publish(cfgs []any) (*Assembly, error) {
	var (
		prompts  []string
		loop     Loop
		all      []interfaces.Tool
		pipeline = &tools.Pipeline{Approver: a.host.Approver}
	)
	for _, c := range a.contribs {
		prompts = append(prompts, c.prompts...)
		all = append(all, c.tools...)
		pipeline.Pre = append(pipeline.Pre, c.pre...)
		pipeline.Guards = append(pipeline.Guards, c.guards...)
		pipeline.Around = append(pipeline.Around, c.around...)
		pipeline.Post = append(pipeline.Post, c.post...)
		loop.add(c.loop)
	}
	registry := tools.NewRegistry()
	names := map[string]bool{}
	for _, t := range all {
		if names[t.Name()] {
			return nil, fmt.Errorf("工具名 %s 重复", t.Name())
		}
		names[t.Name()] = true
		if err := registry.Register(t); err != nil {
			return nil, err
		}
	}
	// 作为子 Agent 时，被过滤掉的工具也可能出现在目标 preset 自己的规则里，因此只在顶层检查。
	if a.opts.filter == nil {
		for _, r := range a.checkedRules {
			if r.Tool != "*" && !names[r.Tool] {
				return nil, fmt.Errorf("权限规则引用的工具 %s 不存在", r.Tool)
			}
		}
	}
	if f := a.opts.filter; f != nil {
		for _, name := range f.Allow {
			if !names[name] {
				return nil, fmt.Errorf("工具过滤引用的工具 %s 不存在", name)
			}
		}
		registry = tools.NewRegistry()
		kept := all[:0:0]
		for _, t := range all {
			// 溢出结果须能被读取，read_spill 随 spill 组件保留。
			if f.Keeps(t.Name()) || t.Name() == spill.ToolReadSpill {
				kept = append(kept, t)
				if err := registry.Register(t); err != nil {
					return nil, err
				}
			}
		}
		all = kept
	}
	// 宿主基线：没有任何规则匹配的调用需要审批，不会因漏配权限组件而全放行。
	pol := &policy.Policy{Rules: a.rules, Default: policy.Ask, SessionTools: a.sessionTools}
	pipeline.Pre = append([]tools.PreHook{pol.Hook()}, pipeline.Pre...)
	if parent := a.opts.parent; parent != nil {
		pipeline.Pre = append(slices.Clone(parent.pre), pipeline.Pre...)
		pipeline.Guards = append(slices.Clone(parent.guards), pipeline.Guards...)
		a.denyApproval = a.denyApproval || parent.denyApproval
		if parent.sandbox != "" && a.workspace != nil && a.workspaceInfo.Sandbox.rank() > parent.sandbox.rank() {
			return nil, fmt.Errorf("子 Agent 的沙箱模式 %s 宽于父 Agent 的 %s", a.workspaceInfo.Sandbox, parent.sandbox)
		}
	}
	if a.denyApproval {
		pipeline.Approver = tools.ApproverFunc(func(context.Context, tools.ApprovalRequest) (tools.ApprovalResult, error) {
			return tools.ApprovalResult{Outcome: tools.ApprovalRejected, Feedback: "approval policy is never"}, nil
		})
	}
	cfg := a.host.Dispatch
	cfg.Pipeline = pipeline
	cfg.ReturnArgumentErrors = true
	d, err := tools.NewDispatcher(registry, cfg)
	if err != nil {
		return nil, err
	}
	for _, fn := range a.afterPublish {
		if err := fn(pipeline); err != nil {
			return nil, err
		}
	}
	asm := &Assembly{
		PresetID:     a.preset.ID,
		SystemPrompt: strings.Join(prompts, "\n\n"),
		Loop:         loop,
		Tools:        d.Tools(),
		Dispatcher:   d,
		Pipeline:     pipeline,
		Workspace:    a.workspace,
		closers:      a.closers,
	}
	kept := map[string]bool{}
	for _, t := range all {
		kept[t.Name()] = true
	}
	if asm.Manifest, err = a.manifest(asm.SystemPrompt, cfgs, kept); err != nil {
		return nil, err
	}
	return asm, nil
}

// Manifest 是解析后执行语义的规范化描述。持久运行可记录 Digest，恢复时不一致即拒绝。秘密只出现引用。
type Manifest struct {
	Preset       string              `json:"preset"`
	Components   []ManifestComponent `json:"components"`
	Tools        []ManifestTool      `json:"tools"`
	PromptSHA256 string              `json:"prompt_sha256"`
	Workspace    *WorkspaceInfo      `json:"workspace,omitempty"`
}

// ManifestComponent 是装配清单中的一个组件。
type ManifestComponent struct {
	ID      string `json:"id"`
	Use     string `json:"use"`
	Version string `json:"version"`
	Config  any    `json:"config,omitempty"`
	// Info 是组件在初始化时记录的运行时信息。
	Info map[string]string `json:"info,omitempty"`
}

// ManifestTool 是装配清单中的一个工具，Component 是提供它的组件行。
type ManifestTool struct {
	Name              string `json:"name"`
	Component         string `json:"component"`
	SchemaSHA256      string `json:"schema_sha256"`
	DescriptionSHA256 string `json:"description_sha256"`
}

// Digest 返回清单 JSON 的 SHA-256。
func (m Manifest) Digest() string {
	data, _ := json.Marshal(m)
	return sha256Hex(string(data))
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// manifest 记录各行的有效配置（含默认值与 Resolve 补全的内容；被工具过滤跳过的行为 null）与保留下来的工具。
func (a *Assembler) manifest(prompt string, cfgs []any, kept map[string]bool) (Manifest, error) {
	m := Manifest{Preset: a.preset.ID, PromptSHA256: sha256Hex(prompt)}
	for i, r := range a.preset.Rows {
		m.Components = append(m.Components, ManifestComponent{ID: r.ID, Use: r.Use, Version: r.comp.version, Config: cfgs[i], Info: a.contribs[i].info})
		for _, t := range a.contribs[i].tools {
			if !kept[t.Name()] {
				continue
			}
			m.Tools = append(m.Tools, ManifestTool{Name: t.Name(), Component: r.ID, SchemaSHA256: sha256Hex(t.ArgsSchema()), DescriptionSHA256: sha256Hex(t.Description())})
		}
	}
	if a.workspace != nil {
		info := a.workspaceInfo
		m.Workspace = &info
	}
	return m, nil
}

// rawJSON 把一行的原始配置转换为可 JSON 编码的值；未写配置时为 nil。
func rawJSON(r *Row) (any, error) {
	if r.raw == nil || r.raw.Kind == 0 {
		return nil, nil
	}
	var v any
	if err := r.raw.Decode(&v); err != nil {
		return nil, fmt.Errorf("组件 %s: %w", r.ID, err)
	}
	return v, nil
}

// ErrManifestChanged 表示恢复持久运行时，装配清单与运行开始时记录的不一致。
var ErrManifestChanged = errors.New("装配清单与持久运行开始时记录的不一致")

const manifestOperation = "preset/manifest"

// PinManifest 在持久运行中固定装配清单：首次运行把摘要写入日志，恢复时比对，不一致返回 ErrManifestChanged，
// 由宿主决定新建会话。ctx 须是交给 Agent 的同一个 ctx（带日志与执行键），在调用 Agent 之前执行；没有日志时什么也不做。
// 摘要只保证执行语义一致，外部副作用仍依赖各工具的核对。
func (a *Assembly) PinManifest(ctx context.Context) error {
	if !durable.HasJournal(ctx) {
		return nil
	}
	if durable.ExecutionKey(ctx) == "" {
		return errors.New("持久运行缺少执行键")
	}
	digest := a.Manifest.Digest()
	record, err := durable.InspectOperation(ctx, manifestOperation)
	if err == nil && record.Status == "completed" {
		var pinned string
		if err := json.Unmarshal(record.Value, &pinned); err != nil {
			return fmt.Errorf("日志中的装配清单摘要无效: %w", err)
		}
		if pinned != digest {
			return fmt.Errorf("%w：运行开始时为 %s，当前为 %s", ErrManifestChanged, pinned, digest)
		}
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	value, err := json.Marshal(digest)
	if err != nil {
		return err
	}
	_, err = durable.Do(ctx, manifestOperation, json.RawMessage(`"goagent-preset-manifest-v1"`), durable.OperationPolicy{MaxAttempts: 3, RetryFailed: true},
		func(context.Context) (json.RawMessage, error) { return value, nil })
	return err
}
