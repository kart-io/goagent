package preset

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

const localWorkspace = `{id: w, use: workspace, config: {sandbox: danger-full-access, executor: local, network: true}}`

// approvals 记录审批请求并按 outcome 答复。
type approvals struct {
	reasons []string
	outcome tools.ApprovalOutcome
}

func (a *approvals) Approve(_ context.Context, req tools.ApprovalRequest) (tools.ApprovalResult, error) {
	a.reasons = append(a.reasons, req.Reason)
	return tools.ApprovalResult{Outcome: a.outcome}, nil
}

func invokeTool(t *testing.T, asm *Assembly, name string, args map[string]any) *interfaces.ToolOutput {
	t.Helper()
	tool, ok := byToolName(asm)[name]
	require.True(t, ok, "tool %s not found in %v", name, toolNames(asm))
	out, err := tool.Invoke(context.Background(), &interfaces.ToolInput{Args: args})
	require.NoError(t, err)
	return out
}

func requireRejected(t *testing.T, out *interfaces.ToolOutput, contains string) {
	t.Helper()
	msg, ok := tools.RejectedResult(out)
	require.True(t, ok, "expected rejection, got %+v", out)
	require.Contains(t, msg, contains)
}

func hooksPreset(hooks string) Source {
	return trusted(`presets: [{id: p, components: [` + localWorkspace + `,
  {id: c, use: coding-tools},
  {id: perms, use: permissions, config: {base: coding}},
  {id: hooks, use: claude-code-hooks, config: ` + hooks + `}]}]`)
}

func TestClaudeCodeHooks(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("x\n"), 0o644))
	capture := filepath.Join(t.TempDir(), "stdin.json")
	hooks := `{hooks: {
  PreToolUse: [
    {matcher: Bash, hooks: [{type: command, command: 'input=$(cat); case "$input" in *"rm "*) echo "no rm" >&2; exit 2;; esac'}]},
    {matcher: Bash, hooks: [{type: command, command: 'grep -q confirm && echo ''{"hookSpecificOutput":{"permissionDecision":"ask","permissionDecisionReason":"please confirm"}}'' || true'}]},
    {matcher: "Read|Write", hooks: [{type: command, command: 'cat > ` + capture + `; echo ''{"decision":"approve"}'''}]},
    {matcher: Bash, hooks: [{type: command, command: 'exit 1'}, {type: command, command: 'sleep 3', timeout: 1}]}],
  PostToolUse: [
    {matcher: Read, hooks: [{type: command, command: 'echo ''{"decision":"block","reason":"lint failed","hookSpecificOutput":{"additionalContext":"see docs"}}'''}]}]}}`
	approver := &approvals{outcome: tools.ApprovalAllowedOnce}
	asm, err := Build(context.Background(), mustGet(t, hooksPreset(hooks)), Host{Root: root, ArtifactDir: t.TempDir(), Approver: approver})
	require.NoError(t, err)
	defer func() { require.NoError(t, asm.Close()) }()

	// 退出码 2 阻止，stderr 交给模型；失败与超时的钩子不阻止。
	requireRejected(t, invokeTool(t, asm, "run_command", map[string]any{"command": "rm a.txt"}), "no rm")
	start := time.Now()
	require.True(t, invokeTool(t, asm, "run_command", map[string]any{"command": "echo hi"}).Success)
	require.Less(t, time.Since(start), 3*time.Second, "hook timeout is enforced")
	require.Len(t, approver.reasons, 1, "policy still asks for commands; hook allow does not pre-approve")

	// 钩子的 ask 与策略的 ask 合并为一次审批。
	require.True(t, invokeTool(t, asm, "run_command", map[string]any{"command": "echo confirm"}).Success)
	require.Len(t, approver.reasons, 2)
	require.Contains(t, approver.reasons[1], "please confirm")

	// stdin 使用 Claude Code 的工具名与 file_path；调用后钩子的反馈作为附加上下文。
	out := invokeTool(t, asm, "read_file", map[string]any{"path": "a.txt"})
	require.True(t, out.Success)
	require.Equal(t, []string{"lint failed", "see docs"}, out.AdditionalContext)
	var stdin map[string]any
	data, err := os.ReadFile(capture)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &stdin))
	require.Equal(t, "Read", stdin["tool_name"])
	require.Equal(t, "PreToolUse", stdin["hook_event_name"])
	require.Equal(t, "a.txt", stdin["tool_input"].(map[string]any)["file_path"])
	require.Len(t, approver.reasons, 2, "reads stay allowed by the coding base rules")

	for name, cfg := range map[string]string{
		"unsupported event": `{hooks: {Stop: [{hooks: [{type: command, command: x}]}]}}`,
		"unsupported type":  `{hooks: {PreToolUse: [{hooks: [{type: prompt, command: x}]}]}}`,
		"bad matcher":       `{hooks: {PreToolUse: [{matcher: "(", hooks: [{type: command, command: x}]}]}}`,
		"empty":             `{}`,
	} {
		_, err := Load(hooksPreset(cfg))
		require.Error(t, err, name)
	}
}

