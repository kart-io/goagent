package memory

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/store"
)

func TestNew(t *testing.T) {
	store := New()
	require.NotNil(t, store)
	assert.Equal(t, 0, store.Size())
}

func TestStore_PutAndGet(t *testing.T) {
	store := New()
	ctx := context.Background()

	namespace := []string{"users", "preferences"}
	key := "user123"
	value := map[string]interface{}{
		"theme":    "dark",
		"language": "en",
	}

	// Put value
	err := store.Put(ctx, namespace, key, value)
	require.NoError(t, err)

	// Get value
	storeValue, err := store.Get(ctx, namespace, key)
	require.NoError(t, err)
	require.NotNil(t, storeValue)

	assert.Equal(t, value, storeValue.Value)
	assert.Equal(t, namespace, storeValue.Namespace)
	assert.Equal(t, key, storeValue.Key)
	assert.NotZero(t, storeValue.Created)
	assert.NotZero(t, storeValue.Updated)
	assert.NotNil(t, storeValue.Metadata)
}

func TestStore_PutUpdate(t *testing.T) {
	store := New()
	ctx := context.Background()

	namespace := []string{"users"}
	key := "user123"

	// Initial put
	err := store.Put(ctx, namespace, key, "value1")
	require.NoError(t, err)

	value1, err := store.Get(ctx, namespace, key)
	require.NoError(t, err)
	created1 := value1.Created
	updated1 := value1.Updated

	// Wait a bit to ensure different timestamp
	time.Sleep(10 * time.Millisecond)

	// Update
	err = store.Put(ctx, namespace, key, "value2")
	require.NoError(t, err)

	value2, err := store.Get(ctx, namespace, key)
	require.NoError(t, err)

	// Created should be same, Updated should be different
	assert.Equal(t, created1, value2.Created)
	assert.True(t, value2.Updated.After(updated1))
	assert.Equal(t, "value2", value2.Value)
}

func TestStore_GetNonExistent(t *testing.T) {
	store := New()
	ctx := context.Background()

	// Get from non-existent namespace
	_, err := store.Get(ctx, []string{"nonexistent"}, "key")
	assert.Error(t, err)

	// Put a value
	err = store.Put(ctx, []string{"users"}, "user1", "value1")
	require.NoError(t, err)

	// Get non-existent key from existing namespace
	_, err = store.Get(ctx, []string{"users"}, "nonexistent")
	assert.Error(t, err)
}

func TestStore_Delete(t *testing.T) {
	store := New()
	ctx := context.Background()

	namespace := []string{"users"}
	key := "user123"

	// Put value
	err := store.Put(ctx, namespace, key, "value")
	require.NoError(t, err)
	assert.Equal(t, 1, store.Size())

	// Delete value
	err = store.Delete(ctx, namespace, key)
	require.NoError(t, err)
	assert.Equal(t, 0, store.Size())

	// Verify deleted
	_, err = store.Get(ctx, namespace, key)
	assert.Error(t, err)

	// Delete non-existent should not error
	err = store.Delete(ctx, []string{"nonexistent"}, "key")
	assert.NoError(t, err)
}

