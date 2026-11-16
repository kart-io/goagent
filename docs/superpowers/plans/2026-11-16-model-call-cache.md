# 模型调用缓存实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用 Builder 级的单次模型调用缓存替换整轮 `CacheMiddleware`。缓存按宿主声明的可信身份隔离，与长期记忆配合时能逻辑失效，并按配置在 `EraseWithin` 内物理删除。

**Architecture:**
- `cache` 增加可选接口 `Tagged`，由 `SimpleCache` 实现。
- `memory` 增加 `LastRewindSeq`、只读的 `CacheVersion`，以及通用擦除回调 `OnErase`。`OnErase` 由维护队列 `queueErase` 重做，`memory` 不导入 `cache`。
- `builder` 新增 `ModelCache`：键与标签的推导、构建校验、`Eraser()` 与 `DeleteSession`。在 `modelOperation` 中、`call` 之前分支处理命中，写入前复核记忆版本。
- 最后删除 `CacheMiddleware` 并更新文档。

**Tech Stack:** Go 1.25、`codec/json`、`crypto/hmac`、`crypto/sha256`；测试用 testify 与 `go test -race`。

**Spec:** `docs/superpowers/specs/2026-11-16-model-call-cache-design.md`（a5285e27，已获批准）。执行者必须同时读 spec，计划不重复其中的理由。

## Global Constraints

- 所有注释、错误信息、文档、提交信息都用简体中文；标识符沿用项目的英文命名。
- 破坏性变更不做兼容：`CacheMiddleware` 直接删除，在 `scripts/apicheck/retired_api.go` 登记。
- 分层：`memory` 不得导入 `cache` 或 `builder`；`cache` 不得导入 `memory`。用 `scripts/verify_imports.sh` 与 `go run ./scripts/apicheck` 检查。
- 规则写成数据：构建校验沿用 `validateLongTermMemory` 的方式写成规则表；测试都是表驱动加 `t.Run`。
- `ModelCache.TTL` 必须大于 0；`TagKey` 至少 32 字节；`EraseRequired` 下 `TTL <= memory.Config.EraseWithin`（默认 5 分钟）。
- 只缓存 `FinishReason` 为 `stop` 或 `tool_calls`、通过 `validateDurableTurn`（`allowEmpty=false`）、且 `ProviderState` 为空的回答。
- 证据来源取值为 `"cache"`；命中事件为 `AgentEvent{Type: "step", Step: "model_cached"}`；元数据键为 `model_cache_hits`。
- 缓存值格式：`[]byte`，前缀 `"mc1\n"`，后面是 `llm.StreamChunk` 的 JSON。键前缀为 `"mc1:"`，后接十六进制 SHA-256。
- `LastRewindSeq` 的 JSON 名是 `last_rewind_seq,omitzero`；不升级 `memory-state-blob/3`、`memory-head/3`。实施时如果发现 head 或 state 的解码拒绝未知字段，就停下来回报。
- 测试一律用 `taskset -c 2,3,6-27` 运行（本机 CPU 核 0、8 有故障）；禁止 `-mod=mod`。

## Review Focus

1. **宿主的 `Scope` 对某些请求返回空字符串加 `true`**。期望不缓存，而不是用空身份当键。测试在 Task 4 的 `TestModelCacheScope`。
2. **同一个 `ModelCache` 同时用于两个 Builder（两个 Agent 共用后端）**。期望隔离身份相同、请求相同时可以命中，客户端身份不同时不命中。测试在 Task 5 的 `TestModelCacheSharedAcrossAgents`。
3. **缓存后端的 `Get` 返回 `ErrCacheMiss` 以外的错误（例如 Redis 断开）**。期望按未命中继续调用模型，并记录这个错误，不让整轮失败。测试在 Task 5 的 `TestModelCacheBackendErrors`。
4. **请求钩子或 `RequestError` 换了客户端之后再调用**。期望用新客户端的身份重算键，不命中旧客户端的条目。测试在 Task 5 的 `TestModelCacheReplannedClient`。
5. **一次 `Forget` 时同一命名空间有多个会话在跑**。期望所有会话的条目都被删除，后续请求都不命中。测试在 Task 3 的 `TestForgetErasesAcrossSessions`（memory 侧）。

