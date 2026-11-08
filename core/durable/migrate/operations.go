// Package migrate 提供显式离线账本转换，不参与在线执行与自动恢复。
package migrate

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
)

// OperationRef 指明一个目标操作及其预期逻辑身份，Key 为 ChildKey 派生的完整键。
type OperationRef struct {
	Key string
	ID  string
}

// OperationOptions 默认为只读预览；Apply 为真才原子写入全部已验证目标。
type OperationOptions struct{ Apply bool }

// OperationReport 区分待转换和已经是当前格式的目标。
// Applied 只在 Apply 请求确认成功时为真；提交结果未知的错误须重新预览核对，不应清空记录。
type OperationReport struct {
	Pending []OperationRef
	Current []OperationRef
	Applied bool
}

// OperationsV1 把明确列出的 v1 操作快照转换为当前 v2；不调用业务函数或自动枚举运行。
// 调用前须停止并协调所有执行写入者、备份日志，并使用与运行相同的 Store/Scope。
// 目标按存储身份排序持锁，全部加载验证后用一次 SaveAll 原子提交；只改版本，保留全部快照字段与 Data 原字节。
// 当前 v2 记录也验证但不改写。重复调用安全；预览也持锁，以避免读取撕裂的批次。
// Store 的提交结果未知时可能已经全部写入；重新预览得到 Current 即可确认，不做反向回滚或重新执行业务。
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