// 工作区内的钩子文件属于不可信内容：即使组件行来自可信层，其中的命令也需要授权，文件变化后授权失效。
func TestClaudeCodeHooksFileTrust(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".claude"), 0o755))
	settings := filepath.Join(root, ".claude", "settings.json")
	write := func(command string) {
		data, err := json.Marshal(map[string]any{"hooks": map[string]any{
			"PreToolUse": []any{map[string]any{"matcher": "Bash", "hooks": []any{map[string]any{"type": "command", "command": command}}}},
			"Stop":       []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "ignored"}}}},
		}})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(settings, data, 0o644))
	}
	write("echo blocked >&2; exit 2")
	store := &MemoryTrustStore{}
	host := Host{Root: root, ArtifactDir: t.TempDir(), Trust: store}
	p := mustGet(t, hooksPreset(`{path: .claude/settings.json}`))
	require.Empty(t, p.Expansions, "the row itself is trusted")

	_, err := Build(context.Background(), p, host)
	var trustErr *TrustRequiredError
	require.ErrorAs(t, err, &trustErr)
	require.Len(t, trustErr.Items, 1)
	require.Equal(t, "workspace", trustErr.Items[0].Origin.Layer)
	require.Contains(t, trustErr.Items[0].Reason, "echo blocked")
	require.NoError(t, store.Grant(trustErr.Root, trustErr.Digest))
	asm, err := Build(context.Background(), p, host)
	require.NoError(t, err)
	requireRejected(t, invokeTool(t, asm, "run_command", map[string]any{"command": "ls"}), "blocked")
	require.NoError(t, asm.Close())

	write("echo changed >&2; exit 2")
	_, err = Build(context.Background(), p, host)
	require.ErrorAs(t, err, &trustErr, "a changed hook file needs a new grant")

	limited := mustGet(t, trusted(`limits: {hostProcesses: ["echo changed >&2; exit 2"]}`), hooksPreset(`{path: .claude/settings.json}`))
	asm, err = Build(context.Background(), limited, Host{Root: root, ArtifactDir: t.TempDir()})
	require.NoError(t, err)
	require.NoError(t, asm.Close())

	// 来自项目层的内联钩子同样是扩权项。
	inline := mustGet(t, trusted(`presets: [{id: p}]`), project(`presets: [{id: p, components: [{id: h, use: claude-code-hooks, config: {hooks: {PreToolUse: [{hooks: [{type: command, command: "true"}]}]}}}]}]`))
	require.Len(t, inline.Expansions, 1)
}

func writeSkill(t *testing.T, dir, name, front, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, name), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name, "SKILL.md"), []byte("---\n"+front+"\n---\n"+body), 0o644))
}

