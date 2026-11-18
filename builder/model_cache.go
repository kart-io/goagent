package builder

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/cache"
	"github.com/kart-io/goagent/core/observe"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
)

// EraseMode determines how erased content is physically deleted from the cache.
type EraseMode int

const (
	// EraseAuto physically deletes when the cache supports deletion by tag, and
	// otherwise falls back to TTL expiry.
	EraseAuto EraseMode = iota
	// EraseOff does not physically delete; erased content stays in the cache
	// until the TTL.
	EraseOff
	// EraseRequired requires physical deletion to be available; building fails when
	// the cache does not support it or the memory has no OnErase configured.
	EraseRequired
)

// ModelCache is the model call cache configuration. Worker processes do not go
// through the Builder and must wire OnErase with the same ModelCache (same
// backend, same TagKey). When the client neither declares a CacheIdentity nor
// yields a resolvable model name (the request's Model or the client's
// ModelName), that call is silently not cached.
type ModelCache struct {
	// Cache is the cache backend and only exchanges bytes; with EraseRequired
	// it must also implement cache.Tagged.
	Cache cache.Bytes
	// TTL must be greater than 0; with EraseRequired it must not exceed
	// memory.Config.EraseWithin (enforced only when long-term memory is
	// configured).
	TTL time.Duration
	// Scope returns a trusted isolation identity; returning false skips caching
	// for this call. It may be omitted when long-term memory is configured, in
	// which case it defaults to the bound namespace.
	Scope func(ctx context.Context) (string, bool)
	Erase EraseMode
	// TagKey is the HMAC key for tag digests; when tags are used it must be at
	// least 32 bytes.
	TagKey []byte
}

const minTagKeyLen = 32

// writeErrorDeleteTimeout 是写入失败后清理条目的删除超时。
const writeErrorDeleteTimeout = time.Second

// WithModelCache enables caching for model calls.
func (b *AgentBuilder[S]) WithModelCache(mc ModelCache) *AgentBuilder[S] {
	b.config.ModelCache = &mc
	return b
}

func (mc ModelCache) tagged() (cache.Tagged, bool) {
	t, ok := mc.Cache.(cache.Tagged)
	return t, ok
}

// Eraser returns the callback for memory.Config.OnErase; it is nil when Erase
// is EraseOff or the cache does not support deletion by tag.
func (mc ModelCache) Eraser() memory.EraseFunc {
	t, ok := mc.tagged()
	if !ok || mc.Erase == EraseOff {
		return nil
	}
	return func(ctx context.Context, ns []string, scope string) error {
		if err := mc.checkTagKey(); err != nil {
			return err
		}
		return t.DeleteTag(ctx, memoryTag(mc.TagKey, ns, scope))
	}
}

// DeleteSession deletes all cache entries of a session, for the host to call
// after deleting or rewinding a session; it does nothing when physical deletion
// is unavailable.
func (mc ModelCache) DeleteSession(ctx context.Context, sessionID string) error {
	t, ok := mc.tagged()
	if !ok || mc.Erase == EraseOff {
		return nil
	}
	if err := mc.checkTagKey(); err != nil {
		return err
	}
	return t.DeleteTag(ctx, sessionTag(mc.TagKey, sessionID))
}

// shortTagKey 是 TagKey 不足时的错误文案，Build、Validate 与标签删除共用。
const shortTagKey = "使用标签删除时 TagKey 至少 32 字节"

// checkTagKey 拦截 TagKey 不足的标签删除：Worker 不经过 Builder 校验，算出的标签与写入时对不上，删除会静默落空。
func (mc ModelCache) checkTagKey() error {
	if len(mc.TagKey) < minTagKeyLen {
		return agentErrors.NewInvalidConfigError("builder", "model_cache", shortTagKey)
	}
	return nil
}

// configRule 是一条配置规则：bad 成立时以 err 拒绝，或以 warn 告警。
type configRule struct {
	bad  func() bool
	err  string
	warn string
}

