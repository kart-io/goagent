package store

import (
	"context"
	"strings"
	"sync"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/internal/snapshot"
)

// StoreValue represents a stored value with metadata
type StoreValue struct {
	Value     interface{}            `json:"value"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	TTL       *time.Duration         `json:"ttl,omitzero"`
	Version   int                    `json:"version"`
}

// IsExpired checks if the value has expired
func (v *StoreValue) IsExpired() bool {
	if v.TTL == nil {
		return false
	}
	return time.Since(v.Timestamp) > *v.TTL
}

// LangGraphStore 定义带版本、TTL与通知的存储接口。
// 内存实现的输入、读取、更新回调与每个Watch事件均为独立快照。
// Update回调返回错误或panic时不得发布数据或事件；回调不得重入同一存储。
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
	Value     *StoreValue            `json:"value,omitzero"`
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
	watchers  map[string][]*storeWatcher
	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once
}

type storeWatcher struct {
	ch   chan StoreEvent
	stop func() bool
}

// NewInMemoryLangGraphStore creates a new in-memory store
func NewInMemoryLangGraphStore() *InMemoryLangGraphStore {
	return &InMemoryLangGraphStore{
		data:     make(map[string]map[string]*StoreValue),
		watchers: make(map[string][]*storeWatcher),
	}
}

// Put stores a value
func (s *InMemoryLangGraphStore) Put(ctx context.Context, namespace []string, key string, value interface{}) error {
	return s.PutWithTTL(ctx, namespace, key, value, 0)
}

// PutWithTTL stores a value with TTL
func (s *InMemoryLangGraphStore) PutWithTTL(ctx context.Context, namespace []string, key string, value interface{}, ttl time.Duration) error {
	storeValue := &StoreValue{Value: value, Metadata: map[string]interface{}{"namespace": namespace, "key": key}}
	if ttl > 0 {
		storeValue.TTL = &ttl
	}
	storeValue, err := s.copyStoreValue(storeValue)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return agentErrors.New(agentErrors.CodeInternal, "store is closed").
			WithComponent("langgraph_store").
			WithOperation("put_with_ttl")
	}

	ns := namespaceToString(namespace)

	// Get current version
	version := 1
	if existing, exists := s.data[ns][key]; exists {
		version = existing.Version + 1
	}

	storeValue.Timestamp = time.Now()
	storeValue.Version = version

	// Notify watchers
	if err := s.notifyWatchers(namespace, StoreEvent{
		Type:      EventTypePut,
		Namespace: namespace,
		Key:       key,
		Value:     storeValue,
		Timestamp: time.Now(),
	}); err != nil {
		return err
	}
	if s.data[ns] == nil {
		s.data[ns] = make(map[string]*StoreValue)
	}
	s.data[ns][key] = storeValue

	return nil
}

// Get retrieves a value
func (s *InMemoryLangGraphStore) Get(ctx context.Context, namespace []string, key string) (*StoreValue, error) {
	value, err := s.readValue(namespace, key)
	if err != nil {
		return nil, err
	}
	// 已发布记录不再原地修改；锁外复制不会调用持锁的业务协议。
	return s.copyStoreValue(value)
}

func (s *InMemoryLangGraphStore) readValue(namespace []string, key string) (*StoreValue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, agentErrors.New(agentErrors.CodeInternal, "store is closed").
			WithComponent("langgraph_store").
			WithOperation("get")
	}

	ns := namespaceToString(namespace)
	if s.data[ns] == nil {
		return nil, agentErrors.New(agentErrors.CodeStoreNotFound, "namespace not found").
			WithComponent("langgraph_store").
			WithOperation("get").
			WithContext("namespace", namespace)
	}

	value, exists := s.data[ns][key]
	if !exists {
		return nil, agentErrors.NewStoreNotFoundError(namespace, key)
	}

	// Check expiration
	if value.IsExpired() {
		return nil, agentErrors.New(agentErrors.CodeStoreNotFound, "value has expired").
			WithComponent("langgraph_store").
			WithOperation("get").
			WithContext("namespace", namespace).
			WithContext("key", key)
	}

	// 已发布记录保持不可变，由公共读取入口在锁外复制。
	return value, nil
}

// Search performs similarity search (simplified for in-memory)
func (s *InMemoryLangGraphStore) Search(ctx context.Context, namespace []string, query string, limit int) ([]*StoreValue, error) {
	results, err := s.searchValues(namespace, query, limit)
	if err != nil {
		return nil, err
	}
	for i, value := range results {
		copy, err := s.copyStoreValue(value)
		if err != nil {
			return nil, err
		}
		results[i] = copy
	}
	return results, nil
}

func (s *InMemoryLangGraphStore) searchValues(namespace []string, query string, limit int) ([]*StoreValue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, agentErrors.New(agentErrors.CodeInternal, "store is closed").
			WithComponent("langgraph_store").
			WithOperation("search")
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
			results = append(results, value)
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
		return agentErrors.New(agentErrors.CodeInternal, "store is closed").
			WithComponent("langgraph_store").
			WithOperation("delete")
	}

	ns := namespaceToString(namespace)
	if s.data[ns] == nil {
		return agentErrors.New(agentErrors.CodeStoreNotFound, "namespace not found").
			WithComponent("langgraph_store").
			WithOperation("delete").
			WithContext("namespace", namespace)
	}

	if _, exists := s.data[ns][key]; !exists {
		return agentErrors.NewStoreNotFoundError(namespace, key)
	}

	// Note: We intentionally keep empty namespaces to distinguish
	// between "namespace not found" and "key not found" errors

	// Notify watchers
	if err := s.notifyWatchers(namespace, StoreEvent{
		Type:      EventTypeDelete,
		Namespace: namespace,
		Key:       key,
		Timestamp: time.Now(),
	}); err != nil {
		return err
	}
	delete(s.data[ns], key)

	return nil
}

// List returns all keys in a namespace
func (s *InMemoryLangGraphStore) List(ctx context.Context, namespace []string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, agentErrors.New(agentErrors.CodeInternal, "store is closed").
			WithComponent("langgraph_store").
			WithOperation("list")
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
		return nil, agentErrors.New(agentErrors.CodeInternal, "store is closed").
			WithComponent("langgraph_store").
			WithOperation("list_with_prefix")
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
		return agentErrors.New(agentErrors.CodeInternal, "store is closed").
			WithComponent("langgraph_store").
			WithOperation("update")
	}

	ns := namespaceToString(namespace)

	current, exists := s.data[ns][key]
	if !exists {
		current = &StoreValue{
			Timestamp: time.Now(),
			Version:   0,
		}
	}

	// 回调只操作隔离快照，错误或panic不会发布局部修改。
	updateCopy, err := s.copyStoreValue(current)
	if err != nil {
		return err
	}

	// Apply update
	updated, err := updateFunc(updateCopy)
	if err != nil {
		return err
	}
	if updated == nil {
		return agentErrors.New(agentErrors.CodeInvalidInput, "存储更新回调不能返回空记录").WithComponent("langgraph_store").WithOperation("update")
	}
	updated, err = s.copyStoreValue(updated)
	if err != nil {
		return err
	}

	// Update version
	updated.Version = current.Version + 1
	updated.Timestamp = time.Now()

	// Notify watchers
	if err := s.notifyWatchers(namespace, StoreEvent{
		Type:      EventTypeUpdate,
		Namespace: namespace,
		Key:       key,
		Value:     updated,
		Timestamp: time.Now(),
	}); err != nil {
		return err
	}
	if s.data[ns] == nil {
		s.data[ns] = make(map[string]*StoreValue)
	}
	s.data[ns][key] = updated

	return nil
}

// Watch watches for changes in a namespace
func (s *InMemoryLangGraphStore) Watch(ctx context.Context, namespace []string) (<-chan StoreEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, agentErrors.New(agentErrors.CodeInternal, "store is closed").
			WithComponent("langgraph_store").
			WithOperation("watch")
	}

	ns := namespaceToString(namespace)
	ch := make(chan StoreEvent, 100)

	w := &storeWatcher{ch: ch}
	s.watchers[ns] = append(s.watchers[ns], w)
	// AfterFunc 不为 Background 或尚未取消的标准context永久占用等待goroutine。
	w.stop = context.AfterFunc(ctx, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, registered := range s.watchers[ns] {
			if registered == w {
				s.watchers[ns] = append(s.watchers[ns][:i], s.watchers[ns][i+1:]...)
				if len(s.watchers[ns]) == 0 {
					delete(s.watchers, ns)
				}
				close(w.ch)
				return
			}
		}
	})

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
			for _, w := range watchers {
				w.stop()
				close(w.ch)
			}
		}

		s.watchers = make(map[string][]*storeWatcher)
		s.data = make(map[string]map[string]*StoreValue)
	})

	return nil
}

// notifyWatchers notifies all watchers of an event
func (s *InMemoryLangGraphStore) notifyWatchers(namespace []string, event StoreEvent) error {
	ns := namespaceToString(namespace)
	if watchers, exists := s.watchers[ns]; exists {
		// 所有快照成功后才发事件，避免克隆失败时发布一半通知。
		events := make([]StoreEvent, len(watchers))
		for i := range watchers {
			copy, err := snapshot.Clone(event)
			if err != nil {
				return agentErrors.Wrap(err, agentErrors.CodeStoreSerialization, "存储事件快照失败").WithComponent("langgraph_store").WithOperation("notify")
			}
			events[i] = copy.(StoreEvent)
		}
		for i, w := range watchers {
			select {
			case w.ch <- events[i]:
			default:
				// Don't block if channel is full
			}
		}
	}
	return nil
}

// copyStoreValue creates a deep copy of a StoreValue
func (s *InMemoryLangGraphStore) copyStoreValue(value *StoreValue) (*StoreValue, error) {
	if value == nil {
		return nil, nil
	}
	copy, err := snapshot.Clone(value)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeStoreSerialization, "存储记录快照失败").WithComponent("langgraph_store").WithOperation("snapshot")
	}
	return copy.(*StoreValue), nil
}

// namespaceToString converts namespace array to string
func namespaceToString(namespace []string) string {
	return NamespaceKey(namespace)
}

// StoreWithCache 缓存完整业务条目。ttl<=0 表示不额外限制缓存寿命，业务TTL始终有效。
// 同一包装器的Get与写入串行，防止冷读在写入失效后发布旧缓存。
// 直接改写backend可能在配置期限内读到旧缓存，跨包装器一致性由宿主负责。
type StoreWithCache struct {
	backend LangGraphStore
	cache   *InMemoryLangGraphStore
	ttl     time.Duration
	mu      sync.Mutex
	expires map[cacheKey]time.Time
}

type cacheKey struct{ namespace, key string }

// NewStoreWithCache creates a cached store
func NewStoreWithCache(backend LangGraphStore, ttl time.Duration) *StoreWithCache {
	return &StoreWithCache{
		backend: backend,
		cache:   NewInMemoryLangGraphStore(),
		ttl:     ttl,
		expires: make(map[cacheKey]time.Time),
	}
}

// Get retrieves with cache
func (s *StoreWithCache) Get(ctx context.Context, namespace []string, key string) (*StoreValue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := cacheKey{namespaceToString(namespace), key}
	if deadline := s.expires[id]; !deadline.IsZero() && !time.Now().Before(deadline) {
		s.invalidate(namespace, key)
	}
	if cached, err := s.cache.Get(ctx, namespace, key); err == nil {
		return cached, nil
	}

	// Get from backend
	value, err := s.backend.Get(ctx, namespace, key)
	if err != nil {
		return nil, err
	}

	if value == nil || value.IsExpired() {
		return nil, agentErrors.NewStoreNotFoundError(namespace, key)
	}
	if err := s.populate(namespace, key, value); err != nil {
		return nil, err
	}
	return s.cache.copyStoreValue(value)
}

// populate 缓存完整业务记录，淘汰期限与业务TTL分别判断，避免改变版本及过期语义。
func (s *StoreWithCache) populate(namespace []string, key string, value *StoreValue) error {
	copy, err := s.cache.copyStoreValue(value)
	if err != nil {
		return err
	}
	if copy.IsExpired() {
		return nil
	}
	ns := namespaceToString(namespace)
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	if s.cache.closed {
		return agentErrors.New(agentErrors.CodeStoreConnection, "缓存已关闭").WithComponent("store_cache").WithOperation("populate")
	}
	if s.cache.data[ns] == nil {
		s.cache.data[ns] = make(map[string]*StoreValue)
	}
	s.cache.data[ns][key] = copy
	if s.ttl > 0 {
		s.expires[cacheKey{ns, key}] = time.Now().Add(s.ttl)
	}
	return nil
}

// invalidate 的成功与否不影响已经提交的后端写入，冷缓存缺失也是正常状态。
func (s *StoreWithCache) invalidate(namespace []string, key string) {
	_ = s.cache.Delete(context.Background(), namespace, key)
	delete(s.expires, cacheKey{namespaceToString(namespace), key})
}

func (s *StoreWithCache) refresh(ctx context.Context, namespace []string, key string) {
	s.invalidate(namespace, key)
	if value, err := s.backend.Get(ctx, namespace, key); err == nil && value != nil && !value.IsExpired() {
		_ = s.populate(namespace, key, value)
	}
}

// Put stores in both cache and backend
func (s *StoreWithCache) Put(ctx context.Context, namespace []string, key string, value interface{}) error {
	owned, err := snapshot.Clone(value)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeStoreSerialization, "存储写入快照失败").WithComponent("store_cache").WithOperation("put")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Store in backend
	if err := s.backend.Put(ctx, namespace, key, owned); err != nil {
		return err
	}

	s.refresh(ctx, namespace, key)
	return nil
}

// PutWithTTL stores with TTL in both cache and backend
func (s *StoreWithCache) PutWithTTL(ctx context.Context, namespace []string, key string, value interface{}, ttl time.Duration) error {
	owned, err := snapshot.Clone(value)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeStoreSerialization, "存储写入快照失败").WithComponent("store_cache").WithOperation("put_with_ttl")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Store in backend
	if err := s.backend.PutWithTTL(ctx, namespace, key, owned, ttl); err != nil {
		return err
	}

	s.refresh(ctx, namespace, key)
	return nil
}

// Search performs search on backend
func (s *StoreWithCache) Search(ctx context.Context, namespace []string, query string, limit int) ([]*StoreValue, error) {
	values, err := s.backend.Search(ctx, namespace, query, limit)
	if err != nil {
		return nil, err
	}
	owned, err := snapshot.Clone(values)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeStoreSerialization, "存储查询快照失败").WithComponent("store_cache").WithOperation("search")
	}
	return owned.([]*StoreValue), nil
}

// Delete removes from both cache and backend
func (s *StoreWithCache) Delete(ctx context.Context, namespace []string, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Delete from backend
	if err := s.backend.Delete(ctx, namespace, key); err != nil {
		return err
	}

	// Remove from cache
	s.invalidate(namespace, key)
	return nil
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
	s.mu.Lock()
	defer s.mu.Unlock()
	// Update in backend
	if err := s.backend.Update(ctx, namespace, key, func(current *StoreValue) (*StoreValue, error) {
		copy, err := s.cache.copyStoreValue(current)
		if err != nil {
			return nil, err
		}
		updated, err := updateFunc(copy)
		if err != nil {
			return nil, err
		}
		if updated == nil {
			return nil, agentErrors.New(agentErrors.CodeInvalidInput, "存储更新回调不能返回空记录").WithComponent("store_cache").WithOperation("update")
		}
		return s.cache.copyStoreValue(updated)
	}); err != nil {
		return err
	}

	// Invalidate cache
	s.invalidate(namespace, key)
	return nil
}

// Watch watches backend for changes
func (s *StoreWithCache) Watch(ctx context.Context, namespace []string) (<-chan StoreEvent, error) {
	return s.backend.Watch(ctx, namespace)
}

// Close closes both cache and backend
func (s *StoreWithCache) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.cache.Close()
	clear(s.expires)
	return s.backend.Close()
}