func TestStore_List(t *testing.T) {
	store := New()
	ctx := context.Background()

	namespace := []string{"users"}

	// Put multiple values
	err := store.Put(ctx, namespace, "user1", "value1")
	require.NoError(t, err)
	err = store.Put(ctx, namespace, "user2", "value2")
	require.NoError(t, err)
	err = store.Put(ctx, namespace, "user3", "value3")
	require.NoError(t, err)

	// List keys
	keys, err := store.List(ctx, namespace)
	require.NoError(t, err)
	assert.Equal(t, 3, len(keys))
	assert.Contains(t, keys, "user1")
	assert.Contains(t, keys, "user2")
	assert.Contains(t, keys, "user3")

	// List non-existent namespace
	keys, err = store.List(ctx, []string{"nonexistent"})
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestStore_Clear(t *testing.T) {
	store := New()
	ctx := context.Background()

	namespace := []string{"users"}

	// Put multiple values
	err := store.Put(ctx, namespace, "user1", "value1")
	require.NoError(t, err)
	err = store.Put(ctx, namespace, "user2", "value2")
	require.NoError(t, err)

	assert.Equal(t, 2, store.Size())

	// Clear namespace
	err = store.Clear(ctx, namespace)
	require.NoError(t, err)

	assert.Equal(t, 0, store.Size())

	// Verify all keys are gone
	keys, err := store.List(ctx, namespace)
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestStore_Search(t *testing.T) {
	store := New()
	ctx := context.Background()

	namespace := []string{"products"}

	// Put values with metadata
	err := store.Put(ctx, namespace, "product1", "Laptop")
	require.NoError(t, err)
	val1, _ := store.Get(ctx, namespace, "product1")
	val1.Metadata["category"] = "electronics"
	val1.Metadata["price"] = 1000
	err = store.Put(ctx, namespace, "product1", val1.Value)
	require.NoError(t, err)

	err = store.Put(ctx, namespace, "product2", "Phone")
	require.NoError(t, err)
	val2, _ := store.Get(ctx, namespace, "product2")
	val2.Metadata["category"] = "electronics"
	val2.Metadata["price"] = 500
	err = store.Put(ctx, namespace, "product2", val2.Value)
	require.NoError(t, err)

	err = store.Put(ctx, namespace, "product3", "Desk")
	require.NoError(t, err)
	val3, _ := store.Get(ctx, namespace, "product3")
	val3.Metadata["category"] = "furniture"
	val3.Metadata["price"] = 300
	err = store.Put(ctx, namespace, "product3", val3.Value)
	require.NoError(t, err)

	// Search with no filter (returns all)
	results, err := store.Search(ctx, namespace, nil)
	require.NoError(t, err)
	assert.Equal(t, 3, len(results))

	// Search by category
	results, err = store.Search(ctx, namespace, map[string]interface{}{
		"category": "electronics",
	})
	require.NoError(t, err)
	assert.Equal(t, 2, len(results))

	// Search by multiple criteria
	results, err = store.Search(ctx, namespace, map[string]interface{}{
		"category": "electronics",
		"price":    500,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, "Phone", results[0].Value)

	// Search non-matching criteria
	results, err = store.Search(ctx, namespace, map[string]interface{}{
		"category": "nonexistent",
	})
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestStore_MultipleNamespaces(t *testing.T) {
	store := New()
	ctx := context.Background()

	// Put values in different namespaces
	err := store.Put(ctx, []string{"users"}, "user1", "value1")
	require.NoError(t, err)
	err = store.Put(ctx, []string{"products"}, "product1", "value2")
	require.NoError(t, err)
	err = store.Put(ctx, []string{"users", "preferences"}, "pref1", "value3")
	require.NoError(t, err)

	assert.Equal(t, 3, store.Size())

	// Verify isolation
	keys1, err := store.List(ctx, []string{"users"})
	require.NoError(t, err)
	assert.Equal(t, 1, len(keys1))

	keys2, err := store.List(ctx, []string{"products"})
	require.NoError(t, err)
	assert.Equal(t, 1, len(keys2))

	keys3, err := store.List(ctx, []string{"users", "preferences"})
	require.NoError(t, err)
	assert.Equal(t, 1, len(keys3))

	// Clear one namespace
	err = store.Clear(ctx, []string{"users"})
	require.NoError(t, err)

	// Other namespaces should be unaffected
	assert.Equal(t, 2, store.Size())
	_, err = store.Get(ctx, []string{"products"}, "product1")
	assert.NoError(t, err)
}

func TestStore_Namespaces(t *testing.T) {
	store := New()
	ctx := context.Background()

	// Put values in different namespaces
	err := store.Put(ctx, []string{"users"}, "user1", "value1")
	require.NoError(t, err)
	err = store.Put(ctx, []string{"products"}, "product1", "value2")
	require.NoError(t, err)

	namespaces := store.Namespaces()
	assert.Equal(t, 2, len(namespaces))
	assert.Contains(t, namespaces, "/users")
	assert.Contains(t, namespaces, "/products")
}

func TestNamespaceToKey(t *testing.T) {
	tests := []struct {
		namespace []string
		expected  string
	}{
		{[]string{}, "/"},
		{[]string{"users"}, "/users"},
		{[]string{"users", "preferences"}, "/users/preferences"},
		{[]string{"a", "b", "c", "d"}, "/a/b/c/d"},
	}

	for _, tt := range tests {
		result := namespaceToKey(tt.namespace)
		assert.Equal(t, tt.expected, result)
	}
}

func TestMatchesFilter(t *testing.T) {
	value := &storeValue{
		Value: "test",
		Metadata: map[string]interface{}{
			"category": "electronics",
			"price":    100,
			"active":   true,
		},
	}

	tests := []struct {
		name     string
		filter   map[string]interface{}
		expected bool
	}{
		{"empty filter", nil, true},
		{"empty filter map", map[string]interface{}{}, true},
		{"matching single field", map[string]interface{}{"category": "electronics"}, true},
		{"matching multiple fields", map[string]interface{}{"category": "electronics", "price": 100}, true},
		{"non-matching field", map[string]interface{}{"category": "furniture"}, false},
		{"non-existent field", map[string]interface{}{"nonexistent": "value"}, false},
		{"partial match", map[string]interface{}{"category": "electronics", "price": 200}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesFilter(value, tt.filter)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func BenchmarkStore_Put(b *testing.B) {
	store := New()
	ctx := context.Background()
	namespace := []string{"bench"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Put(ctx, namespace, "key", i)
	}
}

func BenchmarkStore_Get(b *testing.B) {
	store := New()
	ctx := context.Background()
	namespace := []string{"bench"}
	store.Put(ctx, namespace, "key", "value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Get(ctx, namespace, "key")
	}
}

func BenchmarkStore_Search(b *testing.B) {
	store := New()
	ctx := context.Background()
	namespace := []string{"bench"}

	// Populate with test data
	for i := 0; i < 100; i++ {
		store.Put(ctx, namespace, fmt.Sprintf("key%d", i), i)
		val, _ := store.Get(ctx, namespace, fmt.Sprintf("key%d", i))
		val.Metadata["category"] = "test"
		store.Put(ctx, namespace, fmt.Sprintf("key%d", i), val.Value)
	}

	filter := map[string]interface{}{"category": "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Search(ctx, namespace, filter)
	}
}

// storeValue is a type alias to make tests work with the local type
type storeValue = store.Value
