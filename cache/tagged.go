package cache

import (
	"context"
	"slices"
	"time"
)

// Tagged 是支持按标签批量删除的缓存能力，与 Cache 独立，由后端按需实现。
//
// 实现要求：
//   - DeleteTag 必须幂等：标签不存在、重复调用、条目已被删除或过期都返回 nil。
//   - 外部后端（如 Redis）里标签集合的过期时间不得短于其成员条目的 TTL，否则标签先过期会让条目无法按标签删除。
//   - 标签关联的条目很多时应分批删除，避免单次操作长时间阻塞后端。
type Tagged interface {
	// SetTagged 写入条目并关联标签。对同一个键重复调用时，新标签集合整体替换旧的。
	SetTagged(ctx context.Context, key string, value []byte, ttl time.Duration, tags ...string) error

	// DeleteTag 删除所有关联该标签的条目。
	DeleteTag(ctx context.Context, tag string) error
}

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
