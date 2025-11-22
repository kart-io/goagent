package distributed

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCircuitBreaker_NewCircuitBreaker tests circuit breaker creation
func TestCircuitBreaker_NewCircuitBreaker(t *testing.T) {
	tests := []struct {
		name           string
		config         *CircuitBreakerConfig
		expectedMaxFail uint32
		expectedTimeout time.Duration
	}{
		{
			name:            "with nil config uses defaults",
			config:          nil,
			expectedMaxFail: 5,
			expectedTimeout: 60 * time.Second,
		},
		{
			name: "with custom config",
			config: &CircuitBreakerConfig{
				MaxFailures: 3,
				Timeout:     30 * time.Second,
			},
			expectedMaxFail: 3,
			expectedTimeout: 30 * time.Second,
		},
		{
			name: "with zero values uses defaults",
			config: &CircuitBreakerConfig{
				MaxFailures: 0,
				Timeout:     0,
			},
			expectedMaxFail: 5,
			expectedTimeout: 60 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := NewCircuitBreaker(tt.config)

			assert.NotNil(t, cb)
			assert.Equal(t, StateClosed, cb.State())
			assert.Equal(t, uint32(0), cb.Failures())
			assert.Equal(t, tt.expectedMaxFail, cb.config.MaxFailures)
			assert.Equal(t, tt.expectedTimeout, cb.config.Timeout)
		})
	}
}

// TestCircuitBreaker_ClosedState tests behavior in closed state
func TestCircuitBreaker_ClosedState(t *testing.T) {
	t.Run("allows all requests", func(t *testing.T) {
		cb := NewCircuitBreaker(&CircuitBreakerConfig{
			MaxFailures: 3,
			Timeout:     1 * time.Second,
		})

		// Execute multiple successful requests
		for i := 0; i < 10; i++ {
			err := cb.Execute(func() error {
				return nil
			})
			assert.NoError(t, err)
			assert.Equal(t, StateClosed, cb.State())
			assert.Equal(t, uint32(0), cb.Failures())
		}
	})

	t.Run("counts failures", func(t *testing.T) {
		cb := NewCircuitBreaker(&CircuitBreakerConfig{
			MaxFailures: 5,
			Timeout:     1 * time.Second,
		})

		testErr := errors.New("test error")

		// Execute failing requests
		for i := uint32(1); i <= 4; i++ {
			err := cb.Execute(func() error {
				return testErr
			})
			assert.Equal(t, testErr, err)
			assert.Equal(t, StateClosed, cb.State())
			assert.Equal(t, i, cb.Failures())
		}
	})

	t.Run("opens circuit after max failures", func(t *testing.T) {
		cb := NewCircuitBreaker(&CircuitBreakerConfig{
			MaxFailures: 3,
			Timeout:     1 * time.Second,
		})

		testErr := errors.New("test error")

		// Execute failing requests until circuit opens
		for i := 0; i < 3; i++ {
			err := cb.Execute(func() error {
				return testErr
			})
			assert.Equal(t, testErr, err)
		}

		// Circuit should now be open
		assert.Equal(t, StateOpen, cb.State())
		assert.Equal(t, uint32(3), cb.Failures())
	})

	t.Run("resets failure count on success", func(t *testing.T) {
		cb := NewCircuitBreaker(&CircuitBreakerConfig{
			MaxFailures: 5,
			Timeout:     1 * time.Second,
		})

		testErr := errors.New("test error")

		// Execute some failing requests
		for i := 0; i < 3; i++ {
			err := cb.Execute(func() error {
				return testErr
			})
			assert.Equal(t, testErr, err)
		}
		assert.Equal(t, uint32(3), cb.Failures())

		// Execute a successful request
		err := cb.Execute(func() error {
			return nil
		})
		assert.NoError(t, err)
		assert.Equal(t, uint32(0), cb.Failures())
		assert.Equal(t, StateClosed, cb.State())
	})
}

