# Caching Guide

[English](CACHING_GUIDE.md) | [中文](CACHING_GUIDE_CN.md)

GoAgent does not cache any model answer or tool result by default (`ConfigureForRAG` does not cache either). When you need caching, pick the component by what you cache:

| What is cached | Component | Notes |
|---|---|---|
| Generic key-value | `cache.Cache`; recommended `cache.NewSimpleCache(ttl)`; use `cache.WithoutBackgroundCleanup()` when there is no background lifecycle entry point | `sync.Map` + TTL with background cleanup; `NoOpCache` is the empty implementation. See [cache/README.md](../../cache/README.md) for the package |
| Results of a single tool | `tools/middleware.Caching` (via `middleware.Wrap`) | Backed by any `cache.Cache`; see [Tool Middleware](TOOL_MIDDLEWARE.md#built-in-middleware) |
| A single model call → answer | `builder.WithModelCache(builder.ModelCache{...})` | Keyed by the full request and isolated by the isolation identity the host declares; a hit only saves one model call, and the session log is still written. See [Model Call Caching](#model-call-caching) |

## Generic Key-Value Cache

```go
c := cache.NewSimpleCache(5 * time.Minute) // 5 minutes when ttl <= 0
defer c.Close()

_ = c.Set(ctx, "key", "value", 10*time.Minute) // uses the TTL given at construction when ttl <= 0
value, err := c.Get(ctx, "key")
if errors.Is(err, agentErrors.ErrCacheMiss) {
    // miss or expired
}
stats := c.GetStats() // Hits, Misses, Size, HitRate
```

`cache.NewCacheKeyGenerator(prefix)` generates keys with SHA256 (`GenerateKey(prompt, params)`, `GenerateKeySimple(parts...)`). `cache.NewCacheFromConfig(cfg)` returns `NoOpCache` when `Enabled=false`, otherwise a `SimpleCache` constructed with `DefaultTTL`. `CacheConfig` contains only the fields that actually take effect, `Enabled` and `DefaultTTL`; the former `Type`, `MaxSize`, and `CleanupInterval` fields have been removed, and strict config decoding rejects the corresponding old keys.

`InMemoryCache`, `LRUCache`, `MultiTierCache`, and their constructors have been removed, with no compatibility aliases:

| Old | New |
|---|---|
| `cache.NewInMemoryCache(maxSize, ttl, cleanup)` | `cache.NewSimpleCache(ttl)`; use `cache.WithoutBackgroundCleanup()` when there is no background lifecycle entry point |
| `cache.NewLRUCache(maxSize, ttl, cleanup)` | When only TTL is needed, use `cache.NewSimpleCache(ttl)`; when you need capacity and LRU semantics, inject a `cache.Cache` that explicitly implements those contracts |
| `cache.NewMultiTierCache(tiers...)` | When only a single TTL tier is needed, use `cache.NewSimpleCache(ttl)`; when you need multi-tier semantics, compose them explicitly and implement `cache.Cache` |

`SimpleCache` provides no capacity limit, LRU, or multi-tier backfill semantics, so you cannot mechanically replace the constructors above and still claim those policies are kept. `NoOpCache.Get` returns the central `errors.ErrCacheDisabled`, and `Set` stores nothing. For other interface cleanup, see the [Compatibility Entry Point Migration Guide](COMPATIBILITY_CLEANUP.md).

## Tool Result Caching

```go
cachedTool := middleware.Wrap(originalTool, middleware.Caching(
    middleware.WithCache(cache.NewSimpleCache(10*time.Minute)),
    middleware.WithTTL(10*time.Minute),
))
```

- Only successful results are cached; the key consists of the tool name and the full SHA256 of the arguments, and is isolated by session (`tools.WithSessionID`, set by the Builder from `SessionID`) and `ToolInput.CallerID`. Other dimensions that affect the result, such as tenant or knowledge version, must still be put into the key with `WithCacheKeyFunc`, or not cached.
- Do not cache tools with side effects: on a hit the tool does not run.
- Durable runs already rely on the Dispatcher call journal to replay on recovery without re-executing; tool caching addresses repeated computation across runs, and the two are unrelated. Wrapped tools do not forward optional interfaces such as `DispatchTimeoutProvider`; for caveats see [Tool Middleware](TOOL_MIDDLEWARE.md#middlewarewrap).

## Model Call Caching

```go
mc := builder.ModelCache{
    Cache:  c,                // cache.Bytes; physical deletion also requires cache.Tagged (SimpleCache implements it)
    TTL:    5 * time.Minute,  // must be greater than 0
    Scope:  func(ctx context.Context) (string, bool) { return tenantUser(ctx) },
    Erase:  builder.EraseAuto, // EraseOff / EraseAuto (default) / EraseRequired
    TagKey: tagKey,           // HMAC key for tag digests; at least 32 bytes when tags are used
}
agent, err := builder.NewSimpleBuilder(client).WithModelCache(mc).Build()
```

`Scope` returns a trusted isolation identity (tenant, user); when it returns `false` or an empty string, this call neither reads nor writes the cache. The session ID cannot serve as the basis for isolation: the caller sets it arbitrarily, and when it is omitted, all users sharing the same Agent get the same value. When long-term memory is configured, `Scope` can be omitted and defaults to the namespace bound by `memory.WithNamespace`; otherwise it is required.

The key is the SHA-256 of: the isolation identity, the session ID, the client identity and resolved model name, the full request (including recalled blocks and sampling parameters), the normalized tool definitions, and the long-term memory version. A change in any of them is a miss. A client can implement `CacheIdentity() string` to declare its own identity (base URL, deployment name, etc.); without a declaration, the provider plus model name is used (the request's `Model`, or the client's `ModelName()` when it is empty). When neither can be obtained, this call neither reads nor writes the cache, with no error or event.

### When It Applies

With a session log, the next request in the same session contains the previous turn's question and answer, so it does not hit. Hits mainly happen in:

- Stateless Agents without a session log: the same question repeated under the same identity, such as classification, extraction, and routing with fixed prompts;
- Repeated submissions with the same history under the same identity, such as a user regenerating or a frontend retrying; when long-term memory is configured, regeneration does not hit: you must call `session.Rewind` before regenerating, and the subsequent `Memory.Rewound` changes the memory version even if no record is affected;
- Two identical intermediate requests within a tool loop.

Crash recovery of durable runs is covered by durable journal replay and does not depend on the cache. Do not expect a high hit rate for stateful conversations. The following answers are not cached: errors, truncation, empty answers, invalid tool calls, and answers carrying `ProviderState` (so reasoning models with thinking signatures enabled do not use the cache). When two identical requests arrive concurrently, each calls the model once; they are not merged.

### Behavior on a Hit

A hit consumes no tokens, does not use budget, and is not accounted; the turn's `Usage` is the zero value. Callers determine whether there was a hit from:

- the `Cached` flag on observe events;
- the output metadata `model_cache_hits`.

Usage-related metadata is evaluated per round. "Fully from cache" means the round had a hit and sent no real request:

- `model_calls` is the number of logical rounds and does not count rounds fully from cache; retries within the same round are not counted separately.
- When the primary client request in a round fails and, after switching clients via RequestError, the round hits the cache, the failed request may have consumed tokens, so the round is counted in `model_calls` as a real round; it has no usage report, so `usage_complete` may be false. Such a round is counted in both `model_calls` and `model_cache_hits`, and their sum does not equal the total number of rounds.
- When the final answer comes fully from cache, `answer_usage_reported` is true: a hit has no usage to report, which is not treated as missing.

The event stream first emits `step: model_cached`, then, when `Content` is non-empty, a single whole `text` event; multiple incremental deltas are not replayed.

### Trust Boundary

- The cache backend is as trusted as the session store. Anyone who can write to the cache can forge answers, including forging tool calls for the Agent to execute; tool calls after a hit still go through the same validation as before writing (the tool must exist and the arguments must be valid), but this does not stop calls that are valid but malicious. External caches must have access control and must not be shared with untrusted services.
- Cached values are plaintext: model answers, tool call arguments (which may contain personal information), and reasoning text. Encryption and network isolation are the host's responsibility.
- Tags are HMAC digests that contain no plaintext user ID or session ID, and do not leak through cache key names, monitoring, or slow-query logs.

### Deletion Guarantees

After `Forget`, `Rewound`, or `Purge`, the same request does not hit (the key includes the memory version). Physical deletion is determined by `Erase`:

| Mode | Behavior | Use for |
|---|---|---|
| `EraseOff` | Logical deletion only; old entries remain until the TTL expires; no warning when the TTL exceeds `EraseWithin` | In-process caches, very short TTLs, or no compliance requirements |
| `EraseAuto` (default) | Physically deletes when the cache implements `cache.Tagged` and memory configures `OnErase`; otherwise falls back to TTL. When the cache does not support `Tagged`, the build logs a warning; when memory does not configure `OnErase`, it likewise falls back to TTL but without a warning, and you need to wire it yourself (with `mc.Eraser()`); a warning is also logged when the TTL exceeds `EraseWithin` | Most scenarios |
| `EraseRequired` | `Build` fails when the conditions are not met | Production environments with compliance requirements for deleting personal data |

- `EraseRequired` requires the TTL to be no greater than `memory.Config.EraseWithin`; this constraint only takes effect when long-term memory is configured. Without long-term memory, the upper bound on how long the cache retains data after a session is deleted is the TTL.
- A failed physical deletion does not affect logical correctness; it only delays the purge. Under `EraseRequired` the upper bound of the delay is the TTL; for Redis it is the TTL plus the expiration reclaim cycle (Redis expiration reclaim has no hard upper bound).
- Forgetting and rewinding clear all cache entries under the namespace's current scope, which is coarse-grained.
- The memory version is read only after recall (hydrateRecall). A deletion that happens between recall and reading the version may cause an entry to be written with the post-deletion version: such an entry is logically unreachable, and its physical residue is bounded by the TTL.

### Wiring OnErase

The long-term memory side only accepts a generic callback; `memory` does not know about the cache:

```go
m, err := memory.New(memory.Config{
    // ...
    OnErase: mc.Eraser(), // memory.EraseFunc; nil under EraseOff or when the cache does not support Tagged
})
```

- When `OnErase` is configured, `memory.New` requires `RetireGrace >= EraseWithin`, otherwise it returns a configuration error.
- A standalone Worker process does not go through the Builder; when wiring `OnErase` it must use the same cache backend as the Builder, and the same `TagKey` of at least 32 bytes. Otherwise the tags do not match and deletion silently misses. Under `EraseRequired`, the Builder can only validate the memory instance in its own process.
- A Worker should call `mc.Validate()` at startup and exit on error. It checks that `Cache` is non-nil, `TTL` is greater than 0, the cache implements `cache.Tagged` under `EraseRequired`, and the length of `TagKey` when tag deletion is performed, using the same set of rules as Build. Without this call, a misconfiguration is only exposed at the first `OnErase`, after which every maintenance round fails.
- When `OnErase` keeps failing, every maintenance round reports the error through the `onError` of `RunMaintenance`, and it accumulates in `Memory.EraseErrors()` (the number of times erasure failed right after commit). Hosts should monitor it.
- In multi-process deployments, the deadline for redoing erasure (`Until`) depends on each process's clock, and clock skew changes the actual deletion deadline.

### Deleting or Rewinding a Session

Rewinding a session without long-term memory does not trigger `Memory.Rewound`; the cache relies on the full history in the key to guarantee no hit, and the host must handle physical residue: call `mc.DeleteSession(ctx, sessionID)` after `session.Rewind` or after deleting the session.

`DeleteSession` returns nil under `EraseOff` or when the backend does not support `Tagged`, but nothing is physically deleted; entries remain until the TTL expires.

### Upgrading

`LastRewindSeq` is an add-only field, and the storage format version is unchanged. In mixed-version deployments, an old binary rewriting the head resets it to zero, making entries after a rewind reachable again; physical deletion still takes effect. During an upgrade, avoid having old and new binaries serve the same namespace with rewind operations at the same time.

## Distributed Deployment

When multiple processes share long-term memory, the model call cache can be switched to Redis (`github.com/kart-io/goagent-contrib/cache/redis`, Redis 7 or later): it implements `cache.Bytes` and `cache.Tagged` and is passed as `builder.ModelCache.Cache`. For the vector index and full deployment instructions, see ["分布式部署" in memory/README.md](../../memory/README.md#分布式部署). Cache-related notes:

- **All instances use the same `TagKey`**: tags are HMACs; with different `TagKey`s the same memory gets different tags on each instance, and `DeleteTag` cannot delete entries written by other instances.
- **Every process that holds memory must wire up erasure**: including processes that only run `memory/worker`; the `Forget` and `Purge` they execute must also reach the shared cache through `OnErase`.
- **Switching caches**: during a mixed deployment (some instances still use `SimpleCache`), deletions do not reach local caches, and the cross-instance deletion guarantee does not hold. Switch all at once, or disable ModelCache during the switch. Falling back from Redis to local requires no data migration; entries in Redis expire by TTL.
- **Redis eviction policy**: the token design keeps "not readable after deletion" unaffected by eviction; "already physically deleted on return" requires `maxmemory-policy` to be `noeviction` (or usage never reaching `maxmemory`), otherwise evicted member sets leave some entries to be reclaimed only by TTL.

## Recommendations

- The cache key must cover every dimension that affects the result: user/tenant, session, knowledge or data version, tool version.
- For retrieval-augmented answers, prefer retrieving fresh evidence each time; when you need answer caching, include the knowledge version in the key.
- Set the TTL by how often the data changes; `SimpleCache` has no entry limit by default, so when keys are determined by external input, set an upper bound with `cache.WithMaxEntries(n)` (the tool caching middleware likewise has no entry limit by default; set it with `middleware.WithCacheMaxEntries(n)`), or have an external cache implement `cache.Cache`.

## Configuration Migration After the Design Review Fixes

`SimpleCache` still requires `Close()` by default; `NewSimpleCache(ttl, cache.WithoutBackgroundCleanup())` creates no background cleanup goroutine and reclaims expired items on access/write. Functional tool caching uses this mode by default; the lifecycle of an injected cache is managed by the host, and constructing the middleware does not create or close the host's cache.

Tool caching takes an isolated snapshot of business output that preserves Go types, keeping `AdditionalContext`, `Labels`, and nested results. Resource objects that do not support safe snapshots are still returned normally but are not cached. The default full digest changes the cache key; old short-digest entries wait for their original TTL to expire or are cleared by the host; old keys are not read as a fallback.
