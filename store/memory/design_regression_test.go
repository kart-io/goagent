package memory

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/store/decorator"
	"github.com/stretchr/testify/require"
)

type rejectedSnapshot struct{ cause error }

func (v rejectedSnapshot) CloneSnapshot() (interface{}, error) { return nil, v.cause }

type lockCheckingSnapshot struct{ Check func() error }

func (v lockCheckingSnapshot) CloneSnapshot() (interface{}, error) {
	if err := v.Check(); err != nil {
		return nil, err
	}
	return v, nil
}

func TestDesignMemoryCloneOutsideReadLocks(t *testing.T) {
	s := New()
	ctx := context.Background()
	reading := false
	v := lockCheckingSnapshot{Check: func() error {
		if !reading {
			return nil
		}
		done := make(chan error, 1)
		go func() { done <- s.Put(ctx, nil, "independent", 1) }()
		select {
		case err := <-done:
			return err
		case <-time.After(200 * time.Millisecond):
			return errors.New("快照调用被存储锁阻塞")
		}
	}}
	require.NoError(t, s.PutWithMetadata(ctx, nil, "k", v, map[string]interface{}{"分类": "值"}))
	reading = true
	_, err := s.Get(ctx, nil, "k")
	require.NoError(t, err)
	_, err = s.Search(ctx, nil, map[string]interface{}{"分类": "值"})
	require.NoError(t, err)
}

func TestDesignMemoryCloneFailureReleasesLocks(t *testing.T) {
	s := New()
	ctx := context.Background()
	mode := 0
	v := lockCheckingSnapshot{Check: func() error {
		switch mode {
		case 1:
			return errors.New("快照拒绝")
		case 2:
			panic("快照异常")
		}
		return nil
	}}
	require.NoError(t, s.Put(ctx, nil, "k", v))
	for _, nextMode := range []int{1, 2} {
		mode = nextMode
		for _, op := range []func(){func() { _, _ = s.Get(ctx, nil, "k") }, func() { _, _ = s.Search(ctx, nil, nil) }} {
			func() { defer func() { _ = recover() }(); op() }()
			require.NoError(t, s.Put(ctx, nil, "independent", 1))
		}
	}
}

func TestDesignMemorySnapshotErrorClassification(t *testing.T) {
	cause := errors.New("底层快照拒绝")
	err := New().Put(context.Background(), nil, "k", rejectedSnapshot{cause: cause})
	require.Equal(t, agentErrors.CodeStoreSerialization, agentErrors.GetCode(err))
	require.ErrorIs(t, err, cause)
}

func TestDesignMemoryOwnership(t *testing.T) {
	s := New()
	ctx := context.Background()
	ns := []string{"独立"}
	input := map[string][]byte{"v": {1}}
	require.NoError(t, s.Put(ctx, ns, "k", input))
	input["v"][0] = 9
	ns[0] = "篡改"
	got, err := s.Get(ctx, []string{"独立"}, "k")
	require.NoError(t, err)
	require.Equal(t, byte(1), got.Value.(map[string][]byte)["v"][0])
	got.Value.(map[string][]byte)["v"][0] = 8
	results, err := s.Search(ctx, []string{"独立"}, nil)
	require.NoError(t, err)
	require.Equal(t, byte(1), results[0].Value.(map[string][]byte)["v"][0])
	require.NoError(t, s.Delete(ctx, []string{"独立"}, "k"))
	require.Equal(t, "k", got.Key, "删除不能清空仍被外部持有的记录")
	require.Equal(t, "k", results[0].Key)
}

