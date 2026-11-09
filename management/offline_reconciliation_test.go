package management_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
	mt "github.com/kart-io/goagent/management/internal/managementtest"
	"github.com/stretchr/testify/require"
)

type offlineReconciliationReader struct {
	record mg.Reconciliation
	err    error
}

func (r offlineReconciliationReader) Lookup(context.Context, mg.Actor, string) (mg.Reconciliation, error) {
	return r.record, r.err
}

// 核对证据不能只检查引用存在，必须绑定租户、任务、不可变版本和完整操作集合。
func TestOfflineReconciliationRejectsMismatchedEvidence(t *testing.T) {
	ctx := context.Background()
	f := mt.NewFixture(t)
	f.Factory.Capabilities.Reconcile = true
	se := f.ReadySession(t)
	task, err := f.Service.SubmitTask(ctx, f.Actor, mg.SubmitTaskRequest{IdempotencyKey: "reconcile-task", SessionID: se.ID, Input: []byte(`1`)})
	require.NoError(t, err)
	task.State = mg.Attention
	task.Revision++
	task.Operations = []durable.OperationRef{{ParentKey: "parent", ID: "write-a"}, {ParentKey: "parent", ID: "write-b"}}
	require.NoError(t, f.Store.Put(ctx, "tasks", task))
	versions, err := f.Service.ListVersions(ctx, f.Actor, mg.Query{AgentID: se.AgentID})
	require.NoError(t, err)
	require.Len(t, versions.Items, 1)
	record := mg.Reconciliation{ID: "proof", TenantID: f.Actor.TenantID, TaskID: task.ID, VersionHash: versions.Items[0].ConfigHash, EvidenceHash: "immutable-proof", Resolution: "not_applied", VerifiedBy: "reviewer", VerifiedAt: time.Now().UTC(), OperationRefs: []durable.OperationRef{task.Operations[1], task.Operations[0]}}
	r := mg.ResumeTaskRequest{IdempotencyKey: "reconcile", TaskID: task.ID, ExpectedRevision: task.Revision, ReconciliationRef: record.ID}
	for _, tc := range []struct {
		name   string
		change func(*mg.Reconciliation)
	}{
		{"其他租户", func(r *mg.Reconciliation) { r.TenantID = "other" }},
		{"其他任务", func(r *mg.Reconciliation) { r.TaskID = "other" }},
		{"其他版本", func(r *mg.Reconciliation) { r.VersionHash = "other" }},
		{"缺少证据摘要", func(r *mg.Reconciliation) { r.EvidenceHash = "" }},
		{"未认证审核者", func(r *mg.Reconciliation) { r.VerifiedBy = "" }},
		{"未经确认结论", func(r *mg.Reconciliation) { r.Resolution = "unknown" }},
		{"缺少核验时间", func(r *mg.Reconciliation) { r.VerifiedAt = time.Time{} }},
		{"缺少操作", func(r *mg.Reconciliation) { r.OperationRefs = r.OperationRefs[:1] }},
		{"重复操作", func(r *mg.Reconciliation) {
			r.OperationRefs = []durable.OperationRef{task.Operations[0], task.Operations[0]}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := record
			tc.change(&bad)
			cfg := f.Config()
			cfg.Reconciliations = offlineReconciliationReader{record: bad}
			s, e := mg.New(cfg)
			require.NoError(t, e)
			before := offlineCount(t, f, "commands")
			_, e = s.ResumeTask(ctx, f.Actor, r)
			require.ErrorIs(t, e, agentErrors.ErrManagementConflict)
			require.Equal(t, before, offlineCount(t, f, "commands"))
			cur, e := s.GetTask(ctx, f.Actor, task.ID)
			require.NoError(t, e)
			require.Equal(t, task, cur)
		})
	}
	_, err = f.Service.ResumeTask(ctx, f.Actor, r)
	require.ErrorIs(t, err, agentErrors.ErrManagementUnsupported)
	cfg := f.Config()
	unavailable := errors.New("核验源不可用")
	cfg.Reconciliations = offlineReconciliationReader{err: unavailable}
	s, err := mg.New(cfg)
	require.NoError(t, err)
	_, err = s.ResumeTask(ctx, f.Actor, r)
	require.ErrorIs(t, err, unavailable)
	cfg.Reconciliations = offlineReconciliationReader{record: record}
	s, err = mg.New(cfg)
	require.NoError(t, err)
	for _, bad := range []mg.ResumeTaskRequest{
		{IdempotencyKey: "mixed-wait", TaskID: task.ID, ExpectedRevision: task.Revision, ReconciliationRef: "proof", WaitID: "wait"},
		{IdempotencyKey: "mixed-input", TaskID: task.ID, ExpectedRevision: task.Revision, ReconciliationRef: "proof", Input: []byte(`1`)},
	} {
		_, err = s.ResumeTask(ctx, f.Actor, bad)
		require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	}
	cmd, err := s.ResumeTask(ctx, f.Actor, r)
	require.NoError(t, err)
	require.Equal(t, record.ID, cmd.ReconciliationRef)
	require.Equal(t, record.EvidenceHash, cmd.ReconciliationHash)
	require.Empty(t, cmd.Input)
	require.Empty(t, cmd.WaitID)
	require.Equal(t, "pending", cmd.State)
	stored, err := s.GetCommand(ctx, f.Actor, cmd.ID)
	require.NoError(t, err)
	require.Equal(t, cmd, stored)
	again, err := s.ResumeTask(ctx, f.Actor, r)
	require.NoError(t, err)
	require.Equal(t, cmd, again)
}

