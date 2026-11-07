package builder

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
)

func TestFormattedOutputValidationErrorCode(t *testing.T) {
	a := &ConfigurableAgent[any, *core.AgentState]{config: &AgentConfig{}}
	err := a.validateOutput(context.Background(), nil)
	require.Equal(t, agentErrors.CodeInvalidOutput, agentErrors.GetCode(err))
	require.Equal(t, "builder", agentErrors.GetComponent(err))
	require.Contains(t, err.Error(), "最终输出不能为空")
}
