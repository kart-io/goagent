package store

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/stretchr/testify/require"
)

type snapshotPayload struct {
	Bytes []byte
	Items map[string][]int
}

type rejectedSnapshot struct{ cause error }

func (v rejectedSnapshot) CloneSnapshot() (interface{}, error) { return nil, v.cause }

type lockCheckingSnapshot struct{ Check func() error }

func (v lockCheckingSnapshot) CloneSnapshot() (interface{}, error) {
	if err := v.Check(); err != nil {
		return nil, err
	}
	return v, nil
}

func TestDesignLangGraphCloneOutsideReadAndPutLocks(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryLangGraphStore()
	defer s.Close()
	reading := false
	value := lockCheckingSnapshot{Check: func() error {
		done := make(chan error, 1)
		go func() {
			if reading {
				done <- s.Put(ctx, nil, "independent", 1)
				return
			}
			_, err := s.List(ctx, nil)
			done <- err
		}()
		select {
		case err := <-done:
			return err
		case <-time.After(200 * time.Millisecond):
			return errors.New("快照调用被存储锁阻塞")
		}
	}}
	require.NoError(t, s.Put(ctx, nil, "k", value))
	reading = true
	_, err := s.Get(ctx, nil, "k")
	require.NoError(t, err)
	_, err = s.Search(ctx, nil, "k", 1)
	require.NoError(t, err)
}

func TestDesignLangGraphCloneFailureReleasesLocks(t *testing.T) {
	s := NewInMemoryLangGraphStore()
	defer s.Close()
	ctx := context.Background()
	mode := 0
	v := lockCheckingSnapshot{Check: func() error {
		switch mode {
		case 1:
			return errors.New("拒绝快照")
		case 2:
			panic("快照异常")
		}
		return nil
	}}
	require.NoError(t, s.Put(ctx, nil, "k", v))
	for _, nextMode := range []int{1, 2} {
		mode = nextMode
		for _, op := range []func(){
			func() { _, _ = s.Get(ctx, nil, "k") },
			func() { _, _ = s.Search(ctx, nil, "k", 1) },
			func() { _ = s.Put(ctx, nil, "bad", v) },
			func() { _ = s.Update(ctx, nil, "k", func(v *StoreValue) (*StoreValue, error) { return v, nil }) },
		} {
			func() { defer func() { _ = recover() }(); op() }()
			require.NoError(t, s.Put(ctx, nil, "independent", 1))
		}
	}
}

func TestDesignNotificationCloneFailureDoesNotCommit(t *testing.T) {
	for _, panicClone := range []bool{false, true} {
		s := NewInMemoryLangGraphStore()
		ctx := context.Background()
		require.NoError(t, s.Put(ctx, nil, "k", 1))
		events, err := s.Watch(ctx, nil)
		require.NoError(t, err)
		calls := 0
		v := lockCheckingSnapshot{Check: func() error {
			calls++
			if calls == 2 {
				if panicClone {
					panic("通知快照异常")
				}
				return errors.New("通知快照拒绝")
			}
			return nil
		}}
		func() { defer func() { _ = recover() }(); _ = s.Put(ctx, nil, "k", v) }()
		stored, err := s.Get(ctx, nil, "k")
		require.NoError(t, err)
		require.Equal(t, 1, stored.Value)
		select {
		case <-events:
			t.Fatal("通知快照失败不得发事件")
		default:
		}
		require.NoError(t, s.Put(ctx, nil, "independent", 1))
		require.NoError(t, s.Close())
	}
}

