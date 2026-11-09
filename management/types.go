// Package management 为业务宿主提供持久的 Agent、会话、任务与审批管理。
package management

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/execution"
	"github.com/kart-io/goagent/revision"
)

type TaskState string

const (
	Queued          TaskState = "queued"
	Running         TaskState = "running"
	WaitingApproval TaskState = "waiting_approval"
	WaitingInput    TaskState = "waiting_input"
	Canceling       TaskState = "canceling"
	Succeeded       TaskState = "succeeded"
	Failed          TaskState = "failed"
	Canceled        TaskState = "canceled"
	Attention       TaskState = "attention"
)

func (s TaskState) Terminal() bool { return s == Succeeded || s == Failed || s == Canceled }
func CanTransition(from, to TaskState) bool {
	if from.Terminal() {
		return false
	}
	switch from {
	case Queued:
		return to == Running || to == Canceled || to == Attention
	case Running:
		return to == WaitingApproval || to == WaitingInput || to == Canceling || to == Succeeded || to == Failed || to == Canceled || to == Attention || to == Queued
	case WaitingApproval, WaitingInput:
		return to == Queued || to == Canceled || to == Attention
	case Canceling:
		return to == Canceled || to == Succeeded || to == Failed || to == Attention || to == Queued
	case Attention:
		return to == Queued || to == Canceled
	}
	return false
}

type Actor struct {
	TenantID string `json:"tenant_id"`
	Subject  string `json:"subject"`
}

func ValidateActor(a Actor) error {
	if a.TenantID == "" || a.Subject == "" {
		return agentErrors.ErrManagementUnauthenticated
	}
	return nil
}

type Meta struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Revision  int64     `json:"revision"`
	CreatedAt time.Time `json:"created_at"`
}
type Agent struct {
	Meta
	Name             string `json:"name"`
	Enabled          bool   `json:"enabled"`
	PublishedVersion string `json:"published_version,omitempty"`
}
type Version struct {
	Meta
	RevisionID            string          `json:"revision_id,omitempty"`
	AgentID               string          `json:"agent_id"`
	Version               string          `json:"version"`
	RunnerKind            string          `json:"runner_kind"`
	ImplementationVersion string          `json:"implementation_version"`
	Config                json.RawMessage `json:"config"`
	ConfigHash            string          `json:"config_hash"`
	CreatedBy             string          `json:"created_by"`
	Capabilities          Capabilities    `json:"capabilities"`
}
type Session struct {
	Meta
	RevisionID   string `json:"revision_id,omitempty"`
	AgentID      string `json:"agent_id"`
	Version      string `json:"version"`
	LogID        string `json:"log_id"`
	Status       string `json:"status"`
	ParentID     string `json:"parent_id,omitempty"`
	ForkSeq      int64  `json:"fork_seq,omitempty"`
	SourceHash   string `json:"source_hash,omitempty"`
	ActiveTaskID string `json:"active_task_id,omitempty"`
}
type Task struct {
	Operations []durable.OperationRef `json:"operations,omitempty"`
	Meta
	RevisionID      string          `json:"revision_id,omitempty"`
	SessionID       string          `json:"session_id"`
	AgentID         string          `json:"agent_id"`
	Version         string          `json:"version"`
	RunID           string          `json:"run_id"`
	ExecutionKey    string          `json:"execution_key"`
	Input           json.RawMessage `json:"input"`
	InputHash       string          `json:"input_hash"`
	State           TaskState       `json:"state"`
	WaitEpoch       int64           `json:"wait_epoch"`
	Waits           []WaitPoint     `json:"waits,omitempty"`
	CancelRequested bool            `json:"cancel_requested,omitempty"`
	RecoveryNeeded  bool            `json:"recovery_needed,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
	ResultRef       string          `json:"result_ref,omitempty"`
	ErrorKind       string          `json:"error_kind,omitempty"`
	FinishedAt      *time.Time      `json:"finished_at,omitempty"`
	Budget          budget.Config   `json:"budget"`
	Actor           Actor           `json:"actor"`
}
type Attempt struct {
	Meta
	TaskID     string     `json:"task_id"`
	Number     int        `json:"number"`
	WorkerID   string     `json:"worker_id"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Outcome    string     `json:"outcome,omitempty"`
	TraceID    string     `json:"trace_id,omitempty"`
}
type WaitPoint struct {
	Rejected    bool            `json:"rejected,omitempty"`
	Reason      string          `json:"reason,omitempty"`
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Key         string          `json:"key"`
	InterruptID string          `json:"interrupt_id,omitempty"`
	IntentHash  string          `json:"intent_hash"`
	Info        json.RawMessage `json:"info"`
	Epoch       int64           `json:"epoch"`
}
type Approval struct {
	CommandID    string `json:"command_id,omitempty"`
	CommandState string `json:"command_state,omitempty"`
	Meta
	TaskID    string     `json:"task_id"`
	AgentID   string     `json:"agent_id"`
	SessionID string     `json:"session_id"`
	Wait      WaitPoint  `json:"wait"`
	State     string     `json:"state"`
	Decision  string     `json:"decision,omitempty"`
	Actor     Actor      `json:"actor"`
	Reason    string     `json:"reason,omitempty"`
	DecidedAt *time.Time `json:"decided_at,omitempty"`
}
type Command struct {
	ReconciliationRef  string `json:"reconciliation_ref,omitempty"`
	ReconciliationHash string `json:"reconciliation_hash,omitempty"`
	Meta
	TaskID      string          `json:"task_id,omitempty"`
	SessionID   string          `json:"session_id,omitempty"`
	Kind        string          `json:"kind"`
	State       string          `json:"state"`
	Actor       Actor           `json:"actor"`
	WaitID      string          `json:"wait_id,omitempty"`
	ApprovalID  string          `json:"approval_id,omitempty"`
	Input       json.RawMessage `json:"input,omitempty"`
	ErrorKind   string          `json:"error_kind,omitempty"`
	AvailableAt time.Time       `json:"available_at"`
}
type Audit struct {
	Meta
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Operation    string `json:"operation"`
	Actor        Actor  `json:"actor"`
	Reason       string `json:"reason,omitempty"`
	CommandID    string `json:"command_id,omitempty"`
}
type Delivery struct {
	Meta
	Kind        string          `json:"kind"`
	ResourceID  string          `json:"resource_id"`
	Payload     json.RawMessage `json:"payload"`
	State       string          `json:"state"`
	Attempts    int             `json:"attempts"`
	AvailableAt time.Time       `json:"available_at"`
}
type WorkerOwner struct {
	Meta
	OwnerID    string    `json:"owner_id"`
	Generation int64     `json:"generation"`
	LastSeen   time.Time `json:"last_seen"`
}
type Idempotency struct {
	Meta
	Operation string          `json:"operation"`
	Key       string          `json:"key"`
	Hash      string          `json:"hash"`
	Response  json.RawMessage `json:"response"`
	ExpiresAt time.Time       `json:"expires_at"`
}

