package builder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/builder/contextpolicy"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/core/observe"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

// 子进程执行：ExecuteStream 内部 goroutine 中宿主代码 panic 时进程不得退出，调用方拿到 *PanicError。
// 用子进程是为了在回归时只让子进程崩溃，而不是整个测试二进制。
const panicCaseEnv = "GOAGENT_PANIC_CASE"

type panicPolicy struct{ where string }

func (p *panicPolicy) Descriptor() contextpolicy.Descriptor {
	return contextpolicy.Descriptor{ID: "panic", Version: "v1", ConfigHash: "h", Durable: true}
}
func (p *panicPolicy) BeforeTurn(_ context.Context, t contextpolicy.Turn) (contextpolicy.Turn, error) {
	if p.where == "before_turn" {
		panic("before_turn boom")
	}
	return t, nil
}
func (p *panicPolicy) Stats() map[string]int64 {
	if p.where == "stats" {
		panic("stats boom")
	}
	return nil
}

type panicMiddleware struct{}

func (panicMiddleware) Name() string { return "panic" }
func (panicMiddleware) OnBefore(context.Context, *middleware.MiddlewareRequest) (*middleware.MiddlewareRequest, error) {
	panic("middleware boom")
}
func (panicMiddleware) OnAfter(_ context.Context, r *middleware.MiddlewareResponse) (*middleware.MiddlewareResponse, error) {
	return r, nil
}
func (panicMiddleware) OnError(_ context.Context, err error) error { return err }

type panicModel struct{ *nativeEventModel }

func (m *panicModel) CompleteToolsStream(context.Context, *llm.CompletionRequest, []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	panic("model boom")
}

// concurrentPanicTool 不经 Dispatcher 直接声明可并发，覆盖并发派发路径自身的恢复。
type concurrentPanicTool struct{ interfaces.Tool }

func (concurrentPanicTool) SupportsConcurrentCalls() bool { return true }

type printObserver struct{}

func (printObserver) Observe(_ context.Context, e observe.Event) {
	fmt.Printf("OBSERVE %s kind=%q\n", e.Type, e.ErrorKind)
}

