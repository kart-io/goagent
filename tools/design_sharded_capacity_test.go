package tools

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func TestDesignShardedCapacityBounds(t *testing.T) {
	for _, capacity := range []int{1, 3, 10, 65} {
		t.Run(fmt.Sprintf("容量%d", capacity), func(t *testing.T) {
			c := NewShardedToolCache(ShardedCacheConfig{ShardCount: 32, Capacity: capacity})
			defer c.Close()
			for i := 0; i < 256; i++ {
				require.NoError(t, c.Set(context.Background(), fmt.Sprintf("tool:key:%d", i), &interfaces.ToolOutput{Success: true, Result: i}, time.Minute))
				require.LessOrEqual(t, c.Size(), capacity, "缓存条目总数不能超过公开配置容量")
			}
		})
	}
}

func TestDesignShardedDefaultTTL(t *testing.T) {
	c := NewShardedToolCacheWithOptions(WithDefaultTTL(time.Minute))
	defer c.Close()
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "default-ttl", &interfaces.ToolOutput{Success: true, Result: "有效"}, 0))
	out, ok := c.Get(ctx, "default-ttl")
	require.True(t, ok, "零TTL应使用配置的默认有效期")
	require.Equal(t, "有效", out.Result)
}