// TestCircuitBreaker_OpenState tests behavior in open state
func TestCircuitBreaker_OpenState(t *testing.T) {
	t.Run("blocks all requests", func(t *testing.T) {
		cb := NewCircuitBreaker(&CircuitBreakerConfig{
			MaxFailures: 2,
			Timeout:     1 * time.Second,
		})

		// Force circuit to open
		testErr := errors.New("test error")
		for i := 0; i < 2; i++ {
			cb.Execute(func() error {
				return testErr
			})
		}
		assert.Equal(t, StateOpen, cb.State())

		// Try to execute a request
		executedCount := 0
		err := cb.Execute(func() error {
			executedCount++
			return nil
		})

		assert.Equal(t, ErrCircuitOpen, err)
		assert.Equal(t, 0, executedCount, "function should not be executed when circuit is open")
		assert.Equal(t, StateOpen, cb.State())
	})

	t.Run("transitions to half-open after timeout", func(t *testing.T) {
		cb := NewCircuitBreaker(&CircuitBreakerConfig{
			MaxFailures: 2,
			Timeout:     100 * time.Millisecond,
		})

		// Force circuit to open
		testErr := errors.New("test error")
		for i := 0; i < 2; i++ {
			cb.Execute(func() error {
				return testErr
			})
		}
		assert.Equal(t, StateOpen, cb.State())

		// Wait for timeout
		time.Sleep(150 * time.Millisecond)

		// Next request should transition to half-open
		executedCount := 0
		err := cb.Execute(func() error {
			executedCount++
			return nil
		})

		assert.NoError(t, err)
		assert.Equal(t, 1, executedCount, "function should be executed in half-open state")
		assert.Equal(t, StateClosed, cb.State(), "should transition to closed on success")
	})

	t.Run("does not transition before timeout", func(t *testing.T) {
		cb := NewCircuitBreaker(&CircuitBreakerConfig{
			MaxFailures: 2,
			Timeout:     1 * time.Second,
		})

		// Force circuit to open
		testErr := errors.New("test error")
		for i := 0; i < 2; i++ {
			cb.Execute(func() error {
				return testErr
			})
		}
		assert.Equal(t, StateOpen, cb.State())

		// Try immediately (before timeout)
		time.Sleep(10 * time.Millisecond)
		err := cb.Execute(func() error {
			return nil
		})

		assert.Equal(t, ErrCircuitOpen, err)
		assert.Equal(t, StateOpen, cb.State())
	})
}

// TestCircuitBreaker_HalfOpenState tests behavior in half-open state
func TestCircuitBreaker_HalfOpenState(t *testing.T) {
	t.Run("closes circuit on success", func(t *testing.T) {
		cb := NewCircuitBreaker(&CircuitBreakerConfig{
			MaxFailures: 2,
			Timeout:     100 * time.Millisecond,
		})

		// Force circuit to open
		testErr := errors.New("test error")
		for i := 0; i < 2; i++ {
			cb.Execute(func() error {
				return testErr
			})
		}
		assert.Equal(t, StateOpen, cb.State())

		// Wait for timeout
		time.Sleep(150 * time.Millisecond)

		// Execute successful request in half-open state
		err := cb.Execute(func() error {
			return nil
		})

		assert.NoError(t, err)
		assert.Equal(t, StateClosed, cb.State())
		assert.Equal(t, uint32(0), cb.Failures())
	})

	t.Run("reopens circuit on failure", func(t *testing.T) {
		cb := NewCircuitBreaker(&CircuitBreakerConfig{
			MaxFailures: 2,
			Timeout:     100 * time.Millisecond,
		})

		// Force circuit to open
		testErr := errors.New("test error")
		for i := 0; i < 2; i++ {
			cb.Execute(func() error {
				return testErr
			})
		}
		assert.Equal(t, StateOpen, cb.State())

		// Wait for timeout
		time.Sleep(150 * time.Millisecond)

		// Execute failing request in half-open state
		err := cb.Execute(func() error {
			return testErr
		})

		assert.Equal(t, testErr, err)
		assert.Equal(t, StateOpen, cb.State())
	})
}

