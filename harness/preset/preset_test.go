package preset

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

var probeLog struct {
	sync.Mutex
	events []string
}

func logEvent(e string) {
	probeLog.Lock()
	defer probeLog.Unlock()
	probeLog.events = append(probeLog.events, e)
}

func takeEvents() []string {
	probeLog.Lock()
	defer probeLog.Unlock()
	out := probeLog.events
	probeLog.events = nil
	return out
}

type probeConfig struct {
	Name    string  `yaml:"name"`
	Fail    bool    `yaml:"fail"`
	Token   *Secret `yaml:"token"`
	Process string  `yaml:"process"`
}

func init() {
	Register(Spec[probeConfig]{
		Name: "test-probe", Version: "1",
		Needs: func(c *probeConfig) Needs {
			if c.Process != "" {
				return Needs{HostProcesses: []string{c.Process}}
			}
			return Needs{}
		},
		Build: func(_ context.Context, a *Assembler, c *probeConfig) error {
			logEvent("build " + c.Name)
			a.OnClose(func() error { logEvent("close " + c.Name); return nil })
			if c.Token != nil {
				v, err := a.Secret(*c.Token)
				if err != nil {
					return err
				}
				logEvent("secret " + v)
			}
			if c.Fail {
				return errors.New("boom")
			}
			a.AddPrompt(c.Name)
			return nil
		},
	})
}

func trusted(data string) Source {
	return Source{Layer: LayerHost, Path: "host", Trusted: true, Data: []byte(data)}
}

func project(data string) Source {
	return Source{Layer: LayerProject, Path: "project", Data: []byte(data)}
}

func mustGet(t *testing.T, sources ...Source) *Resolved {
	t.Helper()
	set, err := Load(sources...)
	require.NoError(t, err)
	p, ok := set.Get("p")
	require.True(t, ok)
	return p
}

func rowIDs(p *Resolved) []string {
	ids := make([]string, len(p.Rows))
	for i, r := range p.Rows {
		ids[i] = r.ID
	}
	return ids
}

func TestMergeSemantics(t *testing.T) {
	base := trusted(`
presets:
  - id: p
    name: base
    description: first
    components:
      - {id: a, use: test-probe, config: {name: a}}
      - {id: b, use: test-probe, config: {name: b}}
      - {id: c, use: test-probe, config: {name: c}}
`)
	p := mustGet(t, base, trusted(`
presets:
  - id: p
    name: changed
    components:
      - {id: b, use: test-probe, config: {name: b2}}
      - {id: c, remove: true}
      - {id: d, use: test-probe}
`), trusted(`
presets:
  - id: p
    description: ""
    components:
      - {id: c, use: test-probe, config: {name: c2}}
`))
	require.Equal(t, []string{"a", "b", "d", "c"}, rowIDs(p), "replacement keeps position; re-added and new rows append")
	require.Equal(t, "changed", p.Name)
	require.Empty(t, p.Description)
	require.Len(t, p.Rows[1].Overrides, 1)
	require.Equal(t, "b2", p.Rows[1].config.(*probeConfig).Name)
	require.Len(t, p.Removed, 1)
	require.Equal(t, "c", p.Removed[0].ID)

	set, err := Load(base, trusted("presets: [{id: p, remove: true}]"), trusted("presets: [{id: p, components: [{id: x, use: test-probe}]}]"))
	require.NoError(t, err)
	p, _ = set.Get("p")
	require.Equal(t, []string{"x"}, rowIDs(p), "a removed preset is recreated from scratch")

	p = mustGet(t, base, trusted("presets: [{id: p, components: [{id: a, use: persona, replace: true, config: {prompt: hi}}]}]"))
	require.Equal(t, "persona", p.Rows[0].Use)

	for name, src := range map[string]Source{
		"duplicate component":    trusted("presets: [{id: p, components: [{id: a, use: test-probe}, {id: a, use: test-probe}]}]"),
		"duplicate preset":       trusted("presets: [{id: p}, {id: p}]"),
		"use change":             trusted("presets: [{id: p, components: [{id: a, use: persona}]}]"),
		"replace missing":        trusted("presets: [{id: p, components: [{id: z, use: test-probe, replace: true}]}]"),
		"remove missing":         trusted("presets: [{id: p, components: [{id: z, remove: true}]}]"),
		"remove missing preset":  trusted("presets: [{id: q, remove: true}]"),
		"unknown top field":      trusted("preset: []"),
		"unknown config field":   trusted("presets: [{id: p, components: [{id: a, use: test-probe, config: {nmae: x}}]}]"),
		"unknown component":      trusted("presets: [{id: p, components: [{id: z, use: nope}]}]"),
		"limits in project":      project("limits: {network: true}"),
		"locked by project":      project("presets: [{id: p, components: [{id: z, use: test-probe, locked: true}]}]"),
		"empty secret reference": trusted("presets: [{id: p, components: [{id: z, use: test-probe, config: {token: {secret: ''}}}]}]"),
	} {
		_, err := Load(base, src)
		require.Error(t, err, name)
	}

	locked := trusted("presets: [{id: p, components: [{id: a, use: test-probe, locked: true}]}]")
	for name, src := range map[string]Source{
		"replace locked":       project("presets: [{id: p, components: [{id: a, use: test-probe}]}]"),
		"remove locked":        project("presets: [{id: p, components: [{id: a, remove: true}]}]"),
		"remove locked preset": project("presets: [{id: p, remove: true}]"),
	} {
		_, err := Load(base, locked, src)
		require.ErrorContains(t, err, "锁定", name)
	}

	for name, data := range map[string]string{
		"singleton": `presets: [{id: p, components: [{id: w1, use: workspace, config: {image: x}}, {id: w2, use: workspace, config: {image: x}}]}]`,
		"requires":  `presets: [{id: p, components: [{id: c, use: coding-tools}]}]`,
	} {
		_, err := Load(trusted(data))
		require.Error(t, err, name)
	}
}

