package reflection

import (
	"context"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/testing/panictest"
)

// panicMemory 的 GetByType panic，模拟宿主实现的记忆。
type panicMemory struct{ memory.EnhancedMemory }

func (panicMemory) GetByType(context.Context, memory.MemoryType, int) ([]*memory.MemoryEntry, error) {
	panic("memory boom")
}

// 周期反思在后台 goroutine 中运行：宿主记忆 panic 时进程不得退出，panic 交给全局 PanicHandler 记录，
// 反思循环继续下一轮（收到多次 panic）。
func TestBackgroundReflectionPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		recorder := &panictest.Recorder{}
		core.SetGlobalMetricsCollector(recorder)
		agent := NewSelfReflectiveAgentWithContext(context.Background(), nil, panicMemory{}, WithReflectionInterval(10*time.Millisecond))
		events := recorder.Wait(2, 2*time.Second)
		if len(events) < 2 || events[0].Component != "self_reflective_agent" || events[0].Value != "memory boom" {
			t.Fatalf("反思循环应在 panic 后继续：%+v", events)
		}
		if err := agent.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}
