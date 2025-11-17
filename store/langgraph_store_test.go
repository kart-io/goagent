package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestInMemoryLangGraphStore tests
func TestInMemoryLangGraphStore_Creation(t *testing.T) {
	store := NewInMemoryLangGraphStore()
	assert.NotNil(t, store)
	assert.NotNil(t, store.data)
	assert.NotNil(t, store.watchers)
}

func TestInMemoryLangGraphStore_Put(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryLangGraphStore()

	// Test basic put
	err := store.Put(ctx, []string{"users"}, "123", map[string]interface{}{
		"name":  "John Doe",
		"email": "john@example.com",
	})
	assert.NoError(t, err)

	// Test put with nested namespace
	err = store.Put(ctx, []string{"org", "team", "members"}, "456", map[string]interface{}{
		"name": "Jane Smith",
		"role": "admin",
	})
	assert.NoError(t, err)

	// Test overwrite
	err = store.Put(ctx, []string{"users"}, "123", map[string]interface{}{
		"name":  "John Updated",
		"email": "john.updated@example.com",
	})
	assert.NoError(t, err)
}

func TestInMemoryLangGraphStore_PutWithTTL(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryLangGraphStore()

	// Test with TTL
	err := store.PutWithTTL(ctx, []string{"cache"}, "temp", "temporary data", 100*time.Millisecond)
	assert.NoError(t, err)

	// Should be accessible immediately
	value, err := store.Get(ctx, []string{"cache"}, "temp")
	assert.NoError(t, err)
	assert.Equal(t, "temporary data", value.Value)
	assert.NotNil(t, value.TTL)

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should be expired
	_, err = store.Get(ctx, []string{"cache"}, "temp")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestInMemoryLangGraphStore_Get(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryLangGraphStore()

	// Setup test data
	userData := map[string]interface{}{
		"name": "Test User",
		"id":   789,
	}
	store.Put(ctx, []string{"users"}, "789", userData)

	// Test successful get
	value, err := store.Get(ctx, []string{"users"}, "789")
	assert.NoError(t, err)
	assert.NotNil(t, value)
	assert.Equal(t, userData, value.Value)
	assert.Equal(t, 1, value.Version)
	assert.NotNil(t, value.Metadata)

	// Test non-existent namespace
	_, err = store.Get(ctx, []string{"nonexistent"}, "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "STORE_NOT_FOUND")

	// Test non-existent key
	_, err = store.Get(ctx, []string{"users"}, "999")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "STORE_NOT_FOUND")
}

func TestInMemoryLangGraphStore_Search(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryLangGraphStore()

	// Add test data
	store.Put(ctx, []string{"documents"}, "doc1", "This is a test document about testing")
	store.Put(ctx, []string{"documents"}, "doc2", "Another document with different content")
	store.Put(ctx, []string{"documents"}, "test_doc", "Test document with test in the key")

	// Search for "test"
	results, err := store.Search(ctx, []string{"documents"}, "test", 10)
	assert.NoError(t, err)
	assert.Len(t, results, 2) // Should find doc1 and test_doc

	// Search with limit
	results, err = store.Search(ctx, []string{"documents"}, "test", 1)
	assert.NoError(t, err)
	assert.Len(t, results, 1)

	// Search in non-existent namespace
	results, err = store.Search(ctx, []string{"nonexistent"}, "test", 10)
	assert.NoError(t, err)
	assert.Len(t, results, 0)
}

func TestInMemoryLangGraphStore_Delete(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryLangGraphStore()

	// Setup test data
	store.Put(ctx, []string{"users"}, "123", "user data")

	// Delete existing key
	err := store.Delete(ctx, []string{"users"}, "123")
	assert.NoError(t, err)

	// Verify deletion
	_, err = store.Get(ctx, []string{"users"}, "123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "STORE_NOT_FOUND")

	// Delete non-existent key
	err = store.Delete(ctx, []string{"users"}, "999")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "STORE_NOT_FOUND")

	// Delete from non-existent namespace
	err = store.Delete(ctx, []string{"nonexistent"}, "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "STORE_NOT_FOUND")
}

func TestInMemoryLangGraphStore_List(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryLangGraphStore()

	// Add test data
	store.Put(ctx, []string{"items"}, "item1", "data1")
	store.Put(ctx, []string{"items"}, "item2", "data2")
	store.Put(ctx, []string{"items"}, "item3", "data3")

	// List all keys
	keys, err := store.List(ctx, []string{"items"})
	assert.NoError(t, err)
	assert.Len(t, keys, 3)
	assert.Contains(t, keys, "item1")
	assert.Contains(t, keys, "item2")
	assert.Contains(t, keys, "item3")

	// List from empty namespace
	keys, err = store.List(ctx, []string{"empty"})
	assert.NoError(t, err)
	assert.Len(t, keys, 0)

	// Add expired item
	store.PutWithTTL(ctx, []string{"items"}, "temp", "data", 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	// List should not include expired items
	keys, err = store.List(ctx, []string{"items"})
	assert.NoError(t, err)
	assert.Len(t, keys, 3)
	assert.NotContains(t, keys, "temp")
}

func TestInMemoryLangGraphStore_ListWithPrefix(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryLangGraphStore()

	// Add test data
	store.Put(ctx, []string{"keys"}, "prefix_1", "data1")
	store.Put(ctx, []string{"keys"}, "prefix_2", "data2")
	store.Put(ctx, []string{"keys"}, "other_1", "data3")
	store.Put(ctx, []string{"keys"}, "prefix_3", "data4")

	// List with prefix
	keys, err := store.ListWithPrefix(ctx, []string{"keys"}, "prefix_")
	assert.NoError(t, err)
	assert.Len(t, keys, 3)
	assert.Contains(t, keys, "prefix_1")
	assert.Contains(t, keys, "prefix_2")
	assert.Contains(t, keys, "prefix_3")
	assert.NotContains(t, keys, "other_1")

	// List with non-matching prefix
	keys, err = store.ListWithPrefix(ctx, []string{"keys"}, "nonexistent_")
	assert.NoError(t, err)
	assert.Len(t, keys, 0)
}

func TestInMemoryLangGraphStore_Update(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryLangGraphStore()

	// Add initial data
	store.Put(ctx, []string{"counters"}, "count", 0)

	// Update with increment
	err := store.Update(ctx, []string{"counters"}, "count", func(sv *StoreValue) (*StoreValue, error) {
		count := sv.Value.(int)
		sv.Value = count + 1
		return sv, nil
	})
	assert.NoError(t, err)

	// Verify update
	value, err := store.Get(ctx, []string{"counters"}, "count")
	assert.NoError(t, err)
	assert.Equal(t, 1, value.Value)
	assert.Equal(t, 2, value.Version) // Version should be incremented

	// Update non-existent key (should create)
	err = store.Update(ctx, []string{"counters"}, "new", func(sv *StoreValue) (*StoreValue, error) {
		sv.Value = "created"
		return sv, nil
	})
	assert.NoError(t, err)

	value, err = store.Get(ctx, []string{"counters"}, "new")
	assert.NoError(t, err)
	assert.Equal(t, "created", value.Value)

	// Update with error
	err = store.Update(ctx, []string{"counters"}, "count", func(sv *StoreValue) (*StoreValue, error) {
		return nil, fmt.Errorf("update failed")
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "update failed")
}

func TestInMemoryLangGraphStore_Watch(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryLangGraphStore()

	// Start watching
	watchCtx, cancel := context.WithCancel(ctx)
	events, err := store.Watch(watchCtx, []string{"watched"})
	assert.NoError(t, err)
	assert.NotNil(t, events)

	// Collect events in background
	receivedEvents := make([]StoreEvent, 0)
	done := make(chan bool)
	go func() {
		for event := range events {
			receivedEvents = append(receivedEvents, event)
		}
		done <- true
	}()

	// Generate events
	store.Put(ctx, []string{"watched"}, "key1", "value1")
	store.Update(ctx, []string{"watched"}, "key1", func(sv *StoreValue) (*StoreValue, error) {
		sv.Value = "updated"
		return sv, nil
	})
	store.Delete(ctx, []string{"watched"}, "key1")

	// Give some time for events to be processed
	time.Sleep(100 * time.Millisecond)

	// Cancel watch
	cancel()
	<-done

	// Verify events
	assert.Len(t, receivedEvents, 3)
	assert.Equal(t, EventTypePut, receivedEvents[0].Type)
	assert.Equal(t, EventTypeUpdate, receivedEvents[1].Type)
	assert.Equal(t, EventTypeDelete, receivedEvents[2].Type)
}

func TestInMemoryLangGraphStore_Close(t *testing.T) {
	store := NewInMemoryLangGraphStore()
	ctx := context.Background()

	// Add some data
	store.Put(ctx, []string{"test"}, "key", "value")

	// Start watching
	events, err := store.Watch(ctx, []string{"test"})
	assert.NoError(t, err)

	// Close store
	err = store.Close()
	assert.NoError(t, err)

	// Operations should fail after close
	err = store.Put(ctx, []string{"test"}, "new", "data")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")

	_, err = store.Get(ctx, []string{"test"}, "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")

	// Watch channel should be closed
	_, ok := <-events
	assert.False(t, ok)

	// Multiple close calls should be safe
	err = store.Close()
	assert.NoError(t, err)
}

func TestStoreValue_IsExpired(t *testing.T) {
	// Test without TTL
	value := &StoreValue{
		Value:     "data",
		Timestamp: time.Now(),
	}
	assert.False(t, value.IsExpired())

	// Test with unexpired TTL
	ttl := 1 * time.Hour
	value = &StoreValue{
		Value:     "data",
		Timestamp: time.Now(),
		TTL:       &ttl,
	}
	assert.False(t, value.IsExpired())

	// Test with expired TTL
	ttl = 1 * time.Millisecond
	value = &StoreValue{
		Value:     "data",
		Timestamp: time.Now().Add(-1 * time.Hour),
		TTL:       &ttl,
	}
	assert.True(t, value.IsExpired())
}

func TestNamespaceToString(t *testing.T) {
	tests := []struct {
		namespace []string
		expected  string
	}{
		{[]string{"users"}, "users"},
		{[]string{"org", "team"}, "org:team"},
		{[]string{"a", "b", "c", "d"}, "a:b:c:d"},
		{[]string{}, ""},
	}

	for _, tt := range tests {
		result := namespaceToString(tt.namespace)
		assert.Equal(t, tt.expected, result)
	}
}

// TestStoreWithCache tests
func TestStoreWithCache_Creation(t *testing.T) {
	backend := NewInMemoryLangGraphStore()
	ttl := 5 * time.Minute
	cached := NewStoreWithCache(backend, ttl)

	assert.NotNil(t, cached)
	assert.NotNil(t, cached.backend)
	assert.NotNil(t, cached.cache)
	assert.Equal(t, ttl, cached.ttl)
}

func TestStoreWithCache_Get(t *testing.T) {
	ctx := context.Background()
	backend := NewInMemoryLangGraphStore()
	cached := NewStoreWithCache(backend, 1*time.Hour)

	// Add data to backend
	backend.Put(ctx, []string{"users"}, "123", "user data")

	// First get - should hit backend
	value, err := cached.Get(ctx, []string{"users"}, "123")
	assert.NoError(t, err)
	assert.Equal(t, "user data", value.Value)

	// Modify backend directly
	backend.Put(ctx, []string{"users"}, "123", "modified data")

	// Second get - should hit cache
	value, err = cached.Get(ctx, []string{"users"}, "123")
	assert.NoError(t, err)
	assert.Equal(t, "user data", value.Value) // Still old value from cache
}

func TestStoreWithCache_Put(t *testing.T) {
	ctx := context.Background()
	backend := NewInMemoryLangGraphStore()
	cached := NewStoreWithCache(backend, 1*time.Hour)

	// Put through cache
	err := cached.Put(ctx, []string{"items"}, "item1", "data1")
	assert.NoError(t, err)

	// Verify in backend
	backendValue, err := backend.Get(ctx, []string{"items"}, "item1")
	assert.NoError(t, err)
	assert.Equal(t, "data1", backendValue.Value)

	// Verify in cache
	cacheValue, err := cached.cache.Get(ctx, []string{"items"}, "item1")
	assert.NoError(t, err)
	assert.Equal(t, "data1", cacheValue.Value)
}

func TestStoreWithCache_Delete(t *testing.T) {
	ctx := context.Background()
	backend := NewInMemoryLangGraphStore()
	cached := NewStoreWithCache(backend, 1*time.Hour)

	// Add data
	cached.Put(ctx, []string{"items"}, "item1", "data1")

	// Delete
	err := cached.Delete(ctx, []string{"items"}, "item1")
	assert.NoError(t, err)

	// Verify deletion in backend
	_, err = backend.Get(ctx, []string{"items"}, "item1")
	assert.Error(t, err)

	// Verify deletion in cache
	_, err = cached.cache.Get(ctx, []string{"items"}, "item1")
	assert.Error(t, err)
}

func TestStoreWithCache_Update(t *testing.T) {
	ctx := context.Background()
	backend := NewInMemoryLangGraphStore()
	cached := NewStoreWithCache(backend, 1*time.Hour)

	// Add initial data
	cached.Put(ctx, []string{"counters"}, "count", 0)

	// Update
	err := cached.Update(ctx, []string{"counters"}, "count", func(sv *StoreValue) (*StoreValue, error) {
		sv.Value = sv.Value.(int) + 1
		return sv, nil
	})
	assert.NoError(t, err)

	// Verify update in backend
	value, err := backend.Get(ctx, []string{"counters"}, "count")
	assert.NoError(t, err)
	assert.Equal(t, 1, value.Value)

	// Cache should be invalidated, next get should fetch from backend
	value, err = cached.Get(ctx, []string{"counters"}, "count")
	assert.NoError(t, err)
	assert.Equal(t, 1, value.Value)
}

// Benchmark tests
func BenchmarkInMemoryLangGraphStore_Put(b *testing.B) {
	ctx := context.Background()
	store := NewInMemoryLangGraphStore()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Put(ctx, []string{"bench"}, fmt.Sprintf("key_%d", i), fmt.Sprintf("value_%d", i))
	}
}

func BenchmarkInMemoryLangGraphStore_Get(b *testing.B) {
	ctx := context.Background()
	store := NewInMemoryLangGraphStore()

	// Setup data
	for i := 0; i < 1000; i++ {
		store.Put(ctx, []string{"bench"}, fmt.Sprintf("key_%d", i), fmt.Sprintf("value_%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Get(ctx, []string{"bench"}, fmt.Sprintf("key_%d", i%1000))
	}
}

func BenchmarkInMemoryLangGraphStore_Search(b *testing.B) {
	ctx := context.Background()
	store := NewInMemoryLangGraphStore()

	// Setup data
	for i := 0; i < 1000; i++ {
		store.Put(ctx, []string{"docs"}, fmt.Sprintf("doc_%d", i), fmt.Sprintf("content with test data %d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Search(ctx, []string{"docs"}, "test", 10)
	}
}

func BenchmarkInMemoryLangGraphStore_Update(b *testing.B) {
	ctx := context.Background()
	store := NewInMemoryLangGraphStore()

	// Setup initial value
	store.Put(ctx, []string{"counter"}, "value", 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Update(ctx, []string{"counter"}, "value", func(sv *StoreValue) (*StoreValue, error) {
			sv.Value = sv.Value.(int) + 1
			return sv, nil
		})
	}
}

func BenchmarkStoreWithCache(b *testing.B) {
	ctx := context.Background()
	backend := NewInMemoryLangGraphStore()
	cached := NewStoreWithCache(backend, 1*time.Hour)

	// Setup data
	for i := 0; i < 1000; i++ {
		backend.Put(ctx, []string{"bench"}, fmt.Sprintf("key_%d", i), fmt.Sprintf("value_%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Mix of cache hits and misses
		cached.Get(ctx, []string{"bench"}, fmt.Sprintf("key_%d", i%100))
	}
}

// Helper function tests
func TestCopyStoreValue(t *testing.T) {
	store := &InMemoryLangGraphStore{}

	original := &StoreValue{
		Value: map[string]interface{}{
			"nested": map[string]interface{}{
				"key": "value",
			},
		},
		Metadata: map[string]interface{}{
			"test": true,
		},
		Timestamp: time.Now(),
		Version:   1,
	}

	copy := store.copyStoreValue(original)

	// Verify deep copy
	assert.Equal(t, original.Value, copy.Value)
	assert.Equal(t, original.Version, copy.Version)

	// Modify copy should not affect original
	copyMap := copy.Value.(map[string]interface{})
	copyMap["new"] = "data"

	originalMap := original.Value.(map[string]interface{})
	_, exists := originalMap["new"]
	assert.False(t, exists)

	// Test nil input
	nilCopy := store.copyStoreValue(nil)
	assert.Nil(t, nilCopy)
}
