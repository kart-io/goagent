package memory

import (
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

// workerID 是 Worker 的互斥锁与心跳快照（根作用域）：先于其他所有锁获取。
const workerID = "memory/worker"

// workerBeat 是 Worker 的心跳：每处理完一个任务保存一次，失去锁（如 pgstore 租约被接管）时保存返回 ErrLeaseLost。
type workerBeat struct {
	At time.Time `json:"at"`
}

// Run 循环执行 Worker 轮次（ProcessPending），直到 ctx 结束。多个实例共用存储时由 memory/worker 锁互斥，同一时刻只有一个在处理。
// 单个任务的失败记入任务并按 JobAttempts 重试；轮次的错误（维护失败、失去锁等）调用 onError（可为 nil）后等待下一轮。
func (m *Memory) Run(ctx context.Context, onError func(error)) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
		n, err := m.ProcessPending(ctx)
		if err != nil && onError != nil && ctx.Err() == nil {
			onError(err)
		}
		wait := constants.DefaultMemoryWorkerInterval
		if n > 0 && err == nil {
			wait = 0
		}
		timer.Reset(wait)
	}
}

// ProcessPending 执行一轮 Worker（设计 4.3）：持 memory/worker 锁，先 Maintain，再按 queue 分片中的登记逐个作用域处理待处理的任务，
// 返回处理的任务数。任务自身的失败记入任务，不作为返回错误；每个任务之后保存心跳，失去锁时结束本轮并返回 ErrLeaseLost。
// 没有待处理任务的登记在 state 锁内移除。
func (m *Memory) ProcessPending(ctx context.Context) (int, error) {
	if m.cfg.Model == nil || m.cfg.Sessions == nil {
		return 0, agentErrors.Newf(agentErrors.CodeInvalidConfig, "抽取需要 Model 与 Sessions").WithComponent("memory").WithOperation("process")
	}
	unlock, err := m.cfg.Durable.Lock(ctx, workerID)
	if err != nil {
		return 0, err
	}
	defer func() { _ = unlock() }()
	errs := []error{m.Maintain(ctx)}
	entries, err := m.queued(ctx)
	if err != nil {
		return 0, errors.Join(append(errs, err)...)
	}
	n := 0
	for _, e := range entries {
		if e.Reason != queueJobs {
			continue
		}
		k, err := m.processEntry(ctx, e)
		n += k
		if err == nil {
			continue
		}
		if ctx.Err() != nil || errors.Is(err, errRoundOver) {
			return n, errors.Join(append(errs, err)...)
		}
		errs = append(errs, err)
	}
	return n, errors.Join(errs...)
}

// errRoundOver 包装心跳失败：无法确认仍持有 Worker 锁，结束本轮，以免与接管者重复调用模型。
var errRoundOver = errors.New("长期记忆 Worker 心跳失败，结束本轮")

// processEntry 处理一条任务登记：作用域已不是当前的直接移除登记；否则依次处理待处理的任务，最后在 state 锁内移除已没有任务的登记。
func (m *Memory) processEntry(ctx context.Context, e queueEntry) (int, error) {
	scope, s, err := m.read(ctx, e.Namespace)
	if err != nil {
		return 0, err
	}
	if scope != e.Scope {
		return 0, m.unregister(ctx, e)
	}
	jobs := pendingJobs(s)
	for i, j := range jobs {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		var result error = errJobStale
		if !jobStale(s, j.Session, j.TurnSeq, j.To) {
			result = m.processInScope(ctx, j)
		}
		if result != nil && ctx.Err() != nil {
			return i, ctx.Err()
		}
		if err := m.finishJob(ctx, j, result); err != nil {
			return i, err
		}
		snap, err := encodeSnapshot(workerID, workerBeat{At: m.now()})
		if err == nil {
			err = m.cfg.Durable.Save(context.WithoutCancel(ctx), workerID, snap)
		}
		if err != nil {
			return i + 1, fmt.Errorf("%w：%w", errRoundOver, err)
		}
	}
	var errs []error
	if len(jobs) > 0 {
		errs = append(errs, m.maintainNamespace(ctx, e.Namespace))
	}
	return len(jobs), errors.Join(append(errs, m.settle(ctx, e, hasPending))...)
}

// maintainNamespace 是处理完一批任务之后的命名空间维护：社区、反思与 Prune（设计 6.4）。它们不属于某个任务，
// 只绑定命名空间、不受任务核对，各自按依据的版本复核；错误作为 Worker 本轮的错误返回，不写入任务。
func (m *Memory) maintainNamespace(ctx context.Context, ns []string) error {
	ctx = WithNamespace(ctx, ns...)
	return errors.Join(m.syncCommunities(ctx, ns), m.reflect(ctx), m.Prune(ctx))
}

