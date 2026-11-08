package memory

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

// MemoryServiceConfig 配置默认事实记忆服务。
type MemoryServiceConfig struct {
	Store        FactStore
	Capabilities ProviderCapabilities
	Limits       FactLimits
	Now          func() time.Time
	NewID        func() (FactID, error)
	Index        MemoryIndex
	Embedders    EmbedderResolver
	Observer     MemoryObserver
}

// DefaultMemoryService 使用宿主提供的持久 Store 实现事实生命周期与权限校验。
type DefaultMemoryService struct {
	store     FactStore
	limits    FactLimits
	now       func() time.Time
	newID     func() (FactID, error)
	index     MemoryIndex
	embedders EmbedderResolver
	observer  MemoryObserver
}

var _ MemoryService = (*DefaultMemoryService)(nil)

// NewDefaultMemoryService 校验 Provider 能力后创建默认组合服务。
func NewDefaultMemoryService(config MemoryServiceConfig) (*DefaultMemoryService, error) {
	if config.Store == nil {
		return nil, &MemoryError{Kind: ErrProviderCapability, Op: "构造事实记忆服务"}
	}
	if config.Limits == (FactLimits{}) {
		config.Limits = DefaultFactLimits()
	}
	if err := ValidateFactLimits(config.Limits, config.Capabilities); err != nil {
		return nil, err
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.NewID == nil {
		config.NewID = randomFactID
	}
	return &DefaultMemoryService{
		store: config.Store, limits: config.Limits, now: config.Now,
		newID: config.NewID, index: config.Index, embedders: config.Embedders, observer: config.Observer,
	}, nil
}

func randomFactID() (FactID, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return FactID(hex.EncodeToString(id[:])), nil
}

// Save 创建事实并返回不可变操作收据。
func (s *DefaultMemoryService) Save(ctx context.Context, scope WriteScope, key IdempotencyKey, input FactInput) (result SaveResult, retErr error) {
	started := time.Now()
	defer func() {
		count := 0
		if retErr == nil {
			count = 1
		}
		s.observe("save", started, count, retErr)
	}()
	if err := s.validateWriteScope(scope); err != nil {
		return SaveResult{}, err
	}
	if strings.TrimSpace(string(key)) == "" {
		return SaveResult{}, &MemoryError{Kind: ErrIdempotencyConflict, Op: "保存事实"}
	}
	if err := s.validateInput(input); err != nil {
		return SaveResult{}, err
	}
	id, err := s.newID()
	if err != nil {
		return SaveResult{}, &MemoryError{Kind: ErrProviderCapability, Op: "生成事实标识", Cause: err}
	}
	digest, err := requestDigest("save", scope.Target, FactID(""), 0, input)
	if err != nil {
		return SaveResult{}, err
	}
	mutationResult, err := s.store.ApplyFact(ctx, scope, FactMutation{
		FactID: id, NewFactID: id, Input: input, Actor: scope.Access.Principal,
		IdempotencyKey: key, RequestDigest: digest, Action: ChangeCreated,
		IdempotencyExpiresAt: s.now().Add(s.limits.IdempotencyRetention),
	})
	if err != nil {
		return mutationResult.Receipt, err
	}
	if mutationResult.Lifecycle == FactDeleted {
		return mutationResult.Receipt, lifecycleError(mutationResult.Lifecycle)
	}
	if mutationResult.Lifecycle != FactActive && mutationResult.Lifecycle != FactExpired && mutationResult.Lifecycle != FactSuperseded {
		return mutationResult.Receipt, lifecycleError(mutationResult.Lifecycle)
	}
	return mutationResult.Receipt, nil
}

// Get 读取当前可见的有效事实。
func (s *DefaultMemoryService) Get(ctx context.Context, access MemoryAccess, id FactID) (fact Fact, retErr error) {
	started := time.Now()
	defer func() {
		count := 0
		if retErr == nil {
			count = 1
		}
		s.observe("get", started, count, retErr)
	}()
	if err := validateReadAccess(access); err != nil {
		return Fact{}, err
	}
	fact, err := s.store.Get(ctx, access, id, 0)
	if err != nil {
		return Fact{}, err
	}
	if !scopeAllowed(access.ReadScopes, fact.Scope) {
		return Fact{}, &MemoryError{Kind: ErrAuthorization, Op: "读取事实"}
	}
	if err := lifecycleError(fact.Status); err != nil {
		return Fact{}, err
	}
	if !fact.ExpiresAt.IsZero() && !s.now().Before(fact.ExpiresAt) {
		return Fact{}, &MemoryError{Kind: ErrGone, Op: "读取事实"}
	}
	return fact, nil
}

// List 返回授权作用域中的有效事实分页。
func (s *DefaultMemoryService) List(ctx context.Context, access MemoryAccess, filter FactFilter, page PageRequest) (result FactPage, retErr error) {
	started := time.Now()
	defer func() { s.observe("list", started, len(result.Items), retErr) }()
	if err := validateReadAccess(access); err != nil {
		return FactPage{}, err
	}
	if err := s.validatePage(page); err != nil {
		return FactPage{}, err
	}
	if len(filter.Scopes) == 0 {
		filter.Scopes = append([]MemoryScope(nil), access.ReadScopes...)
	}
	for _, scope := range filter.Scopes {
		if !scopeAllowed(access.ReadScopes, scope) {
			return FactPage{}, &MemoryError{Kind: ErrAuthorization, Op: "列出事实"}
		}
	}
	result, err := s.store.List(ctx, access, filter, page)
	if err != nil {
		return FactPage{}, err
	}
	items := result.Items[:0]
	for _, fact := range result.Items {
		if !scopeAllowed(access.ReadScopes, fact.Scope) {
			return FactPage{}, &MemoryError{Kind: ErrAuthorization, Op: "列出事实"}
		}
		if fact.Status != FactActive || (!fact.ExpiresAt.IsZero() && !s.now().Before(fact.ExpiresAt)) {
			continue
		}
		items = append(items, fact)
	}
	result.Items = items
	return result, nil
}

// Update 按 expected revision 原位修改事实。
func (s *DefaultMemoryService) Update(ctx context.Context, scope WriteScope, id FactID, revision Revision, patch FactPatch) (fact Fact, retErr error) {
	started := time.Now()
	defer func() {
		count := 0
		if retErr == nil {
			count = 1
		}
		s.observe("update", started, count, retErr)
	}()
	if err := s.validateWriteScope(scope); err != nil {
		return Fact{}, err
	}
	if id == "" || revision == 0 {
		return Fact{}, &MemoryError{Kind: ErrRevisionConflict, Op: "更新事实"}
	}
	if err := s.validatePatch(patch); err != nil {
		return Fact{}, err
	}
	digest, err := requestDigest("update", scope.Target, id, revision, patch)
	if err != nil {
		return Fact{}, err
	}
	mutationResult, err := s.store.ApplyFact(ctx, scope, FactMutation{
		FactID: id, ExpectedRevision: revision, Patch: &patch,
		Actor: scope.Access.Principal, RequestDigest: digest, Action: ChangeUpdated,
	})
	if err != nil {
		return Fact{}, err
	}
	if err := lifecycleError(mutationResult.Lifecycle); err != nil {
		return Fact{}, err
	}
	return s.Get(ctx, scope.Access, id)
}

// Supersede 创建新事实并使被替代事实退出默认读取。
func (s *DefaultMemoryService) Supersede(ctx context.Context, scope WriteScope, oldID FactID, revision Revision, key IdempotencyKey, input FactInput) (result SaveResult, retErr error) {
	started := time.Now()
	defer func() {
		count := 0
		if retErr == nil {
			count = 1
		}
		s.observe("supersede", started, count, retErr)
	}()
	if err := s.validateWriteScope(scope); err != nil {
		return SaveResult{}, err
	}
	if oldID == "" || revision == 0 || strings.TrimSpace(string(key)) == "" {
		return SaveResult{}, &MemoryError{Kind: ErrIdempotencyConflict, Op: "替代事实"}
	}
	if err := s.validateInput(input); err != nil {
		return SaveResult{}, err
	}
	id, err := s.newID()
	if err != nil {
		return SaveResult{}, &MemoryError{Kind: ErrProviderCapability, Op: "生成事实标识", Cause: err}
	}
	digest, err := requestDigest("supersede", scope.Target, oldID, revision, input)
	if err != nil {
		return SaveResult{}, err
	}
	mutationResult, err := s.store.ApplyFact(ctx, scope, FactMutation{
		FactID: oldID, NewFactID: id, ExpectedRevision: revision, Input: input,
		Actor: scope.Access.Principal, IdempotencyKey: key, RequestDigest: digest,
		IdempotencyExpiresAt: s.now().Add(s.limits.IdempotencyRetention),
		Action:               ChangeSuperseded,
	})
	if err != nil {
		return mutationResult.Receipt, err
	}
	if mutationResult.Lifecycle == FactDeleted {
		return mutationResult.Receipt, lifecycleError(mutationResult.Lifecycle)
	}
	if mutationResult.Lifecycle != FactActive && mutationResult.Lifecycle != FactExpired && mutationResult.Lifecycle != FactSuperseded {
		return mutationResult.Receipt, lifecycleError(mutationResult.Lifecycle)
	}
	return mutationResult.Receipt, nil
}

// Delete 逻辑删除事实；持久索引清理由 outbox 异步完成。
func (s *DefaultMemoryService) Delete(ctx context.Context, scope WriteScope, id FactID, revision Revision) (result DeleteResult, retErr error) {
	started := time.Now()
	defer func() {
		count := 0
		if retErr == nil && result.Deleted {
			count = 1
		}
		s.observe("delete", started, count, retErr)
	}()
	if err := s.validateWriteScope(scope); err != nil {
		return DeleteResult{}, err
	}
	if id == "" || revision == 0 {
		return DeleteResult{}, &MemoryError{Kind: ErrRevisionConflict, Op: "删除事实"}
	}
	mutationResult, err := s.store.ApplyFact(ctx, scope, FactMutation{
		FactID: id, ExpectedRevision: revision, Actor: scope.Access.Principal,
		Action: ChangeDeleted,
	})
	if err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{FactID: mutationResult.Receipt.FactID, Deleted: mutationResult.Lifecycle == FactDeleted}, nil
}

// DeleteScope 发起受独立 ClearAccess 授权的作用域清理。
func (s *DefaultMemoryService) DeleteScope(ctx context.Context, access ClearAccess, target MemoryScope, key IdempotencyKey) (operation ClearOperation, retErr error) {
	started := time.Now()
	defer func() {
		count := 0
		if retErr == nil {
			count = 1
		}
		s.observe("delete_scope", started, count, retErr)
	}()
	if !clearAllowed(access, target) {
		return ClearOperation{}, &MemoryError{Kind: ErrAuthorization, Op: "清空作用域"}
	}
	if strings.TrimSpace(string(key)) == "" {
		return ClearOperation{}, &MemoryError{Kind: ErrIdempotencyConflict, Op: "清空作用域"}
	}
	return s.store.BeginScopeClear(ctx, access, target, key)
}

// Recall 在配置索引与嵌入解析器前返回明确能力错误。
func (s *DefaultMemoryService) Recall(ctx context.Context, access MemoryAccess, query string, options RecallOptions) (result RecallResult, retErr error) {
	started := time.Now()
	defer func() { s.observe("recall", started, len(result.Matches), retErr) }()
	if s.index == nil || s.embedders == nil {
		return RecallResult{}, &MemoryError{Kind: ErrProviderCapability, Op: "召回事实：未配置索引或嵌入器"}
	}
	return s.recall(ctx, access, query, options)
}

// ResolveSnapshot 在快照恢复能力接入前返回明确错误。
func (s *DefaultMemoryService) ResolveSnapshot(ctx context.Context, access MemoryAccess, snapshot SnapshotRef) (result RecallResult, retErr error) {
	started := time.Now()
	defer func() { s.observe("resolve_snapshot", started, len(result.Matches), retErr) }()
	return s.resolveSnapshot(ctx, access, snapshot)
}

// ListChanges 查询不含正文的变更记录。
func (s *DefaultMemoryService) ListChanges(ctx context.Context, access MemoryAccess, id FactID, page PageRequest) (result ChangePage, retErr error) {
	started := time.Now()
	defer func() { s.observe("list_changes", started, len(result.Items), retErr) }()
	if err := validateReadAccess(access); err != nil {
		return ChangePage{}, err
	}
	if err := s.validatePage(page); err != nil {
		return ChangePage{}, err
	}
	return s.store.ListChanges(ctx, access, id, page)
}

func (s *DefaultMemoryService) observe(operation string, started time.Time, count int, err error) {
	if s.observer != nil {
		observeMemory(s.observer, MemoryEvent{Operation: operation, Duration: time.Since(started), Count: count, ErrorCode: memoryErrorCode(err)})
	}
}

func (s *DefaultMemoryService) validateInput(input FactInput) error {
	if len([]byte(input.Content)) == 0 || len([]byte(input.Content)) > s.limits.MaxFactBytes {
		return &MemoryError{Kind: ErrCapacityExceeded, Op: "事实正文长度超限"}
	}
	metadata, err := json.Marshal(input.Metadata)
	if err != nil {
		return &MemoryError{Kind: ErrInvalidFact, Op: "编码事实元数据", Cause: err}
	}
	if len(metadata) > s.limits.MaxMetadataBytes {
		return &MemoryError{Kind: ErrCapacityExceeded, Op: "事实元数据长度超限"}
	}
	if input.RecallClass != "" && input.RecallClass != RecallNormal && input.RecallClass != RecallAlways {
		return &MemoryError{Kind: ErrInvalidFact, Op: "事实召回类型无效"}
	}
	return nil
}

func (s *DefaultMemoryService) validatePatch(patch FactPatch) error {
	if patch.Content != nil && (len([]byte(*patch.Content)) == 0 || len([]byte(*patch.Content)) > s.limits.MaxFactBytes) {
		return &MemoryError{Kind: ErrCapacityExceeded, Op: "事实正文长度超限"}
	}
	if patch.Metadata != nil {
		metadata, err := json.Marshal(patch.Metadata)
		if err != nil {
			return &MemoryError{Kind: ErrInvalidFact, Op: "编码事实元数据", Cause: err}
		}
		if len(metadata) > s.limits.MaxMetadataBytes {
			return &MemoryError{Kind: ErrCapacityExceeded, Op: "事实元数据长度超限"}
		}
	}
	if patch.RecallClass != nil && *patch.RecallClass != RecallNormal && *patch.RecallClass != RecallAlways {
		return &MemoryError{Kind: ErrInvalidFact, Op: "事实召回类型无效"}
	}
	return nil
}

func (s *DefaultMemoryService) validatePage(page PageRequest) error {
	if page.Limit < 1 || page.Limit > s.limits.MaxPageSize {
		return &MemoryError{Kind: ErrInvalidCursor, Op: "分页大小超出范围"}
	}
	return nil
}

func (s *DefaultMemoryService) validateWriteScope(scope WriteScope) error {
	if scope.Access.Principal.TenantID == "" || scope.Access.Principal.UserID == "" || scope.Access.Principal.AgentID == "" ||
		scope.Target.TenantID != scope.Access.Principal.TenantID || !scopeMatchesPrincipal(scope.Access.Principal, scope.Target) || !scopeAllowed(scope.Access.WriteScopes, scope.Target) {
		return &MemoryError{Kind: ErrAuthorization, Op: "写入事实"}
	}
	return nil
}

func lifecycleError(status FactStatus) error {
	switch status {
	case FactActive:
		return nil
	case FactDeleted, FactExpired, FactSuperseded:
		return &MemoryError{Kind: ErrGone, Op: "事实生命周期已结束"}
	default:
		return &MemoryError{Kind: ErrGone, Op: "事实不存在"}
	}
}

func requestDigest(operation string, scope MemoryScope, id FactID, revision Revision, payload any) (string, error) {
	data, err := json.Marshal(struct {
		Operation string
		Scope     MemoryScope
		FactID    FactID
		Revision  Revision
		Payload   any
	}{operation, scope, id, revision, payload})
	if err != nil {
		return "", &MemoryError{Kind: ErrInvalidFact, Op: "计算事实请求摘要", Cause: err}
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func validateReadAccess(access MemoryAccess) error {
	if access.Principal.TenantID == "" || access.Principal.UserID == "" || access.Principal.AgentID == "" {
		return &MemoryError{Kind: ErrAuthorization, Op: "读取事实"}
	}
	for _, scope := range access.ReadScopes {
		if scope.TenantID != access.Principal.TenantID || scope.OwnerID == "" {
			return &MemoryError{Kind: ErrAuthorization, Op: "读取事实"}
		}
		switch scope.OwnerKind {
		case OwnerAgent:
			if scope.OwnerID != access.Principal.UserID || scope.AgentID != access.Principal.AgentID {
				return &MemoryError{Kind: ErrAuthorization, Op: "读取事实"}
			}
		case OwnerUser:
			if scope.OwnerID != access.Principal.UserID || (scope.AgentID != "" && scope.AgentID != access.Principal.AgentID) {
				return &MemoryError{Kind: ErrAuthorization, Op: "读取事实"}
			}
		default:
			return &MemoryError{Kind: ErrAuthorization, Op: "读取事实"}
		}
	}
	return nil
}

func scopeMatchesPrincipal(principal Principal, scope MemoryScope) bool {
	if scope.TenantID != principal.TenantID || scope.OwnerID == "" {
		return false
	}
	switch scope.OwnerKind {
	case OwnerAgent:
		return scope.OwnerID == principal.UserID && scope.AgentID == principal.AgentID
	case OwnerUser:
		return scope.OwnerID == principal.UserID && (scope.AgentID == "" || scope.AgentID == principal.AgentID)
	default:
		return false
	}
}

func scopeAllowed(scopes []MemoryScope, target MemoryScope) bool {
	for _, scope := range scopes {
		if scope == target {
			return true
		}
	}
	return false
}

func clearAllowed(access ClearAccess, target MemoryScope) bool {
	if access.Principal.TenantID == "" || access.Principal.TenantID != target.TenantID || target.OwnerID == "" || len(access.Capabilities) == 0 {
		return false
	}
	for _, capability := range access.Capabilities {
		if capability == "memory.clear" && scopeAllowed(access.TargetScopes, target) {
			return true
		}
	}
	return false
}
