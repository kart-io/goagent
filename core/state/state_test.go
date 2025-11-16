package state

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAgentState(t *testing.T) {
	state := NewAgentState()
	require.NotNil(t, state)
	assert.Equal(t, 0, state.Size())
	assert.Empty(t, state.Keys())
}

func TestNewAgentStateWithData(t *testing.T) {
	data := map[string]interface{}{
		"key1": "value1",
		"key2": 42,
		"key3": true,
	}

	state := NewAgentStateWithData(data)
	require.NotNil(t, state)
	assert.Equal(t, 3, state.Size())

	val1, ok := state.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val1)

	val2, ok := state.Get("key2")
	assert.True(t, ok)
	assert.Equal(t, 42, val2)

	val3, ok := state.Get("key3")
	assert.True(t, ok)
	assert.Equal(t, true, val3)
}

func TestAgentState_SetAndGet(t *testing.T) {
	state := NewAgentState()

	// Test setting and getting string
	state.Set("name", "Alice")
	val, ok := state.Get("name")
	assert.True(t, ok)
	assert.Equal(t, "Alice", val)

	// Test setting and getting int
	state.Set("age", 30)
	val, ok = state.Get("age")
	assert.True(t, ok)
	assert.Equal(t, 30, val)

	// Test setting and getting bool
	state.Set("active", true)
	val, ok = state.Get("active")
	assert.True(t, ok)
	assert.Equal(t, true, val)

	// Test getting non-existent key
	val, ok = state.Get("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, val)
}

func TestAgentState_Update(t *testing.T) {
	state := NewAgentState()

	updates := map[string]interface{}{
		"key1": "value1",
		"key2": 100,
		"key3": []string{"a", "b", "c"},
	}

	state.Update(updates)

	assert.Equal(t, 3, state.Size())

	val1, ok := state.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val1)

	val2, ok := state.Get("key2")
	assert.True(t, ok)
	assert.Equal(t, 100, val2)

	val3, ok := state.Get("key3")
	assert.True(t, ok)
	assert.Equal(t, []string{"a", "b", "c"}, val3)
}

func TestAgentState_Snapshot(t *testing.T) {
	state := NewAgentState()
	state.Set("key1", "value1")
	state.Set("key2", 42)

	snapshot := state.Snapshot()

	// Verify snapshot contents
	assert.Equal(t, 2, len(snapshot))
	assert.Equal(t, "value1", snapshot["key1"])
	assert.Equal(t, 42, snapshot["key2"])

	// Modify snapshot and verify original is unchanged
	snapshot["key3"] = "new value"
	_, ok := state.Get("key3")
	assert.False(t, ok)

	// Modify original and verify snapshot is unchanged
	state.Set("key4", "another value")
	_, exists := snapshot["key4"]
	assert.False(t, exists)
}

func TestAgentState_Clone(t *testing.T) {
	original := NewAgentState()
	original.Set("key1", "value1")
	original.Set("key2", 42)
	original.Set("key3", map[string]interface{}{"nested": "value"})

	cloned := original.Clone()
	require.NotNil(t, cloned)

	// Verify cloned state has same values
	assert.Equal(t, original.Size(), cloned.Size())

	val1, ok := cloned.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val1)

	val2, ok := cloned.Get("key2")
	assert.True(t, ok)
	assert.Equal(t, 42, val2)

	// Modify cloned state and verify original is unchanged
	cloned.Set("key4", "new value")
	_, ok = original.Get("key4")
	assert.False(t, ok)

	// Modify original and verify clone is unchanged
	original.Set("key5", "another value")
	_, ok = cloned.Get("key5")
	assert.False(t, ok)
}

func TestAgentState_Delete(t *testing.T) {
	state := NewAgentState()
	state.Set("key1", "value1")
	state.Set("key2", "value2")

	assert.Equal(t, 2, state.Size())

	state.Delete("key1")

	assert.Equal(t, 1, state.Size())
	_, ok := state.Get("key1")
	assert.False(t, ok)

	val, ok := state.Get("key2")
	assert.True(t, ok)
	assert.Equal(t, "value2", val)

	// Delete non-existent key should not panic
	state.Delete("nonexistent")
	assert.Equal(t, 1, state.Size())
}

func TestAgentState_Clear(t *testing.T) {
	state := NewAgentState()
	state.Set("key1", "value1")
	state.Set("key2", "value2")
	state.Set("key3", "value3")

	assert.Equal(t, 3, state.Size())

	state.Clear()

	assert.Equal(t, 0, state.Size())
	assert.Empty(t, state.Keys())

	_, ok := state.Get("key1")
	assert.False(t, ok)
}

func TestAgentState_Keys(t *testing.T) {
	state := NewAgentState()
	state.Set("key1", "value1")
	state.Set("key2", "value2")
	state.Set("key3", "value3")

	keys := state.Keys()

	assert.Equal(t, 3, len(keys))
	assert.Contains(t, keys, "key1")
	assert.Contains(t, keys, "key2")
	assert.Contains(t, keys, "key3")
}

