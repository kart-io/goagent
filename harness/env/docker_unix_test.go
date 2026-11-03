//go:build unix

package env

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func fileUID(info os.FileInfo) uint32 {
	return info.Sys().(*syscall.Stat_t).Uid
}

// fakeDocker 模拟守护进程：创建在后台完成，客户端被杀也照样建出容器，这正是 docker run 被中断时泄漏容器的原因。
// 状态目录里一个文件代表一个容器，calls 记录每次调用的子命令。
func fakeDocker(t *testing.T, createDelay string) (docker, state string) {
	t.Helper()
	dir := t.TempDir()
	state = filepath.Join(dir, "containers")
	require.NoError(t, os.Mkdir(state, 0o755))
	script := `#!/bin/bash
state=` + state + `
echo "$1" >> ` + dir + `/calls
name=""; prev=""
for a in "$@"; do [ "$prev" = --name ] && name=$a; prev=$a; done
case "$1" in
run|create)
  [ -n "$FAKE_IMAGE_MISSING" ] && [ ! -e ` + dir + `/pulled ] && { echo "Error response from daemon: No such image: x" >&2; exit 1; }
  setsid bash -c "sleep ` + createDelay + `; touch $state/$name" </dev/null >/dev/null 2>&1 &
  while [ ! -e "$state/$name" ]; do sleep 0.02; done
  [ "$1" = create ] && { echo id-$name; exit 0; }
  sleep 30 ;;
start)
  name=$3
  [ -e "$state/$name" ] || { echo "Error response from daemon: No such container: $name" >&2; exit 1; }
  case "$FAKE_START" in
  missing) echo 'Error response from daemon: failed to create task for container: exec: "x": executable file not found in $PATH' >&2; rm -f "$state/$name"; exit 1 ;;
  denied) echo 'Error response from daemon: failed to create task for container: exec: "/etc/passwd": permission denied' >&2; rm -f "$state/$name"; exit 1 ;;
  exit7) echo out; rm -f "$state/$name"; exit 7 ;;
  *) sleep 30 ;;
  esac ;;
rm) rm -f "$state/$3" ;;
image) exit 1 ;;
pull) touch ` + dir + `/pulled ;;
esac
`
	docker = filepath.Join(dir, "docker")
	require.NoError(t, os.WriteFile(docker, []byte(script), 0o755))
	return docker, state
}

func remaining(t *testing.T, state string) []string {
	t.Helper()
	entries, err := os.ReadDir(state)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// 超时落在创建期间或运行期间，都不能留下容器。
func TestDockerExecutorTimeoutLeavesNoContainer(t *testing.T) {
	for name, tc := range map[string]struct {
		createDelay string
		timeout     time.Duration
	}{
		"创建期间": {"0.6", 100 * time.Millisecond},
		"运行期间": {"0.05", 400 * time.Millisecond},
	} {
		t.Run(name, func(t *testing.T) {
			docker, state := fakeDocker(t, tc.createDelay)
			e := DockerExecutor{Root: t.TempDir(), Image: "fake", Docker: docker}
			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()
			_, err := e.Run(ctx, Command{Argv: []string{"sleep", "60"}})
			require.ErrorIs(t, err, context.DeadlineExceeded)
			time.Sleep(time.Second)
			require.Empty(t, remaining(t, state), "超时后容器必须删除")
		})
	}
}

// 容器没能启动时，退出码与 docker run 一致：找不到命令 127，不可执行 126。
func TestDockerExecutorStartFailureExitCode(t *testing.T) {
	for mode, want := range map[string]int{"missing": 127, "denied": 126, "exit7": 7} {
		t.Run(mode, func(t *testing.T) {
			docker, state := fakeDocker(t, "0")
			t.Setenv("FAKE_START", mode)
			e := DockerExecutor{Root: t.TempDir(), Image: "fake", Docker: docker}
			res, err := e.Run(context.Background(), Command{Argv: []string{"x"}})
			require.NoError(t, err)
			require.Equal(t, want, res.ExitCode, string(res.Output))
			require.Empty(t, remaining(t, state))
		})
	}
}

// 镜像不在本地时先拉取再创建；拉取随 ctx 取消，不会留下容器。
func TestDockerExecutorPullsMissingImage(t *testing.T) {
	docker, state := fakeDocker(t, "0")
	t.Setenv("FAKE_IMAGE_MISSING", "1")
	t.Setenv("FAKE_START", "exit7")
	e := DockerExecutor{Root: t.TempDir(), Image: "fake", Docker: docker}
	res, err := e.Run(context.Background(), Command{Argv: []string{"x"}})
	require.NoError(t, err)
	require.Equal(t, 7, res.ExitCode, string(res.Output))
	calls, err := os.ReadFile(filepath.Join(filepath.Dir(docker), "calls"))
	require.NoError(t, err)
	require.Equal(t, "create\nimage\npull\ncreate\nstart\nrm\n", string(calls))
	require.Empty(t, remaining(t, state))
}
