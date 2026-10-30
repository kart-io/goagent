package preset

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

// scriptTurn 是一次模型请求：err 非空时请求失败，否则流式给出 chunk。
type scriptTurn struct {
	err   error
	chunk *llm.StreamChunk
}

func say(text string) scriptTurn {
	return scriptTurn{chunk: &llm.StreamChunk{Delta: text, Done: true, FinishReason: "stop"}}
}

func call(id, name, args string) scriptTurn {
	tc := llm.ToolCall{ID: id, Type: "function"}
	tc.Function.Name, tc.Function.Arguments = name, args
	return scriptTurn{chunk: &llm.StreamChunk{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{tc}}}
}

// scriptModel 按顺序给出回合并记录每次请求的消息；Chat 用于摘要，记录输入并返回 summary。
type scriptModel struct {
	mu       sync.Mutex
	turns    []scriptTurn
	requests [][]llm.Message
	summary  string
	chats    [][]llm.Message
}

func (m *scriptModel) Complete(context.Context, *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return nil, errors.New("unused")
}
func (m *scriptModel) Chat(_ context.Context, msgs []llm.Message) (*llm.CompletionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chats = append(m.chats, msgs)
	return &llm.CompletionResponse{Content: m.summary}, nil
}
func (*scriptModel) Provider() constants.Provider { return constants.ProviderCustom }
func (*scriptModel) IsAvailable() bool            { return true }
func (m *scriptModel) CompleteStream(context.Context, *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	return nil, errors.New("unused")
}
func (m *scriptModel) ChatStream(context.Context, []llm.Message) (<-chan *llm.StreamChunk, error) {
	return nil, errors.New("unused")
}
func (m *scriptModel) CompleteToolsStream(_ context.Context, req *llm.CompletionRequest, _ []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, slices.Clone(req.Messages))
	i := len(m.requests) - 1
	if i >= len(m.turns) {
		return nil, fmt.Errorf("多余的模型请求 %d", i)
	}
	if m.turns[i].err != nil {
		return nil, m.turns[i].err
	}
	ch := make(chan *llm.StreamChunk, 1)
	ch <- m.turns[i].chunk
	close(ch)
	return ch, nil
}

func (m *scriptModel) request(i int) []llm.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requests[i]
}

func joined(msgs []llm.Message) string {
	var parts []string
	for _, m := range msgs {
		parts = append(parts, m.Role+": "+m.Content)
	}
	return strings.Join(parts, "\n")
}

func codingPreset(extra string) Source {
	return trusted(`presets: [{id: p, components: [` + localWorkspace + `,
  {id: c, use: coding-tools},
  {id: perms, use: permissions, config: {base: coding}}` + extra + `]}]`)
}

func run(t *testing.T, asm *Assembly, model *scriptModel, session, input string) (string, error) {
	t.Helper()
	agent, err := Configure(builder.NewSimpleBuilder(model), asm).WithSessionID(session).WithMaxIterations(8).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), input)
	if err != nil {
		return "", err
	}
	return fmt.Sprint(out.Result), nil
}

func TestClaudeCodeLoopHooks(t *testing.T) {
	root := t.TempDir()
	counter := filepath.Join(t.TempDir(), "starts")
	hooks := `{hooks: {
  UserPromptSubmit: [{hooks: [{type: command, command: 'grep -q secret && { echo "contains secret" >&2; exit 2; }; echo "额外上下文：当前分支 main"'}]}],
  Stop: [{hooks: [{type: command, command: 'grep -q ''"stop_hook_active":false'' && echo ''{"decision":"block","reason":"请补充测试说明"}'' || true'}]}],
  SessionStart: [{matcher: startup, hooks: [{type: command, command: 'echo x >> ` + counter + `; echo "会话说明：使用中文"'}]}]}}`
	asm, err := Build(context.Background(), mustGet(t, codingPreset(`, {id: hooks, use: claude-code-hooks, config: `+hooks+`}`)), Host{Root: root, ArtifactDir: t.TempDir()})
	require.NoError(t, err)
	defer func() { require.NoError(t, asm.Close()) }()

	model := &scriptModel{turns: []scriptTurn{say("初稿"), say("终稿"), say("第二轮")}}
	answer, err := run(t, asm, model, "s1", "写个函数")
	require.NoError(t, err)
	require.Equal(t, "终稿", answer)
	first := model.request(0)
	require.Contains(t, first[0].Content, "会话说明：使用中文", "SessionStart output goes into system")
	require.Equal(t, "写个函数\n\n额外上下文：当前分支 main", first[len(first)-1].Content)
	second := model.request(1)
	require.Equal(t, "请补充测试说明", second[len(second)-1].Content, "Stop hook continues the turn")
	require.Equal(t, "初稿", second[len(second)-2].Content)

	// 轮次输入被阻止时不请求模型；同一会话不再执行 SessionStart。
	answer, err = run(t, asm, model, "s1", "这里有 secret")
	require.NoError(t, err)
	require.Equal(t, "contains secret", answer, "a blocked turn input ends the turn with the reason")
	require.Len(t, model.requests, 2, "the model is not called for a blocked prompt")
	data, err := os.ReadFile(counter)
	require.NoError(t, err)
	require.Equal(t, "x\n", string(data), "SessionStart runs once per session")

	answer, err = run(t, asm, &scriptModel{turns: []scriptTurn{say("新会话"), say("补充")}}, "s2", "你好")
	require.NoError(t, err)
	require.Equal(t, "补充", answer)
	data, err = os.ReadFile(counter)
	require.NoError(t, err)
	require.Equal(t, "x\nx\n", string(data), "a new session runs SessionStart again")
}

