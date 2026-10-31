package contextpolicy

import (
	"context"
	"github.com/kart-io/goagent/core/durable"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReferenceContentIdentity(t *testing.T) {
	file, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	persist, err := NewDurableResultStore(file)
	require.NoError(t, err)
	for _, store := range []ResultStore{NewMemoryStore(), persist} {
		ctx := WithResultScope(context.Background(), "run-A")
		data := []byte("evidence")
		ref := NewRef("run-A", "policy", "call", data)
		require.NoError(t, store.PutIfAbsent(ctx, ref, data))
		require.NoError(t, store.PutIfAbsent(ctx, ref, data))
		data[0] = 'X'
		got, err := store.Get(ctx, ref)
		require.NoError(t, err)
		require.Equal(t, "evidence", string(got))
		got[0] = 'X'
		got, err = store.Get(ctx, ref)
		require.NoError(t, err)
		require.Equal(t, "evidence", string(got))
		changed := NewRef("run-A", "policy", "call", []byte("changed"))
		require.Equal(t, ref.Key, changed.Key)
		require.Error(t, store.PutIfAbsent(ctx, changed, []byte("changed")))
		_, err = store.Get(WithResultScope(ctx, "run-B"), ref)
		require.Error(t, err)
		bad := ref
		bad.SHA256 = "invalid"
		_, err = store.Get(ctx, bad)
		require.Error(t, err)
	}
	require.NotEqual(t, NewRef("run", "a/b", "c", nil).Key, NewRef("run", "a", "b/c", nil).Key)
}
func TestContextConfigFingerprint(t *testing.T) {
	config := OffloadConfig{MaxToolResultRunes: 10, Store: NewMemoryStore(), Namespace: "evidence"}
	a, err := NewOffload(config)
	require.NoError(t, err)
	config.ReadPageRunes = 2
	b, err := NewOffload(config)
	require.NoError(t, err)
	require.NotEqual(t, a.Identity(), b.Identity())
	config.ReadPageRunes = 0
	config.ExcludeTools = []string{"secret"}
	b, err = NewOffload(config)
	require.NoError(t, err)
	require.NotEqual(t, a.Identity(), b.Identity())
}
