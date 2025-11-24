// Package core provides the lifecycle manager implementation.
//
// This file provides coordinated lifecycle management for GoAgent components:
//   - Priority-based startup and shutdown ordering
//   - Dependency resolution
//   - Health aggregation
//   - Graceful shutdown with timeout support
package core

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// =============================================================================
// Lifecycle Manager Implementation
// =============================================================================

// componentEntry holds a registered component and its metadata.
type componentEntry struct {
	name      string
	component interfaces.Lifecycle
	priority  int
	state     interfaces.LifecycleState
	config    interface{}
}

// DefaultLifecycleManager implements interfaces.LifecycleManager.
//
// Features:
//   - Priority-based startup (lower priority starts first)
//   - Reverse priority shutdown (higher priority stops first)
//   - Dependency resolution for components implementing DependencyAware
//   - Concurrent health checks
//   - Graceful shutdown with configurable timeout
type DefaultLifecycleManager struct {
	mu         sync.RWMutex
	components map[string]*componentEntry
	shutdown   chan struct{}
	done       chan struct{}
	config     LifecycleManagerConfig
}

// LifecycleManagerConfig configures the lifecycle manager.
type LifecycleManagerConfig struct {
	// DefaultInitTimeout is the default timeout for Init operations
	DefaultInitTimeout time.Duration

	// DefaultStartTimeout is the default timeout for Start operations
	DefaultStartTimeout time.Duration

	// DefaultStopTimeout is the default timeout for Stop operations
	DefaultStopTimeout time.Duration

	// HealthCheckInterval is the interval between automatic health checks
	HealthCheckInterval time.Duration

	// ContinueOnError determines whether to continue if a component fails
	ContinueOnError bool
}

// DefaultLifecycleManagerConfig returns sensible defaults.
func DefaultLifecycleManagerConfig() LifecycleManagerConfig {
	return LifecycleManagerConfig{
		DefaultInitTimeout:  30 * time.Second,
		DefaultStartTimeout: 30 * time.Second,
		DefaultStopTimeout:  30 * time.Second,
		HealthCheckInterval: 30 * time.Second,
		ContinueOnError:     false,
	}
}

// NewLifecycleManager creates a new lifecycle manager with default config.
func NewLifecycleManager() *DefaultLifecycleManager {
	return NewLifecycleManagerWithConfig(DefaultLifecycleManagerConfig())
}

// NewLifecycleManagerWithConfig creates a new lifecycle manager with custom config.
func NewLifecycleManagerWithConfig(config LifecycleManagerConfig) *DefaultLifecycleManager {
	return &DefaultLifecycleManager{
		components: make(map[string]*componentEntry),
		shutdown:   make(chan struct{}),
		done:       make(chan struct{}),
		config:     config,
	}
}

// Register adds a component to be managed.
func (m *DefaultLifecycleManager) Register(name string, component interfaces.Lifecycle, priority int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.components[name]; exists {
		return agentErrors.New(agentErrors.CodeAlreadyExists, "component already registered").
			WithComponent("lifecycle_manager").
			WithOperation("register").
			WithContext("name", name)
	}

	m.components[name] = &componentEntry{
		name:      name,
		component: component,
		priority:  priority,
		state:     interfaces.StateUninitialized,
	}

	return nil
}

// RegisterWithConfig registers a component with its configuration.
func (m *DefaultLifecycleManager) RegisterWithConfig(name string, component interfaces.Lifecycle, priority int, config interface{}) error {
	if err := m.Register(name, component, priority); err != nil {
		return err
	}

	m.mu.Lock()
	m.components[name].config = config
	m.mu.Unlock()

	return nil
}

// Unregister removes a component from management.
func (m *DefaultLifecycleManager) Unregister(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, exists := m.components[name]
	if !exists {
		return agentErrors.New(agentErrors.CodeNotFound, "component not found").
			WithComponent("lifecycle_manager").
			WithOperation("unregister").
			WithContext("name", name)
	}

	// Cannot unregister running components
	if entry.state == interfaces.StateRunning {
		return agentErrors.New(agentErrors.CodeAgentValidation, "cannot unregister running component").
			WithComponent("lifecycle_manager").
			WithOperation("unregister").
			WithContext("name", name).
			WithContext("state", string(entry.state))
	}

	delete(m.components, name)
	return nil
}

// InitAll initializes all registered components in priority order.
func (m *DefaultLifecycleManager) InitAll(ctx context.Context) error {
	entries := m.getSortedEntries(true) // ascending priority

	for _, entry := range entries {
		if entry.state != interfaces.StateUninitialized {
			continue
		}

		initCtx, cancel := context.WithTimeout(ctx, m.config.DefaultInitTimeout)
		err := m.initComponent(initCtx, entry)
		cancel()

		if err != nil {
			if !m.config.ContinueOnError {
				return err
			}
			// Log error but continue
		}
	}

	return nil
}

