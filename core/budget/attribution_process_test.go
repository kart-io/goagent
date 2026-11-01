package budget

import (
	"context"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/testing/panictest"
	"github.com/stretchr/testify/require"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestAttributionProcessRecovery(t *testing.T) {
	cfg := Config{RootKey: "r", Version: "v1", MaxModelRequests: 2, MaxAgentCalls: 2}
	chain := []usage.Owner{{RunID: "r", AgentID: "root", AgentCallKey: "r"}}
	ctx := context.Background()
	if dir := os.Getenv("GOAGENT_ATTRIBUTION_CHILD"); dir != "" {
		s, e := durable.NewFileStore(dir)
		if e != nil {
			panic(e)
		}
		c, e := NewDurable(cfg, s)
		if e != nil {
			panic(e)
		}
		if e = c.ReserveAttributed(ctx, "a", 0, chain); e != nil {
			panic(e)
		}
		if os.Getenv("GOAGENT_ATTRIBUTION_COMMIT") == "1" {
			_, e = c.RecordEvidence(ctx, "a", chain, usage.Evidence{KnownFields: usage.Total, Tokens: interfaces.TokenUsage{TotalTokens: 5}, Source: "provider"})
			if e != nil {
				panic(e)
			}
		}
		os.Exit(86)
	}
	for _, commit := range []string{"0", "1"} {
		t.Run(commit, func(t *testing.T) {
			dir := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run=^TestAttributionProcessRecovery$")
			cmd.Env = append(os.Environ(), "GOAGENT_ATTRIBUTION_CHILD="+dir, "GOAGENT_ATTRIBUTION_COMMIT="+commit)
			output, e := panictest.CombinedOutput(cmd, panictest.ChildTimeout(t, time.Minute))
			var exited *exec.ExitError
			require.ErrorAs(t, e, &exited, string(output))
			require.Equal(t, 86, exited.ExitCode())
			s, e := durable.NewFileStore(dir)
			require.NoError(t, e)
			c, e := NewDurable(cfg, s)
			require.NoError(t, e)
			r, e := c.Attribution(ctx)
			require.NoError(t, e)
			require.Equal(t, 1, r.Root.ReservedRequests)
			require.Len(t, r.Calls, 1)
			if commit == "0" {
				require.Equal(t, 1, r.UnknownUsageEntries)
			} else {
				require.Equal(t, 5, r.Root.KnownTokens.TotalTokens)
			}
			_, e = c.RecordEvidence(ctx, "a", chain, usage.Evidence{KnownFields: usage.Total, Tokens: interfaces.TokenUsage{TotalTokens: 5}, Source: "provider"})
			require.NoError(t, e)
			r, e = c.Attribution(ctx)
			require.NoError(t, e)
			require.Equal(t, 5, r.Root.KnownTokens.TotalTokens)
			require.ErrorIs(t, c.ReserveAttributed(ctx, "a", 0, chain), ErrReservationExists)
		})
	}
}
