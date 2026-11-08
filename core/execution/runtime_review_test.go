package execution

import (
	"github.com/kart-io/goagent/core/state"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestReviewRT06ManagerConcurrentIdentity(t *testing.T) {
	m := NewRuntimeManager[string, *state.AgentState]()
	const n = 40
	got := make([]*Runtime[string, *state.AgentState], n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			got[i] = m.GetOrCreateRuntime("同一会话", "", state.NewAgentState(), nil, nil)
			m.GetRuntime("同一会话")
			m.Size()
			m.CleanupExpired(time.Hour)
		}(i)
	}
	close(start)
	wg.Wait()
	for _, r := range got {
		require.Same(t, got[0], r)
	}
}
