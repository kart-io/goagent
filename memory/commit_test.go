package memory

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

// Worker 单次提交的测试，设计见 docs/design/MEMORY_G2_DERIVATION.md 第 6 节。

// commitFixture 是单次提交测试的环境：抽取回复固定为 extract，整理与判重的回复由各测试给出，可在模型调用时注入并发修改。
type commitFixture struct {
	t         *testing.T
	m         *Memory
	st        *hookStore
	sessions  session.Store
	ctx       context.Context
	extract   string
	reconcile func(user string) string
	resolve   func(user string) string
	onExtract func()
	reflect   error
}

func newCommitFixture(t *testing.T, cfg Config) *commitFixture {
	f := &commitFixture{t: t, st: newHookStore(t), sessions: session.NewMemoryStore(), ctx: WithNamespace(context.Background(), "u"),
		reconcile: func(string) string { return `{"op":"add"}` },
		resolve:   func(string) string { return `{"resolutions":[]}` },
	}
	model := &scriptedModel{reply: func(system, user string) (string, error) {
		switch system {
		case reconcilePrompt:
			return f.reconcile(user), nil
		case resolvePrompt:
			return f.resolve(user), nil
		case failurePrompt:
			return `{"lesson":"中止前先确认","importance":0.5}`, nil
		case reflectPrompt:
			return `{"insights":[]}`, f.reflect
		}
		if hook := f.onExtract; hook != nil {
			f.onExtract = nil
			hook()
		}
		return f.extract, nil
	}}
	cfg.Durable, cfg.Model, cfg.Sessions, cfg.Graph = f.st, model, f.sessions, true
	f.m = newMemory(t, cfg)
	return f
}

func (f *commitFixture) remember(id, content string) Record {
	r, err := f.m.Remember(f.ctx, Record{ID: id, Kind: KindFact, Content: content, Importance: 0.5})
	require.NoError(f.t, err)
	return r
}

// enqueue 追加一轮并登记任务。
func (f *commitFixture) enqueue(key string, aborted bool) Job {
	from, to := appendTurn(f.t, f.sessions, "s", aborted, llm.UserMessage("这一轮的对话"), llm.Message{Role: "assistant", Content: "好的"})
	j := Job{Key: key, Namespace: []string{"u"}, Session: "s", Scope: initialScope, From: from, To: to, Aborted: aborted, TurnSeq: stateOf(f.t, f.m).Seq}
	require.NoError(f.t, f.m.Enqueue(f.ctx, j))
	return j
}

// run 登记并处理一轮，返回处理后的任务。
func (f *commitFixture) run(key string) Job {
	f.enqueue(key, false)
	_, err := f.m.ProcessPending(f.ctx)
	require.NoError(f.t, err)
	return f.job(key)
}

func (f *commitFixture) job(key string) Job {
	j, ok, err := f.m.Job(f.ctx, key)
	require.NoError(f.t, err)
	require.True(f.t, ok)
	return j
}

func (f *commitFixture) state() state {
	s := stateOf(f.t, f.m)
	checkInvariants(f.t, []string{"u"}, s)
	return s
}

func validContents(s state) []string {
	var out []string
	for _, r := range s.Records {
		if r.Valid() {
			out = append(out, r.Content)
		}
	}
	slices.Sort(out)
	return out
}