// TestCircuitBreaker_StateString tests state string representation
func TestCircuitBreaker_StateString(t *testing.T) {
	tests := []struct {
		state    CircuitState
		expected string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{CircuitState(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}

// TestCircuitBreaker_Reset tests manual reset
func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker(&CircuitBreakerConfig{
		MaxFailures: 2,
		Timeout:     1 * time.Second,
	})

	// Force circuit to open
	testErr := errors.New("test error")
	for i := 0; i < 2; i++ {
		cb.Execute(func() error {
			return testErr
		})
	}
	assert.Equal(t, StateOpen, cb.State())
	assert.Equal(t, uint32(2), cb.Failures())

	// Reset the circuit
	cb.Reset()

	assert.Equal(t, StateClosed, cb.State())
	assert.Equal(t, uint32(0), cb.Failures())

	// Should accept requests now
	err := cb.Execute(func() error {
		return nil
	})
	assert.NoError(t, err)
}

// TestCircuitBreaker_OnStateChange tests state change callback
func TestCircuitBreaker_OnStateChange(t *testing.T) {
	var transitions []string
	var mu sync.Mutex

	cb := NewCircuitBreaker(&CircuitBreakerConfig{
		MaxFailures: 2,
		Timeout:     100 * time.Millisecond,
		OnStateChange: func(from, to CircuitState) {
			mu.Lock()
			defer mu.Unlock()
			transitions = append(transitions, from.String()+"->"+to.String())
		},
	})

	// Trigger state change: closed -> open
	testErr := errors.New("test error")
	for i := 0; i < 2; i++ {
		cb.Execute(func() error {
			return testErr
		})
	}

	// Wait for callback
	time.Sleep(50 * time.Millisecond)

	// Wait for timeout to transition to half-open
	time.Sleep(150 * time.Millisecond)

	// Trigger state change: half-open -> closed (or open->half-open first)
	cb.Execute(func() error {
		return nil
	})

	// Wait for callbacks
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	assert.Contains(t, transitions, "closed->open")
	// Should see either open->half-open->closed or just one transition
	assert.True(t, len(transitions) >= 1)
}

// TestCircuitBreaker_ConcurrentRequests tests thread safety
func TestCircuitBreaker_ConcurrentRequests(t *testing.T) {
	cb := NewCircuitBreaker(&CircuitBreakerConfig{
		MaxFailures: 10,
		Timeout:     1 * time.Second,
	})

	const numGoroutines = 100
	var wg sync.WaitGroup
	var successCount atomic.Int32
	var errorCount atomic.Int32

	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()

			err := cb.Execute(func() error {
				// Simulate some work
				time.Sleep(time.Millisecond)

				// Fail half the requests
				if idx%2 == 0 {
					return errors.New("test error")
				}
				return nil
			})

			if err != nil {
				errorCount.Add(1)
			} else {
				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	// Verify counts
	total := successCount.Load() + errorCount.Load()
	assert.Equal(t, int32(numGoroutines), total)

	// State should still be valid
	state := cb.State()
	assert.True(t, state == StateClosed || state == StateOpen || state == StateHalfOpen)
}

// TestCircuitBreaker_HalfOpenConcurrentRequests tests concurrent requests in half-open state
func TestCircuitBreaker_HalfOpenConcurrentRequests(t *testing.T) {
	cb := NewCircuitBreaker(&CircuitBreakerConfig{
		MaxFailures: 2,
		Timeout:     100 * time.Millisecond,
	})

	// Force circuit to open
	testErr := errors.New("test error")
	for i := 0; i < 2; i++ {
		cb.Execute(func() error {
			return testErr
		})
	}
	require.Equal(t, StateOpen, cb.State())

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Launch concurrent requests
	const numGoroutines = 10
	var wg sync.WaitGroup
	var executedCount atomic.Int32
	var openErrors atomic.Int32

	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()

			err := cb.Execute(func() error {
				executedCount.Add(1)
				time.Sleep(10 * time.Millisecond)
				return nil
			})

			if err == ErrCircuitOpen {
				openErrors.Add(1)
			}
		}()
	}

	wg.Wait()

	// In half-open state, only one request should be allowed through initially
	// Others may be rejected or succeed if the first one closes the circuit
	assert.True(t, executedCount.Load() >= 1, "at least one request should execute")
	assert.Equal(t, StateClosed, cb.State(), "circuit should be closed after successful request")
}

// TestCircuitBreaker_RapidStateTransitions tests rapid state changes
func TestCircuitBreaker_RapidStateTransitions(t *testing.T) {
	cb := NewCircuitBreaker(&CircuitBreakerConfig{
		MaxFailures: 2,
		Timeout:     50 * time.Millisecond,
	})

	testErr := errors.New("test error")

	for cycle := 0; cycle < 5; cycle++ {
		// Closed -> Open
		for i := 0; i < 2; i++ {
			cb.Execute(func() error {
				return testErr
			})
		}
		assert.Equal(t, StateOpen, cb.State())

		// Wait for timeout -> Half-Open -> Closed
		time.Sleep(100 * time.Millisecond)
		err := cb.Execute(func() error {
			return nil
		})
		assert.NoError(t, err)
		assert.Equal(t, StateClosed, cb.State())
	}
}

// TestCircuitBreaker_DefaultConfig tests default configuration
func TestCircuitBreaker_DefaultConfig(t *testing.T) {
	config := DefaultCircuitBreakerConfig()

	assert.Equal(t, uint32(5), config.MaxFailures)
	assert.Equal(t, 60*time.Second, config.Timeout)
	assert.Nil(t, config.OnStateChange)
}

// BenchmarkCircuitBreaker_ClosedState benchmarks performance in closed state
func BenchmarkCircuitBreaker_ClosedState(b *testing.B) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cb.Execute(func() error {
				return nil
			})
		}
	})
}

// BenchmarkCircuitBreaker_OpenState benchmarks performance in open state
func BenchmarkCircuitBreaker_OpenState(b *testing.B) {
	cb := NewCircuitBreaker(&CircuitBreakerConfig{
		MaxFailures: 1,
		Timeout:     10 * time.Minute,
	})

	// Force circuit open
	cb.Execute(func() error {
		return errors.New("test error")
	})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cb.Execute(func() error {
				return nil
			})
		}
	})
}
