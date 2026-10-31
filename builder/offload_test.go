package builder

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder/contextpolicy"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

func newOffload(t *testing.T, cfg contextpolicy.OffloadConfig) *contextpolicy.Offload {
	t.Helper()
	o, err := contextpolicy.NewOffload(cfg)
	require.NoError(t, err)
	return o
}

// dispatched 让工具经过 Dispatcher，持久运行中记入日志。
func dispatched(t *testing.T, ts ...interfaces.Tool) []interfaces.Tool {
	t.Helper()
	reg := tools.NewRegistry()
	for _, tool := range ts {
		require.NoError(t, reg.Register(tool))
	}
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{})
	require.NoError(t, err)
	return d.Tools()
}

func toolCalls(calls ...llm.ToolCall) []*llm.StreamChunk {
	return []*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: calls}}
}

func finalAnswer(text string) []*llm.StreamChunk {
	return []*llm.StreamChunk{{Done: true, FinishReason: "stop", Content: text}}
}

type readingModel struct{ *nativeEventModel }

// CompleteToolsStream 在第三次请求时按对话中的引用读取证据。
func (m *readingModel) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, ts []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	if len(m.requests) == 2 {
		var ref map[string]any
		if err := json.Unmarshal([]byte(req.Messages[3].Content), &ref); err != nil {
			return nil, err
		}
		args, _ := json.Marshal(map[string]any{"key": ref["context_result"], "offset": 2})
		m.turns[2] = toolCalls(nativeCall("read", contextpolicy.ReadToolName, string(args)))
	}
	return m.nativeEventModel.CompleteToolsStream(ctx, req, ts)
}

// 非持久执行：较早的长结果换成引用、分页读回，最后一次请求不提供工具并还原原文。
func TestOffloadThroughNativeLoop(t *testing.T) {
	model := &readingModel{nativeModel(
		toolCalls(nativeCall("a", "lookup", "{}")),
		toolCalls(nativeCall("b", "lookup", "{}")), nil,
		[]*llm.StreamChunk{{Delta: "丙丁"}, {Done: true}},
	)}
	offload := newOffload(t, contextpolicy.OffloadConfig{Store: contextpolicy.NewMemoryStore(), MaxToolResultRunes: 3, KeepRecentToolRounds: 1, ReadPageRunes: 2})
	var finals []bool
	agent, err := NewSimpleBuilder(model).WithSystemPrompt("instructions").WithTools(NewMockTool("lookup", "甲乙丙丁戊"), offload).
		WithMaxIterations(4).WithOffload(offload).
		WithPreStep(func(_ context.Context, step *Step) (PreStepResult, error) {
			finals = append(finals, step.Final)
			return PreStepResult{}, nil
		}).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "read evidence")
	require.NoError(t, err)
	require.Equal(t, "丙丁", out.Result)
	require.Equal(t, []bool{false, false, false, true}, finals)
	require.Len(t, model.requests, 4)
	require.Equal(t, "甲乙丙丁戊", model.requests[1].Messages[3].Content, "最近一轮保留原文")
	require.Contains(t, model.requests[2].Messages[3].Content, `"context_result":"r1"`)
	require.Equal(t, "a", model.requests[2].Messages[3].ToolCallID, "引用不改变工具调用的配对")
	last := model.requests[3].Messages
	require.Contains(t, last[len(last)-1].Content, `"text":"丙丁"`)
	require.Empty(t, model.toolSets[3])
	require.Equal(t, "甲乙丙丁戊", last[3].Content, "最后一次请求无法再读取，引用还原为原文")
	require.EqualValues(t, 1, offload.Stats()["offloaded_results"])
}

func durableOffloadContext(t *testing.T, key string) (context.Context, contextpolicy.ResultStore) {
	t.Helper()
	file, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	results, err := contextpolicy.NewDurableResultStore(file)
	require.NoError(t, err)
	return durable.WithJournal(durable.WithExecutionKey(context.Background(), key), file), results
}

