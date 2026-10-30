package durable

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 诊断不能把未完成检查点当成失败证据，也不能执行步骤、审批钩子、加锁或写入。
func TestRunnerDiagnoseEvidenceAndReadOnly(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)
	calls := 0
	r := &Runner{Store: store, Version: "v1", Steps: []Step{{Name: "effect", RequireApproval: true, Run: func(context.Context, StepInput) (json.RawMessage, error) {
		calls++
		return nil, errors.New("SECRET failure")
	}}}}
	cp, err := r.Run(ctx, "run", json.RawMessage(`"SECRET input"`), "")
	require.ErrorIs(t, err, ErrApprovalRequired)
	r.Store = diagnosticReadOnlyStore{Store: store}
	r.BeforeApprove = func(context.Context, string, *Snapshot) error { t.Fatal("approval hook executed"); return nil }
	report, err := r.Diagnose(ctx, "run")
	require.NoError(t, err)
	require.Equal(t, "approval_waiting", report.Status)
	require.Zero(t, calls)
	r.Store = store
	r.BeforeApprove = nil
	_, err = r.Run(ctx, "run", nil, cp.Key)
	require.Error(t, err)
	r.Store = diagnosticReadOnlyStore{Store: store}
	report, err = r.Diagnose(ctx, "run")
	require.NoError(t, err)
	require.Equal(t, "outcome_unknown", report.Status)
	require.Contains(t, report.Actions, "reconcile_effects_before_resume")
	raw, err := json.Marshal(report)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "SECRET")
	require.Equal(t, 1, calls)
	r.Version = "v2"
	report, err = r.Diagnose(ctx, "run")
	require.NoError(t, err)
	require.Equal(t, "blocked", report.Status)
	require.Contains(t, report.Blockers, "version_mismatch")
	r.Version = "v1"
	r.Steps[0].RequireApproval = false
	report, err = r.Diagnose(ctx, "run")
	require.NoError(t, err)
	require.Contains(t, report.Blockers, "plan_mismatch")
}

type diagnosticReadOnlyStore struct{ Store }

func (diagnosticReadOnlyStore) Lock(context.Context, string) (func() error, error) {
	panic("diagnosis attempted lock")
}
func (diagnosticReadOnlyStore) Save(context.Context, string, *Snapshot) error {
	panic("diagnosis attempted write")
}

type diagnosticFailStore struct {
	Store
	err error
}

func (s diagnosticFailStore) Load(context.Context, string) (*Snapshot, error) { return nil, s.err }

func TestDiagnoseStoreFailureAndMissing(t *testing.T) {
	secret := errors.New("SECRET store error")
	r := &Runner{Store: diagnosticFailStore{err: secret}, Version: "v1"}
	_, err := r.Diagnose(context.Background(), "run")
	require.ErrorIs(t, err, secret)
	require.NotContains(t, err.Error(), "SECRET")
	r.Store = diagnosticFailStore{err: os.ErrNotExist}
	report, err := r.Diagnose(context.Background(), "run")
	require.NoError(t, err)
	require.Equal(t, "not_found", report.Status)
}

func TestDiagnoseOperationsUseOnlyExplicitEvidence(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)
	r := &Runner{Store: store, Version: "v1"}
	_, err = r.Run(ctx, "run", nil, "")
	require.NoError(t, err)
	for _, status := range []string{"pending", "failed", "completed", "waiting"} {
		key := ChildKey("parent", status)
		rec := &OperationRecord{ID: status, Key: key, InputHash: "hash", Status: status, Attempts: 1, MaxAttempts: 2, Value: json.RawMessage(`"SECRET output"`), FailureKind: "SECRET injected category"}
		if status == "waiting" {
			rec.Continuation = json.RawMessage(`[{"continuation":{"secret":"SECRET continuation"}}]`)
		}
		require.NoError(t, saveOperation(ctx, store, operationStorageID(key), rec))
	}
	r.Store = diagnosticReadOnlyStore{Store: store}
	report, err := r.Diagnose(ctx, "run", OperationRef{ParentKey: "parent", ID: "pending"}, OperationRef{ParentKey: "parent", ID: "failed"}, OperationRef{ParentKey: "parent", ID: "completed"}, OperationRef{ParentKey: "parent", ID: "waiting"}, OperationRef{ParentKey: "parent", ID: "missing"})
	require.NoError(t, err)
	require.Equal(t, "completed", report.Status)
	require.Equal(t, "pending", report.Operations[0].Status)
	require.Contains(t, report.Operations[0].Actions, "reconcile_effects_before_resume")
	require.Equal(t, "failed", report.Operations[1].Status)
	require.Contains(t, report.Operations[1].Actions, "review_failure_and_retry_policy")
	require.Equal(t, "completed", report.Operations[2].Status)
	require.Equal(t, "waiting", report.Operations[3].Status)
	require.Equal(t, "not_found", report.Operations[4].Status)
	raw, err := json.Marshal(report)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(raw), "SECRET"))
}