// 提交在 head 保存时丢失租约，整个 fn 重做：统计不重复累加，写入与没有丢失租约时相同（I8）。
func TestCommitJobLeaseLostRedo(t *testing.T) {
	type outcome struct {
		stats    JobStats
		contents []string
		edges    []string
		merged   []RecordRef
		saves    int32
	}
	run := func(lose bool) outcome {
		f := newCommitFixture(t, Config{})
		x := f.remember("x", "用户住在上海")
		f.extract = `{"memories":[{"kind":"fact","content":"用户住在上海浦东","importance":0.8},{"kind":"fact","content":"用户养了一只猫","importance":0.6}],
			"relations":[{"from":0,"subject":{"name":"用户","type":"person"},"predicate":"住在","object":{"name":"浦东","type":"place"},"fact":"用户住在浦东","exclusive":true}]}`
		f.reconcile = func(user string) string {
			if strings.Contains(user, "新信息：用户住在上海浦东") {
				return `{"op":"update","target":"x","content":"用户住在上海浦东"}`
			}
			return `{"op":"add"}`
		}
		f.enqueue("k", false)
		// 登记之后第一次保存 head 就是任务的提交。
		var saves atomic.Int32
		f.st.h.before = func(op, scope, id string) error {
			if op == "save" && scope != "" && id == headID && saves.Add(1) == 1 && lose {
				return agentErrors.ErrLeaseLost
			}
			return nil
		}
		_, err := f.m.ProcessPending(f.ctx)
		require.NoError(t, err)
		j := f.job("k")
		require.Equal(t, JobDone, j.Status, j.Error)
		s := f.state()
		merged := s.Records[stableID("k", "0")]
		require.Equal(t, stableID("k", "0"), s.Records[x.ID].SupersededBy)
		var edges []string
		for id, e := range s.Edges {
			if e.Valid() {
				edges = append(edges, id+"/"+e.Fact)
			}
		}
		slices.Sort(edges)
		return outcome{stats: j.Stats, contents: validContents(s), edges: edges, merged: merged.MergedFrom, saves: saves.Load()}
	}
	want, got := run(false), run(true)
	require.Equal(t, JobStats{Records: 2, Relations: 1}, want.stats)
	require.Greater(t, got.saves, want.saves, "丢失租约后重做了提交")
	want.saves, got.saves = 0, 0
	require.Equal(t, want, got)
}

// 计划完成时任务已不是待处理（例如被另一个 Worker 完成）：提交不写入任何东西。
func TestCommitJobSkipsWhenNoLongerPending(t *testing.T) {
	f := newCommitFixture(t, Config{})
	f.extract = `{"memories":[{"kind":"fact","content":"用户住在上海","importance":0.8}]}`
	f.onExtract = func() {
		_, err := f.m.mutate(f.ctx, []string{"u"}, "", func(t *tx) error {
			t.Jobs.Delete("k")
			t.DoneJobs.Put("k", f.m.now())
			return nil
		})
		require.NoError(t, err)
	}
	j := f.run("k")
	require.Equal(t, JobDone, j.Status)
	require.Zero(t, j.Stats)
	s := f.state()
	require.Empty(t, s.Records)
	require.NotContains(t, s.Finished, "k")
}

// 展示给模型的已有记录在整理之后被修改（D1）：决定降级为新增，正文是候选原文，不合并，来源只含本轮（复审 I3、第 13 节 I2）。
// 统计记下降级与缺少合法 from 的关系。
func TestCommitJobDowngradesChangedBasis(t *testing.T) {
	f := newCommitFixture(t, Config{})
	x := f.remember("x", "用户住在上海")
	f.extract = `{"memories":[{"kind":"fact","content":"用户住在上海浦东","importance":0.8}],"relations":[
		{"from":0,"subject":{"name":"用户","type":"person"},"predicate":"住在","object":{"name":"浦东","type":"place"},"fact":"用户住在浦东"},
		{"subject":{"name":"用户","type":"person"},"predicate":"喜欢","object":{"name":"咖啡","type":"concept"}},
		{"from":5,"subject":{"name":"用户","type":"person"},"predicate":"认识","object":{"name":"小王","type":"person"}}]}`
	f.reconcile = func(string) string {
		content := "用户住在上海闵行"
		_, err := f.m.Update(f.ctx, x.ID, x.Revision, Patch{Content: &content})
		require.NoError(t, err)
		return `{"op":"update","target":"x","content":"用户住在上海浦东（合并了旧记录）"}`
	}
	queued := f.enqueue("k", false)
	_, err := f.m.ProcessPending(f.ctx)
	require.NoError(t, err)
	j := f.job("k")
	require.Equal(t, JobDone, j.Status, j.Error)
	require.Equal(t, JobStats{Records: 1, Relations: 1, Dropped: 2, Conflicts: 1}, j.Stats)
	s := f.state()
	r := s.Records[stableID("k", "0")]
	require.Equal(t, "用户住在上海浦东", r.Content)
	require.Empty(t, r.MergedFrom)
	require.Equal(t, []Source{queued.source()}, r.Sources)
	require.True(t, s.Records[x.ID].Valid(), "用户修改过的记录不被否定")
	require.Equal(t, []string{"用户住在上海浦东", "用户住在上海闵行"}, validContents(s))
}

