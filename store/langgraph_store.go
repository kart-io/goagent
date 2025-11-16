package store

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// StoreValue represents a stored value with metadata
type StoreValue struct {
	Value     interface{}            `json:"value"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	TTL       *time.Duration         `json:"ttl,omitempty"`
	Version   int                    `json:"version"`
}

// IsExpired checks if the value has expired
func (v *StoreValue) IsExpired() bool {
	if v.TTL == nil {
		return false
	}
	return time.Since(v.Timestamp) > *v.TTL
}

// LangGraphStore interface for long-term memory
type LangGraphStore interface {
	// Put stores a value at the specified namespace and key
	Put(ctx context.Context, namespace []string, key string, value interface{}) error

	// PutWithTTL stores a value with TTL
	PutWithTTL(ctx context.Context, namespace []string, key string, value interface{}, ttl time.Duration) error

	// Get retrieves a value from the specified namespace and key
	Get(ctx context.Context, namespace []string, key string) (*StoreValue, error)

	// Search performs similarity search within a namespace
	Search(ctx context.Context, namespace []string, query string, limit int) ([]*StoreValue, error)

	// Delete removes a value
	Delete(ctx context.Context, namespace []string, key string) error

	// List returns all keys in a namespace
	List(ctx context.Context, namespace []string) ([]string, error)

	// ListWithPrefix returns keys with a specific prefix
	ListWithPrefix(ctx context.Context, namespace []string, prefix string) ([]string, error)

	// Update atomically updates a value
	Update(ctx context.Context, namespace []string, key string, updateFunc func(*StoreValue) (*StoreValue, error)) error

	// Watch watches for changes in a namespace
	Watch(ctx context.Context, namespace []string) (<-chan StoreEvent, error)

	// Close closes the store
	Close() error
}

// StoreEvent represents a change event in the store
type StoreEvent struct {
	Type      EventType              `json:"type"`
	Namespace []string               `json:"namespace"`
	Key       string                 `json:"key"`
	Value     *StoreValue            `json:"value,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// EventType defines the type of store event
type EventType string

const (
	EventTypePut    EventType = "put"
	EventTypeUpdate EventType = "update"
	EventTypeDelete EventType = "delete"
)

// InMemoryLangGraphStore for development and testing
type InMemoryLangGraphStore struct {
	data      map[string]map[string]*StoreValue
	watchers  map[string][]chan StoreEvent
	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once
}

// NewInMemoryLangGraphStore creates a new in-memory store
func NewInMemoryLangGraphStore() *InMemoryLangGraphStore {
	return &InMemoryLangGraphStore{
		data:     make(map[string]map[string]*StoreValue),
		watchers: make(map[string][]chan StoreEvent),
	}
}

// Put stores a value
func (s *InMemoryLangGraphStore) Put(ctx context.Context, namespace []string, key string, value interface{}) error {
	return s.PutWithTTL(ctx, namespace, key, value, 0)
}

// PutWithTTL stores a value with TTL
func (s *InMemoryLangGraphStore) PutWithTTL(ctx context.Context, namespace []string, key string, value interface{}, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("store is closed")
	}

	ns := namespaceToString(namespace)
	if s.data[ns] == nil {
		s.data[ns] = make(map[string]*StoreValue)
	}

	// Get current version
	version := 1
	if existing, exists := s.data[ns][key]; exists {
		version = existing.Version + 1
	}

	storeValue := &StoreValue{
		Value:     value,
		Timestamp: time.Now(),
		Version:   version,
		Metadata: map[string]interface{}{
			"namespace": namespace,
			"key":       key,
		},
	}

	if ttl > 0 {
		storeValue.TTL = &ttl
	}

	s.data[ns][key] = storeValue

	// Notify watchers
	s.notifyWatchers(namespace, StoreEvent{
		Type:      EventTypePut,
		Namespace: namespace,
		Key:       key,
		Value:     storeValue,
		Timestamp: time.Now(),
	})

	return nil
}