func TestDesignMemoryIndexLifecycle(t *testing.T) {
	s := New()
	ctx := context.Background()
	ns := []string{"索引"}
	d := decorator.NewWithMetadata(s, map[string]interface{}{"分类": "旧"})
	require.NoError(t, d.Put(ctx, ns, "k", 1))
	require.NoError(t, d.Put(ctx, ns, "k", 2))
	results, err := s.Search(ctx, ns, map[string]interface{}{"分类": "旧"})
	require.NoError(t, err)
	require.Len(t, results, 1, "索引只能引用当前唯一版本")
	require.Equal(t, 2, results[0].Value)
	require.NoError(t, s.Clear(ctx, ns))
	require.NoError(t, s.Put(ctx, ns, "new", 3))
	results, err = s.Search(ctx, ns, map[string]interface{}{"分类": "旧"})
	require.NoError(t, err)
	require.Empty(t, results, "Clear之后旧索引不得复活")
}

func TestDesignMemoryNamespaceIsolation(t *testing.T) {
	s := New()
	ctx := context.Background()
	namespaces := [][]string{{"a/b"}, {"a", "b"}, {}, {""}, {"a", ""}, {"a/"}}
	for i, ns := range namespaces {
		require.NoError(t, s.Put(ctx, ns, "k", i))
	}
	for i, ns := range namespaces {
		v, err := s.Get(ctx, ns, "k")
		require.NoError(t, err)
		require.Equal(t, i, v.Value)
	}
}

func TestDesignMemoryConcurrentReplaceDelete(t *testing.T) {
	s := New()
	ctx := context.Background()
	ns := []string{"竞争"}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 1000; n++ {
				_ = s.Put(ctx, ns, "k", n)
				_, _ = s.Get(ctx, ns, "k")
				_, _ = s.Search(ctx, ns, nil)
				_ = s.Delete(ctx, ns, "k")
			}
		}()
	}
	wg.Wait()
}

func TestDesignMemoryMetadataSnapshot(t *testing.T) {
	s := New()
	ctx := context.Background()
	ns := []string{"元数据"}
	metadata := map[string]interface{}{"嵌套": []int{1}, "分类": "旧"}
	require.NoError(t, s.PutWithMetadata(ctx, ns, "k", 1, metadata))
	metadata["嵌套"].([]int)[0] = 9
	got, err := s.Get(ctx, ns, "k")
	require.NoError(t, err)
	require.Equal(t, []int{1}, got.Metadata["嵌套"])
	got.Metadata["分类"] = "外部修改"
	require.NoError(t, s.PutWithMetadata(ctx, ns, "k", 2, map[string]interface{}{"分类": "新"}))
	results, err := s.Search(ctx, ns, map[string]interface{}{"分类": "旧"})
	require.NoError(t, err)
	require.Empty(t, results)
	results, err = s.Search(ctx, ns, map[string]interface{}{"分类": "新", "嵌套": []int{1}})
	require.NoError(t, err)
	require.Len(t, results, 1)
	results[0].Metadata["嵌套"].([]int)[0] = 9
	results, err = s.Search(ctx, ns, map[string]interface{}{"嵌套": []int{1}})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Error(t, s.PutWithMetadata(ctx, ns, "k", 3, map[string]interface{}{"拒绝": make(chan int)}))
	got, err = s.Get(ctx, ns, "k")
	require.NoError(t, err)
	require.Equal(t, 2, got.Value)
}

func TestDesignMemoryNamespacePublicOperations(t *testing.T) {
	s := New()
	ctx := context.Background()
	a, b := []string{"a/b"}, []string{"a", "b"}
	require.NoError(t, s.PutWithMetadata(ctx, a, "a", 1, map[string]interface{}{"分类": "a"}))
	require.NoError(t, s.PutWithMetadata(ctx, b, "b", 2, map[string]interface{}{"分类": "b"}))
	keys, err := s.List(ctx, b)
	require.NoError(t, err)
	require.Equal(t, []string{"b"}, keys)
	values, err := s.Search(ctx, a, map[string]interface{}{"分类": "a"})
	require.NoError(t, err)
	require.Len(t, values, 1)
	require.NoError(t, s.Delete(ctx, a, "a"))
	require.NoError(t, s.Clear(ctx, a))
	_, err = s.Get(ctx, b, "b")
	require.NoError(t, err)
}