func TestDesignSnapshotErrorClassification(t *testing.T) {
	cause := errors.New("底层快照拒绝")
	ctx := context.Background()
	s := NewInMemoryLangGraphStore()
	defer s.Close()
	cached := NewStoreWithCache(NewInMemoryLangGraphStore(), time.Hour)
	defer cached.Close()
	for _, write := range []func() error{
		func() error { return s.Put(ctx, nil, "k", rejectedSnapshot{cause: cause}) },
		func() error { return cached.Put(ctx, nil, "k", rejectedSnapshot{cause: cause}) },
		func() error { return cached.PutWithTTL(ctx, nil, "k", rejectedSnapshot{cause: cause}, time.Hour) },
	} {
		err := write()
		require.Equal(t, agentErrors.CodeStoreSerialization, agentErrors.GetCode(err))
		require.ErrorIs(t, err, cause)
	}
	err := s.Update(ctx, nil, "k", func(*StoreValue) (*StoreValue, error) { return nil, nil })
	require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err))
	err = cached.Update(ctx, nil, "k", func(*StoreValue) (*StoreValue, error) { return nil, nil })
	require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err))
	require.NoError(t, cached.cache.Close())
	err = cached.populate(nil, "k", &StoreValue{Value: 1})
	require.Equal(t, agentErrors.CodeStoreConnection, agentErrors.GetCode(err))
}

func TestDesignUpdateFailureIsolation(t *testing.T) {
	for _, panicUpdate := range []bool{false, true} {
		t.Run(map[bool]string{false: "错误", true: "异常"}[panicUpdate], func(t *testing.T) {
			s := NewInMemoryLangGraphStore()
			defer s.Close()
			ctx := context.Background()
			ns := []string{"隔离"}
			require.NoError(t, s.PutWithTTL(ctx, ns, "k", &snapshotPayload{Bytes: []byte{1}, Items: map[string][]int{"v": {1}}}, time.Hour))
			before, err := s.Get(ctx, ns, "k")
			require.NoError(t, err)
			events, err := s.Watch(ctx, ns)
			require.NoError(t, err)
			func() {
				defer func() { _ = recover() }()
				err = s.Update(ctx, ns, "k", func(v *StoreValue) (*StoreValue, error) {
					v.Value.(*snapshotPayload).Bytes[0] = 9
					v.Value.(*snapshotPayload).Items["v"][0] = 9
					v.Metadata["key"] = "篡改"
					*v.TTL = time.Second
					if panicUpdate {
						panic("拒绝")
					}
					return nil, errors.New("拒绝")
				})
			}()
			after, getErr := s.Get(ctx, ns, "k")
			require.NoError(t, getErr)
			require.Equal(t, byte(1), after.Value.(*snapshotPayload).Bytes[0], "失败更新不能修改已保存数据")
			require.Equal(t, 1, after.Value.(*snapshotPayload).Items["v"][0])
			require.Equal(t, "k", after.Metadata["key"])
			require.Equal(t, before.Timestamp, after.Timestamp)
			require.Equal(t, before.Version, after.Version)
			require.Equal(t, time.Hour, *after.TTL)
			select {
			case <-events:
				t.Fatal("失败更新不应发事件")
			default:
			}
		})
	}
}

func TestDesignLangGraphOwnership(t *testing.T) {
	s := NewInMemoryLangGraphStore()
	defer s.Close()
	ctx := context.Background()
	ns := []string{"隔离"}
	input := &snapshotPayload{Bytes: []byte{1}, Items: map[string][]int{"v": {1}}}
	events1, err := s.Watch(ctx, ns)
	require.NoError(t, err)
	events2, err := s.Watch(ctx, ns)
	require.NoError(t, err)
	require.NoError(t, s.Put(ctx, ns, "k", input))
	input.Bytes[0] = 9
	ns[0] = "篡改"
	first := <-events1
	first.Value.Value.(*snapshotPayload).Items["v"][0] = 9
	first.Namespace[0] = "篡改"
	second := <-events2
	require.Equal(t, 1, second.Value.Value.(*snapshotPayload).Items["v"][0])
	require.Equal(t, []string{"隔离"}, second.Namespace)
	v, err := s.Get(ctx, []string{"隔离"}, "k")
	require.NoError(t, err)
	require.Equal(t, byte(1), v.Value.(*snapshotPayload).Bytes[0])
	v.Value.(*snapshotPayload).Bytes[0] = 8
	results, err := s.Search(ctx, []string{"隔离"}, "k", 1)
	require.NoError(t, err)
	require.Equal(t, byte(1), results[0].Value.(*snapshotPayload).Bytes[0])
}

