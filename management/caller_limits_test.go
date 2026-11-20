package management_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
	mt "github.com/kart-io/goagent/management/internal/managementtest"
)

func withConfig(t *testing.T, f *mt.Fixture, edit func(*mg.Config)) {
	t.Helper()
	cfg := f.Config()
	edit(&cfg)
	var err error
	f.Service, err = mg.New(cfg)
	require.NoError(t, err)
}

func submit(t *testing.T, f *mt.Fixture, se mg.Session, key string, b *mg.TaskBudget) (mg.Task, error) {
	t.Helper()
	return f.Service.SubmitTask(context.Background(), f.Actor, mg.SubmitTaskRequest{IdempotencyKey: key, SessionID: se.ID, Input: []byte(`{"q":1}`), Budget: b})
}

func TestSubmitTaskBudgetCapsToolCalls(t *testing.T) {
	f := mt.NewFixture(t)
	se := f.ReadySession(t)
	f.Factory.RunFunc = func(ctx context.Context, spec mg.RunSpec) (mg.RunResult, error) {
		c, ok := budget.FromContext(ctx)
		if !ok {
			return mg.RunResult{}, errors.New("budget not bound")
		}
		for i := 0; i < 3; i++ {
			if err := c.ReserveTool(ctx, fmt.Sprintf("tool-%d", i)); err != nil {
				return mg.RunResult{}, err
			}
		}
		return mg.RunResult{Outcome: "completed", Output: []byte(`{}`)}, nil
	}
	task, err := submit(t, f, se, "tools", &mg.TaskBudget{MaxToolCalls: 2})
	require.NoError(t, err)
	f.StartWorker(t)
	got := offlineExecutionWaitTask(t, f, task.ID, mg.Failed)
	require.Equal(t, "budget_exhausted", got.ErrorKind)
}

func TestSubmitTaskZeroBudgetPersistsUnlimited(t *testing.T) {
	f := mt.NewFixture(t)
	se := f.ReadySession(t)
	task, err := submit(t, f, se, "zero", &mg.TaskBudget{})
	require.NoError(t, err)
	stored, err := f.Service.GetTask(context.Background(), f.Actor, task.ID)
	require.NoError(t, err)
	for _, b := range []budget.Config{task.Budget, stored.Budget} {
		require.Equal(t, budget.Config{RootKey: task.ID, Version: mg.TaskBudgetVersion}, b)
	}
	f.Factory.RunFunc = func(ctx context.Context, spec mg.RunSpec) (mg.RunResult, error) {
		c, _ := budget.FromContext(ctx)
		for i := 0; i < 101; i++ {
			if err := c.Reserve(ctx, fmt.Sprintf("m-%d", i)); err != nil {
				return mg.RunResult{}, err
			}
		}
		return mg.RunResult{Outcome: "completed", Output: []byte(`{}`)}, nil
	}
	f.StartWorker(t)
	offlineExecutionWaitTask(t, f, task.ID, mg.Succeeded)
}

func TestSubmitTaskBudgetIsPartOfIdempotency(t *testing.T) {
	f := mt.NewFixture(t)
	se := f.ReadySession(t)
	first, err := submit(t, f, se, "same", &mg.TaskBudget{MaxModelRequests: 3})
	require.NoError(t, err)
	again, err := submit(t, f, se, "same", &mg.TaskBudget{MaxModelRequests: 3})
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID)
	_, err = submit(t, f, se, "same", &mg.TaskBudget{MaxModelRequests: 4})
	require.ErrorIs(t, err, agentErrors.ErrManagementConflict)
	_, err = submit(t, f, se, "same", nil)
	require.ErrorIs(t, err, agentErrors.ErrManagementConflict)
	_, err = submit(t, f, se, "negative", &mg.TaskBudget{MaxToolCalls: -1})
	require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
}

func TestDefaultTaskBudgetAppliesWhenRequestOmits(t *testing.T) {
	f := mt.NewFixture(t)
	now := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	withConfig(t, f, func(c *mg.Config) {
		c.Now = func() time.Time { return now }
		c.DefaultTaskBudget = mg.TaskBudget{MaxModelRequests: 7, MaxToolCalls: 8, MaxAgentCalls: 9, Timeout: time.Hour}
	})
	se := f.ReadySession(t)
	task, err := submit(t, f, se, "default", nil)
	require.NoError(t, err)
	require.Equal(t, budget.Config{RootKey: task.ID, Version: mg.TaskBudgetVersion, MaxModelRequests: 7, MaxToolCalls: 8, MaxAgentCalls: 9, DeadlineUnixMilli: now.Add(time.Hour).UnixMilli()}, task.Budget)
	override, err := submit(t, f, se, "override", &mg.TaskBudget{MaxModelRequests: 1})
	require.NoError(t, err)
	require.Equal(t, budget.Config{RootKey: override.ID, Version: mg.TaskBudgetVersion, MaxModelRequests: 1}, override.Budget)

	cfg := f.Config()
	cfg.DefaultTaskBudget = mg.TaskBudget{Timeout: -time.Second}
	_, err = mg.New(cfg)
	require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
}

