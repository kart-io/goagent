package testhelpers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AssertMapHasKey checks if a map has a specific key with a descriptive error message
func AssertMapHasKey(t *testing.T, m map[string]interface{}, key string) {
	t.Helper()
	_, ok := m[key]
	assert.True(t, ok, "Expected map to have key '%s', but it was missing. Available keys: %v", key, mapKeys(m))
}

// RequireMapValue gets a map value and requires it to be of the expected type
func RequireMapValue[T any](t *testing.T, m map[string]interface{}, key string) T {
	t.Helper()
	value, ok := m[key]
	require.True(t, ok, "Expected map to have key '%s', but it was missing", key)

	typedValue, ok := value.(T)
	require.True(t, ok, "Expected key '%s' to be of type %T, but got %T", key, typedValue, value)

	return typedValue
}

// GetMapValue gets a map value and returns it if it's of the expected type, otherwise returns false
func GetMapValue[T any](t *testing.T, m map[string]interface{}, key string) (T, bool) {
	t.Helper()
	var zero T

	value, ok := m[key]
	if !ok {
		return zero, false
	}

	typedValue, ok := value.(T)
	if !ok {
		return zero, false
	}

	return typedValue, true
}

// mapKeys returns all keys from a map for debugging
func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
