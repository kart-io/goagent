package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/memory"
)

func budgetRetention(ctx context.Context, w io.Writer, root string) error {
	return runSteps(w,
		step{"核心记忆总在召回块最前", func() error { return pinnedFirst(ctx, root) }},
		step{"召回块按 rune 截断", func() error { return recallRunes(ctx, w, root) }},
		step{"权重、半衰期与类别过滤", func() error { return ranking(ctx, w, root) }},
		step{"TTL、容量淘汰与失效保留期", func() error { return retention(ctx, w, root) }},
		step{"换嵌入模型后 Reindex", func() error { return reindex(ctx, w, root) }},
	)
}

// blockItems 取召回块中的条目行（以 "- " 开头）。
func blockItems(block string) []string {
	var out []string
	for _, l := range strings.Split(block, "\n") {
		if strings.HasPrefix(l, "- ") {
			out = append(out, l)
		}
	}
	return out
}

func remember(ctx context.Context, m *memory.Memory, recs ...memory.Record) ([]memory.Record, error) {
	out := make([]memory.Record, len(recs))
	for i, r := range recs {
		var err error
		if out[i], err = m.Remember(ctx, r); err != nil {
			return nil, fmt.Errorf("写入 %q：%w", r.Content, err)
		}
	}
	return out, nil
}

func pinnedFirst(ctx context.Context, root string) error {
	e, err := newEnv(root, "pinned", memory.Config{})
	if err != nil {
		return err
	}
	actx := memory.WithNamespace(ctx, alice...)
	if _, err := remember(actx, e.mem,
		memory.Record{Kind: memory.KindFact, Content: "用户喜欢喝咖啡", Importance: 0.9},
		memory.Record{Kind: memory.KindFact, Content: "用户的名字叫小明", Importance: 0.1, Pinned: true},
		memory.Record{Kind: memory.KindFact, Content: "用户周末去爬山", Importance: 0.9},
	); err != nil {
		return err
	}
	pinned, err := e.mem.Pinned(actx)
	if err != nil {
		return err
	}
	var checks []check
	for _, q := range []string{"今天天气怎么样", "推荐一款咖啡"} {
		block, err := e.mem.RecallBlock(actx, q)
		if err != nil {
			return err
		}
		items := blockItems(block)
		first := ""
		if len(items) > 0 {
			first = items[0]
		}
		checks = append(checks,
			check{"查询 " + q + "：召回块以 RecallPrefix 开头", strings.HasPrefix(block, memory.RecallPrefix), true},
			check{"查询 " + q + "：第一条是核心记忆（实际 " + first + "）", strings.HasSuffix(first, "用户的名字叫小明"), true},
		)
	}
	return verify(append(checks, check{"Pinned 返回的核心记忆", len(pinned), 1})...)
}

