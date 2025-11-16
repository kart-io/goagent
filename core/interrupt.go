package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kart-io/goagent/core/checkpoint"
	"github.com/kart-io/goagent/core/state"
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
	mu         sync.RWMutex

	// Checkpointer for persisting interrupted state
	checkpointer checkpoint.Checkpointer

	// Hooks for interrupt lifecycle events
	onInterruptCreated  func(*Interrupt)
	onInterruptResolved func(*Interrupt, *InterruptResponse)
}

// NewInterruptManager creates a new interrupt manager.
func NewInterruptManager(checkpointer checkpoint.Checkpointer) *InterruptManager {
	return &InterruptManager{
		interrupts:   make(map[string]*Interrupt),
		responses:    make(map[string]*InterruptResponse),
		channels:     make(map[string]chan *InterruptResponse),
		checkpointer: checkpointer,
	}
}

// CreateInterrupt creates a new interrupt and waits for human response.
// Returns the created interrupt (with ID assigned) and the response.
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

	// Store interrupt under lock
	m.mu.Lock()
	m.interrupts[interruptCopy.ID] = &interruptCopy
	responseChan := make(chan *InterruptResponse, 1)
	m.channels[interruptCopy.ID] = responseChan
	m.mu.Unlock()

	// Save state if checkpointer available
	if m.checkpointer != nil && interruptCopy.State != nil {
		_ = m.checkpointer.Save(ctx, fmt.Sprintf("interrupt_%s", interruptCopy.ID), interruptCopy.State)
	}

	// Call onCreate hook
	if m.onInterruptCreated != nil {
		m.onInterruptCreated(&interruptCopy)
	}

	// Wait for response or context cancellation
	select {
	case response := <-responseChan:
		return &interruptCopy, response, nil
	case <-ctx.Done():
		return &interruptCopy, nil, ctx.Err()
	case <-time.After(getTimeoutForInterrupt(&interruptCopy)):
		return &interruptCopy, nil, fmt.Errorf("interrupt %s timed out", interruptCopy.ID)
	}
}

// RespondToInterrupt provides a response to an existing interrupt.
func (m *InterruptManager) RespondToInterrupt(interruptID string, response *InterruptResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	interrupt, ok := m.interrupts[interruptID]
	if !ok {
		return fmt.Errorf("interrupt not found: %s", interruptID)
	}

	response.InterruptID = interruptID
	response.RespondedAt = time.Now()

	m.responses[interruptID] = response

	// Send response to waiting goroutine
	ch, ok := m.channels[interruptID]
	if ok {
		ch <- response
		close(ch)
		delete(m.channels, interruptID)
	}

	// Call onResolved hook
	if m.onInterruptResolved != nil {
		m.onInterruptResolved(interrupt, response)
	}

	return nil
}

// GetInterrupt retrieves an interrupt by ID.
func (m *InterruptManager) GetInterrupt(interruptID string) (*Interrupt, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	interrupt, ok := m.interrupts[interruptID]
	if !ok {
		return nil, fmt.Errorf("interrupt not found: %s", interruptID)
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
	m.onInterruptCreated = fn
}

// OnInterruptResolved sets a hook for when interrupts are resolved.
func (m *InterruptManager) OnInterruptResolved(fn func(*Interrupt, *InterruptResponse)) {
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
			return fmt.Errorf("interrupt failed: %w", err)
		}

		// Check if approved
		if !response.Approved {
			return fmt.Errorf("interrupted and not approved: %s", response.Reason)
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
