package builder

import (
	"errors"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAgentWaitingCodeDoesNotChangeWaitLeaves(t *testing.T) {
	wait := &WaitingError{Waits: []*durable.WaitError{{Path: []durable.WaitStep{{ID: "approval", Key: "child"}}}}}
	require.ErrorIs(t, wait, agentErrors.ErrAgentWaiting)
	require.Len(t, durable.Waits(wait), 1)
	require.Nil(t, durable.Waits(errors.Join(wait, errors.New("执行失败"))))
	require.Equal(t, agentErrors.CodeWaiting, agentErrors.GetCode(wait))
	require.Equal(t, agentErrors.CodeWaiting, agentErrors.GetCode(agentErrors.ErrorWithRetry(wait, 1, 2)))
}
