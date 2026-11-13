package memory

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

// Weights 是召回打分 α·相关性 + β·新近性 + γ·重要性 的系数（Generative Agents）。
type Weights struct {
	Relevance  float64 `json:"relevance"`
	Recency    float64 `json:"recency"`
	Importance float64 `json:"importance"`
}

// Retention 是删除策略。衰减只影响召回排序；删除只发生在 TTL 到期、超出容量与失效后超过保留期。
type Retention struct {
	// TTL 按类别给出记录的存活时间（从创建起），未列出的类别不过期。
	TTL map[Kind]time.Duration `json:"ttl"`
	// MaxRecords 是每个命名空间未钉住的有效记录上限，超出时淘汰得分最低的；0 表示不限。
	MaxRecords int `json:"max_records"`
	// InvalidRetention 是失效记录的保留期，默认 30 天。
	InvalidRetention time.Duration `json:"invalid_retention"`
}

// Config 配置长期记忆。Durable 必填：记录、图谱与抽取任务都存于其中。
// Index 与 Embedder 同时提供时启用向量召回，否则只按文本重合召回；Model 与 Sessions 同时提供时 Worker 才能抽取。
type Config struct {
	// Durable 是记忆数据的持久存储，须同时实现 durable.Scoped（作用域用于 Purge 的整体删除）。
	Durable durable.Store `json:"-"`
	// MaxStateBytes 是一个命名空间的数据（state）解压后的上限，默认 8 MiB；删除类操作不受限制。存储设置不进入 Identity。
	MaxStateBytes int `json:"-"`
	// StateCacheBytes 是进程内 state 解码缓存的容量，按估计的堆占用（解压后字节数的 2.5 倍）计，默认 64 MiB；负数关闭缓存。
	StateCacheBytes int `json:"-"`
	// EraseWithin 是遗忘或删除之后、旧内容从基础快照中物理擦除的时限，默认 5 分钟（增量提交第 6 节）。
	EraseWithin time.Duration `json:"-"`
	// RollbackWindow 大于 0 时开启删除日志：遗忘与 Purge 先记入日志，数据回滚之后宿主据此重放删除；Prune 删除超过窗口的条目。
	// 默认 0（关闭），此时数据回滚不能保证删除承诺。
	RollbackWindow time.Duration `json:"-"`
	// RetireGrace 是 Purge 后旧作用域留在 retiring 中的时长，默认 10 分钟：到期时再清理一次索引，清掉提交后才写入的迟到向量。
	// 须大于 Index 单次写入的超时与重试时间之和。
	RetireGrace time.Duration `json:"-"`
	// ReindexEvery 是维护（Maintain）每隔多少轮对有登记的命名空间执行一次 Reindex(false)，默认 30。
	ReindexEvery int                 `json:"-"`
	Index        Index               `json:"-"`
	Embedder     llm.EmbeddingClient `json:"-"`
	// EmbedModel 标识嵌入模型，其摘要是索引键的一部分；改变后用 Reindex 重建索引。
	EmbedModel string        `json:"embed_model"`
	Model      llm.Client    `json:"-"`
	Sessions   session.Store `json:"-"`
	// Graph 为真时同时抽取实体与关系（时序知识图谱），召回时取距种子实体 GraphHops 跳内的关系，按 PageRank 取至多 GraphRelations 条。
	Graph          bool `json:"-"`
	GraphHops      int  `json:"graph_hops"`
	GraphRelations int  `json:"graph_relations"`
	// Communities 为真时 Worker 在每个任务后划分社区并为变化的社区写摘要（每个变化的社区一次模型调用），
	// 召回时附上至多 GraphCommunities 个相关社区的摘要。需要 Graph。
	Communities      bool `json:"communities"`
	GraphCommunities int  `json:"graph_communities"`
	// Instructions 是对抽取的补充要求，如需要记住或不得记住的内容。
	Instructions string `json:"instructions"`
	// TrackAccess 为真时记录召回的访问时间：召回后异步批量写入，失败忽略；新近性与容量淘汰按最近访问计。
	// 默认关闭，此时新近性按创建时间计，召回不产生写入。
	TrackAccess bool `json:"track_access,omitzero"`

	RecallK             int           `json:"recall_k"`
	RecallRunes         int           `json:"recall_runes"`
	Weights             Weights       `json:"weights"`
	HalfLife            time.Duration `json:"half_life"`
	ReconcileCandidates int           `json:"reconcile_candidates"`
	ReflectionThreshold float64       `json:"reflection_threshold"`
	JobAttempts         int           `json:"job_attempts"`
	Retention           Retention     `json:"retention"`
}

