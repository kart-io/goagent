package httpapi_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/management/internal/managementtest"
)

func TestSubmitTaskIgnoresClientBudget(t *testing.T) {
	host := mg.TaskBudget{MaxModelRequests: 7, MaxToolCalls: 9, MaxAgentCalls: 11, Timeout: time.Hour}
	for _, tc := range []struct{ name, budget string }{
		{"空预算不能解除宿主上限", `{}`},
		{"自带上限不能覆盖宿主上限", `{"max_model_requests":100000,"max_tool_calls":100000,"max_agent_calls":100000}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := managementtest.NewFixture(t)
			se := f.ReadySession(t)
			cfg := f.Config()
			cfg.DefaultTaskBudget = host
			var err error
			f.Service, err = mg.New(cfg)
			require.NoError(t, err)
			body := fmt.Sprintf(`{"session_id":%q,"input":{},"budget":%s}`, se.ID, tc.budget)
			task := offlineDecode[mg.Task](t, offlineRequest(t, offlineHandler(t, f), "POST", "/v1/tasks", body, "预算", 202))
			stored, err := f.Service.GetTask(context.Background(), f.Actor, task.ID)
			require.NoError(t, err)
			require.Equal(t, host.MaxModelRequests, stored.Budget.MaxModelRequests)
			require.Equal(t, host.MaxToolCalls, stored.Budget.MaxToolCalls)
			require.Equal(t, host.MaxAgentCalls, stored.Budget.MaxAgentCalls)
			require.NotZero(t, stored.Budget.DeadlineUnixMilli)
		})
	}
}