---

## File Structure

| 文件 | 职责 |
|---|---|
| `cache/tagged.go`（新建） | `Tagged` 接口 |
| `cache/simple_cache.go` | `SimpleCache` 实现 `Tagged`（标签到键集合的索引） |
| `memory/cache_version.go`（新建） | `CacheVersion`、`NamespaceVersion` |
| `memory/erase.go`（新建） | `EraseFunc`、`queueErase` 的登记与重做、`ErasesOnDelete` |
| `memory/state_codec.go`、`state_tx.go`、`state_head.go`、`durable_state.go` | `LastRewindSeq` 标量 |
| `memory/memory.go`、`memory/scope.go`、`memory/job.go` | 在 Forget、Rewound、retire、Maintain 中挂接擦除 |
| `builder/model_cache.go`（新建） | `ModelCache`、`EraseMode`、`WithModelCache`、校验规则表、`Eraser`、`DeleteSession` |
| `builder/model_cache_key.go`（新建） | 键、标签、值的编解码（纯函数） |
| `builder/model_call.go` | 命中分支、写入、`savedTurn.Cached` |
| `builder/durable_turn.go`、`builder/events.go`、`builder/tool_events.go`、`builder/loop_hooks.go` | 经 ctx 传入缓存范围、计数 |
| 删除：`core/middleware` 中的缓存中间件及测试、`constants/cache.go` 中只被它使用的常量 | — |

---

### Task 1: `cache.Tagged` 与 `SimpleCache` 实现

**Files:**
- Create: `cache/tagged.go`
- Modify: `cache/simple_cache.go`
- Test: `cache/tagged_test.go`

**Interfaces:**
- Produces:
  ```go
  type Tagged interface {
      SetTagged(ctx context.Context, key string, value []byte, ttl time.Duration, tags ...string) error
      DeleteTag(ctx context.Context, tag string) error
  }
  var _ Tagged = (*SimpleCache)(nil)
  ```

- [ ] **Step 1: 写失败的测试** `TestSimpleCacheTagged`，表驱动，各用例断言如下：
  - 用 `SetTagged(k1, v, ttl, "a", "b")` 和 `SetTagged(k2, v, ttl, "b")` 写入后，`DeleteTag("a")` 使 `Get(k1)` 返回 `agentErrors.ErrCacheMiss`，`Get(k2)` 仍然命中。
  - `DeleteTag("b")` 删除 k1 和 k2。
  - 对不存在的标签调用 `DeleteTag` 返回 nil（幂等）。
  - 普通 `Delete(k1)` 和条目过期后，再 `DeleteTag` 不报错，标签索引里也不再留有 k1。断言方式：`DeleteTag` 之后 `GetStats` 的条目数不变。
  - 对同一个键再次 `SetTagged` 并换一组标签，旧标签不再关联这个键。
  - 用 `-race` 并发执行 `SetTagged` 与 `DeleteTag`，不出现数据竞争。

- [ ] **Step 2: 运行，确认失败**
  `taskset -c 2,3,6-27 go test ./cache -run TestSimpleCacheTagged`，预期编译失败（`SetTagged` 未定义）。

- [ ] **Step 3: 实现**
  - `SimpleCache` 增加 `tags map[string]map[string]struct{}`，用 `sync.Mutex` 保护，同时在 `cacheEntry` 上记录它的标签。
  - `remove`、`Delete`、`Clear` 和过期清理时同步摘除标签索引。
  - 文档注释写明实现要求：`DeleteTag` 幂等；外部后端里标签集合的过期时间不短于条目 TTL；标签多时分批删除。

- [ ] **Step 4: 运行通过**
  `taskset -c 2,3,6-27 go test -race ./cache`，全部 PASS。

- [ ] **Step 5: 提交**
  `git commit -m "feat(cache): 增加按标签删除的 Tagged 接口并由 SimpleCache 实现"`

---

### Task 2: `LastRewindSeq` 与 `Memory.CacheVersion`

