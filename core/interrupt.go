package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/kart-io/goagent/core/checkpoint"
	"github.com/kart-io/goagent/core/state"
	agentErrors "github.com/kart-io/goagent/errors"
)

// InterruptType defines the type of interrupt.
type InterruptType string

const (
	// InterruptTypeApproval requires human approval to continue
	InterruptTypeApproval InterruptType = "approval"

	// InterruptTypeInput requires human input/feedback
	InterruptTypeInput InterruptType = "input"

	// InterruptTypeReview requires human review before proceeding
	InterruptTypeReview InterruptType = "review"

	// InterruptTypeDecision requires human decision making
	InterruptTypeDecision InterruptType = "decision"
)

// InterruptPriority defines the priority/severity of an interrupt.
type InterruptPriority string

const (
	// InterruptPriorityLow can be handled asynchronously
	InterruptPriorityLow InterruptPriority = "low"

	// InterruptPriorityMedium should be handled in a reasonable timeframe
	InterruptPriorityMedium InterruptPriority = "medium"

	// InterruptPriorityHigh requires immediate attention
	InterruptPriorityHigh InterruptPriority = "high"

	// InterruptPriorityCritical blocks execution until resolved
	InterruptPriorityCritical InterruptPriority = "critical"
)

// Interrupt represents a point where human intervention is needed.
type Interrupt struct {
	// ID is a unique identifier for this interrupt
	//
	// 需要在 CreateInterrupt 等待期间响应时应预先设置 ID：CreateInterrupt 只给内部副本分配 ID，不回写传入对象。
	// 为空时自动生成，只能经 OnInterruptCreated 钩子、ListPendingInterrupts 或返回值取得。
	ID string

	// Type specifies what kind of interrupt this is
	Type InterruptType

	// Priority indicates how urgently this needs to be handled
	Priority InterruptPriority

	// Message describes what human action is needed
	Message string

	// Context provides additional context for the interrupt
	Context map[string]interface{}

	// State is a snapshot of the agent state at interrupt time
	State state.State

	// CreatedAt is when the interrupt was created
	CreatedAt time.Time

	// ExpiresAt is when the interrupt expires (optional)
	ExpiresAt *time.Time

	// Metadata holds additional interrupt metadata
	Metadata map[string]interface{}
}

// InterruptResponse represents the human response to an interrupt.
type InterruptResponse struct {
	// InterruptID is the ID of the interrupt being responded to
	InterruptID string

	// Approved indicates if the action was approved
	Approved bool

	// Input contains any human-provided input/feedback
	Input map[string]interface{}

	// Reason explains why the decision was made
	Reason string

	// RespondedAt is when the response was provided
	RespondedAt time.Time

	// RespondedBy identifies who provided the response
	RespondedBy string
}

// InterruptManager manages interrupts and their lifecycle.
type InterruptManager struct {
	// entries 以 ID 为键，记录每次 CreateInterrupt 从占用 ID 到等待方退出的全部状态；条目删除前该 ID 不可复用
	entries map[string]*interruptEntry
	mu      sync.RWMutex

	// Checkpointer for persisting interrupted state
	checkpointer checkpoint.Checkpointer

	// Hooks for interrupt lifecycle events
	onInterruptCreated  func(*Interrupt)
	onInterruptResolved func(*Interrupt, *InterruptResponse)
}

// interruptPhase 是一次 CreateInterrupt 在登记流程中所处的阶段。
type interruptPhase int

const (
	// phaseSaving 已占用 ID、正在保存状态快照，对外不可见
	phaseSaving interruptPhase = iota
	// phaseCreating 已登记、onCreated 钩子尚未执行完，期间到达的响应由 CreateInterrupt 在钩子完成后处理
	phaseCreating
	// phaseWaiting 等待响应
	phaseWaiting
)

// interruptEntry 是一次 CreateInterrupt 的状态，字段受 InterruptManager.mu 保护。
// response 非 nil 或 canceled 为 true 即 ch 已被认领：只有认领者向 ch 发送响应或关闭它。
type interruptEntry struct {
	interrupt *Interrupt
	phase     interruptPhase
	// ch 带 1 个缓冲，认领者写入后不会阻塞
	ch chan *InterruptResponse
	// response 是已被 RespondToInterrupt 接受的响应
	response *InterruptResponse
	// canceled 表示已被 CancelInterrupt 取消，对外不可见；在 phaseSaving 时取消由 CreateInterrupt 在保存完成后处理
	canceled bool
}