// Memory 是一个长期记忆实例，可被多个 Agent 与会话并发共享；读写按上下文中的命名空间隔离。
type Memory struct {
	cfg      Config
	identity string
	now      func() time.Time
	cache    *stateCache
	// modelTag 是 EmbedModel 的摘要，写入索引键。
	modelTag string
	// decodes 合并同一提交的并发解码（G1 建议 8）。
	decodes singleflight.Group
	// commitTimeout 是一次提交的时间上限（constants.MemoryCommitTimeout），测试可以调小。
	commitTimeout time.Duration
	// 压缩的触发条件（增量提交 4.1），测试可以调小。
	overlaySoft, overlayHard, overlayItems int
	// baseShare：覆盖层达到基础快照解压后大小的 1/baseShare 时压缩；0 表示不按比例压缩。
	baseShare   int
	eraseWithin time.Duration
	// compactErrors 计数提交成功之后失败的压缩；compactions 计数成功切换的基础快照。
	compactErrors, compactions atomic.Int64
	// droppedJobs 计数没有处理就丢弃的任务（跨越 Purge、没有作用域、state 超限或登记后发生了遗忘）。
	droppedJobs atomic.Int64
	// maintenance 计数 Maintain 的轮次，用于每 ReindexEvery 轮的定期维护。
	maintenance atomic.Int64
	access      accessBatcher
}

// New 校验并补齐配置。
//
// 不运行 Worker（Run 或 ProcessPending）的部署必须运行 RunMaintenance（或定期调用 Maintain）：
// 否则 Purge 中断后的旧作用域与 Forget 之后残留的向量不会被清理。
func New(cfg Config) (*Memory, error) {
	if cfg.Durable == nil || (cfg.Index == nil) != (cfg.Embedder == nil) || (cfg.Communities && !cfg.Graph) {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "长期记忆需要 Durable；Index 与 Embedder 须同时提供；Communities 需要 Graph").WithComponent("memory").WithOperation("new")
	}
	if _, ok := cfg.Durable.(durable.Scoped); !ok {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "长期记忆的 Durable 须实现 durable.Scoped").WithComponent("memory").WithOperation("new")
	}
	defaults := []struct {
		zero bool
		set  func()
	}{
		{cfg.RecallK <= 0, func() { cfg.RecallK = constants.DefaultMemoryRecallK }},
		{cfg.RecallRunes <= 0, func() { cfg.RecallRunes = constants.DefaultMemoryRecallRunes }},
		{cfg.Weights == (Weights{}), func() { cfg.Weights = Weights{Relevance: 0.6, Recency: 0.2, Importance: 0.2} }},
		{cfg.HalfLife <= 0, func() { cfg.HalfLife = constants.DefaultMemoryRecencyHalfLife }},
		{cfg.ReconcileCandidates <= 0, func() { cfg.ReconcileCandidates = constants.DefaultMemoryReconcileCandidates }},
		{cfg.ReflectionThreshold <= 0, func() { cfg.ReflectionThreshold = constants.DefaultMemoryReflectionThreshold }},
		{cfg.JobAttempts <= 0, func() { cfg.JobAttempts = constants.DefaultMemoryJobAttempts }},
		{cfg.Retention.InvalidRetention <= 0, func() { cfg.Retention.InvalidRetention = constants.DefaultMemoryInvalidRetention }},
		{cfg.GraphHops <= 0, func() { cfg.GraphHops = constants.DefaultMemoryGraphHops }},
		{cfg.GraphRelations <= 0, func() { cfg.GraphRelations = constants.DefaultMemoryGraphRelations }},
		{cfg.GraphCommunities <= 0, func() { cfg.GraphCommunities = constants.DefaultMemoryGraphCommunities }},
		{cfg.MaxStateBytes <= 0, func() { cfg.MaxStateBytes = constants.DefaultMemoryMaxStateBytes }},
		{cfg.StateCacheBytes == 0, func() { cfg.StateCacheBytes = constants.DefaultMemoryStateCacheBytes }},
		{cfg.EraseWithin <= 0, func() { cfg.EraseWithin = constants.DefaultMemoryEraseWithin }},
		{cfg.RetireGrace <= 0, func() { cfg.RetireGrace = constants.DefaultMemoryRetireGrace }},
		{cfg.ReindexEvery <= 0, func() { cfg.ReindexEvery = constants.DefaultMemoryReindexEvery }},
	}
	for _, d := range defaults {
		if d.zero {
			d.set()
		}
	}
	cfg.RecallK = min(cfg.RecallK, constants.MaxMemoryRecallK)
	// 外层的 Graph 字段沿用 G0 的指纹形式：配置不变时 Identity 不变，已有的召回快照照常恢复。
	fingerprint, err := json.Marshal(struct {
		Config
		Graph bool
	}{cfg, cfg.Graph})
	if err != nil {
		return nil, err
	}
	return &Memory{cfg: cfg, identity: fmt.Sprintf("memory:%x", sha256.Sum256(fingerprint)), now: time.Now,
		modelTag: modelTag(cfg.EmbedModel), cache: newStateCache(cfg.StateCacheBytes), commitTimeout: constants.MemoryCommitTimeout,
		overlaySoft: overlaySoftBytes, overlayHard: overlayHardBytes, overlayItems: overlayMaxItems, baseShare: baseShareDiv, eraseWithin: cfg.EraseWithin}, nil
}