**Files:**
- Create: `memory/cache_version.go`
- Modify: `memory/state_codec.go:28-35, 95, 262`、`memory/state_tx.go:149-159`、`memory/state_head.go:326`、`memory/durable_state.go:61`、`memory/memory.go`（`Rewound` 的 mutate 回调）
- Test: `memory/cache_version_test.go`

**Interfaces:**
- Produces:
  ```go
  type NamespaceVersion struct {
      Namespace     []string
      Scope         string
      LastForgetSeq int64
      LastRewindSeq int64
  }
  // CacheVersion 读取每个读命名空间的最新版本，按命名空间排序；不取锁，只读 meta 与 head。
  func (m *Memory) CacheVersion(ctx context.Context, read [][]string) ([]NamespaceVersion, error)
  ```
  另外：`tx` 增加 `LastRewindSeq() int64` 和 `SetLastRewindSeq(int64)`。

- [ ] **Step 1: 写失败的测试** `TestCacheVersion`，表驱动，各用例断言如下：
  - 新命名空间的版本是 `{ns, 当前作用域, 0, 0}`。
  - `Forget` 之后 `LastForgetSeq` 变大，`LastRewindSeq` 不变。
  - `Rewound` 之后 `LastRewindSeq` 变大，`LastForgetSeq` 不变。
  - `Purge` 之后 `Scope` 改变。
  - 进程重启后重新打开（同一个 FileStore 目录），读出的值与重启前相同。
  - 用不含 `last_rewind_seq` 字段的旧 head JSON 解码，得到 0 且不报错（直接构造字节）。
  
  另写一个测试 `TestLastRewindSeqSurvivesCompaction`：触发一次基础快照重写后，`LastRewindSeq` 不丢失。

- [ ] **Step 2: 运行，确认失败**
  `taskset -c 2,3,6-27 go test ./memory -run 'TestCacheVersion|TestLastRewindSeq'`，预期编译失败。

- [ ] **Step 3: 实现**
  - `stateScalars`、`state`、两处标量拷贝（`state_codec.go:95`、`:262`）、`state_head.go:326`、`state_tx.go:159` 都加上 `LastRewindSeq`。
  - `Rewound` 在写回退标记的同一次 mutate 中调用 `t.SetLastRewindSeq(mk.Seq)`。
  - 先核对 head 与 state 的解码调用点是否使用 `codec/json` 的 `Strict`。是的话停下来回报。
  - `CacheVersion` 复用 `currentScope` 与 head 的读取路径，不解码整份 state。

- [ ] **Step 4: 运行通过**
  `taskset -c 2,3,6-27 go test -race ./memory`，全部 PASS（原有测试也要通过）。

- [ ] **Step 5: 提交**
  `git commit -m "feat(memory): 新增 LastRewindSeq 与 CacheVersion，供模型缓存判断删除类操作"`

---

### Task 3: `memory.Config.OnErase` 与 `queueErase`

**Files:**
- Create: `memory/erase.go`
- Modify: `memory/memory.go`（`Config`、校验规则表 `:144` 附近、`Forget`、`Rewound`）、`memory/job.go`（`queueReason`、`queueEntry`、`Maintain`）、`memory/scope.go:113`（`retire`）
- Test: `memory/erase_test.go`

**Interfaces:**
- Consumes: 无
- Produces:
  ```go
  // EraseFunc 清除依据 (ns, scope) 下记忆生成的派生副本；必须幂等。
  type EraseFunc func(ctx context.Context, ns []string, scope string) error
  // Config 新增字段
  OnErase EraseFunc `json:"-"`
  // ErasesOnDelete 报告是否配置了 OnErase。
  func (m *Memory) ErasesOnDelete() bool
  // EraseWithin 返回生效的擦除时限（含默认值），供 Builder 校验 TTL。
  func (m *Memory) EraseWithin() time.Duration
  ```
  另外：`queueReason` 新增 `queueErase = "erase"`；`queueEntry` 新增 `Until time.Time \`json:"until,omitzero"\``。

