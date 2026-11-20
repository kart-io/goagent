package core

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBatchMaxConcurrencyZeroIsUnlimited(t *testing.T) {
	const n = 25
	var running atomic.Int32
	release := make(chan struct{})
	var once sync.Once
	r := NewRunnableFunc(func(ctx context.Context, i int) (int, error) {
		if running.Add(1) == n {
			once.Do(func() { close(release) })
		}
		select {
		case <-release:
			return i, nil
		case <-time.After(5 * time.Second):
			return 0, context.DeadlineExceeded
		}
	})
	inputs := make([]int, n)
	out, err := r.Batch(context.Background(), inputs)
	require.NoError(t, err, "旧默认并发 10 时所有输入无法同时运行")
	require.Len(t, out, n)
}