func TestAgentState_Size(t *testing.T) {
	state := NewAgentState()
	assert.Equal(t, 0, state.Size())

	state.Set("key1", "value1")
	assert.Equal(t, 1, state.Size())

	state.Set("key2", "value2")
	assert.Equal(t, 2, state.Size())

	state.Delete("key1")
	assert.Equal(t, 1, state.Size())

	state.Clear()
	assert.Equal(t, 0, state.Size())
}

func TestAgentState_TypedGetters(t *testing.T) {
	state := NewAgentState()
	state.Set("string", "hello")
	state.Set("int", 42)
	state.Set("bool", true)
	state.Set("float", 3.14)
	state.Set("map", map[string]interface{}{"nested": "value"})
	state.Set("slice", []interface{}{1, 2, 3})

	// Test GetString
	str, ok := state.GetString("string")
	assert.True(t, ok)
	assert.Equal(t, "hello", str)

	str, ok = state.GetString("int")
	assert.False(t, ok)
	assert.Empty(t, str)

	str, ok = state.GetString("nonexistent")
	assert.False(t, ok)
	assert.Empty(t, str)

	// Test GetInt
	i, ok := state.GetInt("int")
	assert.True(t, ok)
	assert.Equal(t, 42, i)

	i, ok = state.GetInt("string")
	assert.False(t, ok)
	assert.Equal(t, 0, i)

	// Test GetBool
	b, ok := state.GetBool("bool")
	assert.True(t, ok)
	assert.True(t, b)

	b, ok = state.GetBool("int")
	assert.False(t, ok)
	assert.False(t, b)

	// Test GetFloat64
	f, ok := state.GetFloat64("float")
	assert.True(t, ok)
	assert.Equal(t, 3.14, f)

	f, ok = state.GetFloat64("string")
	assert.False(t, ok)
	assert.Equal(t, 0.0, f)

	// Test GetMap
	m, ok := state.GetMap("map")
	assert.True(t, ok)
	assert.Equal(t, map[string]interface{}{"nested": "value"}, m)

	m, ok = state.GetMap("string")
	assert.False(t, ok)
	assert.Nil(t, m)

	// Test GetSlice
	slice, ok := state.GetSlice("slice")
	assert.True(t, ok)
	assert.Equal(t, []interface{}{1, 2, 3}, slice)

	slice, ok = state.GetSlice("string")
	assert.False(t, ok)
	assert.Nil(t, slice)
}

func TestAgentState_ConcurrentAccess(t *testing.T) {
	state := NewAgentState()
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			state.Set("key", val)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state.Get("key")
		}()
	}

	// Concurrent updates
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			state.Update(map[string]interface{}{
				"batch_key": val,
			})
		}(i)
	}

	wg.Wait()

	// Should not panic and should have some value
	_, ok := state.Get("key")
	assert.True(t, ok)
}

func TestAgentState_ConcurrentOperations(t *testing.T) {
	state := NewAgentState()
	var wg sync.WaitGroup
	iterations := 1000

	// Multiple goroutines performing different operations
	wg.Add(5)

	// Goroutine 1: Set operations
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			state.Set("counter", i)
		}
	}()

	// Goroutine 2: Get operations
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			state.Get("counter")
		}
	}()

	// Goroutine 3: Update operations
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			state.Update(map[string]interface{}{
				"data": i,
			})
		}
	}()

	// Goroutine 4: Snapshot operations
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			state.Snapshot()
		}
	}()

	// Goroutine 5: Keys/Size operations
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			state.Keys()
			state.Size()
		}
	}()

	wg.Wait()

	// State should be consistent after all operations
	assert.NotPanics(t, func() {
		state.Snapshot()
		state.Keys()
		state.Size()
	})
}

func TestAgentState_String(t *testing.T) {
	state := NewAgentState()
	state.Set("key1", "value1")
	state.Set("key2", "value2")

	str := state.String()
	assert.Contains(t, str, "AgentState")
	assert.Contains(t, str, "size: 2")
}

func BenchmarkAgentState_Set(b *testing.B) {
	state := NewAgentState()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.Set("key", i)
	}
}

func BenchmarkAgentState_Get(b *testing.B) {
	state := NewAgentState()
	state.Set("key", "value")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.Get("key")
	}
}

func BenchmarkAgentState_Update(b *testing.B) {
	state := NewAgentState()
	updates := map[string]interface{}{
		"key1": "value1",
		"key2": 42,
		"key3": true,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.Update(updates)
	}
}

func BenchmarkAgentState_Snapshot(b *testing.B) {
	state := NewAgentState()
	state.Set("key1", "value1")
	state.Set("key2", 42)
	state.Set("key3", true)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.Snapshot()
	}
}

func BenchmarkAgentState_ConcurrentReadWrite(b *testing.B) {
	state := NewAgentState()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				state.Set("key", i)
			} else {
				state.Get("key")
			}
			i++
		}
	})
}
