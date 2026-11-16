package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
)

func TestSimpleCacheTagged(t *testing.T) {
	ctx := context.Background()
	val := []byte("v")

	seed := func(t *testing.T, c *SimpleCache) {
		t.Helper()
		if err := c.SetTagged(ctx, "k1", val, time.Minute, "a", "b"); err != nil {
			t.Fatal(err)
		}
		if err := c.SetTagged(ctx, "k2", val, time.Minute, "b"); err != nil {
			t.Fatal(err)
		}
	}
	hit := func(t *testing.T, c *SimpleCache, key string) bool {
		t.Helper()
		_, err := c.Get(ctx, key)
		if err != nil && !errors.Is(err, agentErrors.ErrCacheMiss) {
			t.Fatalf("Get(%s) 意外错误: %v", key, err)
		}
		return err == nil
	}

	// mutated 写入 k1 后把传入的标签切片改掉，用于验证 SetTagged 不保留调用方的切片。
	mutated := func(t *testing.T, c *SimpleCache) {
		tags := []string{"a"}
		if err := c.SetTagged(ctx, "k1", val, time.Minute, tags...); err != nil {
			t.Fatal(err)
		}
		tags[0] = "x"
	}

	tests := []struct {
		name string
		run  func(t *testing.T, c *SimpleCache)
		// 期望的存活键与标签索引
		wantHit  map[string]bool
		wantTags map[string][]string
	}{
		{
			name: "删除标签a只影响k1",
			run: func(t *testing.T, c *SimpleCache) {
				seed(t, c)
				if err := c.DeleteTag(ctx, "a"); err != nil {
					t.Fatal(err)
				}
			},
			wantHit:  map[string]bool{"k1": false, "k2": true},
			wantTags: map[string][]string{"b": {"k2"}},
		},
		{
			name: "删除标签b删除k1与k2",
			run: func(t *testing.T, c *SimpleCache) {
				seed(t, c)
				if err := c.DeleteTag(ctx, "b"); err != nil {
					t.Fatal(err)
				}
			},
			wantHit:  map[string]bool{"k1": false, "k2": false},
			wantTags: map[string][]string{},
		},
		{
			name: "不存在的标签幂等",
			run: func(t *testing.T, c *SimpleCache) {
				seed(t, c)
				for range 2 {
					if err := c.DeleteTag(ctx, "none"); err != nil {
						t.Fatal(err)
					}
				}
			},
			wantHit:  map[string]bool{"k1": true, "k2": true},
			wantTags: map[string][]string{"a": {"k1"}, "b": {"k1", "k2"}},
		},
		{
			name: "重复删除同一标签幂等",
			run: func(t *testing.T, c *SimpleCache) {
				seed(t, c)
				for range 2 {
					if err := c.DeleteTag(ctx, "a"); err != nil {
						t.Fatal(err)
					}
				}
			},
			wantHit:  map[string]bool{"k1": false, "k2": true},
			wantTags: map[string][]string{"b": {"k2"}},
		},
		{
			name: "普通Delete后摘除标签索引且DeleteTag不误删",
			run: func(t *testing.T, c *SimpleCache) {
				seed(t, c)
				if err := c.Delete(ctx, "k1"); err != nil {
					t.Fatal(err)
				}
				if got := c.GetStats().Size; got != 1 {
					t.Fatalf("Delete 后条目数 = %d, 期望 1", got)
				}
				if err := c.DeleteTag(ctx, "a"); err != nil {
					t.Fatal(err)
				}
				if got := c.GetStats().Size; got != 1 {
					t.Fatalf("DeleteTag 后条目数 = %d, 期望 1", got)
				}
			},
			wantHit:  map[string]bool{"k1": false, "k2": true},
			wantTags: map[string][]string{"b": {"k2"}},
		},
		{
			name: "过期清理后摘除标签索引",
			run: func(t *testing.T, c *SimpleCache) {
				if err := c.SetTagged(ctx, "k1", val, time.Millisecond, "a"); err != nil {
					t.Fatal(err)
				}
				if err := c.SetTagged(ctx, "k2", val, time.Minute, "a"); err != nil {
					t.Fatal(err)
				}
				time.Sleep(5 * time.Millisecond)
				c.cleanupExpired()
				if got := c.GetStats().Size; got != 1 {
					t.Fatalf("清理后条目数 = %d, 期望 1", got)
				}
			},
			wantHit:  map[string]bool{"k1": false, "k2": true},
			wantTags: map[string][]string{"a": {"k2"}},
		},
		{
			name: "过期未清理的条目可按标签删除且不残留索引",
			run: func(t *testing.T, c *SimpleCache) {
				if err := c.SetTagged(ctx, "k1", val, time.Millisecond, "a"); err != nil {
					t.Fatal(err)
				}
				time.Sleep(5 * time.Millisecond)
				for range 2 {
					if err := c.DeleteTag(ctx, "a"); err != nil {
						t.Fatalf("删除过期条目的标签: %v", err)
					}
				}
				if got := c.GetStats().Size; got != 0 {
					t.Fatalf("DeleteTag 后条目数 = %d, 期望 0", got)
				}
				// 残留的 a→k1 索引会让下面的 DeleteTag 误删改挂标签 b 的 k1。
				if err := c.SetTagged(ctx, "k1", val, time.Minute, "b"); err != nil {
					t.Fatal(err)
				}
				if err := c.DeleteTag(ctx, "a"); err != nil {
					t.Fatal(err)
				}
			},
			wantHit:  map[string]bool{"k1": true},
			wantTags: map[string][]string{"b": {"k1"}},
		},
		{
			name: "同键换标签后旧标签不再关联",
			run: func(t *testing.T, c *SimpleCache) {
				seed(t, c)
				if err := c.SetTagged(ctx, "k1", val, time.Minute, "c"); err != nil {
					t.Fatal(err)
				}
				if err := c.DeleteTag(ctx, "a"); err != nil {
					t.Fatal(err)
				}
			},
			wantHit:  map[string]bool{"k1": true, "k2": true},
			wantTags: map[string][]string{"b": {"k2"}, "c": {"k1"}},
		},
		{
			name: "普通Set覆盖带标签条目后旧标签不再关联",
			run: func(t *testing.T, c *SimpleCache) {
				seed(t, c)
				if err := c.Set(ctx, "k1", "plain", time.Minute); err != nil {
					t.Fatal(err)
				}
				if err := c.DeleteTag(ctx, "b"); err != nil {
					t.Fatal(err)
				}
			},
			wantHit:  map[string]bool{"k1": true, "k2": false},
			wantTags: map[string][]string{},
		},
		{
			name: "Clear清空标签索引",
			run: func(t *testing.T, c *SimpleCache) {
				seed(t, c)
				if err := c.Clear(ctx); err != nil {
					t.Fatal(err)
				}
			},
			wantHit:  map[string]bool{"k1": false, "k2": false},
			wantTags: map[string][]string{},
		},
		{
			name: "容量淘汰后摘除标签索引",
			run: func(t *testing.T, c *SimpleCache) {
				c.maxEntries = 1
				if err := c.SetTagged(ctx, "k1", val, time.Minute, "a"); err != nil {
					t.Fatal(err)
				}
				if err := c.SetTagged(ctx, "k2", val, time.Minute, "a"); err != nil {
					t.Fatal(err)
				}
			},
			wantHit:  map[string]bool{"k1": false, "k2": true},
			wantTags: map[string][]string{"a": {"k2"}},
		},
		{
			name: "调用后修改传入切片，DeleteTag仍能删除",
			run: func(t *testing.T, c *SimpleCache) {
				mutated(t, c)
				if err := c.DeleteTag(ctx, "a"); err != nil {
					t.Fatal(err)
				}
			},
			wantHit:  map[string]bool{"k1": false},
			wantTags: map[string][]string{},
		},
		{
			name: "调用后修改传入切片，覆盖写入不残留旧标签",
			run: func(t *testing.T, c *SimpleCache) {
				mutated(t, c)
				if err := c.SetTagged(ctx, "k1", val, time.Minute, "b"); err != nil {
					t.Fatal(err)
				}
				if err := c.DeleteTag(ctx, "a"); err != nil {
					t.Fatal(err)
				}
			},
			wantHit:  map[string]bool{"k1": true},
			wantTags: map[string][]string{"b": {"k1"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewSimpleCache(time.Minute, WithoutBackgroundCleanup())
			defer c.Close()
			tt.run(t, c)

			for key, want := range tt.wantHit {
				if got := hit(t, c, key); got != want {
					t.Errorf("Get(%s) 命中 = %v, 期望 %v", key, got, want)
				}
			}
			c.tagMu.Lock()
			defer c.tagMu.Unlock()
			if len(c.tags) != len(tt.wantTags) {
				t.Errorf("标签索引 = %v, 期望 %v", c.tags, tt.wantTags)
			}
			for tag, keys := range tt.wantTags {
				set := c.tags[tag]
				if len(set) != len(keys) {
					t.Errorf("标签 %s 的键 = %v, 期望 %v", tag, set, keys)
				}
				for _, k := range keys {
					if _, ok := set[k]; !ok {
						t.Errorf("标签 %s 缺少键 %s", tag, k)
					}
				}
			}
		})
	}
}

func TestSimpleCacheTaggedConcurrent(t *testing.T) {
	ctx := context.Background()
	c := NewSimpleCache(time.Minute, WithoutBackgroundCleanup())
	defer c.Close()

	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := range 200 {
				key := fmt.Sprintf("k%d", (g*200+i)%20)
				_ = c.SetTagged(ctx, key, []byte("v"), time.Minute, "t1", fmt.Sprintf("t%d", i%3))
				_ = c.Delete(ctx, key)
			}
		}()
		go func() {
			defer wg.Done()
			for i := range 200 {
				_ = c.DeleteTag(ctx, fmt.Sprintf("t%d", i%3))
			}
		}()
	}
	wg.Wait()

	_ = c.DeleteTag(ctx, "t1")
	if got := c.GetStats().Size; got != 0 {
		t.Errorf("清理后条目数 = %d, 期望 0", got)
	}
	c.tagMu.Lock()
	defer c.tagMu.Unlock()
	if len(c.tags) != 0 {
		t.Errorf("残留标签索引: %v", c.tags)
	}
}