- [ ] **Step 1: 写失败的测试** `TestOnErase`，表驱动。用一个记录调用、可以注入失败的 `EraseFunc` 和假时钟（沿用 `memory` 测试里的 `now` 注入方式）。各用例断言如下：
  - `Forget` 提交后立即调用一次 `OnErase(ns, 当前作用域)`。
  - `Rewound` 同上。
  - `Purge` 后，下一次 `Maintain` 的 `retire` 对旧作用域调用 `OnErase`。`RemoveScope` 失败时 `OnErase` 仍然被调用。
  - 让 `OnErase` 第一次失败：`Forget` 不返回错误，因为已经提交。再次 `Forget` 直接返回，不调用 `OnErase`。下一次 `Maintain` 调用它并成功。
  - 在 `Until`（提交时间加 `EraseWithin`）之前，每次 `Maintain` 都会再调用一次；过了 `Until` 且最后一次成功后，登记被注销，之后的 `Maintain` 不再调用。
  - 过了 `Until` 但最后一次仍失败时，保留登记，`Maintain` 返回错误。
  - 登记之后命名空间被 Purge 过（作用域已变），`Maintain` 直接注销这条登记，不调用 `OnErase`。
  - `OnErase == nil` 时不登记 `queueErase`，`ErasesOnDelete()` 返回 false。
  
  另写 `TestForgetErasesAcrossSessions`（Review Focus 第 5 条）：同一命名空间下两个会话各自完成一轮，然后一次 `Forget`，`OnErase` 收到的 `(ns, scope)` 恰好一次，且覆盖这个命名空间。

- [ ] **Step 2: 运行，确认失败**
  `taskset -c 2,3,6-27 go test ./memory -run 'TestOnErase|TestForgetErasesAcrossSessions'`，预期编译失败。

- [ ] **Step 3: 实现**
  - `Forget` 和 `Rewound` 在 `OnErase != nil` 时，通过 `withBeforeSave` 登记 `queueEntry{Namespace, Scope: t.scope, Reason: queueErase, Until: now + EraseWithin}`，与现有的 `queueIndex` 登记并列。提交后调用一次 `OnErase`，错误不返回给调用方，交给维护重做。
  - `Maintain` 的 switch 增加 `e.Reason == queueErase` 分支，按上面的规则调用 `OnErase` 并决定是否注销。
  - `retire` 中把 `OnErase` 的调用与 `RemoveScope`、索引清理放在一起，用 `errors.Join` 汇总错误，彼此不短路。

- [ ] **Step 4: 运行通过**
  `taskset -c 2,3,6-27 go test -race ./memory` 全部 PASS；`scripts/verify_imports.sh` 通过（`memory` 没有导入 `cache`）。

- [ ] **Step 5: 提交**
  `git commit -m "feat(memory): 增加通用擦除回调 OnErase，Forget 与 Rewound 失败时由维护在 EraseWithin 内重做"`

---

### Task 4: `builder.ModelCache` 的配置、校验、键与标签

**Files:**
- Create: `builder/model_cache.go`、`builder/model_cache_key.go`
- Modify: `builder/config_options.go`（`AgentConfig` 增加 `ModelCache *ModelCache`）、`builder/builder.go`（在 `Build` 中调用校验，位置与 `validateLongTermMemory` 相同）
- Test: `builder/model_cache_test.go`

**Interfaces:**
- Consumes: Task 1 的 `cache.Tagged`；Task 2 的 `memory.NamespaceVersion`；Task 3 的 `memory.EraseFunc`、`(*memory.Memory).ErasesOnDelete`、`(*memory.Memory).EraseWithin`。
- Produces:
  ```go
  type EraseMode int
  const (EraseAuto EraseMode = iota; EraseOff; EraseRequired) // 零值即默认的 EraseAuto

  type ModelCache struct {
      Cache  cache.Cache
      TTL    time.Duration
      Scope  func(ctx context.Context) (string, bool)
      Erase  EraseMode
      TagKey []byte
  }
  func (b *AgentBuilder[S]) WithModelCache(mc ModelCache) *AgentBuilder[S]
  func (mc ModelCache) Eraser() memory.EraseFunc
  func (mc ModelCache) DeleteSession(ctx context.Context, sessionID string) error

  // model_cache_key.go（包内）
  type cacheKeyInput struct {
      Identity  string
      SessionID string
      Client    string // CacheIdentity()，或 provider+"\x00"+模型名
      Request   *llm.CompletionRequest
      Tools     []interfaces.Tool
      Memory    []memory.NamespaceVersion
  }
  func modelCacheKey(in cacheKeyInput) (key string, ok bool) // schema 解析失败时 ok=false
  func clientIdentity(c llm.Client, req *llm.CompletionRequest) (string, bool)
  func sessionTag(tagKey []byte, sessionID string) string
  func memoryTag(tagKey []byte, ns []string, scope string) string
  func encodeCachedTurn(t *llm.StreamChunk) ([]byte, error)
  func decodeCachedTurn(b []byte) (*llm.StreamChunk, error)
  func (mc ModelCache) identity(ctx context.Context, ltm bool) (string, bool)
  ```

