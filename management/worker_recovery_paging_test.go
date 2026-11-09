package management

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/stretchr/testify/require"
)

// 专用于恢复测试：强制遵守过滤和分页，并在事务失败时回滚。
type recoveryPageStore struct {
	reviewStore
	lists, failAt int
}

func (s *recoveryPageStore) WithinTx(ctx context.Context, fn func(Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	before := maps.Clone(s.reviewStore)
	err := fn(s)
	if err != nil {
		s.reviewStore = before
	}
	return err
}

func (s *recoveryPageStore) List(ctx context.Context, kind string, sel Selection) ([]Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.lists++
	if s.failAt > 0 && s.lists == s.failAt {
		return nil, errors.New("恢复读取失败")
	}
	var out []Document
	for _, d := range s.reviewStore {
		if d.Kind != kind || (sel.TenantID != "" && sel.TenantID != d.TenantID) {
			continue
		}
		var state struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(d.Data, &state); err != nil {
			return nil, err
		}
		for _, status := range sel.Query.Statuses {
			if state.State == status {
				out = append(out, d)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if sel.Query.Limit > 0 && len(out) > sel.Query.Limit {
		out = out[:sel.Query.Limit]
	}
	return out, nil
}

func seedRecovery(t *testing.T, count int) (*Worker, *recoveryPageStore) {
	t.Helper()
	st := &recoveryPageStore{reviewStore: reviewStore{}}
	w := NewWorker(&Service{cfg: Config{Store: st, Now: time.Now, Limits: DefaultLimits()}})
	for i := 0; i < count; i++ {
		meta := Meta{ID: fmt.Sprintf("%06d", i), TenantID: fmt.Sprintf("tenant-%d", i%2), Revision: 1}
		state := Running
		if i%2 == 1 {
			state = Canceling
		}
		require.NoError(t, insert(context.Background(), st, "tasks", Task{Meta: meta, State: state, CancelRequested: state == Canceling}))
		require.NoError(t, insert(context.Background(), st, "commands", Command{Meta: meta, State: "applying"}))
	}
	return w, st
}

func TestWorkerRecoveryDrainsBeyondFirstTenThousand(t *testing.T) {
	w, st := seedRecovery(t, 10001)
	ctx := context.Background()
	terminal := Task{Meta: Meta{ID: "terminal", TenantID: "tenant-0", Revision: 7}, State: Succeeded}
	require.NoError(t, insert(ctx, st, "tasks", terminal))
	require.NoError(t, w.recover(ctx))
	for i := 0; i < 10001; i++ {
		id, tenant := fmt.Sprintf("%06d", i), fmt.Sprintf("tenant-%d", i%2)
		task, err := read[Task](ctx, st, "tasks", tenant, id)
		require.NoError(t, err)
		require.Equal(t, Queued, task.State, id)
		require.True(t, task.RecoveryNeeded, id)
		require.Equal(t, i%2 == 1, task.CancelRequested)
		require.EqualValues(t, 2, task.Revision)
		cmd, err := read[Command](ctx, st, "commands", tenant, id)
		require.NoError(t, err)
		require.Equal(t, "pending", cmd.State, id)
	}
	before := maps.Clone(st.reviewStore)
	require.NoError(t, w.recover(ctx))
	require.Equal(t, before, st.reviewStore, "重复恢复不得重复修改版本或终态")
}

func TestWorkerRecoveryRollsBackFailedScan(t *testing.T) {
	w, st := seedRecovery(t, 401)
	before := maps.Clone(st.reviewStore)
	st.failAt = 2
	require.ErrorContains(t, w.recover(context.Background()), "恢复读取失败")
	require.Equal(t, before, st.reviewStore)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, w.recover(ctx), context.Canceled)
	require.Equal(t, before, st.reviewStore)
}
