package memory

import (
	"context"
	"time"
)

// FactID 是持久事实的稳定标识。
type FactID string

// Revision 是事实的乐观并发版本。
type Revision uint64

// MutationVersion 在单个 FactID 内单调递增，用于拒绝乱序索引 mutation。
type MutationVersion uint64

// StoreSequence 是整个 MemoryService 的持久变更序号。
type StoreSequence uint64

// IndexGeneration 标识全局活动索引代际。
type IndexGeneration uint64

// IdempotencyKey 是调用方为一次写操作提供的稳定去重键。
type IdempotencyKey string

// EmbeddingProfileID 标识一个兼容的嵌入向量空间。
type EmbeddingProfileID string

// MutationID 标识一个持久索引任务。
type MutationID string

// ClearOperationID 标识一个作用域清理操作。
type ClearOperationID string

// RebuildID 标识一个全局索引重建操作。
type RebuildID string

// Principal 标识由宿主认证的当前调用主体。
type Principal struct {
	TenantID string
	UserID   string
	AgentID  string
}

// OwnerKind 指明事实作用域归属主体类型。
type OwnerKind string

const (
	OwnerAgent OwnerKind = "agent"
	OwnerUser  OwnerKind = "user"
)

// MemoryScope 是事实的数据归属范围。
type MemoryScope struct {
	TenantID  string
	OwnerKind OwnerKind
	OwnerID   string
	AgentID   string
}

// MemoryAccess 是普通事实操作的主体和授权作用域集合。
type MemoryAccess struct {
	Principal   Principal
	ReadScopes  []MemoryScope
	WriteScopes []MemoryScope
}

// WriteScope 是一次写操作的主体与明确目标作用域。
type WriteScope struct {
	Access MemoryAccess
	Target MemoryScope
}

// MaintenanceAccess 是宿主单独签发的管理操作权限。
type MaintenanceAccess struct {
	TenantID         string
	OperatorID       string
	AuthorizedScopes []MemoryScope
	Capabilities     []string
}

// ClearAccess 是对明确目标作用域执行清理的独立授权。
type ClearAccess struct {
	Principal    Principal
	TargetScopes []MemoryScope
	Capabilities []string
}

// RecallClass 控制事实的召回选择策略。
type RecallClass string

const (
	RecallNormal RecallClass = "normal"
	RecallAlways RecallClass = "always"
)

// FactStatus 表示事实的生命周期状态。
type FactStatus string

const (
	FactActive     FactStatus = "active"
	FactSuperseded FactStatus = "superseded"
	FactExpired    FactStatus = "expired"
	FactDeleted    FactStatus = "deleted"
)

// FactSource 保存事实的可追溯来源引用。
type FactSource struct {
	Kind        string
	SourceID    string
	SessionID   string
	DocumentID  string
	Position    string
	EventSeq    uint64
	ConfirmedBy string
	ConfirmedAt time.Time
}

// FactInput 是创建事实所需的不可变输入。
type FactInput struct {
	Content     string
	Metadata    map[string]any
	Source      FactSource
	ExpiresAt   time.Time
	RecallClass RecallClass
	Priority    int
}

// FactPatch 表示部分更新；nil 字段保持原值。
type FactPatch struct {
	Content     *string
	Metadata    map[string]any
	Source      *FactSource
	ExpiresAt   *time.Time
	RecallClass *RecallClass
	Priority    *int
}

// Fact 是持久事实的当前可见表示。
type Fact struct {
	ID              FactID
	Scope           MemoryScope
	Content         string
	Metadata        map[string]any
	Source          FactSource
	Status          FactStatus
	Revision        Revision
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ExpiresAt       time.Time
	SupersedesID    FactID
	RecallClass     RecallClass
	Priority        int
	IndexState      IndexState
	MutationVersion MutationVersion
}

// IndexState 表示事实当前索引同步状态。
type IndexState string

const (
	IndexPending IndexState = "pending"
	IndexReady   IndexState = "ready"
	IndexFailed  IndexState = "failed"
)

// MutationReceipt 是不包含事实正文的不可变操作收据。
type MutationReceipt struct {
	OperationID string
	FactID      FactID
	Revision    Revision
	IndexState  IndexState
}

// FactMutationResult 是操作收据与事实当前生命周期状态。
type FactMutationResult struct {
	Receipt   MutationReceipt
	Lifecycle FactStatus
}

// SaveResult 是不可变写入操作收据。
type SaveResult = MutationReceipt

// DeleteResult 是逻辑删除收据。
type DeleteResult struct {
	FactID  FactID
	Deleted bool
}

// PageRequest 请求一页稳定排序的数据。
type PageRequest struct {
	Limit  int
	Cursor string
}

// FactFilter 筛选事实列表。
type FactFilter struct {
	Scopes        []MemoryScope
	Statuses      []FactStatus
	Tags          []string
	SourceIDs     []string
	UpdatedAfter  time.Time
	UpdatedBefore time.Time
}

// FactPage 是事实分页结果。
type FactPage struct {
	Items      []Fact
	NextCursor string
}