func (e *interruptEntry) visible() bool {
	return e.phase != phaseSaving && !e.canceled
}

func (e *interruptEntry) claimed() bool {
	return e.response != nil || e.canceled
}

// NewInterruptManager creates a new interrupt manager.
func NewInterruptManager(checkpointer checkpoint.Checkpointer) *InterruptManager {
	return &InterruptManager{
		entries:      make(map[string]*interruptEntry),
		checkpointer: checkpointer,
	}
}

// CreateInterrupt creates a new interrupt and waits for human response.
// Returns the created interrupt (with ID assigned) and the response.
//
// 返回时（收到响应、被 CancelInterrupt 取消、超时或 ctx 结束）都会清理该中断的内部状态，之后的 RespondToInterrupt
// 返回 not found；被取消时返回 CodeContextCanceled 错误，响应为 nil 时错误必不为 nil。保存状态快照期间被取消时
// 不再登记、不调用 onCreated。超时或 ctx 结束时若响应已被 RespondToInterrupt 接受，以该响应为准。onCreated
// 执行期间到达的响应等它执行完后才调用 onResolved 并返回。
// ID 与尚未清理完的中断（包括已被响应或取消、等待方尚未退出的）重复时立即返回 CodeAlreadyExists 错误，
// 不保存快照、不登记、不调用 onCreated；已结束并清理的中断的 ID 可以复用。
func (m *InterruptManager) CreateInterrupt(ctx context.Context, interrupt *Interrupt) (*Interrupt, *InterruptResponse, error) {
	// Create a copy to avoid concurrent access issues
	interruptCopy := *interrupt

	// Set defaults
	if interruptCopy.ID == "" {
		interruptCopy.ID = generateInterruptID()
	}
	if interruptCopy.CreatedAt.IsZero() {
		interruptCopy.CreatedAt = time.Now()
	}

	// 先占用 ID 再保存快照：重复的 ID 不得覆盖仍在等待的中断的快照
	entry, err := m.reserve(&interruptCopy)
	if err != nil {
		return &interruptCopy, nil, err
	}

	// 先保存状态再登记：中断一旦可见就可能被响应，快照必须已经写入
	// 保存失败照旧忽略；checkpointer panic 时释放占用的 ID 并返回错误，否则该 ID 会永久被判为占用
	if m.checkpointer != nil && interruptCopy.State != nil {
		if panicked, err := CallSafely(ctx, "interrupt_manager", "save_state", func() error {
			return m.checkpointer.Save(ctx, fmt.Sprintf("interrupt_%s", interruptCopy.ID), interruptCopy.State)
		}); panicked {
			m.remove(entry)
			return &interruptCopy, nil, err
		}
	}

	onCreated, canceled := m.register(entry)
	if canceled {
		return &interruptCopy, nil, interruptCanceledError(interruptCopy.ID)
	}
	defer m.remove(entry)

	// 钩子在锁外调用，可以调用 manager 的方法；panic 时撤销该中断并返回 PanicError
	if onCreated != nil {
		panicked, err := CallSafely(ctx, "interrupt_manager", "on_interrupt_created", func() error {
			onCreated(&interruptCopy)
			return nil
		})
		deferred, onResolved := m.finishCreating(entry, panicked)
		if panicked {
			return &interruptCopy, nil, err
		}
		if deferred != nil {
			// 响应在 onCreated 执行期间到达，由这里调用 onResolved；panic 没有调用方接收，只交给全局 PanicHandler
			_, _ = callResolvedHook(onResolved, &interruptCopy, deferred)
			return &interruptCopy, deferred, nil
		}
	}

	// Wait for response or context cancellation
	var exitErr error
	select {
	case response, ok := <-entry.ch:
		if !ok {
			return &interruptCopy, nil, interruptCanceledError(interruptCopy.ID)
		}
		return &interruptCopy, response, nil
	case <-ctx.Done():
		exitErr = ctx.Err()
	case <-time.After(getTimeoutForInterrupt(&interruptCopy)):
		exitErr = agentErrors.New(agentErrors.CodeContextTimeout, "interrupt timed out").
			WithComponent("interrupt_manager").
			WithOperation("create_interrupt").
			WithContext("interrupt_id", interruptCopy.ID)
	}

	// 无人认领则撤销后返回，之后的响应返回 not found。已被 RespondToInterrupt/CancelInterrupt 认领则它们必定
	// 随后发送响应或关闭通道，以其结果为准，避免响应方成功而等待方报错
	if m.removeIfUnclaimed(entry) {
		return &interruptCopy, nil, exitErr
	}
	if response, ok := <-entry.ch; ok {
		return &interruptCopy, response, nil
	}
	return &interruptCopy, nil, interruptCanceledError(interruptCopy.ID)
}

