package redis

import (
	"context"
	"github.com/kart-io/goagent/utils/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()
	// Create a miniredis server
	mr := miniredis.RunT(t)

	// Create config
	config := &Config{
		Addr:         mr.Addr(),
		Password:     "",
		DB:           0,
		Prefix:       "test:store:",
		TTL:          0,
		PoolSize:     10,
		MinIdleConns: 2,
		MaxRetries:   3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}

	// Create store
	s, err := New(config)
	require.NoError(t, err)
	require.NotNil(t, s)

	return s, mr
}

func TestNew(t *testing.T) {
	s, mr := setupTestStore(t)
	defer mr.Close()
	defer s.Close()

	assert.NotNil(t, s)
	assert.NotNil(t, s.client)
	assert.NotNil(t, s.config)
}

func TestNew_ConnectionFailure(t *testing.T) {
	config := &Config{
		Addr:        "localhost:9999", // Non-existent server
		DialTimeout: 1 * time.Second,
	}

	s, err := New(config)
	assert.Error(t, err)
	assert.Nil(t, s)
}

func TestStore_Put(t *testing.T) {
	s, mr := setupTestStore(t)
	defer mr.Close()
	defer s.Close()

	ctx := context.Background()
	namespace := []string{"users", "test"}
	key := "user1"
	value := map[string]interface{}{
		"name": "Alice",
		"age":  30,
	}

	err := s.Put(ctx, namespace, key, value)
	assert.NoError(t, err)

	// Verify the value was stored
	stored, err := s.Get(ctx, namespace, key)
	require.NoError(t, err)

	// JSON serialization converts numbers to float64
	expectedValue := map[string]interface{}{
		"name": "Alice",
		"age":  float64(30),
	}
	assert.Equal(t, expectedValue, stored.Value)
	assert.Equal(t, namespace, stored.Namespace)
	assert.Equal(t, key, stored.Key)
	assert.NotZero(t, stored.Created)
	assert.NotZero(t, stored.Updated)
}

func TestStore_Put_Update(t *testing.T) {
	s, mr := setupTestStore(t)
	defer mr.Close()
	defer s.Close()

	ctx := context.Background()
	namespace := []string{"users"}
	key := "user1"

	// First put
	value1 := "initial value"
	err := s.Put(ctx, namespace, key, value1)
	require.NoError(t, err)

	stored1, err := s.Get(ctx, namespace, key)
	require.NoError(t, err)
	created := stored1.Created

	time.Sleep(10 * time.Millisecond)

	// Update
	value2 := "updated value"
	err = s.Put(ctx, namespace, key, value2)
	require.NoError(t, err)

	stored2, err := s.Get(ctx, namespace, key)
	require.NoError(t, err)

	assert.Equal(t, value2, stored2.Value)
	assert.Equal(t, created, stored2.Created) // Created should remain the same
	assert.True(t, stored2.Updated.After(stored1.Updated))
}

func TestStore_Get(t *testing.T) {
	s, mr := setupTestStore(t)
	defer mr.Close()
	defer s.Close()

	ctx := context.Background()
	namespace := []string{"test"}
	key := "key1"
	value := "test value"

	// Put a value
	err := s.Put(ctx, namespace, key, value)
	require.NoError(t, err)

	// Get the value
	stored, err := s.Get(ctx, namespace, key)
	require.NoError(t, err)
	assert.Equal(t, value, stored.Value)
}

