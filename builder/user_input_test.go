package builder

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/testing/mocks"
)

func runUserInput(t *testing.T, store session.Store, model *mocks.ScriptedClient, ctx context.Context, input any) (*AgentOutput, error) {
	t.Helper()
	agent, err := NewSimpleBuilder(model).WithSessionID("s").WithSessionLog(store).WithDurableScope("ui").Build()
	require.NoError(t, err)
	return agent.AsRunnable().Invoke(ctx, input)
}

func turnStarts(t *testing.T, events []session.Event) []session.TurnStart {
	t.Helper()
	var out []session.TurnStart
	for _, ev := range events {
		if ev.Type != constants.SessionEventTurnStart {
			continue
		}
		var d session.TurnStart
		require.NoError(t, json.Unmarshal(ev.Data, &d))
		out = append(out, d)
	}
	return out
}

var testImage = llm.ImagePart{Ref: "image:1", MIME: "image/png", SHA256: "aa"}

func TestUserInputRecordsImageRefsInSessionLog(t *testing.T) {
	store := session.NewMemoryStore()
	model := mocks.NewScriptedClient(mocks.TextReply("好", "好"))
	_, err := runUserInput(t, store, model, context.Background(), UserInput{Text: "看图", Images: []llm.ImagePart{testImage}})
	require.NoError(t, err)

	sent := model.Requests()[0].Messages
	last := sent[len(sent)-1]
	require.Equal(t, "看图", last.Content)
	require.Equal(t, []llm.ImagePart{testImage}, last.Images)
	require.Empty(t, last.Images[0].URL)

	events := sessionEvents(t, store, "s")
	starts := turnStarts(t, events)
	require.Len(t, starts, 1)
	require.Equal(t, "看图", starts[0].Input)
	require.Equal(t, []llm.ImagePart{testImage}, starts[0].Images)

	derived, err := session.DeriveMessages(events)
	require.NoError(t, err)
	require.Equal(t, llm.Message{Role: constants.RoleUser, Content: "看图", Images: []llm.ImagePart{testImage}}, derived[0])
}

func TestUserInputReachesNativeToolLoop(t *testing.T) {
	store := session.NewMemoryStore()
	model := mocks.NewScriptedClient(mocks.TextReply("好", "好"))
	agent, err := NewSimpleBuilder(model).WithSessionID("s").WithSessionLog(store).WithTools(plainTool()).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), UserInput{Text: "看图", Images: []llm.ImagePart{testImage}})
	require.NoError(t, err)
	sent := model.Requests()[0].Messages
	require.Equal(t, []llm.ImagePart{testImage}, sent[len(sent)-1].Images)
	require.Equal(t, []llm.ImagePart{testImage}, turnStarts(t, sessionEvents(t, store, "s"))[0].Images)
}

func TestUserInputPointerAccepted(t *testing.T) {
	store := session.NewMemoryStore()
	model := mocks.NewScriptedClient(mocks.TextReply("好", "好"))
	_, err := runUserInput(t, store, model, context.Background(), &UserInput{Text: "看图", Images: []llm.ImagePart{testImage}})
	require.NoError(t, err)
	sent := model.Requests()[0].Messages
	require.Equal(t, []llm.ImagePart{testImage}, sent[len(sent)-1].Images)
	require.Equal(t, "看图", sent[len(sent)-1].Content)
}

func TestStringInputTurnStartUnchanged(t *testing.T) {
	store := session.NewMemoryStore()
	_, err := runUserInput(t, store, mocks.NewScriptedClient(mocks.TextReply("好", "好")), context.Background(), "你好")
	require.NoError(t, err)
	for _, ev := range sessionEvents(t, store, "s") {
		if ev.Type != constants.SessionEventTurnStart {
			continue
		}
		var raw map[string]any
		require.NoError(t, json.Unmarshal(ev.Data, &raw))
		require.NotContains(t, raw, "images")
	}
}

func TestSameExecutionKeyDifferentImagesIsNotSameTurn(t *testing.T) {
	store := session.NewMemoryStore()
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "run-1"), session.DurableStore(store, "s"))
	_, err := runUserInput(t, store, mocks.NewScriptedClient(mocks.TextReply("好", "好")), ctx, UserInput{Text: "看图", Images: []llm.ImagePart{testImage}})
	require.NoError(t, err)

	// 改 Text 的冲突作为基准：改 Images 必须得到同一错误。
	_, textErr := runUserInput(t, store, mocks.NewScriptedClient(mocks.TextReply("好", "好")), ctx, UserInput{Text: "别的", Images: []llm.ImagePart{testImage}})
	require.Error(t, textErr)
	other := testImage
	other.Ref = "image:2"
	_, imgErr := runUserInput(t, store, mocks.NewScriptedClient(mocks.TextReply("好", "好")), ctx, UserInput{Text: "看图", Images: []llm.ImagePart{other}})
	require.Error(t, imgErr)
	require.Equal(t, textErr.Error(), imgErr.Error())

	// 相同 Text 与 Images 仍是同一轮：不重复写入。
	_, err = runUserInput(t, store, mocks.NewScriptedClient(), ctx, UserInput{Text: "看图", Images: []llm.ImagePart{testImage}})
	require.NoError(t, err)
	require.Len(t, turnStarts(t, sessionEvents(t, store, "s")), 1)
}
