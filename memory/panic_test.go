package memory

import (
	"context"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/testing/panictest"
)

// panicVectorStore 的 Delete panic，模拟宿主实现的向量库。
type panicVectorStore struct{ VectorStore }

func (panicVectorStore) Delete(context.Context, string) error { panic("vector boom") }

// 后台整理（每个周期调用 maintain）中宿主 VectorStore panic 时进程不得退出：
// panic 交给全局 PanicHandler 记录，记忆的锁已释放，后续读写照常。
func TestHierarchicalMemoryMaintainPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		recorder := &panictest.Recorder{}
		core.SetGlobalMetricsCollector(recorder)
		m := NewHierarchicalMemoryWithContext(context.Background(), panicVectorStore{})
		defer m.Shutdown(context.Background())
		if err := m.Store(context.Background(), "k", "x", StoreOptions{}); err != nil {
			t.Fatal(err)
		}
		m.maintain()
		if events := recorder.Wait(1, time.Second); len(events) != 1 || events[0].Value != "vector boom" {
			t.Fatalf("PanicHandler 未收到 panic：%+v", events)
		}
		if err := m.Store(context.Background(), "k2", "y", StoreOptions{}); err != nil {
			t.Fatalf("panic 后记忆应可继续使用：%v", err)
		}
	})
}