func TestSkills(t *testing.T) {
	root := t.TempDir()
	agents, claude := filepath.Join(root, ".agents", "skills"), filepath.Join(root, ".claude", "skills")
	writeSkill(t, agents, "pdf", "name: pdf\ndescription: 处理 PDF", "先读 ref.md")
	require.NoError(t, os.WriteFile(filepath.Join(agents, "pdf", "ref.md"), []byte("参考内容"), 0o644))
	writeSkill(t, claude, "pdf", "name: pdf\ndescription: 被覆盖", "x")
	writeSkill(t, claude, "secret-op", "name: secret-op\ndescription: 隐藏\ndisable-model-invocation: true", "y")
	src := trusted(`presets: [{id: p, components: [{id: s, use: skills}]}]`)
	asm, err := Build(context.Background(), mustGet(t, src), Host{Root: root, ArtifactDir: t.TempDir()})
	require.NoError(t, err)
	defer func() { require.NoError(t, asm.Close()) }()
	require.Contains(t, asm.SystemPrompt, "- pdf：处理 PDF")
	require.NotContains(t, asm.SystemPrompt, "被覆盖")
	require.NotContains(t, asm.SystemPrompt, "secret-op")

	out := invokeTool(t, asm, ToolLoadSkill, map[string]any{"name": "pdf"})
	require.Equal(t, "先读 ref.md", out.Result.(map[string]any)["instructions"])
	require.Equal(t, []string{"ref.md"}, out.Result.(map[string]any)["files"])
	out = invokeTool(t, asm, ToolLoadSkill, map[string]any{"name": "pdf", "file": "ref.md"})
	require.Equal(t, "参考内容", out.Result.(map[string]any)["content"])
	requireRejected(t, invokeTool(t, asm, ToolLoadSkill, map[string]any{"name": "pdf", "file": "../../../etc/passwd"}), "has no file")
	requireRejected(t, invokeTool(t, asm, ToolLoadSkill, map[string]any{"name": "nope"}), "unknown skill")
	require.Contains(t, asm.Manifest.Components[0].Info["skills"], "pdf@")

	writeSkill(t, agents, "broken", "name: Broken\ndescription: x", "")
	_, err = Build(context.Background(), mustGet(t, src), Host{Root: root, ArtifactDir: t.TempDir()})
	require.ErrorContains(t, err, "小写字母")

	// 不可信层读取工作区外的技能目录需要授权。
	outside := mustGet(t, trusted(`presets: [{id: p}]`), project(`presets: [{id: p, components: [{id: s, use: skills, config: {dirs: ["~/.agents/skills"]}}]}]`))
	require.Len(t, outside.Expansions, 1)
	require.Contains(t, outside.Expansions[0].Reason, "工作区外的技能目录")
}

// taskRunner 按 "工具名 JSON参数" 的任务直接调用子 Agent 的工具，替代模型。
func taskRunner(runs *atomic.Int32) func(context.Context, *Assembly, string) (string, error) {
	return func(ctx context.Context, asm *Assembly, task string) (string, error) {
		runs.Add(1)
		name, raw, _ := strings.Cut(task, " ")
		var args map[string]any
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			return "", err
		}
		tool, ok := byToolName(asm)[name]
		if !ok {
			return "no tool " + name, nil
		}
		out, err := tool.Invoke(ctx, &interfaces.ToolInput{Args: args})
		if err != nil {
			return "", err
		}
		if msg, rejected := tools.RejectedResult(out); rejected {
			return "rejected: " + msg, nil
		}
		return "ok", nil
	}
}

func delegate(t *testing.T, asm *Assembly, tool, task string) string {
	t.Helper()
	out := invokeTool(t, asm, tool, map[string]any{"task": task})
	require.True(t, out.Success, "%+v", out)
	return out.Result.(map[string]any)["answer"].(string)
}

func subagentSet(parentPerms, sub, worker string) Source {
	return trusted(`presets:
  - id: lead
    components: [` + localWorkspace + `, {id: c, use: coding-tools}, {id: perms, use: permissions, config: ` + parentPerms + `}, {id: sub, use: subagent, config: ` + sub + `}]
  - id: worker
    components: [` + worker + `]`)
}

const workerComponents = localWorkspace + `, {id: c, use: coding-tools}, {id: perms, use: permissions, config: {base: coding, rules: [{tool: run_command, decision: allow}]}}`