// reserve 为 interrupt 占用 ID，ID 仍被未清理完的中断占用时返回 CodeAlreadyExists。
func (m *InterruptManager) reserve(interrupt *Interrupt) (*interruptEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, inUse := m.entries[interrupt.ID]; inUse {
		return nil, agentErrors.New(agentErrors.CodeAlreadyExists, "中断 ID 已被仍在等待的中断占用").
			WithComponent("interrupt_manager").
			WithOperation("create_interrupt").
			WithContext("interrupt_id", interrupt.ID)
	}
	entry := &interruptEntry{interrupt: interrupt, phase: phaseSaving, ch: make(chan *InterruptResponse, 1)}
	m.entries[interrupt.ID] = entry
	return entry, nil
}

// register 使中断对外可见并返回需要调用的 onCreated；保存快照期间已被取消时删除条目，canceled 为 true。
func (m *InterruptManager) register(entry *interruptEntry) (onCreated func(*Interrupt), canceled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry.canceled {
		m.removeLocked(entry)
		return nil, true
	}
	onCreated = m.onInterruptCreated
	if onCreated != nil {
		entry.phase = phaseCreating
	} else {
		entry.phase = phaseWaiting
	}
	return onCreated, false
}

// finishCreating 在 onCreated 返回后结束 phaseCreating，返回期间暂存的响应。钩子 panic 时在同一临界区内撤销该中断，
// 响应方不会在撤销前把它当作普通中断处理；暂存的响应随之丢弃。
func (m *InterruptManager) finishCreating(entry *interruptEntry, panicked bool) (deferred *InterruptResponse, onResolved func(*Interrupt, *InterruptResponse)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry.phase = phaseWaiting
	if panicked {
		m.removeLocked(entry)
		return nil, nil
	}
	return entry.response, m.onInterruptResolved
}

// removeIfUnclaimed 在 ch 尚未被认领时删除条目并返回 true。
func (m *InterruptManager) removeIfUnclaimed(entry *interruptEntry) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry.claimed() {
		return false
	}
	m.removeLocked(entry)
	return true
}

// remove 在等待方退出时清理该中断的全部状态，可重复调用。
func (m *InterruptManager) remove(entry *interruptEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeLocked(entry)
}

// removeLocked 只删除仍属于该次 CreateInterrupt 的条目，同 ID 的新中断不受影响；调用方须持有 m.mu。
func (m *InterruptManager) removeLocked(entry *interruptEntry) {
	if m.entries[entry.interrupt.ID] == entry {
		delete(m.entries, entry.interrupt.ID)
	}
}

func interruptCanceledError(interruptID string) error {
	return agentErrors.New(agentErrors.CodeContextCanceled, "中断已被取消").
		WithComponent("interrupt_manager").
		WithOperation("create_interrupt").
		WithContext("interrupt_id", interruptID)
}

func callResolvedHook(onResolved func(*Interrupt, *InterruptResponse), interrupt *Interrupt, response *InterruptResponse) (bool, error) {
	if onResolved == nil {
		return false, nil
	}
	return CallSafely(context.Background(), "interrupt_manager", "on_interrupt_resolved", func() error {
		onResolved(interrupt, response)
		return nil
	})
}