- [ ] **Step 1: 写失败的测试**
  - `TestModelCacheValidation`，表驱动，覆盖 spec 5.1 校验表的全部 8 行。每行断言 `Build()` 返回 `CodeInvalidConfig`，或者构建成功并在测试 logger 里出现一条警告。
  - `TestModelCacheKey`，表驱动，以一个基准输入为准，逐项改动后断言键不同：
    - 身份、会话 ID、客户端身份、模型名；
    - 一条消息的内容；
    - `Temperature` 显式为 0（`TemperatureSet=true`）与未设置；
    - 增加一个工具；
    - 记忆版本中的任一字段。
    
    另外断言：
    - 工具 schema 只有键顺序不同时，键相同；
    - schema 不是合法 JSON 时 `ok=false`；
    - 字段内容为 `("ab","c")` 和 `("a","bc")` 时，键不同（长度前缀生效）。
  - `TestClientIdentity`：
    - 实现了 `CacheIdentity()` 的客户端，结果就是它的返回值；
    - `req.Model` 非空时用它；
    - 否则用 `ModelName()`；
    - 都没有时 `ok=false`。
  - `TestModelCacheTags`：
    - 标签里不含明文的会话 ID 和命名空间组件；
    - `TagKey` 不同，标签不同；
    - 命名空间 `["a:b","c"]` 与 `["a","b:c"]` 得到不同的标签。
  - `TestModelCacheScope`（Review Focus 第 1 条）：
    - `Scope` 返回 `("", true)` 或 `(x, false)` 时，`identity` 返回 false；
    - `Scope == nil` 且有长期记忆时，用 `memory.NamespaceFrom(ctx)`；
    - 命名空间为空时返回 false。
  - `TestCachedTurnCodec`：
    - 编码再解码，得到与原值相同的 chunk；
    - 前缀不是 `"mc1\n"` 或 JSON 非法时返回错误。
  - `TestModelCacheEraserAndDeleteSession`：在 `SimpleCache` 上写入带标签的条目后，`Eraser()(ctx, ns, scope)` 只删除这个命名空间的条目，`DeleteSession` 只删除这个会话的条目。

- [ ] **Step 2: 运行，确认失败**
  `taskset -c 2,3,6-27 go test ./builder -run 'TestModelCache|TestClientIdentity|TestCachedTurnCodec'`，预期编译失败。

- [ ] **Step 3: 实现**
  - 键：按 `cacheKeyInput` 的字段顺序逐个写入 SHA-256，每个字段先写 8 字节大端长度。`Request` 用 `json.Marshal`，依赖 `llm/temperature.go` 的 `MarshalJSON`。工具 schema 先 `json.Unmarshal` 到 `any`，再 `json.Marshal` 得到规范形式。
  - 标签：在 HMAC-SHA256 中写入前缀 `"session"` 或 `"memory"`，再按长度前缀写入各字段，取十六进制。
  - 校验写成 `[]struct{ bad func() bool; err func() error; warn string }` 规则表。

- [ ] **Step 4: 运行通过**
  `taskset -c 2,3,6-27 go test -race ./builder -run 'TestModelCache|TestClientIdentity|TestCachedTurnCodec'`，全部 PASS。

- [ ] **Step 5: 提交**
  `git commit -m "feat(builder): 增加 ModelCache 配置、构建校验与键和标签的推导"`

---

### Task 5: 在 `modelOperation` 中接入命中与写入

