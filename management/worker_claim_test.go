package management

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 模拟 pgx：事务已提交，但提交时 ctx 已取消，调用方只拿到 context canceled。
type cancelOnCommitStore struct {
	reviewStore
	cancel context.CancelFunc
	txs    int
}

func (s *cancelOnCommitStore) WithinTx(ctx context.Context, fn func(Tx) error) error {
	if err := fn(s.reviewStore); err != nil {
		return err
	}
	if s.txs++; s.txs == 1 {
		s.cancel()
	}
	return ctx.Err()
}

func TestWorkerRunReleasesSlotWhenCanceledDuringClaim(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := &cancelOnCommitStore{reviewStore: reviewStore{}, cancel: cancel}
	w := NewWorker(&Service{cfg: Config{Store: st, Now: time.Now, Limits: DefaultLimits()}})

	_ = w.Run(parent)

	o, err := read[WorkerOwner](context.Background(), st, "workers", "__system", "default")
	require.NoError(t, err)
	require.Empty(t, o.OwnerID, "认领途中取消后槽位不得留给已退出的 Worker")
}
