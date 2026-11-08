package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLazyCleanupReclaimsExpiredEntriesOnWrite(t *testing.T) {
	c := NewSimpleCache(time.Minute, WithoutBackgroundCleanup())
	defer c.Close()
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "expired", "旧值", time.Nanosecond))
	c.nextCleanup.Store(0)
	require.NoError(t, c.Set(ctx, "fresh", "新值", time.Minute))
	size := 0
	c.data.Range(func(key, value any) bool { size++; return true })
	require.Equal(t, 1, size, "新写入需要回收已过期的其他键")
	value, err := c.Get(ctx, "fresh")
	require.NoError(t, err)
	require.Equal(t, "新值", value)
}
