package memory

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/internal/snapshot"
	"github.com/kart-io/goagent/store"
)

// Store is a thread-safe in-memory implementation of store.Store.
//
// Suitable for:
//   - Development and testing
//   - Small-scale deployments
//   - Ephemeral data that doesn't need persistence
//
// Performance optimizations:
//   - Uses sync.RWMutex for efficient read-heavy workloads
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

// Put 保存独立快照，并保留已有元数据。
func (s *Store) Put(ctx context.Context, namespace []string, key string, value interface{}) error {
	return s.PutWithMetadata(ctx, namespace, key, value, nil)
}

// PutWithMetadata 原子合并元数据、替换记录并维护索引；输入均由存储取得独立所有权。
func (s *Store) PutWithMetadata(ctx context.Context, namespace []string, key string, value interface{}, metadata map[string]interface{}) error {
	input, err := snapshot.Clone(&store.Value{Value: value, Namespace: namespace, Metadata: metadata})
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeStoreSerialization, "存储写入快照失败").WithComponent("store").WithOperation("put_with_metadata")
	}
	storeValue := input.(*store.Value)
	nsKey := namespaceToKey(namespace)
	now := time.Now()
	storeValue.Updated = now
	storeValue.Key = key
	storeValue.Created = now
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data[nsKey] == nil {
		s.data[nsKey] = make(map[string]*store.Value)
	}
	merged := make(map[string]interface{})
	if oldValue := s.data[nsKey][key]; oldValue != nil {
		storeValue.Created = oldValue.Created
		for k, v := range oldValue.Metadata {
			merged[k] = v
		}
		s.removeFromIndex(nsKey, oldValue)
	}
	for k, v := range storeValue.Metadata {
		merged[k] = v
	}
	storeValue.Metadata = merged
	s.data[nsKey][key] = storeValue
	s.addToIndex(nsKey, storeValue)
	return nil
}

// Get retrieves a value by namespace and key.
func (s *Store) Get(ctx context.Context, namespace []string, key string) (*store.Value, error) {
	value, err := s.readValue(namespace, key)
	if err != nil {
		return nil, err
	}
	return copyValue(value)
}

func (s *Store) readValue(namespace []string, key string) (*store.Value, error) {
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

func copyValue(value *store.Value) (*store.Value, error) {
	copy, err := snapshot.Clone(value)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeStoreSerialization, "存储记录快照失败").WithComponent("store").WithOperation("snapshot")
	}
	return copy.(*store.Value), nil
}

// Delete removes a value by namespace and key.
func (s *Store) Delete(ctx context.Context, namespace []string, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	nsKey := namespaceToKey(namespace)
	if s.data[nsKey] == nil {
		return nil
	}

	if oldValue := s.data[nsKey][key]; oldValue != nil {
		s.removeFromIndex(nsKey, oldValue)
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
	results := s.searchValues(namespace, filter)
	for i, value := range results {
		copy, err := copyValue(value)
		if err != nil {
			return nil, err
		}
		results[i] = copy
	}
	return results, nil
}

func (s *Store) searchValues(namespace []string, filter map[string]interface{}) []*store.Value {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nsKey := namespaceToKey(namespace)
	if s.data[nsKey] == nil {
		return []*store.Value{}
	}

	// If no filter, return all values (fast path)
	if len(filter) == 0 {
		nsData := s.data[nsKey]
		results := make([]*store.Value, 0, len(nsData))
		for _, value := range nsData {
			results = append(results, value)
		}
		return results
	}

	// Use inverted index for fast lookup when filter is provided
	return s.searchWithIndex(nsKey, filter)
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
	delete(s.index, nsKey)

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
	return store.NamespaceKey(namespace)
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
		if !reflect.DeepEqual(metaValue, filterValue) {
			return false
		}
	}

	return true
}

// addToIndex adds a value to the inverted index.
// Must be called while holding write lock.
func (s *Store) addToIndex(nsKey string, value *store.Value) {
	if s.index[nsKey] == nil {
		s.index[nsKey] = make(map[string]map[interface{}]map[*store.Value]struct{})
	}

	// Index each metadata field
	for metaKey, metaValue := range value.Metadata {
		if !indexable(metaValue) {
			continue
		}
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
		if !indexable(metaValue) {
			continue
		}
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
	// 不可比较值不能作为map键；对这类元数据使用完整相等比较。
	for _, v := range filter {
		if !indexable(v) {
			results := []*store.Value{}
			for _, value := range s.data[nsKey] {
				if matchesFilter(value, filter) {
					results = append(results, value)
				}
			}
			return results
		}
	}
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
			if s.data[nsKey][value.Key] == value {
				results = append(results, value)
			}
		}
		return results
	}

	// For multiple filters, intersect candidate sets
	results := make([]*store.Value, 0, len(candidates))
	for value := range candidates {
		if s.data[nsKey][value.Key] != value {
			continue
		}
		// Check if value matches all other filter criteria
		allMatch := true
		for filterKey, filterValue := range filter {
			if filterKey == firstKey {
				continue // Already verified
			}
			if !reflect.DeepEqual(value.Metadata[filterKey], filterValue) {
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

func indexable(value interface{}) bool {
	if value == nil {
		return true
	}
	switch reflect.TypeOf(value).Kind() {
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return true
	default:
		return false
	}
}
