package management

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTaskTransitionRejectsTerminalResume(t *testing.T) {
	for _, s := range []TaskState{Succeeded, Failed, Canceled} {
		require.False(t, CanTransition(s, Queued))
	}
	require.True(t, CanTransition(Running, WaitingApproval))
}
func TestDefaultsAndLimits(t *testing.T) {
	l := DefaultLimits()
	require.Equal(t, 50, l.PageSize)
	require.Equal(t, 200, l.MaxPageSize)
	require.Equal(t, 1<<20, l.MaxInputBytes)
	require.Equal(t, 4<<20, l.MaxOutputBytes)
}
func TestCrossTenantReferencesRejected(t *testing.T) {
	require.Error(t, ValidateActor(Actor{}))
	require.NoError(t, ValidateActor(Actor{TenantID: "租户", Subject: "操作人"}))
}