func TestTrustModel(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	base := trusted(`
presets:
  - id: p
    components:
      - {id: perms, use: permissions, config: {base: coding}}
      - {id: a, use: test-probe, config: {name: a}}
`)
	build := func(p *Resolved, trust TrustStore) error {
		asm, err := Build(ctx, p, Host{Root: root, Trust: trust, ArtifactDir: t.TempDir(), Secret: func(n string) (string, bool) { return "value-of-" + n, n == "TOKEN" }})
		if err == nil {
			require.NoError(t, asm.Close())
		}
		return err
	}

	// 收紧无需授权。
	require.Empty(t, mustGet(t, base, project(`presets: [{id: p, components: [{id: mine, use: permissions, config: {rules: [{tool: run_command, decision: deny}]}}]}]`)).Expansions)

	cases := map[string]string{
		"allow rule":      `presets: [{id: p, components: [{id: mine, use: permissions, config: {rules: [{tool: "*", command: go test, decision: allow}]}}]}]`,
		"replace trusted": `presets: [{id: p, components: [{id: perms, use: permissions, config: {}}]}]`,
		"remove trusted":  `presets: [{id: p, components: [{id: perms, remove: true}]}]`,
		"host process":    `presets: [{id: p, components: [{id: m, use: test-probe, config: {process: github-mcp}}]}]`,
		"secret":          `presets: [{id: p, components: [{id: m, use: test-probe, config: {token: {secret: TOKEN}}}]}]`,
		"sandbox":         `presets: [{id: p, components: [{id: w, use: workspace, config: {sandbox: danger-full-access, executor: local, network: true}}]}]`,
	}
	for name, data := range cases {
		takeEvents()
		p := mustGet(t, base, project(data))
		require.NotEmpty(t, p.Expansions, name)
		require.Empty(t, takeEvents(), "Load must not build components: %s", name)

		store := &MemoryTrustStore{}
		err := build(p, store)
		var trustErr *TrustRequiredError
		require.ErrorAs(t, err, &trustErr, name)
		require.Empty(t, takeEvents(), "untrusted preset must not build: %s", name)
		require.NoError(t, store.Grant(trustErr.Root, trustErr.Digest))
		require.NoError(t, build(p, store), name)
	}

	// 配置变化后授权失效。
	store := &MemoryTrustStore{}
	p := mustGet(t, base, project(cases["allow rule"]))
	var trustErr *TrustRequiredError
	require.ErrorAs(t, build(p, store), &trustErr)
	require.NoError(t, store.Grant(trustErr.Root, trustErr.Digest))
	p = mustGet(t, base, project(strings.Replace(cases["allow rule"], "go test", "make", 1)))
	require.ErrorAs(t, build(p, store), &trustErr)

	// 可信层上限内的能力无需授权；可信层重新加入被删除的组件后不再算作删除。
	limits := trusted("limits: {hostProcesses: [github-mcp], secrets: [TOKEN]}")
	require.Empty(t, mustGet(t, limits, base, project(cases["host process"])).Expansions)
	require.Empty(t, mustGet(t, limits, base, project(cases["secret"])).Expansions)
	require.Empty(t, mustGet(t, base, project(cases["remove trusted"]), trusted(`presets: [{id: p, components: [{id: perms, use: permissions}]}]`)).Expansions)

	// 秘密在 Build 时解析，缺失时报错且只含名称。
	takeEvents()
	require.NoError(t, build(mustGet(t, limits, base, project(cases["secret"])), nil))
	require.Contains(t, takeEvents(), "secret value-of-TOKEN")
	missing := mustGet(t, trusted(`presets: [{id: p, components: [{id: m, use: test-probe, config: {token: {secret: OTHER}}}]}]`))
	require.ErrorContains(t, build(missing, nil), "秘密 OTHER 未设置")
}