func recallRunes(ctx context.Context, w io.Writer, root string) error {
	const budget = 200
	e, err := newEnv(root, "runes", memory.Config{RecallRunes: budget, RecallK: 20})
	if err != nil {
		return err
	}
	actx := memory.WithNamespace(ctx, alice...)
	// 每行带 [类别 ID 日期] 前缀，短 ID 让预算能放下多条，便于观察截断。
	recs := []memory.Record{{ID: "veg", Kind: memory.KindFact, Content: "用户是素食者，行程不要安排含肉的餐食", Importance: 0.5, Pinned: true}}
	for i := range 10 {
		recs = append(recs, memory.Record{ID: fmt.Sprintf("trip%d", i+1), Kind: memory.KindEpisode, Importance: float64(i+1) / 10,
			Content: fmt.Sprintf("第%d次旅行：用户去了第%d个城市，参观博物馆与老街", i+1, i+1)})
	}
	if _, err := remember(actx, e.mem, recs...); err != nil {
		return err
	}
	got, err := e.mem.Retrieve(actx, "旅行计划")
	if err != nil {
		return err
	}
	ranked, err := e.mem.Recall(actx, memory.Query{Text: "旅行计划", K: 20})
	if err != nil {
		return err
	}
	if len(got.Records) < 2 {
		return fmt.Errorf("预算 %d 应至少写入核心记忆与一条召回结果，实际 %d 条：\n%s", budget, len(got.Records), got.Text)
	}
	var inBlock, missingInText, leaked []string
	for _, r := range got.Records {
		inBlock = append(inBlock, r.Content)
		if !strings.Contains(got.Text, r.Content) {
			missingInText = append(missingInText, r.Content)
		}
	}
	for _, r := range recs {
		if !slices.Contains(inBlock, r.Content) && strings.Contains(got.Text, r.Content) {
			leaked = append(leaked, r.Content)
		}
	}
	// 写入块的召回结果应是排序结果的前缀：低分的先被舍弃。
	var rankedPrefix []string
	for _, r := range ranked {
		if !r.Pinned && len(rankedPrefix) < len(inBlock)-1 {
			rankedPrefix = append(rankedPrefix, r.Content)
		}
	}
	runes := utf8.RuneCountInString(got.Text)
	_, _ = fmt.Fprintf(w, "    召回块 %d 个 rune（%d 字节），写入 %d/%d 条记录\n", runes, len(got.Text), len(got.Records), len(recs))
	return verify(
		check{"整块不超过 RecallRunes（rune 数 " + fmt.Sprint(runes) + "）", runes <= budget, true},
		check{"按 rune 而非字节计（块的字节数超过预算）", len(got.Text) > budget, true},
		check{"发生了截断", len(got.Records) < len(recs), true},
		check{"核心记忆排在第一", got.Records[0].Pinned, true},
		check{"Retrieval.Records 都出现在块中", missingInText, []string(nil)},
		check{"不在 Retrieval.Records 的记录未出现在块中", leaked, []string(nil)},
		check{"写入的召回结果是排序前缀", inBlock[1:], rankedPrefix},
	)
}

func ranking(ctx context.Context, w io.Writer, root string) error {
	e, err := newEnv(root, "ranking", memory.Config{})
	if err != nil {
		return err
	}
	actx := memory.WithNamespace(ctx, alice...)
	if _, err := remember(actx, e.mem, memory.Record{Kind: memory.KindFact, Content: "用户喜欢喝绿茶", Importance: 0.9}); err != nil {
		return err
	}
	e.clock.Advance(30 * 24 * time.Hour)
	if _, err := remember(actx, e.mem,
		memory.Record{Kind: memory.KindFact, Content: "用户喜欢喝红茶", Importance: 0.2},
		memory.Record{Kind: memory.KindEpisode, Content: "用户上周在杭州喝了龙井茶", Importance: 0.5},
	); err != nil {
		return err
	}
	type order struct {
		name    string
		weights memory.Weights
		query   memory.Query
		want    []string
	}
	facts := []memory.Kind{memory.KindFact}
	orders := []order{
		{"只看新近性", memory.Weights{Recency: 1}, memory.Query{Text: "喜欢喝茶", Kinds: facts}, []string{"用户喜欢喝红茶", "用户喜欢喝绿茶"}},
		{"只看重要性", memory.Weights{Importance: 1}, memory.Query{Text: "喜欢喝茶", Kinds: facts}, []string{"用户喜欢喝绿茶", "用户喜欢喝红茶"}},
		{"类别过滤 episode", memory.Weights{Recency: 1}, memory.Query{Text: "龙井茶", Kinds: []memory.Kind{memory.KindEpisode}}, []string{"用户上周在杭州喝了龙井茶"}},
		{"空查询按新近性", memory.Weights{Recency: 1}, memory.Query{Kinds: facts}, []string{"用户喜欢喝红茶", "用户喜欢喝绿茶"}},
	}
	var checks []check
	for _, o := range orders {
		m, err := e.sibling(memory.Config{Weights: o.weights, HalfLife: 7 * 24 * time.Hour})
		if err != nil {
			return err
		}
		rs, err := m.Recall(actx, o.query)
		if err != nil {
			return err
		}
		checks = append(checks, check{o.name, contents(rs), o.want})
	}
	m, err := e.sibling(memory.Config{Weights: memory.Weights{Recency: 1}, HalfLife: 7 * 24 * time.Hour})
	if err != nil {
		return err
	}
	score := func() (float64, error) {
		rs, err := m.Recall(actx, memory.Query{Text: "红茶", Kinds: facts, K: 1})
		if err != nil || len(rs) == 0 {
			return 0, fmt.Errorf("没有召回红茶：%w", err)
		}
		return rs[0].Score, nil
	}
	fresh, err := score()
	if err != nil {
		return err
	}
	e.clock.Advance(14 * 24 * time.Hour)
	aged, err := score()
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "    半衰期 7 天：红茶得分 %.3f → 14 天后 %.3f\n", fresh, aged)
	return verify(append(checks,
		check{fmt.Sprintf("两个半衰期后新近性约为原来的 1/4（%.3f → %.3f）", fresh, aged), math.Abs(aged/fresh-0.25) < 0.01, true},
		check{"衰减不删除：两个半衰期后仍可召回", aged > 0, true},
	)...)
}

