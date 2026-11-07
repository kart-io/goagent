package durable

import (
	"errors"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDurableWaitingCodeKeepsDiagnosticCausePrivate(t *testing.T) {
	cause := errors.New("诊断原因")
	wait := &WaitError{Path: []WaitStep{{ID: "approval", Key: "child"}}, Cause: cause}
	require.Len(t, Waits(wait), 1)
	require.False(t, errors.Is(wait, cause))
	require.Equal(t, agentErrors.CodeWaiting, agentErrors.GetCode(wait))
}
