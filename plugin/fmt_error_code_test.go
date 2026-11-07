package plugin

import (
	"testing"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
)

func TestFormattedPluginConfigurationErrorCode(t *testing.T) {
	err := validateName("bad.name")
	require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
	require.Equal(t, "plugin", agentErrors.GetComponent(err))
	require.Contains(t, err.Error(), `插件名 "bad.name" 不能包含点号`)
}