func retention(ctx context.Context, w io.Writer, root string) error {
	e, err := newEnv(root, "retention", memory.Config{Retention: memory.Retention{
		TTL:              map[memory.Kind]time.Duration{memory.KindEpisode: 24 * time.Hour},
		MaxRecords:       3,
		InvalidRetention: 48 * time.Hour,
	}})
	if err != nil {
		return err
	}
	actx := memory.WithNamespace(ctx, alice...)
	recs, err := remember(actx, e.mem,
		memory.Record{ID: "pinned", Kind: memory.KindFact, Content: "用户是左撇子", Importance: 0.05, Pinned: true},
		memory.Record{ID: "f-high", Kind: memory.KindFact, Content: "用户对青霉素过敏", Importance: 0.9},
		memory.Record{ID: "f-mid", Kind: memory.KindFact, Content: "用户喜欢靠窗的座位", Importance: 0.5},
		memory.Record{ID: "f-low", Kind: memory.KindFact, Content: "用户上次点了一杯柠檬水", Importance: 0.1},
		memory.Record{ID: "episode", Kind: memory.KindEpisode, Content: "用户今天咨询了退票流程", Importance: 0.8},
	)
	if err != nil {
		return err
	}
	type state struct {
		valid  bool
		reason memory.InvalidReason
		found  bool
	}
	snapshot := func() (map[string]state, error) {
		out := map[string]state{}
		for _, r := range recs {
			got, err := e.mem.Get(actx, r.ID)
			switch {
			case agentErrors.IsCode(err, agentErrors.CodeStoreNotFound):
				out[r.ID] = state{}
			case err != nil:
				return nil, err
			default:
				out[r.ID] = state{valid: got.Valid(), reason: got.InvalidReason, found: true}
			}
		}
		return out, nil
	}
	valid := state{valid: true, found: true}
	expired := state{reason: memory.ReasonExpired, found: true}
	forgotten := state{reason: memory.ReasonForgotten, found: true}
	gone := state{}
	// 每个阶段先推进时间、执行动作，再 Prune，最后核对所有记录的状态。
	stages := []struct {
		name    string
		advance time.Duration
		action  func() error
		want    map[string]state
	}{
		{"25 小时后：情景 TTL 到期；未钉住的有效记录 3 条，未超容量", 25 * time.Hour, nil,
			map[string]state{"pinned": valid, "f-high": valid, "f-mid": valid, "f-low": valid, "episode": expired}},
		{"再写一条事实后超出容量：淘汰得分最低的未钉住记录", 0, func() error {
			_, err := e.mem.Remember(actx, memory.Record{ID: "f-new", Kind: memory.KindFact, Content: "用户订了下周的机票", Importance: 0.7})
			return err
		}, map[string]state{"pinned": valid, "f-high": valid, "f-mid": valid, "f-low": expired, "episode": expired}},
		{"遗忘一条：失效但仍保留", time.Hour, func() error { return e.mem.Forget(actx, "f-mid") },
			map[string]state{"pinned": valid, "f-high": valid, "f-mid": forgotten, "f-low": expired, "episode": expired}},
		{"超过失效保留期 48 小时：失效记录被物理删除", 49 * time.Hour, nil,
			map[string]state{"pinned": valid, "f-high": valid, "f-mid": gone, "f-low": gone, "episode": gone}},
	}
	var checks []check
	for _, s := range stages {
		e.clock.Advance(s.advance)
		if s.action != nil {
			if err := s.action(); err != nil {
				return fmt.Errorf("%s：%w", s.name, err)
			}
		}
		if err := e.mem.Prune(actx); err != nil {
			return fmt.Errorf("%s：Prune：%w", s.name, err)
		}
		got, err := snapshot()
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(w, "    %s\n", s.name)
		checks = append(checks, check{s.name, got, s.want})
	}
	rs, err := e.mem.Recall(actx, memory.Query{Text: "退票流程"})
	if err != nil {
		return err
	}
	return verify(append(checks, check{"过期的情景不再召回", contents(rs), []string(nil)})...)
}