func panicTool(name string) interfaces.Tool {
	return tools.NewBaseTool(name, name, `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		panic(name + " boom")
	})
}

func answerModel(toolCalls ...llm.ToolCall) *nativeEventModel {
	if len(toolCalls) == 0 {
		return nativeModel([]*llm.StreamChunk{{Delta: "答"}, {Done: true, FinishReason: "stop"}})
	}
	return nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: toolCalls}},
		[]*llm.StreamChunk{{Delta: "答"}, {Done: true, FinishReason: "stop"}},
	)
}

func dispatcherTools(cfg tools.DispatchConfig, tool interfaces.Tool) ([]interfaces.Tool, error) {
	reg := tools.NewRegistry()
	if err := reg.Register(tool); err != nil {
		return nil, err
	}
	d, err := tools.NewDispatcher(reg, cfg)
	if err != nil {
		return nil, err
	}
	return d.Tools(), nil
}

// runPanicCase 在子进程中执行；调用方仍用 defer recover 包裹，以区分“框架转为错误”和“调用方接住 panic”。
func runPanicCase(name string) error {
	ctx := observe.WithObserver(context.Background(), printObserver{})
	lookup := NewMockTool("lookup", "x")
	policy := func(where string) contextpolicy.Factory {
		return func() contextpolicy.Policy { return &panicPolicy{where: where} }
	}
	var b *SimpleAgentBuilder
	switch name {
	case "factory":
		b = NewSimpleBuilder(answerModel()).WithTools(lookup).WithContextPolicies(func() contextpolicy.Policy { panic("factory boom") })
	case "before_turn", "stats":
		b = NewSimpleBuilder(answerModel()).WithTools(lookup).WithContextPolicies(policy(name))
	case "raw_tool":
		b = NewSimpleBuilder(answerModel(nativeCall("c1", "boom", "{}"))).WithTools(panicTool("boom")).WithMaxIterations(3)
	case "concurrent_tool":
		b = NewSimpleBuilder(answerModel(nativeCall("c1", "boom", "{}"), nativeCall("c2", "boom", "{}"))).
			WithTools(concurrentPanicTool{panicTool("boom")}).WithParallelToolCalls(2).WithMaxIterations(3)
	case "model":
		b = NewSimpleBuilder(&panicModel{answerModel()}).WithTools(lookup)
	case "middleware_stream":
		b = NewSimpleBuilder(answerModel()).WithTools(lookup).WithMiddleware(panicMiddleware{})
	case "dispatcher_tool", "dispatcher_durable_tool", "dispatcher_on_result":
		cfg, tool := tools.DispatchConfig{}, panicTool("boom")
		if name == "dispatcher_on_result" {
			cfg.OnResult = func(context.Context, tools.DispatchResult) error { panic("on_result boom") }
			tool = NewMockTool("boom", "x")
		}
		ts, err := dispatcherTools(cfg, tool)
		if err != nil {
			return err
		}
		b = NewSimpleBuilder(answerModel(nativeCall("c1", "boom", "{}"))).WithTools(ts...).WithMaxIterations(3)
		if name == "dispatcher_durable_tool" {
			store, err := durable.NewFileStore(os.Getenv(panicCaseEnv + "_DIR"))
			if err != nil {
				return err
			}
			ctx = durable.WithJournal(durable.WithExecutionKey(ctx, "node"), store)
			b = b.WithDurableScope("panic")
		}
	case "stream_error_handler":
		b = NewSimpleBuilder(answerModel(nativeCall("c1", "boom", "{}"))).WithTools(panicTool("boom")).WithMaxIterations(3).
			WithErrorHandler(panicErrorHandler)
	case "stream_on_error":
		b = NewSimpleBuilder(answerModel(nativeCall("c1", "boom", "{}"))).WithTools(panicTool("boom")).WithMaxIterations(3).
			WithCallbacks(&panicCallback{where: "OnError"})
	case "stream_on_end":
		b = NewSimpleBuilder(answerModel()).WithTools(lookup).WithCallbacks(&panicCallback{where: "OnEnd"})
	case "execute_sync_middleware":
		agent, err := NewSimpleBuilder(NewMockLLMClient("ok")).WithMiddleware(panicMiddleware{}).Build()
		if err != nil {
			return err
		}
		_, err = func() (out *AgentOutput, e error) {
			defer func() {
				if v := recover(); v != nil {
					e = fmt.Errorf("CALLER_RECOVERED: %v", v)
				}
			}()
			return agent.Execute(ctx, "hi")
		}()
		return err
	default:
		return fmt.Errorf("unknown case %q", name)
	}
	agent, err := b.Build()
	if err != nil {
		return err
	}
	return func() (e error) {
		defer func() {
			if v := recover(); v != nil {
				e = fmt.Errorf("CALLER_RECOVERED: %v", v)
			}
		}()
		_, e = agent.AsRunnable().Invoke(ctx, "hi")
		return e
	}()
}

func TestPanicChild(t *testing.T) {
	name := os.Getenv(panicCaseEnv)
	if name == "" {
		t.Skip("仅在子进程中运行")
	}
	err := runPanicCase(name)
	var p *agentErrors.PanicError
	if errors.As(err, &p) {
		fmt.Printf("PANIC_ERROR op=%q value=%q stack_at_origin=%v\n", p.Op, fmt.Sprint(p.Value), bytes.Contains(p.Stack, []byte("panic_test.go")))
	}
	fmt.Printf("CHILD_SURVIVED eof=%v err=%v\n", errors.Is(err, io.ErrUnexpectedEOF), err)
}

func TestStreamPanicDoesNotKillProcess(t *testing.T) {
	for _, tc := range []struct {
		name, wantValue string
		// syncCaller 表示同步 Execute：不经 agent 观测，只断言 panic 已转为 *PanicError。
		syncCaller bool
	}{
		{name: "factory", wantValue: "factory boom"},
		{name: "before_turn", wantValue: "before_turn boom"},
		{name: "stats", wantValue: "stats boom"},
		{name: "raw_tool", wantValue: "boom boom"},
		{name: "concurrent_tool", wantValue: "boom boom"},
		{name: "model", wantValue: "model boom"},
		{name: "middleware_stream", wantValue: "middleware boom"},
		{name: "dispatcher_tool", wantValue: "boom boom"},
		{name: "dispatcher_durable_tool", wantValue: "boom boom"},
		{name: "dispatcher_on_result", wantValue: "on_result boom"},
		{name: "stream_error_handler", wantValue: "error_handler boom"},
		{name: "stream_on_error", wantValue: "OnError boom"},
		{name: "stream_on_end", wantValue: "OnEnd boom"},
		{name: "execute_sync_middleware", wantValue: "middleware boom", syncCaller: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestPanicChild$", "-test.v")
			cmd.Env = append(os.Environ(), panicCaseEnv+"="+tc.name, panicCaseEnv+"_DIR="+t.TempDir())
			out, err := cmd.CombinedOutput()
			text := string(out)
			if err != nil || strings.Contains(text, "panic:") {
				t.Fatalf("子进程崩溃：%v\n%s", err, text)
			}
			want := []string{
				"CHILD_SURVIVED eof=false",
				fmt.Sprintf("value=%q stack_at_origin=true", tc.wantValue),
			}
			if !tc.syncCaller {
				// agent 观测以失败结束，而不是被 panic 展开时记成成功。
				want = append(want, `OBSERVE agent_error kind="panic"`)
			}
			for _, w := range want {
				if !strings.Contains(text, w) {
					t.Fatalf("缺少 %q\n%s", w, text)
				}
			}
			if strings.Contains(text, "CALLER_RECOVERED") || strings.Contains(text, "OBSERVE agent_end") {
				t.Fatalf("panic 未在框架内转为错误\n%s", text)
			}
		})
	}
}

type panicCompleteModel struct{ *MockLLMClient }

func (panicCompleteModel) Complete(context.Context, *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	panic("complete boom")
}

type countingErrorCallback struct {
	core.BaseCallback
	errs []error
}

func (c *countingErrorCallback) OnError(_ context.Context, err error) error {
	c.errs = append(c.errs, err)
	return nil
}

// 链内 panic 转为 *PanicError 后必须经过 errorHandler 与 OnError，各一次。
func TestExecutePanicReachesErrorHandlerAndOnError(t *testing.T) {
	for _, tc := range []struct {
		name, wantValue string
		build           func() *SimpleAgentBuilder
		stream          bool
	}{
		{name: "sync_middleware", wantValue: "middleware boom", build: func() *SimpleAgentBuilder {
			return NewSimpleBuilder(NewMockLLMClient("ok")).WithMiddleware(panicMiddleware{})
		}},
		{name: "sync_model", wantValue: "complete boom", build: func() *SimpleAgentBuilder {
			return NewSimpleBuilder(panicCompleteModel{NewMockLLMClient()})
		}},
		{name: "stream_tool", wantValue: "boom boom", stream: true, build: func() *SimpleAgentBuilder {
			return NewSimpleBuilder(answerModel(nativeCall("c1", "boom", "{}"))).WithTools(panicTool("boom")).WithMaxIterations(3)
		}},
		{name: "stream_middleware", wantValue: "middleware boom", stream: true, build: func() *SimpleAgentBuilder {
			return NewSimpleBuilder(answerModel()).WithTools(NewMockTool("lookup", "x")).WithMiddleware(panicMiddleware{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var handled []error
			cb := &countingErrorCallback{}
			agent, err := tc.build().
				WithErrorHandler(func(err error) error { handled = append(handled, err); return err }).
				WithCallbacks(cb).Build()
			if err != nil {
				t.Fatal(err)
			}
			err = func() (e error) {
				defer func() {
					if v := recover(); v != nil {
						e = fmt.Errorf("CALLER_RECOVERED: %v", v)
					}
				}()
				if tc.stream {
					_, e = agent.AsRunnable().Invoke(context.Background(), "hi")
				} else {
					_, e = agent.Execute(context.Background(), "hi")
				}
				return e
			}()
			var p *agentErrors.PanicError
			if !errors.As(err, &p) || fmt.Sprint(p.Value) != tc.wantValue {
				t.Fatalf("期望 *PanicError(value=%q)，实际 %v", tc.wantValue, err)
			}
			if len(handled) != 1 || !errors.As(handled[0], &p) {
				t.Fatalf("errorHandler 应收到一次 *PanicError，实际 %v", handled)
			}
			if len(cb.errs) != 1 || !errors.As(cb.errs[0], &p) {
				t.Fatalf("OnError 应收到一次 *PanicError，实际 %v", cb.errs)
			}
		})
	}
}

func panicErrorHandler(error) error { panic("error_handler boom") }

// panicCallback 在 where 指定的回调中 panic，并记录各回调被调用的次数。
type panicCallback struct {
	core.BaseCallback
	where string
	calls map[string]int
}

func (c *panicCallback) hit(name string) {
	if c.calls == nil {
		c.calls = map[string]int{}
	}
	c.calls[name]++
	if c.where == name {
		panic(name + " boom")
	}
}
func (c *panicCallback) OnStart(context.Context, interface{}) error { c.hit("OnStart"); return nil }
func (c *panicCallback) OnEnd(context.Context, interface{}) error   { c.hit("OnEnd"); return nil }
func (c *panicCallback) OnError(context.Context, error) error       { c.hit("OnError"); return nil }
func (c *panicCallback) OnShutdown(context.Context) error           { c.hit("OnShutdown"); return nil }

type failingCompleteModel struct{ *MockLLMClient }

func (failingCompleteModel) Complete(context.Context, *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return nil, errors.New("complete failed")
}

// legacyToolModel 只实现非流式工具调用，ExecuteWithTools 因此走旧工具循环：首轮请求工具 boom，之后直接回答。
type legacyToolModel struct {
	*MockLLMClient
	panicGenerate bool
	rounds        int
}

func (m *legacyToolModel) GenerateWithTools(context.Context, string, []interfaces.Tool) (*llm.ToolCallResponse, error) {
	if m.panicGenerate {
		panic("generate boom")
	}
	m.rounds++
	if m.rounds == 1 {
		return &llm.ToolCallResponse{ToolCalls: []llm.ToolCall{nativeCall("c1", "boom", "{}")}}, nil
	}
	return &llm.ToolCallResponse{Content: "答"}, nil
}

func (m *legacyToolModel) StreamWithTools(context.Context, string, []interfaces.Tool) (<-chan llm.ToolChunk, error) {
	return nil, errors.New("未实现")
}

// durablePanicTool 在 ExecuteStream 启动 goroutine 前的持久能力检查中 panic。
type durablePanicTool struct{ interfaces.Tool }

func (durablePanicTool) SupportsDurableCalls() bool { panic("durable check boom") }

// 同步入口中宿主代码的 panic 发生在调用方 goroutine，回归时调用方能接住，因此直接在本进程测试。
// frame 断言调用栈指向 panic 发生的函数，而不只是测试函数本身。
func TestSyncEntryPanicReturnsPanicError(t *testing.T) {
	type agentT = ConfigurableAgent[any, *core.AgentState]
	execute := func(ctx context.Context, agent *agentT, _ *panicCallback) error {
		_, err := agent.Execute(ctx, "hi")
		return err
	}
	executeWithTools := func(ctx context.Context, agent *agentT, _ *panicCallback) error {
		_, err := agent.ExecuteWithTools(ctx, "hi")
		return err
	}
	for _, tc := range []struct {
		// where 在 Build 之后才生效，Build 内部的 Initialize 不受影响。
		name, where, wantValue, frame string
		build                         func(cb *panicCallback) *SimpleAgentBuilder
		run                           func(ctx context.Context, agent *agentT, cb *panicCallback) error
		// wantCalls 断言回调 panic 后不再调用回调：发生 panic 的回调只调用一次，其他回调不被调用。
		wantCalls map[string]int
	}{
		{name: "execute_error_handler", wantValue: "error_handler boom", frame: "builder.panicErrorHandler",
			build: func(cb *panicCallback) *SimpleAgentBuilder {
				return NewSimpleBuilder(failingCompleteModel{NewMockLLMClient()}).WithErrorHandler(panicErrorHandler).WithCallbacks(cb)
			},
			run: execute, wantCalls: map[string]int{}},
		{name: "execute_on_error", where: "OnError", wantValue: "OnError boom", frame: "(*panicCallback).OnError",
			build: func(cb *panicCallback) *SimpleAgentBuilder {
				return NewSimpleBuilder(failingCompleteModel{NewMockLLMClient()}).WithCallbacks(cb, cb)
			},
			run: execute, wantCalls: map[string]int{"OnError": 1}},
		{name: "execute_on_end", where: "OnEnd", wantValue: "OnEnd boom", frame: "(*panicCallback).OnEnd",
			build: func(cb *panicCallback) *SimpleAgentBuilder {
				return NewSimpleBuilder(NewMockLLMClient("ok")).WithCallbacks(cb, cb)
			},
			run: execute, wantCalls: map[string]int{"OnEnd": 1}},
		{name: "build_on_start", where: "OnStart", wantValue: "OnStart boom", frame: "(*panicCallback).OnStart",
			build: func(cb *panicCallback) *SimpleAgentBuilder { return NewSimpleBuilder(NewMockLLMClient("ok")) },
			run: func(_ context.Context, _ *agentT, cb *panicCallback) error {
				_, err := NewSimpleBuilder(NewMockLLMClient("ok")).WithCallbacks(cb, cb).Build()
				return err
			},
			wantCalls: map[string]int{"OnStart": 1}},
		{name: "initialize_on_start", where: "OnStart", wantValue: "OnStart boom", frame: "(*panicCallback).OnStart",
			build: func(cb *panicCallback) *SimpleAgentBuilder {
				return NewSimpleBuilder(NewMockLLMClient("ok")).WithCallbacks(cb, cb)
			},
			run:       func(ctx context.Context, agent *agentT, _ *panicCallback) error { return agent.Initialize(ctx) },
			wantCalls: map[string]int{"OnStart": 1}},
		{name: "shutdown_on_shutdown", where: "OnShutdown", wantValue: "OnShutdown boom", frame: "(*panicCallback).OnShutdown",
			build: func(cb *panicCallback) *SimpleAgentBuilder {
				return NewSimpleBuilder(NewMockLLMClient("ok")).WithCallbacks(cb, cb)
			},
			run:       func(ctx context.Context, agent *agentT, _ *panicCallback) error { return agent.Shutdown(ctx) },
			wantCalls: map[string]int{"OnShutdown": 1}},
		{name: "legacy_tool", wantValue: "boom boom", frame: "builder.panicTool.",
			build: func(cb *panicCallback) *SimpleAgentBuilder {
				return NewSimpleBuilder(&legacyToolModel{MockLLMClient: NewMockLLMClient()}).WithTools(panicTool("boom")).WithCallbacks(cb)
			},
			run: executeWithTools, wantCalls: map[string]int{}},
		{name: "legacy_model", wantValue: "generate boom", frame: "(*legacyToolModel).GenerateWithTools",
			build: func(cb *panicCallback) *SimpleAgentBuilder {
				return NewSimpleBuilder(&legacyToolModel{MockLLMClient: NewMockLLMClient(), panicGenerate: true}).
					WithTools(NewMockTool("boom", "x")).WithCallbacks(cb)
			},
			run: executeWithTools, wantCalls: map[string]int{}},
		{name: "stream_start", wantValue: "durable check boom", frame: "durablePanicTool.SupportsDurableCalls",
			build: func(cb *panicCallback) *SimpleAgentBuilder {
				return NewSimpleBuilder(answerModel()).WithTools(durablePanicTool{NewMockTool("boom", "x")}).
					WithDurableScope("panic").WithCallbacks(cb)
			},
			run: func(ctx context.Context, agent *agentT, _ *panicCallback) error {
				store, err := durable.NewFileStore(t.TempDir())
				if err != nil {
					return err
				}
				ctx = durable.WithJournal(durable.WithExecutionKey(ctx, "node"), store)
				// 连续两次：第一次 panic 后执行权必须已释放，第二次才不会卡在执行闸门上。
				for i := 0; i < 2; i++ {
					runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					_, err = agent.AsRunnable().Invoke(runCtx, "hi")
					cancel()
					if errors.Is(err, context.DeadlineExceeded) {
						return err
					}
				}
				return err
			},
			wantCalls: map[string]int{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cb := &panicCallback{}
			agent, err := tc.build(cb).Build()
			if err != nil {
				t.Fatal(err)
			}
			cb.where, cb.calls = tc.where, nil
			err = func() (e error) {
				defer func() {
					if v := recover(); v != nil {
						e = fmt.Errorf("CALLER_RECOVERED: %v", v)
					}
				}()
				return tc.run(context.Background(), agent, cb)
			}()
			var p *agentErrors.PanicError
			if !errors.As(err, &p) || fmt.Sprint(p.Value) != tc.wantValue {
				t.Fatalf("期望 *PanicError(value=%q)，实际 %v", tc.wantValue, err)
			}
			if !bytes.Contains(p.Stack, []byte(tc.frame)) {
				t.Fatalf("调用栈未指向 panic 发生处 %q：\n%s", tc.frame, p.Stack)
			}
			if fmt.Sprint(cb.calls) != fmt.Sprint(tc.wantCalls) {
				t.Fatalf("回调调用次数 %v，期望 %v", cb.calls, tc.wantCalls)
			}
		})
	}
}