// initComponent initializes a single component.
func (m *DefaultLifecycleManager) initComponent(ctx context.Context, entry *componentEntry) error {
	m.mu.Lock()
	entry.state = interfaces.StateInitialized
	config := entry.config
	component := entry.component
	name := entry.name
	m.mu.Unlock()

	// 调用组件的 Init 方法（带 panic 保护 - 使用可配置的 PanicHandler）
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				// 使用全局 PanicHandlerRegistry 处理 panic
				panicErr := panicToError(ctx, "lifecycle_manager", "init", r)
				// 包装为 lifecycle 特定的错误
				if agentErr, ok := panicErr.(*agentErrors.AgentError); ok {
					agentErr.Context["component_name"] = name
					err = agentErr
				} else {
					err = panicErr
				}
			}
		}()
		err = component.Init(ctx, config)
	}()

	if err != nil {
		m.mu.Lock()
		entry.state = interfaces.StateFailed
		m.mu.Unlock()

		return agentErrors.Wrap(err, agentErrors.CodeAgentInitialization, "component initialization failed").
			WithComponent("lifecycle_manager").
			WithOperation("init").
			WithContext("name", name)
	}

	return nil
}

// StartAll starts all initialized components in priority order.
func (m *DefaultLifecycleManager) StartAll(ctx context.Context) error {
	entries := m.getSortedEntries(true) // ascending priority

	// Resolve dependencies first
	orderedEntries, err := m.resolveDependencies(entries)
	if err != nil {
		return err
	}

	for _, entry := range orderedEntries {
		if entry.state != interfaces.StateInitialized {
			continue
		}

		startCtx, cancel := context.WithTimeout(ctx, m.config.DefaultStartTimeout)
		err := m.startComponent(startCtx, entry)
		cancel()

		if err != nil {
			if !m.config.ContinueOnError {
				return err
			}
		}
	}

	return nil
}

// startComponent starts a single component.
func (m *DefaultLifecycleManager) startComponent(ctx context.Context, entry *componentEntry) error {
	m.mu.Lock()
	entry.state = interfaces.StateStarting
	component := entry.component
	name := entry.name
	m.mu.Unlock()

	// 调用组件的 Start 方法（带 panic 保护 - 使用可配置的 PanicHandler）
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				// 使用全局 PanicHandlerRegistry 处理 panic
				panicErr := panicToError(ctx, "lifecycle_manager", "start", r)
				// 包装为 lifecycle 特定的错误
				if agentErr, ok := panicErr.(*agentErrors.AgentError); ok {
					agentErr.Context["component_name"] = name
					err = agentErr
				} else {
					err = panicErr
				}
			}
		}()
		err = component.Start(ctx)
	}()

	if err != nil {
		m.mu.Lock()
		entry.state = interfaces.StateFailed
		m.mu.Unlock()

		return agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "component start failed").
			WithComponent("lifecycle_manager").
			WithOperation("start").
			WithContext("name", name)
	}

	m.mu.Lock()
	entry.state = interfaces.StateRunning
	m.mu.Unlock()

	return nil
}

// StopAll stops all running components in reverse priority order.
func (m *DefaultLifecycleManager) StopAll(ctx context.Context) error {
	entries := m.getSortedEntries(false) // descending priority (reverse order)

	var lastErr error
	for _, entry := range entries {
		if entry.state != interfaces.StateRunning {
			continue
		}

		stopCtx, cancel := context.WithTimeout(ctx, m.config.DefaultStopTimeout)
		err := m.stopComponent(stopCtx, entry)
		cancel()

		if err != nil {
			lastErr = err
			// Continue stopping other components
		}
	}

	// Signal completion
	select {
	case <-m.done:
	default:
		close(m.done)
	}

	return lastErr
}

// stopComponent stops a single component.
func (m *DefaultLifecycleManager) stopComponent(ctx context.Context, entry *componentEntry) error {
	m.mu.Lock()
	entry.state = interfaces.StateStopping
	component := entry.component
	name := entry.name
	m.mu.Unlock()

	// 调用组件的 Stop 方法（带 panic 保护 - 使用可配置的 PanicHandler）
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				// 使用全局 PanicHandlerRegistry 处理 panic
				panicErr := panicToError(ctx, "lifecycle_manager", "stop", r)
				// 包装为 lifecycle 特定的错误
				if agentErr, ok := panicErr.(*agentErrors.AgentError); ok {
					agentErr.Context["component_name"] = name
					err = agentErr
				} else {
					err = panicErr
				}
			}
		}()
		err = component.Stop(ctx)
	}()

	if err != nil {
		m.mu.Lock()
		entry.state = interfaces.StateFailed
		m.mu.Unlock()

		return agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "component stop failed").
			WithComponent("lifecycle_manager").
			WithOperation("stop").
			WithContext("name", name)
	}

	m.mu.Lock()
	entry.state = interfaces.StateStopped
	m.mu.Unlock()

	return nil
}