// legacyTask rewrites a queued task into the shape management/v1 persisted:
// MaxAgentCalls omitted from the task, 1000 recorded in its ledger.
func legacyTask(t *testing.T, f *mt.Fixture, se mg.Session, task mg.Task) mg.Task {
	t.Helper()
	ctx := context.Background()
	task.Budget = budget.Config{RootKey: task.ID, Version: mg.LegacyTaskBudgetVersion, MaxModelRequests: 100, MaxToolCalls: 1000, DeadlineUnixMilli: time.Now().Add(24 * time.Hour).UnixMilli()}
	require.NoError(t, f.Store.Put(ctx, "tasks", task))
	ledger := task.Budget
	ledger.MaxAgentCalls = 1000
	c, err := budget.NewDurable(ledger, session.DurableStore(f.Sessions, se.LogID))
	require.NoError(t, err)
	require.NoError(t, c.Reserve(ctx, "before-upgrade"))
	return task
}

func TestV1TaskBudgetBackfillsAgentCalls(t *testing.T) {
	f := mt.NewFixture(t)
	se := f.ReadySession(t)
	task, err := submit(t, f, se, "legacy", nil)
	require.NoError(t, err)
	legacyTask(t, f, se, task)
	got, err := f.Service.GetTask(context.Background(), f.Actor, task.ID)
	require.NoError(t, err)
	require.Equal(t, 1000, got.Budget.MaxAgentCalls)
	f.Factory.RunFunc = func(ctx context.Context, spec mg.RunSpec) (mg.RunResult, error) {
		c, ok := budget.FromContext(ctx)
		if !ok {
			return mg.RunResult{}, errors.New("budget not bound")
		}
		if err := c.Reserve(ctx, "after-upgrade"); err != nil {
			return mg.RunResult{}, err
		}
		return mg.RunResult{Outcome: "completed", Output: []byte(`{}`)}, nil
	}
	f.StartWorker(t)
	done := offlineExecutionWaitTask(t, f, task.ID, mg.Succeeded)
	require.Empty(t, done.ErrorKind)
	run, err := f.Service.GetRun(context.Background(), f.Actor, task.ID)
	require.NoError(t, err)
	require.NotNil(t, run.Usage)
}

func TestV1TaskUsageAvailableViaGetRun(t *testing.T) {
	f := mt.NewFixture(t)
	se := f.ReadySession(t)
	task, err := submit(t, f, se, "legacy-done", nil)
	require.NoError(t, err)
	task.State = mg.Succeeded
	finished := time.Now().UTC()
	task.FinishedAt = &finished
	legacyTask(t, f, se, task)
	run, err := f.Service.GetRun(context.Background(), f.Actor, task.ID)
	require.NoError(t, err)
	require.NotNil(t, run.Usage, "usage status %q", run.UsageStatus)
	require.NotEqual(t, "unavailable", run.UsageStatus)
}

