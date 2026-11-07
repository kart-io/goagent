package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
)

func TestFormattedGraphConfigurationErrorCode(t *testing.T) {
	g := NewGraph[int]()
	err := g.AddNode("bad", nil)
	require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
	require.Equal(t, "workflow", agentErrors.GetComponent(err))
	require.Contains(t, err.Error(), `node "bad" has no function`)
}
