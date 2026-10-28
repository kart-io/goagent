package core

import (
	"context"
	"fmt"
	"sync"
	"time"

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
	interrupts map[string]*Interrupt
	responses  map[string]*InterruptResponse
	channels   map[string]chan *InterruptResponse
	// creating 记录 onCreated 钩子尚未执行完的中断，期间到达的响应暂存其中，由 CreateInterrupt 在钩子完成后处理
	creating map[string]*createGate
	mu       sync.RWMutex

	// Checkpointer for persisting interrupted state
	checkpointer checkpoint.Checkpointer

	// Hooks for interrupt lifecycle events
	onInterruptCreated  func(*Interrupt)
	onInterruptResolved func(*Interrupt, *InterruptResponse)
}

// createGate 暂存 onCreated 执行期间到达的响应，受 InterruptManager.mu 保护。
type createGate struct {
	response *InterruptResponse
}

// NewInterruptManager creates a new interrupt manager.
func NewInterruptManager(checkpointer checkpoint.Checkpointer) *InterruptManager {
	return &InterruptManager{
		interrupts:   make(map[string]*Interrupt),
		responses:    make(map[string]*InterruptResponse),
		channels:     make(map[string]chan *InterruptResponse),
		creating:     make(map[string]*createGate),
		checkpointer: checkpointer,
	}
}

// CreateInterrupt creates a new interrupt and waits for human response.
// Returns the created interrupt (with ID assigned) and the response.
//
// 返回时（收到响应、被 CancelInterrupt 取消、超时或 ctx 结束）都会清理该中断的内部状态，之后的 RespondToInterrupt
// 返回 not found；被取消时返回 CodeContextCanceled 错误，响应为 nil 时错误必不为 nil。超时或 ctx 结束时若响应
// 已被 RespondToInterrupt 接受，以该响应为准。onCreated 执行期间到达的响应等它执行完后才调用 onResolved 并返回。
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

	// 先保存状态再登记：中断一旦可见就可能被响应，快照必须已经写入
	if m.checkpointer != nil && interruptCopy.State != nil {
		_ = m.checkpointer.Save(ctx, fmt.Sprintf("interrupt_%s", interruptCopy.ID), interruptCopy.State)
	}

	// Store interrupt under lock
	m.mu.Lock()
	m.interrupts[interruptCopy.ID] = &interruptCopy
	responseChan := make(chan *InterruptResponse, 1)
	m.channels[interruptCopy.ID] = responseChan
	onCreated := m.onInterruptCreated
	gate := &createGate{}
	if onCreated != nil {
		m.creating[interruptCopy.ID] = gate
	}
	m.mu.Unlock()
	defer m.removeInterrupt(&interruptCopy, responseChan)

	// 钩子在锁外调用，可以调用 manager 的方法；panic 时撤销该中断并返回 PanicError
	if onCreated != nil {
		panicked, err := CallSafely(ctx, "interrupt_manager", "on_interrupt_created", func() error {
			onCreated(&interruptCopy)
			return nil
		})
		m.mu.Lock()
		if m.creating[interruptCopy.ID] == gate {
			delete(m.creating, interruptCopy.ID)
		}
		deferred := gate.response
		onResolved := m.onInterruptResolved
		if panicked {
			// 与清除 creating 在同一临界区内撤销，响应方不会在撤销前把它当作普通中断处理；暂存的响应随之丢弃
			m.removeInterruptLocked(&interruptCopy, responseChan)
		}
		m.mu.Unlock()
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
	case response, ok := <-responseChan:
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

	// 通道仍登记说明无人认领：撤销后返回，之后的响应返回 not found。已被 RespondToInterrupt/CancelInterrupt
	// 认领则它们必定随后发送响应或关闭通道，以其结果为准，避免响应方成功而等待方报错
	m.mu.Lock()
	unclaimed := m.channels[interruptCopy.ID] == responseChan
	if unclaimed {
		m.removeInterruptLocked(&interruptCopy, responseChan)
	}
	m.mu.Unlock()
	if unclaimed {
		return &interruptCopy, nil, exitErr
	}
	if response, ok := <-responseChan; ok {
		return &interruptCopy, response, nil
	}
	return &interruptCopy, nil, interruptCanceledError(interruptCopy.ID)
}

