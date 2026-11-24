package cache

import (
	"context"
	"slices"
	"time"

	"github.com/kart-io/goagent/interfaces"
)

// Tagged is the tag-deletion cache capability. It is an alias of
// interfaces.TaggedCache, which documents the consistency contract.
type Tagged = interfaces.TaggedCache

// SimpleCache 在 tagMu 下写入条目并登记标签，DeleteTag 同样持有 tagMu，S2 在 DeleteTag 返回时成立；
// 不带标签的 Set 不取锁，只可能让 DeleteTag 多删。
var _ Tagged = (*SimpleCache)(nil)

// SetTagged 写入条目并关联标签。标签切片会被复制，调用方之后修改它不影响索引。
func (c *SimpleCache) SetTagged(ctx context.Context, key string, value []byte, ttl time.Duration, tags ...string) error {
	return c.set(key, value, ttl, slices.Clone(tags))
}

// DeleteTag 删除所有关联该标签的条目。
func (c *SimpleCache) DeleteTag(ctx context.Context, tag string) error {
	c.tagMu.Lock()
	defer c.tagMu.Unlock()

	for key := range c.tags[tag] {
		if v, ok := c.data.LoadAndDelete(key); ok {
			c.size.Add(-1)
			c.untag(key, v.(*cacheEntry).tags)
		}
	}
	delete(c.tags, tag)
	return nil
}

// tag 把 key 登记到各标签下，调用方须持有 tagMu。
func (c *SimpleCache) tag(key string, tags []string) {
	for _, t := range tags {
		set := c.tags[t]
		if set == nil {
			set = make(map[string]struct{})
			c.tags[t] = set
		}
		set[key] = struct{}{}
	}
}

// untagStale 摘除被无锁 Set 覆盖的旧条目留下的索引，保留当前条目仍带的标签；调用方须持有 tagMu。
// 带标签条目只在锁内写入，锁内读到的当前条目就是 key 下唯一可能可读的带标签条目，保留它的标签即不会漏删。
func (c *SimpleCache) untagStale(key string, tags []string) {
	var keep []string
	if v, ok := c.data.Load(key); ok {
		keep = v.(*cacheEntry).tags
	}
	for _, t := range tags {
		if !slices.Contains(keep, t) {
			c.untag(key, []string{t})
		}
	}
}

// untag 把 key 从各标签下摘除，空标签一并删除；调用方须持有 tagMu。
func (c *SimpleCache) untag(key string, tags []string) {
	for _, t := range tags {
		set := c.tags[t]
		delete(set, key)
		if len(set) == 0 {
			delete(c.tags, t)
		}
	}
}
