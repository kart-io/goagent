package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/checkpoint"
	agentErrors "github.com/kart-io/goagent/errors"
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

// requireInterruptCleaned 断言等待方退出后 manager 内部不再保留该中断的任何状态。
func requireInterruptCleaned(t *testing.T, manager *InterruptManager) {
	t.Helper()
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	require.Empty(t, manager.interrupts, "interrupts 未清理")
	require.Empty(t, manager.channels, "channels 未清理")
	require.Empty(t, manager.responses, "responses 未清理")
	require.Empty(t, manager.creating, "creating 未清理")
}

// CancelInterrupt 后 CreateInterrupt 返回明确的取消错误，不再返回 nil 响应配 nil 错误。
func TestInterruptManager_CancelReturnsError(t *testing.T) {
	manager := NewInterruptManager(nil)
	created := make(chan string, 1)
	manager.OnInterruptCreated(func(i *Interrupt) { created <- i.ID })

	type result struct {
		response *InterruptResponse
		err      error
	}
	done := make(chan result, 1)
	go func() {
		_, response, err := manager.CreateInterrupt(context.Background(), &Interrupt{Type: InterruptTypeApproval})
		done <- result{response, err}
	}()
	id := <-created
	require.NoError(t, manager.CancelInterrupt(id))

	res := <-done
	require.Nil(t, res.response)
	require.Error(t, res.err)
	require.True(t, agentErrors.IsCode(res.err, agentErrors.CodeContextCanceled), "错误码应为 CONTEXT_CANCELED，实际 %v", res.err)
	requireInterruptCleaned(t, manager)
	require.Error(t, manager.RespondToInterrupt(id, &InterruptResponse{Approved: true}), "取消后的响应应返回错误")
}

// ExecuteWithInterrupts 遇到被取消的中断返回错误，不解引用 nil 响应。
func TestInterruptableExecutor_CanceledInterrupt(t *testing.T) {
	manager := NewInterruptManager(nil)
	manager.OnInterruptCreated(func(i *Interrupt) { require.NoError(t, manager.CancelInterrupt(i.ID)) })
	executor := NewInterruptableExecutor(manager, nil)
	executor.AddInterruptRule(InterruptRule{
		Name:            "always",
		Condition:       func(context.Context, State) bool { return true },
		CreateInterrupt: func(context.Context, State) *Interrupt { return &Interrupt{Type: InterruptTypeApproval} },
	})

	executed := false
	err := executor.ExecuteWithInterrupts(context.Background(), NewAgentState(), func(context.Context, State) error {
		executed = true
		return nil
	})
	require.Error(t, err)
	require.True(t, agentErrors.IsCode(errors.Unwrap(err), agentErrors.CodeContextCanceled), "应返回取消错误，实际 %v", err)
	require.False(t, executed)
}

// ctx 结束、超时、收到响应后，等待方退出时清理全部内部状态；迟到的 Respond 返回 not found。
func TestInterruptManager_WaiterExitCleansUp(t *testing.T) {
	t.Run("ctx 结束", func(t *testing.T) {
		manager := NewInterruptManager(nil)
		created := make(chan string, 1)
		manager.OnInterruptCreated(func(i *Interrupt) { created <- i.ID })
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, _, err := manager.CreateInterrupt(ctx, &Interrupt{Type: InterruptTypeApproval})
			done <- err
		}()
		id := <-created
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
		requireInterruptCleaned(t, manager)
		require.Empty(t, manager.ListPendingInterrupts())
		err := manager.RespondToInterrupt(id, &InterruptResponse{Approved: true})
		require.True(t, agentErrors.IsCode(err, agentErrors.CodeAgentNotFound), "迟到的响应应返回 not found，实际 %v", err)
	})
	t.Run("超时", func(t *testing.T) {
		manager := NewInterruptManager(nil)
		expires := time.Now().Add(10 * time.Millisecond)
		_, _, err := manager.CreateInterrupt(context.Background(), &Interrupt{ID: "expiring", Type: InterruptTypeApproval, ExpiresAt: &expires})
		require.True(t, agentErrors.IsCode(err, agentErrors.CodeContextTimeout), "实际 %v", err)
		requireInterruptCleaned(t, manager)
		err = manager.RespondToInterrupt("expiring", &InterruptResponse{Approved: true})
		require.True(t, agentErrors.IsCode(err, agentErrors.CodeAgentNotFound), "迟到的响应应返回 not found，实际 %v", err)
	})
	t.Run("收到响应", func(t *testing.T) {
		manager := NewInterruptManager(nil)
		created := make(chan string, 1)
		manager.OnInterruptCreated(func(i *Interrupt) { created <- i.ID })
		id, done := createInBackground(t, manager, created)
		require.NoError(t, manager.RespondToInterrupt(id, &InterruptResponse{Approved: true}))
		require.NoError(t, <-done)
		requireInterruptCleaned(t, manager)
		err := manager.RespondToInterrupt(id, &InterruptResponse{Approved: true})
		require.True(t, agentErrors.IsCode(err, agentErrors.CodeAgentNotFound), "重复响应应返回 not found，实际 %v", err)
	})
}

