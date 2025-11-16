package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kart-io/goagent/store"
)

// Store is a thread-safe in-memory implementation of store.Store.
//
// Suitable for:
//   - Development and testing
//   - Small-scale deployments
//   - Ephemeral data that doesn't need persistence
type Store struct {
	// data maps namespace path to key-value pairs
	data map[string]map[string]*store.Value
	mu   sync.RWMutex
}

// New creates a new in-memory store.
func New() *Store {
	return &Store{
		data: make(map[string]map[string]*store.Value),
	}
}

// Put stores a value with the given namespace and key.
func (s *Store) Put(ctx context.Context, namespace []string, key string, value interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	nsKey := namespaceToKey(namespace)
	if s.data[nsKey] == nil {
		s.data[nsKey] = make(map[string]*store.Value)
	}

	now := time.Now()
	existing := s.data[nsKey][key]

	storeValue := &store.Value{
		Value:     value,
		Metadata:  make(map[string]interface{}),
		Updated:   now,
		Namespace: namespace,
		Key:       key,
	}

	if existing != nil {
		storeValue.Created = existing.Created
		// Preserve existing metadata
		for k, v := range existing.Metadata {
			storeValue.Metadata[k] = v
		}
	} else {
		storeValue.Created = now
	}

	s.data[nsKey][key] = storeValue
	return nil
}

// Get retrieves a value by namespace and key.
func (s *Store) Get(ctx context.Context, namespace []string, key string) (*store.Value, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nsKey := namespaceToKey(namespace)
	if s.data[nsKey] == nil {
		return nil, fmt.Errorf("namespace not found: %v", namespace)
	}

	value, ok := s.data[nsKey][key]
	if !ok {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	return value, nil
}

// Delete removes a value by namespace and key.
func (s *Store) Delete(ctx context.Context, namespace []string, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	nsKey := namespaceToKey(namespace)
	if s.data[nsKey] == nil {
		return nil
	}

	delete(s.data[nsKey], key)

	// Clean up empty namespace
	if len(s.data[nsKey]) == 0 {
		delete(s.data, nsKey)
	}

	return nil
}

// Search finds values matching the filter within a namespace.
func (s *Store) Search(ctx context.Context, namespace []string, filter map[string]interface{}) ([]*store.Value, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nsKey := namespaceToKey(namespace)
	if s.data[nsKey] == nil {
		return []*store.Value{}, nil
	}

	results := make([]*store.Value, 0)

	for _, value := range s.data[nsKey] {
		if matchesFilter(value, filter) {
			results = append(results, value)
		}
	}

	return results, nil
}

// List returns all keys within a namespace.
func (s *Store) List(ctx context.Context, namespace []string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nsKey := namespaceToKey(namespace)
	if s.data[nsKey] == nil {
		return []string{}, nil
	}

	keys := make([]string, 0, len(s.data[nsKey]))
	for key := range s.data[nsKey] {
		keys = append(keys, key)
	}

	return keys, nil
}

// Clear removes all values within a namespace.
func (s *Store) Clear(ctx context.Context, namespace []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	nsKey := namespaceToKey(namespace)
	delete(s.data, nsKey)

	return nil
}

// Size returns the total number of values across all namespaces.
func (s *Store) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := 0
	for _, ns := range s.data {
		total += len(ns)
	}
	return total
}

// Namespaces returns all namespace keys currently in the store.
func (s *Store) Namespaces() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	namespaces := make([]string, 0, len(s.data))
	for ns := range s.data {
		namespaces = append(namespaces, ns)
	}
	return namespaces
}

// namespaceToKey converts a namespace slice to a string key.
func namespaceToKey(namespace []string) string {
	if len(namespace) == 0 {
		return "/"
	}
	return "/" + joinNamespace(namespace)
}

// joinNamespace joins namespace components with "/".
func joinNamespace(namespace []string) string {
	if len(namespace) == 0 {
		return ""
	}
	result := namespace[0]
	for i := 1; i < len(namespace); i++ {
		result += "/" + namespace[i]
	}
	return result
}

// matchesFilter checks if a store.Value matches the given filter.
func matchesFilter(value *store.Value, filter map[string]interface{}) bool {
	if len(filter) == 0 {
		return true
	}

	for key, filterValue := range filter {
		metaValue, ok := value.Metadata[key]
		if !ok {
			return false
		}
		if metaValue != filterValue {
			return false
		}
	}

	return true
}