// jobKey 是上下文中正在处理的任务的绑定：mutate 在锁内据此核对作用域、LastForgetSeq 与回退标记。
type jobKey struct{}

type jobBinding struct {
	scope   string
	session string
	turnSeq int64
	to      int64
}

func jobOf(ctx context.Context) (jobBinding, bool) {
	b, ok := ctx.Value(jobKey{}).(jobBinding)
	return b, ok
}

// processInScope 只在任务的作用域仍是当前作用域时处理任务；处理期间的每次读取都核对作用域（checkJobScope），
// 每次写入在锁内核对作用域、LastForgetSeq 与回退标记（mutate），任务跨越 Purge、遗忘或回退时返回 errScopeChanged 或 errJobStale。
func (m *Memory) processInScope(ctx context.Context, j Job) error {
	meta, err := m.loadMeta(ctx, j.Namespace)
	if err != nil {
		return err
	}
	if meta.Current != j.Scope {
		return errScopeChanged
	}
	return m.process(context.WithValue(WithNamespace(ctx, j.Namespace...), jobKey{}, jobBinding{scope: j.Scope, session: j.Session, turnSeq: j.TurnSeq, to: j.To}), j)
}

// jobScope 返回 Worker 正在处理的任务的作用域；不在任务中时为空（不限定作用域）。
func jobScope(ctx context.Context) string {
	b, _ := jobOf(ctx)
	return b.scope
}

// checkJobScope 核对读取得到的作用域是正在处理的任务的作用域。
func checkJobScope(ctx context.Context, scope string) error {
	if want := jobScope(ctx); want != "" && want != scope {
		return errScopeChanged
	}
	return nil
}

// process 抽取一轮的记忆：锁外读取对话、抽取、整理与实体判重得到计划，再在一次提交中复核并写入，任务同时完成（设计第 6 节）。
// 记录 ID 由任务 Key 与下标决定；提交之前崩溃时任务仍待处理，重做从头计划，不会重复写入。
func (m *Memory) process(ctx context.Context, j Job) error {
	events, err := m.cfg.Sessions.Read(ctx, j.Session, 0)
	if err != nil {
		return err
	}
	source := events
	if !j.Aborted {
		source = session.Active(events)
	}
	transcript := renderTurn(source, j.From, j.To)
	if transcript == "" {
		// 轮次已被退回，或只有注入的召回块。
		return nil
	}
	p := &jobPlan{at: turnTime(events, j.From, m.now()), vectors: map[string][]float32{}}
	if j.Aborted {
		err = m.planLesson(ctx, j, transcript, p)
	} else {
		err = m.planTurn(ctx, j, transcript, p)
	}
	if err != nil {
		return err
	}
	return m.commitJob(ctx, j, p)
}

// planTurn 抽取一轮的记忆与关系并整理成计划。
func (m *Memory) planTurn(ctx context.Context, j Job, transcript string, p *jobPlan) error {
	var out struct {
		Memories  []candidate `json:"memories"`
		Relations []relation  `json:"relations"`
	}
	prompt := extractPrompt
	if m.cfg.Graph {
		prompt += relationPrompt
	}
	dated := "对话时间：" + p.at.Format(time.DateTime) + "\n" + transcript
	if err := m.chatJSON(ctx, strings.TrimSpace(prompt+"\n"+m.cfg.Instructions), dated, &out); err != nil {
		return err
	}
	if err := m.planMemories(ctx, j, out.Memories, p); err != nil {
		return err
	}
	var err error
	p.relations, p.dropped, p.ends, err = m.planRelations(ctx, j, out.Relations, len(out.Memories))
	return err
}

type candidate struct {
	Kind       Kind     `json:"kind"`
	Content    string   `json:"content"`
	Keywords   []string `json:"keywords"`
	Entities   []string `json:"entities"`
	Importance float64  `json:"importance"`
}

const extractPrompt = "你从一轮对话中抽取值得跨会话长期记住的信息，输出 JSON：" +
	`{"memories":[{"kind":"fact|episode|skill","content":"…","keywords":["…"],"entities":["…"],"importance":0.0}]}。` +
	"fact 是关于用户或业务的稳定事实、偏好与决定；episode 是本轮做了什么、结果如何（至多一条）；skill 是本轮验证有效、可复用的做法。" +
	"content 写成脱离上下文也能看懂的完整陈述，对话中的相对时间（昨天、上周、下个月）按对话时间换算成具体日期写入；importance 取 0 到 1，琐碎信息不要记；不要记录密码、密钥、令牌等凭证；" +
	"标注为[相关长期记忆]的内容是已有记忆，不要重复抽取。没有值得记的内容时输出 {\"memories\":[]}。只输出 JSON。"