// CacheStats 返回 state 解码缓存的计数。
func (m *Memory) CacheStats() CacheStats {
	s, n := m.cache.snapshot()
	s.Bytes = n
	return s
}

// Identity 标识召回配置；写入召回快照，配置改变后旧快照恢复时召回为空。
func (m *Memory) Identity() string { return m.identity }

// errNoChange 由修改函数返回，表示没有需要提交的修改：mutate 不提交，返回当前 state。
var errNoChange = errors.New("长期记忆没有需要提交的修改")

// conflict 返回 CodeConflict 错误。
func conflict(format string, args ...any) error {
	return agentErrors.Newf(agentErrors.CodeConflict, format, args...).WithComponent("memory")
}

func notFound(id string) error {
	return agentErrors.Newf(agentErrors.CodeStoreNotFound, "长期记忆记录 %q 不存在", id).WithComponent("memory")
}

// prepare 补齐记录的默认值并校验：ID 为空时生成，命名空间取 ns，生效时间默认为创建时间。
func (m *Memory) prepare(r Record, ns []string) (Record, error) {
	r.Namespace = ns
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.Kind == "" {
		r.Kind = KindFact
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = m.now()
	}
	if r.ValidFrom.IsZero() {
		r.ValidFrom = r.CreatedAt
	}
	r.Keywords, r.Entities, r.Sources = slices.Clone(r.Keywords), slices.Clone(r.Entities), slices.Clone(r.Sources)
	return r, r.validate()
}

// createRecord 在 tx 中新建记录，分配实例号，Revision 与 ContentRev 为 1；ID 已存在时不写入并返回 false。
func createRecord(t *tx, r Record) bool {
	if _, ok := t.Records.Get(r.ID); ok {
		return false
	}
	r.Incarnation, r.Revision, r.ContentRev = t.incarnation(), 1, 1
	t.Records.Put(r.ID, r)
	return true
}

// Remember 写入一条新记录（命名空间取自上下文），返回补齐后的记录。ID 为空时生成；ID 已存在时返回 CodeConflict，修改用 Update。
// 嵌入在锁外完成，向量在提交之后写入；作用域在嵌入期间改变（Purge）时重试一次，仍改变则返回 CodeConflict。
// 返回 ErrMemoryCommitUnknown 时可以重试：得到 CodeConflict 说明上一次其实已经提交。
func (m *Memory) Remember(ctx context.Context, r Record) (Record, error) {
	return m.remember(ctx, r, "")
}

