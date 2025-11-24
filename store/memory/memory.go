package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/store"
)

// valuePool is a sync.Pool for store.Value objects to reduce allocations
var valuePool = sync.Pool{
	New: func() interface{} {
		return &store.Value{
			Metadata: make(map[string]interface{}),
		}
	},
}

// Store is a thread-safe in-memory implementation of store.Store.
//
// Suitable for:
//   - Development and testing
//   - Small-scale deployments
//   - Ephemeral data that doesn't need persistence
//
// Performance optimizations:
//   - Uses sync.RWMutex for efficient read-heavy workloads
//   - Object pooling with sync.Pool to reduce GC pressure
//   - Optimized lock granularity to minimize write lock duration
//   - Inverted index for O(1) metadata-based search queries
type Store struct {
	// data maps namespace path to key-value pairs
	data map[string]map[string]*store.Value

	// index provides inverted index for fast metadata searches
	// Structure: namespace -> metadataKey -> metadataValue -> set of store.Value pointers
	index map[string]map[string]map[interface{}]map[*store.Value]struct{}

	mu sync.RWMutex
}

// New creates a new in-memory store.
func New() *Store {
	return &Store{
		data:  make(map[string]map[string]*store.Value),
		index: make(map[string]map[string]map[interface{}]map[*store.Value]struct{}),
	}
}

// Put stores a value with the given namespace and key.
// Optimizations:
//   - Uses object pool to reduce allocations
//   - Minimizes write lock duration by preparing data outside critical section
func (s *Store) Put(ctx context.Context, namespace []string, key string, value interface{}) error {
	nsKey := namespaceToKey(namespace)
	now := time.Now()

	// Get object from pool
	storeValue := valuePool.Get().(*store.Value)
	storeValue.Value = value
	storeValue.Updated = now
	storeValue.Namespace = namespace
	storeValue.Key = key

	// First try with read lock to check if we're updating existing value
	s.mu.RLock()
	existing := s.data[nsKey]
	var existingValue *store.Value
	if existing != nil {
		existingValue = existing[key]
	}
	s.mu.RUnlock()

	// Prepare metadata outside write lock
	if existingValue != nil {
		storeValue.Created = existingValue.Created
		// Copy existing metadata
		for k, v := range existingValue.Metadata {
			storeValue.Metadata[k] = v
		}
	} else {
		storeValue.Created = now
		// Clear metadata from pool if it has any (使用 Go 1.21+ clear() 提高性能)
		if len(storeValue.Metadata) > 0 {
			clear(storeValue.Metadata)
		}
	}

	// Now acquire write lock only for the actual update
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data[nsKey] == nil {
		s.data[nsKey] = make(map[string]*store.Value)
	}

	// Return old value to pool and remove from index if it exists
	if oldValue := s.data[nsKey][key]; oldValue != nil && oldValue != existingValue {
		s.removeFromIndex(nsKey, oldValue)
		returnValueToPool(oldValue)
	}

	s.data[nsKey][key] = storeValue

	// Update inverted index for this value
	s.addToIndex(nsKey, storeValue)

	return nil
}

// Get retrieves a value by namespace and key.
func (s *Store) Get(ctx context.Context, namespace []string, key string) (*store.Value, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nsKey := namespaceToKey(namespace)
	if s.data[nsKey] == nil {
		return nil, agentErrors.New(agentErrors.CodeStoreNotFound, "namespace not found").
			WithComponent("store").
			WithOperation("get").
			WithContext("namespace", fmt.Sprintf("%v", namespace))
	}

	value, ok := s.data[nsKey][key]
	if !ok {
		return nil, agentErrors.NewStoreNotFoundError(namespace, key)
	}

	return value, nil
}

// Delete removes a value by namespace and key.
// Returns the old value to the object pool.
func (s *Store) Delete(ctx context.Context, namespace []string, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	nsKey := namespaceToKey(namespace)
	if s.data[nsKey] == nil {
		return nil
	}

	// Return old value to pool before deleting
	if oldValue := s.data[nsKey][key]; oldValue != nil {
		// Remove from index first
		s.removeFromIndex(nsKey, oldValue)
		returnValueToPool(oldValue)
	}

	delete(s.data[nsKey], key)

	// Clean up empty namespace
	if len(s.data[nsKey]) == 0 {
		delete(s.data, nsKey)
	}

	return nil
}