// relationPrompt 在启用图谱时附在抽取提示词后。关系必须以 from 指出依据的记忆：遗忘或回退那条记忆时，关系随之失效。
const relationPrompt = "\n另外在同一个 JSON 中输出实体之间的关系：" +
	`"relations":[{"from":0,"subject":{"name":"…","type":"person|org|place|product|concept|…","aliases":["…"]},"predicate":"…","object":{"name":"…","type":"…"},"fact":"一句完整陈述","exclusive":true}]。` +
	"from 是这条关系依据的那条记忆在 memories 中的下标（从 0 起），每条关系都必须有；没有对应记忆的关系不要输出，需要时先把它写成一条记忆。" +
	"fact 中的相对时间同样换算成具体日期；用户本人的 name 写\"用户\"、type 写 person；predicate 用简短的动词短语，同一含义用同一说法；" +
	"exclusive 表示主体在该谓词上同时只能有一个客体（如住在、就职于），可以有多个时（如喜欢、认识）为 false。没有关系时输出空数组。"

const reconcilePrompt = "你维护长期记忆。给定一条新信息和若干条相似的已有记忆，判断如何处理，输出 JSON：" +
	`{"op":"add|update|invalidate|noop","target":"已有记忆的 id","content":"update 时合并后的完整内容"}。` +
	"add：新信息与已有记忆无关；update：新信息补充或更新了某条已有记忆，给出合并后的内容；" +
	"invalidate：新信息与某条已有记忆矛盾，以新信息为准；noop：已有记忆已经包含新信息。只输出 JSON。"

// jobPlan 是一个任务在锁外得到的写入计划，由 commitJob 在一次提交中复核并写入。
type jobPlan struct {
	at        time.Time
	decisions []decision
	relations []relation
	ends      resolvedEnds
	dropped   int                  // 缺少 from 或 from 越界的关系
	vectors   map[string][]float32 // 正文 → 向量：提交时记录的正文总是计划中出现过的某段正文
}

// 整理决定的操作。
const (
	opAdd        = "add"
	opUpdate     = "update"
	opInvalidate = "invalidate"
	opNoop       = "noop"
)

// decision 是一个候选的整理决定（设计 6.2）。目标是已有记录（target）或本任务已计划的记录（planned，候选下标；-1 表示不是）。
// shown 是展示给模型的已有记录的版本（召回只返回有效记录），提交时复核；shownPlanned 是展示的计划记录。
type decision struct {
	index        int
	id           string
	c            candidate
	op           string
	target       string
	planned      int
	content      string // update 合并后的正文
	shown        []RecordRef
	shownPlanned []int
}

// plannedRecord 是计划中、尚未提交的记录，参与后续候选的整理（任务内去重，I7）。
type plannedRecord struct {
	index   int
	id      string
	kind    Kind
	content string
	removed bool
}

// shownItem 是展示给模型的一条相似记忆：已有记录（ref 非空）或计划记录。
type shownItem struct {
	id, content string
	relevance   float64
	ref         *RecordRef
	planned     int
}

