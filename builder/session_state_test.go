package builder

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
)

// counterState 是宿主自定义的状态：编码格式由它自己决定，恢复后 count 仍是 int。
type counterState struct{ *core.AgentState }

func newCounterState() *counterState { return &counterState{core.NewAgentState()} }

func (s *counterState) MarshalJSON() ([]byte, error) {
	n, _ := s.GetInt("count")
	return json.Marshal(struct {
		Count int `json:"count"`
	}{n})
}

func (s *counterState) UnmarshalJSON(data []byte) error {
	var v struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	s.AgentState = core.NewAgentState()
	s.Set("count", v.Count)
	return nil
}

// plainState 实现 core.State 但没有 JSON 编解码。
type plainState struct{ core.State }

func replies(answers ...string) *mocks.ScriptedClient {
	rules := make([]mocks.ScriptedRule, len(answers))
	for i, a := range answers {
		rules[i] = mocks.TextReply(a, a)
	}
	return mocks.NewScriptedClient(rules...)
}

func newCounterAgent(t *testing.T, store session.Store, answers ...string) *ConfigurableAgent[*counterState] {
	t.Helper()
	agent, err := NewAgentBuilder[*counterState](replies(answers...)).WithState(newCounterState()).
		WithSessionID("a").WithSessionLog(store).WithSessionState().Build()
	require.NoError(t, err)
	return agent
}

func runTurn(t *testing.T, agent *ConfigurableAgent[*counterState], session string) {
	t.Helper()
	_, err := agent.AsRunnable().Invoke(tools.WithSessionID(context.Background(), session), "hi")
	require.NoError(t, err)
}

func count(agent *ConfigurableAgent[*counterState]) int {
	n, _ := agent.GetState().GetInt("count")
	return n
}

// 新进程中的 Agent 从会话日志恢复自定义状态，值保持宿主定义的类型。
func TestSessionStateRestoresAcrossProcesses(t *testing.T) {
	store := session.NewMemoryStore()
	first := newCounterAgent(t, store, "一")
	first.GetState().Set("count", 3)
	runTurn(t, first, "a")

	second := newCounterAgent(t, store, "二")
	runTurn(t, second, "a")
	require.Equal(t, 3, count(second))
}

// 同一 Agent 服务多个会话时状态按会话隔离，没有记录的会话从构建时的状态开始。
func TestSessionStateFollowsSession(t *testing.T) {
	store := session.NewMemoryStore()
	agent := newCounterAgent(t, store, "1", "2", "3")
	agent.GetState().Set("count", 5)
	runTurn(t, agent, "a")

	runTurn(t, agent, "b")
	require.Equal(t, 0, count(agent), "会话 b 不应继承会话 a 的状态")
	agent.GetState().Set("count", 7)

	runTurn(t, agent, "a")
	require.Equal(t, 5, count(agent))
}

// 退回会话后状态随之退回到被保留的最后一轮。
func TestSessionStateFollowsRewind(t *testing.T) {
	ctx := context.Background()
	store := session.NewMemoryStore()
	agent := newCounterAgent(t, store, "1", "2", "3")
	agent.GetState().Set("count", 1)
	runTurn(t, agent, "a")
	kept := session.LastSeq(sessionEvents(t, store, "a"))
	agent.GetState().Set("count", 2)
	runTurn(t, agent, "a")

	_, err := session.Rewind(ctx, store, "a", kept, session.LastSeq(sessionEvents(t, store, "a")))
	require.NoError(t, err)
	runTurn(t, agent, "a")
	require.Equal(t, 1, count(agent))
}

func TestSessionStateRequiresLogAndCodec(t *testing.T) {
	model := replies()
	tests := []struct {
		name  string
		build func() error
	}{
		{"缺少会话日志", func() error {
			_, err := NewSimpleBuilder(model).WithSessionState().Build()
			return err
		}},
		{"状态没有 JSON 编解码", func() error {
			_, err := NewAgentBuilder[plainState](model).WithState(plainState{core.NewAgentState()}).
				WithSessionLog(session.NewMemoryStore()).WithSessionState().Build()
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorContains(t, tt.build(), "WithSessionState")
		})
	}
}

// 默认的 *core.AgentState 也能恢复，值为 JSON 通用类型。
func TestSessionStateAgentStateDefault(t *testing.T) {
	store := session.NewMemoryStore()
	build := func(text string) *SimpleAgent {
		agent, err := NewSimpleBuilder(replies(text)).WithSessionID("s").
			WithSessionLog(store).WithSessionState().Build()
		require.NoError(t, err)
		return agent
	}
	first := build("答一")
	first.GetState().Set("user", map[string]any{"lang": "zh"})
	_, err := first.AsRunnable().Invoke(context.Background(), "hi")
	require.NoError(t, err)

	second := build("答二")
	_, err = second.AsRunnable().Invoke(context.Background(), "hi")
	require.NoError(t, err)
	user, ok := second.GetState().GetMap("user")
	require.True(t, ok)
	require.Equal(t, "zh", user["lang"])
}
