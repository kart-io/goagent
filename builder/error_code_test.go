package builder

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
)

// 宿主可按错误码修正启动或日志配置，无需从句柄错误消息猜测缺失项。
func TestBuilderConfigurationErrorsExposeCodeAndContext(t *testing.T) {
	a := &ConfigurableAgent[*core.AgentState]{config: &AgentConfig{}}
	_, err := a.Start(context.Background(), "run").Wait(context.Background())
	require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
	require.Equal(t, "builder", agentErrors.GetComponent(err))
	require.Equal(t, "native_tool_stream", agentErrors.GetContext(err)["config_key"])
	_, _, err = a.openSessionTurn(context.Background(), "run")
	require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
	require.Equal(t, "builder", agentErrors.GetComponent(err))
	require.Equal(t, "session_id", agentErrors.GetContext(err)["config_key"])
}