// planMemories 把各候选与相似的已有记忆以及本任务已计划的记录比较（Mem0），得到整理决定：新增、合并更新、否定旧记忆或已包含。
// 已被本任务前面的决定合并或否定的已有记录不再展示。决定的目标是计划记录时直接改写计划（设计 6.2 任务内去重）。
func (m *Memory) planMemories(ctx context.Context, j Job, cands []candidate, p *jobPlan) error {
	var planned []*plannedRecord
	superseded := map[string]bool{}
	for i, c := range cands {
		if strings.TrimSpace(c.Content) == "" || !slices.Contains(kinds, c.Kind) {
			continue
		}
		c.Importance = min(max(c.Importance, 0), 1)
		vec, err := m.vector(ctx, p, c.Content)
		if err != nil {
			return err
		}
		similar, scope, err := m.search(ctx, j.Namespace, Query{Text: c.Content, K: m.cfg.ReconcileCandidates, Kinds: []Kind{c.Kind}})
		if err == nil {
			err = checkJobScope(ctx, scope)
		}
		if err != nil {
			return err
		}
		shown := m.shownItems(similar, planned, superseded, c, vec, p)
		d := decision{index: i, id: stableID(j.Key, fmt.Sprint(i)), c: c, op: opAdd, planned: -1}
		reply := struct {
			Op      string `json:"op"`
			Target  string `json:"target"`
			Content string `json:"content"`
		}{Op: opAdd}
		if len(shown) > 0 {
			var existing strings.Builder
			for _, s := range shown {
				fmt.Fprintf(&existing, "- id=%s：%s\n", s.id, s.content)
				if s.ref != nil {
					d.shown = append(d.shown, *s.ref)
				} else {
					d.shownPlanned = append(d.shownPlanned, s.planned)
				}
			}
			if err := m.chatJSON(ctx, reconcilePrompt, "新信息："+c.Content+"\n\n已有记忆：\n"+existing.String(), &reply); err != nil {
				return err
			}
		}
		k := slices.IndexFunc(shown, func(s shownItem) bool { return s.id == reply.Target })
		var target *plannedRecord
		if k >= 0 {
			if shown[k].ref != nil {
				d.target = shown[k].ref.ID
			} else {
				target = planned[slices.IndexFunc(planned, func(r *plannedRecord) bool { return r.index == shown[k].planned })]
				d.planned = target.index
			}
		}
		if k < 0 && reply.Op != opNoop {
			reply.Op = opAdd
		}
		d.content = c.Content
		switch reply.Op {
		case opNoop:
			if k < 0 {
				continue // 模型指向不存在的记忆：候选跳过
			}
			d.op = opNoop
		case opUpdate:
			d.op = opUpdate
			if strings.TrimSpace(reply.Content) != "" {
				d.content = reply.Content
			}
			if _, err := m.vector(ctx, p, d.content); err != nil {
				return err
			}
			if target != nil {
				target.content = d.content
			} else {
				superseded[d.target] = true
				planned = append(planned, &plannedRecord{index: i, id: d.id, kind: c.Kind, content: d.content})
			}
		case opInvalidate:
			d.op = opInvalidate
			if target != nil {
				target.removed = true
			} else {
				superseded[d.target] = true
			}
			planned = append(planned, &plannedRecord{index: i, id: d.id, kind: c.Kind, content: c.Content})
		default:
			d.target, d.planned = "", -1
			planned = append(planned, &plannedRecord{index: i, id: d.id, kind: c.Kind, content: c.Content})
		}
		p.decisions = append(p.decisions, d)
	}
	return nil
}

// shownItems 合并召回的已有记录与同类别的计划记录（相似度取向量余弦与字符对重合度的较大者，大于 0 的入选），按相关度取前 ReconcileCandidates 条；
// 没有计划记录入选时保持召回的顺序。
func (m *Memory) shownItems(similar []Scored, planned []*plannedRecord, superseded map[string]bool, c candidate, vec []float32, p *jobPlan) []shownItem {
	var out []shownItem
	for _, s := range similar {
		if !superseded[s.ID] {
			ref := refOf(s.Record)
			out = append(out, shownItem{id: s.ID, content: s.Content, relevance: s.Relevance, ref: &ref, planned: -1})
		}
	}
	grams, mixed := bigrams(c.Content), false
	for _, r := range planned {
		if r.removed || r.kind != c.Kind {
			continue
		}
		if rel := max(cosine(vec, p.vectors[r.content]), overlap(grams, bigrams(r.content))); rel > 0 {
			out, mixed = append(out, shownItem{id: r.id, content: r.content, relevance: rel, planned: r.index}), true
		}
	}
	if mixed {
		slices.SortStableFunc(out, func(a, b shownItem) int { return cmp.Compare(b.relevance, a.relevance) })
	}
	return out[:min(len(out), m.cfg.ReconcileCandidates)]
}

// vector 返回正文的向量并记入计划；没有配置索引时为 nil。
func (m *Memory) vector(ctx context.Context, p *jobPlan, text string) ([]float32, error) {
	if v, ok := p.vectors[text]; ok {
		return v, nil
	}
	v, err := m.embed(ctx, text)
	if err != nil {
		return nil, err
	}
	p.vectors[text] = v
	return v, nil
}

// cosine 是两个向量的余弦相似度；任一为空或长度不同时为 0。
func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}

// pendingRecord 是提交中正在构造的计划记录。
type pendingRecord struct {
	r             Record
	mergedPlanned []int
}

// jobWrites 是 commitJob 一次提交的结果，提交之后据此更新索引。
type jobWrites struct {
	stats      JobStats
	records    []indexKey // 新建的记录
	recordVecs [][]float32
	removed    []Record // 被合并或否定的已有记录（旧版本）
	relations  relationWrites
	cascaded   cascaded
}

