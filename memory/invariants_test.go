package memory

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// checkInvariants 校验一个作用域的 state 满足 docs/design/MEMORY_G1_STORAGE.md 3.3 中当前已实现的不变量 1～4；
// 索引与 queue 相关的不变量在 G1d、G1e 补充。测试直接写入的原始对象没有实例号（0），不参与不变量 1。
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