// vectorKeys 按 memory README 公开的布局列出记录 id 的向量键：键在 append(ns, "#"+scope) 下，形如 ID#实例号.ContentRev#模型摘要。
func vectorKeys(ctx context.Context, idx *memory.MemoryIndex, m *memory.Memory, ns []string, id string) ([]string, error) {
	vs, err := m.CacheVersion(ctx, [][]string{ns})
	if err != nil {
		return nil, err
	}
	if len(vs) != 1 {
		return nil, fmt.Errorf("CacheVersion 应返回 1 个命名空间的版本，实际 %#v", vs)
	}
	ids, err := idx.IDs(ctx, append(slices.Clone(ns), "#"+vs[0].Scope))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, k := range ids {
		if strings.HasPrefix(k, id+"#") {
			out = append(out, k)
		}
	}
	return out, nil
}

func reindex(ctx context.Context, w io.Writer, root string) error {
	concepts := [][]string{{"猫", "宠物", "喵"}, {"咖啡", "拿铁", "美式"}, {"跑步", "健身", "运动"}}
	idx := memory.NewMemoryIndex()
	e, err := newEnv(root, "reindex", memory.Config{Index: idx, Embedder: fakeEmbedder{concepts: concepts}, EmbedModel: "fake-v1"})
	if err != nil {
		return err
	}
	actx := memory.WithNamespace(ctx, alice...)
	recs, err := remember(actx, e.mem,
		memory.Record{Kind: memory.KindFact, Content: "用户家里有一只猫", Importance: 0.5},
		memory.Record{Kind: memory.KindFact, Content: "用户每天喝一杯拿铁", Importance: 0.5},
	)
	if err != nil {
		return err
	}
	// "宠物" 与 "用户家里有一只猫" 没有共同的字符对，只能靠向量找到。
	relevance := func(m *memory.Memory) (float64, error) {
		rs, err := m.Recall(actx, memory.Query{Text: "宠物", K: 5})
		if err != nil {
			return 0, err
		}
		for _, r := range rs {
			if r.ID == recs[0].ID {
				return r.Relevance, nil
			}
		}
		return 0, nil
	}
	v1, err := relevance(e.mem)
	if err != nil {
		return err
	}
	// 新版本程序换了嵌入模型：向量空间不兼容，旧向量不能被采用。
	m2, err := e.sibling(memory.Config{Index: idx, Embedder: fakeEmbedder{concepts: concepts, shift: 1}, EmbedModel: "fake-v2"})
	if err != nil {
		return err
	}
	stale, err := relevance(m2)
	if err != nil {
		return err
	}
	if err := m2.Reindex(actx, false); err != nil {
		return err
	}
	rebuilt, err := relevance(m2)
	if err != nil {
		return err
	}
	keysBefore, err := vectorKeys(ctx, idx, m2, alice, recs[1].ID)
	if err != nil {
		return err
	}
	if err := m2.Forget(actx, recs[1].ID); err != nil {
		return err
	}
	if err := m2.Maintain(ctx); err != nil {
		return err
	}
	keysAfter, err := vectorKeys(ctx, idx, m2, alice, recs[1].ID)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "    \"宠物\"→猫 的相关性：v1=%.2f，换模型未重建=%.2f，Reindex 后=%.2f；被遗忘记录的向量键 %d → %d\n", v1, stale, rebuilt, len(keysBefore), len(keysAfter))
	return verify(
		check{fmt.Sprintf("v1 向量召回（相关性 %.2f）", v1), v1 > 0.5, true},
		check{fmt.Sprintf("换模型后不采用旧向量（相关性 %.2f）", stale), stale < 0.5, true},
		check{fmt.Sprintf("Reindex 后恢复（相关性 %.2f）", rebuilt), rebuilt > 0.5, true},
		check{"遗忘前存在向量键", len(keysBefore) > 0, true},
		check{"遗忘后向量键被清理", keysAfter, []string(nil)},
	)
}