func TestDesignNamespaceIsolation(t *testing.T) {
	s := NewInMemoryLangGraphStore()
	defer s.Close()
	ctx := context.Background()
	namespaces := [][]string{{"a:b"}, {"a", "b"}, {}, {""}, {"a", ""}, {"a:"}}
	for i, ns := range namespaces {
		require.NoError(t, s.Put(ctx, ns, "k", i))
	}
	for i, ns := range namespaces {
		v, err := s.Get(ctx, ns, "k")
		require.NoError(t, err)
		require.Equal(t, i, v.Value)
	}
}

func TestDesignCacheColdMutation(t *testing.T) {
	for _, operation := range []string{"删除", "更新"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			backend := NewInMemoryLangGraphStore()
			cached := NewStoreWithCache(backend, time.Hour)
			defer cached.Close()
			ns := []string{"冷缓存"}
			require.NoError(t, backend.Put(ctx, ns, "k", 1))
			if operation == "删除" {
				require.NoError(t, cached.Delete(ctx, ns, "k"))
				return
			}
			calls := 0
			require.NoError(t, cached.Update(ctx, ns, "k", func(v *StoreValue) (*StoreValue, error) { calls++; v.Value = v.Value.(int) + 1; return v, nil }))
			require.Equal(t, 1, calls)
			v, err := backend.Get(ctx, ns, "k")
			require.NoError(t, err)
			require.Equal(t, 2, v.Value)
		})
	}
}

func TestDesignCachePreservesEntry(t *testing.T) {
	ctx := context.Background()
	backend := NewInMemoryLangGraphStore()
	cached := NewStoreWithCache(backend, time.Hour)
	defer cached.Close()
	ns := []string{"缓存元数据"}
	require.NoError(t, backend.PutWithTTL(ctx, ns, "k", map[string][]byte{"v": {1}}, time.Hour))
	require.NoError(t, backend.Update(ctx, ns, "k", func(v *StoreValue) (*StoreValue, error) { v.Metadata["自定义"] = []int{1}; return v, nil }))
	cold, err := cached.Get(ctx, ns, "k")
	require.NoError(t, err)
	hot, err := cached.Get(ctx, ns, "k")
	require.NoError(t, err)
	require.Equal(t, cold, hot, "命中缓存必须保留业务版本、时间及TTL")
	cold.Value.(map[string][]byte)["v"][0] = 9
	hot.Metadata["自定义"].([]int)[0] = 9
	next, err := cached.Get(ctx, ns, "k")
	require.NoError(t, err)
	require.Equal(t, byte(1), next.Value.(map[string][]byte)["v"][0])
	require.Equal(t, []int{1}, next.Metadata["自定义"])
}

func TestDesignCacheBusinessTTL(t *testing.T) {
	for _, cacheTTL := range []time.Duration{0, time.Hour} {
		t.Run(cacheTTL.String(), func(t *testing.T) {
			ctx := context.Background()
			backend := NewInMemoryLangGraphStore()
			cached := NewStoreWithCache(backend, cacheTTL)
			defer cached.Close()
			ns := []string{"TTL"}
			require.NoError(t, backend.PutWithTTL(ctx, ns, "冷", 1, 30*time.Millisecond))
			require.NoError(t, cached.PutWithTTL(ctx, ns, "写", 1, 30*time.Millisecond))
			_, err := cached.Get(ctx, ns, "冷")
			require.NoError(t, err)
			time.Sleep(60 * time.Millisecond)
			for _, key := range []string{"冷", "写"} {
				_, err = cached.Get(ctx, ns, key)
				require.Error(t, err, "缓存不得延长后端TTL")
			}
		})
	}
}

func TestDesignWatchCloseCancel(t *testing.T) {
	if os.Getenv("GOAGENT_WATCH_REGRESSION_HELPER") == "1" {
		for i := 0; i < 100; i++ {
			s := NewInMemoryLangGraphStore()
			ctx, cancel := context.WithCancel(context.Background())
			ch, err := s.Watch(ctx, []string{"观察"})
			require.NoError(t, err)
			require.NoError(t, s.Close())
			cancel()
			_, open := <-ch
			require.False(t, open)
		}
		time.Sleep(30 * time.Millisecond)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDesignWatchCloseCancel$")
	cmd.Env = append(os.Environ(), "GOAGENT_WATCH_REGRESSION_HELPER=1")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "关闭后取消不得使后台panic: %s", output)
}

