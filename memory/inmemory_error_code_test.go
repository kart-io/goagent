package memory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// 参数错误和缺失记录必须能按错误码与字段区分，调用方无需分析消息文本。
func TestInMemoryErrorsExposeCodeAndContext(t *testing.T) {
	m := NewInMemoryManager(nil)
	ctx := context.Background()
	for _, tc := range []struct {
		name, parameter string
		run             func() error
	}{
		{"空对话", "conversation", func() error { return m.AddConversation(ctx, nil) }},
		{"空会话", "session_id", func() error { return m.AddConversation(ctx, &interfaces.Conversation{}) }},
		{"空键", "key", func() error { return m.Store(ctx, "", "value") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err))
			require.Equal(t, "memory", agentErrors.GetComponent(err))
			require.Equal(t, tc.parameter, agentErrors.GetContext(err)["parameter"])
		})
	}
	_, err := m.Retrieve(ctx, "missing")
	require.Equal(t, agentErrors.CodeNotFound, agentErrors.GetCode(err))
	require.Equal(t, "retrieve", agentErrors.GetOperation(err))
	require.Equal(t, "missing", agentErrors.GetContext(err)["key"])
	require.NoError(t, m.Store(ctx, "present", "value"))
	value, err := m.Retrieve(ctx, "present")
	require.NoError(t, err)
	require.Equal(t, "value", value)
}
