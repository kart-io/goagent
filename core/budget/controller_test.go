package budget

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/testing/panictest"
	"github.com/stretchr/testify/require"
)

func TestRootBudgetConcurrentAdmission(t *testing.T) {
	ctx := context.Background()
	c, err := New(Config{RootKey: "run", Version: "v1", MaxModelRequests: 2})
	require.NoError(t, err)
	start := make(chan struct{})
	errs := make(chan error, 3)
	var wg sync.WaitGroup
	for i := range 3 {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; errs <- c.Reserve(ctx, fmt.Sprint(i)) }()
	}
	close(start)
	wg.Wait()
	close(errs)
	admitted := 0
	for e := range errs {
		if e == nil {
			admitted++
		} else {
			require.ErrorIs(t, e, ErrBudgetExceeded)
		}
	}
	require.Equal(t, 2, admitted)
	snap, err := c.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, snap.ReservedRequests)
	require.Equal(t, 2, snap.UnknownUsageEntries)
}
func TestUsageIsIdempotentAndRequiresReservation(t *testing.T) {
	ctx := context.Background()
	c, err := New(Config{RootKey: "run", Version: "v1", MaxModelRequests: 2})
	require.NoError(t, err)
	require.Error(t, c.RecordUsage(ctx, "missing", nil))
	require.NoError(t, c.Reserve(ctx, "a"))
	require.ErrorIs(t, c.Reserve(ctx, "a"), ErrReservationExists)
	require.NoError(t, c.RecordUsage(ctx, "a", nil))
	u := &interfaces.TokenUsage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}
	require.NoError(t, c.RecordUsage(ctx, "a", u))
	require.NoError(t, c.RecordUsage(ctx, "a", u))
	u.TotalTokens = 90
	require.Error(t, c.RecordUsage(ctx, "a", u))
	s, err := c.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, 5, s.KnownTokens.TotalTokens)
	require.Equal(t, 1, s.KnownUsageEntries)
}
func TestBudgetSurvivesProcessExit(t *testing.T) {
	if dir := os.Getenv("GOAGENT_BUDGET_CHILD"); dir != "" {
		store, e := durable.NewFileStore(dir)
		if e != nil {
			panic(e)
		}
		c, e := NewDurable(Config{RootKey: "run", Version: "v1", MaxModelRequests: 1}, store)
		if e != nil {
			panic(e)
		}
		if e = c.Reserve(context.Background(), "model-1"); e != nil {
			panic(e)
		}
		os.Exit(86)
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestBudgetSurvivesProcessExit$")
	cmd.Env = append(os.Environ(), "GOAGENT_BUDGET_CHILD="+dir)
	output, err := panictest.CombinedOutput(cmd, panictest.ChildTimeout(t, time.Minute))
	var exited *exec.ExitError
	require.ErrorAs(t, err, &exited, string(output))
	require.Equal(t, 86, exited.ExitCode(), string(output))
	store, err := durable.NewFileStore(dir)
	require.NoError(t, err)
	cfg := Config{RootKey: "run", Version: "v1", MaxModelRequests: 1}
	c, err := NewDurable(cfg, store)
	require.NoError(t, err)
	require.ErrorIs(t, c.Reserve(context.Background(), "new"), ErrBudgetExceeded)
	cfg.Version = "v2"
	_, err = NewDurable(cfg, store)
	require.Error(t, err)
	cfg.Version = "v1"
	cfg.MaxModelRequests = 2
	_, err = NewDurable(cfg, store)
	require.Error(t, err)
}

type failingStore struct{ durable.Store }

func (f failingStore) Save(context.Context, string, *durable.Snapshot) error {
	return errors.New("save failed")
}
func TestBudgetSaveFailureDoesNotAdmit(t *testing.T) {
	s, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	_, err = NewDurable(Config{RootKey: "x", Version: "v1", MaxModelRequests: 2}, failingStore{s})
	require.ErrorContains(t, err, "save failed")
}

func TestRemainingDecreasesAfterReserve(t *testing.T) {
	ctx := context.Background()
	mem, err := New(Config{RootKey: "run", Version: "v1", MaxModelRequests: 2})
	require.NoError(t, err)
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	cfg := Config{RootKey: "run", Version: "v1", MaxModelRequests: 2}
	dur, err := NewDurable(cfg, store)
	require.NoError(t, err)
	for _, c := range []*Controller{mem, dur} {
		n, err := c.Remaining(ctx)
		require.NoError(t, err)
		require.Equal(t, 2, n)
		require.NoError(t, c.Reserve(ctx, "a"))
		n, err = c.Remaining(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, n)
		// 重复预留被拒绝时余量不变。
		require.ErrorIs(t, c.Reserve(ctx, "a"), ErrReservationExists)
		require.NoError(t, c.Reserve(ctx, "b"))
		require.ErrorIs(t, c.Reserve(ctx, "c"), ErrBudgetExceeded)
		n, err = c.Remaining(ctx)
		require.NoError(t, err)
		require.Zero(t, n)
	}
	// 持久余量来自 Store：另一个控制器实例（模拟另一进程）看到同一账本。
	other, err := NewDurable(cfg, store)
	require.NoError(t, err)
	n, err := other.Remaining(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestRemainingUnlimitedAndCanceled(t *testing.T) {
	c, ok := FromContext(context.Background())
	require.False(t, ok)
	n, err := c.Remaining(context.Background())
	require.NoError(t, err)
	require.Equal(t, Unlimited, n)
	bound, err := New(Config{RootKey: "run", Version: "v1", MaxModelRequests: 1})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = bound.Remaining(ctx)
	require.ErrorIs(t, err, context.Canceled)
}