// Search finds values matching the filter within a namespace.
// Optimization: Uses inverted index for O(1) lookup when filter is provided,
// falling back to linear scan only when no filter is specified.
func (s *Store) Search(ctx context.Context, namespace []string, filter map[string]interface{}) ([]*store.Value, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nsKey := namespaceToKey(namespace)
	if s.data[nsKey] == nil {
		return []*store.Value{}, nil
	}

	// If no filter, return all values (fast path)
	if len(filter) == 0 {
		nsData := s.data[nsKey]
		results := make([]*store.Value, 0, len(nsData))
		for _, value := range nsData {
			results = append(results, value)
		}
		return results, nil
	}

	// Use inverted index for fast lookup when filter is provided
	return s.searchWithIndex(nsKey, filter), nil
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
// Optimized to use strings.Join instead of string concatenation to reduce allocations.
func joinNamespace(namespace []string) string {
	if len(namespace) == 0 {
		return ""
	}
	return strings.Join(namespace, "/")
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

// returnValueToPool returns a store.Value object to the pool after clearing it.
// This reduces GC pressure by reusing objects.
func returnValueToPool(v *store.Value) {
	if v == nil {
		return
	}

	// Clear the value to prevent memory leaks
	v.Value = nil
	v.Namespace = nil
	v.Key = ""

	// Clear metadata map but keep it for reuse (使用 Go 1.21+ clear() 提高性能)
	if len(v.Metadata) > 0 {
		clear(v.Metadata)
	}

	// Return to pool
	valuePool.Put(v)
}

// addToIndex adds a value to the inverted index.
// Must be called while holding write lock.
func (s *Store) addToIndex(nsKey string, value *store.Value) {
	if s.index[nsKey] == nil {
		s.index[nsKey] = make(map[string]map[interface{}]map[*store.Value]struct{})
	}

	// Index each metadata field
	for metaKey, metaValue := range value.Metadata {
		if s.index[nsKey][metaKey] == nil {
			s.index[nsKey][metaKey] = make(map[interface{}]map[*store.Value]struct{})
		}
		if s.index[nsKey][metaKey][metaValue] == nil {
			s.index[nsKey][metaKey][metaValue] = make(map[*store.Value]struct{})
		}
		s.index[nsKey][metaKey][metaValue][value] = struct{}{}
	}
}

// removeFromIndex removes a value from the inverted index.
// Must be called while holding write lock.
func (s *Store) removeFromIndex(nsKey string, value *store.Value) {
	if s.index[nsKey] == nil {
		return
	}

	// Remove from each metadata index
	for metaKey, metaValue := range value.Metadata {
		if s.index[nsKey][metaKey] != nil && s.index[nsKey][metaKey][metaValue] != nil {
			delete(s.index[nsKey][metaKey][metaValue], value)

			// Clean up empty maps
			if len(s.index[nsKey][metaKey][metaValue]) == 0 {
				delete(s.index[nsKey][metaKey], metaValue)
			}
			if len(s.index[nsKey][metaKey]) == 0 {
				delete(s.index[nsKey], metaKey)
			}
		}
	}

	// Clean up empty namespace index
	if len(s.index[nsKey]) == 0 {
		delete(s.index, nsKey)
	}
}

// searchWithIndex performs indexed search for values matching all filter criteria.
// Time complexity: O(min(result_set_size)) instead of O(N).
// Must be called while holding read lock.
func (s *Store) searchWithIndex(nsKey string, filter map[string]interface{}) []*store.Value {
	if s.index[nsKey] == nil {
		return []*store.Value{}
	}

	// Find the smallest candidate set by checking index for first filter key
	var candidates map[*store.Value]struct{}
	var firstKey string

	for filterKey, filterValue := range filter {
		if s.index[nsKey][filterKey] != nil && s.index[nsKey][filterKey][filterValue] != nil {
			candidateSet := s.index[nsKey][filterKey][filterValue]
			if candidates == nil || len(candidateSet) < len(candidates) {
				candidates = candidateSet
				firstKey = filterKey
			}
		} else {
			// If any filter key has no matches, return empty result
			return []*store.Value{}
		}
	}

	if candidates == nil {
		return []*store.Value{}
	}

	// If only one filter, return all candidates directly
	if len(filter) == 1 {
		results := make([]*store.Value, 0, len(candidates))
		for value := range candidates {
			results = append(results, value)
		}
		return results
	}

	// For multiple filters, intersect candidate sets
	results := make([]*store.Value, 0, len(candidates))
	for value := range candidates {
		// Check if value matches all other filter criteria
		allMatch := true
		for filterKey, filterValue := range filter {
			if filterKey == firstKey {
				continue // Already verified
			}
			if value.Metadata[filterKey] != filterValue {
				allMatch = false
				break
			}
		}
		if allMatch {
			results = append(results, value)
		}
	}

	return results
}