// Get retrieves a value
func (s *InMemoryLangGraphStore) Get(ctx context.Context, namespace []string, key string) (*StoreValue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, fmt.Errorf("store is closed")
	}

	ns := namespaceToString(namespace)
	if s.data[ns] == nil {
		return nil, fmt.Errorf("namespace not found")
	}

	value, exists := s.data[ns][key]
	if !exists {
		return nil, fmt.Errorf("key not found")
	}

	// Check expiration
	if value.IsExpired() {
		return nil, fmt.Errorf("value has expired")
	}

	// Return a copy to prevent external modification
	return s.copyStoreValue(value), nil
}

// Search performs similarity search (simplified for in-memory)
func (s *InMemoryLangGraphStore) Search(ctx context.Context, namespace []string, query string, limit int) ([]*StoreValue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, fmt.Errorf("store is closed")
	}

	ns := namespaceToString(namespace)
	if s.data[ns] == nil {
		return []*StoreValue{}, nil
	}

	results := make([]*StoreValue, 0, limit)
	queryLower := strings.ToLower(query)

	for key, value := range s.data[ns] {
		// Skip expired values
		if value.IsExpired() {
			continue
		}

		// Simple string matching for in-memory implementation
		// In production, you'd use vector similarity search
		matched := false

		// Check if key contains query
		if strings.Contains(strings.ToLower(key), queryLower) {
			matched = true
		}

		// Check value content if it's a string
		if str, ok := value.Value.(string); ok {
			if strings.Contains(strings.ToLower(str), queryLower) {
				matched = true
			}
		}

		if matched {
			results = append(results, s.copyStoreValue(value))
			if len(results) >= limit {
				break
			}
		}
	}

	return results, nil
}

// Delete removes a value
func (s *InMemoryLangGraphStore) Delete(ctx context.Context, namespace []string, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("store is closed")
	}

	ns := namespaceToString(namespace)
	if s.data[ns] == nil {
		return fmt.Errorf("namespace not found")
	}

	if _, exists := s.data[ns][key]; !exists {
		return fmt.Errorf("key not found")
	}

	delete(s.data[ns], key)

	// Note: We intentionally keep empty namespaces to distinguish
	// between "namespace not found" and "key not found" errors

	// Notify watchers
	s.notifyWatchers(namespace, StoreEvent{
		Type:      EventTypeDelete,
		Namespace: namespace,
		Key:       key,
		Timestamp: time.Now(),
	})

	return nil
}

// List returns all keys in a namespace
func (s *InMemoryLangGraphStore) List(ctx context.Context, namespace []string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, fmt.Errorf("store is closed")
	}

	ns := namespaceToString(namespace)
	if s.data[ns] == nil {
		return []string{}, nil
	}

	keys := make([]string, 0, len(s.data[ns]))
	for key, value := range s.data[ns] {
		// Skip expired values
		if !value.IsExpired() {
			keys = append(keys, key)
		}
	}

	return keys, nil
}

// ListWithPrefix returns keys with a specific prefix
func (s *InMemoryLangGraphStore) ListWithPrefix(ctx context.Context, namespace []string, prefix string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, fmt.Errorf("store is closed")
	}

	ns := namespaceToString(namespace)
	if s.data[ns] == nil {
		return []string{}, nil
	}

	keys := make([]string, 0)
	for key, value := range s.data[ns] {
		if strings.HasPrefix(key, prefix) && !value.IsExpired() {
			keys = append(keys, key)
		}
	}

	return keys, nil
}