// commitJob 在一次提交中复核并写入任务的计划，同时完成任务（设计 6.3）。fn 可能重做（ErrLeaseLost），每次从头计算。
// 任务已不是待处理时不写入；作用域、遗忘与回退的核对由 mutate 按任务绑定完成。
func (m *Memory) commitJob(ctx context.Context, j Job, p *jobPlan) error {
	now := m.now()
	var w jobWrites
	_, err := m.mutate(ctx, j.Namespace, j.Scope, func(t *tx) error {
		w = jobWrites{}
		if cur, ok := t.Jobs.Get(j.Key); !ok || cur.Status != JobPending {
			return errNoChange
		}
		w.stats.Dropped = p.dropped
		entities := p.ends.effective(t)
		refs := m.applyDecisions(t, j, p, now, &w)
		w.relations = writeRelations(t, j, p.at, now, p.relations, refs, entities, p.ends.vectors)
		w.stats.Relations, w.stats.Dropped = w.relations.written, w.stats.Dropped+w.relations.dropped
		w.cascaded = cascade(t, now)
		w.cascaded.touch(w.relations.touched...)
		m.syncGraph(t, w.cascaded)
		t.Jobs.Delete(j.Key)
		t.DoneJobs.Put(j.Key, now)
		t.Finished.Put(j.Key, w.stats)
		return nil
	})
	if err != nil {
		return err
	}
	return errors.Join(m.dropIndex(ctx, j.Namespace, j.Scope, recordSpace, keysOf(w.removed)...),
		m.writeIndex(ctx, j.Namespace, j.Scope, recordSpace, w.records, w.recordVecs),
		m.writeIndex(ctx, j.Namespace, j.Scope, entitySpace, w.relations.created, w.relations.vecs),
		m.indexAfter(ctx, j.Namespace, j.Scope, w.cascaded))
}

// applyDecisions 在 tx 中复核并执行整理决定，返回候选下标到记录版本的映射（关系据此得到依据）。
// 复核先对全部决定执行，基准是任务开始前的状态（I6）：D1 展示的已有记录中任一条已删除、失效或版本变化，D2 已有目标已被前面通过的决定采用；
// 目标是计划记录时，该记录须仍在且正文仍是计划时的样子。不成立的决定降级为新增：正文用候选原文，来源只含本轮，不合并（复审 I3）。
func (m *Memory) applyDecisions(t *tx, j Job, p *jobPlan, now time.Time, w *jobWrites) map[int]RecordRef {
	d1 := make([]bool, len(p.decisions))
	for i, d := range p.decisions {
		d1[i] = d.op == opAdd || !slices.ContainsFunc(d.shown, func(ref RecordRef) bool {
			cur, ok := t.Records.m[ref.ID]
			return !ok || !cur.Valid() || refOf(cur) != ref
		})
	}
	built := map[int]*pendingRecord{} // 计划记录，按创建它的候选下标
	diverged := map[int]bool{}        // 正文已不是计划时样子的计划记录
	owner := map[int]int{}            // 候选下标 → 它对应的计划记录
	existing := map[int]RecordRef{}   // noop 映射到的已有记录
	taken := map[string]bool{}
	type supersede struct {
		target   string
		by       int
		transfer bool
	}
	var supersedes []supersede
	record := func(d decision, content string) *pendingRecord {
		r := &pendingRecord{r: Record{ID: d.id, Namespace: slices.Clone(t.ns), Kind: d.c.Kind, Content: content,
			Keywords: slices.Clone(d.c.Keywords), Entities: slices.Clone(d.c.Entities), Importance: d.c.Importance,
			CreatedAt: now, ValidFrom: p.at, Sources: []Source{j.source()}}}
		built[d.index], owner[d.index] = r, d.index
		return r
	}
	for i, d := range p.decisions {
		ok := d1[i]
		switch {
		case d.op == opAdd:
		case d.planned >= 0:
			ok = ok && built[d.planned] != nil && !diverged[d.planned]
		default:
			ok = ok && !taken[d.target]
		}
		if !ok {
			w.stats.Conflicts++
			record(d, d.c.Content)
			if d.op == opUpdate {
				// 后续决定看到的是合并后的正文。
				diverged[d.index] = true
				if d.planned >= 0 {
					diverged[d.planned] = true
				}
			}
			continue
		}
		switch {
		case d.op == opAdd:
			record(d, d.c.Content)
		case d.op == opNoop && d.planned >= 0:
			owner[d.index] = d.planned
		case d.op == opNoop:
			existing[d.index] = refOf(t.Records.m[d.target])
		case d.op == opUpdate && d.planned >= 0:
			r := built[d.planned]
			r.r.Content = d.content
			mergeInto(t, &r.r, d.shown)
			r.mergedPlanned = append(r.mergedPlanned, slices.DeleteFunc(slices.Clone(d.shownPlanned), func(k int) bool { return k == d.planned })...)
			owner[d.index] = d.planned
		case d.op == opUpdate:
			r := record(d, d.content)
			mergeInto(t, &r.r, d.shown)
			r.mergedPlanned = slices.Clone(d.shownPlanned)
			taken[d.target] = true
			supersedes = append(supersedes, supersede{d.target, d.index, true})
		case d.op == opInvalidate && d.planned >= 0:
			delete(built, d.planned)
			record(d, d.c.Content)
		default:
			record(d, d.c.Content)
			taken[d.target] = true
			supersedes = append(supersedes, supersede{d.target, d.index, false})
		}
	}
	// 计划记录一并创建，再补上指向计划记录的 MergedFrom（它们的版本在创建时才确定）。
	refs := map[int]RecordRef{}
	for _, k := range slices.Sorted(maps.Keys(built)) {
		r := built[k].r
		if createRecord(t, r) {
			r, _ = t.Records.Get(r.ID)
			w.records, w.recordVecs = append(w.records, recordKey(r)), append(w.recordVecs, p.vectors[r.Content])
			w.stats.Records++
		}
		refs[k] = refOf(t.Records.m[r.ID])
	}
	for _, k := range slices.Sorted(maps.Keys(built)) {
		var merged []RecordRef
		for _, q := range built[k].mergedPlanned {
			if ref, ok := refs[q]; ok && q != k && !slices.Contains(merged, ref) {
				merged = append(merged, ref)
			}
		}
		if len(merged) > 0 {
			r, _ := t.Records.Get(built[k].r.ID)
			r.MergedFrom = append(r.MergedFrom, merged...)
			t.Records.Put(r.ID, r)
		}
	}
	for _, s := range supersedes {
		cur, _ := t.Records.Get(s.target)
		t.Records.Put(cur.ID, cur.invalidated(now, ReasonSuperseded, built[s.by].r.ID))
		w.removed = append(w.removed, cur)
		if s.transfer {
			transferOrigins(t, refOf(cur), refs[s.by])
		}
	}
	out := map[int]RecordRef{}
	for i, k := range owner {
		if ref, ok := refs[k]; ok {
			out[i] = ref
		}
	}
	maps.Copy(out, existing)
	return out
}

