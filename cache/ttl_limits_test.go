package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSimpleCacheZeroTTLNeverExpires(t *testing.T) {
	for name, lazy := range map[string]bool{"后台清理": false, "惰性清理": true} {
		t.Run(name, func(t *testing.T) {
			var opts []SimpleCacheOption
			if lazy {
				opts = append(opts, WithoutBackgroundCleanup())
			}
			c := NewSimpleCache(0, opts...)
			defer c.Close()
			ctx := context.Background()
			require.NoError(t, c.Set(ctx, "forever", 1, 0))
			require.NoError(t, c.Set(ctx, "short", 2, time.Millisecond))
			time.Sleep(5 * time.Millisecond)
			require.NoError(t, c.Set(ctx, "trigger", 3, 0))
			v, err := c.Get(ctx, "forever")
			require.NoError(t, err)
			require.Equal(t, 1, v)
			_, err = c.Get(ctx, "short")
			require.Error(t, err, "单独指定的 ttl 仍然生效")
		})
	}
}
