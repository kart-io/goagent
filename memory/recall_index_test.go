package memory

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// bigram 索引与原实现的等价性（docs/design/MEMORY_G1_BIGRAM_INDEX.md 第 7 节）：同一组记录，原 recall（逐条构造字符对）
// 与 recallState（索引）返回的结果逐项相同：ID、Relevance 与 Score 逐位相同，顺序相同。

// recallQueries 生成 n 个查询：正文子串、实体名、随机汉字、中英混合、空查询；部分带 Kinds 过滤与不同的 K。
func recallQueries(rng *rand.Rand, s state, n int) []Query {
	var contents, names []string
	for _, r := range s.Records {
		contents = append(contents, r.Content)
	}
	for _, e := range s.Entities {
		names = append(names, e.Name)
	}
	kinds := []Kind{KindFact, KindEpisode, KindReflection, KindSkill}
	out := make([]Query, 0, n)
	for i := range n {
		var text string
		switch i % 6 {
		case 0, 1:
			c := []rune(contents[rng.IntN(len(contents))])
			o, l := rng.IntN(len(c)), 2+rng.IntN(20)
			text = string(c[o:min(len(c), o+l)])
		case 2:
			if len(names) > 0 {
				text = names[rng.IntN(len(names))] + "的情况"
			}
		case 3:
			r := make([]rune, 1+rng.IntN(8))
			for j := range r {
				r[j] = rune(0x4e00 + rng.IntN(3000))
			}
			text = string(r)
		case 4:
			text = "用户 Likes 茶 ABC123，" + string([]rune(contents[rng.IntN(len(contents))])[:3])
		default:
			text = ""
		}
		q := Query{Text: text, K: []int{0, 1, 5, 50}[rng.IntN(4)]}
		if rng.IntN(4) == 0 {
			q.Kinds = []Kind{kinds[rng.IntN(len(kinds))]}
		}
		out = append(out, q)
	}
	return out
}

// requireSameRanking 比较两组结果：ID、Relevance、Score 逐位相同，顺序相同。
func requireSameRanking(t *testing.T, want, got []Scored, q Query) {
	t.Helper()
	require.Len(t, got, len(want), "查询 %q", q.Text)
	for i := range want {
		require.Equal(t, want[i].ID, got[i].ID, "查询 %q 第 %d 项", q.Text, i)
		require.Equal(t, math.Float64bits(want[i].Relevance), math.Float64bits(got[i].Relevance), "查询 %q 第 %d 项的 Relevance", q.Text, i)
		require.Equal(t, math.Float64bits(want[i].Score), math.Float64bits(got[i].Score), "查询 %q 第 %d 项的 Score", q.Text, i)
		require.Equal(t, want[i].Content, got[i].Content)
	}
}

