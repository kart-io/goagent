package mocks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kart-io/goagent/core/state"
	"github.com/kart-io/goagent/store"
)

// MockState provides a mock implementation of the State interface
type MockState struct {
	mu         sync.RWMutex
	data       map[string]interface{}
	snapshots  []map[string]interface{}
	getCount   int
	setCount   int
	updateFunc func(key string, value interface{}) error
}

// NewMockState creates a new mock state
func NewMockState() *MockState {
	return &MockState{
		data:      make(map[string]interface{}),
		snapshots: []map[string]interface{}{},
	}
}

// Get retrieves a value
func (m *MockState) Get(key string) (interface{}, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.getCount++
	val, exists := m.data[key]
	return val, exists
}

// Set sets a value
func (m *MockState) Set(key string, value interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setCount++
	m.data[key] = value
}

// Update batch updates values
func (m *MockState) Update(updates map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range updates {
		m.data[k] = v
		if m.updateFunc != nil {
			m.updateFunc(k, v)
		}
	}
}

// Delete removes a key
func (m *MockState) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
}

// Clear removes all keys
func (m *MockState) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = make(map[string]interface{})
}

// Snapshot creates a snapshot
func (m *MockState) Snapshot() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snapshot := make(map[string]interface{})
	for k, v := range m.data {
		snapshot[k] = v
	}
	m.snapshots = append(m.snapshots, snapshot)
	return snapshot
}

// Restore restores from a snapshot
func (m *MockState) Restore(snapshot map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = make(map[string]interface{})
	for k, v := range snapshot {
		m.data[k] = v
	}
}

// Clone creates a copy
func (m *MockState) Clone() state.State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	newState := NewMockState()
	for k, v := range m.data {
		newState.data[k] = v
	}
	return newState
}

// Keys returns all keys
func (m *MockState) Keys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys
}

// Size returns the number of keys
func (m *MockState) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data)
}

// GetString gets a string value
func (m *MockState) GetString(key string) (string, bool) {
	val, exists := m.Get(key)
	if !exists {
		return "", false
	}
	str, ok := val.(string)
	return str, ok
}

// GetInt gets an int value
func (m *MockState) GetInt(key string) (int, bool) {
	val, exists := m.Get(key)
	if !exists {
		return 0, false
	}
	num, ok := val.(int)
	return num, ok
}

// GetBool gets a bool value
func (m *MockState) GetBool(key string) (bool, bool) {
	val, exists := m.Get(key)
	if !exists {
		return false, false
	}
	b, ok := val.(bool)
	return b, ok
}

// GetFloat64 gets a float64 value
func (m *MockState) GetFloat64(key string) (float64, bool) {
	val, exists := m.Get(key)
	if !exists {
		return 0, false
	}
	f, ok := val.(float64)
	return f, ok
}

// GetTime gets a time value
func (m *MockState) GetTime(key string) (time.Time, bool) {
	val, exists := m.Get(key)
	if !exists {
		return time.Time{}, false
	}
	t, ok := val.(time.Time)
	return t, ok
}

// SetUpdateFunc sets a function to call on updates
func (m *MockState) SetUpdateFunc(fn func(key string, value interface{}) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateFunc = fn
}

// GetStats returns statistics
func (m *MockState) GetStats() (getCount, setCount int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getCount, m.setCount
}

// MockStore provides a mock implementation of the Store interface
type MockStore struct {
	mu           sync.RWMutex
	data         map[string]map[string]*store.Value
	namespaces   []string
	putCount     int
	getCount     int
	shouldError  bool
	errorMessage string
}

// NewMockStore creates a new mock store
func NewMockStore() *MockStore {
	return &MockStore{
		data: make(map[string]map[string]*store.Value),
	}
}

