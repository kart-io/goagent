package session

import (
	"testing"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
)

func TestInvalidSessionBatchIsClassified(t *testing.T) {
	err := ValidateBatches([]AppendBatch{{ID: ""}})
	require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err))
	require.Equal(t, "session", agentErrors.GetComponent(err))
}
