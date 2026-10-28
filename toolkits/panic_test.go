package toolkits

import (
	"context"
	"errors"
	"testing"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/testing/panictest"
)

// stubTool 直接实现 interfaces.Tool，panics 为真时 Invoke panic。
type stubTool struct {
	name   string
	panics bool
}

func (s stubTool) Name() string        { return s.name }
func (s stubTool) Description() string { return s.name }
func (s stubTool) ArgsSchema() string  { return `{"type":"object"}` }
func (s stubTool) Invoke(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	if s.panics {
		panic("toolkit boom")
	}
	return &interfaces.ToolOutput{Result: s.name, Success: true}, nil
}

// 并行执行时工具在框架 goroutine 中 panic：进程不得退出，返回可识别的 PanicError，其他工具照常完成。
func TestToolkitParallelToolPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		executor := NewToolkitExecutor(NewBaseToolkit(stubTool{name: "ok"}, stubTool{name: "boom", panics: true})).WithParallel(true)
		results, err := executor.ExecuteMultiple(context.Background(), map[string]*interfaces.ToolInput{"ok": {}, "boom": {}})
		var p *agentErrors.PanicError
		if !errors.As(err, &p) || p.Value != "toolkit boom" {
			t.Fatalf("应返回 PanicError：%v", err)
		}
		if results["ok"] == nil || results["ok"].Result != "ok" {
			t.Fatalf("其他工具应照常完成：%+v", results)
		}
	})
}
