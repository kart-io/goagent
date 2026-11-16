package cache

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/constants"
)

// BenchmarkSimpleCacheSetParallel 度量并发写入：配置与工具缓存（tools/middleware/caching.go）相同，
// 键空间 1024 个，低于容量上限，不触发淘汰；各协程交错覆盖同一批键。
func BenchmarkSimpleCacheSetParallel(b *testing.B) {
	ctx := context.Background()
	val := []byte("value")
	keys := make([]string, 1024)
	for i := range keys {
		keys[i] = "key-" + strconv.Itoa(i)
	}
	for _, bc := range []struct {
		name string
		set  func(c *SimpleCache, key string) error
	}{
		{"Set", func(c *SimpleCache, key string) error { return c.Set(ctx, key, val, time.Minute) }},
		{"SetTagged", func(c *SimpleCache, key string) error {
			return c.SetTagged(ctx, key, val, time.Minute, "session", "memory")
		}},
	} {
		b.Run(bc.name, func(b *testing.B) {
			c := NewSimpleCache(time.Minute, WithoutBackgroundCleanup(), WithMaxEntries(constants.DefaultMiddlewareCacheCapacity))
			defer c.Close()
			var next atomic.Int64
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := int(next.Add(1)) * 7919
				for pb.Next() {
					if err := bc.set(c, keys[i%len(keys)]); err != nil {
						b.Error(err)
						return
					}
					i++
				}
			})
		})
	}
}
