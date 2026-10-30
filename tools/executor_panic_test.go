package tools

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/testing/panictest"
)

// panicTool 直接实现 interfaces.Tool，不经 BaseTool 的保护，模拟宿主自定义工具。
type panicTool struct {
	name  string
	calls atomic.Int32
}

func (p *panicTool) Name() string        { return p.name }
func (p *panicTool) Description() string { return "panic tool" }
func (p *panicTool) ArgsSchema() string  { return `{"type":"object"}` }
func (p *panicTool) Invoke(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	p.calls.Add(1)
	panic(p.name + " boom")
}

func requirePanicValue(t *testing.T, err error, want string) {
	t.Helper()
	var p *agentErrors.PanicError
	if !errors.As(err, &p) || p.Value != want {
		t.Fatalf("错误应能识别为 PanicError(%q)：%v", want, err)
	}
}

// 并行 goroutine 中工具 panic 时进程不得退出：该调用的结果是 PanicError 且不重试，其他调用照常完成。
func TestToolExecutorParallelToolPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		boom := &panicTool{name: "boom"}
		executor := NewToolExecutor(WithRetryPolicy(&RetryPolicy{MaxRetries: 2, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, Multiplier: 1}))
		results, err := executor.ExecuteParallel(context.Background(), []*ToolCallRequest{
			{ID: "ok", Tool: NewMockTool("ok", 0, false)},
			{ID: "boom", Tool: boom},
		})
		if err == nil {
			t.Fatal("有工具失败时应返回错误")
		}
		if results[0].Error != nil || results[0].Output == nil {
			t.Fatalf("其他工具应照常完成：%+v", results[0])
		}
		requirePanicValue(t, results[1].Error, "boom boom")
		if n := boom.calls.Load(); n != 1 {
			t.Fatalf("panic 不应重试，调用次数 = %d", n)
		}
	})
}

// errorHandler 在并行 goroutine 中 panic 时进程不得退出，它的 panic 作为该调用的错误结果。
func TestToolExecutorErrorHandlerPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		executor := NewToolExecutor(
			WithRetryPolicy(&RetryPolicy{}),
			WithErrorHandler(func(*ToolCallRequest, error) error { panic("handler boom") }),
		)
		results, _ := executor.ExecuteParallel(context.Background(), []*ToolCallRequest{
			{ID: "fail", Tool: NewMockTool("fail", 0, true)},
			{ID: "ok", Tool: NewMockTool("ok", 0, false)},
		})
		requirePanicValue(t, results[0].Error, "handler boom")
		if results[1].Error != nil {
			t.Fatalf("其他工具应照常完成：%v", results[1].Error)
		}
	})
}

// 调用方 goroutine 上的顺序执行同样返回 PanicError，而不是让 panic 穿透。
func TestToolExecutorSequentialToolPanicReturnsPanicError(t *testing.T) {
	_, err := NewToolExecutor(WithRetryPolicy(&RetryPolicy{})).ExecuteSequential(context.Background(), []*ToolCallRequest{{ID: "boom", Tool: &panicTool{name: "boom"}}})
	requirePanicValue(t, err, "boom boom")
}