// removeInterrupt 在等待方退出时清理该中断的全部状态，可重复调用。
func (m *InterruptManager) removeInterrupt(interrupt *Interrupt, ch chan *InterruptResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeInterruptLocked(interrupt, ch)
}

// removeInterruptLocked 只删除仍属于该次 CreateInterrupt 的条目，同 ID 的新中断不受影响；调用方须持有 m.mu。
// CancelInterrupt 已删除 interrupts 条目时 responses 也一并清理。
func (m *InterruptManager) removeInterruptLocked(interrupt *Interrupt, ch chan *InterruptResponse) {
	id := interrupt.ID
	if m.channels[id] == ch {
		delete(m.channels, id)
	}
	if cur, ok := m.interrupts[id]; !ok || cur == interrupt {
		delete(m.interrupts, id)
		delete(m.responses, id)
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
// 中响应同一个中断也不会死锁。已被响应过的中断返回 CodeInvalidInput 错误，等待方已退出的返回 not found。
func (m *InterruptManager) RespondToInterrupt(interruptID string, response *InterruptResponse) error {
	m.mu.Lock()
	interrupt, ok := m.interrupts[interruptID]
	if !ok {
		m.mu.Unlock()
		return agentErrors.New(agentErrors.CodeAgentNotFound, "interrupt not found").
			WithComponent("interrupt_manager").
			WithOperation("respond_to_interrupt").
			WithContext("interrupt_id", interruptID)
	}

	// 取走通道即认领该中断：之后的 CancelInterrupt 不会再关闭它，等待方超时退出时也会以本响应为准
	ch, pending := m.channels[interruptID]
	if !pending {
		m.mu.Unlock()
		return agentErrors.New(agentErrors.CodeInvalidInput, "中断已被响应").
			WithComponent("interrupt_manager").
			WithOperation("respond_to_interrupt").
			WithContext("interrupt_id", interruptID)
	}
	delete(m.channels, interruptID)

	response.InterruptID = interruptID
	response.RespondedAt = time.Now()
	m.responses[interruptID] = response

	if gate, creating := m.creating[interruptID]; creating {
		gate.response = response
		m.mu.Unlock()
		return nil
	}
	onResolved := m.onInterruptResolved
	m.mu.Unlock()

	_, hookErr := callResolvedHook(onResolved, interrupt, response)

	// 通道带 1 个缓冲且只由认领者写入，等待方已退出也不会阻塞
	ch <- response
	close(ch)

	return hookErr
}

// GetInterrupt retrieves an interrupt by ID.
func (m *InterruptManager) GetInterrupt(interruptID string) (*Interrupt, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	interrupt, ok := m.interrupts[interruptID]
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeAgentNotFound, "interrupt not found").
			WithComponent("interrupt_manager").
			WithOperation("get_interrupt").
			WithContext("interrupt_id", interruptID)
	}

	return interrupt, nil
}

// ListPendingInterrupts returns all interrupts awaiting response.
func (m *InterruptManager) ListPendingInterrupts() []*Interrupt {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pending := make([]*Interrupt, 0)
	for id, interrupt := range m.interrupts {
		if _, responded := m.responses[id]; !responded {
			pending = append(pending, interrupt)
		}
	}

	return pending
}

// CancelInterrupt cancels a pending interrupt.
func (m *InterruptManager) CancelInterrupt(interruptID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch, ok := m.channels[interruptID]
	if ok {
		close(ch)
		delete(m.channels, interruptID)
	}

	delete(m.interrupts, interruptID)
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

var (
	interruptCounter   uint64
	interruptCounterMu sync.Mutex
)

func generateInterruptID() string {
	interruptCounterMu.Lock()
	defer interruptCounterMu.Unlock()
	interruptCounter++
	return fmt.Sprintf("interrupt_%d_%d", time.Now().Unix(), interruptCounter)
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