// mergeInto 把展示的已有记录并入合并记录：MergedFrom 取这些记录，Sources 取它们的来源在前、记录原有来源在后的并集，按 (Session, From, To) 去重。
// 任一展示记录被遗忘时合并记录随之擦除，其轮次被回退时合并记录随之 rewound（复审 I4）。调用前已复核这些记录仍是展示时的版本。
func mergeInto(t *tx, r *Record, shown []RecordRef) {
	var sources []Source
	add := func(s Source) {
		if !slices.ContainsFunc(sources, func(x Source) bool { return x.Session == s.Session && x.From == s.From && x.To == s.To }) {
			sources = append(sources, s)
		}
	}
	for _, ref := range shown {
		if !slices.Contains(r.MergedFrom, ref) {
			r.MergedFrom = append(r.MergedFrom, ref)
		}
		for _, s := range t.Records.m[ref.ID].Sources {
			add(s)
		}
	}
	for _, s := range r.Sources {
		add(s)
	}
	r.Sources = sources
}

// transferOrigins 为派生自 from 的关系与名称来源各追加一条指向 to 的副本，沿用原陈述、时间与轮次：
// 合并更新不使图谱流失；原来源保留，替代者被回退、from 恢复时仍然成立。
// 全表只读遍历 table.m，只对要修改的对象 Get。
func transferOrigins(t *tx, from, to RecordRef) {
	var edges, entities []string
	for id, e := range t.Edges.m {
		if slices.ContainsFunc(e.Derivations, func(d Derivation) bool { return d.Record == from }) {
			edges = append(edges, id)
		}
	}
	for id, e := range t.Entities.m {
		if slices.ContainsFunc(e.Names, func(n EntityName) bool {
			return slices.ContainsFunc(n.Origins, func(o Origin) bool { return o.Record == from })
		}) {
			entities = append(entities, id)
		}
	}
	slices.Sort(edges)
	slices.Sort(entities)
	for _, id := range edges {
		e, _ := t.Edges.Get(id)
		i := slices.IndexFunc(e.Derivations, func(d Derivation) bool { return d.Record == from })
		d := e.Derivations[i]
		d.Record = to
		e.Derivations = addDerivation(t, e.Derivations, d)
		e.Revision++
		t.Edges.Put(id, e)
	}
	for _, id := range entities {
		e, _ := t.Entities.Get(id)
		for k, n := range e.Names {
			if i := slices.IndexFunc(n.Origins, func(o Origin) bool { return o.Record == from }); i >= 0 {
				o := n.Origins[i]
				o.Record = to
				e.Names[k].Origins = addOrigin(t, n.Origins, o, func(o Origin) Origin { return o })
			}
		}
		e.Revision++
		t.Entities.Put(id, e)
	}
}