// Update atomically updates a value
func (s *InMemoryLangGraphStore) Update(ctx context.Context, namespace []string, key string, updateFunc func(*StoreValue) (*StoreValue, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("store is closed")
	}

	ns := namespaceToString(namespace)
	if s.data[ns] == nil {
		s.data[ns] = make(map[string]*StoreValue)
	}

	current, exists := s.data[ns][key]
	if !exists {
		current = &StoreValue{
			Timestamp: time.Now(),
			Version:   0,
		}
	}

	// Create a copy for the update function (shallow copy to preserve types)
	updateCopy := &StoreValue{
		Value:     current.Value,
		Metadata:  current.Metadata, // Shallow copy is fine for update
		Timestamp: current.Timestamp,
		Version:   current.Version,
		TTL:       current.TTL,
	}

	// Apply update
	updated, err := updateFunc(updateCopy)
	if err != nil {
		return err
	}

	// Update version
	updated.Version = current.Version + 1
	updated.Timestamp = time.Now()

	s.data[ns][key] = updated

	// Notify watchers
	s.notifyWatchers(namespace, StoreEvent{
		Type:      EventTypeUpdate,
		Namespace: namespace,
		Key:       key,
		Value:     updated,
		Timestamp: time.Now(),
	})

	return nil
}

// Watch watches for changes in a namespace
func (s *InMemoryLangGraphStore) Watch(ctx context.Context, namespace []string) (<-chan StoreEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, fmt.Errorf("store is closed")
	}

	ns := namespaceToString(namespace)
	ch := make(chan StoreEvent, 100)

	if s.watchers[ns] == nil {
		s.watchers[ns] = make([]chan StoreEvent, 0)
	}
	s.watchers[ns] = append(s.watchers[ns], ch)

	// Handle context cancellation
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		defer s.mu.Unlock()

		// Remove watcher
		if watchers, exists := s.watchers[ns]; exists {
			for i, w := range watchers {
				if w == ch {
					s.watchers[ns] = append(watchers[:i], watchers[i+1:]...)
					break
				}
			}
		}
		close(ch)
	}()

	return ch, nil
}

// Close closes the store
func (s *InMemoryLangGraphStore) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		s.closed = true

		// Close all watcher channels
		for _, watchers := range s.watchers {
			for _, ch := range watchers {
				close(ch)
			}
		}

		s.watchers = make(map[string][]chan StoreEvent)
		s.data = make(map[string]map[string]*StoreValue)
	})

	return nil
}

// notifyWatchers notifies all watchers of an event
func (s *InMemoryLangGraphStore) notifyWatchers(namespace []string, event StoreEvent) {
	ns := namespaceToString(namespace)
	if watchers, exists := s.watchers[ns]; exists {
		for _, ch := range watchers {
			select {
			case ch <- event:
			default:
				// Don't block if channel is full
			}
		}
	}
}

// copyStoreValue creates a deep copy of a StoreValue
func (s *InMemoryLangGraphStore) copyStoreValue(value *StoreValue) *StoreValue {
	if value == nil {
		return nil
	}

	// Create a deep copy of Metadata
	copyMetadata := make(map[string]interface{})
	for k, v := range value.Metadata {
		copyMetadata[k] = deepCopyValue(v)
	}

	// Copy TTL
	ttl := value.TTL
	if ttl != nil {
		ttlCopy := *ttl
		ttl = &ttlCopy
	}

	return &StoreValue{
		Value:     deepCopyValue(value.Value),
		Metadata:  copyMetadata,
		Timestamp: value.Timestamp,
		Version:   value.Version,
		TTL:       ttl,
	}
}

// deepCopyValue creates a deep copy of interface{} value, preserving types
func deepCopyValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case map[string]interface{}:
		// Deep copy map
		copied := make(map[string]interface{}, len(val))
		for k, v := range val {
			copied[k] = deepCopyValue(v)
		}
		return copied

	case []interface{}:
		// Deep copy slice
		copied := make([]interface{}, len(val))
		for i, v := range val {
			copied[i] = deepCopyValue(v)
		}
		return copied

	case []string:
		// Deep copy string slice
		copied := make([]string, len(val))
		copy(copied, val)
		return copied

	case []int:
		// Deep copy int slice
		copied := make([]int, len(val))
		copy(copied, val)
		return copied

	case []float64:
		// Deep copy float64 slice
		copied := make([]float64, len(val))
		copy(copied, val)
		return copied

	default:
		// For primitive types (string, int, float64, bool, etc.), return as-is
		// They are immutable in Go
		return val
	}
}