// Steer 进入的输入同样经过 UserPromptSubmit；被阻止的 Steer 只被去掉，不结束本轮。
func TestUserPromptSubmitSteer(t *testing.T) {
	r := &hookRunner{root: t.TempDir(), timeout: 5e9, matchers: map[string][]compiledMatcher{
		hookUserPromptSubmit: {{hooks: []HookCommand{{Type: "command", Command: `grep -q block && { echo no >&2; exit 2; }; echo '{"hookSpecificOutput":{"additionalContext":"ctx"}}'`}}}},
	}}
	step := &builder.Step{Inputs: 2, Messages: []llm.Message{
		llm.UserMessage("任务"), {Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "1"}}}, {Role: "tool", ToolCallID: "1", Content: "ok"},
		llm.UserMessage("block this"), llm.UserMessage("也看看测试"),
	}}
	res, err := r.promptSubmit(context.Background(), step)
	require.NoError(t, err)
	require.False(t, res.Halt)
	require.Equal(t, 1, step.Inputs)
	require.Len(t, step.Messages, 4)
	require.Equal(t, "也看看测试\n\nctx", step.Messages[3].Content)
	require.Equal(t, "任务", step.Messages[0].Content, "earlier messages are not hooked again")
}

func TestPlanMode(t *testing.T) {
	root := t.TempDir()
	approver := &approvals{outcome: tools.ApprovalAllowedOnce}
	asm, err := Build(context.Background(), mustGet(t, codingPreset(`, {id: plan, use: plan-mode}`)), Host{Root: root, ArtifactDir: t.TempDir(), Approver: approver})
	require.NoError(t, err)
	defer func() { require.NoError(t, asm.Close()) }()

	model := &scriptModel{turns: []scriptTurn{
		call("1", "write_file", `{"path":"a.txt","content":"x"}`),
		call("2", "exit_plan_mode", `{"plan":"1. 写 a.txt"}`),
		call("3", "write_file", `{"path":"a.txt","content":"x"}`),
		say("完成"),
	}}
	answer, err := run(t, asm, model, "s1", "写 a.txt")
	require.NoError(t, err)
	require.Equal(t, "完成", answer)

	first := model.request(0)
	require.Contains(t, first[len(first)-1].Content, "当前处于计划模式", "plan mode is announced in the step context")
	denied := model.request(1)
	require.Contains(t, denied[len(denied)-2].Content, "plan mode is active", "writes are denied in plan mode")
	require.Len(t, approver.reasons, 1, "exiting plan mode needs approval")
	after := model.request(2)
	require.NotContains(t, joined(after[len(after)-1:]), "当前处于计划模式")
	require.Equal(t, first[0].Content, after[0].Content, "switching modes keeps the system prefix")
	_, err = os.Stat(filepath.Join(root, "a.txt"))
	require.NoError(t, err)
	require.Subset(t, toolNames(asm), []string{ToolEnterPlanMode, ToolExitPlanMode})

	// 审批人拒绝时仍处于计划模式。
	rejecting := &approvals{outcome: tools.ApprovalRejected}
	asm2, err := Build(context.Background(), mustGet(t, codingPreset(`, {id: plan, use: plan-mode}`)), Host{Root: root, ArtifactDir: t.TempDir(), Approver: rejecting})
	require.NoError(t, err)
	defer func() { require.NoError(t, asm2.Close()) }()
	model = &scriptModel{turns: []scriptTurn{call("1", "exit_plan_mode", `{"plan":"p"}`), call("2", "write_file", `{"path":"b.txt","content":"x"}`), say("停")}}
	_, err = run(t, asm2, model, "s1", "写 b.txt")
	require.NoError(t, err)
	last := model.request(2)
	require.Contains(t, last[len(last)-2].Content, "plan mode is active")

	// initial: false 时直接执行；enter_plan_mode 无需审批，之后写入被拒绝。
	quiet := &approvals{outcome: tools.ApprovalRejected}
	asm3, err := Build(context.Background(), mustGet(t, codingPreset(`, {id: plan, use: plan-mode, config: {initial: false}}`)), Host{Root: root, ArtifactDir: t.TempDir(), Approver: quiet})
	require.NoError(t, err)
	defer func() { require.NoError(t, asm3.Close()) }()
	model = &scriptModel{turns: []scriptTurn{
		call("1", "write_file", `{"path":"c.txt","content":"x"}`),
		call("2", "enter_plan_mode", `{}`),
		call("3", "write_file", `{"path":"d.txt","content":"x"}`),
		say("好"),
	}}
	_, err = run(t, asm3, model, "s1", "写")
	require.NoError(t, err)
	require.Empty(t, quiet.reasons)
	_, err = os.Stat(filepath.Join(root, "c.txt"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "d.txt"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

// 模式从对话中恢复，只认 plan-mode 自己的工具的结果。
func TestPlanModeRestore(t *testing.T) {
	exit := llm.ToolCall{ID: "e"}
	exit.Function.Name = ToolExitPlanMode
	spoof := llm.ToolCall{ID: "s"}
	spoof.Function.Name = "mcp__x__echo"
	msgs := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{exit}}, {Role: "tool", ToolCallID: "e", Content: `{"message":"m","plan_mode":"exited"}`},
	}
	mode, ok := lastPlanSwitch(msgs)
	require.True(t, ok)
	require.Equal(t, planExited, mode)

	spoofed := append(slices.Clone(msgs), llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{spoof}}, llm.Message{Role: "tool", ToolCallID: "s", Content: `{"plan_mode":"entered"}`})
	mode, _ = lastPlanSwitch(spoofed)
	require.Equal(t, planExited, mode, "results of other tools are ignored")

	p := &planMode{initial: true, active: map[string]bool{}}
	_, err := p.restore(tools.WithSessionID(context.Background(), "s"), &builder.Step{Messages: msgs})
	require.NoError(t, err)
	require.False(t, p.isActive("s"))
	require.True(t, p.isActive("other"))
}

