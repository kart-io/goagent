package plugin

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateNameAndVersion(t *testing.T) {
	for _, bad := range []string{"", " a", "a ", "a/b", "a@b", "a.b"} {
		require.Error(t, validateName(bad), bad)
	}
	require.NoError(t, validateName("orders"))
	for _, bad := range []string{"", " 1", "1/2"} {
		require.Error(t, validateVersion(bad), bad)
	}
	require.NoError(t, validateVersion("1.2.0"))
}

func TestEnvProvide(t *testing.T) {
	r := newResources()
	env := newEnv("inventory", r)
	require.ErrorContains(t, env.Provide("inventory.db", 1), "只能在 Start 中")

	env.starting.Store(true)
	require.NoError(t, env.Provide("inventory.db", 1))
	require.ErrorContains(t, env.Provide("inventory.db", 2), "已存在")
	require.ErrorContains(t, env.Provide("pricing.db", 1), "须以")
	require.ErrorContains(t, env.Provide("inventory.", 1), "须以")
	require.ErrorContains(t, env.Provide("inventory.nil", nil), "nil")
	env.starting.Store(false)

	n, err := Lookup[int](r, "inventory.db")
	require.NoError(t, err)
	require.Equal(t, 1, n)
	_, err = Lookup[string](r, "inventory.db")
	require.ErrorContains(t, err, "类型")
	_, err = Lookup[int](r, "inventory.none")
	require.ErrorContains(t, err, "不存在")

	r.clear()
	_, ok := r.Resolve("inventory.db")
	require.False(t, ok)
}

func TestResourcesConcurrent(t *testing.T) {
	r := newResources()
	env := newEnv("a", r)
	env.starting.Store(true)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) { defer wg.Done(); _ = env.Provide("a.k"+string(rune('A'+i%26)), i) }(i)
		go func() { defer wg.Done(); r.Resolve("a.kA") }()
	}
	wg.Wait()
}
