//go:build !windows

package shell

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/interfaces"
)

func TestShellOutputAntiAbuseCap(t *testing.T) {
	run := func(b *ShellToolBuilder) (int, error) {
		out, err := b.WithAllowedCommands("head").WithTimeout(10*time.Second).Build().Invoke(context.Background(),
			&interfaces.ToolInput{Args: map[string]any{"command": "head", "args": []any{"-c", "2000000", "/dev/zero"}}})
		if err != nil {
			return 0, err
		}
		return len(out.Result.(map[string]any)["output"].(string)), nil
	}
	n, err := run(NewShellToolBuilder())
	require.NoError(t, err)
	require.Equal(t, constants.DefaultShellMaxOutputBytes, n, "0 仍是默认 1MiB")
	n, err = run(NewShellToolBuilder().WithMaxOutputBytes(UnlimitedOutput))
	require.NoError(t, err)
	require.Equal(t, 2000000, n)
	n, err = run(NewShellToolBuilder().WithMaxOutputBytes(10))
	require.NoError(t, err)
	require.Equal(t, 10, n)
	_, err = run(NewShellToolBuilder().WithMaxOutputBytes(-2))
	require.Error(t, err)
}
