package builder

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/llm"
)

type maskingSerializer struct{ err error }

func (s maskingSerializer) Serialize(interface{}) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return "MASKED", nil
}

// 序列化器必须作用于所有入口：用它脱敏时，原文不能出现在发给模型的消息里。
func TestInputSerializerAppliesToStreamingEntries(t *testing.T) {
	answer := []*llm.StreamChunk{{Delta: "ok"}, {Done: true, FinishReason: "stop"}}
	t.Run("无工具", func(t *testing.T) {
		model := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: answer}
		agent, err := NewSimpleBuilder(model).WithInputSerializer(maskingSerializer{}).Build()
		require.NoError(t, err)
		_, err = agent.AsRunnable().Invoke(context.Background(), "user@example.com")
		require.NoError(t, err)
		messages := model.request.Messages
		require.Equal(t, "MASKED", messages[len(messages)-1].Content)
	})
	t.Run("原生工具流", func(t *testing.T) {
		model := nativeModel(answer)
		agent, err := NewSimpleBuilder(model).WithTools(NewMockTool("lookup", "x")).WithInputSerializer(maskingSerializer{}).Build()
		require.NoError(t, err)
		_, err = agent.AsRunnable().Invoke(context.Background(), "user@example.com")
		require.NoError(t, err)
		messages := model.requests[0].Messages
		require.Equal(t, "MASKED", messages[len(messages)-1].Content)
	})
	t.Run("序列化失败", func(t *testing.T) {
		sentinel := errors.New("serialize failed")
		model := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: answer}
		agent, err := NewSimpleBuilder(model).WithInputSerializer(maskingSerializer{err: sentinel}).Build()
		require.NoError(t, err)
		_, err = agent.AsRunnable().Invoke(context.Background(), "user@example.com")
		require.ErrorIs(t, err, sentinel)
		require.Nil(t, model.request, "序列化失败时不得请求模型")
	})
}
