package middleware

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kart-io/goagent/constants"
	"github.com/stretchr/testify/require"
)

// 计时只保留最近的窗口，平均值仍按全部记录计算。
func TestTimingMiddlewareKeepsBoundedWindow(t *testing.T) {
	mw := NewTimingMiddleware()
	n := constants.DefaultTimingWindow + 100
	for i := 1; i <= n; i++ {
		_, err := mw.OnAfter(context.Background(), &MiddlewareResponse{Duration: time.Duration(i)})
		require.NoError(t, err)
	}
	require.Len(t, mw.GetTimings(), constants.DefaultTimingWindow)
	require.Equal(t, time.Duration((n+1)/2), mw.GetAverageLatency())
}

// 缓存条目数有上限；写入时先回收过期条目，读到过期条目时删除。
func TestCacheMiddlewareBoundedAndReclaimsExpired(t *testing.T) {
	store := func(mw *CacheMiddleware, i int) {
		mw.storeResponse(fmt.Sprint(i), &MiddlewareResponse{Output: i, Metadata: map[string]interface{}{}})
	}
	mw := NewCacheMiddlewareWithShards(time.Minute, 4)
	for i := range constants.DefaultMiddlewareCacheCapacity * 2 {
		store(mw, i)
	}
	require.LessOrEqual(t, mw.Size(), constants.DefaultMiddlewareCacheCapacity)

	mw = NewCacheMiddlewareWithShards(time.Millisecond, 4)
	for i := range 100 {
		store(mw, i)
	}
	time.Sleep(5 * time.Millisecond)
	for i := range 100 {
		_, err := mw.OnBefore(context.Background(), &MiddlewareRequest{Input: i})
		require.NoError(t, err)
	}
	require.Zero(t, mw.Size(), "读到的过期条目应被删除")
}
