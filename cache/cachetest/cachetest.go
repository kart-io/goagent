// Package cachetest 提供后端无关的 cache.Tagged 一致性测试，只经公开接口，对任何满足契约的后端都应成立。
package cachetest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/cache"
	agentErrors "github.com/kart-io/goagent/errors"
)

// Cache 是被测后端需要同时满足的接口。
type Cache interface {
	cache.Bytes
	cache.Tagged
}

const (
	longTTL = time.Minute
	opWait  = 5 * time.Second
)

// RunTagged 对后端运行 Tagged 一致性用例。
// open 每次调用返回一个实例：同一个 t 内同名的实例共享数据（模拟两个进程），不同名互相隔离；每个子测试的数据初始为空。
func RunTagged(t *testing.T, open func(t *testing.T, name string) Cache) {
	cases := []struct {
		name string
		run  func(t *testing.T, open func(t *testing.T, name string) Cache)
	}{
		{"读写", caseReadWrite},
		{"删除标签跨实例生效", caseDeleteTagAcrossInstances},
		{"幂等删除", caseIdempotentDelete},
		{"标签替换", caseTagReplace},
		{"多标签只删其一", caseMultiTagDeleteOne},
		{"无标签覆盖", caseUntaggedOverwrite},
		{"TTL", caseTTL},
		{"删除后新写入可读", caseWriteAfterDelete},
		{"并发不漏删", caseConcurrentNoMissedDelete},
		{"不同名隔离", caseIsolation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { tc.run(t, open) })
	}
}

func opCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opWait)
	t.Cleanup(cancel)
	return ctx
}

// requireMiss 断言读取未命中；其他错误一律视为失败。
func requireMiss(t *testing.T, c Cache, key string) {
	t.Helper()
	v, err := c.GetBytes(opCtx(t), key)
	require.Truef(t, errors.Is(err, agentErrors.ErrCacheMiss), "GetBytes(%s) 期望未命中，得到 value=%q err=%v", key, v, err)
}

func requireValue(t *testing.T, c Cache, key, want string) {
	t.Helper()
	v, err := c.GetBytes(opCtx(t), key)
	require.NoError(t, err)
	require.Equal(t, want, string(v))
}

// requireValueOrMiss 允许读到 want 或未命中，读到其他值失败。
func requireValueOrMiss(t *testing.T, c Cache, key, want string) {
	t.Helper()
	v, err := c.GetBytes(opCtx(t), key)
	if err != nil {
		require.Truef(t, errors.Is(err, agentErrors.ErrCacheMiss), "GetBytes(%s) 意外错误: %v", key, err)
		return
	}
	require.Equal(t, want, string(v))
}

func caseReadWrite(t *testing.T, open func(*testing.T, string) Cache) {
	a, b := open(t, "x"), open(t, "x")
	require.NoError(t, a.SetBytes(opCtx(t), "k", []byte("v"), longTTL))
	requireValue(t, b, "k", "v")
}

func caseDeleteTagAcrossInstances(t *testing.T, open func(*testing.T, string) Cache) {
	a, b := open(t, "x"), open(t, "x")
	require.NoError(t, a.SetTagged(opCtx(t), "k", []byte("v"), longTTL, "s"))
	requireValue(t, b, "k", "v")
	require.NoError(t, b.DeleteTag(opCtx(t), "s"))
	requireMiss(t, a, "k")
	requireMiss(t, b, "k")
}

func caseIdempotentDelete(t *testing.T, open func(*testing.T, string) Cache) {
	a := open(t, "x")
	require.NoError(t, a.DeleteTag(opCtx(t), "never-existed"))
	require.NoError(t, a.SetTagged(opCtx(t), "k", []byte("v"), longTTL, "s"))
	require.NoError(t, a.DeleteTag(opCtx(t), "s"))
	require.NoError(t, a.DeleteTag(opCtx(t), "s"))
	requireMiss(t, a, "k")
}

func caseTagReplace(t *testing.T, open func(*testing.T, string) Cache) {
	a, b := open(t, "x"), open(t, "x")
	require.NoError(t, a.SetTagged(opCtx(t), "k", []byte("v1"), longTTL, "s1"))
	require.NoError(t, a.SetTagged(opCtx(t), "k", []byte("v2"), longTTL, "s2"))
	require.NoError(t, b.DeleteTag(opCtx(t), "s1"))
	requireValueOrMiss(t, a, "k", "v2")
	require.NoError(t, b.DeleteTag(opCtx(t), "s2"))
	requireMiss(t, a, "k")
}

func caseMultiTagDeleteOne(t *testing.T, open func(*testing.T, string) Cache) {
	a := open(t, "x")
	require.NoError(t, a.SetTagged(opCtx(t), "k", []byte("v"), longTTL, "s", "m"))
	require.NoError(t, a.DeleteTag(opCtx(t), "m"))
	requireMiss(t, a, "k")
}

