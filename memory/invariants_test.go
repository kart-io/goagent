package memory

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// checkInvariants 校验一个作用域的 state 满足 docs/design/MEMORY_G1_STORAGE.md 3.3 中当前已实现的不变量；
// 后续步骤加入版本字段与索引后在此补充。
func checkInvariants(t *testing.T, ns []string, s state) {
	t.Helper()
	require.GreaterOrEqual(t, s.Seq, int64(0))
	require.NotNil(t, s.Records)
	for id, r := range s.Records {
		require.Equal(t, id, r.ID, "记录的键与 ID 一致")
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
}

// runsClean 在独立的 goroutine 中运行 fn，返回它是否没有失败（require 失败会调用 runtime.Goexit）。
func runsClean(fn func(*testing.T)) bool {
	tt := &testing.T{}
	done := make(chan struct{})
	go func() { defer close(done); fn(tt) }()
	<-done
	return !tt.Failed()
}
