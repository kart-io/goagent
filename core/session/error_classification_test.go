package session

import (
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestInvalidSessionBatchIsClassified(t *testing.T) {
	err := ValidateBatches([]AppendBatch{{ID: ""}})
	require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err))
	require.Equal(t, "session", agentErrors.GetComponent(err))
}
