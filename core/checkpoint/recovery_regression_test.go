package checkpoint

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	agentstate "github.com/kart-io/goagent/core/state"
)

type recoveryBackend struct {
	*MockCheckpointer
	beforeSave func(string, agentstate.State)
	afterSave  func(string, agentstate.State)
	failed     atomic.Bool
}

func (b *recoveryBackend) Save(ctx context.Context, id string, state agentstate.State) error {
	if b.failed.Load() {
		return fmt.Errorf("后端不可用")
	}
	if b.beforeSave != nil {
		b.beforeSave(id, state)
	}
	err := b.MockCheckpointer.Save(ctx, id, state)
	if b.afterSave != nil {
		b.afterSave(id, state)
	}
	return err
}

func recoveryState(v int) agentstate.State {
	s := agentstate.NewAgentState()
	s.Set("v", v)
	return s
}

func recoveryCheckpointer(t *testing.T, primary, secondary Checkpointer, modes ...string) *DistributedCheckpointer {
	t.Helper()
	cfg := DefaultDistributedCheckpointerConfig()
	cfg.PrimaryBackend, cfg.SecondaryBackend = primary, secondary
	cfg.HealthCheckInterval = time.Hour
	if len(modes) > 0 {
		cfg.ReplicationMode = modes[0]
	}
	dc, err := NewDistributedCheckpointer(cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		select {
		case <-dc.healthCheckStop:
		default:
			require.NoError(t, dc.Close())
		}
	})
	return dc
}

func TestRecoveryFailbackKeepsAcknowledgedState(t *testing.T) {
	ctx := context.Background()
	p := &recoveryBackend{MockCheckpointer: NewMockCheckpointer()}
	s := NewMockCheckpointer()
	dc := recoveryCheckpointer(t, p, s)
	require.NoError(t, dc.Save(ctx, "existing", recoveryState(1)))
	p.failed.Store(true)
	require.NoError(t, dc.Save(ctx, "existing", recoveryState(2)))
	require.NoError(t, dc.Save(ctx, "new", recoveryState(3)))
	require.NoError(t, dc.Delete(ctx, "existing"))
	p.failed.Store(false)
	dc.checkBackendHealth()
	state, err := dc.Load(ctx, "new")
	require.NoError(t, err)
	v, _ := state.Get("v")
	require.Equal(t, 3, v)
	exists, err := dc.Exists(ctx, "existing")
	require.NoError(t, err)
	require.False(t, exists, "已确认删除不能复活")
	require.Equal(t, "secondary", dc.GetStatus()["active_backend"])
}

func TestRecoveryReplicationPreservesSaveDeleteOrder(t *testing.T) {
	for _, deletion := range []bool{false, true} {
		t.Run(fmt.Sprint(deletion), func(t *testing.T) {
			ctx := context.Background()
			entered, release, later := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
			var once sync.Once
			defer once.Do(func() { close(release) })
			s := &recoveryBackend{MockCheckpointer: NewMockCheckpointer(), beforeSave: func(_ string, state agentstate.State) {
				v, _ := state.Get("v")
				if v == 1 {
					close(entered)
					<-release
				} else {
					later <- struct{}{}
				}
			}}
			dc := recoveryCheckpointer(t, NewMockCheckpointer(), s)
			require.NoError(t, dc.Save(ctx, "s", recoveryState(1)))
			<-entered
			completed := make(chan error, 1)
			go func() {
				if deletion {
					completed <- dc.Delete(ctx, "s")
				} else {
					completed <- dc.Save(ctx, "s", recoveryState(2))
				}
			}()
			// 第二次副库写入若越过第一条阻塞写入，旧实现会在释放后覆盖新值。
			select {
			case <-later:
			case <-time.After(30 * time.Millisecond):
			}
			once.Do(func() { close(release) })
			require.NoError(t, <-completed)
			require.Eventually(t, func() bool {
				exists, _ := s.Exists(ctx, "s")
				if deletion {
					return !exists
				}
				state, err := s.Load(ctx, "s")
				if err != nil {
					return false
				}
				v, _ := state.Get("v")
				return v == 2
			}, time.Second, time.Millisecond)
			// 等待复制彻底排空后再次断言，避免提前看到中间状态。
			require.NoError(t, dc.Close())
			if deletion {
				exists, _ := s.Exists(ctx, "s")
				require.False(t, exists)
			} else {
				state, err := s.Load(ctx, "s")
				require.NoError(t, err)
				v, _ := state.Get("v")
				require.Equal(t, 2, v)
			}
		})
	}
}

func TestRecoveryConcurrentCommitEnqueueOrder(t *testing.T) {
	for _, mode := range []string{constants.CheckpointReplicationModeAsync, constants.CheckpointReplicationModeSync} {
		for _, deletion := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/删除%t", mode, deletion), func(t *testing.T) {
				testRecoveryConcurrentCommitOrder(t, mode, deletion)
			})
		}
	}
}