func TestBuildFailureClosesResources(t *testing.T) {
	takeEvents()
	p := mustGet(t, trusted(`presets: [{id: p, components: [{id: a, use: test-probe, config: {name: a}}, {id: b, use: test-probe, config: {name: b, fail: true}}]}]`))
	_, err := Build(context.Background(), p, Host{ArtifactDir: t.TempDir()})
	require.ErrorContains(t, err, "组件 b")
	require.Equal(t, []string{"build a", "build b", "close b", "close a"}, takeEvents())

	p = mustGet(t, trusted(`presets: [{id: p, components: [{id: a, use: test-probe, config: {name: a}}]}]`))
	asm, err := Build(context.Background(), p, Host{ArtifactDir: t.TempDir()})
	require.NoError(t, err)
	require.NoError(t, asm.Close())
	require.NoError(t, asm.Close())
	require.Equal(t, []string{"build a", "close a"}, takeEvents())
}

func workspacePreset(cfg, tools string) Source {
	return trusted(`presets: [{id: p, components: [{id: w, use: workspace, config: ` + cfg + `}, {id: c, use: coding-tools, config: {tools: ` + tools + `}}]}]`)
}

func toolNames(asm *Assembly) []string {
	names := make([]string, len(asm.Tools))
	for i, t := range asm.Tools {
		names[i] = t.Name()
	}
	return names
}

func TestSandboxEnforcement(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	host := Host{Root: root, ArtifactDir: t.TempDir()}
	build := func(src Source) (*Assembly, error) { return Build(ctx, mustGet(t, src), host) }

	_, err := build(workspacePreset("{executor: local}", "[]"))
	require.ErrorContains(t, err, "要求在容器中执行命令")
	_, err = build(workspacePreset("{sandbox: danger-full-access, executor: local}", "[]"))
	require.ErrorContains(t, err, "网络能力")

	asm, err := build(workspacePreset("{sandbox: danger-full-access, executor: local, network: true}", "[]"))
	require.NoError(t, err)
	require.Contains(t, toolNames(asm), "run_command")

	// 只读模式 + 本地执行器：既不提供写入类工具，也不提供执行命令的工具。
	asm, err = build(workspacePreset("{sandbox: read-only, executor: local}", "[]"))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"read_file", "glob", "grep", "todo_write"}, toolNames(asm))
	_, err = build(workspacePreset("{sandbox: read-only, executor: local}", "[write_file]"))
	require.ErrorContains(t, err, "只读模式")
	_, err = build(workspacePreset("{sandbox: read-only, executor: local}", "[run_command]"))
	require.ErrorContains(t, err, "只读方式挂载")

	// 只读模式 + 只读挂载的容器：可以执行命令，但仍不提供写入类工具。
	asm, err = build(workspacePreset("{sandbox: read-only, image: alpine:3}", "[]"))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"read_file", "glob", "grep", "run_command", "git_status", "git_diff", "git_log", "todo_write"}, toolNames(asm))
	require.True(t, asm.Manifest.Workspace.CommandsEnabled)
}