// remember 写入作用域 expect；expect 为空时写入当前作用域，期间发生 Purge 则在新作用域重试一次。
// 指定的作用域已不是当前作用域时返回 errScopeChanged。
func (m *Memory) remember(ctx context.Context, r Record, expect string) (Record, error) {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return Record{}, err
	}
	if r, err = m.prepare(r, ns); err != nil {
		return Record{}, err
	}
	scope := expect
	if scope == "" {
		meta, err := m.loadMeta(ctx, ns)
		if err != nil {
			return Record{}, err
		}
		scope = meta.Current
	}
	vec, err := m.embed(ctx, r.Content)
	if err != nil {
		return Record{}, err
	}
	var out Record
	write := func(t *tx) error {
		if !createRecord(t, r) {
			return conflict("长期记忆记录 %q 已存在：修改用 Update", r.ID)
		}
		out, _ = t.Records.Get(r.ID)
		return nil
	}
	for attempt := 0; ; attempt++ {
		_, err = m.mutate(ctx, ns, scope, write)
		if !errors.Is(err, errScopeChanged) || expect != "" || attempt > 0 {
			break
		}
		meta, err := m.loadMeta(ctx, ns)
		if err != nil {
			return Record{}, err
		}
		scope = meta.Current
	}
	if err != nil {
		return Record{}, err
	}
	return out, m.writeIndex(ctx, ns, scope, recordSpace, []indexKey{recordKey(out)}, [][]float32{vec})
}

// Patch 是 Update 的修改项，nil 的字段不变。
type Patch struct {
	Content    *string
	Keywords   *[]string
	Entities   *[]string
	Importance *float64
	Pinned     *bool
}

// Update 修改一条有效记录：要求记录的 Revision 等于 expect，否则返回 CodeConflict（并发修改或已失效）；记录不存在时返回 CodeStoreNotFound。
// Revision 加一；正文改变时 ContentRev 加一，并在提交后重写向量。按 expect 判重，结果未知时重试是幂等的。
func (m *Memory) Update(ctx context.Context, id string, expect int64, p Patch) (Record, error) {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return Record{}, err
	}
	meta, err := m.loadMeta(ctx, ns)
	if err != nil {
		return Record{}, err
	}
	var vec []float32
	if p.Content != nil {
		if vec, err = m.embed(ctx, *p.Content); err != nil {
			return Record{}, err
		}
	}
	var out Record
	var changed bool
	_, err = m.mutate(ctx, ns, meta.Current, func(t *tx) error {
		r, ok := t.Records.Get(id)
		if !ok {
			return notFound(id)
		}
		if !r.Valid() || r.Revision != expect {
			return conflict("长期记忆记录 %q 已被修改或已失效（修订号 %d，期望 %d）", id, r.Revision, expect)
		}
		changed = p.Content != nil && *p.Content != r.Content
		if p.Content != nil {
			r.Content = *p.Content
		}
		if p.Keywords != nil {
			r.Keywords = slices.Clone(*p.Keywords)
		}
		if p.Entities != nil {
			r.Entities = slices.Clone(*p.Entities)
		}
		if p.Importance != nil {
			r.Importance = *p.Importance
		}
		if p.Pinned != nil {
			r.Pinned = *p.Pinned
		}
		r.Revision++
		if changed {
			r.ContentRev++
		}
		if err := r.validate(); err != nil {
			return err
		}
		t.Records.Put(id, r)
		out, _ = t.Records.Get(id)
		return nil
	})
	if err != nil {
		return Record{}, err
	}
	if !changed {
		return out, nil
	}
	old := recordKey(out)
	old.ContentRev--
	return out, errors.Join(m.writeIndex(ctx, ns, meta.Current, recordSpace, []indexKey{recordKey(out)}, [][]float32{vec}),
		m.dropIndex(ctx, ns, meta.Current, recordSpace, old))
}

// Get 返回当前命名空间中的一条记录（含失效记录）；不存在时返回 CodeStoreNotFound。
func (m *Memory) Get(ctx context.Context, id string) (Record, error) {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return Record{}, err
	}
	_, s, err := m.read(ctx, ns)
	if err != nil {
		return Record{}, err
	}
	r, ok := s.Records[id]
	if !ok {
		return Record{}, notFound(id)
	}
	return recordKind.clone(r), nil
}

// Query 是一次召回。Text 为空时只按新近性与重要性排序。
type Query struct {
	Text  string
	K     int
	Kinds []Kind
}

// Scored 是召回结果。
type Scored struct {
	Record
	Score     float64
	Relevance float64
}

// recallK 返回一次召回的条数。
func (m *Memory) recallK(q Query) int {
	k := q.K
	if k <= 0 {
		k = m.cfg.RecallK
	}
	return min(k, constants.MaxMemoryRecallK)
}