func TestOfflineRecoverOwnerRequiresConfirmedIdentityAndAtomicAudit(t *testing.T) {
	for _, slot := range []string{"default", "outbox"} {
		t.Run(slot, func(t *testing.T) {
			ctx := context.Background()
			f := mt.NewFixture(t)
			owner := mg.WorkerOwner{Meta: mg.Meta{ID: slot, TenantID: "__system", Revision: 1, CreatedAt: time.Now().UTC()}, OwnerID: "dead-worker", Generation: 7}
			require.NoError(t, f.Store.Put(ctx, "workers", owner))
			call := func(s *mg.Service, id string, gen int64, reason string) error {
				if slot == "outbox" {
					return s.RecoverOutboxSlot(ctx, f.Actor, id, gen, reason)
				}
				return s.RecoverWorkerSlot(ctx, f.Actor, id, gen, reason)
			}
			require.ErrorIs(t, call(f.Service, owner.OwnerID, 7, "核验"), agentErrors.ErrManagementUnsupported)
			require.ErrorIs(t, call(f.Service, owner.OwnerID, 7, ""), agentErrors.ErrManagementInvalid)
			require.ErrorIs(t, call(f.Service, "", 7, "核验"), agentErrors.ErrManagementInvalid)
			cfg := f.Config()
			sentinel := errors.New("进程未确认退出")
			cfg.ConfirmWorkerStopped = func(context.Context, string, int64) error { return sentinel }
			s, err := mg.New(cfg)
			require.NoError(t, err)
			require.ErrorIs(t, call(s, owner.OwnerID, 7, "核验"), sentinel)
			calls := 0
			cfg.ConfirmWorkerStopped = func(_ context.Context, id string, gen int64) error {
				calls++
				require.NotEmpty(t, id)
				require.Positive(t, gen)
				return nil
			}
			s, err = mg.New(cfg)
			require.NoError(t, err)
			require.ErrorIs(t, call(s, owner.OwnerID, 6, "旧代次"), agentErrors.ErrManagementConflict)
			require.ErrorIs(t, call(s, "different-worker", 7, "错误进程"), agentErrors.ErrManagementConflict)
			f.Store.FailNext("insert", "audit", sentinel)
			require.ErrorIs(t, call(s, owner.OwnerID, 7, "确认退出"), sentinel)
			doc, err := f.Store.Get(ctx, "workers", "__system", slot)
			require.NoError(t, err)
			require.Equal(t, offlineDocument(t, "workers", owner), doc, "审计失败时 owner 清除必须回滚")
			require.NoError(t, call(s, owner.OwnerID, 7, "确认退出"))
			doc, err = f.Store.Get(ctx, "workers", "__system", slot)
			require.NoError(t, err)
			require.Equal(t, int64(2), doc.Revision)
			require.Equal(t, 1, offlineCount(t, f, "audit"))
			require.Equal(t, 4, calls)
			require.ErrorIs(t, call(s, owner.OwnerID, 7, "重复回收"), agentErrors.ErrManagementConflict)
		})
	}
}
