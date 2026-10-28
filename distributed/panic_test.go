package distributed

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/testing/panictest"
)

// OnStateChange 在熔断器自己起的 goroutine 中 panic 时进程不得退出，panic 交给全局 PanicHandler 记录，
// 熔断器状态机照常工作，后续状态变化仍会通知回调。
func TestCircuitBreakerOnStateChangePanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		recorder := &panictest.Recorder{}
		core.SetGlobalMetricsCollector(recorder)
		var calls atomic.Int32
		cb := NewCircuitBreaker(&CircuitBreakerConfig{
			MaxFailures: 1,
			Timeout:     time.Hour,
			OnStateChange: func(from, to CircuitState) {
				calls.Add(1)
				panic("state change boom")
			},
		})
		_ = cb.Execute(func() error { return errors.New("fail") })
		events := recorder.Wait(1, 2*time.Second)
		if len(events) != 1 || events[0].Component != "circuit_breaker" || events[0].Value != "state change boom" {
			t.Fatalf("PanicHandler 未收到回调 panic：%+v", events)
		}
		if cb.State() != StateOpen {
			t.Fatalf("状态 = %v，期望 open", cb.State())
		}
		cb.Reset()
		if events := recorder.Wait(2, 2*time.Second); len(events) != 2 || calls.Load() != 2 {
			t.Fatalf("Reset 后的状态变化未通知回调：calls=%d events=%+v", calls.Load(), events)
		}
	})
}