func testRecoveryConcurrentCommitOrder(t *testing.T, mode string, deletion bool) {
	ctx := context.Background()
	committed, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	p := &recoveryBackend{MockCheckpointer: NewMockCheckpointer(), afterSave: func(_ string, state agentstate.State) {
		v, _ := state.Get("v")
		if v == 1 {
			close(committed)
			<-release
		}
	}}
	s := NewMockCheckpointer()
	dc := recoveryCheckpointer(t, p, s, mode)
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { first <- dc.Save(ctx, "s", recoveryState(1)) }()
	<-committed
	go func() {
		if deletion {
			second <- dc.Delete(ctx, "s")
		} else {
			second <- dc.Save(ctx, "s", recoveryState(2))
		}
	}()
	secondDone := false
	select {
	case err := <-second:
		require.NoError(t, err)
		secondDone = true
	case <-time.After(30 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	require.NoError(t, <-first)
	if !secondDone {
		require.NoError(t, <-second)
	}
	// 最终副库必须与主库的提交顺序一致。
	require.NoError(t, dc.Close())
	for _, backend := range []Checkpointer{p, s} {
		if deletion {
			exists, err := backend.Exists(ctx, "s")
			require.NoError(t, err)
			require.False(t, exists)
		} else {
			state, err := backend.Load(ctx, "s")
			require.NoError(t, err)
			v, _ := state.Get("v")
			require.Equal(t, 2, v)
		}
	}
}

func TestRecoveryFailoverFencesQueuedReplication(t *testing.T) {
	ctx := context.Background()
	p := &recoveryBackend{MockCheckpointer: NewMockCheckpointer()}
	s := NewMockCheckpointer()
	cfg := DefaultDistributedCheckpointerConfig()
	cfg.PrimaryBackend, cfg.SecondaryBackend = p, s
	// 延迟启动 worker，精确构造切换时尚未应用的旧任务。
	dc := &DistributedCheckpointer{config: cfg, activeBackend: p, primaryHealthy: true,
		secondaryHealthy: true, replicationQueue: make(chan replicationTask, 10), healthCheckStop: make(chan struct{})}
	require.NoError(t, dc.Save(ctx, "updated", recoveryState(1)))
	require.NoError(t, dc.Save(ctx, "deleted", recoveryState(1)))
	p.failed.Store(true)
	require.NoError(t, dc.Save(ctx, "updated", recoveryState(2)))
	require.NoError(t, dc.Delete(ctx, "deleted"))
	dc.startReplicationWorkers()
	require.NoError(t, dc.Close())
	state, err := s.Load(ctx, "updated")
	require.NoError(t, err)
	v, _ := state.Get("v")
	require.Equal(t, 2, v)
	exists, err := s.Exists(ctx, "deleted")
	require.NoError(t, err)
	require.False(t, exists)
}

func TestRecoveryConcurrentHealthAndFailover(t *testing.T) {
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		p := &recoveryBackend{MockCheckpointer: NewMockCheckpointer()}
		p.failed.Store(true)
		s := NewMockCheckpointer()
		dc := recoveryCheckpointer(t, p, s)
		var wg sync.WaitGroup
		for j := 0; j < 4; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = dc.Save(ctx, "s", recoveryState(2))
				_, _ = dc.Load(ctx, "s")
				dc.checkBackendHealth()
				_ = dc.tryFailover(ctx)
				_ = dc.GetStatus()
			}()
		}
		wg.Wait()
		require.NoError(t, dc.Close())
	}
}

func TestRecoveryHealthyPrimaryCannotTakeNewWrites(t *testing.T) {
	ctx := context.Background()
	primary := &recoveryBackend{MockCheckpointer: NewMockCheckpointer()}
	secondary := NewMockCheckpointer()
	dc := recoveryCheckpointer(t, primary, secondary, constants.CheckpointReplicationModeSync)
	require.NoError(t, dc.Save(ctx, "record", recoveryState(1)))
	primary.failed.Store(true)
	require.NoError(t, dc.Save(ctx, "record", recoveryState(2)))
	primary.failed.Store(false)
	dc.checkBackendHealth()
	require.NoError(t, dc.Save(ctx, "record", recoveryState(3)))
	authoritative, err := secondary.Load(ctx, "record")
	require.NoError(t, err)
	value, _ := authoritative.Get("v")
	require.Equal(t, 3, value)
	stale, err := primary.Load(ctx, "record")
	require.NoError(t, err)
	old, _ := stale.Get("v")
	require.Equal(t, 1, old, "健康恢复不会让旧主库获得新写入")
	require.Equal(t, "secondary", dc.GetStatus()["active_backend"])
}
