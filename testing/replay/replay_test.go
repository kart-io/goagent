package replay_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/testing/replay"
	"github.com/kart-io/goagent/tools"
)

// model 按顺序给出回合，记录调用次数；回合用尽时报错。
type model struct {
	mu    sync.Mutex
	turns []*llm.StreamChunk
	calls int
}

func say(text string) *llm.StreamChunk {
	return &llm.StreamChunk{Delta: text, Done: true, FinishReason: "stop"}
}

func call(id string) *llm.StreamChunk {
	tc := llm.ToolCall{ID: id, Type: "function"}
	tc.Function.Name, tc.Function.Arguments = "lookup", `{"q":"x"}`
	return &llm.StreamChunk{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{tc}}
}

func (m *model) CompleteToolsStream(context.Context, *llm.CompletionRequest, []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calls >= len(m.turns) {
		return nil, fmt.Errorf("多余的模型请求 %d", m.calls)
	}
	ch := make(chan *llm.StreamChunk, 1)
	ch <- m.turns[m.calls]
	m.calls++
	close(ch)
	return ch, nil
}
func (m *model) Complete(context.Context, *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return nil, errors.New("unused")
}
func (m *model) Chat(context.Context, []llm.Message) (*llm.CompletionResponse, error) {
	return nil, errors.New("unused")
}
func (*model) Provider() constants.Provider { return constants.ProviderCustom }
func (*model) IsAvailable() bool            { return true }
func (m *model) CompleteStream(context.Context, *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	return nil, errors.New("unused")
}
func (m *model) ChatStream(context.Context, []llm.Message) (<-chan *llm.StreamChunk, error) {
	return nil, errors.New("unused")
}

// lookup 返回 result，并统计执行次数。
func lookup(result string, runs *atomic.Int32) interfaces.Tool {
	return tools.NewBaseTool("lookup", "查询", `{"type":"object","properties":{"q":{"type":"string"}}}`,
		func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			runs.Add(1)
			return &interfaces.ToolOutput{Success: true, Result: result}, nil
		})
}

func agent(t *testing.T, m *model, tool interfaces.Tool, log session.Store) *builder.ConfigurableAgent[any, *core.AgentState] {
	t.Helper()
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tool))
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{})
	require.NoError(t, err)
	a, err := builder.NewSimpleBuilder(m).WithSystemPrompt("你是助手").WithSessionID("s").WithSessionLog(log).
		WithDurableScope("replay-test").WithTools(d.Tools()...).WithMaxIterations(4).Build()
	require.NoError(t, err)
	return a
}

// record 跑一轮持久运行：调用 lookup 后回答，会话日志兼作 durable 日志。
func record(t *testing.T) session.Store {
	store := session.NewMemoryStore()
	var runs atomic.Int32
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "run-1"), session.DurableStore(store, "s"))
	out, err := agent(t, &model{turns: []*llm.StreamChunk{call("c1"), say("x 是 42")}}, lookup("42", &runs), store).AsRunnable().Invoke(ctx, "查 x")
	require.NoError(t, err)
	require.Equal(t, "x 是 42", out.Result)
	return store
}

func replayTurn(t *testing.T, rp *replay.Replayer, m *model, tool interfaces.Tool) (string, *replay.Report, session.Store) {
	t.Helper()
	turns, err := rp.Turns(context.Background())
	require.NoError(t, err)
	require.Len(t, turns, 1)
	ctx, log, report, err := rp.Prepare(context.Background(), turns[0])
	require.NoError(t, err)
	out, err := agent(t, m, tool, log).AsRunnable().Invoke(ctx, turns[0].Input)
	require.NoError(t, err)
	return fmt.Sprint(out.Result), report, log
}

func modes(r *replay.Report) map[string]replay.Mode {
	out := map[string]replay.Mode{}
	for _, res := range r.Results() {
		out[res.ID] = res.Mode
	}
	return out
}

func conversation(t *testing.T, store session.Store) []llm.Message {
	t.Helper()
	events, err := store.Read(context.Background(), "s", 0)
	require.NoError(t, err)
	msgs, err := session.DeriveMessages(events)
	require.NoError(t, err)
	return msgs
}

