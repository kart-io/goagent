package distributed

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// CircuitState represents the current state of the circuit breaker
type CircuitState int32

const (
	// StateClosed allows all requests through
	StateClosed CircuitState = iota
	// StateOpen blocks all requests
	StateOpen
	// StateHalfOpen allows a single test request through
	StateHalfOpen
)

// String returns the string representation of the circuit state
func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig holds configuration for the circuit breaker
type CircuitBreakerConfig struct {
	// MaxFailures is the number of consecutive failures before opening the circuit
	MaxFailures uint32
	// Timeout is the duration to wait before transitioning from open to half-open
	Timeout time.Duration
	// OnStateChange is called when the circuit state changes (optional)
	OnStateChange func(from, to CircuitState)
}

// DefaultCircuitBreakerConfig returns a default configuration
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		MaxFailures: 5,
		Timeout:     60 * time.Second,
	}
}

// CircuitBreaker implements the circuit breaker pattern to prevent cascading failures
type CircuitBreaker struct {
	config *CircuitBreakerConfig

	// Atomic state management
	state        atomic.Int32 // CircuitState
	failures     atomic.Uint32
	lastFailTime atomic.Int64 // Unix nanoseconds

	// Mutex for state transitions
	mu sync.RWMutex
}

var (
	// ErrCircuitOpen is returned when the circuit is open
	ErrCircuitOpen = errors.New("circuit breaker is open")
	// ErrTooManyRequests is returned when too many requests are attempted in half-open state
	ErrTooManyRequests = errors.New("too many requests in half-open state")
)

// NewCircuitBreaker creates a new circuit breaker with the given configuration
func NewCircuitBreaker(config *CircuitBreakerConfig) *CircuitBreaker {
	if config == nil {
		config = DefaultCircuitBreakerConfig()
	}

	// Validate configuration
	if config.MaxFailures == 0 {
		config.MaxFailures = 5
	}
	if config.Timeout == 0 {
		config.Timeout = 60 * time.Second
	}

	cb := &CircuitBreaker{
		config: config,
	}
	cb.state.Store(int32(StateClosed))
	cb.failures.Store(0)
	cb.lastFailTime.Store(0)

	return cb
}

// Execute runs the given function through the circuit breaker
func (cb *CircuitBreaker) Execute(fn func() error) error {
	// Check if we can execute
	if err := cb.beforeRequest(); err != nil {
		return err
	}

	// Execute the function
	err := fn()

	// Record the result
	cb.afterRequest(err)

	return err
}

// beforeRequest checks if the request can proceed
func (cb *CircuitBreaker) beforeRequest() error {
	state := CircuitState(cb.state.Load())

	switch state {
	case StateClosed:
		// Always allow requests in closed state
		return nil

	case StateOpen:
		// Check if timeout has elapsed
		lastFail := time.Unix(0, cb.lastFailTime.Load())
		if time.Since(lastFail) > cb.config.Timeout {
			// Attempt to transition to half-open
			cb.mu.Lock()
			defer cb.mu.Unlock()

			// Double-check state after acquiring lock
			if CircuitState(cb.state.Load()) == StateOpen {
				if time.Since(time.Unix(0, cb.lastFailTime.Load())) > cb.config.Timeout {
					cb.setState(StateHalfOpen)
					return nil
				}
			}
		}
		return ErrCircuitOpen

	case StateHalfOpen:
		// In half-open state, we need to be careful about concurrent requests
		// Only allow the first request through
		cb.mu.Lock()
		defer cb.mu.Unlock()

		// If we're still in half-open, allow this request
		// Subsequent requests will be rejected
		if CircuitState(cb.state.Load()) == StateHalfOpen {
			return nil
		}

		// State changed while we were waiting for the lock
		return cb.beforeRequest()

	default:
		return ErrCircuitOpen
	}
}

// afterRequest records the result of a request
func (cb *CircuitBreaker) afterRequest(err error) {
	state := CircuitState(cb.state.Load())

	if err != nil {
		// Request failed
		cb.onFailure(state)
	} else {
		// Request succeeded
		cb.onSuccess(state)
	}
}

// onFailure handles a failed request
func (cb *CircuitBreaker) onFailure(state CircuitState) {
	cb.lastFailTime.Store(time.Now().UnixNano())
	failures := cb.failures.Add(1)

	switch state {
	case StateClosed:
		// Check if we should open the circuit
		if failures >= cb.config.MaxFailures {
			cb.mu.Lock()
			defer cb.mu.Unlock()

			// Double-check after acquiring lock
			if CircuitState(cb.state.Load()) == StateClosed {
				if cb.failures.Load() >= cb.config.MaxFailures {
					cb.setState(StateOpen)
				}
			}
		}

	case StateHalfOpen:
		// Failed in half-open state, go back to open
		cb.mu.Lock()
		defer cb.mu.Unlock()

		if CircuitState(cb.state.Load()) == StateHalfOpen {
			cb.setState(StateOpen)
		}
	}
}

// onSuccess handles a successful request
func (cb *CircuitBreaker) onSuccess(state CircuitState) {
	switch state {
	case StateClosed:
		// Reset failure count on success in closed state
		cb.failures.Store(0)

	case StateHalfOpen:
		// Success in half-open state, close the circuit
		cb.mu.Lock()
		defer cb.mu.Unlock()

		if CircuitState(cb.state.Load()) == StateHalfOpen {
			cb.failures.Store(0)
			cb.setState(StateClosed)
		}
	}
}

// setState changes the circuit state and notifies listeners
func (cb *CircuitBreaker) setState(newState CircuitState) {
	oldState := CircuitState(cb.state.Swap(int32(newState)))

	if oldState != newState && cb.config.OnStateChange != nil {
		// Call the callback without holding the lock
		go cb.config.OnStateChange(oldState, newState)
	}
}

// State returns the current state of the circuit breaker
func (cb *CircuitBreaker) State() CircuitState {
	return CircuitState(cb.state.Load())
}

// Failures returns the current failure count
func (cb *CircuitBreaker) Failures() uint32 {
	return cb.failures.Load()
}

// Reset resets the circuit breaker to closed state with zero failures
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	oldState := CircuitState(cb.state.Load())
	cb.state.Store(int32(StateClosed))
	cb.failures.Store(0)
	cb.lastFailTime.Store(0)

	if oldState != StateClosed && cb.config.OnStateChange != nil {
		go cb.config.OnStateChange(oldState, StateClosed)
	}
}