// rules 是与长期记忆无关的规则，Build 与 Validate 共用。
func (mc ModelCache) rules() []configRule {
	_, tagged := mc.tagged()
	return []configRule{
		{bad: func() bool { return mc.Cache == nil || mc.TTL <= 0 }, err: "Cache 不能为空且 TTL 必须大于 0"},
		{bad: func() bool { return mc.Erase == EraseRequired && !tagged }, err: "EraseRequired 要求缓存实现 cache.Tagged"},
		{bad: func() bool { return tagged && mc.Erase != EraseOff && len(mc.TagKey) < minTagKeyLen }, err: shortTagKey},
	}
}

// Validate checks the configuration unrelated to long-term memory: Cache is
// non-nil, TTL is greater than 0, the cache implements cache.Tagged under
// EraseRequired, and TagKey has sufficient length when tag deletion will be
// used. Workers do not go through the Builder and should call it at startup, so
// a misconfiguration does not cause repeated OnErase failures in every
// maintenance round.
func (mc ModelCache) Validate() error {
	return firstConfigError(mc.rules())
}

func firstConfigError(rules []configRule) error {
	for _, r := range rules {
		if r.err != "" && r.bad() {
			return agentErrors.NewInvalidConfigError("builder", "model_cache", r.err)
		}
	}
	return nil
}

func (b *AgentBuilder[S]) validateModelCache() error {
	mc := b.config.ModelCache
	if mc == nil {
		return nil
	}
	_, tagged := mc.tagged()
	m := b.config.LongTermMemory
	ttlOverErase := func() bool { return m != nil && mc.TTL > m.EraseWithin() }
	rules := append(mc.rules(), []configRule{
		{bad: func() bool { return mc.Scope == nil && m == nil }, err: "没有可信的隔离身份：未配置长期记忆时必须提供 Scope"},
		{bad: func() bool { return mc.Erase == EraseRequired && m != nil && !m.ErasesOnDelete() }, err: "EraseRequired 要求长期记忆配置 OnErase（用 ModelCache.Eraser()）"},
		{bad: func() bool { return mc.Erase == EraseRequired && ttlOverErase() }, err: "EraseRequired 要求 TTL 不超过 memory.Config.EraseWithin：物理删除失败时残留时长的上限就是 TTL"},
		{bad: func() bool { return mc.Erase == EraseAuto && !tagged }, warn: "缓存不支持按标签删除，被删内容的物理删除退回 TTL"},
		{bad: func() bool { return mc.Erase == EraseAuto && ttlOverErase() }, warn: "TTL 超过 memory.Config.EraseWithin，被删内容可能在缓存中残留到 TTL"},
	}...)
	if err := firstConfigError(rules); err != nil {
		return err
	}
	for _, r := range rules {
		if r.warn != "" && r.bad() {
			slog.Warn("模型调用缓存：" + r.warn)
		}
	}
	return nil
}

type modelCacheScopeKey struct{}

// modelCacheScope 是一次执行的缓存范围，由 Agent 在轮次开始时经 ctx 交给模型调用；不缓存时为 nil。
type modelCacheScope struct {
	cfg       ModelCache
	identity  string
	sessionID string
	ltm       *memory.Memory // 可为 nil
	read      [][]string     // 本轮 TurnMemory.Read
	hits      *atomic.Int64  // 本次执行的命中计数
	sent      *atomic.Int64  // 本次执行实际发出的模型请求数，含失败的请求
}

// withModelCacheScope 总是覆盖上层的值：工具里运行的子 Agent 不能沿用父 Agent 的范围与计数。
func withModelCacheScope(ctx context.Context, s *modelCacheScope) context.Context {
	return context.WithValue(ctx, modelCacheScopeKey{}, s)
}

// modelCacheScope 按配置、会话 ID 与本轮记忆绑定构造缓存范围；身份不可得时不缓存。
func (a *ConfigurableAgent[S]) modelCacheScope(ctx context.Context) *modelCacheScope {
	mc := a.config.ModelCache
	if mc == nil {
		return nil
	}
	m := a.config.LongTermMemory
	id, ok := mc.identity(ctx, m != nil)
	if !ok {
		return nil
	}
	s := &modelCacheScope{cfg: *mc, identity: id, sessionID: a.sessionLogID(ctx), hits: new(atomic.Int64), sent: new(atomic.Int64)}
	if m != nil {
		// 没有本轮绑定就无法读取记忆版本，删除类操作之后可能取到旧答案：不缓存。
		turn := sessionTurnFrom(ctx)
		if turn == nil || turn.memory == nil {
			return nil
		}
		s.ltm, s.read = m, turn.memory.Read
	}
	return s
}