type RegisterAgentRequest struct {
	IdempotencyKey string `json:"-"`
	Name           string `json:"name"`
}
type SaveVersionRequest struct {
	RevisionID            string          `json:"revision_id,omitempty"`
	IdempotencyKey        string          `json:"-"`
	AgentID               string          `json:"-"`
	Version               string          `json:"version"`
	RunnerKind            string          `json:"runner_kind"`
	ImplementationVersion string          `json:"implementation_version"`
	Config                json.RawMessage `json:"config"`
}
type PublishRequest struct {
	IdempotencyKey   string `json:"-"`
	AgentID          string `json:"-"`
	Version          string `json:"version"`
	ExpectedRevision int64  `json:"expected_revision"`
}
type SetEnabledRequest struct {
	IdempotencyKey   string `json:"-"`
	AgentID          string `json:"-"`
	Enabled          bool   `json:"enabled"`
	ExpectedRevision int64  `json:"expected_revision"`
}
type CreateSessionRequest struct {
	IdempotencyKey string `json:"-"`
	AgentID        string `json:"agent_id"`
	Version        string `json:"version,omitempty"`
}
type ForkSessionRequest struct {
	IdempotencyKey   string `json:"-"`
	SessionID        string `json:"-"`
	UpTo             int64  `json:"up_to"`
	ExpectedRevision int64  `json:"expected_revision"`
}
type SubmitTaskRequest struct {
	IdempotencyKey string          `json:"-"`
	SessionID      string          `json:"session_id"`
	Input          json.RawMessage `json:"input"`
}
type CancelTaskRequest struct {
	IdempotencyKey   string `json:"-"`
	TaskID           string `json:"-"`
	ExpectedRevision int64  `json:"expected_revision"`
	Reason           string `json:"reason"`
}
type ResumeTaskRequest struct {
	IdempotencyKey    string          `json:"-"`
	TaskID            string          `json:"-"`
	ExpectedRevision  int64           `json:"expected_revision"`
	WaitID            string          `json:"wait_id,omitempty"`
	Input             json.RawMessage `json:"input,omitempty"`
	ReconciliationRef string          `json:"reconciliation_ref,omitempty"`
}
type DecisionRequest struct {
	IdempotencyKey   string `json:"-"`
	ApprovalID       string `json:"-"`
	ExpectedRevision int64  `json:"expected_revision"`
	Decision         string `json:"decision"`
	Reason           string `json:"reason"`
}
type HistoryRequest struct {
	SessionID string
	AfterSeq  int64
	Limit     int
	Types     []string
}
type Query struct {
	AgentID, SessionID, TaskID, Cursor string
	Statuses                           []string
	CreatedFrom, CreatedTo             time.Time
	Limit                              int
}
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}
type HistoryPage struct {
	Items   []session.Event `json:"items"`
	NextSeq int64           `json:"next_seq"`
}
type Visibility struct {
	All                               bool
	ResourceIDs, AgentIDs, SessionIDs []string
	PolicyVersion                     string
}
type (
	ResourceRef struct{ Type, ID, AgentID, SessionID, TenantID string }
	Authorizer  interface {
		Authorize(context.Context, Actor, string, ResourceRef) error
		Scope(context.Context, Actor, string, string) (Visibility, error)
	}
)