// ChangeAction 是事实变更事件类型。
type ChangeAction string

const (
	ChangeCreated    ChangeAction = "created"
	ChangeUpdated    ChangeAction = "updated"
	ChangeSuperseded ChangeAction = "superseded"
	ChangeDeleted    ChangeAction = "deleted"
)

// ChangeRecord 保存不含正文的事实变更审计。
type ChangeRecord struct {
	FactID       FactID
	Scope        MemoryScope
	Revision     Revision
	Action       ChangeAction
	Actor        Principal
	At           time.Time
	Source       FactSource
	SupersedesID FactID
}

// ChangePage 是事实变更记录分页结果。
type ChangePage struct {
	Items      []ChangeRecord
	NextCursor string
}

// RecallOptions 限制召回数量、预算和相关度。
type RecallOptions struct {
	Limit           int
	MinScore        float64
	MaxBytes        int
	MaxTokens       int
	AllowedStatuses []FactStatus
	Tags            []string
}

// MatchKind 描述事实的召回原因。
type MatchKind string

const (
	MatchSemantic MatchKind = "semantic"
	MatchAlways   MatchKind = "always"
	MatchKeyword  MatchKind = "keyword"
)

// FactMatch 是一条候选召回事实及归一化分数。
type FactMatch struct {
	Fact      Fact
	Score     float64 // [0,1]，越大越相关
	MatchKind MatchKind
}

// SnapshotFactRef 保存 durable Run 所需的事实引用，不包含正文。
type SnapshotFactRef struct {
	FactID          FactID
	Revision        Revision
	MutationVersion MutationVersion
	Score           float64
}

// SnapshotRef 固定一次逻辑调用使用的事实集合。
type SnapshotRef struct {
	Facts              []SnapshotFactRef
	ScopeFingerprint   string
	PolicyVersion      string
	EmbeddingProfileID EmbeddingProfileID
	IndexGeneration    IndexGeneration
}

// RecallResult 是经过授权、状态和预算校验后的召回结果。
type RecallResult struct {
	Matches  []FactMatch
	Snapshot SnapshotRef
}

// EmbeddingProfile 描述向量空间配置。
type EmbeddingProfile struct {
	ID             EmbeddingProfileID
	ModelProvider  string
	ModelName      string
	ModelVersion   string
	Dimension      int
	DistanceMetric string
}

// IndexMutationAction 描述索引更新操作。
type IndexMutationAction string

const (
	MutationUpsert    IndexMutationAction = "upsert"
	MutationTombstone IndexMutationAction = "tombstone"
)

// IndexMutation 是事实存储事务写入的持久索引任务。
type IndexMutation struct {
	ID              MutationID
	FactID          FactID
	Scope           MemoryScope
	Revision        Revision
	MutationVersion MutationVersion
	StoreSequence   StoreSequence
	ProfileID       EmbeddingProfileID
	Generation      IndexGeneration
	Action          IndexMutationAction
}

// IndexMutationResult 是索引任务的终态结果。
type IndexMutationResult string

const (
	MutationApplied  IndexMutationResult = "applied"
	MutationObsolete IndexMutationResult = "obsolete"
	MutationFailed   IndexMutationResult = "failed"
)

// IndexCandidate 是索引返回的事实 ID 与归一化相关度。
type IndexCandidate struct {
	FactID     FactID
	Revision   Revision
	ProfileID  EmbeddingProfileID
	Generation IndexGeneration
	Score      float64
}

// IndexPage 是索引候选分页结果。
type IndexPage struct {
	Items      []IndexCandidate
	NextCursor string
}

// IndexQuery 描述一次严格作用域过滤的索引查询。
type IndexQuery struct {
	Scopes     []MemoryScope
	ProfileID  EmbeddingProfileID
	Generation IndexGeneration
	Vector     []float32
	Limit      int
	Cursor     string
}

// IndexProfileState 是当前活动索引代际和嵌入 Profile。
type IndexProfileState struct {
	Profile    EmbeddingProfile
	Generation IndexGeneration
}

// MaintenancePrincipal 是启动重建时记录的操作主体。
type MaintenancePrincipal struct {
	TenantID   string
	OperatorID string
}

// MaintenanceFilter 筛选维护任务。
type MaintenanceFilter struct {
	Scopes []MemoryScope
	States []string
}

// MaintenanceItem 是可查询的索引/重建任务摘要。
type MaintenanceItem struct {
	MutationID MutationID
	Scope      MemoryScope
	State      string
	Attempts   int
	Diagnostic string
	NextRetry  time.Time
}

// MaintenancePage 是维护任务分页结果。
type MaintenancePage struct {
	Items      []MaintenanceItem
	NextCursor string
}