// Respond 与 ctx 结束、Cancel 并发：不 panic，结果一致（Respond 成功则等待方拿到该响应），退出后状态清空。
func TestInterruptManager_RespondRacesWithExit(t *testing.T) {
	for i := 0; i < 300; i++ {
		manager := NewInterruptManager(nil)
		created := make(chan string, 1)
		manager.OnInterruptCreated(func(i *Interrupt) { created <- i.ID })
		ctx, cancel := context.WithCancel(context.Background())
		type result struct {
			response *InterruptResponse
			err      error
		}
		done := make(chan result, 1)
		go func() {
			_, response, err := manager.CreateInterrupt(ctx, &Interrupt{Type: InterruptTypeApproval})
			done <- result{response, err}
		}()
		id := <-created

		var wg sync.WaitGroup
		var respondErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			respondErr = manager.RespondToInterrupt(id, &InterruptResponse{Approved: true, Reason: "ok"})
		}()
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				cancel()
			} else {
				_ = manager.CancelInterrupt(id)
			}
		}()
		wg.Wait()
		res := <-done
		cancel()
		if respondErr == nil {
			require.NoError(t, res.err, "第 %d 轮：Respond 成功但等待方返回错误", i)
			require.Equal(t, "ok", res.response.Reason)
		} else {
			require.Error(t, res.err, "第 %d 轮", i)
			require.Nil(t, res.response)
		}
		requireInterruptCleaned(t, manager)
	}
}

// 对同一个中断，onCreated 一定先于 onResolved 完成：onCreated 执行期间到达的响应等它完成后才处理。
func TestInterruptManager_CreatedHookCompletesBeforeResolved(t *testing.T) {
	manager := NewInterruptManager(nil)
	var mu sync.Mutex
	var events []string
	record := func(e string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	}
	created := make(chan string, 1)
	gate := make(chan struct{})
	manager.OnInterruptCreated(func(i *Interrupt) {
		created <- i.ID
		<-gate
		record("created")
	})
	manager.OnInterruptResolved(func(*Interrupt, *InterruptResponse) { record("resolved") })

	done := make(chan error, 1)
	go func() {
		_, response, err := manager.CreateInterrupt(context.Background(), &Interrupt{Type: InterruptTypeApproval})
		if err == nil && !response.Approved {
			err = errors.New("响应内容错误")
		}
		done <- err
	}()
	id := <-created
	// onCreated 仍在执行时响应：Respond 不得阻塞，也不得先于 onCreated 调用 onResolved
	require.NoError(t, manager.RespondToInterrupt(id, &InterruptResponse{Approved: true}))
	close(gate)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(hookLimit):
		t.Fatal("CreateInterrupt 未返回")
	}
	mu.Lock()
	require.Equal(t, []string{"created", "resolved"}, events)
	mu.Unlock()
	requireInterruptCleaned(t, manager)
}

// onCreated 内响应同一个中断不会死锁，onResolved 仍在 onCreated 之后执行。
func TestInterruptManager_RespondInsideCreatedHook(t *testing.T) {
	manager := NewInterruptManager(nil)
	var order []string
	manager.OnInterruptCreated(func(i *Interrupt) {
		require.NoError(t, manager.RespondToInterrupt(i.ID, &InterruptResponse{Approved: true}))
		order = append(order, "created")
	})
	manager.OnInterruptResolved(func(*Interrupt, *InterruptResponse) { order = append(order, "resolved") })

	done := make(chan error, 1)
	go func() {
		_, _, err := manager.CreateInterrupt(context.Background(), &Interrupt{Type: InterruptTypeApproval})
		done <- err
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(hookLimit):
		t.Fatal("onCreated 内响应导致死锁")
	}
	require.Equal(t, []string{"created", "resolved"}, order)
	requireInterruptCleaned(t, manager)
}

// onCreated panic 时，期间到达的响应随中断一起撤销：不调用 onResolved，等待方与响应方都不阻塞。
func TestInterruptManager_CreatedHookPanicWithPendingResponse(t *testing.T) {
	manager := NewInterruptManager(nil)
	resolved := false
	manager.OnInterruptCreated(func(i *Interrupt) {
		require.NoError(t, manager.RespondToInterrupt(i.ID, &InterruptResponse{Approved: true}))
		panic("created boom")
	})
	manager.OnInterruptResolved(func(*Interrupt, *InterruptResponse) { resolved = true })

	_, response, err := manager.CreateInterrupt(context.Background(), &Interrupt{Type: InterruptTypeApproval})
	requirePanicError(t, err, "created boom", "TestInterruptManager_CreatedHookPanicWithPendingResponse")
	require.Nil(t, response)
	require.False(t, resolved)
	requireInterruptCleaned(t, manager)
}