func TestSubagentPermissionsIntersect(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("x\n"), 0o644))
	var runs atomic.Int32
	approver := &approvals{outcome: tools.ApprovalAllowedOnce}
	host := Host{Root: root, ArtifactDir: t.TempDir(), Approver: approver, RunAgent: taskRunner(&runs)}
	lead := mustGetID(t, "lead", subagentSet(`{base: coding, rules: [{tool: run_command, command: rm, decision: deny}, {tool: agent_worker, decision: allow}]}`, `{preset: worker}`, workerComponents))
	asm, err := Build(context.Background(), lead, host)
	require.NoError(t, err)

	// 子 Agent 自己放行 run_command，但父 Agent 的 deny 仍然生效；父 Agent 未放行的命令需要审批。
	require.Equal(t, "rejected: run_command is denied by policy", delegate(t, asm, "agent_worker", `run_command {"command":"rm -rf x"}`))
	require.Empty(t, approver.reasons)
	require.Equal(t, "ok", delegate(t, asm, "agent_worker", `run_command {"command":"echo hi"}`))
	require.Len(t, approver.reasons, 1, "the parent's policy asks for commands it did not allow")
	require.Equal(t, "ok", delegate(t, asm, "agent_worker", `read_file {"path":"a.txt"}`), "reads are allowed by both")
	require.NotEmpty(t, asm.Manifest.Components[3].Info["manifest"])
	require.NoError(t, asm.Close())

	// 父 Agent 的 approval: never 同样约束子 Agent。
	never := mustGetID(t, "lead", subagentSet(`{base: coding, approval: never, rules: [{tool: agent_worker, decision: allow}]}`, `{preset: worker}`, workerComponents))
	asm, err = Build(context.Background(), never, host)
	require.NoError(t, err)
	require.Contains(t, delegate(t, asm, "agent_worker", `run_command {"command":"echo hi"}`), "never")
	require.NoError(t, asm.Close())

	// 子 Agent 的沙箱不能宽于父 Agent。
	narrow := trusted(`presets:
  - id: lead
    components: [{id: w, use: workspace, config: {sandbox: read-only, executor: local}}, {id: sub, use: subagent, config: {preset: worker}}]
  - id: worker
    components: [` + workerComponents + `]`)
	_, err = Build(context.Background(), mustGetID(t, "lead", narrow), host)
	require.ErrorContains(t, err, "宽于父 Agent")
}

func TestSubagentToolFilter(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("x\n"), 0o644))
	var runs atomic.Int32
	host := Host{Root: root, ArtifactDir: t.TempDir(), RunAgent: taskRunner(&runs)}
	// 子 Agent 的 MCP 服务命令不存在：只有在它被过滤掉、根本不初始化时 Build 才能成功。
	worker := workerComponents + `, {id: srv, use: mcp, config: {transport: stdio, command: definitely-not-a-command}}, {id: spill, use: spill}`
	asm, err := Build(context.Background(), mustGetID(t, "lead", subagentSet(`{base: coding, rules: [{tool: agent_worker, decision: allow}]}`, `{preset: worker, toolFilter: {allow: [read_file, grep]}}`, worker)), host)
	require.NoError(t, err)
	require.Equal(t, "no tool run_command", delegate(t, asm, "agent_worker", `run_command {"command":"echo"}`))
	require.Equal(t, "ok", delegate(t, asm, "agent_worker", `read_file {"path":"a.txt"}`))
	require.NoError(t, asm.Close())

	_, err = Build(context.Background(), mustGetID(t, "lead", subagentSet(`{base: coding, rules: [{tool: agent_worker, decision: allow}]}`, `{preset: worker, toolFilter: {allow: [nope]}}`, workerComponents)), host)
	require.ErrorContains(t, err, "工具过滤引用的工具 nope 不存在")
	_, err = Build(context.Background(), mustGetID(t, "lead", subagentSet(`{base: coding, rules: [{tool: agent_worker, decision: allow}]}`, `{preset: worker, toolFilter: {deny: [run_command]}}`, worker)), host)
	require.ErrorContains(t, err, "连接 MCP 服务", "without an allow list the MCP row is initialized")
}