// 持久运行在换出引用之后中断：恢复时 PreStep 的结果被回放、Reduce 不再执行，新实例仍能按回放的对话读取证据。
func TestOffloadReadsAfterResume(t *testing.T) {
	ctx, results := durableOffloadContext(t, "resume")
	build := func(model llm.ToolStreamClient, hooks ...RequestHook) *ConfigurableAgent[any, *core.AgentState] {
		offload := newOffload(t, contextpolicy.OffloadConfig{Store: results, MaxToolResultRunes: 3})
		agent, err := NewSimpleBuilder(model).WithDurableScope("offload").WithSystemPrompt("system").
			WithTools(dispatched(t, NewMockTool("lookup", "long evidence"), offload)...).WithMaxIterations(4).WithOffload(offload).
			WithRequestHook(hooks...).Build()
		require.NoError(t, err)
		return agent
	}
	first := nativeModel(toolCalls(nativeCall("a", "lookup", "{}")))
	interrupt := func(_ context.Context, req *ModelRequest) error {
		if req.Step == 1 {
			return errors.New("进程中断")
		}
		return nil
	}
	_, err := build(first, interrupt).AsRunnable().Invoke(ctx, "question")
	require.ErrorContains(t, err, "进程中断")
	require.Len(t, first.requests, 1)

	resumed := nativeModel(toolCalls(nativeCall("b", contextpolicy.ReadToolName, `{"key":"r1","offset":0}`)), finalAnswer("answer"))
	out, err := build(resumed).AsRunnable().Invoke(ctx, "question")
	require.NoError(t, err)
	require.Equal(t, "answer", out.Result)
	require.Len(t, resumed.requests, 2, "第一次模型回合从日志回放")
	require.Contains(t, resumed.requests[0].Messages[3].Content, `"context_result":"r1"`, "回放的对话中是引用")
	read := resumed.requests[1].Messages[len(resumed.requests[1].Messages)-1]
	require.Equal(t, "b", read.ToolCallID)
	require.Contains(t, read.Content, "long evidence", "新实例从回放的对话重建引用表")

	// 配置改变后，PreStep 记录的意图不同：恢复在请求模型前失败，不回放按旧配置算出的对话。
	changed := nativeModel()
	offload := newOffload(t, contextpolicy.OffloadConfig{Store: results, MaxToolResultRunes: 3, Namespace: "other"})
	agent, err := NewSimpleBuilder(changed).WithDurableScope("offload").WithSystemPrompt("system").
		WithTools(dispatched(t, NewMockTool("lookup", "long evidence"), offload)...).WithMaxIterations(4).WithOffload(offload).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(ctx, "question")
	require.ErrorContains(t, err, "intent")
	require.Empty(t, changed.requests)
}

type referenceSaveFailure struct {
	durable.Store
	fail bool
}

func (s *referenceSaveFailure) Save(ctx context.Context, id string, snapshot *durable.Snapshot) error {
	if s.fail && strings.HasPrefix(id, "goagent/context-result/") {
		return errors.New("reference save failed")
	}
	return s.Store.Save(ctx, id, snapshot)
}

// 证据写入失败时不发出指向不存在证据的引用。
func TestOffloadSavesEvidenceBeforeRequest(t *testing.T) {
	file, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	fault := &referenceSaveFailure{Store: file, fail: true}
	results, err := contextpolicy.NewDurableResultStore(fault)
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "save-order"), file)
	m := nativeModel(toolCalls(nativeCall("a", "lookup", "{}")), finalAnswer("answer"))
	build := func() *ConfigurableAgent[any, *core.AgentState] {
		offload := newOffload(t, contextpolicy.OffloadConfig{Store: results, MaxToolResultRunes: 3})
		a, err := NewSimpleBuilder(m).WithDurableScope("offload").WithTools(dispatched(t, NewMockTool("lookup", "long evidence"), offload)...).
			WithMaxIterations(3).WithOffload(offload).Build()
		require.NoError(t, err)
		return a
	}
	_, err = build().AsRunnable().Invoke(ctx, "question")
	require.ErrorContains(t, err, "reference save failed")
	require.Len(t, m.requests, 1)
	fault.fail = false
	_, err = build().AsRunnable().Invoke(ctx, "question")
	require.NoError(t, err)
	require.Len(t, m.requests, 2)
}

