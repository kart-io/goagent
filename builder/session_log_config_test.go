package builder

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/core/session"
	"github.com/stretchr/testify/require"
)

func TestSessionLogClearsRemovedSystemPrompt(t *testing.T) {
	ctx := context.Background()
	store := session.NewMemoryStore()
	model := newScriptModel(answer("第一次回答"), answer("第二次回答"))
	for _, system := range []string{"旧系统提示词", ""} {
		agent, err := NewSimpleBuilder(model).WithSystemPrompt(system).WithSessionID("s").
			WithSessionLog(store).WithTools(plainTool()).Build()
		require.NoError(t, err)
		_, err = agent.AsRunnable().Invoke(ctx, "任务")
		require.NoError(t, err)
	}
	require.Equal(t, "user", model.request(1).Messages[0].Role, "第二次实际请求已无 system")
	trajectory, err := session.BuildTrajectory(sessionEvents(t, store, "s"))
	require.NoError(t, err)
	require.Empty(t, trajectory.System, "导出应反映已移除的 system")
}
