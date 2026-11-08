package decorator_test

import (
	"context"
	"testing"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/store"
	"github.com/kart-io/goagent/store/decorator"
	"github.com/kart-io/goagent/store/memory"
	"github.com/stretchr/testify/require"
)

type legacyOnly struct{ store.Store }

func TestMetadataUsesAtomicSnapshotEntry(t *testing.T) {
	ctx := context.Background()
	backend := memory.New()
	metadata := map[string]interface{}{"分类": "原始", "数组": []int{1}}
	d := decorator.NewWithMetadata(backend, metadata)
	metadata["分类"] = "篡改"
	metadata["数组"].([]int)[0] = 9
	ns := []string{"metadata"}
	require.NoError(t, d.Put(ctx, ns, "k", 1))
	results, err := backend.Search(ctx, ns, map[string]interface{}{"分类": "原始"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, []int{1}, results[0].Metadata["数组"])
	outer := decorator.NewWithMetadata(d, map[string]interface{}{"层": 2})
	require.NoError(t, outer.Put(ctx, ns, "k", 2))
	results, err = backend.Search(ctx, ns, map[string]interface{}{"分类": "原始", "层": 2})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, 2, results[0].Value)
}

func TestMetadataUnsupportedBackendFailsBeforeWrite(t *testing.T) {
	ctx := context.Background()
	backend := memory.New()
	d := decorator.NewWithMetadata(legacyOnly{Store: backend}, map[string]interface{}{"分类": "值"})
	err := d.Put(ctx, nil, "k", 1)
	require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
	_, err = backend.Get(ctx, nil, "k")
	require.Error(t, err)
	d = decorator.NewWithMetadata(backend, map[string]interface{}{"资源": make(chan int)})
	err = d.Put(ctx, nil, "k", 1)
	require.Equal(t, agentErrors.CodeStoreSerialization, agentErrors.GetCode(err))
	require.Equal(t, 0, backend.Size())
}