// 复核以任务开始前的状态为准（I6），同一目标只采用第一条决定（D2）。直接构造计划，覆盖整理阶段不会产生的组合。
func TestCommitJobChecksAgainstStateBeforeTask(t *testing.T) {
	tests := []struct {
		name       string
		decisions  func(x, y RecordRef) []decision
		conflicts  int
		superseded map[string]string // 记录 → 替代它的计划记录（下标）
		contents   []string
	}{
		{
			name: "前面的决定否定了后面展示过的记录",
			decisions: func(x, y RecordRef) []decision {
				return []decision{
					{index: 0, op: opInvalidate, target: "x", planned: -1, c: candidate{Kind: KindFact, Content: "用户不住在上海"}, shown: []RecordRef{x, y}},
					{index: 1, op: opUpdate, target: "y", planned: -1, c: candidate{Kind: KindFact, Content: "用户养了两只猫"}, content: "用户养了两只猫", shown: []RecordRef{x, y}},
				}
			},
			superseded: map[string]string{"x": "0", "y": "1"},
			contents:   []string{"用户不住在上海", "用户养了两只猫"},
		},
		{
			name: "同一目标的第二条决定降级",
			decisions: func(x, _ RecordRef) []decision {
				return []decision{
					{index: 0, op: opUpdate, target: "x", planned: -1, c: candidate{Kind: KindFact, Content: "用户住在浦东"}, content: "用户住在上海浦东", shown: []RecordRef{x}},
					{index: 1, op: opInvalidate, target: "x", planned: -1, c: candidate{Kind: KindFact, Content: "用户住在杭州"}, shown: []RecordRef{x}},
					{index: 2, op: opNoop, target: "x", planned: -1, c: candidate{Kind: KindFact, Content: "用户住在上海"}, shown: []RecordRef{x}},
				}
			},
			conflicts:  2,
			superseded: map[string]string{"x": "0"},
			contents:   []string{"用户住在上海", "用户住在上海浦东", "用户住在杭州", "用户养了一只猫"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCommitFixture(t, Config{})
			x, y := f.remember("x", "用户住在上海"), f.remember("y", "用户养了一只猫")
			j := f.enqueue("k", false)
			p := &jobPlan{vectors: map[string][]float32{}}
			for _, d := range tt.decisions(refOf(x), refOf(y)) {
				d.id, d.c.Importance = stableID("k", string(rune('0'+d.index))), 0.5
				p.decisions = append(p.decisions, d)
			}
			require.NoError(t, f.m.commitJob(f.ctx, j, p))
			require.Equal(t, tt.conflicts, f.job("k").Stats.Conflicts)
			s := f.state()
			for id, by := range tt.superseded {
				require.Equal(t, stableID("k", by), s.Records[id].SupersededBy, id)
			}
			require.Equal(t, tt.contents, validContents(s))
		})
	}
}

