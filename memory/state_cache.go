package memory

import (
	"container/list"
	"sync"
)

// stateCache 是进程内的 LRU，按估计的堆占用限制总量（G1 3.5、增量提交 5.1）。每个命名空间一个条目，含两级：
// 已解码的基础快照（按 base 的 commit_id 采用）与已还原的 state（按 head 的 commit_id 采用）。
// 它只省去解码与还原，从不省去 Load(head)：跨进程的一致性与不用缓存时相同。条目中的 state 由多个读者共享，只读。
type stateCache struct {
	mu       sync.Mutex
	capacity int
	size     int
	order    *list.List // 前端最近使用，元素为 *cacheEntry
	items    map[string]*list.Element
	stats    cacheStats
}

// cacheEntry 是一个命名空间的缓存条目，放入之后不再修改，由读者共享。
type cacheEntry struct {
	key, scope string
	ref        baseRef
	base       state
	baseIdx    *lazyIndex // 基础快照的索引（解码时在后台构建），可为 nil
	headCommit string
	cur        state
	ov         overlay
	ovRaw      int
	idx        *recallIndex // cur 的查询结构：baseIdx 加覆盖层
	bytes      int
}

// cacheStats 是缓存的计数，供基准与测试观察。
type cacheStats struct {
	Hits, Misses, BaseHits, Evictions int
}

// heapFactor 是解码后 state 的堆占用与解压后字节数之比的估计（G1 7.3 实测 1.7～2.3，取上浮的值）。
const heapFactor = 2.5

// newStateCache 返回容量为 capacity 字节的缓存；capacity <= 0 时返回 nil，表示不缓存（nil 的方法都可调用）。
func newStateCache(capacity int) *stateCache {
	if capacity <= 0 {
		return nil
	}
	return &stateCache{capacity: capacity, order: list.New(), items: map[string]*list.Element{}}
}

// head 返回命名空间 key 在作用域 scope 中、head 为 commit 的条目。
func (c *stateCache) head(key, scope, commit string) (*cacheEntry, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, found := c.items[key]; found {
		e := el.Value.(*cacheEntry)
		if e.scope == scope && e.headCommit == commit {
			c.order.MoveToFront(el)
			c.stats.Hits++
			return e, true
		}
	}
	c.stats.Misses++
	return nil, false
}

// base 返回命名空间 key 在作用域 scope 中、基础快照 commit_id 为 commit 的条目（只用其中的 base 与 baseIdx）。
func (c *stateCache) base(key, scope, commit string) (*cacheEntry, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, found := c.items[key]; found {
		e := el.Value.(*cacheEntry)
		if e.scope == scope && e.ref.CommitID == commit {
			c.order.MoveToFront(el)
			c.stats.BaseHits++
			return e, true
		}
	}
	return nil, false
}

// put 缓存 e；同一命名空间只保留一份，作用域相同时不以旧提交覆盖新提交。超过容量的条目不缓存，并移出旧条目。
// 占用按估计计：基础快照 + 它的索引 + 还原出的 state 复制的映射（覆盖层非空时）+ 覆盖层。
func (c *stateCache) put(e *cacheEntry) {
	if c == nil {
		return
	}
	e.bytes = int(heapFactor * float64(e.ref.RawBytes))
	if e.baseIdx != nil {
		e.bytes += estIndexBytesPerRecord * len(e.base.Records)
	}
	if e.ov.items() > 0 {
		e.bytes += int(heapFactor * (float64(e.ref.RawBytes)/2 + 2*float64(e.ovRaw)))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[e.key]; ok {
		old := el.Value.(*cacheEntry)
		if old.scope == e.scope && old.cur.Seq > e.cur.Seq {
			return
		}
		c.remove(el)
	}
	if e.bytes > c.capacity {
		return
	}
	c.items[e.key] = c.order.PushFront(e)
	c.size += e.bytes
	for c.size > c.capacity {
		c.remove(c.order.Back())
		c.stats.Evictions++
	}
}

// drop 移出命名空间 key 的条目（提交结果未知时使用）。
func (c *stateCache) drop(key string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.remove(el)
	}
}

func (c *stateCache) remove(el *list.Element) {
	e := c.order.Remove(el).(*cacheEntry)
	delete(c.items, e.key)
	c.size -= e.bytes
}

// snapshot 返回计数与当前估计的堆占用。
func (c *stateCache) snapshot() (cacheStats, int) {
	if c == nil {
		return cacheStats{}, 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats, c.size
}
