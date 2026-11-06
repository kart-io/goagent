package builder

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

// throughputModel 是不保留对话的离线终态源，只测框架与会话互斥，不模拟网络延迟。
type throughputModel struct{ calls atomic.Int64 }

func (*throughputModel) Provider() constants.Provider { return constants.ProviderCustom }
func (m *throughputModel) CompleteStream(ctx context.Context, _ *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	m.calls.Add(1)
	ch := make(chan *llm.StreamChunk, 1)
	ch <- &llm.StreamChunk{Done: true, FinishReason: "stop", Content: "回执"}
	close(ch)
	return ch, nil
}

func BenchmarkAgentSessions(b *testing.B) {
	for _, shared := range []bool{true, false} {
		name := "独立会话"
		if shared {
			name = "共享会话"
		}
		b.Run(name, func(b *testing.B) {
			model := &throughputModel{}
			factory := func() *SimpleAgent {
				a, err := NewSimpleBuilder(model).Build()
				if err != nil {
					b.Fatal(err)
				}
				return a
			}
			one := factory()
			var completed atomic.Int64
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				agent := one
				if !shared {
					agent = factory()
				}
				r := agent.AsRunnable()
				for pb.Next() {
					out, err := r.Invoke(context.Background(), "核验")
					if err != nil || out.Result != "回执" {
						b.Error("执行失败", err)
						return
					}
					completed.Add(1)
				}
			})
			b.StopTimer()
			if completed.Load() != int64(b.N) || model.calls.Load() != int64(b.N) {
				b.Fatal("执行次数不符")
			}
		})
	}
}

var _ llm.Client = (*throughputModel)(nil)
