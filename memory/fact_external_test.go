package memory_test

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/memory"
)

// 外部包实现示例只使用 GoAgent 导出的事实记忆接口与类型。
type externalMemoryService struct{}

func (externalMemoryService) Save(context.Context, memory.WriteScope, memory.IdempotencyKey, memory.FactInput) (memory.SaveResult, error) {
	return memory.SaveResult{}, nil
}
func (externalMemoryService) Get(context.Context, memory.MemoryAccess, memory.FactID) (memory.Fact, error) {
	return memory.Fact{}, nil
}
func (externalMemoryService) List(context.Context, memory.MemoryAccess, memory.FactFilter, memory.PageRequest) (memory.FactPage, error) {
	return memory.FactPage{}, nil
}
func (externalMemoryService) Update(context.Context, memory.WriteScope, memory.FactID, memory.Revision, memory.FactPatch) (memory.Fact, error) {
	return memory.Fact{}, nil
}
func (externalMemoryService) Supersede(context.Context, memory.WriteScope, memory.FactID, memory.Revision, memory.IdempotencyKey, memory.FactInput) (memory.SaveResult, error) {
	return memory.SaveResult{}, nil
}
func (externalMemoryService) Delete(context.Context, memory.WriteScope, memory.FactID, memory.Revision) (memory.DeleteResult, error) {
	return memory.DeleteResult{}, nil
}
func (externalMemoryService) DeleteScope(context.Context, memory.ClearAccess, memory.MemoryScope, memory.IdempotencyKey) (memory.ClearOperation, error) {
	return memory.ClearOperation{}, nil
}
func (externalMemoryService) Recall(context.Context, memory.MemoryAccess, string, memory.RecallOptions) (memory.RecallResult, error) {
	return memory.RecallResult{}, nil
}
func (externalMemoryService) ResolveSnapshot(context.Context, memory.MemoryAccess, memory.SnapshotRef) (memory.RecallResult, error) {
	return memory.RecallResult{}, nil
}
func (externalMemoryService) ListChanges(context.Context, memory.MemoryAccess, memory.FactID, memory.PageRequest) (memory.ChangePage, error) {
	return memory.ChangePage{}, nil
}

var _ memory.MemoryService = externalMemoryService{}

func TestFactExternalPackageCanImplementInterfaces(t *testing.T) {
	var service memory.MemoryService = externalMemoryService{}
	if service == nil {
		t.Fatal("外部 Service 未能满足公开接口")
	}
}

type externalScopeResolver struct{}

func (externalScopeResolver) Resolve(context.Context, string) (memory.MemoryAccess, error) {
	return memory.MemoryAccess{}, nil
}

// externalFactStore 仅用于编译期确认 Store 契约完全由导出类型组成。
type externalFactStore struct{}

