package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/internal/livemodel/livemodeltest"
	"github.com/kart-io/goagent/examples/orchestration/internal/demomodel"
	"github.com/kart-io/goagent/examples/orchestration/internal/livecase"
	"github.com/kart-io/goagent/examples/orchestration/internal/livecase/livecasetest"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

// scriptedClient 按申请原因给出政策结论，离线复现与真实模型相同的路径。
func scriptedClient() *livemodel.Client {
	var calls atomic.Int64
	return livemodel.Wrap(&demomodel.Model{Calls: &calls, Respond: func(r *llm.CompletionRequest) llm.StreamChunk {
		text := "结论：退款\n未拆封且在 7 天内。"
		if strings.Contains(r.Messages[len(r.Messages)-1].Content, "穿过") {
			text = "结论：拒绝\n已使用且超过 7 天。"
		}
		return llm.StreamChunk{Content: text, Delta: text}
	}}, livemodel.Config{Provider: "scripted", Model: "v1"})
}

func TestPayIsIdempotentPerRequest(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	var payments atomic.Int64
	r := Requests["R-SMALL"].Refund
	first, err := pay(context.Background(), store, r, &payments)
	require.NoError(t, err)
	again, err := pay(context.Background(), store, r, &payments)
	require.NoError(t, err)
	require.Equal(t, first, again)
	require.Equal(t, int64(1), payments.Load())
	r.Amount++
	_, err = pay(context.Background(), store, r, &payments)
	require.ErrorContains(t, err, "金额不一致")
	require.Equal(t, int64(1), payments.Load())
}

const (
	childDir, childStep, childScripted = "GOAGENT_APPROVAL_DIR", "GOAGENT_APPROVAL_STEP", "GOAGENT_APPROVAL_SCRIPTED"
	childKey                           = "GOAGENT_APPROVAL_KEY"
)

// childMain 执行一步大额流程：start、approve-crash（付款后退出）、resume。
func childMain(testName string) {
	client := scriptedClient()
	if os.Getenv(childScripted) != "1" {
		var err error
		if client, err = livemodel.FromEnv(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	hooks := Hooks{}
	key := ""
	if os.Getenv(childStep) == "approve-crash" {
		key = os.Getenv(childKey)
		hooks.AfterPay = func() { livecase.ExitWithResult(Result{Status: "crashed-after-pay"}) }
	}
	out, err := Run(context.Background(), client, os.Getenv(childDir), "R-LARGE", key, hooks)
	if err != nil {
		fmt.Fprintln(os.Stderr, livemodel.Redact(err.Error()))
		os.Exit(2)
	}
	livecase.ExitWithResult(out)
}

func exerciseFlows(t *testing.T, client *livemodel.Client, testName string, scripted bool) {
	for _, id := range []string{"R-SMALL", "R-REJECT"} {
		t.Run(id, func(t *testing.T) {
			dir := t.TempDir()
			want := Requests[id]
			first, err := Run(context.Background(), client, dir, id, "", Hooks{})
			t.Logf("%s：%+v", id, first)
			require.NoError(t, err)
			require.Equal(t, "completed", first.Status)
			require.Equal(t, want.Decision, first.Decision)
			require.Equal(t, int64(1), first.NewModelCalls)
			receipt, paid, err := LedgerReceipt(dir, id)
			require.NoError(t, err)
			if want.Decision == "退款" {
				require.True(t, paid)
				require.Equal(t, receipt, first.Receipt)
				require.Equal(t, int64(1), first.NewPayments)
			} else {
				require.False(t, paid, "拒绝的申请不付款")
				require.Zero(t, first.NewPayments)
			}
			again, err := Run(context.Background(), client, dir, id, "", Hooks{})
			require.NoError(t, err)
			require.Zero(t, again.NewModelCalls+again.NewPayments)
			require.Equal(t, first.Receipt, again.Receipt)
		})
	}
	t.Run("R-LARGE", func(t *testing.T) {
		dir := t.TempDir()
		env := func(step string, extra ...string) []string {
			e := []string{childDir + "=" + dir, childStep + "=" + step}
			if scripted {
				e = append(e, childScripted+"=1")
			}
			return append(e, extra...)
		}
		var waiting Result
		livecasetest.Result(t, livecasetest.Child(t, testName, env("start")...), &waiting)
		require.Equal(t, "waiting", waiting.Status)
		require.Equal(t, "退款", waiting.Decision)
		require.NotEmpty(t, waiting.ApprovalKey)
		require.Equal(t, int64(1), waiting.NewModelCalls)
		_, paid, err := LedgerReceipt(dir, "R-LARGE")
		require.NoError(t, err)
		require.False(t, paid, "审批前不得付款")

		// 再次推进不带审批键：仍在等待，不重复请求模型。
		var still Result
		livecasetest.Result(t, livecasetest.Child(t, testName, env("start")...), &still)
		require.Equal(t, "waiting", still.Status)
		require.Equal(t, waiting.ApprovalKey, still.ApprovalKey)
		require.Zero(t, still.NewModelCalls)

		_, err = Run(context.Background(), client, dir, "R-LARGE", "not-the-key", Hooks{})
		require.ErrorContains(t, err, "approval does not match")
		_, paid, _ = LedgerReceipt(dir, "R-LARGE")
		require.False(t, paid, "错误审批键不得付款")

		var crashed Result
		livecasetest.Result(t, livecasetest.Child(t, testName, env("approve-crash", childKey+"="+waiting.ApprovalKey)...), &crashed)
		require.Equal(t, "crashed-after-pay", crashed.Status)
		receipt, paid, err := LedgerReceipt(dir, "R-LARGE")
		require.NoError(t, err)
		require.True(t, paid, "付款已写入账本，但检查点未保存")

		var resumed Result
		livecasetest.Result(t, livecasetest.Child(t, testName, env("resume")...), &resumed)
		require.Equal(t, "completed", resumed.Status, "已批准的节点恢复时无需再次审批")
		require.Equal(t, receipt, resumed.Receipt, "重试返回原收据")
		require.Zero(t, resumed.NewPayments, "账本按申请单号去重，不再付款")
		require.Zero(t, resumed.NewModelCalls, "评估节点已提交，不重新请求模型")
		require.Equal(t, 1, resumed.Budget.ReservedRequests)

		var replayed Result
		livecasetest.Result(t, livecasetest.Child(t, testName, env("resume")...), &replayed)
		require.Equal(t, resumed, replayed)
	})
}

func TestScriptedApprovalFlows(t *testing.T) {
	if os.Getenv(childDir) != "" {
		childMain("TestScriptedApprovalFlows")
	}
	exerciseFlows(t, scriptedClient(), "TestScriptedApprovalFlows", true)
}

func TestLiveApprovalFlows(t *testing.T) {
	if os.Getenv(childDir) != "" {
		childMain("TestLiveApprovalFlows")
	}
	client := livemodeltest.Require(t)
	t.Logf("模型：%s/%s", client.Live.Provider, client.Live.Model)
	exerciseFlows(t, client, "TestLiveApprovalFlows", false)
}
