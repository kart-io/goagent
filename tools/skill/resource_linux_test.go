//go:build linux

package skill_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/skill"
)

func TestDescriptorExhaustionIsAnExecutionFailure(t *testing.T) {
	if os.Getenv("GOAGENT_SKILL_EMFILE_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestDescriptorExhaustionIsAnExecutionFailure$", "-test.timeout=20s")
		cmd.Env = append(os.Environ(), "GOAGENT_SKILL_EMFILE_CHILD=1")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return
	}
	catalog, _ := fixture(t)
	tool := skill.New(catalog)
	var original syscall.Rlimit
	require.NoError(t, syscall.Getrlimit(syscall.RLIMIT_NOFILE, &original))
	limit := original
	limit.Cur = min(limit.Cur, 64)
	require.NoError(t, syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limit))
	var opened []*os.File
	defer func() {
		for _, f := range opened {
			_ = f.Close()
		}
		_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &original)
	}()
	for range 64 {
		f, err := os.Open(os.DevNull)
		if err != nil {
			require.ErrorIs(t, err, syscall.EMFILE)
			break
		}
		opened = append(opened, f)
	}
	_, err := catalog.ReadReference(context.Background(), "example", "references/guide.md")
	require.True(t, errors.Is(err, syscall.EMFILE), "probe must actually reach descriptor exhaustion")
	for _, file := range []string{"", "references/guide.md"} {
		out, err := tool.Invoke(context.Background(), input(map[string]any{"name": "example", "file": file}))
		require.ErrorIs(t, err, syscall.EMFILE)
		_, rejected := tools.RejectedResult(out)
		require.False(t, rejected)
	}
	for _, f := range opened {
		require.NoError(t, f.Close())
	}
	opened = nil
	require.NoError(t, syscall.Setrlimit(syscall.RLIMIT_NOFILE, &original))
	out, err := tool.Invoke(context.Background(), input(map[string]any{"name": "example", "file": "references/guide.md"}))
	require.NoError(t, err)
	require.True(t, out.Success, "unchanged resource is readable after recovery")
}