func TestDesignWatchConcurrentClose(t *testing.T) {
	s := NewInMemoryLangGraphStore()
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := s.Watch(ctx, []string{"观察"})
	require.NoError(t, err)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); cancel() }()
	go func() { defer wg.Done(); _ = s.Close() }()
	wg.Wait()
	for range ch {
	}
}

func TestDesignWatchCloseStopsWaiters(t *testing.T) {
	before := runtime.NumGoroutine()
	for i := 0; i < 200; i++ {
		s := NewInMemoryLangGraphStore()
		_, err := s.Watch(context.Background(), []string{"观察"})
		require.NoError(t, err)
		require.NoError(t, s.Close())
	}
	require.Eventually(t, func() bool { return runtime.NumGoroutine() <= before+10 }, time.Second, time.Millisecond, "关闭后不能保留等待永不取消context的goroutine")
}

func TestDesignUnsupportedWriteDoesNotCommit(t *testing.T) {
	s := NewInMemoryLangGraphStore()
	defer s.Close()
	ctx := context.Background()
	ns := []string{"拒绝快照"}
	require.NoError(t, s.Put(ctx, ns, "k", 1))
	before, err := s.Get(ctx, ns, "k")
	require.NoError(t, err)
	for _, value := range []interface{}{make(chan int), &sync.Mutex{}, struct{ data []byte }{data: []byte{1}}} {
		require.Error(t, s.Put(ctx, ns, "k", value))
		require.Error(t, s.Update(ctx, ns, "k", func(v *StoreValue) (*StoreValue, error) { v.Value = value; return v, nil }))
		after, err := s.Get(ctx, ns, "k")
		require.NoError(t, err)
		require.Equal(t, before, after)
	}
	require.Error(t, s.Update(ctx, ns, "k", func(*StoreValue) (*StoreValue, error) { return nil, nil }))
}

func TestDesignUpdateOwnsSuccessfulCallbackResult(t *testing.T) {
	s := NewInMemoryLangGraphStore()
	defer s.Close()
	ctx := context.Background()
	ns := []string{"回调所有权"}
	var retained *StoreValue
	require.NoError(t, s.Update(ctx, ns, "k", func(v *StoreValue) (*StoreValue, error) {
		retained = v
		v.Value = []byte{1}
		v.Metadata = map[string]interface{}{"v": []int{1}}
		return v, nil
	}))
	retained.Value.([]byte)[0] = 9
	retained.Metadata["v"].([]int)[0] = 9
	v, err := s.Get(ctx, ns, "k")
	require.NoError(t, err)
	require.Equal(t, []byte{1}, v.Value)
	require.Equal(t, []int{1}, v.Metadata["v"])
}

func TestDesignNamespacePublicOperations(t *testing.T) {
	s := NewInMemoryLangGraphStore()
	defer s.Close()
	ctx := context.Background()
	a, b := []string{"a:b"}, []string{"a", "b"}
	ca, cancel := context.WithCancel(ctx)
	defer cancel()
	eventsA, err := s.Watch(ca, a)
	require.NoError(t, err)
	eventsB, err := s.Watch(ctx, b)
	require.NoError(t, err)
	require.NoError(t, s.PutWithTTL(ctx, a, "prefix_a", "a", time.Hour))
	require.NoError(t, s.Update(ctx, b, "prefix_b", func(v *StoreValue) (*StoreValue, error) { v.Value = "b"; return v, nil }))
	require.Equal(t, "prefix_a", (<-eventsA).Key)
	require.Equal(t, "prefix_b", (<-eventsB).Key)
	keys, err := s.List(ctx, a)
	require.NoError(t, err)
	require.Equal(t, []string{"prefix_a"}, keys)
	keys, err = s.ListWithPrefix(ctx, b, "prefix_")
	require.NoError(t, err)
	require.Equal(t, []string{"prefix_b"}, keys)
	values, err := s.Search(ctx, b, "prefix", 10)
	require.NoError(t, err)
	require.Len(t, values, 1)
	require.NoError(t, s.Delete(ctx, a, "prefix_a"))
	_, err = s.Get(ctx, b, "prefix_b")
	require.NoError(t, err)
}
