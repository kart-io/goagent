package state

import (
	"fmt"
	"sync"
)

// State defines the interface for Agent state management.
//
// Inspired by LangChain's AgentState design, providing:
//   - Thread-safe state access
//   - State persistence support
//   - State update tracking
//   - State snapshots and cloning
type State interface {
	// Get retrieves a value from state by key.
	// Returns the value and true if found, nil and false if not found.
	Get(key string) (interface{}, bool)

	// Set sets a value in state by key.
	Set(key string, value interface{})

	// Update performs a batch update of multiple state values.
	Update(updates map[string]interface{})

	// Snapshot returns a copy of all state values at this moment.
	// The returned map is a new copy and can be safely modified.
	Snapshot() map[string]interface{}

	// Clone creates a deep copy of the state.
	Clone() State

	// Delete removes a value from state by key.
	Delete(key string)

	// Clear removes all values from state.
	Clear()

	// Keys returns all keys currently in state.
	Keys() []string

	// Size returns the number of values in state.
	Size() int
}

// AgentState is a thread-safe implementation of the State interface.
//
// It uses sync.RWMutex to ensure safe concurrent access from multiple goroutines.
// This is critical for Agent systems where multiple tools or middleware may access
// state simultaneously.
type AgentState struct {
	state map[string]interface{}
	mu    sync.RWMutex
}

// NewAgentState creates a new empty AgentState.
func NewAgentState() *AgentState {
	return &AgentState{
		state: make(map[string]interface{}),
	}
}

// NewAgentStateWithData creates a new AgentState initialized with the given data.
func NewAgentStateWithData(data map[string]interface{}) *AgentState {
	state := &AgentState{
		state: make(map[string]interface{}, len(data)),
	}
	for k, v := range data {
		state.state[k] = v
	}
	return state
}

// Get retrieves a value from state by key.
func (s *AgentState) Get(key string) (interface{}, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.state[key]
	return val, ok
}

// Set sets a value in state by key.
func (s *AgentState) Set(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state[key] = value
}

// Update performs a batch update of multiple state values.
func (s *AgentState) Update(updates map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range updates {
		s.state[k] = v
	}
}

// Snapshot returns a copy of all state values at this moment.
func (s *AgentState) Snapshot() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := make(map[string]interface{}, len(s.state))
	for k, v := range s.state {
		snapshot[k] = v
	}
	return snapshot
}

// Clone creates a deep copy of the state.
func (s *AgentState) Clone() State {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cloned := &AgentState{
		state: make(map[string]interface{}, len(s.state)),
	}
	for k, v := range s.state {
		cloned.state[k] = v
	}
	return cloned
}

// Delete removes a value from state by key.
func (s *AgentState) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state, key)
}

// Clear removes all values from state.
func (s *AgentState) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = make(map[string]interface{})
}

// Keys returns all keys currently in state.
func (s *AgentState) Keys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := make([]string, 0, len(s.state))
	for k := range s.state {
		keys = append(keys, k)
	}
	return keys
}

// Size returns the number of values in state.
func (s *AgentState) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.state)
}

// String returns a string representation of the state for debugging.
func (s *AgentState) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("AgentState{size: %d, keys: %v}", len(s.state), s.Keys())
}

// GetString retrieves a string value from state by key.
// Returns empty string and false if the key doesn't exist or value is not a string.
func (s *AgentState) GetString(key string) (string, bool) {
	val, ok := s.Get(key)
	if !ok {
		return "", false
	}
	str, ok := val.(string)
	return str, ok
}

// GetInt retrieves an int value from state by key.
// Returns 0 and false if the key doesn't exist or value is not an int.
func (s *AgentState) GetInt(key string) (int, bool) {
	val, ok := s.Get(key)
	if !ok {
		return 0, false
	}
	i, ok := val.(int)
	return i, ok
}

// GetBool retrieves a bool value from state by key.
// Returns false and false if the key doesn't exist or value is not a bool.
func (s *AgentState) GetBool(key string) (bool, bool) {
	val, ok := s.Get(key)
	if !ok {
		return false, false
	}
	b, ok := val.(bool)
	return b, ok
}

// GetFloat64 retrieves a float64 value from state by key.
// Returns 0.0 and false if the key doesn't exist or value is not a float64.
func (s *AgentState) GetFloat64(key string) (float64, bool) {
	val, ok := s.Get(key)
	if !ok {
		return 0.0, false
	}
	f, ok := val.(float64)
	return f, ok
}

// GetMap retrieves a map value from state by key.
// Returns nil and false if the key doesn't exist or value is not a map.
func (s *AgentState) GetMap(key string) (map[string]interface{}, bool) {
	val, ok := s.Get(key)
	if !ok {
		return nil, false
	}
	m, ok := val.(map[string]interface{})
	return m, ok
}

// GetSlice retrieves a slice value from state by key.
// Returns nil and false if the key doesn't exist or value is not a slice.
func (s *AgentState) GetSlice(key string) ([]interface{}, bool) {
	val, ok := s.Get(key)
	if !ok {
		return nil, false
	}
	slice, ok := val.([]interface{})
	return slice, ok
}
