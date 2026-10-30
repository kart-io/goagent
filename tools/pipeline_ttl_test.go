package tools

import (
	"context"
	"github.com/stretchr/testify/require"
	"sync/atomic"
	"testing"
	"time"
)

func TestPipelineGrantTTLExpiresAndPrunes(t *testing.T) {
	var approvals atomic.Int32
	p := &Pipeline{GrantTTL: time.Hour, Pre: []PreHook{askWithGrant}, Approver: ApproverFunc(func(context.Context, ApprovalRequest) (ApprovalResult, error) {
		approvals.Add(1)
		return ApprovalResult{Outcome: ApprovalAllowedSession}, nil
	})}
	call := &Call{Tool: "run", SessionID: "s", Args: map[string]any{"cmd": "go test"}}
	for i := 0; i < 2; i++ {
		out, _, err := p.gate(context.Background(), call, true)
		require.NoError(t, err)
		require.Nil(t, out)
	}
	require.EqualValues(t, 1, approvals.Load())
	// Set an already expired deadline rather than waiting for wall-clock expiry.
	v, _ := p.grants.Load("s")
	set := v.(*grantSet)
	set.mu.Lock()
	for i := range set.grants {
		set.grants[i].expiresAt = time.Now().Add(-time.Second)
	}
	set.mu.Unlock()
	require.Empty(t, p.Grants("s"))
	require.Equal(t, 1, p.PruneGrants())
	_, ok := p.grants.Load("s")
	require.False(t, ok)
	out, _, err := p.gate(context.Background(), call, true)
	require.NoError(t, err)
	require.Nil(t, out)
	require.EqualValues(t, 2, approvals.Load())
}
func TestPipelineExpiredGrantRequiresApprovalBeforePrune(t *testing.T) {
	var approvals atomic.Int32
	p := &Pipeline{GrantTTL: time.Hour, Pre: []PreHook{askWithGrant}, Approver: ApproverFunc(func(context.Context, ApprovalRequest) (ApprovalResult, error) {
		approvals.Add(1)
		return ApprovalResult{Outcome: ApprovalAllowedSession}, nil
	})}
	call := &Call{Tool: "run", SessionID: "s", Args: map[string]any{"cmd": "go test"}}
	_, _, err := p.gate(context.Background(), call, true)
	require.NoError(t, err)
	v, _ := p.grants.Load("s")
	set := v.(*grantSet)
	set.mu.Lock()
	set.grants[0].expiresAt = time.Now().Add(-time.Second)
	set.mu.Unlock()
	out, _, err := p.gate(context.Background(), call, true)
	require.NoError(t, err)
	require.Nil(t, out)
	require.EqualValues(t, 2, approvals.Load())
	require.Equal(t, 1, p.PruneGrants())
	require.Len(t, p.Grants("s"), 1)
}

func TestPipelineZeroTTLAndInflightApprovalSurvivePrune(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var approvals atomic.Int32
	p := &Pipeline{Pre: []PreHook{askWithGrant}, Approver: ApproverFunc(func(context.Context, ApprovalRequest) (ApprovalResult, error) {
		approvals.Add(1)
		close(entered)
		<-release
		return ApprovalResult{Outcome: ApprovalAllowedSession}, nil
	})}
	call := &Call{Tool: "run", SessionID: "s", Args: map[string]any{"cmd": "go test"}}
	done := make(chan error, 1)
	go func() { _, _, err := p.gate(context.Background(), call, true); done <- err }()
	<-entered
	require.Zero(t, p.PruneGrants())
	var pending int
	p.inflight.Range(func(_, _ any) bool { pending++; return true })
	require.Equal(t, 1, pending)
	close(release)
	require.NoError(t, <-done)
	require.Zero(t, p.PruneGrants())
	require.Len(t, p.Grants("s"), 1)
	out, _, err := p.gate(context.Background(), call, true)
	require.NoError(t, err)
	require.Nil(t, out)
	require.EqualValues(t, 1, approvals.Load())
}