// derive 在一次提交中写入 Worker 推导出的记录：rs 中尚不存在的记录被创建（重做的任务不会重复创建或覆盖），
// 再调用 then 做同一提交中的其余修改并级联重算；then 返回失效、需要移出索引的记录，返回 errNoChange 时整次放弃。
// 嵌入在锁外完成，索引在提交之后写入并复核。返回 rs 在提交后的版本（放弃时为空）。
func (m *Memory) derive(ctx context.Context, ns []string, scope string, rs []Record, then func(t *tx) ([]Record, error)) ([]Record, error) {
	vecs := make([][]float32, len(rs))
	for i := range rs {
		r, err := m.prepare(rs[i], ns)
		if err != nil {
			return nil, err
		}
		if vecs[i], err = m.embed(ctx, r.Content); err != nil {
			return nil, err
		}
		rs[i] = r
	}
	var created []indexKey
	var createdVecs [][]float32
	var removed, out []Record
	var c cascaded
	now := m.now()
	_, err := m.mutate(ctx, ns, scope, func(t *tx) error {
		created, createdVecs, removed, out, c, scope = nil, nil, nil, nil, cascaded{}, t.scope
		for i, r := range rs {
			if createRecord(t, r) {
				r, _ = t.Records.Get(r.ID)
				created, createdVecs = append(created, recordKey(r)), append(createdVecs, vecs[i])
			}
		}
		if then != nil {
			var err error
			if removed, err = then(t); err != nil {
				created, removed = nil, nil
				return err
			}
		}
		c = m.cascadeTx(t, now)
		for _, r := range rs {
			if cur, ok := t.Records.Get(r.ID); ok {
				out = append(out, cur)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, errors.Join(m.dropIndex(ctx, ns, scope, recordSpace, keysOf(removed)...),
		m.writeIndex(ctx, ns, scope, recordSpace, created, createdVecs), m.indexAfter(ctx, ns, scope, c))
}

const failurePrompt = "这一轮执行失败或被中止。根据对话记录写一条以后可以避免同类失败的教训，输出 JSON：" +
	`{"lesson":"…","keywords":["…"],"importance":0.0}。看不出原因时 lesson 为空。只输出 JSON。`

// planLesson 为中止的轮次计划一条教训（Reflexion）：它是这个任务唯一的写入，与其他任务一样经 commitJob 提交（设计 6.5）。
func (m *Memory) planLesson(ctx context.Context, j Job, transcript string, p *jobPlan) error {
	var out struct {
		Lesson     string   `json:"lesson"`
		Keywords   []string `json:"keywords"`
		Importance float64  `json:"importance"`
	}
	if err := m.chatJSON(ctx, failurePrompt, transcript, &out); err != nil || strings.TrimSpace(out.Lesson) == "" {
		return err
	}
	if _, err := m.vector(ctx, p, out.Lesson); err != nil {
		return err
	}
	p.decisions = []decision{{id: stableID(j.Key, "lesson"), op: opAdd, planned: -1,
		c: candidate{Kind: KindReflection, Content: out.Lesson, Keywords: out.Keywords, Importance: min(max(out.Importance, 0), 1)}}}
	return nil
}

const reflectPrompt = "以下是同一用户近期的若干经历。提炼一到三条更高层的结论（如习惯、偏好、反复出现的问题），输出 JSON：" +
	`{"insights":[{"content":"…","keywords":["…"],"importance":0.0}]}。只输出 JSON。`

// reflect 在未参与反思的有效情景累计重要度达到阈值时提炼结论（Generative Agents），并把这些情景标记为已反思。
// 结论的 ReflectedFrom 是这些情景；提交时任一情景已不是读取时的版本或已失效，整次放弃。
func (m *Memory) reflect(ctx context.Context) error {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return err
	}
	scope, s, err := m.read(ctx, ns)
	if err == nil {
		err = checkJobScope(ctx, scope)
	}
	if err != nil {
		return err
	}
	var episodes []Record
	total := 0.0
	for _, id := range slices.Sorted(maps.Keys(s.Records)) {
		r := s.Records[id]
		if r.Valid() && r.Kind == KindEpisode && !r.Reflected {
			episodes = append(episodes, r)
			total += r.Importance
		}
	}
	if total < m.cfg.ReflectionThreshold {
		return nil
	}
	var list strings.Builder
	ids := make([]string, 0, len(episodes))
	for _, r := range episodes {
		fmt.Fprintf(&list, "- %s\n", r.Content)
		ids = append(ids, r.ID)
	}
	var out struct {
		Insights []candidate `json:"insights"`
	}
	if err := m.chatJSON(ctx, reflectPrompt, list.String(), &out); err != nil {
		return err
	}
	key := stableID(ids...)
	basis := make([]RecordRef, 0, len(episodes))
	for _, r := range episodes {
		basis = append(basis, refOf(r))
	}
	var insights []Record
	for i, in := range out.Insights {
		if strings.TrimSpace(in.Content) == "" {
			continue
		}
		insights = append(insights, Record{ID: stableID(key, fmt.Sprint(i)), Kind: KindReflection, Content: in.Content, Keywords: in.Keywords,
			Importance: min(max(in.Importance, 0), 1), ReflectedFrom: basis})
	}
	// 结论与"已反思"标记在同一次提交中。
	_, err = m.derive(ctx, ns, scope, insights, func(t *tx) ([]Record, error) {
		for _, e := range episodes {
			cur, ok := t.Records.Get(e.ID)
			if !ok || !cur.Valid() || cur.Incarnation != e.Incarnation || cur.Revision != e.Revision {
				return nil, errNoChange
			}
			cur.Reflected = true
			cur.Revision++
			t.Records.Put(cur.ID, cur)
		}
		return nil, nil
	})
	return err
}

// turnTime 返回轮次开始事件的时间：记录与关系的生效时间是对话发生的时间，而不是 Worker 处理的时间。
func turnTime(events []session.Event, from int64, fallback time.Time) time.Time {
	for _, ev := range events {
		if ev.Seq == from && !ev.Time.IsZero() {
			return ev.Time
		}
	}
	return fallback
}

// memoryTools 是长期记忆自身的工具：它们的调用与结果是记忆的副本，抽取时跳过，避免被遗忘的内容经此重新学回。
var memoryTools = []string{constants.ToolMemorySearch, constants.ToolMemorySave, constants.ToolMemoryForget}

// renderTurn 把序号区间内的消息事件写成纯文本；跳过注入的召回块与记忆工具的调用和结果。
func renderTurn(events []session.Event, from, to int64) string {
	var sb strings.Builder
	skipped := map[string]bool{}
	for _, ev := range events {
		if ev.Seq < from || ev.Seq > to {
			continue
		}
		switch ev.Type {
		case constants.SessionEventUserMessage, constants.SessionEventAssistantMessage, constants.SessionEventToolResult:
		default:
			continue
		}
		var d session.MessageData
		if json.Unmarshal(ev.Data, &d) != nil || strings.HasPrefix(d.Message.Content, RecallPrefix) {
			continue
		}
		if ev.Type == constants.SessionEventToolResult &&
			(skipped[d.Message.ToolCallID] || (d.Tool != nil && slices.Contains(memoryTools, d.Tool.Name))) {
			continue
		}
		if d.Message.Content != "" {
			fmt.Fprintf(&sb, "[%s] %s\n", d.Message.Role, truncate(d.Message.Content, 2000))
		}
		for _, tc := range d.Message.ToolCalls {
			if slices.Contains(memoryTools, tc.Function.Name) {
				skipped[tc.ID] = true
				continue
			}
			fmt.Fprintf(&sb, "[%s] 调用工具 %s：%s\n", d.Message.Role, tc.Function.Name, truncate(tc.Function.Arguments, 500))
		}
	}
	return sb.String()
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func stableID(parts ...string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "\x00"))))[:32]
}