// RespondToInterrupt provides a response to an existing interrupt.
//
// onResolved 钩子在锁外、把响应交给等待中的 CreateInterrupt 之前执行，CreateInterrupt 返回时钩子已执行完；
// 钩子 panic 时仍把响应交给等待方，并返回 PanicError。若该中断的 onCreated 仍在执行，响应先暂存、本方法立即返回，
// 由 CreateInterrupt 在 onCreated 完成后调用 onResolved（其 panic 只交给全局 PanicHandler），因此在 onCreated
// 中响应同一个中断也不会死锁。response 为 nil 或中断已被响应过时返回 CodeInvalidInput 错误，等待方已退出的返回 not found。
func (m *InterruptManager) RespondToInterrupt(interruptID string, response *InterruptResponse) error {
	if response == nil {
		return agentErrors.New(agentErrors.CodeInvalidInput, "中断响应不能为 nil").
			WithComponent("interrupt_manager").
			WithOperation("respond_to_interrupt").
			WithContext("interrupt_id", interruptID)
	}

	entry, onResolved, deliver, err := m.acceptResponse(interruptID, response)
	if err != nil || !deliver {
		return err
	}

	_, hookErr := callResolvedHook(onResolved, entry.interrupt, response)

	// 只有认领者写入 ch，等待方已退出也不会阻塞
	entry.ch <- response
	close(entry.ch)

	return hookErr
}

// acceptResponse 记录响应即认领该中断：之后的 CancelInterrupt 不会再关闭 ch，等待方超时退出时也会以本响应为准。
// deliver 为 false 表示 onCreated 仍在执行，响应已暂存，由 CreateInterrupt 处理。
func (m *InterruptManager) acceptResponse(interruptID string, response *InterruptResponse) (entry *interruptEntry, onResolved func(*Interrupt, *InterruptResponse), deliver bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[interruptID]
	if !ok || !entry.visible() {
		return nil, nil, false, agentErrors.New(agentErrors.CodeAgentNotFound, "interrupt not found").
			WithComponent("interrupt_manager").
			WithOperation("respond_to_interrupt").
			WithContext("interrupt_id", interruptID)
	}
	if entry.response != nil {
		return nil, nil, false, agentErrors.New(agentErrors.CodeInvalidInput, "中断已被响应").
			WithComponent("interrupt_manager").
			WithOperation("respond_to_interrupt").
			WithContext("interrupt_id", interruptID)
	}

	response.InterruptID = interruptID
	response.RespondedAt = time.Now()
	entry.response = response
	if entry.phase == phaseCreating {
		return entry, nil, false, nil
	}
	return entry, m.onInterruptResolved, true, nil
}

// GetInterrupt retrieves an interrupt by ID.
func (m *InterruptManager) GetInterrupt(interruptID string) (*Interrupt, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, ok := m.entries[interruptID]
	if !ok || !entry.visible() {
		return nil, agentErrors.New(agentErrors.CodeAgentNotFound, "interrupt not found").
			WithComponent("interrupt_manager").
			WithOperation("get_interrupt").
			WithContext("interrupt_id", interruptID)
	}

	return entry.interrupt, nil
}

// ListPendingInterrupts returns all interrupts awaiting response.
func (m *InterruptManager) ListPendingInterrupts() []*Interrupt {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pending := make([]*Interrupt, 0)
	for _, entry := range m.entries {
		if entry.visible() && entry.response == nil {
			pending = append(pending, entry.interrupt)
		}
	}

	return pending
}

// CancelInterrupt cancels a pending interrupt.
//
// 等待中的 CreateInterrupt 返回 CodeContextCanceled；正在保存状态快照的，保存完成后不再登记并返回同样的错误。
// 已被响应的中断不受影响（等待方仍拿到该响应），但之后对外不可见。ID 不存在时什么也不做。
func (m *InterruptManager) CancelInterrupt(interruptID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.entries[interruptID]
	if !ok {
		return nil
	}
	if !entry.claimed() {
		close(entry.ch)
	}
	entry.canceled = true
	return nil
}

// OnInterruptCreated sets a hook for when interrupts are created.
func (m *InterruptManager) OnInterruptCreated(fn func(*Interrupt)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onInterruptCreated = fn
}

// OnInterruptResolved sets a hook for when interrupts are resolved.
func (m *InterruptManager) OnInterruptResolved(fn func(*Interrupt, *InterruptResponse)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onInterruptResolved = fn
}

// InterruptableExecutor wraps execution with interrupt capability.
type InterruptableExecutor struct {
	manager      *InterruptManager
	checkpointer checkpoint.Checkpointer

	// InterruptRules defines when to interrupt
	interruptRules []InterruptRule
}

