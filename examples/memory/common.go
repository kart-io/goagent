package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
)

// clock 是可推进的假时钟：TTL、半衰期与保留期都经 Config.Now 读取它，不需要真实等待。
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock {
	return &clock{t: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)}
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// fakeEmbedder 按概念词表把文本映射到固定维度：同一概念的不同说法落在同一维，结果只取决于文本。
// shift 把概念轮换到其他维度，模拟换了一个向量空间不兼容的嵌入模型。
type fakeEmbedder struct {
	concepts [][]string
	shift    int
}

func (e fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	n := len(e.concepts)
	v := make([]float32, n+1)
	// 偏置维避免零向量，MemoryIndex 拒绝零向量。
	v[n] = 0.05
	for i, words := range e.concepts {
		for _, w := range words {
			if strings.Contains(text, w) {
				v[(i+e.shift)%n]++
			}
		}
	}
	return v, nil
}

// cachedClient 声明模型身份：ScriptedClient 没有模型名，不声明 CacheIdentity 时模型调用缓存不会生效。
type cachedClient struct{ *mocks.ScriptedClient }

func (cachedClient) CacheIdentity() string { return "scripted-demo" }

// env 是一个场景独占的装配：独立目录的 FileStore、进程内会话日志与假时钟。
type env struct {
	store    *durable.FileStore
	sessions *session.MemoryStore
	clock    *clock
	mem      *memory.Memory
}

func newEnv(root, name string, cfg memory.Config) (*env, error) {
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	fs, err := durable.NewFileStore(dir)
	if err != nil {
		return nil, err
	}
	e := &env{store: fs, sessions: session.NewMemoryStore(), clock: newClock()}
	cfg.Durable = fs
	cfg.Sessions = e.sessions
	if cfg.Now == nil {
		cfg.Now = e.clock.Now
	}
	if e.mem, err = memory.New(cfg); err != nil {
		return nil, err
	}
	return e, nil
}

// agent 按长期记忆的标准装配构建主 Agent；extra 用于追加模型缓存等选项。
func (e *env) agent(chat *mocks.ScriptedClient, extra func(*builder.SimpleAgentBuilder) *builder.SimpleAgentBuilder) (*builder.ConfigurableAgent[*core.AgentState], error) {
	b := builder.NewSimpleBuilder(cachedClient{chat}).
		WithSessionLog(e.sessions).
		WithLongTermMemory(e.mem).
		WithTools(e.mem.Tools()...)
	if extra != nil {
		b = extra(b)
	}
	return b.Build()
}

// turn 以 ns 与会话 sid 跑一轮，返回本轮登记的抽取任务 Key（"会话/turn/start 序号"）。
func (e *env) turn(ctx context.Context, a *builder.ConfigurableAgent[*core.AgentState], ns []string, sid, input string) (string, *builder.AgentOutput, error) {
	ctx = tools.WithSessionID(memory.WithNamespace(ctx, ns...), sid)
	out, err := a.AsRunnable().Invoke(ctx, input)
	if err != nil {
		return "", nil, err
	}
	key, err := e.lastTurnKey(ctx, sid)
	return key, out, err
}

func (e *env) lastTurnKey(ctx context.Context, sid string) (string, error) {
	evs, err := e.sessions.Read(ctx, sid, 0)
	if err != nil {
		return "", err
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type == constants.SessionEventTurnStart {
			return fmt.Sprintf("%s/%d", sid, evs[i].Seq), nil
		}
	}
	return "", fmt.Errorf("会话 %s 没有 turn/start", sid)
}

// regenerate 只退回会话日志、不调用 Memory.Rewound，模拟"重新生成"以得到完全相同的请求。
// 按宿主契约 Rewind 之后应调用 Rewound，而它会使该命名空间的缓存全部失效（见 eraseOps 的 Rewound 行）。
func (e *env) regenerate(ctx context.Context, sid string) error {
	evs, err := e.sessions.Read(ctx, sid, 0)
	if err != nil {
		return err
	}
	_, err = session.Rewind(ctx, e.sessions, sid, 0, session.LastSeq(evs))
	return err
}

// check 是一条断言：got 与 want 深度相等才算通过，失败信息带上实际观察到的值。
type check struct {
	what      string
	got, want any
}

func verify(checks ...check) error {
	var errs []error
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			errs = append(errs, fmt.Errorf("%s：期望 %v，实际 %v", c.what, c.want, c.got))
		}
	}
	return errors.Join(errs...)
}

func contents(rs []memory.Scored) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Content)
	}
	return out
}

func extractReply(name string, contents ...string) mocks.ScriptedRule {
	return mocks.TextReply(name, extractJSON(contents...))
}

// extractJSON 生成抽取模型的回复，格式取自抽取请求的系统提示词。
func extractJSON(contents ...string) string {
	items := make([]string, len(contents))
	for i, c := range contents {
		items[i] = fmt.Sprintf(`{"kind":"fact","content":%q,"keywords":[],"entities":[],"importance":0.8}`, c)
	}
	return `{"memories":[` + strings.Join(items, ",") + `]}`
}

// route 按请求中出现的标记选择回复：Worker 处理任务的先后不固定时，用它代替按顺序消费的脚本。
type route struct {
	marker string
	reply  string
	err    error
	// before 在回复前执行，用于在模型调用期间制造并发修改。
	before func() error
}

// routedClient 返回最多接受 n 次请求的严格客户端；没有标记匹配的请求视为意外请求并报错。
func routedClient(n int, routes ...route) *mocks.ScriptedClient {
	respond := func(_ context.Context, req *llm.CompletionRequest) (mocks.ScriptedReply, error) {
		for _, r := range routes {
			for _, m := range req.Messages {
				if !strings.Contains(m.Content, r.marker) {
					continue
				}
				if r.before != nil {
					if err := r.before(); err != nil {
						return mocks.ScriptedReply{}, err
					}
				}
				return mocks.ScriptedReply{Text: r.reply}, r.err
			}
		}
		return mocks.ScriptedReply{}, fmt.Errorf("意外的模型请求：%v", req.Messages)
	}
	rules := make([]mocks.ScriptedRule, n)
	for i := range rules {
		rules[i] = mocks.ScriptedRule{Name: fmt.Sprintf("路由#%d", i), Respond: respond}
	}
	return mocks.NewScriptedClient(rules...)
}

// requestText 把一次请求的全部消息拼成文本，便于断言其中出现或不出现的内容。
func requestText(req llm.CompletionRequest) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Content)
		b.WriteByte('\n')
		for _, c := range m.ToolCalls {
			b.WriteString(c.Function.Name)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// sibling 在同一份存储上再开一个 Memory 实例（如换了权重或嵌入模型的新版本程序），共享时钟与会话日志。
func (e *env) sibling(cfg memory.Config) (*memory.Memory, error) {
	cfg.Durable = e.store
	cfg.Sessions = e.sessions
	if cfg.Now == nil {
		cfg.Now = e.clock.Now
	}
	return memory.New(cfg)
}