**Files:**
- Modify:
  - `builder/model_call.go`：`savedTurn`、`modelOperation`、`readPlainModelTurn`
  - `builder/durable_turn.go:22-60`
  - `builder/loop_hooks.go:466`：传入缓存范围
  - `builder/events.go:225-240`
  - `builder/tool_events.go:202-209`
  - `observe` 模型事件增加 `Cached bool`
- Test: `builder/model_cache_run_test.go`

**Interfaces:**
- Consumes: Task 4 的 `modelCacheKey`、`clientIdentity`、`memoryTag`、`sessionTag`、`encodeCachedTurn`、`decodeCachedTurn`、`ModelCache.identity`；Task 2 的 `Memory.CacheVersion`。
- Produces:
  ```go
  // 包内 ctx 键 modelCacheScopeKey{} → *modelCacheScope
  type modelCacheScope struct {
      cfg       ModelCache
      identity  string
      sessionID string
      ltm       *memory.Memory // 可为 nil
      read      [][]string     // 本轮 TurnMemory.Read
      hits      *atomic.Int64  // 本次执行的命中计数
  }
  func withModelCacheScope(ctx context.Context, s *modelCacheScope) context.Context
  // savedTurn 新增
  Cached bool `json:"model_cached,omitzero"`
  ```
  `modelOperation` 的签名增加一个参数 `cacheable func(*llm.StreamChunk) error`，作为写入前的校验。`readJournaledTurn` 传入 `validateDurableTurn(turn, tools, false, feedback)`，`readPlainModelTurn` 传入 `validateDurableTurn(turn, nil, false, false)`。另外 `readJournaledTurn` 与 `readPlainModelTurn` 都要把 `client`、`req` 和工具交给缓存查找，具体方式由实施者定（例如再加一个 `cacheRequest` 参数）。

- [ ] **Step 1: 写失败的测试** `TestModelCacheRun`。表驱动，用 `MockToolLLMClient`（`builder/builder_test.go:63`）加调用计数，覆盖 spec 第 7 节测试表中属于 builder 的所有行。重点断言：
  - 两个 `Scope` 不同的 Agent 共用同一个默认会话 ID 和同一个 `SimpleCache`，输入相同时，模型调用次数为 2。
  - 同一身份、同一会话、完全相同的请求第二次执行时：
    - 模型调用次数不增加；
    - 会话日志事件与第一次逐条相同（用 `session` 的读取接口比较，只排除时间戳）；
    - 事件流包含 `model_cached` 和一段完整的 `text`；
    - 输出元数据中 `model_calls` 不计入命中，`model_cache_hits == 1`，`usage_complete` 与未命中时相同。
  - 带预算的持久运行：命中后中断，再恢复执行。断言：
    - 恢复时不报 `usage has no reservation`；
    - 预算的已用额度等于只算真实调用的额度；
    - 恢复时不访问缓存（给缓存包一层计数，断言 `Get` 次数不变）。
  - 下列回答都不写入缓存（断言缓存的 `Set` 次数为 0）：出错、`FinishReason=length`、空回答、调用不存在的工具、带 `ProviderState`，以及回放得到的结果。
  - 缓存值被改写为非法字节，或改写为调用不存在工具的 chunk 时，按未命中处理：调用模型，删除这个条目。
  - 长期记忆场景：
    - 同一轮中先命中，执行 `memory_forget` 之后的下一步不命中；
    - `Forget`、`Rewound`、`Purge` 之后的相同请求不命中，且 `SimpleCache` 中对应标签的条目已被删除（经 `mc.Eraser()` 接到 `memory.Config.OnErase`）。
  - 写入前复核：用一个阻塞的假客户端，在调用进行中执行 `Forget`，客户端返回后断言缓存的 `Set` 次数为 0。
  - `TestModelCacheSharedAcrossAgents`（Review Focus 第 2 条）：两个 Builder 共用同一个 `ModelCache`。身份和请求相同时第二个 Agent 命中；客户端的 `CacheIdentity()` 不同时不命中。
  - `TestModelCacheBackendErrors`（Review Focus 第 3 条）：
    - 包装的缓存 `Get` 返回一般错误时，执行成功，模型被调用，`observe` 中有一条记录这个错误的事件；
    - `SetTagged` 返回错误时，执行同样成功。
  - `TestModelCacheObserve`：命中、未命中、写入失败、写入因版本复核放弃时，`observe` 各发出一条可区分的事件，`model` 事件带 `Cached` 标记。
  - `TestModelCacheToolCallIDs`：工具调用回合命中后，会话日志中出现与之前一轮相同的工具调用 ID 时，`session_log.go` 按 ID 索引的工具元数据不会串用；如果会串用，就在命中时为工具调用重新生成 ID，持久回放时沿用记录里的 ID。实施时先写这个测试来确认现状。
  - `TestModelCacheReplannedClient`（Review Focus 第 4 条）：`RequestError` 钩子替换客户端后，第二次调用用新客户端的身份计算键，不会取到旧客户端的条目。