// InterruptRule defines a condition that triggers an interrupt.
type InterruptRule struct {
	// Name identifies the rule
	Name string

	// Condition evaluates if an interrupt should be triggered
	Condition func(ctx context.Context, state state.State) bool

	// CreateInterrupt creates the interrupt if condition is met
	CreateInterrupt func(ctx context.Context, state state.State) *Interrupt
}

// NewInterruptableExecutor creates a new interruptable executor.
func NewInterruptableExecutor(manager *InterruptManager, checkpointer checkpoint.Checkpointer) *InterruptableExecutor {
	return &InterruptableExecutor{
		manager:        manager,
		checkpointer:   checkpointer,
		interruptRules: make([]InterruptRule, 0),
	}
}

// AddInterruptRule adds a rule that triggers interrupts.
func (e *InterruptableExecutor) AddInterruptRule(rule InterruptRule) {
	e.interruptRules = append(e.interruptRules, rule)
}

// CheckInterrupts evaluates all rules and creates interrupts if needed.
func (e *InterruptableExecutor) CheckInterrupts(ctx context.Context, state state.State) ([]*Interrupt, error) {
	interrupts := make([]*Interrupt, 0)

	for _, rule := range e.interruptRules {
		if rule.Condition(ctx, state) {
			interrupt := rule.CreateInterrupt(ctx, state)
			if interrupt != nil {
				interrupts = append(interrupts, interrupt)
			}
		}
	}

	return interrupts, nil
}

// ExecuteWithInterrupts executes a function with interrupt checking.
func (e *InterruptableExecutor) ExecuteWithInterrupts(
	ctx context.Context,
	state state.State,
	fn func(context.Context, state.State) error,
) error {
	// Check for interrupts before execution
	interrupts, err := e.CheckInterrupts(ctx, state)
	if err != nil {
		return err
	}

	// Handle any triggered interrupts
	for _, interrupt := range interrupts {
		interrupt.State = state.Clone()
		_, response, err := e.manager.CreateInterrupt(ctx, interrupt)
		if err != nil {
			return agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "interrupt failed").
				WithComponent("interruptable_executor").
				WithOperation("execute_with_interrupts")
		}

		// Check if approved
		if !response.Approved {
			return agentErrors.New(agentErrors.CodeAgentExecution, "interrupted and not approved").
				WithComponent("interruptable_executor").
				WithOperation("execute_with_interrupts").
				WithContext("reason", response.Reason)
		}

		// Apply any input from the response to state
		if len(response.Input) > 0 {
			for key, value := range response.Input {
				state.Set(key, value)
			}
		}
	}

	// Execute the function
	return fn(ctx, state)
}

// Helper functions

// generateInterruptID 用随机 UUID：时间戳加进程内计数器在同一秒内重启后会重复，多个进程共用 checkpointer 时快照 key 冲突
func generateInterruptID() string {
	return "interrupt_" + uuid.NewString()
}

func getTimeoutForInterrupt(interrupt *Interrupt) time.Duration {
	if interrupt.ExpiresAt != nil {
		return time.Until(*interrupt.ExpiresAt)
	}

	// Default timeout based on priority
	switch interrupt.Priority {
	case InterruptPriorityCritical:
		return 5 * time.Minute
	case InterruptPriorityHigh:
		return 15 * time.Minute
	case InterruptPriorityMedium:
		return 1 * time.Hour
	case InterruptPriorityLow:
		return 24 * time.Hour
	default:
		return 1 * time.Hour
	}
}

// InterruptConfig configures interrupt behavior.
type InterruptConfig struct {
	// EnableAutoSave saves state before each interrupt
	EnableAutoSave bool

	// DefaultTimeout is the default timeout for interrupts
	DefaultTimeout time.Duration

	// RequireReason requires a reason for all responses
	RequireReason bool

	// EnableHistory keeps history of all interrupts
	EnableHistory bool
}

// DefaultInterruptConfig returns default interrupt configuration.
func DefaultInterruptConfig() *InterruptConfig {
	return &InterruptConfig{
		EnableAutoSave: true,
		DefaultTimeout: 1 * time.Hour,
		RequireReason:  true,
		EnableHistory:  true,
	}
}