// modelCacheTally 是本次执行命中数与真实请求数的快照；Agent 在回合前后各取一次，判断该回合是否完全来自缓存。
type modelCacheTally struct {
	hits, sent int64
}

// modelCacheCounts 返回当前快照；不缓存时 ok 为 false。
func modelCacheCounts(ctx context.Context) (t modelCacheTally, ok bool) {
	s, _ := ctx.Value(modelCacheScopeKey{}).(*modelCacheScope)
	if s == nil {
		return modelCacheTally{}, false
	}
	return modelCacheTally{hits: s.hits.Load(), sent: s.sent.Load()}, true
}

// cachedSince 报告 before 之后的回合是否完全来自缓存：有命中且没有发出真实请求。
// 回合内先有真实请求失败、再换客户端命中时，失败的请求可能已消耗 token，按真实调用计。
func (t modelCacheTally) cachedSince(before modelCacheTally) bool {
	return t.hits > before.hits && t.sent == before.sent
}

// cacheRequest 是计算缓存键所需的请求内容；client 为 nil 时不缓存。
type cacheRequest struct {
	client llm.Client
	req    *llm.CompletionRequest
	tools  []interfaces.Tool
}

// modelCacheEntry 是一次模型调用对应的条目：键与记忆版本在查找前确定，写入时复用。
type modelCacheEntry struct {
	scope   *modelCacheScope
	key     string
	version []memory.NamespaceVersion
}

// resolveModelCache 计算本次调用的条目；不满足缓存条件或读取记忆版本失败时返回 nil，本次既不查也不写。
func resolveModelCache(ctx context.Context, cr cacheRequest) *modelCacheEntry {
	s, _ := ctx.Value(modelCacheScopeKey{}).(*modelCacheScope)
	if s == nil || cr.client == nil || cr.req == nil {
		return nil
	}
	client, ok := clientIdentity(cr.client, cr.req)
	if !ok {
		return nil
	}
	version, err := s.memoryVersion(ctx)
	if err != nil {
		observe.Emit(ctx, observe.Event{Type: "model_cache_read_error", Path: "cache", ErrorKind: observe.ErrorKind(err)})
		return nil
	}
	key, ok := modelCacheKey(cacheKeyInput{Identity: s.identity, SessionID: s.sessionID, Client: client, Request: cr.req, Tools: cr.tools, Memory: version})
	if !ok {
		return nil
	}
	return &modelCacheEntry{scope: s, key: key, version: version}
}

func (s *modelCacheScope) memoryVersion(ctx context.Context) ([]memory.NamespaceVersion, error) {
	if s.ltm == nil || len(s.read) == 0 {
		return nil, nil
	}
	return s.ltm.CacheVersion(ctx, s.read)
}

// cacheableTurn 是写入与命中共用的条件：正常结束、通过 cacheable 校验且不含 ProviderState。
func cacheableTurn(turn *llm.StreamChunk, cacheable func(*llm.StreamChunk) error) error {
	if turn.FinishReason != "stop" && turn.FinishReason != "tool_calls" {
		return agentErrors.Newf(agentErrors.CodeInvalidOutput, "未正常结束的模型回合不缓存").WithComponent("builder").WithOperation("model_cache")
	}
	if turn.ProviderState != nil {
		return agentErrors.Newf(agentErrors.CodeInvalidOutput, "带 ProviderState 的模型回合不缓存").WithComponent("builder").WithOperation("model_cache")
	}
	return cacheable(turn)
}

