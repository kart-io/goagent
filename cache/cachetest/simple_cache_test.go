package cachetest_test

import (
	"sync"
	"testing"
	"time"

	"github.com/kart-io/goagent/cache"
	"github.com/kart-io/goagent/cache/cachetest"
)

func TestSimpleCache(t *testing.T) {
	var mu sync.Mutex
	pools := map[*testing.T]map[string]*cache.SimpleCache{}

	cachetest.RunTagged(t, func(t *testing.T, name string) cachetest.Cache {
		mu.Lock()
		defer mu.Unlock()
		pool, ok := pools[t]
		if !ok {
			pool = map[string]*cache.SimpleCache{}
			pools[t] = pool
		}
		c, ok := pool[name]
		if !ok {
			c = cache.NewSimpleCache(time.Minute, cache.WithoutBackgroundCleanup())
			t.Cleanup(c.Close)
			pool[name] = c
		}
		return c
	})
}
