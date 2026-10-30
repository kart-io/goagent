package builder

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

// scriptTurn 流式产生一次模型回合；send 在 ctx 结束后返回 false。
type scriptTurn func(ctx context.Context, send func(*llm.StreamChunk) bool)

// step 是一次模型请求：fail 非空时请求直接失败，否则执行 run。
type step struct {
	fail error
	run  scriptTurn
}

// scriptModel 按顺序执行回合，记录每次请求。
type scriptModel struct {
	*eventModel
	mu       sync.Mutex
	turns    []step
	requests []*llm.CompletionRequest
}

func newScriptModel(turns ...step) *scriptModel {
	return &scriptModel{eventModel: &eventModel{MockLLMClient: NewMockLLMClient()}, turns: turns}
}

func (m *scriptModel) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, _ []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	m.mu.Lock()
	copied := *req
	copied.Messages = slices.Clone(req.Messages)
	m.requests = append(m.requests, &copied)
	index := len(m.requests) - 1
	m.mu.Unlock()
	if index >= len(m.turns) {
		return nil, errors.New("发生了多余模型请求")
	}
	if m.turns[index].fail != nil {
		return nil, m.turns[index].fail
	}
	stream := make(chan *llm.StreamChunk)
	go func() {
		defer close(stream)
		m.turns[index].run(ctx, func(c *llm.StreamChunk) bool {
			select {
			case stream <- c:
				return true
			case <-ctx.Done():
				return false
			}
		})
	}()
	return stream, nil
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

func answer(text string) step {
	return step{run: func(_ context.Context, send func(*llm.StreamChunk) bool) {
		send(&llm.StreamChunk{Delta: text})
		send(&llm.StreamChunk{Done: true, FinishReason: "stop"})
	}}
}

func callTool(id, name string) step {
	return step{run: func(_ context.Context, send func(*llm.StreamChunk) bool) {
		send(&llm.StreamChunk{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall(id, name, "{}")}})
	}}
}

// gated 先发出 started，等 release 关闭后再给出回合。
func gated(started chan<- struct{}, release <-chan struct{}, then step) step {
	return step{run: func(ctx context.Context, send func(*llm.StreamChunk) bool) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return
		}
		then.run(ctx, send)
	}}
}

func failing(err error) step { return step{fail: err} }