// HealthCheckAll returns aggregated health of all components.
func (m *DefaultLifecycleManager) HealthCheckAll(ctx context.Context) map[string]interfaces.HealthStatus {
	m.mu.RLock()
	entries := make([]*componentEntry, 0, len(m.components))
	for _, entry := range m.components {
		entries = append(entries, entry)
	}
	m.mu.RUnlock()

	results := make(map[string]interfaces.HealthStatus, len(entries))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, entry := range entries {
		wg.Add(1)
		go func(e *componentEntry) {
			defer wg.Done()

			// 调用 HealthCheck（带 panic 保护 - 使用可配置的 PanicHandler）
			var status interfaces.HealthStatus
			func() {
				defer func() {
					if r := recover(); r != nil {
						// 使用全局 PanicHandlerRegistry 处理 panic（记录指标和日志）
						panicErr := panicToError(ctx, "lifecycle_manager", "health_check", r)

						// Panic 转换为 Unhealthy 状态
						status = interfaces.HealthStatus{
							State:         interfaces.HealthUnhealthy,
							Message:       fmt.Sprintf("health check panicked: %v", r),
							ComponentName: e.name,
							LastChecked:   time.Now(),
						}

						// 如果 PanicHandler 返回的是 AgentError，提取详细信息
						if agentErr, ok := panicErr.(*agentErrors.AgentError); ok {
							if stackTrace, exists := agentErr.Context["stack_trace"]; exists {
								status.Details = map[string]interface{}{
									"panic_value": agentErr.Context["panic_value"],
									"stack_trace": stackTrace,
								}
							}
						}
					}
				}()
				status = e.component.HealthCheck(ctx)
			}()

			status.ComponentName = e.name

			mu.Lock()
			results[e.name] = status
			mu.Unlock()
		}(entry)
	}

	wg.Wait()
	return results
}

// GetState returns the current state of a component.
func (m *DefaultLifecycleManager) GetState(name string) (interfaces.LifecycleState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, exists := m.components[name]
	if !exists {
		return "", agentErrors.New(agentErrors.CodeNotFound, "component not found").
			WithComponent("lifecycle_manager").
			WithOperation("get_state").
			WithContext("name", name)
	}

	return entry.state, nil
}

// WaitForShutdown blocks until all components are stopped.
func (m *DefaultLifecycleManager) WaitForShutdown(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.done:
		return nil
	}
}

// SignalShutdown signals all components to begin shutdown.
func (m *DefaultLifecycleManager) SignalShutdown() {
	select {
	case <-m.shutdown:
	default:
		close(m.shutdown)
	}
}

// ShutdownSignal returns a channel that closes when shutdown is signaled.
func (m *DefaultLifecycleManager) ShutdownSignal() <-chan struct{} {
	return m.shutdown
}

// =============================================================================
// Helper Methods
// =============================================================================

// getSortedEntries returns entries sorted by priority.
func (m *DefaultLifecycleManager) getSortedEntries(ascending bool) []*componentEntry {
	m.mu.RLock()
	entries := make([]*componentEntry, 0, len(m.components))
	for _, entry := range m.components {
		entries = append(entries, entry)
	}
	m.mu.RUnlock()

	sort.Slice(entries, func(i, j int) bool {
		if ascending {
			return entries[i].priority < entries[j].priority
		}
		return entries[i].priority > entries[j].priority
	})

	return entries
}

