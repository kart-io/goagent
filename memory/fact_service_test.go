package memory

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func testMemoryService(t *testing.T) (*DefaultMemoryService, *factStoreFixture, WriteScope) {
	t.Helper()
	store := newFactStoreFixture()
	limits := DefaultFactLimits()
	limits.MaxFactBytes = 1024
	limits.MaxMetadataBytes = 1024
	limits.MaxPendingPerTenant = 100
	caps := ProviderCapabilities{
		MaxFactBytes: 1024, MaxMetadataBytes: 1024, MaxPageSize: 200,
		MaxWorkerConcurrency: 4, MaxPendingPerTenant: 100,
		DurableOutbox: true, AtomicFactOutbox: true, ServerSideScopeFilter: true,
		DurableFacts: true, ChangeLog: true, ResumableClear: true,
		OnlineRebuild: true, IdempotencyTombstones: true, ReservedCleanupCapacity: true,
	}
	ids := 0
	svc, err := NewDefaultMemoryService(MemoryServiceConfig{
		Store: store, Limits: limits, Capabilities: caps,
		Now:   func() time.Time { return time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC) },
		NewID: func() (FactID, error) { ids++; return FactID(fmt.Sprintf("fact-%d", ids)), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	scope := MemoryScope{TenantID: "tenant-a", OwnerKind: OwnerAgent, OwnerID: "user-a", AgentID: "agent-a"}
	access := MemoryAccess{
		Principal:  Principal{TenantID: "tenant-a", UserID: "user-a", AgentID: "agent-a"},
		ReadScopes: []MemoryScope{scope}, WriteScopes: []MemoryScope{scope},
	}
	return svc, store, WriteScope{Access: access, Target: scope}
}

func TestFactSaveIdempotentSameRequest(t *testing.T) {
	svc, store, scope := testMemoryService(t)
	input := FactInput{Content: "preferred language is Mandarin", RecallClass: RecallAlways}
	first, err := svc.Save(context.Background(), scope, "save-1", input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Save(context.Background(), scope, "save-1", input)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("重试收据不稳定：first=%+v second=%+v", first, second)
	}
	if got := store.factCount(); got != 1 {
		t.Fatalf("重复保存创建了 %d 条事实，期望 1", got)
	}
	if got := store.changeCount(); got != 1 {
		t.Fatalf("重复保存写入了 %d 条变更，期望 1", got)
	}
}

func TestFactIdempotencyRejectsDifferentRequest(t *testing.T) {
	svc, store, scope := testMemoryService(t)
	if _, err := svc.Save(context.Background(), scope, "same-key", FactInput{Content: "first"}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Save(context.Background(), scope, "same-key", FactInput{Content: "different"})
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("期望幂等冲突，实际为 %v", err)
	}
	if got := store.factCount(); got != 1 {
		t.Fatalf("冲突请求修改了事实，数量=%d", got)
	}
}

func TestFactSaveDeleteRestartReplayDoesNotLeakOrResurrect(t *testing.T) {
	svc, store, scope := testMemoryService(t)
	receipt, err := svc.Save(context.Background(), scope, "delete-replay", FactInput{Content: "private fact"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Delete(context.Background(), scope, receipt.FactID, receipt.Revision); err != nil {
		t.Fatal(err)
	}
	restarted, err := newFactServiceOnStore(store, scope.Access)
	if err != nil {
		t.Fatal(err)
	}
	got, err := restarted.Save(context.Background(), scope, "delete-replay", FactInput{Content: "private fact"})
	if !errors.Is(err, ErrGone) {
		t.Fatalf("删除后幂等重试应返回 Gone，实际为 %+v, %v", got, err)
	}
	if got.OperationID != receipt.OperationID || got.FactID != receipt.FactID {
		t.Fatalf("删除重放未保留稳定收据：got=%+v original=%+v", got, receipt)
	}
	if _, err := restarted.Get(context.Background(), scope.Access, receipt.FactID); !errors.Is(err, ErrGone) {
		t.Fatalf("删除事实仍可读取：%v", err)
	}
	if store.factCount() != 1 {
		t.Fatal("幂等重放复活或重复创建了事实")
	}
}

func TestFactExpiredIdempotencyTombstoneSurvivesGCAndRestart(t *testing.T) {
	svc, store, scope := testMemoryService(t)
	store.now = time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	store.receiptRetention = time.Second
	if _, err := svc.Save(context.Background(), scope, "expired-key", FactInput{Content: "old fact"}); err != nil {
		t.Fatal(err)
	}
	store.now = store.now.Add(2 * time.Second)
	store.collectExpiredReceipts()
	restarted, err := newFactServiceOnStore(store, scope.Access)
	if err != nil {
		t.Fatal(err)
	}
	_, err = restarted.Save(context.Background(), scope, "expired-key", FactInput{Content: "old fact"})
	if !errors.Is(err, ErrIdempotencyExpired) {
		t.Fatalf("清理/重启后旧键应被拒绝，实际为 %v", err)
	}
	if store.factCount() != 1 {
		t.Fatal("过期幂等键重放创建了新事实")
	}
}

func TestFactRevisionCASHasSingleWinner(t *testing.T) {
	svc, _, scope := testMemoryService(t)
	receipt, err := svc.Save(context.Background(), scope, "create", FactInput{Content: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	contents := []string{"v2-a", "v2-b"}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, content := range contents {
		content := content
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.Update(context.Background(), scope, receipt.FactID, receipt.Revision, FactPatch{Content: &content})
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	ok, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			ok++
		} else if errors.Is(err, ErrRevisionConflict) {
			conflicts++
		} else {
			t.Fatalf("意外更新错误：%v", err)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatalf("CAS 结果成功=%d 冲突=%d，期望各 1", ok, conflicts)
	}
}

func TestFactSupersedeLeavesOneActiveFact(t *testing.T) {
	svc, store, scope := testMemoryService(t)
	old, err := svc.Save(context.Background(), scope, "old", FactInput{Content: "lives in Hangzhou"})
	if err != nil {
		t.Fatal(err)
	}
	newFact, err := svc.Supersede(context.Background(), scope, old.FactID, old.Revision, "replace", FactInput{Content: "lives in Suzhou"})
	if err != nil {
		t.Fatal(err)
	}
	if newFact.FactID == old.FactID {
		t.Fatal("替代必须创建新逻辑事实")
	}
	if _, err := svc.Get(context.Background(), scope.Access, old.FactID); !errors.Is(err, ErrGone) {
		t.Fatalf("旧事实仍可读：%v", err)
	}
	current, err := svc.Get(context.Background(), scope.Access, newFact.FactID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != FactActive || current.SupersedesID != old.FactID {
		t.Fatalf("新事实关联错误：%+v", current)
	}
	if got := store.activeFactCount(); got != 1 {
		t.Fatalf("有效事实数=%d，期望 1", got)
	}
}

func TestFactDeleteImmediatelyBlocksHydration(t *testing.T) {
	svc, _, scope := testMemoryService(t)
	receipt, err := svc.Save(context.Background(), scope, "delete", FactInput{Content: "gone"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Delete(context.Background(), scope, receipt.FactID, receipt.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(context.Background(), scope.Access, receipt.FactID); !errors.Is(err, ErrGone) {
		t.Fatalf("逻辑删除后 Get 返回 %v", err)
	}
}

func TestFactClearScopeHasImmediateVisibilityBoundary(t *testing.T) {
	svc, store, scope := testMemoryService(t)
	receipt, err := svc.Save(context.Background(), scope, "before-clear", FactInput{Content: "sensitive"})
	if err != nil {
		t.Fatal(err)
	}
	clearAccess := ClearAccess{Principal: scope.Access.Principal, TargetScopes: []MemoryScope{scope.Target}, Capabilities: []string{"memory.clear"}}
	op, err := svc.DeleteScope(context.Background(), clearAccess, scope.Target, "clear-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(context.Background(), scope.Access, receipt.FactID); !errors.Is(err, ErrGone) {
		t.Fatalf("清空建立边界后事实仍可见：%v", err)
	}
	page, err := svc.List(context.Background(), scope.Access, FactFilter{}, PageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("清空后 List 返回 %d 条事实", len(page.Items))
	}
	if op.State == "complete" {
		t.Fatal("开始清空时不能提前报告完成")
	}
	if got := store.blockedScopeCount(); got != 1 {
		t.Fatalf("清空期间封锁作用域数=%d", got)
	}
}

func TestFactClearRejectsWritesUntilComplete(t *testing.T) {
	svc, store, scope := testMemoryService(t)
	clearAccess := ClearAccess{Principal: scope.Access.Principal, TargetScopes: []MemoryScope{scope.Target}, Capabilities: []string{"memory.clear"}}
	op, err := svc.DeleteScope(context.Background(), clearAccess, scope.Target, "clear-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Save(context.Background(), scope, "during-clear", FactInput{Content: "blocked"}); !errors.Is(err, ErrCapacityExceeded) {
		t.Fatalf("清理期间写入未拒绝：%v", err)
	}
	if err := store.AckClearOperation(context.Background(), op.ID, ClearProgress{Done: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Save(context.Background(), scope, "after-clear", FactInput{Content: "allowed"}); err != nil {
		t.Fatalf("清理完成后仍不能写入：%v", err)
	}
}

func TestFactUserSharedScopeCanBeReadWithoutAgentBinding(t *testing.T) {
	svc, _, originalScope := testMemoryService(t)
	shared := MemoryScope{TenantID: "tenant-a", OwnerKind: OwnerUser, OwnerID: "user-a"}
	access := MemoryAccess{Principal: originalScope.Access.Principal, ReadScopes: []MemoryScope{shared}, WriteScopes: []MemoryScope{shared}}
	scope := WriteScope{Access: access, Target: shared}
	receipt, err := svc.Save(context.Background(), scope, "shared", FactInput{Content: "shared preference"})
	if err != nil {
		t.Fatal(err)
	}
	fact, err := svc.Get(context.Background(), access, receipt.FactID)
	if err != nil {
		t.Fatalf("user_shared scope 读取失败：%v", err)
	}
	if fact.Scope != shared {
		t.Fatalf("scope 不匹配：%+v", fact.Scope)
	}
}

func (s *factStoreFixture) blockedScopeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.blockedScopes)
}

func newFactServiceOnStore(store *factStoreFixture, access MemoryAccess) (*DefaultMemoryService, error) {
	limits := DefaultFactLimits()
	limits.MaxFactBytes, limits.MaxMetadataBytes, limits.MaxPendingPerTenant = 1024, 1024, 100
	caps := store.capabilities()
	return NewDefaultMemoryService(MemoryServiceConfig{Store: store, Limits: limits, Capabilities: caps, Now: func() time.Time { return store.now }, NewID: store.nextID})
}

type factStoreFixture struct {
	mu               sync.Mutex
	facts            map[FactID]Fact
	idempotency      map[string]fixtureIdempotency
	changes          []ChangeRecord
	outbox           []IndexMutation
	seq              StoreSequence
	next             int
	now              time.Time
	receiptRetention time.Duration
	clears           map[ClearOperationID]ClearOperation
	clearKeys        map[string]ClearOperationID
	blockedScopes    map[MemoryScope]ClearOperationID
	mutationResults  map[MutationID]IndexMutationResult
	nextMutation     int
}

type fixtureIdempotency struct {
	digest  string
	receipt MutationReceipt
	state   FactStatus
	expires time.Time
	expired bool
}

func newFactStoreFixture() *factStoreFixture {
	return &factStoreFixture{facts: map[FactID]Fact{}, idempotency: map[string]fixtureIdempotency{}, clears: map[ClearOperationID]ClearOperation{}, clearKeys: map[string]ClearOperationID{}, blockedScopes: map[MemoryScope]ClearOperationID{}, mutationResults: map[MutationID]IndexMutationResult{}, now: time.Now().UTC(), receiptRetention: 30 * 24 * time.Hour}
}
func (s *factStoreFixture) capabilities() ProviderCapabilities {
	return ProviderCapabilities{MaxFactBytes: 1024, MaxMetadataBytes: 1024, MaxPageSize: 200, MaxWorkerConcurrency: 4, MaxPendingPerTenant: 100, DurableOutbox: true, AtomicFactOutbox: true, ServerSideScopeFilter: true, DurableFacts: true, ChangeLog: true, ResumableClear: true, OnlineRebuild: true, IdempotencyTombstones: true, ReservedCleanupCapacity: true}
}
func (s *factStoreFixture) nextID() (FactID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	return FactID(fmt.Sprintf("fact-%d", s.next)), nil
}
func (s *factStoreFixture) factCount() int   { s.mu.Lock(); defer s.mu.Unlock(); return len(s.facts) }
func (s *factStoreFixture) changeCount() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.changes) }
func (s *factStoreFixture) activeFactCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, f := range s.facts {
		if f.Status == FactActive {
			n++
		}
	}
	return n
}
func (s *factStoreFixture) enqueue(m IndexMutation) {
	s.nextMutation++
	m.ID = MutationID(fmt.Sprintf("mutation-%d", s.nextMutation))
	s.outbox = append(s.outbox, m)
}
func (s *factStoreFixture) collectExpiredReceipts() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, record := range s.idempotency {
		if !record.expires.IsZero() && !s.now.Before(record.expires) {
			record.expired = true
			record.receipt = MutationReceipt{}
			s.idempotency[key] = record
		}
	}
}
func (s *factStoreFixture) ApplyFact(ctx context.Context, scope WriteScope, mutation FactMutation) (FactMutationResult, error) {
	if err := ctx.Err(); err != nil {
		return FactMutationResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, blocked := s.blockedScopes[scope.Target]; blocked && mutation.Action != ChangeDeleted {
		return FactMutationResult{}, &MemoryError{Kind: ErrCapacityExceeded, Op: "作用域正在清理"}
	}
	idemKey := string(scope.Target.TenantID) + "|" + string(mutation.Action) + "|" + string(scope.Target.OwnerKind) + "|" + scope.Target.OwnerID + "|" + scope.Target.AgentID + "|" + string(mutation.IdempotencyKey)
	if mutation.IdempotencyKey != "" {
		if old, ok := s.idempotency[idemKey]; ok {
			if old.digest != mutation.RequestDigest {
				return FactMutationResult{}, &MemoryError{Kind: ErrIdempotencyConflict}
			}
			if old.expired {
				return FactMutationResult{}, &MemoryError{Kind: ErrIdempotencyExpired}
			}
			return FactMutationResult{Receipt: old.receipt, Lifecycle: old.state}, nil
		}
	}
	now := s.now
	var fact Fact
	var old Fact
	if mutation.Action == ChangeCreated {
		id := mutation.NewFactID
		if id == "" {
			id = mutation.FactID
		}
		if _, exists := s.facts[id]; exists {
			return FactMutationResult{}, &MemoryError{Kind: ErrRevisionConflict}
		}
		fact = factFromInput(id, scope.Target, mutation.Input, mutation.Actor, now)
		fact.Revision, fact.MutationVersion = 1, 1
		s.facts[id] = fact
	} else {
		var ok bool
		old, ok = s.facts[mutation.FactID]
		if !ok {
			return FactMutationResult{}, &MemoryError{Kind: ErrGone}
		}
		if old.Scope != scope.Target {
			return FactMutationResult{}, &MemoryError{Kind: ErrAuthorization}
		}
		if old.Status != FactActive || (!old.ExpiresAt.IsZero() && !now.Before(old.ExpiresAt)) {
			return FactMutationResult{Lifecycle: old.Status}, &MemoryError{Kind: ErrGone}
		}
		if old.Revision != mutation.ExpectedRevision {
			return FactMutationResult{}, &MemoryError{Kind: ErrRevisionConflict}
		}
		fact = old
		switch mutation.Action {
		case ChangeUpdated:
			if mutation.Patch == nil {
				return FactMutationResult{}, &MemoryError{Kind: ErrInvalidFact}
			}
			applyFactPatch(&fact, *mutation.Patch)
		case ChangeSuperseded:
			fact.Status = FactSuperseded
			fact.Revision++
			fact.MutationVersion++
			fact.UpdatedAt = now
			s.facts[fact.ID] = fact
			newID := mutation.NewFactID
			if newID == "" {
				return FactMutationResult{}, &MemoryError{Kind: ErrInvalidFact}
			}
			newFact := factFromInput(newID, scope.Target, mutation.Input, mutation.Actor, now)
			newFact.Revision, newFact.MutationVersion, newFact.SupersedesID = 1, 1, old.ID
			fact = newFact
		case ChangeDeleted:
			fact.Status = FactDeleted
		default:
			return FactMutationResult{}, &MemoryError{Kind: ErrInvalidFact}
		}
		if mutation.Action != ChangeSuperseded {
			fact.Revision++
			fact.MutationVersion++
			fact.UpdatedAt = now
		}
		s.facts[fact.ID] = fact
		for key, record := range s.idempotency {
			if record.receipt.FactID == fact.ID {
				record.state = fact.Status
				s.idempotency[key] = record
			}
		}
	}
	s.seq++
	receipt := MutationReceipt{OperationID: fmt.Sprintf("op-%d", s.seq), FactID: fact.ID, Revision: fact.Revision, IndexState: IndexPending}
	if mutation.IdempotencyKey != "" {
		s.idempotency[idemKey] = fixtureIdempotency{digest: mutation.RequestDigest, receipt: receipt, state: fact.Status, expires: now.Add(s.receiptRetention)}
	}
	if mutation.Action == ChangeSuperseded {
		s.changes = append(s.changes, ChangeRecord{FactID: old.ID, Scope: old.Scope, Revision: old.Revision + 1, Action: mutation.Action, Actor: mutation.Actor, At: now, SupersedesID: fact.ID})
		s.enqueue(IndexMutation{FactID: old.ID, Scope: old.Scope, Revision: old.Revision + 1, MutationVersion: old.MutationVersion + 1, StoreSequence: s.seq, Action: MutationTombstone})
	}
	s.changes = append(s.changes, ChangeRecord{FactID: fact.ID, Scope: fact.Scope, Revision: fact.Revision, Action: mutation.Action, Actor: mutation.Actor, At: now, Source: fact.Source, SupersedesID: fact.SupersedesID})
	action := MutationUpsert
	if fact.Status != FactActive {
		action = MutationTombstone
	}
	s.enqueue(IndexMutation{FactID: fact.ID, Scope: fact.Scope, Revision: fact.Revision, MutationVersion: fact.MutationVersion, StoreSequence: s.seq, Action: action})
	return FactMutationResult{Receipt: receipt, Lifecycle: fact.Status}, nil
}

func factFromInput(id FactID, scope MemoryScope, in FactInput, actor Principal, now time.Time) Fact {
	metadata := map[string]any{}
	for k, v := range in.Metadata {
		metadata[k] = v
	}
	class := in.RecallClass
	if class == "" {
		class = RecallNormal
	}
	return Fact{ID: id, Scope: scope, Content: in.Content, Metadata: metadata, Source: in.Source, Status: FactActive, CreatedAt: now, UpdatedAt: now, ExpiresAt: in.ExpiresAt, RecallClass: class, Priority: in.Priority, IndexState: IndexPending}
}

func applyFactPatch(f *Fact, p FactPatch) {
	if p.Content != nil {
		f.Content = *p.Content
	}
	if p.Metadata != nil {
		f.Metadata = map[string]any{}
		for k, v := range p.Metadata {
			f.Metadata[k] = v
		}
	}
	if p.Source != nil {
		f.Source = *p.Source
	}
	if p.ExpiresAt != nil {
		f.ExpiresAt = *p.ExpiresAt
	}
	if p.RecallClass != nil {
		f.RecallClass = *p.RecallClass
	}
	if p.Priority != nil {
		f.Priority = *p.Priority
	}
}

func (s *factStoreFixture) Get(ctx context.Context, access MemoryAccess, id FactID, revision Revision) (Fact, error) {
	if err := ctx.Err(); err != nil {
		return Fact{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.facts[id]
	if !ok {
		return Fact{}, &MemoryError{Kind: ErrGone}
	}
	if !scopeAllowed(access.ReadScopes, f.Scope) {
		return Fact{}, &MemoryError{Kind: ErrAuthorization}
	}
	if revision != 0 && revision != f.Revision {
		return Fact{}, &MemoryError{Kind: ErrRevisionConflict}
	}
	if f.Status != FactActive {
		return Fact{}, &MemoryError{Kind: ErrGone}
	}
	return f, nil
}
func (s *factStoreFixture) LoadIndexInput(ctx context.Context, mutation IndexMutation) (IndexInput, error) {
	if err := ctx.Err(); err != nil {
		return IndexInput{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.facts[mutation.FactID]
	if !ok {
		return IndexInput{}, &MemoryError{Kind: ErrGone}
	}
	return IndexInput{FactID: f.ID, Content: f.Content, MutationVersion: f.MutationVersion, Active: f.Status == FactActive}, nil
}
func (s *factStoreFixture) List(ctx context.Context, access MemoryAccess, filter FactFilter, page PageRequest) (FactPage, error) {
	if err := ctx.Err(); err != nil {
		return FactPage{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := FactPage{}
	for _, f := range s.facts {
		if !scopeAllowed(access.ReadScopes, f.Scope) || f.Status != FactActive {
			continue
		}
		if len(filter.Scopes) > 0 && !scopeAllowed(filter.Scopes, f.Scope) {
			continue
		}
		result.Items = append(result.Items, f)
	}
	if len(result.Items) > page.Limit {
		result.Items = result.Items[:page.Limit]
		result.NextCursor = "more"
	}
	return result, nil
}
func (s *factStoreFixture) ListChanges(ctx context.Context, access MemoryAccess, id FactID, page PageRequest) (ChangePage, error) {
	return ChangePage{}, nil
}
func (s *factStoreFixture) ClaimIndexMutations(ctx context.Context, limit int) ([]IndexMutation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []IndexMutation{}
	for _, m := range s.outbox {
		if _, ok := s.mutationResults[m.ID]; !ok {
			out = append(out, m)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
func (s *factStoreFixture) AckIndexMutation(ctx context.Context, m IndexMutation, result IndexMutationResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mutationResults[m.ID] = result
	return nil
}
func (s *factStoreFixture) BeginScopeClear(ctx context.Context, access ClearAccess, target MemoryScope, key IdempotencyKey) (ClearOperation, error) {
	if err := ctx.Err(); err != nil {
		return ClearOperation{}, err
	}
	if key == "" {
		return ClearOperation{}, &MemoryError{Kind: ErrIdempotencyConflict}
	}
	if !clearAllowed(access, target) {
		return ClearOperation{}, &MemoryError{Kind: ErrAuthorization}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	keyID := target.TenantID + "|" + string(target.OwnerKind) + "|" + target.OwnerID + "|" + target.AgentID + "|" + string(key)
	if id, ok := s.clearKeys[keyID]; ok {
		return s.clears[id], nil
	}
	for _, op := range s.clears {
		if op.Scope == target && op.State != "complete" {
			return op, nil
		}
	}
	s.seq++
	id := ClearOperationID(fmt.Sprintf("clear-%d", s.seq))
	op := ClearOperation{ID: id, Scope: target, State: "pending", Cutoff: s.seq, CreatedAt: s.now, UpdatedAt: s.now}
	s.clears[id] = op
	s.clearKeys[keyID] = id
	s.blockedScopes[target] = id
	for id, f := range s.facts {
		if f.Scope == target && f.Status == FactActive {
			f.Status = FactDeleted
			f.Revision++
			f.MutationVersion++
			f.UpdatedAt = s.now
			s.facts[id] = f
			s.seq++
			s.changes = append(s.changes, ChangeRecord{FactID: id, Scope: target, Revision: f.Revision, Action: ChangeDeleted, At: s.now})
			s.enqueue(IndexMutation{FactID: id, Scope: target, Revision: f.Revision, MutationVersion: f.MutationVersion, StoreSequence: s.seq, Action: MutationTombstone})
			for idemKey, record := range s.idempotency {
				if record.receipt.FactID == id {
					record.state = FactDeleted
					s.idempotency[idemKey] = record
				}
			}
		}
	}
	return op, nil
}
func (s *factStoreFixture) GetClearOperation(ctx context.Context, access MemoryAccess, id ClearOperationID) (ClearOperation, error) {
	if err := ctx.Err(); err != nil {
		return ClearOperation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	op, ok := s.clears[id]
	if !ok {
		return ClearOperation{}, &MemoryError{Kind: ErrMaintenanceNotFound}
	}
	if !scopeAllowed(access.ReadScopes, op.Scope) {
		return ClearOperation{}, &MemoryError{Kind: ErrAuthorization}
	}
	return op, nil
}
func (s *factStoreFixture) ClaimClearOperations(ctx context.Context, limit int) ([]ClearOperation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []ClearOperation{}
	for _, op := range s.clears {
		if op.State != "complete" {
			out = append(out, op)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
func (s *factStoreFixture) AckClearOperation(ctx context.Context, id ClearOperationID, progress ClearProgress) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	op, ok := s.clears[id]
	if !ok {
		return &MemoryError{Kind: ErrMaintenanceNotFound}
	}
	if progress.Done {
		op.State = "complete"
		delete(s.blockedScopes, op.Scope)
	} else {
		op.State = "running"
	}
	op.UpdatedAt = s.now
	s.clears[id] = op
	return nil
}
func (s *factStoreFixture) ListMaintenance(context.Context, MaintenanceAccess, MaintenanceFilter, PageRequest) (MaintenancePage, error) {
	return MaintenancePage{}, nil
}
func (s *factStoreFixture) RetryMutation(context.Context, MaintenanceAccess, MutationID) error {
	return nil
}
func (s *factStoreFixture) CreateRebuild(context.Context, MaintenancePrincipal, EmbeddingProfile) (Rebuild, error) {
	return Rebuild{}, nil
}
func (s *factStoreFixture) GetRebuild(context.Context, MaintenanceAccess, RebuildID) (Rebuild, error) {
	return Rebuild{}, nil
}
func (s *factStoreFixture) AdvanceRebuild(context.Context, RebuildID, RebuildProgress) error {
	return nil
}
func (s *factStoreFixture) ActiveIndexProfile(context.Context) (IndexProfileState, error) {
	return IndexProfileState{Profile: EmbeddingProfile{ID: "test-profile", Dimension: 2}, Generation: 1}, nil
}
func (s *factStoreFixture) SwitchActiveGeneration(context.Context, IndexGeneration, RebuildID, StoreSequence) error {
	return nil
}

type fixtureEmbedderResolver struct{}

func (fixtureEmbedderResolver) Resolve(context.Context, EmbeddingProfileID) (Embedder, error) {
	return fixtureEmbedder{}, nil
}

type fixtureEmbedder struct{}

func (fixtureEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{0.25, 0.75}, nil
}
func (fixtureEmbedder) Profile() EmbeddingProfile {
	return EmbeddingProfile{ID: "test-profile", Dimension: 2}
}

type fixtureIndex struct {
	mu      sync.Mutex
	applied []IndexMutation
}

func (i *fixtureIndex) ApplyMutation(_ context.Context, mutation IndexMutation) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.applied = append(i.applied, mutation)
	return nil
}
func (*fixtureIndex) Search(context.Context, IndexQuery) (IndexPage, error) { return IndexPage{}, nil }

func TestFactWorkerEmbedsAndAppliesPendingMutation(t *testing.T) {
	svc, store, scope := testMemoryService(t)
	if _, err := svc.Save(context.Background(), scope, "worker", FactInput{Content: "index me"}); err != nil {
		t.Fatal(err)
	}
	index := &fixtureIndex{}
	worker, err := NewFactWorker(FactWorkerConfig{Store: store, Index: index, Embedders: fixtureEmbedderResolver{}, BatchSize: 10, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(index.applied) != 1 || len(index.applied[0].Vector) != 2 || index.applied[0].Generation != 1 {
		t.Fatalf("索引 mutation 未正确处理：%+v", index.applied)
	}
}