- [ ] **Step 2: 运行，确认失败**
  `taskset -c 2,3,6-27 go test ./builder -run 'TestModelCacheRun|TestModelCacheShared|TestModelCacheBackend|TestModelCacheReplanned'`，预期失败或编译失败。

- [ ] **Step 3: 实现**
  - Agent 在每轮开始时，根据 `config.ModelCache`、`sessionLogID(ctx)` 和 `turnMemory` 的 `Read` 构造 `modelCacheScope`。`identity` 返回 false 时不构造，放进 ctx。
  - `modelOperation` 在持久路径的 `durable.Do` 回调内、非持久路径的 `call(ctx)` 前查找缓存：
    - 键只算一次；
    - 命中时 `turn` 取缓存的值，`saved.Cached = true`，`EvidenceSource = "cache"`，`hits.Add(1)`，然后照常执行 `validate`，不执行 `call`；
    - 校验失败时 `Delete` 这个键，回到正常调用。
  - `recordUsage` 的回放分支在 `saved.Cached` 时跳过。
  - 返回前，命中（而不是回放）时发出 `model_cached` 和一段 `text`，形式与现有回放分支一致。
  - 写入条件：`attempted && !Replayed && !Cached && err == nil && cacheable(turn) == nil && turn.ProviderState 为空 && FinishReason ∈ {stop, tool_calls}`。写入前重新调用 `CacheVersion`，与键中的版本不同就放弃。实现了 `Tagged` 就用 `SetTagged`，带上会话标签和各命名空间标签；否则用 `Set`。
  - `tool_events.go` 和 `events.go`：`model_calls = round + 1 - hits`，`model_cache_hits = hits`，`usage_complete = reports == model_calls`。

- [ ] **Step 4: 运行通过**
  `taskset -c 2,3,6-27 go test -race ./builder`，全部 PASS（原有测试也要通过）。

- [ ] **Step 5: 提交**
  `git commit -m "feat(builder): 模型调用层缓存命中与写入，命中不占预算且回放不记账"`

---

### Task 6: 删除 `CacheMiddleware`

**Files:**
- Delete: `core/middleware` 中的 `CacheMiddleware`、`CacheEntry`、`cacheShard`、`NewCacheMiddleware*` 及其测试，包括 `bounded_test.go`、`advanced_test.go`、`middleware_edge_cases_test.go`、`middleware_test.go` 中的缓存用例；`MiddlewareChain` 中对 `cached_response` 的特殊处理。
- Modify:
  - `builder/long_term_memory.go:40-44`：删除互斥校验及其 import；
  - `builder/long_term_memory_test.go:177`：删除 `TestLongTermMemoryRejectsResponseCache`；
  - `builder/builder_test.go` 中的缓存中间件用例；
  - `builder/quick_builders.go`：两个快速构建器去掉缓存（我方决定：快速构建器拿不到宿主的 `Scope`）；
  - `examples/builder/advanced/main.go`、`examples/integration/langchain-phase2/phase2_demo.go`：改用 `WithModelCache`，给出示例 `Scope` 和 `cache.NewSimpleCache`；
  - `constants/cache.go`：删除只被缓存中间件使用的常量（先 grep 确认没有其他使用方）；
  - `scripts/apicheck` 中相应的登记，以及在 `retired_api.go` 加入已删除的名称。