func TestStore_Get_NotFound(t *testing.T) {
	s, mr := setupTestStore(t)
	defer mr.Close()
	defer s.Close()

	ctx := context.Background()
	namespace := []string{"test"}
	key := "nonexistent"

	_, err := s.Get(ctx, namespace, key)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestStore_Delete(t *testing.T) {
	s, mr := setupTestStore(t)
	defer mr.Close()
	defer s.Close()

	ctx := context.Background()
	namespace := []string{"test"}
	key := "key1"
	value := "test value"

	// Put a value
	err := s.Put(ctx, namespace, key, value)
	require.NoError(t, err)

	// Delete the value
	err = s.Delete(ctx, namespace, key)
	assert.NoError(t, err)

	// Verify it's gone
	_, err = s.Get(ctx, namespace, key)
	assert.Error(t, err)
}

func TestStore_List(t *testing.T) {
	s, mr := setupTestStore(t)
	defer mr.Close()
	defer s.Close()

	ctx := context.Background()
	namespace := []string{"users"}

	// Put multiple values
	keys := []string{"user1", "user2", "user3"}
	for _, key := range keys {
		err := s.Put(ctx, namespace, key, map[string]string{"id": key})
		require.NoError(t, err)
	}

	// List keys
	listed, err := s.List(ctx, namespace)
	require.NoError(t, err)
	assert.ElementsMatch(t, keys, listed)
}

func TestStore_List_EmptyNamespace(t *testing.T) {
	s, mr := setupTestStore(t)
	defer mr.Close()
	defer s.Close()

	ctx := context.Background()
	namespace := []string{"empty"}

	listed, err := s.List(ctx, namespace)
	require.NoError(t, err)
	assert.Empty(t, listed)
}

func TestStore_Search(t *testing.T) {
	s, mr := setupTestStore(t)
	defer mr.Close()
	defer s.Close()

	ctx := context.Background()
	namespace := []string{"products"}

	// Put values with different metadata
	testData := []struct {
		key      string
		value    string
		metadata map[string]interface{}
	}{
		{"prod1", "Product 1", map[string]interface{}{"category": "electronics", "price": 100}},
		{"prod2", "Product 2", map[string]interface{}{"category": "electronics", "price": 200}},
		{"prod3", "Product 3", map[string]interface{}{"category": "books", "price": 50}},
	}

	for _, data := range testData {
		err := s.Put(ctx, namespace, data.key, data.value)
		require.NoError(t, err)

		// Update metadata
		stored, err := s.Get(ctx, namespace, data.key)
		require.NoError(t, err)
		stored.Metadata = data.metadata

		// Use client directly to update metadata
		client := s.client
		jsonData, _ := json.Marshal(stored)
		client.Set(ctx, s.makeKey(namespace, data.key), jsonData, 0)
	}

	// Search with filter
	filter := map[string]interface{}{"category": "electronics"}
	results, err := s.Search(ctx, namespace, filter)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestStore_Search_EmptyFilter(t *testing.T) {
	s, mr := setupTestStore(t)
	defer mr.Close()
	defer s.Close()

	ctx := context.Background()
	namespace := []string{"test"}

	// Put values
	for i := 0; i < 3; i++ {
		err := s.Put(ctx, namespace, string(rune('a'+i)), i)
		require.NoError(t, err)
	}

	// Search with empty filter (should return all)
	results, err := s.Search(ctx, namespace, map[string]interface{}{})
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestStore_Clear(t *testing.T) {
	s, mr := setupTestStore(t)
	defer mr.Close()
	defer s.Close()

	ctx := context.Background()
	namespace := []string{"test"}

	// Put multiple values
	for i := 0; i < 5; i++ {
		err := s.Put(ctx, namespace, string(rune('a'+i)), i)
		require.NoError(t, err)
	}

	// Clear namespace
	err := s.Clear(ctx, namespace)
	assert.NoError(t, err)

	// Verify all keys are gone
	keys, err := s.List(ctx, namespace)
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestStore_WithTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	config := &Config{
		Addr:   mr.Addr(),
		Prefix: "test:ttl:",
		TTL:    100 * time.Millisecond,
	}

	s, err := New(config)
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()
	namespace := []string{"test"}
	key := "expiring"

	// Put value with TTL
	err = s.Put(ctx, namespace, key, "value")
	require.NoError(t, err)

	// Value should exist
	_, err = s.Get(ctx, namespace, key)
	assert.NoError(t, err)

	// Fast forward time in miniredis
	mr.FastForward(200 * time.Millisecond)

	// Value should be expired
	_, err = s.Get(ctx, namespace, key)
	assert.Error(t, err)
}

func TestStore_Ping(t *testing.T) {
	s, mr := setupTestStore(t)
	defer mr.Close()
	defer s.Close()

	ctx := context.Background()
	err := s.Ping(ctx)
	assert.NoError(t, err)
}

func TestStore_Size(t *testing.T) {
	s, mr := setupTestStore(t)
	defer mr.Close()
	defer s.Close()

	ctx := context.Background()

	// Put values in different namespaces
	namespaces := [][]string{
		{"ns1"},
		{"ns1", "sub"},
		{"ns2"},
	}

	for _, ns := range namespaces {
		err := s.Put(ctx, ns, "key1", "value")
		require.NoError(t, err)
	}

	size, err := s.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, size)
}

func TestStore_NewFromClient(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	config := &Config{
		Prefix: "test:",
	}

	s := NewFromClient(client, config)
	defer s.Close()

	assert.NotNil(t, s)
	assert.Equal(t, client, s.client)
}

func TestStore_NamespaceIsolation(t *testing.T) {
	s, mr := setupTestStore(t)
	defer mr.Close()
	defer s.Close()

	ctx := context.Background()
	key := "same_key"
	value1 := "value in ns1"
	value2 := "value in ns2"

	// Put same key in different namespaces
	err := s.Put(ctx, []string{"ns1"}, key, value1)
	require.NoError(t, err)

	err = s.Put(ctx, []string{"ns2"}, key, value2)
	require.NoError(t, err)

	// Get from different namespaces
	stored1, err := s.Get(ctx, []string{"ns1"}, key)
	require.NoError(t, err)
	assert.Equal(t, value1, stored1.Value)

	stored2, err := s.Get(ctx, []string{"ns2"}, key)
	require.NoError(t, err)
	assert.Equal(t, value2, stored2.Value)
}