func (externalFactStore) ApplyFact(context.Context, memory.WriteScope, memory.FactMutation) (memory.FactMutationResult, error) {
	return memory.FactMutationResult{}, nil
}
func (externalFactStore) Get(context.Context, memory.MemoryAccess, memory.FactID, memory.Revision) (memory.Fact, error) {
	return memory.Fact{}, nil
}
func (externalFactStore) List(context.Context, memory.MemoryAccess, memory.FactFilter, memory.PageRequest) (memory.FactPage, error) {
	return memory.FactPage{}, nil
}
func (externalFactStore) ListChanges(context.Context, memory.MemoryAccess, memory.FactID, memory.PageRequest) (memory.ChangePage, error) {
	return memory.ChangePage{}, nil
}
func (externalFactStore) ClaimIndexMutations(context.Context, int) ([]memory.IndexMutation, error) {
	return nil, nil
}
func (externalFactStore) AckIndexMutation(context.Context, memory.IndexMutation, memory.IndexMutationResult) error {
	return nil
}
func (externalFactStore) BeginScopeClear(context.Context, memory.ClearAccess, memory.MemoryScope, memory.IdempotencyKey) (memory.ClearOperation, error) {
	return memory.ClearOperation{}, nil
}
func (externalFactStore) GetClearOperation(context.Context, memory.MemoryAccess, memory.ClearOperationID) (memory.ClearOperation, error) {
	return memory.ClearOperation{}, nil
}
func (externalFactStore) ClaimClearOperations(context.Context, int) ([]memory.ClearOperation, error) {
	return nil, nil
}
func (externalFactStore) AckClearOperation(context.Context, memory.ClearOperationID, memory.ClearProgress) error {
	return nil
}
func (externalFactStore) ListMaintenance(context.Context, memory.MaintenanceAccess, memory.MaintenanceFilter, memory.PageRequest) (memory.MaintenancePage, error) {
	return memory.MaintenancePage{}, nil
}
func (externalFactStore) RetryMutation(context.Context, memory.MaintenanceAccess, memory.MutationID) error {
	return nil
}
func (externalFactStore) CreateRebuild(context.Context, memory.MaintenancePrincipal, memory.EmbeddingProfile) (memory.Rebuild, error) {
	return memory.Rebuild{}, nil
}
func (externalFactStore) GetRebuild(context.Context, memory.MaintenanceAccess, memory.RebuildID) (memory.Rebuild, error) {
	return memory.Rebuild{}, nil
}
func (externalFactStore) AdvanceRebuild(context.Context, memory.RebuildID, memory.RebuildProgress) error {
	return nil
}
func (externalFactStore) ActiveIndexProfile(context.Context) (memory.IndexProfileState, error) {
	return memory.IndexProfileState{}, nil
}
func (externalFactStore) SwitchActiveGeneration(context.Context, memory.IndexGeneration, memory.RebuildID, memory.StoreSequence) error {
	return nil
}

// externalMemoryIndex 仅用于编译期确认 Index 契约。
type externalMemoryIndex struct{}

func (externalMemoryIndex) ApplyMutation(context.Context, memory.IndexMutation) error { return nil }
func (externalMemoryIndex) Search(context.Context, memory.IndexQuery) (memory.IndexPage, error) {
	return memory.IndexPage{}, nil
}

type externalEmbedder struct{}

func (externalEmbedder) Embed(context.Context, string) ([]float32, error) { return nil, nil }
func (externalEmbedder) Profile() memory.EmbeddingProfile                 { return memory.EmbeddingProfile{} }

type externalEmbedderResolver struct{}

func (externalEmbedderResolver) Resolve(context.Context, memory.EmbeddingProfileID) (memory.Embedder, error) {
	return externalEmbedder{}, nil
}

type externalMaintenance struct{}

func (externalMaintenance) Run(context.Context) error { return nil }
func (externalMaintenance) Status(context.Context, memory.MaintenanceAccess, memory.MaintenanceFilter, memory.PageRequest) (memory.MaintenancePage, error) {
	return memory.MaintenancePage{}, nil
}
func (externalMaintenance) Retry(context.Context, memory.MaintenanceAccess, memory.MutationID) error {
	return nil
}
func (externalMaintenance) ClearStatus(context.Context, memory.MaintenanceAccess, memory.ClearOperationID) (memory.ClearOperation, error) {
	return memory.ClearOperation{}, nil
}
func (externalMaintenance) StartRebuild(context.Context, memory.MaintenanceAccess, memory.EmbeddingProfile) (memory.RebuildID, error) {
	return "", nil
}
func (externalMaintenance) RebuildStatus(context.Context, memory.MaintenanceAccess, memory.RebuildID) (memory.Rebuild, error) {
	return memory.Rebuild{}, nil
}

var (
	_ memory.MemoryScopeResolver = externalScopeResolver{}
	_ memory.FactStore           = externalFactStore{}
	_ memory.MemoryIndex         = externalMemoryIndex{}
	_ memory.Embedder            = externalEmbedder{}
	_ memory.EmbedderResolver    = externalEmbedderResolver{}
	_ memory.MemoryMaintenance   = externalMaintenance{}
)