// Put stores an item
func (s *MockStore) Put(ctx context.Context, namespace []string, key string, value interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.shouldError {
		return fmt.Errorf("%s", s.errorMessage)
	}

	s.putCount++
	ns := joinNamespace(namespace)

	if _, exists := s.data[ns]; !exists {
		s.data[ns] = make(map[string]*store.Value)
		s.namespaces = append(s.namespaces, ns)
	}

	now := time.Now()
	item := &store.Value{
		Key:       key,
		Value:     value,
		Namespace: namespace,
		Created:   now,
		Updated:   now,
		Metadata:  make(map[string]interface{}),
	}

	if existing, exists := s.data[ns][key]; exists {
		item.Created = existing.Created
	}

	s.data[ns][key] = item
	return nil
}

// Get retrieves an item
func (s *MockStore) Get(ctx context.Context, namespace []string, key string) (*store.Value, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.shouldError {
		return nil, fmt.Errorf("%s", s.errorMessage)
	}

	s.getCount++
	ns := joinNamespace(namespace)

	if nsData, exists := s.data[ns]; exists {
		if item, exists := nsData[key]; exists {
			return item, nil
		}
	}

	return nil, fmt.Errorf("key not found: %s", key)
}

// Delete removes an item
func (s *MockStore) Delete(ctx context.Context, namespace []string, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.shouldError {
		return fmt.Errorf("%s", s.errorMessage)
	}

	ns := joinNamespace(namespace)

	if nsData, exists := s.data[ns]; exists {
		delete(nsData, key)
		if len(nsData) == 0 {
			delete(s.data, ns)
			// Remove from namespaces
			newNs := []string{}
			for _, n := range s.namespaces {
				if n != ns {
					newNs = append(newNs, n)
				}
			}
			s.namespaces = newNs
		}
	}

	return nil
}

// List lists all keys in a namespace
func (s *MockStore) List(ctx context.Context, namespace []string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.shouldError {
		return nil, fmt.Errorf("%s", s.errorMessage)
	}

	ns := joinNamespace(namespace)

	if nsData, exists := s.data[ns]; exists {
		keys := make([]string, 0, len(nsData))
		for k := range nsData {
			keys = append(keys, k)
		}
		return keys, nil
	}

	return []string{}, nil
}

// Search searches for items
func (s *MockStore) Search(ctx context.Context, namespace []string, filter map[string]interface{}) ([]*store.Value, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.shouldError {
		return nil, fmt.Errorf("%s", s.errorMessage)
	}

	ns := joinNamespace(namespace)
	results := []*store.Value{}

	if nsData, exists := s.data[ns]; exists {
		// Get prefix filter if it exists
		prefix, _ := filter["prefix"].(string)

		for _, item := range nsData {
			// Simple prefix matching
			if prefix != "" && len(item.Key) >= len(prefix) {
				if item.Key[:len(prefix)] == prefix {
					results = append(results, item)
				}
			} else if prefix == "" {
				results = append(results, item)
			}
		}
	}

	// Apply limit if specified
	if limitVal, ok := filter["limit"].(int); ok && limitVal > 0 && len(results) > limitVal {
		results = results[:limitVal]
	}

	return results, nil
}

// Clear removes all items
func (s *MockStore) Clear(ctx context.Context, namespace []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.shouldError {
		return fmt.Errorf("%s", s.errorMessage)
	}

	ns := joinNamespace(namespace)
	delete(s.data, ns)

	// Remove from namespaces list
	newNs := []string{}
	for _, n := range s.namespaces {
		if n != ns {
			newNs = append(newNs, n)
		}
	}
	s.namespaces = newNs

	return nil
}

// Size returns the total number of items
func (s *MockStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, nsData := range s.data {
		count += len(nsData)
	}
	return count
}

// Namespaces returns all namespaces
func (s *MockStore) Namespaces() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.namespaces
}

// SetError configures the store to return errors
func (s *MockStore) SetError(shouldError bool, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shouldError = shouldError
	s.errorMessage = message
}

// GetStats returns statistics
func (s *MockStore) GetStats() (putCount, getCount int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.putCount, s.getCount
}

// joinNamespace joins namespace components
func joinNamespace(namespace []string) string {
	result := ""
	for i, ns := range namespace {
		if i > 0 {
			result += "/"
		}
		result += ns
	}
	return result
}