// recallView 是一轮召回读取的数据：一次 read 的结果，以及（TrackAccess 开启时）访问记录。
type recallView struct {
	scope  string
	v      view
	access map[string]accessEntry
}

func (m *Memory) readRecall(ctx context.Context, ns []string) (recallView, error) {
	scope, v, err := m.readView(ctx, ns)
	if err != nil {
		return recallView{}, err
	}
	rv := recallView{scope: scope, v: v}
	if m.cfg.TrackAccess {
		if rv.access, err = m.loadAccess(ctx, ns, scope); err != nil {
			return recallView{}, err
		}
	}
	return rv, nil
}

// Recall 在当前命名空间的有效记录中混合召回：向量相似度与文本重合取较大者为相关性，再与新近性、重要性加权。
// 只读；TrackAccess 开启时在返回后异步记录访问时间。
func (m *Memory) Recall(ctx context.Context, q Query) ([]Scored, error) {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return nil, err
	}
	out, scope, err := m.search(ctx, ns, q)
	if err != nil {
		return nil, err
	}
	m.touch(ns, scope, out)
	return out, nil
}

// search 是不登记访问的召回，另返回读取时的作用域。
func (m *Memory) search(ctx context.Context, ns []string, q Query) ([]Scored, string, error) {
	rv, err := m.readRecall(ctx, ns)
	if err != nil {
		return nil, "", err
	}
	vector, err := m.vectorHits(ctx, ns, rv.scope, rv.v.cur, q)
	if err != nil {
		return nil, "", err
	}
	return m.rankState(rv.v.cur, rv.v.idx, q, vector, rv.access), rv.scope, nil
}

// lastUsed 是新近性的起点：TrackAccess 开启且有访问记录时为最近访问时间，否则为创建时间。
func lastUsed(r Record, access map[string]accessEntry) time.Time {
	if a, ok := access[r.ID]; ok && a.At.After(r.CreatedAt) {
		return a.At
	}
	return r.CreatedAt
}

func (m *Memory) recency(last, now time.Time) float64 {
	age := now.Sub(last)
	if age < 0 {
		return 1
	}
	return math.Pow(0.5, float64(age)/float64(m.cfg.HalfLife))
}

// bigrams 取去掉空白与标点后的相邻字符对（小写），对中英文都适用。
func bigrams(text string) map[string]bool {
	var runes []rune
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			runes = append(runes, r)
		}
	}
	set := map[string]bool{}
	for i := 0; i+1 < len(runes); i++ {
		set[string(runes[i:i+2])] = true
	}
	return set
}

// overlap 是查询的字符对出现在记录中的比例。
func overlap(query, doc map[string]bool) float64 {
	if len(query) == 0 {
		return 0
	}
	n := 0
	for g := range query {
		if doc[g] {
			n++
		}
	}
	return float64(n) / float64(len(query))
}

// Pinned 返回当前命名空间钉住的有效记录（核心记忆），按创建时间排序。
func (m *Memory) Pinned(ctx context.Context) ([]Record, error) {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return nil, err
	}
	_, s, err := m.read(ctx, ns)
	if err != nil {
		return nil, err
	}
	return pinnedOf(s), nil
}

