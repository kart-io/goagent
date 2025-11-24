// Package interfaces provides lifecycle management interfaces.
//
// This file defines unified lifecycle management for all GoAgent components:
//   - Tools, Agents, Middleware, and Plugins all implement common lifecycle hooks
//   - Enables proper initialization, graceful shutdown, and health monitoring
//   - Supports dependency ordering and coordinated startup/shutdown
package interfaces

import (
	"context"
	"time"
)

// =============================================================================
// Core Lifecycle Interface
// =============================================================================

// Lifecycle defines the standard lifecycle hooks for all managed components.
//
// Components implementing this interface can be managed by the LifecycleManager
// for coordinated startup, shutdown, and health monitoring.
//
// Lifecycle phases:
//  1. Init - Initialize resources (one-time setup)
//  2. Start - Begin active operation
//  3. Running - Normal operation (HealthCheck monitors)
//  4. Stop - Graceful shutdown
type Lifecycle interface {
	// Init initializes the component with configuration.
	// This is called once before Start and should set up resources.
	// Config can be nil if no configuration is needed.
	Init(ctx context.Context, config interface{}) error

	// Start begins the component's active operation.
	// Called after Init succeeds. Component should be ready to serve.
	Start(ctx context.Context) error

	// Stop gracefully stops the component.
	// Should release resources and complete pending operations.
	// Context may have a deadline for forced shutdown.
	Stop(ctx context.Context) error

	// HealthCheck returns the component's current health status.
	// Should be non-blocking and return quickly.
	HealthCheck(ctx context.Context) HealthStatus
}

// LifecycleState represents the current state of a lifecycle-managed component.
type LifecycleState string

const (
	// StateUninitialized - Component has not been initialized
	StateUninitialized LifecycleState = "uninitialized"

	// StateInitialized - Init() has completed successfully
	StateInitialized LifecycleState = "initialized"

	// StateStarting - Start() is in progress
	StateStarting LifecycleState = "starting"

	// StateRunning - Component is running normally
	StateRunning LifecycleState = "running"

	// StateStopping - Stop() is in progress
	StateStopping LifecycleState = "stopping"

	// StateStopped - Component has stopped
	StateStopped LifecycleState = "stopped"

	// StateFailed - Component encountered a fatal error
	StateFailed LifecycleState = "failed"
)

// =============================================================================
// Health Status
// =============================================================================

// HealthStatus represents the health of a component.
type HealthStatus struct {
	// State is the overall health state
	State HealthState `json:"state"`

	// Message provides additional context about the health state
	Message string `json:"message,omitempty"`

	// Details contains component-specific health information
	Details map[string]interface{} `json:"details,omitempty"`

	// LastChecked is when this status was determined
	LastChecked time.Time `json:"last_checked"`

	// ComponentName identifies the component
	ComponentName string `json:"component_name,omitempty"`
}

// HealthState represents the health state of a component.
type HealthState string

const (
	// HealthHealthy - Component is fully operational
	HealthHealthy HealthState = "healthy"

	// HealthDegraded - Component is operational but with reduced capability
	HealthDegraded HealthState = "degraded"

	// HealthUnhealthy - Component is not operational
	HealthUnhealthy HealthState = "unhealthy"

	// HealthUnknown - Health status cannot be determined
	HealthUnknown HealthState = "unknown"
)

// IsHealthy returns true if the status indicates healthy operation.
func (h HealthStatus) IsHealthy() bool {
	return h.State == HealthHealthy
}

// IsOperational returns true if the component can serve requests.
func (h HealthStatus) IsOperational() bool {
	return h.State == HealthHealthy || h.State == HealthDegraded
}

// =============================================================================
// Optional Lifecycle Extensions
// =============================================================================

// LifecycleAware is implemented by components that need lifecycle notifications
// but don't implement the full Lifecycle interface.
type LifecycleAware interface {
	// OnInit is called during initialization
	OnInit(ctx context.Context) error

	// OnShutdown is called during shutdown
	OnShutdown(ctx context.Context) error
}

// Reloadable is implemented by components that support configuration reload
// without restart.
type Reloadable interface {
	// Reload reloads configuration without stopping the component.
	// Returns error if reload fails (component continues with old config).
	Reload(ctx context.Context, config interface{}) error
}

// DependencyAware is implemented by components that depend on other components.
type DependencyAware interface {
	// Dependencies returns the names of components this component depends on.
	// The LifecycleManager will ensure dependencies are started first.
	Dependencies() []string
}

// =============================================================================
// Manager Interface
// =============================================================================

// LifecycleManager manages the lifecycle of multiple components.
type LifecycleManager interface {
	// Register adds a component to be managed.
	// Name must be unique. Priority determines startup/shutdown order.
	Register(name string, component Lifecycle, priority int) error

	// Unregister removes a component from management.
	Unregister(name string) error

	// InitAll initializes all registered components in priority order.
	InitAll(ctx context.Context) error

	// StartAll starts all initialized components in priority order.
	StartAll(ctx context.Context) error

	// StopAll stops all running components in reverse priority order.
	StopAll(ctx context.Context) error

	// HealthCheckAll returns aggregated health of all components.
	HealthCheckAll(ctx context.Context) map[string]HealthStatus

	// GetState returns the current state of a component.
	GetState(name string) (LifecycleState, error)

	// WaitForShutdown blocks until all components are stopped.
	WaitForShutdown(ctx context.Context) error
}

// =============================================================================
// Helper Constructors
// =============================================================================

// NewHealthStatus creates a new HealthStatus with the given state.
func NewHealthStatus(state HealthState, message string) HealthStatus {
	return HealthStatus{
		State:       state,
		Message:     message,
		LastChecked: time.Now(),
	}
}

// NewHealthyStatus creates a healthy status.
func NewHealthyStatus() HealthStatus {
	return NewHealthStatus(HealthHealthy, "operational")
}

// NewUnhealthyStatus creates an unhealthy status with an error message.
func NewUnhealthyStatus(err error) HealthStatus {
	msg := "unknown error"
	if err != nil {
		msg = err.Error()
	}
	return NewHealthStatus(HealthUnhealthy, msg)
}

// NewDegradedStatus creates a degraded status with a reason.
func NewDegradedStatus(reason string) HealthStatus {
	return NewHealthStatus(HealthDegraded, reason)
}
