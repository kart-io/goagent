package memory

import "errors"

var (
	ErrAuthorization       = errors.New("事实记忆授权失败")
	ErrRevisionConflict    = errors.New("事实修订冲突")
	ErrIdempotencyConflict = errors.New("幂等键请求冲突")
	ErrIdempotencyExpired  = errors.New("幂等键已过期")
	ErrDeleted             = errors.New("事实已删除")
	ErrGone                = errors.New("事实不可用")
	ErrSnapshotInvalidated = errors.New("事实快照已失效")
	ErrCapacityExceeded    = errors.New("事实记忆容量已满")
	ErrInvalidCursor       = errors.New("分页游标无效")
	ErrMaintenanceNotFound = errors.New("维护任务不存在")
	ErrInvalidLimits       = errors.New("事实记忆限额无效")
	ErrProviderCapability  = errors.New("Provider 能力不足")
	ErrInvalidFact         = errors.New("事实内容无效")
	ErrFeatureUnavailable  = errors.New("事实记忆能力尚未启用")
)

// MemoryError 是可分类的事实记忆错误；Error 不格式化底层 cause，避免泄露事实内容。
type MemoryError struct {
	Kind  error
	Op    string
	Cause error
}

// Error 返回不包含事实正文的错误描述。
func (e *MemoryError) Error() string {
	if e == nil {
		return "事实记忆错误"
	}
	if e.Kind == nil {
		return e.Op
	}
	if e.Op == "" {
		return e.Kind.Error()
	}
	return e.Op + ": " + e.Kind.Error()
}

// Unwrap 保留错误分类和底层错误的 errors.Is/errors.As 查询能力。
func (e *MemoryError) Unwrap() []error {
	if e == nil {
		return nil
	}
	out := make([]error, 0, 2)
	if e.Kind != nil {
		out = append(out, e.Kind)
	}
	if e.Cause != nil {
		out = append(out, e.Cause)
	}
	return out
}
