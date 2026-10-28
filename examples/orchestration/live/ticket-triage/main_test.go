package main

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/examples/internal/livemodel/livemodeltest"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

// 不调用模型：规则图工具按订单返回窗口判断，未知订单报错且不计数。
func TestOrderLookupTool(t *testing.T) {
	var runs atomic.Int64
	tool, err := orderLookupTool(&runs)
	require.NoError(t, err)
	for id, want := range map[string]bool{"A1001": true, "A1002": false, "A1003": true} {
		out, err := tool.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{"order_id": id}})
		require.NoError(t, err)
		require.Equal(t, want, out.Result.(order).WithinWindow, id)
	}
	_, err = tool.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{"order_id": "Z9"}})
	require.ErrorContains(t, err, "不存在")
	require.Equal(t, int64(3), runs.Load())
}

func TestLiveTicketTriage(t *testing.T) {
	client := livemodeltest.Require(t)
	for id, ticket := range Tickets {
		t.Run(id, func(t *testing.T) {
			dir := t.TempDir()
			first, err := Run(context.Background(), client, dir, id)
			t.Logf("%s/%s 首次：%+v", client.Live.Provider, client.Live.Model, first)
			require.NoError(t, err)
			require.Equal(t, ticket.Expected, first.Decision, first.Answer)
			require.Equal(t, int64(1), first.LookupRuns, "规则图只执行一次")
			require.GreaterOrEqual(t, first.ChildCreated, int64(1), "必须咨询政策子 Agent")
			require.Equal(t, first.ChildCreated, first.ChildClose, "每个子会话都要清理")
			require.Equal(t, int(first.NewModelCalls), first.Budget.ReservedRequests, "父子请求都经过根预算")
			require.LessOrEqual(t, first.Budget.ReservedRequests, 8)
			require.Positive(t, first.Budget.KnownTokens.TotalTokens)

			again, err := Run(context.Background(), client, dir, id)
			require.NoError(t, err)
			require.Zero(t, again.NewModelCalls, "回放不发新请求")
			require.Zero(t, again.ChildCreated, "已完成的子工具不再创建子会话")
			require.Zero(t, again.LookupRuns)
			require.Equal(t, first.Answer, again.Answer)
			require.Equal(t, first.Budget, again.Budget, "回放不重复计量")
		})
	}
}
