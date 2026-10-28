package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/checkpoint"
)

func TestNewInterruptManager(t *testing.T) {
	checkpointer := checkpoint.NewInMemorySaver()
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
	checkpointer := checkpoint.NewInMemorySaver()
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

// hookLimit 只用来防止测试挂起，不参与正确性判断。
const hookLimit = 2 * time.Second

// createInBackground 在 goroutine 中调用 CreateInterrupt，经 onCreated 钩子取得中断 ID。
func createInBackground(t *testing.T, manager *InterruptManager, created <-chan string) (string, <-chan error) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, _, err := manager.CreateInterrupt(context.Background(), &Interrupt{Type: InterruptTypeApproval, Message: "m"})
		done <- err
	}()
	select {
	case id := <-created:
		return id, done
	case <-time.After(hookLimit):
		t.Fatal("onCreated 钩子未被调用")
		return "", nil
	}
}

// CreateInterrupt 返回时 onResolved 钩子必须已执行完：钩子阻塞期间等待方不得返回。
func TestInterruptManager_ResolvedHookRunsBeforeWaiterReturns(t *testing.T) {
	manager := NewInterruptManager(nil)
	created := make(chan string, 1)
	manager.OnInterruptCreated(func(i *Interrupt) { created <- i.ID })
	returnedEarly := make(chan bool, 1)
	var done <-chan error
	manager.OnInterruptResolved(func(*Interrupt, *InterruptResponse) {
		// 等待方若提前返回，这里会在 100ms 内观察到；修复后它在钩子返回前不可能返回
		select {
		case <-done:
			returnedEarly <- true
		case <-time.After(100 * time.Millisecond):
			returnedEarly <- false
		}
	})

	var id string
	id, done = createInBackground(t, manager, created)
	require.NoError(t, manager.RespondToInterrupt(id, &InterruptResponse{Approved: true}))
	require.False(t, <-returnedEarly, "CreateInterrupt 在 onResolved 钩子执行完之前返回")
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(hookLimit):
		t.Fatal("CreateInterrupt 未返回")
	}
}

// 钩子在锁外执行：钩子里调用 manager 的方法不会死锁。
func TestInterruptManager_HooksCanCallManager(t *testing.T) {
	manager := NewInterruptManager(nil)
	created := make(chan string, 1)
	manager.OnInterruptCreated(func(i *Interrupt) {
		_, err := manager.GetInterrupt(i.ID)
		require.NoError(t, err)
		created <- i.ID
	})
	var got *Interrupt
	var getErr error
	var pending []*Interrupt
	manager.OnInterruptResolved(func(i *Interrupt, _ *InterruptResponse) {
		got, getErr = manager.GetInterrupt(i.ID)
		pending = manager.ListPendingInterrupts()
	})

	id, done := createInBackground(t, manager, created)
	responded := make(chan error, 1)
	go func() { responded <- manager.RespondToInterrupt(id, &InterruptResponse{Approved: true}) }()
	select {
	case err := <-responded:
		require.NoError(t, err)
	case <-time.After(hookLimit):
		t.Fatal("RespondToInterrupt 死锁：钩子在持锁时被调用")
	}
	require.NoError(t, getErr)
	require.Equal(t, id, got.ID)
	require.Empty(t, pending, "钩子执行时响应已记录")
	require.NoError(t, <-done)
}

// onResolved 钩子 panic 时 RespondToInterrupt 返回 PanicError，等待方照常收到响应，不会永远阻塞。
func TestInterruptManager_ResolvedHookPanic(t *testing.T) {
	manager := NewInterruptManager(nil)
	created := make(chan string, 1)
	manager.OnInterruptCreated(func(i *Interrupt) { created <- i.ID })
	manager.OnInterruptResolved(func(*Interrupt, *InterruptResponse) { panic("resolved boom") })

	id, done := createInBackground(t, manager, created)
	err := manager.RespondToInterrupt(id, &InterruptResponse{Approved: true})
	requirePanicError(t, err, "resolved boom", "TestInterruptManager_ResolvedHookPanic")
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(hookLimit):
		t.Fatal("onResolved 钩子 panic 后等待方未收到响应")
	}
}

// onCreated 钩子 panic 时 CreateInterrupt 返回 PanicError，中断随之撤销，不留在待处理列表中。
func TestInterruptManager_CreatedHookPanic(t *testing.T) {
	manager := NewInterruptManager(nil)
	manager.OnInterruptCreated(func(*Interrupt) { panic("created boom") })

	_, _, err := manager.CreateInterrupt(context.Background(), &Interrupt{Type: InterruptTypeApproval})
	requirePanicError(t, err, "created boom", "TestInterruptManager_CreatedHookPanic")
	require.Empty(t, manager.ListPendingInterrupts())
}

// gatedSaver 的 Save 通知 saving 后阻塞到 release 关闭。
type gatedSaver struct {
	checkpoint.Checkpointer
	saving  chan struct{}
	release chan struct{}
}

func (g *gatedSaver) Save(ctx context.Context, threadID string, s State) error {
	close(g.saving)
	<-g.release
	return g.Checkpointer.Save(ctx, threadID, s)
}

// 中断在状态保存之后才对外可见：响应方不可能在快照写入之前看到并处理它。
func TestInterruptManager_StateSavedBeforeVisible(t *testing.T) {
	saver := &gatedSaver{Checkpointer: checkpoint.NewInMemorySaver(), saving: make(chan struct{}), release: make(chan struct{})}
	manager := NewInterruptManager(saver)
	created := make(chan string, 1)
	manager.OnInterruptCreated(func(i *Interrupt) { created <- i.ID })
	state := NewAgentState()
	state.Set("k", "v")
	done := make(chan error, 1)
	go func() {
		_, _, err := manager.CreateInterrupt(context.Background(), &Interrupt{Type: InterruptTypeApproval, State: state})
		done <- err
	}()

	<-saver.saving
	require.Empty(t, manager.ListPendingInterrupts(), "状态保存完成前中断不应可见")
	close(saver.release)
	id := <-created
	require.NoError(t, manager.RespondToInterrupt(id, &InterruptResponse{Approved: true}))
	require.NoError(t, <-done)
}
