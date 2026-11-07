package execution

import (
	"testing"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
)

func TestFormattedScopeConfigurationErrorCode(t *testing.T) {
	_, err := NewScope(Config{RunID: " run "})
	require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
	require.Equal(t, "execution", agentErrors.GetComponent(err))
	require.Contains(t, err.Error(), "RunID 必须非空且无首尾空白")
}