// 复现 Gemini 实测：模型把引用键抄错一位。经 Dispatcher 记入日志后仍交还模型改正，且整轮可重放。
func TestOffloadWrongKeyReturnedToModel(t *testing.T) {
	ctx, results := durableOffloadContext(t, "wrong-key")
	model := nativeModel(
		toolCalls(nativeCall("a", "lookup", "{}")),
		toolCalls(nativeCall("b", contextpolicy.ReadToolName, `{"key":"r9","offset":0}`)),
		toolCalls(nativeCall("c", contextpolicy.ReadToolName, `{"key":"r1","offset":0}`)),
		finalAnswer("answer"),
	)
	for range 2 {
		offload := newOffload(t, contextpolicy.OffloadConfig{Store: results, MaxToolResultRunes: 3})
		agent, err := NewSimpleBuilder(model).WithDurableScope("offload").WithSystemPrompt("system").
			WithTools(dispatched(t, NewMockTool("lookup", "long evidence"), offload)...).WithMaxIterations(5).WithOffload(offload).Build()
		require.NoError(t, err)
		out, err := agent.AsRunnable().Invoke(ctx, "question")
		require.NoError(t, err)
		require.Equal(t, "answer", out.Result)
	}
	require.Len(t, model.requests, 4, "第二次执行全部重放")
	require.Contains(t, model.requests[2].Messages[5].Content, "unknown context_result key")
	require.Contains(t, model.requests[3].Messages[7].Content, "long evidence")
}

// trackingStore 记录尚未释放的运行键。
type trackingStore struct {
	contextpolicy.ResultStore
	mu   sync.Mutex
	live map[string]bool
}

func (s *trackingStore) PutIfAbsent(ctx context.Context, ref contextpolicy.Ref, data []byte) error {
	s.mu.Lock()
	s.live[ref.RunKey] = true
	s.mu.Unlock()
	return s.ResultStore.PutIfAbsent(ctx, ref, data)
}
func (s *trackingStore) Release(ctx context.Context, run string) error {
	s.mu.Lock()
	delete(s.live, run)
	s.mu.Unlock()
	return s.ResultStore.Release(ctx, run)
}