// 带标签写入后被无标签 SetBytes 覆盖：DeleteTag 可以多删新值，但绝不能让旧的带标签值复活。
func caseUntaggedOverwrite(t *testing.T, open func(*testing.T, string) Cache) {
	a, b := open(t, "x"), open(t, "x")
	require.NoError(t, a.SetTagged(opCtx(t), "k", []byte("v1"), longTTL, "s"))
	require.NoError(t, a.SetBytes(opCtx(t), "k", []byte("v2"), longTTL))
	require.NoError(t, b.DeleteTag(opCtx(t), "s"))
	requireValueOrMiss(t, a, "k", "v2")
	requireValueOrMiss(t, b, "k", "v2")
}

// 条目只会在 TTL 之后消失；Redis 后端经网络运行，给足余量并轮询而不是固定等待。
func caseTTL(t *testing.T, open func(*testing.T, string) Cache) {
	a := open(t, "x")
	require.NoError(t, a.SetTagged(opCtx(t), "k", []byte("v"), 200*time.Millisecond, "s"))
	require.Eventually(t, func() bool {
		_, err := a.GetBytes(opCtx(t), "k")
		return errors.Is(err, agentErrors.ErrCacheMiss)
	}, 5*time.Second, 50*time.Millisecond, "TTL 过后条目应未命中")
	require.NoError(t, a.DeleteTag(opCtx(t), "s"))
}

func caseWriteAfterDelete(t *testing.T, open func(*testing.T, string) Cache) {
	a, b := open(t, "x"), open(t, "x")
	require.NoError(t, a.SetTagged(opCtx(t), "k", []byte("old"), longTTL, "s"))
	require.NoError(t, a.DeleteTag(opCtx(t), "s"))
	require.NoError(t, b.SetTagged(opCtx(t), "k", []byte("new"), longTTL, "s"))
	requireValue(t, a, "k", "new")
	requireValue(t, b, "k", "new")
}

// 8 个写者并发 SetTagged，另一协程不停 DeleteTag。
//   - 偶数写者写完后等到"写入返回之后才开始"的那次 DeleteTag 返回，此后该键只有自己写过，必须未命中（运行期检查）。
//   - 奇数写者只写不等，留给收尾阶段：写者停止后再做一次 DeleteTag，所有键必须未命中。
//
// 运行期检查只依赖 DeleteTag 的开始与返回顺序，对任何满足 S1 的后端都不会偶发失败。
func caseConcurrentNoMissedDelete(t *testing.T, open func(*testing.T, string) Cache) {
	const (
		writers       = 8
		keysPerWriter = 4
	)
	duration := 2 * time.Second
	if testing.Short() {
		duration = 200 * time.Millisecond
	}
	shared := open(t, "x")
	deleter := open(t, "x")

	var started, finished atomic.Int64
	stopDel := make(chan struct{})
	delDone := make(chan struct{})
	go func() {
		defer close(delDone)
		for {
			select {
			case <-stopDel:
				return
			default:
			}
			started.Add(1)
			if err := deleter.DeleteTag(opCtx(t), "s"); err != nil {
				t.Errorf("DeleteTag: %v", err)
				return
			}
			finished.Add(1)
		}
	}()

	deadline := time.Now().Add(duration)
	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := open(t, "x")
			for i := 0; time.Now().Before(deadline); i++ {
				key := fmt.Sprintf("w%d-k%d", w, i%keysPerWriter)
				if err := c.SetTagged(opCtx(t), key, []byte(fmt.Sprintf("v%d", i)), longTTL, "s"); err != nil {
					t.Errorf("SetTagged(%s): %v", key, err)
					return
				}
				if w%2 == 1 {
					continue
				}
				// 第 target 次 DeleteTag 一定在本次写入返回之后才开始。
				target := started.Load() + 1
				waitUntil := time.Now().Add(opWait)
				for finished.Load() < target {
					if time.Now().After(waitUntil) {
						t.Errorf("等待 DeleteTag 超时")
						return
					}
					time.Sleep(time.Millisecond)
				}
				_, err := c.GetBytes(opCtx(t), key)
				if !assert.Truef(t, errors.Is(err, agentErrors.ErrCacheMiss), "写入返回后开始的 DeleteTag 已返回，%s 仍可读: err=%v", key, err) {
					return
				}
			}
		}()
	}
	wg.Wait()
	close(stopDel)
	<-delDone
	if t.Failed() {
		return
	}

	require.NoError(t, shared.DeleteTag(opCtx(t), "s"))
	for w := range writers {
		for k := range keysPerWriter {
			requireMiss(t, shared, fmt.Sprintf("w%d-k%d", w, k))
		}
	}
}

func caseIsolation(t *testing.T, open func(*testing.T, string) Cache) {
	a, c := open(t, "x"), open(t, "y")
	require.NoError(t, a.SetTagged(opCtx(t), "k", []byte("a"), longTTL, "s"))
	require.NoError(t, c.SetTagged(opCtx(t), "k", []byte("c"), longTTL, "s"))
	requireValue(t, a, "k", "a")
	requireValue(t, c, "k", "c")
	require.NoError(t, a.DeleteTag(opCtx(t), "s"))
	requireMiss(t, a, "k")
	requireValue(t, c, "k", "c")
}
