package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/orchestration/internal/livecase"
	"github.com/kart-io/goagent/examples/orchestration/internal/livecase/livecasetest"
	"github.com/stretchr/testify/require"
)

func TestMissingFacts(t *testing.T) {
	for _, tc := range []struct {
		draft string
		want  []string
	}{
		{"退款 3 个工作日内原路退回，定制商品除外，邮箱 refund@demo.example。", nil},
		{"退款3个工作日内到账。", []string{"定制", "refund@demo.example"}},
		{"", RequiredFacts},
		{"三个工作日内退回，定制除外，refund@demo.example", []string{"3个工作日"}},
	} {
		require.Equal(t, tc.want, MissingFacts(tc.draft), tc.draft)
	}
}

// 不调用模型：拓扑包含写作→审校→（写作|结束）的循环。
func TestLoopTopology(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	var w, r atomic.Int64
	g, err := build(context.Background(), &livemodel.Client{}, store, &w, &r, Hooks{})
	require.NoError(t, err)
	require.NotNil(t, g)
}

const childDir, childMode = "GOAGENT_REVIEW_CHILD_DIR", "GOAGENT_REVIEW_CHILD_MODE"

type crashed struct{ Calls int64 }

func TestLiveReviewLoopRecovery(t *testing.T) {
	if dir := os.Getenv(childDir); dir != "" {
		client, err := livemodel.FromEnv()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		hooks := Hooks{}
		if os.Getenv(childMode) == "crash" {
			// 第 2 轮写作开始前退出：第 1 轮写与审都已提交检查点。
			hooks.BeforeWrite = func(round int) {
				if round == 2 {
					livecase.ExitWithResult(crashed{Calls: client.Calls()})
				}
			}
		}
		out, err := Run(context.Background(), client, dir, hooks)
		if err != nil {
			fmt.Fprintln(os.Stderr, livemodel.Redact(err.Error()))
			os.Exit(2)
		}
		livecase.ExitWithResult(out)
	}
	client := livemodel.Require(t)
	dir := t.TempDir()
	env := func(mode string) []string { return []string{childDir + "=" + dir, childMode + "=" + mode} }

	var crash crashed
	livecasetest.Result(t, livecasetest.Child(t, "TestLiveReviewLoopRecovery", env("crash")...), &crash)
	require.Equal(t, int64(2), crash.Calls, "第 1 轮写与审各 1 次请求；第一稿缺少邮箱，必然进入第 2 轮")

	var resumed Result
	livecasetest.Result(t, livecasetest.Child(t, "TestLiveReviewLoopRecovery", env("resume")...), &resumed)
	t.Logf("%s/%s 恢复：轮次 %d，新增请求 %d，预留 %d\n%s", client.Live.Provider, client.Live.Model, resumed.Rounds, resumed.NewModelCalls, resumed.Budget.ReservedRequests, resumed.Draft)
	require.True(t, resumed.Done)
	require.Equal(t, "通过", resumed.Verdict, resumed.Draft)
	require.Empty(t, MissingFacts(resumed.Draft), "宿主独立检查终稿")
	require.GreaterOrEqual(t, resumed.Rounds, 2)
	require.LessOrEqual(t, resumed.Rounds, MaxRounds)
	require.Equal(t, int64(resumed.Rounds-1), resumed.WriterRuns, "已提交的第 1 轮不再执行")
	require.Equal(t, resumed.WriterRuns, resumed.ReviewerRuns)
	require.Equal(t, int(crash.Calls+resumed.NewModelCalls), resumed.Budget.ReservedRequests, "崩溃前的请求不重发")
	require.Equal(t, 2*resumed.Rounds, resumed.Budget.ReservedRequests)

	var replayed Result
	livecasetest.Result(t, livecasetest.Child(t, "TestLiveReviewLoopRecovery", env("replay")...), &replayed)
	require.Zero(t, replayed.NewModelCalls)
	require.Zero(t, replayed.WriterRuns+replayed.ReviewerRuns)
	require.Equal(t, resumed.Draft, replayed.Draft)
	require.Equal(t, resumed.Budget, replayed.Budget)
	require.Equal(t, strconv.Itoa(resumed.Rounds), strconv.Itoa(replayed.Rounds))
}