// 同一轮中相近的候选只产生一条记录（I7）：后一条的相似项包含前一条计划记录，决定指向它时改写计划而不是新建。
func TestCommitJobDedupesWithinTurn(t *testing.T) {
	first := stableID("k", "0")
	tests := []struct {
		op       string
		contents []string
		stats    JobStats
		ref      string // 两条关系依据的记录
	}{
		{op: "noop", contents: []string{"用户住在上海"}, stats: JobStats{Records: 1, Relations: 2}, ref: first},
		{op: "update", contents: []string{"用户住在上海市"}, stats: JobStats{Records: 1, Relations: 2}, ref: first},
		{op: "invalidate", contents: []string{"用户住在上海市"}, stats: JobStats{Records: 1, Relations: 1, Dropped: 1}, ref: stableID("k", "1")},
	}
	for _, tt := range tests {
		t.Run(tt.op, func(t *testing.T) {
			f := newCommitFixture(t, Config{})
			f.extract = `{"memories":[{"kind":"fact","content":"用户住在上海","importance":0.8},{"kind":"fact","content":"用户住在上海市","importance":0.8}],"relations":[
				{"from":0,"subject":{"name":"用户","type":"person"},"predicate":"住在","object":{"name":"上海","type":"place"}},
				{"from":1,"subject":{"name":"用户","type":"person"},"predicate":"喜欢","object":{"name":"上海","type":"place"}}]}`
			f.reconcile = func(user string) string {
				require.Contains(t, user, "id="+first+"：用户住在上海\n", "前一条计划记录作为相似项")
				return `{"op":"` + tt.op + `","target":"` + first + `","content":"用户住在上海市"}`
			}
			j := f.run("k")
			require.Equal(t, JobDone, j.Status, j.Error)
			require.Equal(t, tt.stats, j.Stats)
			s := f.state()
			require.Equal(t, tt.contents, validContents(s))
			for _, e := range s.Edges {
				require.Equal(t, tt.ref, e.Derivations[0].Record.ID)
			}
		})
	}
}

// 锁外判重期间实体被用户操作改动（另一来源的别名随记录修改而删除）：判重结果作废，按引用新建实体，删除的别名不会带回（I8）。
func TestCommitJobDiscardsStaleEntityJudgement(t *testing.T) {
	f := newCommitFixture(t, Config{})
	f.extract = `{"memories":[{"kind":"fact","content":"用户在 Acme 工作","importance":0.8}],"relations":[
		{"from":0,"subject":{"name":"用户","type":"person"},"predicate":"就职于","object":{"name":"Acme","type":"org"}}]}`
	f.run("k1")
	f.extract = `{"memories":[{"kind":"fact","content":"爱克米是 Acme 的中文名","importance":0.8}],"relations":[
		{"from":0,"subject":{"name":"用户","type":"person"},"predicate":"了解","object":{"name":"爱克米","type":"org","aliases":["Acme"]}}]}`
	f.run("k2")
	alias := f.state().Records[stableID("k2", "0")]
	acme := func(s state) Entity {
		for _, e := range s.Entities {
			if e.Name == "Acme" {
				return e
			}
		}
		t.Fatal("没有 Acme 实体")
		return Entity{}
	}
	require.Contains(t, acme(f.state()).Aliases, "爱克米")

	f.extract = `{"memories":[{"kind":"fact","content":"Acme公司在招人","importance":0.8}],"relations":[
		{"from":0,"subject":{"name":"用户","type":"person"},"predicate":"关注","object":{"name":"Acme公司","type":"org"}}]}`
	f.resolve = func(user string) string {
		id := acme(f.state()).ID
		require.Contains(t, user, "id="+id)
		content := "爱克米这个名字已不再使用"
		_, err := f.m.Update(f.ctx, alias.ID, alias.Revision, Patch{Content: &content})
		require.NoError(t, err)
		return `{"resolutions":[{"index":0,"duplicate_of":"` + id + `"}]}`
	}
	j := f.run("k3")
	require.Equal(t, JobDone, j.Status, j.Error)
	s := f.state()
	require.NotContains(t, acme(s).Aliases, "爱克米")
	require.NotContains(t, acme(s).Aliases, "Acme公司", "判重作废，不并入 Acme")
	var names []string
	for _, e := range s.Entities {
		for _, n := range e.Names {
			names = append(names, n.Name)
		}
	}
	require.NotContains(t, names, "爱克米", "删除的别名没有被带回")
	require.Contains(t, names, "Acme公司", "判重作废后按引用新建")
}

