package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInterruptManager(t *testing.T) {
	checkpointer := NewInMemorySaver()
	manager := NewInterruptManager(checkpointer)

	assert.NotNil(t, manager)
	assert.NotNil(t, manager.interrupts)
	assert.NotNil(t, manager.responses)
	assert.NotNil(t, manager.channels)
	assert.Equal(t, checkpointer, manager.checkpointer)
}

func TestInterruptManager_CreateAndRespond(t *testing.T) {
	manager := NewInterruptManager(nil)
	ctx := context.Background()

	// Create interrupt in goroutine
	interrupt := &Interrupt{
		Type:     InterruptTypeApproval,
		Priority: InterruptPriorityHigh,
		Message:  "Please approve this action",
		Context: map[string]interface{}{
			"action": "delete_database",
		},
	}

	// Start waiting for response
	type result struct {
		interrupt *Interrupt
		response  *InterruptResponse
		err       error
	}
	resultChan := make(chan result, 1)

	go func() {
		interrupt, response, err := manager.CreateInterrupt(ctx, interrupt)
		resultChan <- result{interrupt: interrupt, response: response, err: err}
	}()

	// Wait a bit to ensure interrupt is created
	time.Sleep(50 * time.Millisecond)

	// Get the list of pending interrupts to find the created one
	pending := manager.ListPendingInterrupts()
	require.Len(t, pending, 1)
	created := pending[0]

	// Verify interrupt was created
	retrieved, err := manager.GetInterrupt(created.ID)
	require.NoError(t, err)
	assert.Equal(t, InterruptTypeApproval, retrieved.Type)
	assert.Equal(t, "Please approve this action", retrieved.Message)

	// Respond to interrupt
	response := &InterruptResponse{
		Approved:    true,
		Reason:      "Action approved by admin",
		RespondedBy: "admin@example.com",
	}

	err = manager.RespondToInterrupt(created.ID, response)
	require.NoError(t, err)

	// Verify we received the response
	select {
	case res := <-resultChan:
		require.NoError(t, res.err)
		assert.True(t, res.response.Approved)
		assert.Equal(t, "Action approved by admin", res.response.Reason)
		assert.NotEmpty(t, res.interrupt.ID)
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestInterruptManager_RespondNotApproved(t *testing.T) {
	manager := NewInterruptManager(nil)
	ctx := context.Background()

	interrupt := &Interrupt{
		Type:     InterruptTypeApproval,
		Priority: InterruptPriorityMedium,
		Message:  "Dangerous operation",
	}

	type result struct {
		interrupt *Interrupt
		response  *InterruptResponse
	}
	resultChan := make(chan result, 1)

	go func() {
		interrupt, response, _ := manager.CreateInterrupt(ctx, interrupt)
		resultChan <- result{interrupt: interrupt, response: response}
	}()

	time.Sleep(50 * time.Millisecond)

	// Get the created interrupt ID
	pending := manager.ListPendingInterrupts()
	require.Len(t, pending, 1)
	interruptID := pending[0].ID

	// Respond with denial
	response := &InterruptResponse{
		Approved:    false,
		Reason:      "Too risky",
		RespondedBy: "reviewer@example.com",
	}

	err := manager.RespondToInterrupt(interruptID, response)
	require.NoError(t, err)

	res := <-resultChan
	assert.False(t, res.response.Approved)
	assert.Equal(t, "Too risky", res.response.Reason)
	assert.NotEmpty(t, res.interrupt.ID)
}

func TestInterruptManager_ListPending(t *testing.T) {
	manager := NewInterruptManager(nil)

	// Create multiple interrupts without responding
	interrupts := []*Interrupt{
		{Type: InterruptTypeApproval, Message: "Approval 1"},
		{Type: InterruptTypeInput, Message: "Input 1"},
		{Type: InterruptTypeReview, Message: "Review 1"},
	}

	for _, interrupt := range interrupts {
		manager.mu.Lock()
		if interrupt.ID == "" {
			interrupt.ID = generateInterruptID()
		}
		manager.interrupts[interrupt.ID] = interrupt
		manager.channels[interrupt.ID] = make(chan *InterruptResponse, 1)
		manager.mu.Unlock()
	}

	// List pending interrupts
	pending := manager.ListPendingInterrupts()
	assert.Len(t, pending, 3)

	// Respond to one
	err := manager.RespondToInterrupt(interrupts[0].ID, &InterruptResponse{
		Approved: true,
	})
	require.NoError(t, err)

	// List again
	pending = manager.ListPendingInterrupts()
	assert.Len(t, pending, 2)
}

func TestInterruptManager_CancelInterrupt(t *testing.T) {
	manager := NewInterruptManager(nil)
	ctx := context.Background()

	interrupt := &Interrupt{
		Type:     InterruptTypeApproval,
		Priority: InterruptPriorityLow,
		Message:  "Cancellable interrupt",
	}

	responseChan := make(chan *InterruptResponse, 1)
	errorChan := make(chan error, 1)

	go func() {
		_, response, err := manager.CreateInterrupt(ctx, interrupt)
		if err != nil {
			errorChan <- err
			return
		}
		responseChan <- response
	}()

	time.Sleep(50 * time.Millisecond)

	// Get the created interrupt ID
	pending := manager.ListPendingInterrupts()
	require.Len(t, pending, 1)
	interruptID := pending[0].ID

	// Cancel the interrupt
	err := manager.CancelInterrupt(interruptID)
	require.NoError(t, err)

	// Verify the goroutine receives a nil response due to channel closure
	select {
	case response := <-responseChan:
		// Channel was closed, so we get nil
		assert.Nil(t, response)
	case err := <-errorChan:
		// Or we get an error
		assert.NotNil(t, err)
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for cancellation")
	}
}

func TestInterruptManager_WithCheckpointer(t *testing.T) {
	checkpointer := NewInMemorySaver()
	manager := NewInterruptManager(checkpointer)
	ctx := context.Background()

	state := NewAgentState()
	state.Set("important_data", "critical_value")

	interrupt := &Interrupt{
		Type:     InterruptTypeApproval,
		Priority: InterruptPriorityHigh,
		Message:  "State should be saved",
		State:    state,
	}

	var createdID string
	go func() {
		time.Sleep(50 * time.Millisecond)
		pending := manager.ListPendingInterrupts()
		if len(pending) > 0 {
			createdID = pending[0].ID
			manager.RespondToInterrupt(createdID, &InterruptResponse{
				Approved: true,
			})
		}
	}()

	createdInterrupt, _, err := manager.CreateInterrupt(ctx, interrupt)
	require.NoError(t, err)
	require.NotEmpty(t, createdInterrupt.ID)

	// Verify state was saved
	savedState, err := checkpointer.Load(ctx, "interrupt_"+createdInterrupt.ID)
	require.NoError(t, err)
	assert.NotNil(t, savedState)

	value, ok := savedState.Get("important_data")
	assert.True(t, ok)
	assert.Equal(t, "critical_value", value)
}

func TestInterruptManager_Hooks(t *testing.T) {
	manager := NewInterruptManager(nil)
	ctx := context.Background()

	var mu sync.Mutex
	createdCalled := false
	resolvedCalled := false

	manager.OnInterruptCreated(func(i *Interrupt) {
		mu.Lock()
		defer mu.Unlock()
		createdCalled = true
		assert.Equal(t, "Hook test", i.Message)
	})

	manager.OnInterruptResolved(func(i *Interrupt, r *InterruptResponse) {
		mu.Lock()
		defer mu.Unlock()
		resolvedCalled = true
		assert.True(t, r.Approved)
	})

	interrupt := &Interrupt{
		Type:    InterruptTypeApproval,
		Message: "Hook test",
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		pending := manager.ListPendingInterrupts()
		if len(pending) > 0 {
			manager.RespondToInterrupt(pending[0].ID, &InterruptResponse{
				Approved: true,
			})
		}
	}()

	_, _, err := manager.CreateInterrupt(ctx, interrupt)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.True(t, createdCalled, "onCreate hook should be called")
	assert.True(t, resolvedCalled, "onResolved hook should be called")
}

func TestInterruptableExecutor_AddRule(t *testing.T) {
	manager := NewInterruptManager(nil)
	executor := NewInterruptableExecutor(manager, nil)

	rule := InterruptRule{
		Name: "test_rule",
		Condition: func(ctx context.Context, state State) bool {
			return true
		},
		CreateInterrupt: func(ctx context.Context, state State) *Interrupt {
			return &Interrupt{
				Type:    InterruptTypeApproval,
				Message: "Test interrupt",
			}
		},
	}

	executor.AddInterruptRule(rule)
	assert.Len(t, executor.interruptRules, 1)
}

func TestInterruptableExecutor_CheckInterrupts(t *testing.T) {
	manager := NewInterruptManager(nil)
	executor := NewInterruptableExecutor(manager, nil)
	ctx := context.Background()
	state := NewAgentState()

	// Add a rule that always triggers
	executor.AddInterruptRule(InterruptRule{
		Name: "always_trigger",
		Condition: func(ctx context.Context, state State) bool {
			return true
		},
		CreateInterrupt: func(ctx context.Context, state State) *Interrupt {
			return &Interrupt{
				Type:     InterruptTypeApproval,
				Priority: InterruptPriorityHigh,
				Message:  "Always interrupt",
			}
		},
	})

	// Add a rule that never triggers
	executor.AddInterruptRule(InterruptRule{
		Name: "never_trigger",
		Condition: func(ctx context.Context, state State) bool {
			return false
		},
		CreateInterrupt: func(ctx context.Context, state State) *Interrupt {
			return &Interrupt{
				Type:    InterruptTypeInput,
				Message: "Never interrupt",
			}
		},
	})

	interrupts, err := executor.CheckInterrupts(ctx, state)
	require.NoError(t, err)
	assert.Len(t, interrupts, 1)
	assert.Equal(t, "Always interrupt", interrupts[0].Message)
}

func TestInterruptableExecutor_ExecuteWithInterrupts(t *testing.T) {
	manager := NewInterruptManager(nil)
	executor := NewInterruptableExecutor(manager, nil)
	ctx := context.Background()
	state := NewAgentState()
	state.Set("action", "delete")

	// Add rule that triggers on delete action
	executor.AddInterruptRule(InterruptRule{
		Name: "delete_approval",
		Condition: func(ctx context.Context, state State) bool {
			action, ok := state.Get("action")
			return ok && action == "delete"
		},
		CreateInterrupt: func(ctx context.Context, state State) *Interrupt {
			return &Interrupt{
				Type:     InterruptTypeApproval,
				Priority: InterruptPriorityCritical,
				Message:  "Approve delete action",
			}
		},
	})

	// Approve in background
	go func() {
		time.Sleep(50 * time.Millisecond)
		pending := manager.ListPendingInterrupts()
		if len(pending) > 0 {
			manager.RespondToInterrupt(pending[0].ID, &InterruptResponse{
				Approved: true,
				Reason:   "Approved by test",
			})
		}
	}()

	executed := false
	err := executor.ExecuteWithInterrupts(ctx, state, func(ctx context.Context, state State) error {
		executed = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, executed, "Function should be executed after approval")
}

func TestInterruptableExecutor_ExecuteRejected(t *testing.T) {
	manager := NewInterruptManager(nil)
	executor := NewInterruptableExecutor(manager, nil)
	ctx := context.Background()
	state := NewAgentState()
	state.Set("risky", true)

	executor.AddInterruptRule(InterruptRule{
		Name: "risky_check",
		Condition: func(ctx context.Context, state State) bool {
			risky, ok := state.Get("risky")
			return ok && risky == true
		},
		CreateInterrupt: func(ctx context.Context, state State) *Interrupt {
			return &Interrupt{
				Type:     InterruptTypeReview,
				Priority: InterruptPriorityHigh,
				Message:  "Risky operation needs review",
			}
		},
	})

	// Reject in background
	go func() {
		time.Sleep(50 * time.Millisecond)
		pending := manager.ListPendingInterrupts()
		if len(pending) > 0 {
			manager.RespondToInterrupt(pending[0].ID, &InterruptResponse{
				Approved:    false,
				Reason:      "Too risky",
				RespondedBy: "safety_officer",
			})
		}
	}()

	executed := false
	err := executor.ExecuteWithInterrupts(ctx, state, func(ctx context.Context, state State) error {
		executed = true
		return nil
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not approved")
	assert.False(t, executed, "Function should not be executed if not approved")
}

func TestGetTimeoutForInterrupt(t *testing.T) {
	tests := []struct {
		name     string
		priority InterruptPriority
		expected time.Duration
	}{
		{"critical", InterruptPriorityCritical, 5 * time.Minute},
		{"high", InterruptPriorityHigh, 15 * time.Minute},
		{"medium", InterruptPriorityMedium, 1 * time.Hour},
		{"low", InterruptPriorityLow, 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interrupt := &Interrupt{
				Priority: tt.priority,
			}
			timeout := getTimeoutForInterrupt(interrupt)
			assert.Equal(t, tt.expected, timeout)
		})
	}
}

func TestGenerateInterruptID(t *testing.T) {
	id1 := generateInterruptID()
	id2 := generateInterruptID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2, "IDs should be unique")
	assert.Contains(t, id1, "interrupt_")
}
