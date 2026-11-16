package builder

import (
	"context"
	"log/slog"
	"time"

	"github.com/kart-io/goagent/cache"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/memory"
)

// EraseMode 决定被删内容在缓存中的物理删除方式。
type EraseMode int

const (
	// EraseAuto 在缓存支持按标签删除时物理删除，否则退回 TTL 过期。
	EraseAuto EraseMode = iota
	// EraseOff 不做物理删除，被删内容在缓存中保留到 TTL。
	EraseOff
	// EraseRequired 要求物理删除可用，缓存不支持或记忆未配置 OnErase 时构建失败。
	EraseRequired
)

// ModelCache 是模型调用缓存的配置。Worker 进程不经过 Builder，须用同一个 ModelCache（同一后端、同一 TagKey）装配 OnErase。
type ModelCache struct {
	Cache cache.Cache
	// TTL 必须大于 0；EraseRequired 时不得超过 memory.Config.EraseWithin。
	TTL time.Duration
	// Scope 返回可信的隔离身份，返回 false 时本次不缓存；配置了长期记忆时可省略，默认取绑定的命名空间。
	Scope func(ctx context.Context) (string, bool)
	Erase EraseMode
	// TagKey 是标签摘要的 HMAC 密钥，使用标签时至少 32 字节。
	TagKey []byte
}

const minTagKeyLen = 32

// WithModelCache 为模型调用启用缓存。
func (b *AgentBuilder[S]) WithModelCache(mc ModelCache) *AgentBuilder[S] {
	b.config.ModelCache = &mc
	return b
}

func (mc ModelCache) tagged() (cache.Tagged, bool) {
	t, ok := mc.Cache.(cache.Tagged)
	return t, ok
}

// Eraser 返回供 memory.Config.OnErase 使用的回调；Erase 为 EraseOff 或缓存不支持按标签删除时为 nil。
func (mc ModelCache) Eraser() memory.EraseFunc {
	t, ok := mc.tagged()
	if !ok || mc.Erase == EraseOff {
		return nil
	}
	return func(ctx context.Context, ns []string, scope string) error {
		return t.DeleteTag(ctx, memoryTag(mc.TagKey, ns, scope))
	}
}

// DeleteSession 删除一个会话的全部缓存条目，供宿主在删除或回退会话后调用；物理删除不可用时什么也不做。
func (mc ModelCache) DeleteSession(ctx context.Context, sessionID string) error {
	t, ok := mc.tagged()
	if !ok || mc.Erase == EraseOff {
		return nil
	}
	return t.DeleteTag(ctx, sessionTag(mc.TagKey, sessionID))
}

func (b *AgentBuilder[S]) validateModelCache() error {
	mc := b.config.ModelCache
	if mc == nil {
		return nil
	}
	_, tagged := mc.tagged()
	m := b.config.LongTermMemory
	ttlOverErase := func() bool { return m != nil && mc.TTL > m.EraseWithin() }
	rules := []struct {
		bad  func() bool
		err  string
		warn string
	}{
		{bad: func() bool { return mc.Cache == nil || mc.TTL <= 0 }, err: "Cache 不能为空且 TTL 必须大于 0"},
		{bad: func() bool { return mc.Scope == nil && m == nil }, err: "没有可信的隔离身份：未配置长期记忆时必须提供 Scope"},
		{bad: func() bool { return mc.Erase == EraseRequired && !tagged }, err: "EraseRequired 要求缓存实现 cache.Tagged"},
		{bad: func() bool { return mc.Erase == EraseRequired && m != nil && !m.ErasesOnDelete() }, err: "EraseRequired 要求长期记忆配置 OnErase（用 ModelCache.Eraser()）"},
		{bad: func() bool { return mc.Erase == EraseRequired && ttlOverErase() }, err: "EraseRequired 要求 TTL 不超过 memory.Config.EraseWithin：物理删除失败时残留时长的上限就是 TTL"},
		{bad: func() bool { return tagged && mc.Erase != EraseOff && len(mc.TagKey) < minTagKeyLen }, err: "使用标签删除时 TagKey 至少 32 字节"},
		{bad: func() bool { return mc.Erase == EraseAuto && !tagged }, warn: "缓存不支持按标签删除，被删内容的物理删除退回 TTL"},
		{bad: func() bool { return mc.Erase != EraseRequired && ttlOverErase() }, warn: "TTL 超过 memory.Config.EraseWithin，被删内容可能在缓存中残留到 TTL"},
	}
	for _, r := range rules {
		if r.err != "" && r.bad() {
			return agentErrors.NewInvalidConfigError("builder", "model_cache", r.err)
		}
	}
	for _, r := range rules {
		if r.warn != "" && r.bad() {
			slog.Warn("模型调用缓存：" + r.warn)
		}
	}
	return nil
}