// 失败教训是中止轮次任务唯一的写入，与任务完成在同一次提交中（设计 6.5）。
func TestCommitJobLesson(t *testing.T) {
	f := newCommitFixture(t, Config{})
	f.enqueue("k", true)
	_, err := f.m.ProcessPending(f.ctx)
	require.NoError(t, err)
	j := f.job("k")
	require.Equal(t, JobDone, j.Status, j.Error)
	require.Equal(t, JobStats{Records: 1}, j.Stats)
	require.Equal(t, KindReflection, f.state().Records[stableID("k", "lesson")].Kind)
}

// 提交之后的维护（反思等）失败不影响任务：任务已完成，错误作为本轮的错误返回，不计入 DroppedJobs（设计 6.4）。
func TestMaintenanceErrorDoesNotFailJob(t *testing.T) {
	f := newCommitFixture(t, Config{ReflectionThreshold: 0.1})
	f.extract = `{"memories":[{"kind":"episode","content":"用户今天完成了搬家","importance":0.9}]}`
	f.reflect = errors.New("反思失败")
	f.enqueue("k", false)
	_, err := f.m.ProcessPending(f.ctx)
	require.ErrorContains(t, err, "反思失败")
	j := f.job("k")
	require.Equal(t, JobDone, j.Status)
	require.Zero(t, j.Attempts)
	require.Zero(t, f.m.DroppedJobs())
}

// Finished 与 DoneJobs 一起由 Prune 在保留期后删除。
func TestPruneDeletesFinishedWithDoneJobs(t *testing.T) {
	f := newCommitFixture(t, Config{Retention: Retention{InvalidRetention: time.Hour}})
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	f.m.now = func() time.Time { return now }
	f.extract = `{"memories":[{"kind":"fact","content":"用户住在上海","importance":0.8}]}`
	require.Equal(t, JobStats{Records: 1}, f.run("k").Stats)
	require.Contains(t, f.state().Finished, "k")
	now = now.Add(2 * time.Hour)
	require.NoError(t, f.m.Prune(f.ctx))
	s := f.state()
	require.NotContains(t, s.DoneJobs, "k")
	require.NotContains(t, s.Finished, "k")
}

// 任务统计经覆盖层与基础快照都能还原：另一个不用缓存的实例读到的与写入的相同，覆盖层中的删除遮住基础快照中的统计。
func TestFinishedPersistsThroughOverlayAndCompaction(t *testing.T) {
	ctx := context.Background()
	st, writer := seedAndCompact(t, 3)
	reader := durableMemory(t, st, Config{StateCacheBytes: -1})
	a, b := JobStats{Records: 2, Relations: 1}, JobStats{Dropped: 1, Conflicts: 3}
	put := func(fn func(t *tx)) {
		t.Helper()
		_, err := writer.mutate(ctx, testNS, "", func(t *tx) error { fn(t); return nil })
		require.NoError(t, err)
	}
	check := func(msg string, want map[string]JobStats) {
		t.Helper()
		_, s, err := reader.read(ctx, testNS)
		require.NoError(t, err)
		require.Equal(t, want, s.Finished, msg)
	}

	put(func(t *tx) { t.Finished.Put("a", a); t.Finished.Put("b", b) })
	require.Positive(t, headOf(t, writer).OverlayItems, "统计在覆盖层中")
	check("覆盖层", map[string]JobStats{"a": a, "b": b})
	require.NoError(t, writer.compact(ctx, testNS, true, true))
	require.Zero(t, headOf(t, writer).OverlayItems)
	check("基础快照", map[string]JobStats{"a": a, "b": b})
	put(func(t *tx) { t.Finished.Delete("a") })
	check("覆盖层删除基础快照中的统计", map[string]JobStats{"b": b})
}
