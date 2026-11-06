package tools

import (
	"context"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func TestDispatchGovernanceReturnsIndependentValues(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(NewBaseTool("lookup", "合成查询", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true}, nil
	})))
	d, err := NewDispatcher(reg, DispatchConfig{Authorize: func(context.Context, string, *interfaces.ToolInput) error { return nil }, MaxAdmissionConcurrency: 2, MaxQueued: 4})
	require.NoError(t, err)
	report := d.Governance()
	require.Equal(t, []string{"lookup"}, report.ToolNames)
	require.True(t, report.AuthorizationConfigured)
	require.Equal(t, 30*time.Second, report.Timeout)
	report.ToolNames[0] = "被修改"
	require.Equal(t, []string{"lookup"}, d.Governance().ToolNames)
	var absent *Dispatcher
	require.Empty(t, absent.Governance())
}