// TenantAuthorizer 仅用于宿主已经完成用户权限校验的单租户接入，不提供跨租户授权。
type TenantAuthorizer struct{}

func (TenantAuthorizer) Authorize(_ context.Context, a Actor, _ string, r ResourceRef) error {
	if err := ValidateActor(a); err != nil {
		return err
	}
	if r.TenantID != a.TenantID {
		return agentErrors.ErrManagementNotFound
	}
	return nil
}

func (TenantAuthorizer) Scope(_ context.Context, a Actor, _, _ string) (Visibility, error) {
	return Visibility{All: true, PolicyVersion: "tenant-v1"}, ValidateActor(a)
}

type Limits struct {
	PageSize, MaxPageSize, MaxInputBytes, MaxOutputBytes, Workers, MaxPending int
	IdempotencyRetention, TaskRetention, AuditRetention                       time.Duration
}

func DefaultLimits() Limits {
	return Limits{50, 200, 1 << 20, 4 << 20, 4, 10000, 30 * 24 * time.Hour, 90 * 24 * time.Hour, 180 * 24 * time.Hour}
}

type Capabilities struct {
	Reconcile bool `json:"reconcile"`
	Approval  bool `json:"approval"`
	Fork      bool `json:"fork"`
	Input     bool `json:"input"`
}
type Resources struct {
	Task           Task
	Session        Session
	Scope          *execution.Scope
	Journal        durable.Store
	Budget         *budget.Controller
	Sessions       session.Store
	ApprovalReader func(context.Context, string) (Approval, error)
}
type RunSpec struct {
	Reconciliation *Reconciliation
	Task           Task
	ResumeInputs   map[string]string
}
type RunResult struct {
	Operations []durable.OperationRef `json:"operations,omitempty"`
	Outcome    string                 `json:"outcome"`
	Output     json.RawMessage        `json:"output,omitempty"`
	Waits      []WaitPoint            `json:"waits,omitempty"`
	ErrorKind  string                 `json:"error_kind,omitempty"`
	ResultRef  string                 `json:"result_ref,omitempty"`
	FinishedAt time.Time              `json:"finished_at"`
}
type RunEvidence struct {
	State  string
	Result RunResult
}
type Runtime interface {
	Run(context.Context, RunSpec) (RunResult, error)
	Inspect(context.Context, RunSpec) (RunEvidence, error)
}

// RuntimeReconciler 只接受宿主核对的不可变证据，不得绕过原操作的重试策略。
type RuntimeReconciler interface {
	Reconcile(context.Context, RunSpec, Reconciliation) (RunEvidence, error)
}
type Binding struct {
	Runtime Runtime
	Cleanup func(context.Context) error
}
type Factory interface {
	Validate(context.Context, Version) (Capabilities, error)
	Build(context.Context, Version, Resources) (Binding, error)
}

// RevisionFactory 从归档中验证完整修订及其运行器，不能用当前配置替代旧版本。
// 普通 Factory 仍可处理未归档的既有 Version，但不能接纳带 RevisionID 的版本。
type RevisionFactory interface {
	Factory
	ValidateRevision(context.Context, Version) (revision.AgentRevision, Capabilities, error)
}
type Reconciliation struct {
	ID, TenantID, TaskID, VersionHash, EvidenceHash, Resolution, VerifiedBy string
	VerifiedAt                                                              time.Time
	OperationRefs                                                           []durable.OperationRef
}
type ReconciliationReader interface {
	Lookup(context.Context, Actor, string) (Reconciliation, error)
}
type DeliverySink interface {
	Accept(context.Context, Delivery) error
}
type Config struct {
	Tracer               trace.Tracer
	ConfirmWorkerStopped func(context.Context, string, int64) error
	Store                Store
	Sessions             session.Store
	HistoryReader        HistoryReader
	Authorizer           Authorizer
	Factories            map[string]Factory
	Reconciliations      ReconciliationReader
	Delivery             DeliverySink
	Limits               Limits
	Now                  func() time.Time
	Report               func(string, error)
}
type HistoryReader interface {
	History(context.Context, string, int64, int, []string) (HistoryPage, error)
}
