// Package migrate provides explicit offline journal conversions and takes no
// part in online execution or automatic recovery.
package migrate

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
)

// OperationRef identifies a target operation and its expected logical identity;
// Key is the full key derived by ChildKey.
type OperationRef struct {
	Key string
	ID  string
}

// OperationOptions defaults to a read-only preview; only when Apply is true are
// all validated targets written atomically.
type OperationOptions struct{ Apply bool }

// OperationReport distinguishes targets awaiting conversion from those already
// in the current format. Applied is true only when an Apply request is
// confirmed successful; on errors with an unknown commit outcome, preview again
// to check rather than clearing records.
type OperationReport struct {
	Pending []OperationRef
	Current []OperationRef
	Applied bool
}

// OperationsV1 converts explicitly listed v1 operation snapshots to the current
// v2; it does not call business functions or enumerate runs automatically.
// Before calling it, stop and coordinate all execution writers, back up the
// journal, and use the same Store/Scope as the runs. Targets are locked in
// storage-identity order, all loaded and validated, and then committed
// atomically with one SaveAll; only the version changes, and all snapshot
// fields and the original Data bytes are preserved. Current v2 records are
// validated but not rewritten. Repeated calls are safe; previews also hold the
// locks to avoid reading a torn batch. When the Store's commit outcome is
// unknown everything may already be written; a fresh preview that reports
// Current confirms it, with no reverse rollback or re-execution of business
// logic.
func OperationsV1(ctx context.Context, store durable.Store, refs []OperationRef, options OperationOptions) (report OperationReport, err error) {
	if ctx == nil || store == nil || len(refs) == 0 {
		return report, migrationError(agentErrors.CodeInvalidInput, "需要上下文、日志存储和明确的目标操作")
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	ordered := slices.Clone(refs)
	for _, ref := range ordered {
		if ref.Key == "" || ref.ID == "" {
			return report, migrationError(agentErrors.CodeInvalidInput, "目标操作需要完整键和逻辑ID")
		}
	}
	slices.SortFunc(ordered, func(a, b OperationRef) int {
		return strings.Compare(durable.OperationStorageID(a.Key), durable.OperationStorageID(b.Key))
	})
	for i := 1; i < len(ordered); i++ {
		if ordered[i-1].Key == ordered[i].Key {
			return report, migrationError(agentErrors.CodeInvalidInput, "目标操作存储身份重复")
		}
	}
	var unlocks []func() error
	defer func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			err = errors.Join(err, unlocks[i]())
		}
	}()
	for _, ref := range ordered {
		unlock, err := store.Lock(ctx, durable.OperationStorageID(ref.Key))
		if err != nil {
			return report, err
		}
		unlocks = append(unlocks, unlock)
	}
	var writes []durable.Write
	for _, ref := range ordered {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		storageID := durable.OperationStorageID(ref.Key)
		snapshot, err := store.Load(ctx, storageID)
		if err != nil {
			return report, err
		}
		if snapshot == nil {
			return report, migrationError(agentErrors.CodeStateValidation, "目标操作快照为空")
		}
		converted := *snapshot
		converted.Data = slices.Clone(snapshot.Data)
		retired := snapshot.Version == "goagent-operation-v1"
		if retired {
			converted.Version = "goagent-operation-v2"
		}
		record, err := durable.DecodeOperationRecord(&converted, ref.Key, ref.ID)
		if err != nil {
			return report, err
		}
		if retired && (record.Status == "waiting" || record.Reconciled) {
			return report, migrationError(agentErrors.CodeStateValidation, "v1操作不能包含等待或效果核对语义")
		}
		if retired {
			report.Pending = append(report.Pending, ref)
			writes = append(writes, durable.Write{ID: storageID, Snapshot: &converted})
		} else {
			report.Current = append(report.Current, ref)
		}
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if !options.Apply {
		return report, nil
	}
	if len(writes) > 0 {
		if err := store.SaveAll(ctx, writes...); err != nil {
			return report, err
		}
	}
	report.Applied = true
	return report, nil
}

func migrationError(code agentErrors.ErrorCode, message string) error {
	return agentErrors.New(code, message).WithComponent("durable_migration").WithOperation("operations_v1")
}
