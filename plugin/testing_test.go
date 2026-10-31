package plugin

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/tools"
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