// resolveDependencies orders entries based on their dependencies.
// Uses topological sort to ensure dependencies start before dependents.
func (m *DefaultLifecycleManager) resolveDependencies(entries []*componentEntry) ([]*componentEntry, error) {
	// Build dependency graph: dependsOn[A] = [B, C] means A depends on B and C
	dependsOn := make(map[string][]string)
	for _, entry := range entries {
		if dep, ok := entry.component.(interfaces.DependencyAware); ok {
			dependsOn[entry.name] = dep.Dependencies()
		}
	}

	// Build entry map and compute in-degrees
	entryMap := make(map[string]*componentEntry, len(entries))
	inDegree := make(map[string]int)
	for _, entry := range entries {
		entryMap[entry.name] = entry
		inDegree[entry.name] = len(dependsOn[entry.name])
	}

	// Build reverse dependency graph: dependedBy[B] = [A] means A depends on B
	dependedBy := make(map[string][]string)
	for name, deps := range dependsOn {
		for _, dep := range deps {
			if _, exists := entryMap[dep]; exists {
				dependedBy[dep] = append(dependedBy[dep], name)
			}
		}
	}

	// Find nodes with no dependencies (in-degree = 0)
	var queue []string
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	// Process queue
	var result []*componentEntry
	for len(queue) > 0 {
		// Pop from queue
		name := queue[0]
		queue = queue[1:]

		result = append(result, entryMap[name])

		// Reduce in-degree of components that depend on this one
		for _, dependent := range dependedBy[name] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// Check for cycles
	if len(result) != len(entries) {
		return nil, agentErrors.New(agentErrors.CodeAgentValidation, "circular dependency detected").
			WithComponent("lifecycle_manager").
			WithOperation("resolve_dependencies")
	}

	return result, nil
}

// =============================================================================
// Base Lifecycle Implementation
// =============================================================================

// BaseLifecycle provides a no-op implementation of Lifecycle interface.
// Components can embed this to only override methods they need.
type BaseLifecycle struct {
	name string
}

// NewBaseLifecycle creates a new BaseLifecycle.
func NewBaseLifecycle(name string) *BaseLifecycle {
	return &BaseLifecycle{name: name}
}

// Init does nothing by default.
func (b *BaseLifecycle) Init(ctx context.Context, config interface{}) error {
	return nil
}

// Start does nothing by default.
func (b *BaseLifecycle) Start(ctx context.Context) error {
	return nil
}

// Stop does nothing by default.
func (b *BaseLifecycle) Stop(ctx context.Context) error {
	return nil
}

// HealthCheck returns healthy by default.
func (b *BaseLifecycle) HealthCheck(ctx context.Context) interfaces.HealthStatus {
	return interfaces.NewHealthyStatus()
}

// =============================================================================
// Functional Lifecycle
// =============================================================================

// FunctionalLifecycle allows creating Lifecycle implementations using functions.
type FunctionalLifecycle struct {
	name     string
	initFn   func(context.Context, interface{}) error
	startFn  func(context.Context) error
	stopFn   func(context.Context) error
	healthFn func(context.Context) interfaces.HealthStatus
}

// FunctionalLifecycleOption configures a FunctionalLifecycle.
type FunctionalLifecycleOption func(*FunctionalLifecycle)

// WithInitFunc sets the Init function.
func WithInitFunc(fn func(context.Context, interface{}) error) FunctionalLifecycleOption {
	return func(f *FunctionalLifecycle) {
		f.initFn = fn
	}
}

// WithStartFunc sets the Start function.
func WithStartFunc(fn func(context.Context) error) FunctionalLifecycleOption {
	return func(f *FunctionalLifecycle) {
		f.startFn = fn
	}
}

// WithStopFunc sets the Stop function.
func WithStopFunc(fn func(context.Context) error) FunctionalLifecycleOption {
	return func(f *FunctionalLifecycle) {
		f.stopFn = fn
	}
}

// WithHealthFunc sets the HealthCheck function.
func WithHealthFunc(fn func(context.Context) interfaces.HealthStatus) FunctionalLifecycleOption {
	return func(f *FunctionalLifecycle) {
		f.healthFn = fn
	}
}

// NewFunctionalLifecycle creates a new FunctionalLifecycle.
func NewFunctionalLifecycle(name string, opts ...FunctionalLifecycleOption) *FunctionalLifecycle {
	f := &FunctionalLifecycle{name: name}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// Init calls the configured init function.
func (f *FunctionalLifecycle) Init(ctx context.Context, config interface{}) error {
	if f.initFn != nil {
		return f.initFn(ctx, config)
	}
	return nil
}

// Start calls the configured start function.
func (f *FunctionalLifecycle) Start(ctx context.Context) error {
	if f.startFn != nil {
		return f.startFn(ctx)
	}
	return nil
}

// Stop calls the configured stop function.
func (f *FunctionalLifecycle) Stop(ctx context.Context) error {
	if f.stopFn != nil {
		return f.stopFn(ctx)
	}
	return nil
}

// HealthCheck calls the configured health function.
func (f *FunctionalLifecycle) HealthCheck(ctx context.Context) interfaces.HealthStatus {
	if f.healthFn != nil {
		return f.healthFn(ctx)
	}
	return interfaces.NewHealthyStatus()
}

// =============================================================================
// Global Lifecycle Manager
// =============================================================================

var (
	globalLifecycleManager     *DefaultLifecycleManager
	globalLifecycleManagerOnce sync.Once
)

// GlobalLifecycleManager returns the global lifecycle manager instance.
func GlobalLifecycleManager() *DefaultLifecycleManager {
	globalLifecycleManagerOnce.Do(func() {
		globalLifecycleManager = NewLifecycleManager()
	})
	return globalLifecycleManager
}