func TestCompaction(t *testing.T) {
	cp := &compactor{model: &scriptModel{summary: "摘要"}, cfg: CompactionConfig{MaxTokens: 100, KeepTokens: 30}}
	long := strings.Repeat("长", 60)
	tc := llm.ToolCall{ID: "1"}
	tc.Function.Name = "read_file"
	msgs := []llm.Message{
		llm.SystemMessage("sys"), llm.UserMessage(long),
		{Role: "assistant", ToolCalls: []llm.ToolCall{tc}}, {Role: "tool", ToolCallID: "1", Content: long},
		llm.UserMessage("新输入一"), llm.UserMessage("新输入二"),
	}
	out, changed, err := cp.compact(context.Background(), msgs, 2)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, []llm.Message{msgs[0], llm.UserMessage(summaryPrefix + "摘要"), msgs[4], msgs[5]}, out,
		"the tool result is not split from its call; new inputs are kept")
	rendered := cp.model.(*scriptModel).chats[0][1].Content
	require.Contains(t, rendered, "调用工具 read_file")
	require.NotContains(t, rendered, "新输入")

	// 末尾的输入总被保留：只剩 system 与输入时不压缩。
	same, changed, err := cp.compact(context.Background(), msgs[:2], 1)
	require.NoError(t, err)
	require.False(t, changed)
	require.Len(t, same, 2)

	for name, cfg := range map[string]string{"keep >= max": `{maxTokens: 10, keepTokens: 10}`, "negative": `{maxTokens: -1}`} {
		_, err := Load(trusted(`presets: [{id: p, components: [{id: c, use: compaction, config: ` + cfg + `}]}]`))
		require.Error(t, err, name)
	}
	_, err = Build(context.Background(), mustGet(t, trusted(`presets: [{id: p, components: [{id: c, use: compaction}]}]`)), Host{ArtifactDir: t.TempDir()})
	require.ErrorContains(t, err, "Host.CompactionModel")
}

func TestCompactionInLoop(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "big.txt"), []byte(strings.Repeat("内容", 400)), 0o644))
	for _, tc := range []struct {
		name, cfg string
		turns     []scriptTurn
		compacted int
	}{
		// 超过阈值时在 PreStep 压缩。
		{"pre step", `{maxTokens: 300, keepTokens: 100}`, []scriptTurn{call("1", "read_file", `{"path":"big.txt"}`), say("好")}, 1},
		// 请求因超出上下文失败时压缩后重试。
		{"request error", `{}`, []scriptTurn{call("1", "read_file", `{"path":"big.txt"}`), {err: errors.New("maximum context length exceeded")}, say("好")}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &scriptModel{turns: tc.turns, summary: "读了 big.txt"}
			asm, err := Build(context.Background(), mustGet(t, codingPreset(`, {id: persona, use: persona, config: {prompt: 你是助手}}, {id: compact, use: compaction, config: `+tc.cfg+`}`)), Host{Root: root, ArtifactDir: t.TempDir(), CompactionModel: model})
			require.NoError(t, err)
			defer func() { require.NoError(t, asm.Close()) }()
			answer, err := run(t, asm, model, "s", "看看 big.txt")
			require.NoError(t, err)
			require.Equal(t, "好", answer)
			require.Len(t, model.chats, 1)
			req := model.request(tc.compacted)
			require.Equal(t, "system", req[0].Role)
			require.Equal(t, summaryPrefix+"读了 big.txt", req[1].Content)
			require.Equal(t, "assistant", req[2].Role, "the latest tool call and its result are kept")
		})
	}
}