// chatJSON 请求模型并解析回复中的 JSON 对象（容忍代码块包裹与前后说明）。回复不是合法 JSON 时，把原回复与解析错误
// 交回模型修正一次：温度为 0 时重试同一请求会得到同样的错误输出，修正请求的输入不同。被截断的回复不修正。
func (m *Memory) chatJSON(ctx context.Context, system, user string, out any) error {
	msgs := []llm.Message{llm.SystemMessage(system), llm.UserMessage(user)}
	for attempt := 0; ; attempt++ {
		resp, err := llm.Chat(ctx, m.cfg.Model, msgs)
		if err != nil {
			return err
		}
		if resp.FinishReason != "" && resp.FinishReason != "stop" {
			return agentErrors.Newf(agentErrors.CodeLLMResponse, "记忆模型的回复不完整（finish_reason=%s）；因长度截断时调大模型的 MaxTokens", resp.FinishReason).
				WithComponent("memory").WithOperation("chat_json")
		}
		err = decodeJSON(resp.Content, out)
		if err == nil || attempt == constants.MemoryJSONRepairAttempts {
			return err
		}
		msgs = append(msgs, llm.Message{Role: "assistant", Content: resp.Content},
			llm.UserMessage("上面的回复不是合法 JSON（"+err.Error()+"）。按原要求重新输出完整、合法的 JSON，只输出 JSON。"))
	}
}

// decodeJSON 解析 text 中第一个 { 到最后一个 } 之间的 JSON 对象。
func decodeJSON(text string, out any) error {
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return agentErrors.Newf(agentErrors.CodeLLMResponse, "记忆模型没有返回 JSON").WithComponent("memory").WithOperation("chat_json")
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), out); err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeLLMResponse, "记忆模型返回的 JSON 无法解析").WithComponent("memory").WithOperation("chat_json")
	}
	return nil
}