// 非持久执行共享同一 Offload 时，Builder 生成的临时作用域在执行结束（成功或失败）后释放；
// 宿主经 WithResultScope 提供的作用域留给宿主回收。
func TestOffloadEphemeralScopeReleased(t *testing.T) {
	store := &trackingStore{ResultStore: contextpolicy.NewMemoryStore(), live: map[string]bool{}}
	offload := newOffload(t, contextpolicy.OffloadConfig{Store: store, MaxToolResultRunes: 3})
	run := func(ctx context.Context, fail bool) error {
		second := []*llm.StreamChunk{{Delta: "答"}, {Done: true, FinishReason: "stop"}}
		if fail {
			second = []*llm.StreamChunk{{Error: errors.New("模型失败")}}
		}
		model := nativeModel(toolCalls(nativeCall("a", "lookup", "{}")), toolCalls(nativeCall("b", "lookup", "{}")), second)
		agent, err := NewSimpleBuilder(model).WithTools(NewMockTool("lookup", "long evidence"), offload).WithMaxIterations(4).WithOffload(offload).Build()
		require.NoError(t, err)
		_, err = agent.AsRunnable().Invoke(ctx, "question")
		return err
	}
	for i := range 20 {
		err := run(context.Background(), i%2 == 1)
		if i%2 == 1 {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
	require.Empty(t, store.live, "临时作用域全部释放")

	hosted := contextpolicy.WithResultScope(context.Background(), "host-run")
	require.NoError(t, run(hosted, false))
	require.Equal(t, map[string]bool{"host-run": true}, store.live, "宿主作用域由宿主释放")
	require.NoError(t, offload.Release(hosted))
	require.Empty(t, store.live)
}

func TestOffloadConfiguration(t *testing.T) {
	offload := newOffload(t, contextpolicy.OffloadConfig{Store: contextpolicy.NewMemoryStore(), MaxToolResultRunes: 3})
	lookup := NewMockTool("lookup", "x")
	_, err := NewSimpleBuilder(nativeModel()).WithTools(lookup).WithOffload(offload).Build()
	require.ErrorContains(t, err, "读取工具")
	_, err = NewSimpleBuilder(nativeModel()).WithTools(lookup, offload, NewMockTool(contextpolicy.ReadToolName, "x")).WithOffload(offload).Build()
	require.Error(t, err)
	_, err = NewSimpleBuilder(nativeModel()).WithTools(lookup, offload).WithOffload(offload).WithOffload(offload).Build()
	require.Error(t, err)
	_, err = NewSimpleBuilder(NewMockLLMClient()).WithTools(lookup, offload).WithOffload(offload).Build()
	require.Error(t, err, "需要原生工具流")

	agent, err := NewSimpleBuilder(nativeModel()).WithTools(lookup, offload).WithOffload(offload).Build()
	require.NoError(t, err)
	_, err = agent.Execute(context.Background(), "question")
	require.ErrorContains(t, err, "WithOffload")

	// 非持久的结果存储不能用于持久运行：恢复后证据已不存在。
	ctx, _ := durableOffloadContext(t, "memory")
	durableAgent, err := NewSimpleBuilder(nativeModel(toolCalls(nativeCall("a", "lookup", "{}")), finalAnswer("answer"))).WithDurableScope("offload").
		WithTools(dispatched(t, NewMockTool("lookup", "long evidence"), offload)...).WithMaxIterations(3).WithOffload(offload).Build()
	require.NoError(t, err)
	_, err = durableAgent.AsRunnable().Invoke(ctx, "question")
	require.ErrorContains(t, err, "durable result store")
}

// 带身份的 PreStep：身份进入记录的意图，改变后恢复失败；不带身份的钩子沿用原有意图，改了实现也照常回放。
func TestIdentifiedPreStepIntent(t *testing.T) {
	ctx, _ := durableOffloadContext(t, "identity")
	run := func(build func(*SimpleAgentBuilder) *SimpleAgentBuilder) error {
		agent, err := build(NewSimpleBuilder(nativeModel(finalAnswer("answer"))).WithDurableScope("pre").WithTools(dispatched(t, NewMockTool("lookup", "x"))...)).Build()
		require.NoError(t, err)
		_, err = agent.AsRunnable().Invoke(ctx, "question")
		return err
	}
	noop := func(context.Context, *Step) (PreStepResult, error) { return PreStepResult{}, nil }
	require.NoError(t, run(func(b *SimpleAgentBuilder) *SimpleAgentBuilder {
		return b.WithIdentifiedPreStep("glossary/v1", noop).WithPreStep(noop)
	}))
	require.NoError(t, run(func(b *SimpleAgentBuilder) *SimpleAgentBuilder {
		return b.WithIdentifiedPreStep("glossary/v1", noop).WithPreStep(func(context.Context, *Step) (PreStepResult, error) {
			return PreStepResult{}, errors.New("回放时不应执行")
		})
	}), "身份不变时回放记录")
	err := run(func(b *SimpleAgentBuilder) *SimpleAgentBuilder {
		return b.WithIdentifiedPreStep("glossary/v2", noop).WithPreStep(noop)
	})
	require.ErrorContains(t, err, "intent")
}