// bruteRecall 是 G0 的逐条召回（不含向量）：每条有效记录现算正文与关键词的字符对重合，作为索引召回的对照。
func bruteRecall(m *Memory, s state, q Query) []Scored {
	now := m.now()
	query := bigrams(q.Text)
	var out []Scored
	for _, r := range s.Records {
		if !r.Valid() || (len(q.Kinds) > 0 && !slices.Contains(q.Kinds, r.Kind)) {
			continue
		}
		rel := overlap(query, bigrams(r.Content+" "+strings.Join(r.Keywords, " ")))
		if len(query) > 0 && rel == 0 {
			continue
		}
		out = append(out, Scored{Record: r, Relevance: rel, Score: m.cfg.Weights.Relevance*rel + m.cfg.Weights.Recency*m.recency(r.CreatedAt, now) + m.cfg.Weights.Importance*r.Importance})
	}
	slices.SortFunc(out, func(a, b Scored) int {
		if a.Score != b.Score {
			if a.Score > b.Score {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out[:min(m.recallK(q), len(out))]
}

func TestBigramIndexEquivalentToRecall(t *testing.T) {
	ctx := context.Background()
	for _, ds := range benchDatasets(t) {
		for _, n := range []int{150, 1000} { // 150 < minIndexedRecords：不建索引的路径
			t.Run(fmt.Sprintf("%s/%d", ds.name, n), func(t *testing.T) {
				m := manualCompaction(durableMemory(t, newHookStore(t), Config{}))
				now := time.Unix(1_800_500_000, 0).UTC()
				m.now = func() time.Time { return now }
				rng := rand.New(rand.NewPCG(uint64(n), 5))
				kinds := []Kind{KindFact, KindEpisode, KindReflection, KindSkill}
				_, err := m.mutate(ctx, testNS, "", func(t *tx) error {
					if err := benchState(n, ds.gen)(t); err != nil {
						return err
					}
					for _, id := range t.Records.Keys() {
						r, _ := t.Records.Get(id)
						r.Kind = kinds[rng.IntN(len(kinds))]
						t.Records.Put(id, r)
					}
					return nil
				})
				require.NoError(t, err)
				require.NoError(t, m.compact(ctx, testNS, true, true))
				check := func(phase string, queries int) {
					_, v, err := m.readView(ctx, testNS)
					require.NoError(t, err)
					if n >= minIndexedRecords {
						require.True(t, v.idx.indexed(), "%s：基础快照有索引", phase)
					}
					for _, q := range recallQueries(rng, v.cur, queries) {
						want := bruteRecall(m, v.cur, q)
						got, err := m.recallState(ctx, testNS, q)
						require.NoError(t, err)
						requireSameRanking(t, want, got, q)
					}
				}
				check("基础快照", 1000)

				// 覆盖层：修改正文、失效、恢复、删除、新增，不压缩，基础索引被遮蔽的部分由覆盖层补上。
				_, err = m.mutate(ctx, testNS, "", func(t *tx) error {
					ids := t.Records.Keys()
					for i := range 120 {
						id := ids[rng.IntN(len(ids))]
						r, ok := t.Records.Get(id)
						if !ok && i%5 != 4 {
							continue // 已在本轮被删除
						}
						switch i % 5 {
						case 0:
							r.Content = ds.gen(rng, 30) + "新的内容"
							t.Records.Put(id, r)
						case 1:
							t.Records.Put(id, r.invalidated(now, ReasonExpired, ""))
						case 2:
							r.InvalidAt, r.InvalidReason = time.Time{}, ""
							t.Records.Put(id, r)
						case 3:
							t.Records.Delete(id)
						default:
							nid := fmt.Sprintf("new-%03d", i)
							t.Records.Put(nid, Record{ID: nid, Kind: KindFact, Content: ds.gen(rng, 40), Keywords: []string{"新增"}, CreatedAt: now})
						}
					}
					return nil
				})
				require.NoError(t, err)
				h := headOf(t, m)
				require.Positive(t, h.OverlayItems, "这些修改留在覆盖层中")
				check("基础快照加覆盖层", 1000)
			})
		}
	}
}

// 字符对的打包与原实现的集合相同，包括大小写、标点、数字与正文末字和关键词首字相连。
func TestRecordBigramsMatchLegacy(t *testing.T) {
	for _, r := range []Record{
		{Content: "Hello, World! 你好，世界 123"},
		{Content: "末", Keywords: []string{"首字", "ÄÖ", "ǅ"}},
		{Content: "\xff\xfe无效字节ＡＢＣ"},
		{Content: ""},
		{Content: strings.Repeat("重复", 10)},
	} {
		legacy := bigrams(r.Content + " " + strings.Join(r.Keywords, " "))
		packed := recordBigrams(r, nil)
		require.Len(t, packed, len(legacy), "%q", r.Content)
		for _, b := range packed {
			s := string([]rune{rune(uint32(b >> 32)), rune(uint32(b))})
			require.True(t, legacy[s], "%q 的字符对 %q", r.Content, s)
		}
	}
}