// namespaceToString converts namespace array to string
func namespaceToString(namespace []string) string {
	return strings.Join(namespace, ":")
}

// StoreWithCache wraps a store with caching
type StoreWithCache struct {
	backend LangGraphStore
	cache   *InMemoryLangGraphStore
	ttl     time.Duration
}

// NewStoreWithCache creates a cached store
func NewStoreWithCache(backend LangGraphStore, ttl time.Duration) *StoreWithCache {
	return &StoreWithCache{
		backend: backend,
		cache:   NewInMemoryLangGraphStore(),
		ttl:     ttl,
	}
}

// Get retrieves with cache
func (s *StoreWithCache) Get(ctx context.Context, namespace []string, key string) (*StoreValue, error) {
	// Try cache first
	if cached, err := s.cache.Get(ctx, namespace, key); err == nil {
		return cached, nil
	}

	// Get from backend
	value, err := s.backend.Get(ctx, namespace, key)
	if err != nil {
		return nil, err
	}

	// Cache the value
	s.cache.PutWithTTL(ctx, namespace, key, value.Value, s.ttl)

	return value, nil
}

// Put stores in both cache and backend
func (s *StoreWithCache) Put(ctx context.Context, namespace []string, key string, value interface{}) error {
	// Store in backend
	if err := s.backend.Put(ctx, namespace, key, value); err != nil {
		return err
	}

	// Update cache
	return s.cache.PutWithTTL(ctx, namespace, key, value, s.ttl)
}

// PutWithTTL stores with TTL in both cache and backend
func (s *StoreWithCache) PutWithTTL(ctx context.Context, namespace []string, key string, value interface{}, ttl time.Duration) error {
	// Store in backend
	if err := s.backend.PutWithTTL(ctx, namespace, key, value, ttl); err != nil {
		return err
	}

	// Update cache with minimum TTL
	cacheTTL := ttl
	if s.ttl < ttl || ttl == 0 {
		cacheTTL = s.ttl
	}

	return s.cache.PutWithTTL(ctx, namespace, key, value, cacheTTL)
}

// Search performs search on backend
func (s *StoreWithCache) Search(ctx context.Context, namespace []string, query string, limit int) ([]*StoreValue, error) {
	return s.backend.Search(ctx, namespace, query, limit)
}

// Delete removes from both cache and backend
func (s *StoreWithCache) Delete(ctx context.Context, namespace []string, key string) error {
	// Delete from backend
	if err := s.backend.Delete(ctx, namespace, key); err != nil {
		return err
	}

	// Remove from cache
	return s.cache.Delete(ctx, namespace, key)
}

// List returns keys from backend
func (s *StoreWithCache) List(ctx context.Context, namespace []string) ([]string, error) {
	return s.backend.List(ctx, namespace)
}

// ListWithPrefix returns keys with prefix from backend
func (s *StoreWithCache) ListWithPrefix(ctx context.Context, namespace []string, prefix string) ([]string, error) {
	return s.backend.ListWithPrefix(ctx, namespace, prefix)
}

// Update atomically updates in backend and invalidates cache
func (s *StoreWithCache) Update(ctx context.Context, namespace []string, key string, updateFunc func(*StoreValue) (*StoreValue, error)) error {
	// Update in backend
	if err := s.backend.Update(ctx, namespace, key, updateFunc); err != nil {
		return err
	}

	// Invalidate cache
	return s.cache.Delete(ctx, namespace, key)
}

// Watch watches backend for changes
func (s *StoreWithCache) Watch(ctx context.Context, namespace []string) (<-chan StoreEvent, error) {
	return s.backend.Watch(ctx, namespace)
}

// Close closes both cache and backend
func (s *StoreWithCache) Close() error {
	s.cache.Close()
	return s.backend.Close()
}