func TestFullReplay(t *testing.T) {
	src := record(t)
	var runs atomic.Int32
	m := &model{}
	answer, report, log := replayTurn(t, &replay.Replayer{Source: src, SessionID: "s"}, m, lookup("不该执行", &runs))
	require.Equal(t, "x 是 42", answer)
	require.Zero(t, m.calls, "model turns come from the log")
	require.Zero(t, runs.Load(), "tool calls come from the log")
	require.Equal(t, map[string]replay.Mode{"model/turn/0": replay.Replayed, "tool/turn/0/call/c1": replay.Replayed, "model/turn/1": replay.Replayed}, modes(report))
	_, diverged := report.Diverged()
	require.False(t, diverged)
	require.Equal(t, conversation(t, src), conversation(t, log), "the replayed conversation equals the recorded one")

	before := conversation(t, src)
	_, _, _ = replayTurn(t, &replay.Replayer{Source: src, SessionID: "s"}, &model{}, lookup("x", &runs))
	require.Equal(t, before, conversation(t, src), "replay never writes to the source log")
}

// 工具改了实现：工具实跑，结果不变时模型继续回放；结果变了，之后的模型回合用新代码实跑。
func TestLiveTools(t *testing.T) {
	src := record(t)
	var runs atomic.Int32
	m := &model{}
	answer, report, _ := replayTurn(t, &replay.Replayer{Source: src, SessionID: "s", Live: replay.Kind("tool")}, m, lookup("42", &runs))
	require.Equal(t, "x 是 42", answer)
	require.EqualValues(t, 1, runs.Load())
	require.Zero(t, m.calls)
	require.Equal(t, replay.Live, modes(report)["tool/turn/0/call/c1"])
	_, diverged := report.Diverged()
	require.False(t, diverged, "the tool still returns the recorded result")

	changed := &model{turns: []*llm.StreamChunk{say("x 是 43")}}
	answer, report, _ = replayTurn(t, &replay.Replayer{Source: src, SessionID: "s", Live: replay.Kind("tool")}, changed, lookup("43", &runs))
	require.Equal(t, "x 是 43", answer)
	require.Equal(t, 1, changed.calls, "the model turn after the divergence runs live")
	first, diverged := report.Diverged()
	require.True(t, diverged)
	require.Equal(t, "tool/turn/0/call/c1", first.ID)
	require.Contains(t, string(first.Recorded), "42")
	require.Contains(t, string(first.Value), "43")
	require.Equal(t, replay.Live, modes(report)["model/turn/1"])
	require.Equal(t, replay.Replayed, modes(report)["model/turn/0"])
}

// 从第二次模型请求切开：之前的边界回放，之后用新模型实跑。
func TestCut(t *testing.T) {
	src := record(t)
	var runs atomic.Int32
	m := &model{turns: []*llm.StreamChunk{say("新答复")}}
	answer, report, log := replayTurn(t, &replay.Replayer{Source: src, SessionID: "s", Cut: replay.ModelTurn(1)}, m, lookup("x", &runs))
	require.Equal(t, "新答复", answer)
	require.Zero(t, runs.Load(), "the tool call before the cut is replayed")
	require.Equal(t, 1, m.calls)
	require.Equal(t, replay.Live, modes(report)["model/turn/1"])
	msgs := conversation(t, log)
	require.Equal(t, "新答复", msgs[len(msgs)-1].Content)
	require.Equal(t, conversation(t, src)[:3], msgs[:3], "the conversation up to the cut is unchanged")
}

// 改了被回放边界的输入（这里是 system 提示词）而没有切开时，重放失败，Report 的最后一项指出出错的边界。
func TestChangedInputWithoutCut(t *testing.T) {
	src := record(t)
	rp := &replay.Replayer{Source: src, SessionID: "s"}
	turns, err := rp.Turns(context.Background())
	require.NoError(t, err)
	ctx, log, report, err := rp.Prepare(context.Background(), turns[0])
	require.NoError(t, err)
	var runs atomic.Int32
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(lookup("42", &runs)))
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{})
	require.NoError(t, err)
	a, err := builder.NewSimpleBuilder(&model{}).WithSystemPrompt("你是另一个助手").WithSessionID("s").WithSessionLog(log).
		WithDurableScope("replay-test").WithTools(d.Tools()...).WithMaxIterations(4).Build()
	require.NoError(t, err)
	_, err = a.AsRunnable().Invoke(ctx, turns[0].Input)
	require.ErrorContains(t, err, "operation intent or attempt policy changed")
	results := report.Results()
	require.Equal(t, "model/turn/0", results[len(results)-1].ID)
}