func TestIdempotencyRetentionZeroNeverExpires(t *testing.T) {
	f := mt.NewFixture(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	withConfig(t, f, func(c *mg.Config) { c.Now = func() time.Time { return now } })
	ctx := context.Background()
	a, err := f.Service.RegisterAgent(ctx, f.Actor, mg.RegisterAgentRequest{IdempotencyKey: "forever", Name: "永久"})
	require.NoError(t, err)
	keys := offlineExecutionDocuments(t, f, "idempotency", mg.Query{})
	require.Len(t, keys, 1)
	require.Contains(t, string(keys[0].Data), `"expires_at":"0001-01-01T00:00:00Z"`)
	now = now.Add(100 * 365 * 24 * time.Hour)
	again, err := f.Service.RegisterAgent(ctx, f.Actor, mg.RegisterAgentRequest{IdempotencyKey: "forever", Name: "永久"})
	require.NoError(t, err)
	require.Equal(t, a.ID, again.ID)
	_, err = f.Service.RegisterAgent(ctx, f.Actor, mg.RegisterAgentRequest{IdempotencyKey: "forever", Name: "不同"})
	require.ErrorIs(t, err, agentErrors.ErrManagementConflict)
	counts, err := f.Service.Prune(ctx, f.Actor)
	require.NoError(t, err)
	require.Zero(t, counts["idempotency"])
	require.Len(t, offlineExecutionDocuments(t, f, "idempotency", mg.Query{}), 1)
}

func TestLimitsZeroMeansUnlimited(t *testing.T) {
	f := mt.NewFixture(t)
	ctx := context.Background()
	se := f.ReadySession(t)
	page, err := f.Service.ListAgents(ctx, f.Actor, mg.Query{Limit: 500})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	_, err = f.Service.History(ctx, f.Actor, mg.HistoryRequest{SessionID: se.ID, Limit: 500})
	require.NoError(t, err)
	big := []byte(`{"q":"` + strings.Repeat("x", 2<<20) + `"}`)
	_, err = f.Service.SubmitTask(ctx, f.Actor, mg.SubmitTaskRequest{IdempotencyKey: "big", SessionID: se.ID, Input: big})
	require.NoError(t, err, "MaxInputBytes 0 must not cap input")
	// MaxPending unset: a submit must not list tasks at all.
	f.Store.FailNext("list", "tasks", errors.New("pending tasks were counted"))
	_, err = submit(t, f, se, "uncounted", nil)
	require.NoError(t, err)
}

func TestMaxPendingCountsByPages(t *testing.T) {
	f := mt.NewFixture(t)
	withConfig(t, f, func(c *mg.Config) { c.Limits = mg.Limits{MaxPending: 3} })
	se := f.ReadySession(t)
	for i := 0; i < 3; i++ {
		_, err := submit(t, f, se, fmt.Sprint("p", i), nil)
		require.NoError(t, err)
	}
	_, err := submit(t, f, se, "p3", nil)
	require.ErrorIs(t, err, agentErrors.ErrManagementBusy)
	other := f.Actor
	other.TenantID = "tenant-other"
	_, err = f.Service.SubmitTask(context.Background(), other, mg.SubmitTaskRequest{IdempotencyKey: "p3", SessionID: se.ID, Input: []byte(`{}`)})
	require.NotErrorIs(t, err, agentErrors.ErrManagementBusy, "MaxPending is counted per tenant")
}

// oldScanLimit is the fixed scan size internal reads used to truncate at.
const oldScanLimit = 10000

func seedMany(t *testing.T, f *mt.Fixture, kind string, n int, value func(i int, m mg.Meta) any) {
	t.Helper()
	ctx := context.Background()
	created := time.Now().UTC().Add(-time.Hour)
	require.NoError(t, f.Store.WithinTx(ctx, func(tx mg.Tx) error {
		for i := 0; i < n; i++ {
			m := mg.Meta{ID: fmt.Sprintf("%s-%06d", kind, i), TenantID: f.Actor.TenantID, Revision: 1, CreatedAt: created}
			raw, err := json.Marshal(value(i, m))
			if err != nil {
				return err
			}
			if err = tx.Insert(ctx, mg.Document{Kind: kind, TenantID: m.TenantID, ID: m.ID, Revision: 1, CreatedAt: created, Data: raw}); err != nil {
				return err
			}
		}
		return nil
	}))
}

func TestSupersedeApprovalsBeyondOldScanLimit(t *testing.T) {
	f := mt.NewFixture(t)
	ctx := context.Background()
	se := f.ReadySession(t)
	task, err := submit(t, f, se, "approvals", nil)
	require.NoError(t, err)
	task.State = mg.WaitingApproval
	require.NoError(t, f.Store.Put(ctx, "tasks", task))
	seedMany(t, f, "approvals", oldScanLimit+1, func(i int, m mg.Meta) any {
		return mg.Approval{Meta: m, TaskID: task.ID, AgentID: task.AgentID, SessionID: se.ID, State: "pending"}
	})
	_, err = f.Service.CancelTask(ctx, f.Actor, mg.CancelTaskRequest{IdempotencyKey: "cancel", TaskID: task.ID, ExpectedRevision: task.Revision})
	require.NoError(t, err)
	for _, d := range offlineExecutionDocuments(t, f, "approvals", mg.Query{TaskID: task.ID}) {
		var ap mg.Approval
		require.NoError(t, json.Unmarshal(d.Data, &ap))
		require.Equal(t, "superseded", ap.State, ap.ID)
	}
}

func TestPruneBeyondOldScanLimit(t *testing.T) {
	f := mt.NewFixture(t)
	withConfig(t, f, func(c *mg.Config) { c.Limits = mg.Limits{AuditRetention: time.Minute} })
	seedMany(t, f, "audit", oldScanLimit+1, func(i int, m mg.Meta) any {
		return mg.Audit{Meta: m, ResourceType: "agents", ResourceID: "x", Operation: "seed", Actor: f.Actor}
	})
	counts, err := f.Service.Prune(context.Background(), f.Actor)
	require.NoError(t, err)
	require.Equal(t, oldScanLimit+1, counts["audit"])
	require.Empty(t, offlineExecutionDocuments(t, f, "audit", mg.Query{}))
}

func TestWorkerClaimsBeyondOldScanLimit(t *testing.T) {
	f := mt.NewFixture(t)
	se := f.ReadySession(t)
	// Earlier, not-yet-due commands fill the first oldScanLimit+1 scan slots.
	seedMany(t, f, "commands", oldScanLimit+1, func(i int, m mg.Meta) any {
		return mg.Command{Meta: m, Kind: "start", TaskID: "missing", State: "pending", Actor: f.Actor, AvailableAt: time.Now().Add(time.Hour)}
	})
	task, err := submit(t, f, se, "late", nil)
	require.NoError(t, err)
	f.StartWorker(t)
	// 每轮认领都要扫完全部种子命令，-race 下单轮可达秒级，等待放宽。
	require.Eventually(t, func() bool {
		got, err := f.Service.GetTask(context.Background(), f.Actor, task.ID)
		return err == nil && got.State == mg.Succeeded
	}, time.Minute, 20*time.Millisecond)
}