// pinnedOf 返回 s 中钉住的有效记录，按创建时间、ID 排序。
func pinnedOf(s state) []Record {
	var out []Record
	for _, r := range s.Records {
		if r.Valid() && r.Pinned {
			out = append(out, recordKind.clone(r))
		}
	}
	slices.SortFunc(out, func(a, b Record) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// Forget 遗忘一条记录：标记为 forgotten（终态）并清空正文、关键词与实体，移出索引；基础快照中的旧内容在 EraseWithin 内物理擦除。
// 开启删除日志时先写日志。已遗忘时直接返回；记录不存在时返回 CodeStoreNotFound。不级联到取代它的记录。
func (m *Memory) Forget(ctx context.Context, id string) error {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return err
	}
	scope, s, err := m.read(ctx, ns)
	if err != nil {
		return err
	}
	r, ok := s.Records[id]
	if !ok {
		return notFound(id)
	}
	if r.InvalidReason == ReasonForgotten {
		return nil
	}
	now := m.now()
	if m.cfg.RollbackWindow > 0 {
		if err := m.logDeletion(ctx, ns, func(l *DeletionLog) { l.Forgotten = append(l.Forgotten, Deletion{ID: id, At: now}) }); err != nil {
			return err
		}
	}
	opts := []mutateOption{shrinking()}
	if m.cfg.Index != nil {
		// 提交之后、删除向量之前崩溃会留下残留：先登记"索引待清理"，由维护执行 Reindex(false)（设计 4.3）。
		opts = append(opts, withBeforeSave(func(ctx context.Context, t *tx) error {
			if r, ok := t.Records.Get(id); !ok || r.InvalidReason == ReasonForgotten {
				return nil
			}
			return m.register(ctx, queueEntry{Namespace: ns, Scope: t.scope, Reason: queueIndex})
		}))
	}
	var old Record
	_, err = m.mutate(ctx, ns, scope, func(t *tx) error {
		r, ok := t.Records.Get(id)
		if !ok {
			return notFound(id)
		}
		if r.InvalidReason == ReasonForgotten {
			return errNoChange
		}
		old = r
		forget(t, r, now)
		return nil
	}, opts...)
	if err != nil || old.ID == "" {
		return err
	}
	return m.dropIndex(ctx, ns, scope, recordSpace, recordKey(old))
}

// forget 在 tx 中遗忘 r：清空内容、标记擦除，并把 LastForgetSeq 设为本次提交的 Seq。
func forget(t *tx, r Record, at time.Time) {
	r = r.invalidated(at, ReasonForgotten, "")
	r.Content, r.Keywords, r.Entities = "", nil, nil
	r.ContentRev++
	t.Records.Put(r.ID, r)
	t.Records.MarkErasure(r.ID)
	t.SetLastForgetSeq(t.nextSeq())
}

// Rewound 使任一来源位于会话 sessionID 中序号 to 之后的有效记录与关系失效（rewound），并恢复只被它们取代的旧记录与旧关系：
// 退回的轮次不再留下记忆，也不会带走更早轮次的事实。被遗忘的记录不动；取代者是被遗忘而不是被退回的，不恢复。
// 涉及变化关系的社区被删除，由下一次刷新重写。一次提交；宿主在 session.Rewind 之后调用。
func (m *Memory) Rewound(ctx context.Context, sessionID string, to int64) error {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return err
	}
	now := m.now()
	var gone, restored []Record
	var scope string
	_, err = m.mutate(ctx, ns, "", func(t *tx) error {
		gone, restored, scope = nil, nil, t.scope
		for _, id := range t.Records.Keys() {
			if r, _ := t.Records.Get(id); r.Valid() && rewound(r.Sources, sessionID, to) {
				t.Records.Put(id, r.invalidated(now, ReasonRewound, ""))
				gone = append(gone, r)
			}
		}
		for _, id := range t.Records.Keys() {
			r, _ := t.Records.Get(id)
			if r.InvalidReason != ReasonSuperseded || rewound(r.Sources, sessionID, to) {
				continue
			}
			if by, ok := t.Records.Get(r.SupersededBy); ok && by.InvalidReason == ReasonRewound {
				r.InvalidAt, r.InvalidReason, r.SupersededBy = time.Time{}, "", ""
				r.Revision++
				t.Records.Put(id, r)
				restored = append(restored, r)
			}
		}
		edgesChanged := false
		goneEdges := map[string]bool{}
		for _, id := range t.Edges.Keys() {
			e, _ := t.Edges.Get(id)
			goneEdges[id] = rewound([]Source{e.Source}, sessionID, to)
		}
		for _, id := range t.Edges.Keys() {
			e, _ := t.Edges.Get(id)
			switch {
			case goneEdges[id] && e.Valid():
				e.InvalidAt = now
			case !goneEdges[id] && !e.Valid() && goneEdges[e.SupersededBy]:
				e.InvalidAt, e.SupersededBy = time.Time{}, ""
			default:
				continue
			}
			e.Revision++
			t.Edges.Put(id, e)
			edgesChanged = true
		}
		if len(gone) == 0 && len(restored) == 0 && !edgesChanged {
			return errNoChange
		}
		if edgesChanged && m.cfg.Communities {
			syncCommunitiesTx(t, ns, nil)
		}
		return nil
	}, shrinking())
	if err != nil {
		return err
	}
	errs := []error{m.dropIndex(ctx, ns, scope, recordSpace, keysOf(gone)...)}
	var vecs [][]float32
	for _, r := range restored {
		vec, err := m.embed(ctx, r.Content)
		if err != nil {
			return errors.Join(append(errs, err)...)
		}
		vecs = append(vecs, vec)
	}
	return errors.Join(append(errs, m.writeIndex(ctx, ns, scope, recordSpace, keysOf(restored), vecs))...)
}

