//go:build unix

package env

import (
	"context"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func requireProcessGone(t *testing.T, output string) {
	t.Helper()
	pid, err := strconv.Atoi(strings.TrimSpace(strings.Split(output, "\n")[0]))
	require.NoError(t, err, output)
	require.Eventually(t, func() bool {
		return syscall.Kill(pid, 0) == syscall.ESRCH
	}, 2*time.Second, 20*time.Millisecond, "background process %d still alive", pid)
}

// 命令退出或超时后，它放到后台的子进程也被结束。
func TestLocalExecutorKillsProcessGroup(t *testing.T) {
	e := LocalExecutor{Root: t.TempDir()}
	res, err := e.Run(context.Background(), Command{Argv: []string{"sh", "-c", "sleep 30 & echo $!"}})
	require.NoError(t, err)
	requireProcessGone(t, string(res.Output))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	res, err = e.Run(ctx, Command{Argv: []string{"sh", "-c", "sleep 30 & echo $!; sleep 30"}})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	requireProcessGone(t, string(res.Output))
}