func TestSubagentStructure(t *testing.T) {
	host := Host{ArtifactDir: t.TempDir(), RunAgent: taskRunner(new(atomic.Int32))}
	_, err := Load(trusted(`presets: [{id: a, components: [{id: s, use: subagent, config: {preset: b}}]}, {id: b, components: [{id: s, use: subagent, config: {preset: a}}]}]`))
	require.ErrorContains(t, err, "引用有环")
	_, err = Load(trusted(`presets: [{id: a, components: [{id: s, use: subagent, config: {preset: missing}}]}]`))
	require.ErrorContains(t, err, "不存在")

	nested := `presets: [
  {id: a, components: [{id: s, use: subagent, config: {preset: b%s}}]},
  {id: b, components: [{id: s, use: subagent, config: {preset: c%s}}]},
  {id: c, components: [{id: p, use: test-probe, config: {name: c}}]}]`
	set, err := Load(trusted(strings.ReplaceAll(strings.Replace(nested, "%s", "", 1), "%s", "")))
	require.NoError(t, err)
	a, _ := set.Get("a")
	_, err = Build(context.Background(), a, host)
	require.ErrorContains(t, err, "嵌套深度 2 超过 maxDepth 1")
	set, err = Load(trusted(strings.ReplaceAll(nested, "%s", ", maxDepth: 2")))
	require.NoError(t, err)
	a, _ = set.Get("a")
	takeEvents()
	asm, err := Build(context.Background(), a, host)
	require.NoError(t, err)
	require.NoError(t, asm.Close())
	require.Equal(t, []string{"build c", "close c"}, takeEvents(), "closing the parent closes nested children")

	_, err = Build(context.Background(), a, Host{ArtifactDir: t.TempDir()})
	require.ErrorContains(t, err, "Host.RunAgent")
}

// finalModel 不调用工具，直接给出答复。
type finalModel struct{ answer string }

func (m finalModel) Complete(context.Context, *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{Content: m.answer}, nil
}
func (m finalModel) Chat(context.Context, []llm.Message) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{Content: m.answer}, nil
}
func (finalModel) Provider() constants.Provider { return constants.ProviderCustom }
func (finalModel) IsAvailable() bool            { return true }
func (m finalModel) stream() <-chan *llm.StreamChunk {
	ch := make(chan *llm.StreamChunk, 2)
	ch <- &llm.StreamChunk{Delta: m.answer}
	ch <- &llm.StreamChunk{Done: true, FinishReason: "stop"}
	close(ch)
	return ch
}
func (m finalModel) CompleteStream(context.Context, *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	return m.stream(), nil
}
func (m finalModel) ChatStream(context.Context, []llm.Message) (<-chan *llm.StreamChunk, error) {
	return m.stream(), nil
}
func (m finalModel) CompleteToolsStream(context.Context, *llm.CompletionRequest, []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	return m.stream(), nil
}

func TestBuilderRunner(t *testing.T) {
	src := trusted(`presets:
  - {id: lead, components: [{id: sub, use: subagent, config: {preset: worker}}, {id: perms, use: permissions, config: {rules: [{tool: agent_worker, decision: allow}]}}]}
  - {id: worker, components: [{id: persona, use: persona, config: {prompt: 你是助手}}]}`)
	set, err := Load(src)
	require.NoError(t, err)
	lead, _ := set.Get("lead")
	asm, err := Build(context.Background(), lead, Host{ArtifactDir: t.TempDir(), RunAgent: BuilderRunner(finalModel{answer: "子 Agent 完成"}, 3)})
	require.NoError(t, err)
	defer func() { require.NoError(t, asm.Close()) }()
	require.Equal(t, "子 Agent 完成", delegate(t, asm, "agent_worker", "整理文档"))
	requireRejected(t, invokeTool(t, asm, "agent_worker", map[string]any{"task": " "}), "must not be empty")
}

func mustGetID(t *testing.T, id string, sources ...Source) *Resolved {
	t.Helper()
	set, err := Load(sources...)
	require.NoError(t, err)
	p, ok := set.Get(id)
	require.True(t, ok)
	return p
}