- Test: 现有测试。

**Interfaces:**
- Consumes: Task 4 的 `WithModelCache`（示例中使用）。
- Produces: 无。

- [ ] **Step 1: 确认引用范围**
  运行 `grep -rn 'CacheMiddleware\|NewCacheMiddleware\|cached_response\|DefaultMiddlewareCache' --include='*.go' .`，记下引用清单。删除完成后，这条命令只应在 `retired_api.go` 中有结果。
- [ ] **Step 2: 删除与修改**
  按上面的 Files 清单执行。
- [ ] **Step 3: 验证**
  - `go build ./...`、`go vet ./...`、`go run ./scripts/apicheck` 全部通过；
  - `taskset -c 2,3,6-27 go test -race ./core/... ./builder/... ./examples/...` 全部 PASS；
  - Step 1 的 grep 只在 `retired_api.go` 中有结果。
- [ ] **Step 4: 提交**
  `git commit -m "refactor!: 删除整轮 CacheMiddleware，由模型调用缓存取代"`

---

### Task 7: 文档

**Files:**
- Modify:
  - `docs/design/MEMORY_GOVERNANCE.md`：0.1 表增加"模型调用缓存"一行；在表下说明 `Rewound` 与 `Forget` 相同；改写 1.8、2.2 第 12 条、5.2、7 节中与 `CacheMiddleware` 相关的条目；
  - `docs/guides/CACHING_GUIDE.md`：把"Agent 输入 → 响应"一行改为模型调用缓存，并新增一节，写入 spec 5.7 的信任边界、5.8 的适用场景、Worker 进程装配 `OnErase` 的方法、`DeleteSession` 的调用时机，以及混合版本升级的说明；
  - `docs/api/MIDDLEWARE_API.md`、`docs/guides/{BUILDER_API_REFERENCE,USER_GUIDE,PRIORITY_CAPABILITIES,RUNTIME_READINESS}.md` 中的缓存中间件条目；
  - `cache/README.md`：说明 `Tagged`；
  - `CHANGELOG.md` 的 `[Unreleased]`：破坏性变更与迁移步骤（`NewCacheMiddleware` 改为 `WithModelCache`）。

- [ ] **Step 1: 改写上面的文档**
  0.1 表新增那一行的内容照抄 spec 5.6 的措辞。
- [ ] **Step 2: 验证**
  `grep -rn 'CacheMiddleware' docs CHANGELOG.md` 只在 CHANGELOG 的迁移说明和 `docs/evaluation` 的历史日志中出现。
- [ ] **Step 3: 提交**
  `git commit -m "docs: 模型调用缓存的使用、信任边界与删除承诺"`

---

### Task 8: 性能测量与整体验收

**Files:**
- Create: `builder/model_cache_bench_test.go`、`docs/evaluation/2026-11-16-model-call-cache/README.md`
- Test: 同上。

- [ ] **Step 1: 写基准** `BenchmarkModelCacheVersion`
  - 对 FileStore（放在 tmpfs 或 NVMe 上，不要放 `/mnt/data`）和 pgstore 各测一组；
  - 使用真实规模的数据：1000 条中文记录加 UUID；
  - 分别测 `CacheVersion` 的耗时，以及一次命中路径的总耗时。
  pgstore 那组放在 goagent-contrib 中运行，或者在没有 DSN 时 `Skip`。
- [ ] **Step 2: 运行并记录**
  `taskset -c 2,3,6-27 go test -run '^$' -bench BenchmarkModelCache -benchmem ./builder`，结果写进评测 README。
- [ ] **Step 3: 整体验收**
  - `go build ./...`、`go vet ./...`、`taskset -c 2,3,6-27 go test -race ./...`、`make lint`、`scripts/verify_imports.sh` 全部通过；
  - 用临时 `go.work` 让 goagent-contrib 指向本工作区，对比 master 与本分支下 `management` 等模块的编译差异（方法见 memory `goagent-downstream-impact-check`），预期只有使用 `CacheMiddleware` 的地方报错，并把这些地方列出来。
- [ ] **Step 4: 提交**
  `git commit -m "test(builder): 模型缓存的记忆版本读取开销基准与验收记录"`
