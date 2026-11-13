package memory

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
)

// I2（G1 3.5）：读者拿到的 state 与缓存共享，之后的写入、召回与整理都不得修改它。并发运行全部写路径与召回，
// 期间每次读取都留存 state 与它的哈希，结束后逐个比对；-race 下同时检查没有对共享 state 的写。
// 召回结果被调用方修改，也不得影响 state。
func TestReturnedStatesAreImmutable(t *testing.T) {
	m := newMemory(t, Config{Index: NewMemoryIndex(), Embedder: bigramEmbedder{}, EmbedModel: "v1", Graph: true, Communities: true,
		Model:       &scriptedModel{reply: func(string, string) (string, error) { return `{"name":"主题","summary":"摘要"}`, nil }},
		TrackAccess: true, Retention: Retention{InvalidRetention: time.Nanosecond, MaxRecords: 12}})
	ctx := WithNamespace(context.Background(), testNS...)
	g := graphOf(m)
	for i := range 16 {
		_, err := m.Remember(ctx, Record{ID: fmt.Sprintf("r%02d", i), Content: fmt.Sprintf("用户第 %d 次提到咖啡与深圳", i),
			Keywords: []string{"咖啡"}, Entities: []string{"用户"}, Importance: 0.5, Sources: []Source{{Session: "s", From: int64(i)}}})
		require.NoError(t, err)
		require.NoError(t, g.PutEntity(ctx, Entity{ID: fmt.Sprintf("e%02d", i), Namespace: testNS, Name: fmt.Sprintf("实体%d", i), Type: "x", Aliases: []string{"别名"}}))
	}
	for i := range 15 {
		require.NoError(t, g.PutEdge(ctx, Edge{ID: fmt.Sprintf("d%02d", i), Namespace: testNS, Subject: fmt.Sprintf("e%02d", i), Predicate: "关联",
			Object: fmt.Sprintf("e%02d", i+1), Fact: "关系", Sources: []Source{{Session: "s", From: int64(i)}}}))
	}

	var mu sync.Mutex
	var kept []state
	var sums []string
	keep := func() {
		_, s, err := m.read(ctx, testNS)
		require.NoError(t, err)
		sum := stateJSON(t, s)
		mu.Lock()
		kept, sums = append(kept, s), append(sums, sum)
		mu.Unlock()
	}
	keep()
	ops := []func(i int){
		func(i int) {
			id := fmt.Sprintf("r%02d", i%16)
			r, err := m.Get(ctx, id)
			if agentErrors.IsCode(err, agentErrors.CodeStoreNotFound) {
				return // 并发的遗忘与整理可能已物理删除
			}
			require.NoError(t, err)
			content := r.Content + "（更新）"
			_, _ = m.Update(ctx, id, r.Revision, Patch{Content: &content, Keywords: &[]string{"更新"}})
		},
		func(i int) { _ = m.Forget(ctx, fmt.Sprintf("r%02d", (i*7)%16)) },
		func(i int) { require.NoError(t, m.Rewound(ctx, "s", int64(14-i%4))) },
		func(int) { require.NoError(t, m.Prune(ctx)) },
		func(int) { require.NoError(t, m.RefreshCommunities(ctx)) },
		func(int) {
			got, err := m.Recall(ctx, Query{Text: "咖啡深圳"})
			require.NoError(t, err)
			for i := range got {
				got[i].Content = "被调用方改写"
				if len(got[i].Keywords) > 0 {
					got[i].Keywords[0] = "被调用方改写"
				}
			}
		},
		func(int) {
			rel, err := m.Related(ctx, "实体3", nil, 2, 5)
			require.NoError(t, err)
			for i := range rel.Communities {
				if len(rel.Communities[i].Members) > 0 {
					rel.Communities[i].Members[0] = "被调用方改写"
				}
			}
		},
		func(int) {
			s, err := m.Snapshot(ctx, "咖啡")
			require.NoError(t, err)
			out, _, err := m.Hydrate(ctx, s, s.Refs)
			require.NoError(t, err)
			for i := range out.Records {
				if len(out.Records[i].Keywords) > 0 {
					out.Records[i].Keywords[0] = "被调用方改写"
				}
			}
		},
	}
	var wg sync.WaitGroup
	for w, op := range ops {
		wg.Go(func() {
			for i := range 6 {
				op(w + i)
				keep()
			}
		})
	}
	wg.Wait()
	m.waitAccess()
	for i, s := range kept {
		require.Equal(t, sums[i], stateJSON(t, s), "第 %d 份留存的 state 被修改", i)
	}
	checkInvariants(t, testNS, kept[len(kept)-1])
}
