package memory

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestFactDefaultsAndProviderCapacity(t *testing.T) {
	limits := DefaultFactLimits()
	if limits.MaxFactBytes != 64*1024 {
		t.Fatalf("正文默认上限 = %d，期望 %d", limits.MaxFactBytes, 64*1024)
	}
	if limits.MaxMetadataBytes != 16*1024 {
		t.Fatalf("元数据默认上限 = %d，期望 %d", limits.MaxMetadataBytes, 16*1024)
	}
	if limits.DefaultPageSize != 50 || limits.MaxPageSize != 200 {
		t.Fatalf("分页默认值错误：default=%d max=%d", limits.DefaultPageSize, limits.MaxPageSize)
	}
	if limits.WorkerConcurrency != 4 || limits.MaxPendingPerTenant != 100_000 {
		t.Fatalf("维护默认值错误：workers=%d pending=%d", limits.WorkerConcurrency, limits.MaxPendingPerTenant)
	}

	caps := ProviderCapabilities{
		MaxFactBytes:            128 * 1024,
		MaxMetadataBytes:        32 * 1024,
		MaxPageSize:             200,
		MaxWorkerConcurrency:    8,
		MaxPendingPerTenant:     200_000,
		DurableOutbox:           true,
		AtomicFactOutbox:        true,
		ServerSideScopeFilter:   true,
		DurableFacts:            true,
		ChangeLog:               true,
		ResumableClear:          true,
		OnlineRebuild:           true,
		IdempotencyTombstones:   true,
		ReservedCleanupCapacity: true,
	}
	if err := ValidateFactLimits(limits, caps); err != nil {
		t.Fatalf("默认配置应符合 Provider 能力：%v", err)
	}

	caps.MaxFactBytes = 32 * 1024
	if err := ValidateFactLimits(limits, caps); !errors.Is(err, ErrProviderCapability) {
		t.Fatalf("超出 Provider 能力应返回 ErrProviderCapability，实际为 %v", err)
	}
}

func TestFactPublicInterfacesHaveNoUnexportedMethods(t *testing.T) {
	interfaces := []any{
		(*MemoryService)(nil),
		(*MemoryScopeResolver)(nil),
		(*FactStore)(nil),
		(*MemoryIndex)(nil),
		(*EmbedderResolver)(nil),
		(*MemoryMaintenance)(nil),
	}
	for _, value := range interfaces {
		typeOf := reflect.TypeOf(value).Elem()
		for i := 0; i < typeOf.NumMethod(); i++ {
			method := typeOf.Method(i)
			if method.PkgPath != "" {
				t.Errorf("接口 %s 包含未导出方法 %s", typeOf.Name(), method.Name)
			}
		}
	}
}

func TestMemoryErrorSupportsIsAsWithoutExposingCause(t *testing.T) {
	cause := errors.New("private fact body")
	wrapped := &MemoryError{Kind: ErrRevisionConflict, Op: "update", Cause: cause}
	if !errors.Is(wrapped, ErrRevisionConflict) || !errors.Is(wrapped, cause) {
		t.Fatal("MemoryError 应支持通过 errors.Is 匹配分类和底层错误")
	}
	var got *MemoryError
	if !errors.As(wrapped, &got) || got != wrapped {
		t.Fatal("MemoryError 应支持通过 errors.As 读取结构化错误")
	}
	if strings.Contains(wrapped.Error(), cause.Error()) {
		t.Fatal("面向用户的错误文本不能泄露底层事实内容")
	}
}

func TestFactProviderMustDeclareLifecycleCapabilities(t *testing.T) {
	caps := ProviderCapabilities{
		MaxFactBytes: 1024, MaxMetadataBytes: 1024, MaxPageSize: 20,
		MaxWorkerConcurrency: 1, MaxPendingPerTenant: 10,
		DurableOutbox: true, AtomicFactOutbox: true, ServerSideScopeFilter: true,
		DurableFacts: true, ChangeLog: true, ResumableClear: true,
		OnlineRebuild: true, IdempotencyTombstones: true, ReservedCleanupCapacity: true,
	}
	caps.ReservedCleanupCapacity = false
	if err := ValidateFactLimits(DefaultFactLimits(), caps); !errors.Is(err, ErrProviderCapability) {
		t.Fatalf("缺少清理保留容量能力应拒绝 Provider，实际为 %v", err)
	}
}