// ClearOperation 是作用域清空任务状态。
type ClearOperation struct {
	ID        ClearOperationID
	Scope     MemoryScope
	State     string
	Cutoff    StoreSequence
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ClearProgress 保存清空任务的持久推进位置。
type ClearProgress struct {
	Cursor string
	Done   bool
}

// Rebuild 描述全局索引代际重建状态。
type Rebuild struct {
	ID             RebuildID
	Profile        EmbeddingProfile
	Generation     IndexGeneration
	Cutoff         StoreSequence
	CaughtUp       StoreSequence
	State          string
	DiagnosticCode string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// RebuildProgress 是重建 Worker 的持久进度。
type RebuildProgress struct {
	Cursor   string
	CaughtUp StoreSequence
	Done     bool
}

// MemoryService 是事实记忆的稳定高层消费接口。
type MemoryService interface {
	Save(context.Context, WriteScope, IdempotencyKey, FactInput) (SaveResult, error)
	Get(context.Context, MemoryAccess, FactID) (Fact, error)
	List(context.Context, MemoryAccess, FactFilter, PageRequest) (FactPage, error)
	Update(context.Context, WriteScope, FactID, Revision, FactPatch) (Fact, error)
	Supersede(context.Context, WriteScope, FactID, Revision, IdempotencyKey, FactInput) (SaveResult, error)
	Delete(context.Context, WriteScope, FactID, Revision) (DeleteResult, error)
	DeleteScope(context.Context, ClearAccess, MemoryScope, IdempotencyKey) (ClearOperation, error)
	Recall(context.Context, MemoryAccess, string, RecallOptions) (RecallResult, error)
	ResolveSnapshot(context.Context, MemoryAccess, SnapshotRef) (RecallResult, error)
	ListChanges(context.Context, MemoryAccess, FactID, PageRequest) (ChangePage, error)
}

// MemoryScopeResolver 从可信宿主上下文解析当前主体授权。
type MemoryScopeResolver interface {
	Resolve(context.Context, string) (MemoryAccess, error)
}

// FactStore 是事实正文、审计、幂等记录和维护状态的权威持久存储。
type FactStore interface {
	ApplyFact(context.Context, WriteScope, FactMutation) (FactMutationResult, error)
	Get(context.Context, MemoryAccess, FactID, Revision) (Fact, error)
	List(context.Context, MemoryAccess, FactFilter, PageRequest) (FactPage, error)
	ListChanges(context.Context, MemoryAccess, FactID, PageRequest) (ChangePage, error)
	ClaimIndexMutations(context.Context, int) ([]IndexMutation, error)
	AckIndexMutation(context.Context, IndexMutation, IndexMutationResult) error
	BeginScopeClear(context.Context, ClearAccess, MemoryScope, IdempotencyKey) (ClearOperation, error)
	GetClearOperation(context.Context, MemoryAccess, ClearOperationID) (ClearOperation, error)
	ClaimClearOperations(context.Context, int) ([]ClearOperation, error)
	AckClearOperation(context.Context, ClearOperationID, ClearProgress) error
	ListMaintenance(context.Context, MaintenanceAccess, MaintenanceFilter, PageRequest) (MaintenancePage, error)
	RetryMutation(context.Context, MaintenanceAccess, MutationID) error
	CreateRebuild(context.Context, MaintenancePrincipal, EmbeddingProfile) (Rebuild, error)
	GetRebuild(context.Context, MaintenanceAccess, RebuildID) (Rebuild, error)
	AdvanceRebuild(context.Context, RebuildID, RebuildProgress) error
	ActiveIndexProfile(context.Context) (IndexProfileState, error)
	SwitchActiveGeneration(context.Context, IndexGeneration, RebuildID, StoreSequence) error
}

// FactMutation 是一个可事务提交的事实变更命令。
type FactMutation struct {
	FactID           FactID
	NewFactID        FactID
	ExpectedRevision Revision
	Input            FactInput
	Patch            *FactPatch
	Actor            Principal
	IdempotencyKey   IdempotencyKey
	RequestDigest    string
	Action           ChangeAction
}

// MemoryIndex 保存候选向量索引，不是事实正文的权威来源。
type MemoryIndex interface {
	ApplyMutation(context.Context, IndexMutation) error
	Search(context.Context, IndexQuery) (IndexPage, error)
}

// Embedder 根据 Profile 生成向量。
type Embedder interface {
	Embed(context.Context, string) ([]float32, error)
	Profile() EmbeddingProfile
}

// EmbedderResolver 按 Profile 获取 Embedder，支持在线重建期间多 Profile 并存。
type EmbedderResolver interface {
	Resolve(context.Context, EmbeddingProfileID) (Embedder, error)
}

// MemoryMaintenance 暴露 Worker 与授权维护操作。
type MemoryMaintenance interface {
	Run(context.Context) error
	Status(context.Context, MaintenanceAccess, MaintenanceFilter, PageRequest) (MaintenancePage, error)
	Retry(context.Context, MaintenanceAccess, MutationID) error
	ClearStatus(context.Context, MaintenanceAccess, ClearOperationID) (ClearOperation, error)
	StartRebuild(context.Context, MaintenanceAccess, EmbeddingProfile) (RebuildID, error)
	RebuildStatus(context.Context, MaintenanceAccess, RebuildID) (Rebuild, error)
}