// lookup 返回可用的缓存回合。查找出错按未命中处理；值损坏或未通过 cacheable 时删除该条目并按未命中处理。
func (e *modelCacheEntry) lookup(ctx context.Context, cacheable func(*llm.StreamChunk) error) *llm.StreamChunk {
	b, err := e.scope.cfg.Cache.GetBytes(ctx, e.key)
	if errors.Is(err, agentErrors.ErrCacheMiss) {
		observe.Emit(ctx, observe.Event{Type: "model_cache_miss", Path: "cache"})
		return nil
	}
	if err != nil && !agentErrors.IsCode(err, agentErrors.CodeInvalidInput) {
		observe.Emit(ctx, observe.Event{Type: "model_cache_read_error", Path: "cache", ErrorKind: observe.ErrorKind(err)})
		return nil
	}
	var turn *llm.StreamChunk
	if err == nil {
		turn, err = decodeCachedTurn(b)
	}
	if err == nil {
		err = cacheableTurn(turn, cacheable)
	}
	if err != nil {
		observe.Emit(ctx, observe.Event{Type: "model_cache_invalid", Path: "cache", ErrorKind: observe.ErrorKind(err)})
		e.deleteKey(ctx)
		return nil
	}
	e.scope.hits.Add(1)
	observe.Emit(ctx, observe.Event{Type: "model_cache_hit", Path: "cache", Cached: true})
	return turn
}

// store 写入真实调用的结果。写入前重读记忆版本：与键中的不同说明调用期间发生了删除，放弃写入。
// 写入后再复核一次：复核与写入之间发生的删除可能早于标签登记而落空，此时删除刚写入的条目。
// 失败只记录，不影响调用方。
func (e *modelCacheEntry) store(ctx context.Context, turn *llm.StreamChunk, cacheable func(*llm.StreamChunk) error) {
	if cacheableTurn(turn, cacheable) != nil {
		return
	}
	if same, err := e.recheck(ctx); !same {
		observe.Emit(ctx, observe.Event{Type: "model_cache_write_skipped", Path: "cache", ErrorKind: observe.ErrorKind(err)})
		return
	}
	// 命中不消耗 token：缓存值不带用量，命中时不计入用量报告。
	stored := *turn
	stored.Usage, stored.Error = nil, nil
	data, err := encodeCachedTurn(&stored)
	if err == nil {
		err = e.write(ctx, data)
	}
	if err != nil {
		observe.Emit(ctx, observe.Event{Type: "model_cache_write_error", Path: "cache", ErrorKind: observe.ErrorKind(err)})
		// 超时的写入可能已在服务端生效；ctx 已超时或取消时原 ctx 的删除必然失败，改用脱离取消的短超时 ctx。
		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeErrorDeleteTimeout)
		defer cancel()
		e.deleteKey(dctx)
		return
	}
	if same, err := e.recheck(ctx); !same {
		observe.Emit(ctx, observe.Event{Type: "model_cache_write_reverted", Path: "cache", ErrorKind: observe.ErrorKind(err)})
		e.deleteKey(ctx)
	}
}

// deleteKey 删除本条目的键，失败只记录事件。
func (e *modelCacheEntry) deleteKey(ctx context.Context) {
	if err := e.scope.cfg.Cache.Delete(ctx, e.key); err != nil {
		observe.Emit(ctx, observe.Event{Type: "model_cache_delete_error", Path: "cache", ErrorKind: observe.ErrorKind(err)})
	}
}

// recheck 重读记忆版本，返回是否与键中的相同；读取失败视为不同并返回该错误。
func (e *modelCacheEntry) recheck(ctx context.Context) (bool, error) {
	now, err := e.scope.memoryVersion(ctx)
	if err != nil {
		return false, err
	}
	return slices.EqualFunc(now, e.version, func(a, b memory.NamespaceVersion) bool {
		return slices.Equal(a.Namespace, b.Namespace) && a.Scope == b.Scope && a.LastForgetSeq == b.LastForgetSeq && a.LastRewindSeq == b.LastRewindSeq
	}), nil
}

// write 在物理删除可用时带会话与各读命名空间的标签写入，否则普通写入。
func (e *modelCacheEntry) write(ctx context.Context, data []byte) error {
	cfg := e.scope.cfg
	t, ok := cfg.tagged()
	if !ok || cfg.Erase == EraseOff {
		return cfg.Cache.SetBytes(ctx, e.key, data, cfg.TTL)
	}
	tags := make([]string, 0, 1+len(e.version))
	tags = append(tags, sessionTag(cfg.TagKey, e.scope.sessionID))
	for _, v := range e.version {
		tags = append(tags, memoryTag(cfg.TagKey, v.Namespace, v.Scope))
	}
	return t.SetTagged(ctx, e.key, data, cfg.TTL, tags...)
}
