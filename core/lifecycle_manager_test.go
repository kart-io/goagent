package core

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Fixtures
// =============================================================================

// mockLifecycleComponent is a test implementation of Lifecycle.
type mockLifecycleComponent struct {
	name         string
	initCalled   atomic.Bool
	startCalled  atomic.Bool
	stopCalled   atomic.Bool
	healthCalled atomic.Bool
	initErr      error
	startErr     error
	stopErr      error
	healthStatus interfaces.HealthStatus
	dependencies []string
	initDelay    time.Duration
	startDelay   time.Duration
	stopDelay    time.Duration
}

func newMockComponent(name string) *mockLifecycleComponent {
	return &mockLifecycleComponent{
		name:         name,
		healthStatus: interfaces.NewHealthyStatus(),
	}
}

func (m *mockLifecycleComponent) Init(ctx context.Context, config interface{}) error {
	m.initCalled.Store(true)
	if m.initDelay > 0 {
		select {
		case <-time.After(m.initDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.initErr
}

func (m *mockLifecycleComponent) Start(ctx context.Context) error {
	m.startCalled.Store(true)
	if m.startDelay > 0 {
		select {
		case <-time.After(m.startDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.startErr
}

func (m *mockLifecycleComponent) Stop(ctx context.Context) error {
	m.stopCalled.Store(true)
	if m.stopDelay > 0 {
		select {
		case <-time.After(m.stopDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.stopErr
}

func (m *mockLifecycleComponent) HealthCheck(ctx context.Context) interfaces.HealthStatus {
	m.healthCalled.Store(true)
	return m.healthStatus
}

func (m *mockLifecycleComponent) Dependencies() []string {
	return m.dependencies
}

// =============================================================================
// LifecycleManager Tests
// =============================================================================

func TestNewLifecycleManager(t *testing.T) {
	manager := NewLifecycleManager()
	assert.NotNil(t, manager)
	assert.NotNil(t, manager.components)
}

func TestLifecycleManager_Register(t *testing.T) {
	manager := NewLifecycleManager()
	component := newMockComponent("test")

	t.Run("Register new component", func(t *testing.T) {
		err := manager.Register("test", component, 0)
		assert.NoError(t, err)
	})

	t.Run("Duplicate registration fails", func(t *testing.T) {
		err := manager.Register("test", component, 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})
}

func TestLifecycleManager_Unregister(t *testing.T) {
	manager := NewLifecycleManager()
	component := newMockComponent("test")
	manager.Register("test", component, 0)

	t.Run("Unregister existing component", func(t *testing.T) {
		err := manager.Unregister("test")
		assert.NoError(t, err)
	})

	t.Run("Unregister non-existent component", func(t *testing.T) {
		err := manager.Unregister("nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestLifecycleManager_InitAll(t *testing.T) {
	t.Run("Successful initialization", func(t *testing.T) {
		manager := NewLifecycleManager()
		comp1 := newMockComponent("comp1")
		comp2 := newMockComponent("comp2")

		manager.Register("comp1", comp1, 1)
		manager.Register("comp2", comp2, 2)

		err := manager.InitAll(context.Background())
		assert.NoError(t, err)
		assert.True(t, comp1.initCalled.Load())
		assert.True(t, comp2.initCalled.Load())
	})

	t.Run("Initialization with error", func(t *testing.T) {
		manager := NewLifecycleManager()
		comp := newMockComponent("failing")
		comp.initErr = errors.New("init failed")

		manager.Register("failing", comp, 0)

		err := manager.InitAll(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "initialization failed")
	})

	t.Run("Continue on error when configured", func(t *testing.T) {
		config := DefaultLifecycleManagerConfig()
		config.ContinueOnError = true
		manager := NewLifecycleManagerWithConfig(config)

		failing := newMockComponent("failing")
		failing.initErr = errors.New("init failed")
		working := newMockComponent("working")

		manager.Register("failing", failing, 1)
		manager.Register("working", working, 2)

		err := manager.InitAll(context.Background())
		assert.NoError(t, err)
		assert.True(t, working.initCalled.Load())
	})
}

func TestLifecycleManager_StartAll(t *testing.T) {
	t.Run("Successful start", func(t *testing.T) {
		manager := NewLifecycleManager()
		comp := newMockComponent("comp")
		manager.Register("comp", comp, 0)

		// Must init first
		manager.InitAll(context.Background())

		err := manager.StartAll(context.Background())
		assert.NoError(t, err)
		assert.True(t, comp.startCalled.Load())

		state, _ := manager.GetState("comp")
		assert.Equal(t, interfaces.StateRunning, state)
	})

	t.Run("Start with error", func(t *testing.T) {
		manager := NewLifecycleManager()
		comp := newMockComponent("comp")
		comp.startErr = errors.New("start failed")

		manager.Register("comp", comp, 0)
		manager.InitAll(context.Background())

		err := manager.StartAll(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "start failed")

		state, _ := manager.GetState("comp")
		assert.Equal(t, interfaces.StateFailed, state)
	})
}

func TestLifecycleManager_StopAll(t *testing.T) {
	t.Run("Successful stop", func(t *testing.T) {
		manager := NewLifecycleManager()
		comp := newMockComponent("comp")
		manager.Register("comp", comp, 0)

		manager.InitAll(context.Background())
		manager.StartAll(context.Background())

		err := manager.StopAll(context.Background())
		assert.NoError(t, err)
		assert.True(t, comp.stopCalled.Load())

		state, _ := manager.GetState("comp")
		assert.Equal(t, interfaces.StateStopped, state)
	})

	t.Run("Stop in reverse priority order", func(t *testing.T) {
		manager := NewLifecycleManager()

		var stopOrder []string
		var mu sync.Mutex

		comp1 := newMockComponent("low-priority")
		comp2 := newMockComponent("high-priority")

		// Create wrapper components to track stop order
		wrap1 := &stopOrderWrapper{mock: comp1, order: &stopOrder, mu: &mu, name: "low-priority"}
		wrap2 := &stopOrderWrapper{mock: comp2, order: &stopOrder, mu: &mu, name: "high-priority"}

		manager.Register("low-priority", wrap1, 1)
		manager.Register("high-priority", wrap2, 10)

		manager.InitAll(context.Background())
		manager.StartAll(context.Background())
		manager.StopAll(context.Background())

		// High priority should stop first
		require.Len(t, stopOrder, 2)
		assert.Equal(t, "high-priority", stopOrder[0])
		assert.Equal(t, "low-priority", stopOrder[1])
	})
}

// stopOrderWrapper wraps a mock to track stop order.
type stopOrderWrapper struct {
	mock  *mockLifecycleComponent
	order *[]string
	mu    *sync.Mutex
	name  string
}

func (w *stopOrderWrapper) Init(ctx context.Context, config interface{}) error {
	return w.mock.Init(ctx, config)
}

func (w *stopOrderWrapper) Start(ctx context.Context) error {
	return w.mock.Start(ctx)
}

func (w *stopOrderWrapper) Stop(ctx context.Context) error {
	w.mu.Lock()
	*w.order = append(*w.order, w.name)
	w.mu.Unlock()
	return w.mock.Stop(ctx)
}

func (w *stopOrderWrapper) HealthCheck(ctx context.Context) interfaces.HealthStatus {
	return w.mock.HealthCheck(ctx)
}

func TestLifecycleManager_HealthCheckAll(t *testing.T) {
	manager := NewLifecycleManager()

	healthy := newMockComponent("healthy")
	healthy.healthStatus = interfaces.NewHealthyStatus()

	unhealthy := newMockComponent("unhealthy")
	unhealthy.healthStatus = interfaces.NewUnhealthyStatus(errors.New("service down"))

	manager.Register("healthy", healthy, 0)
	manager.Register("unhealthy", unhealthy, 0)

	results := manager.HealthCheckAll(context.Background())

	assert.Len(t, results, 2)
	assert.Equal(t, interfaces.HealthHealthy, results["healthy"].State)
	assert.Equal(t, interfaces.HealthUnhealthy, results["unhealthy"].State)
}

func TestLifecycleManager_GetState(t *testing.T) {
	manager := NewLifecycleManager()
	comp := newMockComponent("comp")
	manager.Register("comp", comp, 0)

	t.Run("Get existing state", func(t *testing.T) {
		state, err := manager.GetState("comp")
		require.NoError(t, err)
		assert.Equal(t, interfaces.StateUninitialized, state)
	})

	t.Run("Get non-existent state", func(t *testing.T) {
		_, err := manager.GetState("nonexistent")
		assert.Error(t, err)
	})
}

func TestLifecycleManager_WaitForShutdown(t *testing.T) {
	manager := NewLifecycleManager()
	comp := newMockComponent("comp")
	manager.Register("comp", comp, 0)

	manager.InitAll(context.Background())
	manager.StartAll(context.Background())

	// Start wait in goroutine
	done := make(chan error)
	go func() {
		done <- manager.WaitForShutdown(context.Background())
	}()

	// Stop should signal completion
	manager.StopAll(context.Background())

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("WaitForShutdown did not complete")
	}
}

func TestLifecycleManager_Dependencies(t *testing.T) {
	manager := NewLifecycleManager()

	var startOrder []string
	var mu sync.Mutex

	db := newMockComponent("database")
	cache := newMockComponent("cache")
	cache.dependencies = []string{"database"}
	app := newMockComponent("app")
	app.dependencies = []string{"cache", "database"}

	// Track start order
	dbWrap := &startOrderWrapper{mock: db, order: &startOrder, mu: &mu, name: "database"}
	cacheWrap := &startOrderWrapper{mock: cache, order: &startOrder, mu: &mu, name: "cache"}
	appWrap := &startOrderWrapper{mock: app, order: &startOrder, mu: &mu, name: "app", deps: []string{"cache", "database"}}

	// Register in random priority order
	manager.Register("app", appWrap, 3)
	manager.Register("database", dbWrap, 1)
	manager.Register("cache", cacheWrap, 2)

	manager.InitAll(context.Background())
	err := manager.StartAll(context.Background())
	require.NoError(t, err)

	// Database should start before cache, cache before app
	require.Len(t, startOrder, 3)
	assert.Equal(t, "database", startOrder[0])
}

// startOrderWrapper wraps a mock to track start order.
type startOrderWrapper struct {
	mock  *mockLifecycleComponent
	order *[]string
	mu    *sync.Mutex
	name  string
	deps  []string
}

func (w *startOrderWrapper) Init(ctx context.Context, config interface{}) error {
	return w.mock.Init(ctx, config)
}

func (w *startOrderWrapper) Start(ctx context.Context) error {
	w.mu.Lock()
	*w.order = append(*w.order, w.name)
	w.mu.Unlock()
	return w.mock.Start(ctx)
}

func (w *startOrderWrapper) Stop(ctx context.Context) error {
	return w.mock.Stop(ctx)
}

func (w *startOrderWrapper) HealthCheck(ctx context.Context) interfaces.HealthStatus {
	return w.mock.HealthCheck(ctx)
}

func (w *startOrderWrapper) Dependencies() []string {
	return w.deps
}

func TestLifecycleManager_Concurrency(t *testing.T) {
	manager := NewLifecycleManager()

	// Register many components
	for i := 0; i < 20; i++ {
		comp := newMockComponent("comp")
		manager.Register("comp-"+string(rune('a'+i)), comp, i)
	}

	// Run operations concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			manager.HealthCheckAll(context.Background())
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// =============================================================================
// BaseLifecycle Tests
// =============================================================================

func TestBaseLifecycle(t *testing.T) {
	base := NewBaseLifecycle("test")

	err := base.Init(context.Background(), nil)
	assert.NoError(t, err)

	err = base.Start(context.Background())
	assert.NoError(t, err)

	err = base.Stop(context.Background())
	assert.NoError(t, err)

	status := base.HealthCheck(context.Background())
	assert.Equal(t, interfaces.HealthHealthy, status.State)
}

// =============================================================================
// FunctionalLifecycle Tests
// =============================================================================

func TestFunctionalLifecycle(t *testing.T) {
	var initCalled, startCalled, stopCalled bool

	fl := NewFunctionalLifecycle("test",
		WithInitFunc(func(ctx context.Context, config interface{}) error {
			initCalled = true
			return nil
		}),
		WithStartFunc(func(ctx context.Context) error {
			startCalled = true
			return nil
		}),
		WithStopFunc(func(ctx context.Context) error {
			stopCalled = true
			return nil
		}),
		WithHealthFunc(func(ctx context.Context) interfaces.HealthStatus {
			return interfaces.NewDegradedStatus("test degraded")
		}),
	)

	fl.Init(context.Background(), nil)
	assert.True(t, initCalled)

	fl.Start(context.Background())
	assert.True(t, startCalled)

	fl.Stop(context.Background())
	assert.True(t, stopCalled)

	status := fl.HealthCheck(context.Background())
	assert.Equal(t, interfaces.HealthDegraded, status.State)
}

func TestFunctionalLifecycle_DefaultBehavior(t *testing.T) {
	fl := NewFunctionalLifecycle("test")

	// All methods should work without custom functions
	err := fl.Init(context.Background(), nil)
	assert.NoError(t, err)

	err = fl.Start(context.Background())
	assert.NoError(t, err)

	err = fl.Stop(context.Background())
	assert.NoError(t, err)

	status := fl.HealthCheck(context.Background())
	assert.Equal(t, interfaces.HealthHealthy, status.State)
}

// =============================================================================
// Global Manager Tests
// =============================================================================

func TestGlobalLifecycleManager(t *testing.T) {
	manager1 := GlobalLifecycleManager()
	manager2 := GlobalLifecycleManager()

	assert.Same(t, manager1, manager2)
}

// =============================================================================
// HealthStatus Tests
// =============================================================================

func TestHealthStatus_Helpers(t *testing.T) {
	t.Run("NewHealthyStatus", func(t *testing.T) {
		status := interfaces.NewHealthyStatus()
		assert.True(t, status.IsHealthy())
		assert.True(t, status.IsOperational())
	})

	t.Run("NewUnhealthyStatus", func(t *testing.T) {
		status := interfaces.NewUnhealthyStatus(errors.New("test error"))
		assert.False(t, status.IsHealthy())
		assert.False(t, status.IsOperational())
		assert.Contains(t, status.Message, "test error")
	})

	t.Run("NewDegradedStatus", func(t *testing.T) {
		status := interfaces.NewDegradedStatus("partial failure")
		assert.False(t, status.IsHealthy())
		assert.True(t, status.IsOperational())
	})
}