// Prune 按 Retention 在一次提交中整理当前命名空间：删除超过保留期的失效记录、关系与结束的任务，TTL 到期与超出容量的记录改为 expired。
// 之后删除过期记录的向量，清理访问记录与超过 RollbackWindow 的删除日志条目。
func (m *Memory) Prune(ctx context.Context) error {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return err
	}
	scope, err := m.currentScope(ctx, ns)
	if err != nil {
		return err
	}
	var access map[string]accessEntry
	if m.cfg.TrackAccess {
		if access, err = m.loadAccess(ctx, ns, scope); err != nil {
			return err
		}
	}
	now := m.now()
	var expired []Record
	var remaining map[string]bool
	_, err = m.mutate(ctx, ns, scope, func(t *tx) error {
		expired, remaining = nil, map[string]bool{}
		changed := false
		var valid []Scored
		for _, id := range t.Records.Keys() {
			r, _ := t.Records.Get(id)
			ttl, expires := m.cfg.Retention.TTL[r.Kind]
			switch {
			case !r.Valid() && now.Sub(r.InvalidAt) > m.cfg.Retention.InvalidRetention:
				t.Records.Delete(id)
				changed = true
				continue
			case r.Valid() && expires && now.Sub(r.CreatedAt) > ttl:
				t.Records.Put(id, r.invalidated(now, ReasonExpired, ""))
				expired, changed = append(expired, r), true
			case r.Valid() && !r.Pinned:
				valid = append(valid, Scored{Record: r, Score: m.cfg.Weights.Recency*m.recency(lastUsed(r, access), now) + m.cfg.Weights.Importance*r.Importance})
			}
			remaining[id] = true
		}
		for _, id := range t.Edges.Keys() {
			if e, _ := t.Edges.Get(id); !e.Valid() && now.Sub(e.InvalidAt) > m.cfg.Retention.InvalidRetention {
				t.Edges.Delete(id)
				changed = true
			}
		}
		// 结束的任务按同一保留期删除：完成的只留有键与完成时间，失败与丢弃的按登记时间计。
		for _, key := range t.DoneJobs.Keys() {
			if at, _ := t.DoneJobs.Get(key); now.Sub(at) > m.cfg.Retention.InvalidRetention {
				t.DoneJobs.Delete(key)
				changed = true
			}
		}
		for _, key := range t.Jobs.Keys() {
			if j, _ := t.Jobs.Get(key); j.Status != JobPending && now.Sub(j.CreatedAt) > m.cfg.Retention.InvalidRetention {
				t.Jobs.Delete(key)
				changed = true
			}
		}
		if limit := m.cfg.Retention.MaxRecords; limit > 0 && len(valid) > limit {
			slices.SortFunc(valid, func(a, b Scored) int {
				if a.Score != b.Score {
					if a.Score < b.Score {
						return -1
					}
					return 1
				}
				return strings.Compare(a.ID, b.ID)
			})
			for _, r := range valid[:len(valid)-limit] {
				t.Records.Put(r.ID, r.invalidated(now, ReasonExpired, ""))
				expired, changed = append(expired, r.Record), true
			}
		}
		if !changed {
			return errNoChange
		}
		return nil
	}, shrinking())
	if err != nil {
		return err
	}
	if err := m.dropIndex(ctx, ns, scope, recordSpace, keysOf(expired)...); err != nil {
		return err
	}
	if m.cfg.TrackAccess && slices.ContainsFunc(slices.Collect(maps.Keys(access)), func(id string) bool { return !remaining[id] }) {
		if err := m.editAccess(ctx, ns, scope, func(a map[string]accessEntry) {
			maps.DeleteFunc(a, func(id string, _ accessEntry) bool { return !remaining[id] })
		}); err != nil {
			return err
		}
	}
	return m.pruneDeletions(ctx, ns, now)
}

// currentScope 返回命名空间的当前作用域。
func (m *Memory) currentScope(ctx context.Context, ns []string) (string, error) {
	meta, err := m.loadMeta(ctx, ns)
	return meta.Current, err
}