func TestAssembly(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("用中文提交"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello\n"), 0o644))
	host := `
presets:
  - id: p
    components:
      - {id: persona, use: persona, config: {prompt: 你是编码助手}}
      - {id: w, use: workspace, config: {sandbox: danger-full-access, executor: local, network: true}}
      - {id: instructions, use: instructions}
      - {id: tools, use: coding-tools}
      - {id: perms, use: permissions, config: {base: coding, rules: [{tool: run_command, command: rm, decision: deny}]}}
      - {id: spill, use: spill, config: {maxBytes: 50}}
`
	var asked atomic.Int32
	h := Host{Root: root, ArtifactDir: t.TempDir(), Approver: tools.ApproverFunc(func(context.Context, tools.ApprovalRequest) (tools.ApprovalResult, error) {
		asked.Add(1)
		return tools.ApprovalResult{Outcome: tools.ApprovalAllowedOnce}, nil
	})}
	asm, err := Build(ctx, mustGet(t, trusted(host)), h)
	require.NoError(t, err)
	defer func() { require.NoError(t, asm.Close()) }()
	require.Equal(t, "你是编码助手\n\n项目说明：\n<AGENTS.md>\n用中文提交\n</AGENTS.md>\n", asm.SystemPrompt)
	require.Contains(t, toolNames(asm), "read_spill")

	byName := map[string]interfaces.Tool{}
	for _, tool := range asm.Tools {
		byName[tool.Name()] = tool
	}
	invoke := func(name string, args map[string]any) *interfaces.ToolOutput {
		out, err := byName[name].Invoke(ctx, &interfaces.ToolInput{Args: args})
		require.NoError(t, err)
		return out
	}
	require.True(t, invoke("read_file", map[string]any{"path": "a.txt"}).Success)
	require.Zero(t, asked.Load(), "coding base rules allow reads")
	_, rejected := tools.RejectedResult(invoke("run_command", map[string]any{"command": "rm a.txt"}))
	require.True(t, rejected)
	out := invoke("run_command", map[string]any{"command": "seq 1 100"})
	require.EqualValues(t, 1, asked.Load())
	spilled := out.Result.(map[string]any)
	require.Equal(t, true, spilled["truncated"], "large results are spilled to the artifact dir")
	require.True(t, invoke("read_spill", map[string]any{"path": spilled["spill_path"]}).Success)
	_, err = os.Stat(filepath.Join(root, ".goagent"))
	require.True(t, os.IsNotExist(err), "spill files never land in the workspace")

	// 清单稳定，配置变化时摘要变化。
	again, err := Build(ctx, mustGet(t, trusted(host)), h)
	require.NoError(t, err)
	require.Equal(t, asm.Manifest.Digest(), again.Manifest.Digest())
	require.NoError(t, again.Close())
	changed, err := Build(ctx, mustGet(t, trusted(strings.Replace(host, "maxBytes: 50", "maxBytes: 60", 1))), h)
	require.NoError(t, err)
	require.NotEqual(t, asm.Manifest.Digest(), changed.Manifest.Digest())
	require.NoError(t, changed.Close())

	// 没有权限组件时一律需要审批。
	bare, err := Build(ctx, mustGet(t, workspacePreset("{sandbox: danger-full-access, executor: local, network: true}", "[read_file]")), Host{Root: root, ArtifactDir: t.TempDir()})
	require.NoError(t, err)
	out, err = bare.Tools[0].Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"path": "a.txt"}})
	require.NoError(t, err)
	msg, rejected := tools.RejectedResult(out)
	require.True(t, rejected)
	require.Contains(t, msg, "no approver")
	require.NoError(t, bare.Close())

	_, err = Build(ctx, mustGet(t, trusted(strings.Replace(host, "tool: run_command, command: rm", "tool: nope", 1))), h)
	require.ErrorContains(t, err, "工具 nope 不存在")
}

func TestApprovalNever(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	src := trusted(`presets: [{id: p, components: [
  {id: w, use: workspace, config: {sandbox: danger-full-access, executor: local, network: true}},
  {id: c, use: coding-tools, config: {tools: [run_command]}},
  {id: perms, use: permissions, config: {approval: never}}]}]`)
	asm, err := Build(ctx, mustGet(t, src), Host{Root: root, ArtifactDir: t.TempDir(), Approver: tools.ApproverFunc(func(context.Context, tools.ApprovalRequest) (tools.ApprovalResult, error) {
		t.Fatal("approver must not be asked")
		return tools.ApprovalResult{}, nil
	})})
	require.NoError(t, err)
	defer func() { require.NoError(t, asm.Close()) }()
	out, err := asm.Tools[0].Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"command": "echo hi"}})
	require.NoError(t, err)
	msg, rejected := tools.RejectedResult(out)
	require.True(t, rejected)
	require.Contains(t, msg, "never")
}

func TestFileSourcesAndDump(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	write := func(dir, name, data string) {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".goagent"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".goagent", name), []byte(data), 0o644))
	}
	write(home, "agents.yml", "limits: {secrets: [TOKEN]}\npresets: [{id: p, components: [{id: a, use: test-probe, locked: true}, {id: b, use: test-probe}]}]")
	write(root, "agents.yml", "presets: [{id: p, components: [{id: m, use: test-probe, config: {token: {secret: TOKEN}}}, {id: b, remove: true}]}]")
	sources, err := FileSources(root, home)
	require.NoError(t, err)
	require.Len(t, sources, 2, "missing agents.local.yml is skipped")
	require.True(t, sources[0].Trusted)
	require.False(t, sources[1].Trusted)

	set, err := Load(sources...)
	require.NoError(t, err)
	dump := set.Dump()
	for _, want := range []string{"secrets=[TOKEN]", "a [test-probe] 来源 user", "已锁定", `{"token":{"secret":"TOKEN"}}`, "（已删除）b", "删除来自可信层的组件"} {
		require.Contains(t, dump, want)
	}

	store := &FileTrustStore{Path: filepath.Join(home, ".goagent", "trust.json")}
	ok, err := store.IsTrusted(root, "d")
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, store.Grant(root, "d"))
	ok, err = (&FileTrustStore{Path: store.Path}).IsTrusted(root, "d")
	require.NoError(t, err)
	require.True(t, ok)
}
