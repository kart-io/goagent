package main

import (
	"github.com/kart-io/goagent/testing/panictest"
	"github.com/stretchr/testify/require"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestReferencesProcessRecovery(t *testing.T) {
	if dir := os.Getenv("GOAGENT_REFERENCE_CHILD"); dir != "" {
		out, err := run(dir)
		if err != nil {
			os.Exit(2)
		}
		_, _ = os.Stdout.WriteString(out)
		os.Exit(86)
	}
	dir := t.TempDir()
	child := func() string {
		cmd := exec.Command(os.Args[0], "-test.run=^TestReferencesProcessRecovery$")
		cmd.Env = append(os.Environ(), "GOAGENT_REFERENCE_CHILD="+dir)
		b, err := panictest.CombinedOutput(cmd, panictest.ChildTimeout(t, 2*time.Minute))
		var exit *exec.ExitError
		require.ErrorAs(t, err, &exit)
		require.Equal(t, 86, exit.ExitCode(), string(b))
		return string(b)
	}
	first := child()
	require.Equal(t, first, child())
	require.Contains(t, first, "丙丁")
}
