package memory

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// checkInvariants 校验一个作用域的 state 满足 docs/design/MEMORY_G1_STORAGE.md 3.3 中的不变量 1～4；
// 索引的不变量 6、7 见 checkIndex，queue 的不变量 5 见 checkQueue。测试直接写入的原始对象没有实例号（0），不参与不变量 1。
func checkInvariants(t *testing.T, ns []string, s state) {
	t.Helper()
	require.GreaterOrEqual(t, s.Seq, int64(0))
	require.NotNil(t, s.Records)
	owner := map[int64]string{}
	incarnation := func(key string, n int64) {
		if n == 0 {
			return
		}
		require.Less(t, n, s.NextIncarnation, "%s 的实例号小于 NextIncarnation", key)
		prev, dup := owner[n]
		require.False(t, dup, "实例号 %d 同时属于 %s 与 %s", n, prev, key)
		owner[n] = key
	}
	for id, r := range s.Records {
		require.Equal(t, id, r.ID, "记录的键与 ID 一致")
		incarnation("记录 "+id, r.Incarnation)
		require.Equal(t, r.InvalidAt.IsZero(), r.InvalidReason == "", "记录 %s 的失效时间与原因同时设置", id)
		if r.InvalidReason == ReasonForgotten {
			require.True(t, r.Content == "" && r.Keywords == nil && r.Entities == nil, "被遗忘的记录 %s 不保留内容", id)
		}
	}
	for id, e := range s.Entities {
		incarnation("实体 "+id, e.Incarnation)
	}
	for id, e := range s.Edges {
		incarnation("关系 "+id, e.Incarnation)
	}
	for id, c := range s.Communities {
		incarnation("社区 "+id, c.Incarnation)
	}
	for key, j := range s.Jobs {
		require.Equal(t, key, j.Key, "任务的键与 Key 一致")
		require.True(t, slices.Equal(ns, j.Namespace), "任务 %s 属于命名空间 %v", key, ns)
		_, done := s.DoneJobs[key]
		require.False(t, done, "任务 %s 同时处于待处理与已完成", key)
	}
}

// indexKeys 返回 x 中命名空间 ns 之下全部作用域（含实体子命名空间）的键，排序。
func indexKeys(x *MemoryIndex, ns []string) []string {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var out []string
	for key, space := range x.spaces {
		if strings.HasPrefix(key, spaceKey(ns)+"\x00#") {
			for id := range space {
				out = append(out, id)
			}
		}
	}
	slices.Sort(out)
	return out
}

// checkIndex 在 retiring 清理完成后校验不变量 7：ns 之下只有当前作用域的索引命名空间，其中没有被遗忘记录的键。
// 不变量 6（被采用的键对应有效对象的当前版本）由 adopted 的定义保证，召回只采用这样的键。
func checkIndex(t *testing.T, m *Memory, x *MemoryIndex, ns []string) {
	t.Helper()
	scope, s, err := m.read(context.Background(), ns)
	require.NoError(t, err)
	spaces := map[string]indexSpace{}
	for _, sp := range indexSpaces {
		spaces[spaceKey(sp.namespace(ns, scope))] = sp
	}
	x.mu.RLock()
	defer x.mu.RUnlock()
	for key, space := range x.spaces {
		if !strings.HasPrefix(key, spaceKey(ns)+"\x00#") || len(space) == 0 {
			continue
		}
		sp, ok := spaces[key]
		require.True(t, ok, "索引中没有非当前作用域的键：%q", key)
		for id := range space {
			k, ok := parseIndexKey(id)
			require.True(t, ok, "键 %q 可以解析", id)
			if r, ok := s.Records[k.ID]; ok && sp.sub == "" && r.Incarnation == k.Incarnation {
				require.NotEqual(t, ReasonForgotten, r.InvalidReason, "索引中没有被遗忘记录的键 %q", id)
			}
		}
	}
}

// checkQueue 校验不变量 5：ns 的当前作用域有待处理任务时，queue 分片中有它的任务登记。
func checkQueue(t *testing.T, m *Memory, ns []string) {
	t.Helper()
	ctx := context.Background()
	scope, s, err := m.read(ctx, ns)
	require.NoError(t, err)
	if len(pendingJobs(s)) == 0 {
		return
	}
	entries, err := m.queued(ctx)
	require.NoError(t, err)
	require.Contains(t, entries, queueEntry{Namespace: ns, Scope: scope, Reason: queueJobs}, "有待处理任务的作用域有登记")
}

// requireQueueEmpty 断言 queue 分片中没有任何登记。
func requireQueueEmpty(t *testing.T, m *Memory) {
	t.Helper()
	entries, err := m.queued(context.Background())
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestCheckInvariantsDetectsViolations(t *testing.T) {
	s := state{}
	s.init()
	s.Jobs["k"] = Job{Key: "k", Namespace: testNS}
	checkInvariants(t, testNS, s)

	s.DoneJobs["k"] = s.Records["x"].CreatedAt
	require.False(t, runsClean(func(tt *testing.T) { checkInvariants(tt, testNS, s) }), "待处理与已完成重叠时报错")

	at := time.Unix(1_800_000_000, 0)
	tests := []struct {
		name string
		edit func(*state)
	}{
		{"实例号重复", func(s *state) {
			s.Records["a"] = Record{ID: "a", Incarnation: 1}
			s.Edges["e"] = Edge{ID: "e", Incarnation: 1}
		}},
		{"实例号不小于 NextIncarnation", func(s *state) { s.Records["a"] = Record{ID: "a", Incarnation: 5} }},
		{"只有失效时间", func(s *state) { s.Records["a"] = Record{ID: "a", InvalidAt: at} }},
		{"只有失效原因", func(s *state) { s.Records["a"] = Record{ID: "a", InvalidReason: ReasonExpired} }},
		{"被遗忘仍有内容", func(s *state) {
			s.Records["a"] = Record{ID: "a", InvalidAt: at, InvalidReason: ReasonForgotten, Content: "x"}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := state{NextIncarnation: 5}
			s.init()
			s.Records["ok"] = Record{ID: "ok", Incarnation: 2}
			checkInvariants(t, testNS, s)
			tt.edit(&s)
			require.False(t, runsClean(func(t2 *testing.T) { checkInvariants(t2, testNS, s) }))
		})
	}
}

// runsClean 在独立的 goroutine 中运行 fn，返回它是否没有失败（require 失败会调用 runtime.Goexit）。
func runsClean(fn func(*testing.T)) bool {
	tt := &testing.T{}
	done := make(chan struct{})
	go func() { defer close(done); fn(tt) }()
	<-done
	return !tt.Failed()
}
