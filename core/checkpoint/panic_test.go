package checkpoint

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	agentstate "github.com/kart-io/goagent/core/state"
	"github.com/kart-io/goagent/testing/panictest"
)

// panicBackend 的 Save 与 Ping 在宿主后端中 panic，calls 记录 Save 调用次数。
type panicBackend struct {
	*InMemorySaver
	saves atomic.Int32
}

func (p *panicBackend) Save(context.Context, string, agentstate.State) error {
	p.saves.Add(1)
	panic("backend boom")
}

func (p *panicBackend) Ping(context.Context) error { panic("ping boom") }

// 异步复制与健康检查在后台 goroutine 中调用存储后端：后端 panic 时进程不得退出，
// 复制按失败忽略且 worker 继续处理后续任务，健康检查把该后端记为不健康。
func TestDistributedCheckpointerBackendPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		secondary := &panicBackend{InMemorySaver: NewInMemorySaver()}
		dc, err := NewDistributedCheckpointer(&DistributedCheckpointerConfig{
			PrimaryBackend:      NewInMemorySaver(),
			SecondaryBackend:    secondary,
			EnableReplication:   true,
			ReplicationMode:     ReplicationModeAsync,
			HealthCheckInterval: 10 * time.Millisecond,
		})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 5; i++ {
			if err := dc.Save(context.Background(), "t", agentstate.NewAgentState()); err != nil {
				t.Fatal(err)
			}
		}
		deadline := time.Now().Add(2 * time.Second)
		for secondary.saves.Load() < 5 || dc.GetStatus()["secondary_healthy"] != false {
			if time.Now().After(deadline) {
				t.Fatalf("复制 worker 应继续处理任务、健康检查应记为不健康：saves=%d status=%v", secondary.saves.Load(), dc.GetStatus())
			}
			time.Sleep(5 * time.Millisecond)
		}
		if err := dc.Close(); err != nil {
			t.Fatal(err)
		}
	})
}