// blockingTool 在执行时发出 started，等 release 关闭后返回。
func blockingTool(started chan<- struct{}, release <-chan struct{}) interfaces.Tool {
	var once sync.Once
	return tools.NewBaseTool("lookup", "test", `{"type":"object"}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		once.Do(func() { close(started) })
		<-release
		return &interfaces.ToolOutput{Success: true, Result: "found"}, nil
	})
}

func plainTool() interfaces.Tool {
	return tools.NewBaseTool("lookup", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true, Result: "found"}, nil
	})
}

func lastMessages(req *llm.CompletionRequest, n int) []llm.Message {
	return req.Messages[len(req.Messages)-n:]
}

func waitAnswer(t *testing.T, h *Handle) *AgentOutput {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := h.Wait(ctx)
	require.NoError(t, err)
	return out
}

// 工具执行中收到的 Steer 在本批工具结果之后作为 user 消息进入下一次请求。
func TestHandleSteerDuringTool(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	model := newScriptModel(callTool("c1", "lookup"), answer("答"))
	var inputs []int
	agent, err := NewSimpleBuilder(model).WithTools(blockingTool(started, release)).WithMaxIterations(4).
		WithPreStep(func(_ context.Context, s *Step) (PreStepResult, error) {
			inputs = append(inputs, s.Inputs)
			return PreStepResult{}, nil
		}).
		Build()
	require.NoError(t, err)
	h := agent.Start(context.Background(), "问题")
	<-started
	require.NoError(t, h.Steer("顺便检查 B"))
	require.NoError(t, h.Redirect("工具执行中的纠正降级为 Steer"))
	close(release)
	require.Equal(t, "答", waitAnswer(t, h).Result)
	tail := lastMessages(model.request(1), 3)
	require.Equal(t, "tool", tail[0].Role)
	require.Equal(t, llm.UserMessage("顺便检查 B"), tail[1])
	require.Equal(t, llm.UserMessage("工具执行中的纠正降级为 Steer"), tail[2])
	require.Equal(t, []int{1, 2}, inputs, "PreStep sees the turn input, then the two steered messages")
	require.ErrorIs(t, h.Steer("太晚了"), ErrHandleDone)
}

// 模型给出答复时还有待处理的 Steer：唤醒续跑，而不是结束。
func TestHandleSteerWakesStopping(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	model := newScriptModel(gated(started, release, answer("初稿")), answer("终稿"))
	agent, err := NewSimpleBuilder(model).WithTools(plainTool()).WithMaxIterations(4).Build()
	require.NoError(t, err)
	h := agent.Start(context.Background(), "写一段话")
	<-started
	require.NoError(t, h.Steer("要更短"))
	close(release)
	require.Equal(t, "终稿", waitAnswer(t, h).Result)
	require.Equal(t, []llm.Message{{Role: "assistant", Content: "初稿"}, llm.UserMessage("要更短")}, lastMessages(model.request(1), 2))
}

// Inject 随下一次请求发送，但不唤醒即将结束的轮次。
func TestHandleInjectDoesNotWake(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	model := newScriptModel(gated(started, release, answer("完成")))
	agent, err := NewSimpleBuilder(model).WithTools(plainTool()).WithMaxIterations(4).Build()
	require.NoError(t, err)
	h := agent.Start(context.Background(), "任务")
	<-started
	require.NoError(t, h.Inject("仅供参考"))
	close(release)
	require.Equal(t, "完成", waitAnswer(t, h).Result)
	require.Equal(t, 1, model.count())
}

// Redirect 取消进行中的模型请求，保留已流出的文本，追加纠正后重新请求。
func TestHandleRedirect(t *testing.T) {
	streamed := make(chan struct{})
	slow := step{run: func(ctx context.Context, send func(*llm.StreamChunk) bool) {
		send(&llm.StreamChunk{Delta: "我先介绍 A"})
		close(streamed)
		<-ctx.Done()
	}}
	model := newScriptModel(slow, answer("关于 B 的回答"))
	agent, err := NewSimpleBuilder(model).WithTools(plainTool()).WithMaxIterations(4).Build()
	require.NoError(t, err)
	h := agent.Start(context.Background(), "介绍一下")
	<-streamed
	require.NoError(t, h.Redirect("不要 A，讲 B"))
	require.Equal(t, "关于 B 的回答", waitAnswer(t, h).Result)
	require.Equal(t, []llm.Message{{Role: "assistant", Content: "我先介绍 A"}, llm.UserMessage("不要 A，讲 B")}, lastMessages(model.request(1), 2))
}

// Followup 在本轮结束后作为新一轮输入，续接本轮的对话。
func TestHandleFollowup(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	model := newScriptModel(gated(started, release, answer("一")), answer("二"))
	agent, err := NewSimpleBuilder(model).WithTools(plainTool()).WithMaxIterations(4).Build()
	require.NoError(t, err)
	h := agent.Start(context.Background(), "第一问")
	<-started
	require.NoError(t, h.Followup("第二问"))
	close(release)
	require.Equal(t, "二", waitAnswer(t, h).Result)
	second := model.request(1).Messages
	require.Equal(t, []llm.Message{llm.UserMessage("第一问"), {Role: "assistant", Content: "一"}, llm.UserMessage("第二问")}, second[len(second)-3:])
}

func TestHandleInterrupt(t *testing.T) {
	started := make(chan struct{})
	model := newScriptModel(gated(started, make(chan struct{}), answer("不会到达")))
	agent, err := NewSimpleBuilder(model).WithTools(plainTool()).Build()
	require.NoError(t, err)
	h := agent.Start(context.Background(), "任务")
	<-started
	boom := errors.New("用户取消")
	h.Interrupt(boom)
	_, err = h.Wait(context.Background())
	require.ErrorIs(t, err, boom)

	noTools, err := NewSimpleBuilder(newScriptModel()).Build()
	require.NoError(t, err)
	_, err = noTools.Start(context.Background(), "x").Wait(context.Background())
	require.ErrorContains(t, err, "原生工具流")
}

func TestTurnStopping(t *testing.T) {
	model := newScriptModel(answer("草稿"), answer("定稿"))
	var seen []string
	var continued []int
	hook := func(_ context.Context, s *Stopping) (string, error) {
		seen = append(seen, s.Answer)
		continued = append(continued, s.Continued)
		if len(seen) == 1 {
			return "检查是否满足要求", nil
		}
		return "", nil
	}
	agent, err := NewSimpleBuilder(model).WithTools(plainTool()).WithMaxIterations(4).WithTurnStopping(hook).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "写")
	require.NoError(t, err)
	require.Equal(t, "定稿", out.Result)
	require.Equal(t, []string{"草稿", "定稿"}, seen)
	require.Equal(t, []int{0, 1}, continued)
	require.Equal(t, []llm.Message{{Role: "assistant", Content: "草稿"}, llm.UserMessage("检查是否满足要求")}, lastMessages(model.request(1), 2))

	// 没有剩余额度时不再续跑。
	model = newScriptModel(answer("唯一"))
	agent, err = NewSimpleBuilder(model).WithTools(plainTool()).WithMaxIterations(1).WithTurnStopping(func(context.Context, *Stopping) (string, error) { return "继续", nil }).Build()
	require.NoError(t, err)
	out, err = agent.AsRunnable().Invoke(context.Background(), "写")
	require.NoError(t, err)
	require.Equal(t, "唯一", out.Result)
}

func TestPreStep(t *testing.T) {
	model := newScriptModel(answer("答"))
	rewrite := func(_ context.Context, s *Step) (PreStepResult, error) {
		s.Messages = append(s.Messages, llm.UserMessage("（已压缩历史）"))
		return PreStepResult{}, nil
	}
	agent, err := NewSimpleBuilder(model).WithTools(plainTool()).WithPreStep(rewrite).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "问")
	require.NoError(t, err)
	require.Equal(t, llm.UserMessage("（已压缩历史）"), lastMessages(model.request(0), 1)[0])

	halted := newScriptModel()
	agent, err = NewSimpleBuilder(halted).WithTools(plainTool()).WithPreStep(func(context.Context, *Step) (PreStepResult, error) {
		return PreStepResult{Halt: true, Answer: "输入被拒绝"}, nil
	}).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "问")
	require.NoError(t, err)
	require.Equal(t, "输入被拒绝", out.Result)
	require.Zero(t, halted.count())
}

func TestRequestHooks(t *testing.T) {
	primary, routed := newScriptModel(), newScriptModel(answer("由备用模型回答"))
	route := func(_ context.Context, r *ModelRequest) error {
		r.Client = routed
		r.Request.Temperature = 0.3
		return nil
	}
	agent, err := NewSimpleBuilder(primary).WithTools(plainTool()).WithRequestHook(route).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "问")
	require.NoError(t, err)
	require.Equal(t, "由备用模型回答", out.Result)
	require.Zero(t, primary.count())
	require.Equal(t, 0.3, routed.request(0).Temperature)
}

func TestRequestErrorRecovery(t *testing.T) {
	tooLong := &llm.ProviderError{StatusCode: 400, Err: errors.New("This model's maximum context length is 8192 tokens")}
	model := newScriptModel(failing(tooLong), answer("压缩后成功"))
	var classes []llm.ClassifiedError
	compress := func(_ context.Context, f *RequestFailure) (RequestErrorDecision, error) {
		classes = append(classes, f.Class)
		if !f.Class.ShouldCompress {
			return RequestErrorDecision{}, nil
		}
		return RequestErrorDecision{Action: RequestRetry, Messages: f.Messages[len(f.Messages)-1:]}, nil
	}
	agent, err := NewSimpleBuilder(model).WithSystemPrompt("系统").WithTools(plainTool()).WithRequestError(0, compress).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "问")
	require.NoError(t, err)
	require.Equal(t, "压缩后成功", out.Result)
	require.Equal(t, llm.ReasonContextLength, classes[0].Reason)
	require.Equal(t, []llm.Message{llm.UserMessage("问")}, model.request(1).Messages, "compressed messages replace the history")

	// 回退到另一个客户端；超过恢复次数后以原错误失败。
	down := &llm.ProviderError{StatusCode: 503, Err: errors.New("unavailable")}
	backup := newScriptModel(answer("备用"))
	agent, err = NewSimpleBuilder(newScriptModel(failing(down))).WithTools(plainTool()).WithRequestError(1, func(_ context.Context, f *RequestFailure) (RequestErrorDecision, error) {
		return RequestErrorDecision{Action: RequestRetry, Client: backup}, nil
	}).Build()
	require.NoError(t, err)
	out, err = agent.AsRunnable().Invoke(context.Background(), "问")
	require.NoError(t, err)
	require.Equal(t, "备用", out.Result)

	agent, err = NewSimpleBuilder(newScriptModel(failing(down), failing(down))).WithTools(plainTool()).WithRequestError(1, func(context.Context, *RequestFailure) (RequestErrorDecision, error) {
		return RequestErrorDecision{Action: RequestRetry}, nil
	}).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "问")
	require.ErrorIs(t, err, down)
}

// system 段落在同一轮内字节不变；运行上下文每步重算、附在请求末尾，不进入之后的历史。
func TestPromptPrefixStable(t *testing.T) {
	model := newScriptModel(callTool("c1", "lookup"), answer("答"))
	var step atomic.Int32
	agent, err := NewSimpleBuilder(model).WithSystemPrompt("你是助手").WithTools(plainTool()).WithMaxIterations(4).
		WithPromptSections(PromptSection{Name: "rules", Order: 1, Text: func(PromptScope) string { return "规则：简洁" }},
			PromptSection{Name: "identity", Order: 0, Text: func(PromptScope) string { return "身份：编码助手" }}).
		WithPromptContexts(PromptContext{Name: "step", Text: func(s PromptScope) string {
			step.Add(1)
			return "当前步骤 " + string(rune('0'+s.Step))
		}}).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "问")
	require.NoError(t, err)
	first, second := model.request(0), model.request(1)
	require.Equal(t, first.Messages[0], second.Messages[0])
	require.Equal(t, "system", first.Messages[0].Role)
	require.Contains(t, first.Messages[0].Content, "你是助手\n\n身份：编码助手\n\n规则：简洁")
	require.Equal(t, stepContextMessage("当前步骤 0"), lastMessages(first, 1)[0])
	require.Equal(t, stepContextMessage("当前步骤 1"), lastMessages(second, 1)[0])
	for _, m := range second.Messages[:len(second.Messages)-1] {
		require.NotEqual(t, stepContextMessage("当前步骤 0"), m, "contexts do not enter the history")
	}
	require.Equal(t, first.Messages[:len(first.Messages)-1], second.Messages[:len(first.Messages)-1], "the prefix before the context is unchanged")
}

// 持久运行中钩子与上下文的结果记入日志，恢复时回放，不重新计算也不重复请求模型。
func TestLoopHooksDurableReplay(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "loop"), store)
	var stops, contexts atomic.Int32
	build := func(model *scriptModel) *ConfigurableAgent[any, *core.AgentState] {
		dispatched, err := dispatcherTools(tools.DispatchConfig{}, plainTool())
		require.NoError(t, err)
		agent, err := NewSimpleBuilder(model).WithDurableScope("hooks").WithTools(dispatched...).WithMaxIterations(4).
			WithTurnStopping(func(context.Context, *Stopping) (string, error) {
				if stops.Add(1) == 1 {
					return "再想想", nil
				}
				return "", nil
			}).
			WithPromptContexts(PromptContext{Name: "n", Text: func(PromptScope) string {
				return "第 " + string(rune('0'+contexts.Add(1))) + " 次计算"
			}}).Build()
		require.NoError(t, err)
		return agent
	}
	out, err := build(newScriptModel(answer("初答"), answer("复答"))).AsRunnable().Invoke(ctx, "问")
	require.NoError(t, err)
	require.Equal(t, "复答", out.Result)
	require.EqualValues(t, 2, stops.Load())
	require.EqualValues(t, 2, contexts.Load())

	replay := newScriptModel()
	out, err = build(replay).AsRunnable().Invoke(ctx, "问")
	require.NoError(t, err)
	require.Equal(t, "复答", out.Result)
	require.Zero(t, replay.count())
	require.EqualValues(t, 2, stops.Load(), "the journaled decision is replayed")
	require.EqualValues(t, 2, contexts.Load())
}
