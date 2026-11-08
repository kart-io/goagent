package memory

import (
	"fmt"
	"time"
)

// FactLimits 是事实服务的可配置限额初始值。
// Provider 可按自身能力降低或调整这些默认值。
type FactLimits struct {
	MaxFactBytes         int
	MaxMetadataBytes     int
	DefaultPageSize      int
	MaxPageSize          int
	WorkerConcurrency    int
	MaxPendingPerTenant  int
	MaxRetries           int
	CursorTTL            time.Duration
	IdempotencyRetention time.Duration
}

// ProviderCapabilities 声明持久 Provider 可安全支持的上限和必要能力。
type ProviderCapabilities struct {
	MaxFactBytes            int
	MaxMetadataBytes        int
	MaxPageSize             int
	MaxWorkerConcurrency    int
	MaxPendingPerTenant     int
	DurableOutbox           bool
	AtomicFactOutbox        bool
	ServerSideScopeFilter   bool
	DurableFacts            bool
	ChangeLog               bool
	ResumableClear          bool
	OnlineRebuild           bool
	IdempotencyTombstones   bool
	ReservedCleanupCapacity bool
}

// DefaultFactLimits 返回事实服务的初始限额建议。
func DefaultFactLimits() FactLimits {
	return FactLimits{
		MaxFactBytes:         64 * 1024,
		MaxMetadataBytes:     16 * 1024,
		DefaultPageSize:      50,
		MaxPageSize:          200,
		WorkerConcurrency:    4,
		MaxPendingPerTenant:  100_000,
		MaxRetries:           8,
		CursorTTL:            15 * time.Minute,
		IdempotencyRetention: 30 * 24 * time.Hour,
	}
}

// ValidateFactLimits 校验配置是否为正值并符合 Provider 声明能力。
func ValidateFactLimits(limits FactLimits, caps ProviderCapabilities) error {
	if limits.MaxFactBytes < 1 || limits.MaxMetadataBytes < 1 || limits.DefaultPageSize < 1 ||
		limits.MaxPageSize < limits.DefaultPageSize || limits.WorkerConcurrency < 1 ||
		limits.MaxPendingPerTenant < 1 || limits.MaxRetries < 1 || limits.CursorTTL <= 0 ||
		limits.IdempotencyRetention <= 0 {
		return fmt.Errorf("%w: 事实限额必须为正值且分页范围有效", ErrInvalidLimits)
	}
	if caps.MaxFactBytes < 1 || caps.MaxMetadataBytes < 1 || caps.MaxPageSize < 1 ||
		caps.MaxWorkerConcurrency < 1 || caps.MaxPendingPerTenant < 1 ||
		!caps.DurableOutbox || !caps.AtomicFactOutbox || !caps.ServerSideScopeFilter ||
		!caps.DurableFacts || !caps.ChangeLog || !caps.ResumableClear || !caps.OnlineRebuild ||
		!caps.IdempotencyTombstones || !caps.ReservedCleanupCapacity {
		return fmt.Errorf("%w: Provider 未声明必需的持久化、原子 outbox 或服务端 scope 过滤能力", ErrProviderCapability)
	}
	if limits.MaxFactBytes > caps.MaxFactBytes || limits.MaxMetadataBytes > caps.MaxMetadataBytes ||
		limits.MaxPageSize > caps.MaxPageSize || limits.WorkerConcurrency > caps.MaxWorkerConcurrency ||
		limits.MaxPendingPerTenant > caps.MaxPendingPerTenant {
		return fmt.Errorf("%w: 配置超过 Provider 声明能力", ErrProviderCapability)
	}
	return nil
}
