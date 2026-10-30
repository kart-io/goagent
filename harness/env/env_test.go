package env

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newLocal(t *testing.T) *LocalFS {
	t.Helper()
	l, err := NewLocalFS(t.TempDir())
	require.NoError(t, err)
	return l
}

func put(t *testing.T, l *LocalFS, rel, content string) {
	t.Helper()
	p := filepath.Join(l.Root(), filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func get(t *testing.T, l *LocalFS, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(l.Root(), filepath.FromSlash(rel)))
	require.NoError(t, err)
	return string(data)
}

func TestNewLocalFSRejectsInvalidRoot(t *testing.T) {
	_, err := NewLocalFS(filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)
	file := filepath.Join(t.TempDir(), "f")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	_, err = NewLocalFS(file)
	require.Error(t, err)
}

func TestLocalFSConfinesPaths(t *testing.T) {
	l := newLocal(t)
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(l.Root(), "escape")))
	require.NoError(t, os.Symlink(filepath.Join(outside, "nope"), filepath.Join(l.Root(), "dangling")))
	for _, p := range []string{"", "../x", "a/../../x", outside, "escape/secret", "dangling"} {
		_, err := l.Resolve(p)
		require.Error(t, err, p)
	}
	rel, err := l.Resolve("a/b/../c.go")
	require.NoError(t, err)
	require.Equal(t, "a/c.go", rel)
	rel, err = l.Resolve(filepath.Join(l.Root(), "x.go"))
	require.NoError(t, err)
	require.Equal(t, "x.go", rel)
	rel, err = l.Resolve(".")
	require.NoError(t, err)
	require.Equal(t, ".", rel)

	ctx := context.Background()
	_, err = l.WriteFile(ctx, "escape/x", []byte("x"), WriteIntent{})
	require.ErrorIs(t, err, ErrOutsideWorkspace)
	_, _, err = l.ReadFile(ctx, "../x")
	require.ErrorIs(t, err, ErrOutsideWorkspace)
	require.Error(t, l.Remove(ctx, "."))
	require.Error(t, l.Remove(ctx, "../x"))
}

func TestLocalFSWriteIntents(t *testing.T) {
	ctx := context.Background()
	l := newLocal(t)

	v1, err := l.WriteFile(ctx, "d/a.txt", []byte("one"), WriteIntent{CreateIfAbsent: true})
	require.NoError(t, err)
	require.Equal(t, "one", get(t, l, "d/a.txt"))
	_, err = l.WriteFile(ctx, "d/a.txt", []byte("x"), WriteIntent{CreateIfAbsent: true})
	require.ErrorIs(t, err, ErrExists)

	data, v, err := l.ReadFile(ctx, "d/a.txt")
	require.NoError(t, err)
	require.Equal(t, "one", string(data))
	require.Equal(t, v1, v)

	v2, err := l.WriteFile(ctx, "d/a.txt", []byte("two"), WriteIntent{IfVersion: v1})
	require.NoError(t, err)
	_, err = l.WriteFile(ctx, "d/a.txt", []byte("three"), WriteIntent{IfVersion: v1})
	require.ErrorIs(t, err, ErrStale)
	require.Equal(t, "two", get(t, l, "d/a.txt"))
	_, err = l.WriteFile(ctx, "missing.txt", []byte("x"), WriteIntent{IfVersion: v2})
	require.ErrorIs(t, err, ErrStale)

	// 权限：保留原权限，显式 Perm 时改为指定值。
	require.NoError(t, os.Chmod(filepath.Join(l.Root(), "d/a.txt"), 0o600))
	_, err = l.WriteFile(ctx, "d/a.txt", []byte("four"), WriteIntent{})
	require.NoError(t, err)
	info, err := l.Stat(ctx, "d/a.txt")
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0o600), info.Mode.Perm())
	_, err = l.WriteFile(ctx, "d/a.txt", []byte("five"), WriteIntent{Perm: 0o640})
	require.NoError(t, err)
	info, err = l.Stat(ctx, "d/a.txt")
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0o640), info.Mode.Perm())

	_, err = l.WriteFile(ctx, "d", []byte("x"), WriteIntent{})
	require.ErrorContains(t, err, "directory")
}

// 删除符号链接时删除链接本身，不删除链接目标。
func TestLocalFSRemoveLink(t *testing.T) {
	ctx := context.Background()
	l := newLocal(t)
	victim := filepath.Join(t.TempDir(), "victim")
	require.NoError(t, os.WriteFile(victim, []byte("keep"), 0o600))
	require.NoError(t, os.Symlink(victim, filepath.Join(l.Root(), "link")))
	require.NoError(t, l.Remove(ctx, "link"))
	_, err := os.Lstat(filepath.Join(l.Root(), "link"))
	require.True(t, os.IsNotExist(err))
	data, err := os.ReadFile(victim)
	require.NoError(t, err)
	require.Equal(t, "keep", string(data))
}

func TestLocalFSWalk(t *testing.T) {
	ctx := context.Background()
	l := newLocal(t)
	put(t, l, "a/x.go", "")
	put(t, l, "a/skip/y.go", "")
	put(t, l, "b.txt", "")
	var seen []string
	require.NoError(t, l.Walk(ctx, ".", func(i Info) error {
		if i.IsDir() && i.Path == "a/skip" {
			return fs.SkipDir
		}
		seen = append(seen, i.Path)
		return nil
	}))
	require.Equal(t, []string{".", "a", "a/x.go", "b.txt"}, seen)

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, l.Walk(cctx, ".", func(Info) error { return nil }), context.Canceled)
}

func TestObservedRequiresFreshRead(t *testing.T) {
	ctx := context.Background()
	l := newLocal(t)
	o := NewObserved(l)

	_, err := o.WriteFile(ctx, "new.txt", []byte("n"), WriteIntent{})
	require.NoError(t, err)
	// 自己写入的内容视为已读。
	_, err = o.WriteFile(ctx, "new.txt", []byte("n2"), WriteIntent{})
	require.NoError(t, err)

	put(t, l, "old.txt", "v1")
	_, err = o.WriteFile(ctx, "old.txt", []byte("x"), WriteIntent{})
	require.ErrorIs(t, err, ErrNotObserved)

	_, _, err = o.ReadFile(ctx, "old.txt")
	require.NoError(t, err)
	put(t, l, "old.txt", "changed by someone else")
	_, err = o.WriteFile(ctx, "old.txt", []byte("x"), WriteIntent{})
	require.ErrorIs(t, err, ErrStale)
	require.Equal(t, "changed by someone else", get(t, l, "old.txt"))

	// Peek 不记录：读到新内容后仍以旧版本做条件写入。
	_, _, err = o.Peek(ctx, "old.txt")
	require.NoError(t, err)
	_, err = o.WriteFile(ctx, "old.txt", []byte("x"), WriteIntent{})
	require.ErrorIs(t, err, ErrStale)

	_, _, err = o.ReadFile(ctx, "./old.txt")
	require.NoError(t, err)
	_, err = o.WriteFile(ctx, "old.txt", []byte("x"), WriteIntent{})
	require.NoError(t, err)

	// 文件在读取后被删除再由他人创建，按新建写入会失败。
	_, _, err = o.ReadFile(ctx, "gone.txt")
	require.Error(t, err)
	o.Forget("old.txt")
	_, err = o.WriteFile(ctx, "old.txt", []byte("y"), WriteIntent{})
	require.ErrorIs(t, err, ErrNotObserved)

	require.NoError(t, o.Remove(ctx, "new.txt"))
	_, err = o.WriteFile(ctx, "new.txt", []byte("again"), WriteIntent{})
	require.NoError(t, err)
}

func TestCheckpointedRevert(t *testing.T) {
	ctx := context.Background()
	l := newLocal(t)
	c := NewCheckpointed(l)
	put(t, l, "old.txt", "v1")
	require.NoError(t, os.Chmod(filepath.Join(l.Root(), "old.txt"), 0o600))
	put(t, l, "keep.txt", "k1")
	put(t, l, "del.txt", "d1")

	for _, content := range []string{"v2", "v3"} {
		_, err := c.WriteFile(ctx, "old.txt", []byte(content), WriteIntent{})
		require.NoError(t, err)
	}
	_, err := c.WriteFile(ctx, "dir/new.txt", []byte("n"), WriteIntent{})
	require.NoError(t, err)
	_, err = c.WriteFile(ctx, "keep.txt", []byte("k2"), WriteIntent{})
	require.NoError(t, err)
	require.NoError(t, c.Remove(ctx, "del.txt"))
	require.Equal(t, []Change{{Path: "del.txt"}, {Path: "dir/new.txt", Created: true}, {Path: "keep.txt"}, {Path: "old.txt"}}, c.Changes())

	require.NoError(t, os.Chmod(filepath.Join(l.Root(), "old.txt"), 0o644))
	// 重复路径只恢复一次，不会被当作新建文件删除。
	err = c.Revert(ctx, "old.txt", "./old.txt", "dir/new.txt", "del.txt", "missing.txt")
	require.ErrorContains(t, err, "no recorded changes")
	require.Equal(t, "v1", get(t, l, "old.txt"))
	info, err := l.Stat(ctx, "old.txt")
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0o600), info.Mode.Perm())
	require.Equal(t, "d1", get(t, l, "del.txt"))
	_, err = os.Stat(filepath.Join(l.Root(), "dir/new.txt"))
	require.True(t, os.IsNotExist(err))
	require.Equal(t, []Change{{Path: "keep.txt"}}, c.Changes())

	require.NoError(t, c.Revert(ctx))
	require.Equal(t, "k1", get(t, l, "keep.txt"))
	require.Empty(t, c.Changes())
}

// 写入失败不留检查点；文件被换成指向工作区外的链接时拒绝回滚该文件，不写到链接目标上。
func TestCheckpointedEdgeCases(t *testing.T) {
	ctx := context.Background()
	l := newLocal(t)
	c := NewCheckpointed(l)
	if os.Getuid() != 0 {
		ro := filepath.Join(l.Root(), "ro")
		require.NoError(t, os.Mkdir(ro, 0o555))
		t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
		_, err := c.WriteFile(ctx, "ro/x.txt", []byte("x"), WriteIntent{})
		require.Error(t, err)
		require.Empty(t, c.Changes())
	}

	put(t, l, "a.txt", "orig")
	_, err := c.WriteFile(ctx, "a.txt", []byte("new"), WriteIntent{})
	require.NoError(t, err)
	victim := filepath.Join(t.TempDir(), "victim")
	require.NoError(t, os.WriteFile(victim, []byte("secret"), 0o600))
	require.NoError(t, os.Remove(filepath.Join(l.Root(), "a.txt")))
	require.NoError(t, os.Symlink(victim, filepath.Join(l.Root(), "a.txt")))
	require.ErrorIs(t, c.Revert(ctx), ErrOutsideWorkspace)
	data, err := os.ReadFile(victim)
	require.NoError(t, err)
	require.Equal(t, "secret", string(data))
	require.Equal(t, []Change{{Path: "a.txt"}}, c.Changes())
}

func TestObservedOverCheckpointed(t *testing.T) {
	ctx := context.Background()
	l := newLocal(t)
	c := NewCheckpointed(l)
	o := NewObserved(c)
	put(t, l, "a.txt", "v1")
	_, _, err := o.ReadFile(ctx, "a.txt")
	require.NoError(t, err)
	_, err = o.WriteFile(ctx, "a.txt", []byte("v2"), WriteIntent{})
	require.NoError(t, err)
	require.NoError(t, c.Revert(ctx))
	// 回滚后内容变化，旧的观察版本失效。
	_, err = o.WriteFile(ctx, "a.txt", []byte("v3"), WriteIntent{})
	require.ErrorIs(t, err, ErrStale)
	require.Equal(t, "v1", get(t, l, "a.txt"))
}

func TestLocalExecutor(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "sub"), 0o755))
	e := LocalExecutor{Root: root}
	res, err := e.Run(context.Background(), Command{Argv: []string{"sh", "-c", "pwd; exit 3"}, Dir: "sub"})
	require.NoError(t, err)
	require.Equal(t, 3, res.ExitCode)
	require.Equal(t, filepath.Join(root, "sub")+"\n", string(res.Output))

	res, err = e.Run(context.Background(), Command{Argv: []string{"sh", "-c", "sleep 5 & echo started"}})
	require.NoError(t, err, "background child holding the pipe is a normal exit")
	require.Equal(t, "started\n", string(res.Output))

	_, err = e.Run(context.Background(), Command{})
	require.Error(t, err)
	_, err = e.Run(context.Background(), Command{Argv: []string{"definitely-not-a-command-xyz"}})
	require.Error(t, err)
	require.False(t, e.Capabilities().Container)
}

// 设置 GOAGENT_ENV_DOCKER_IMAGE 为含 sh 的本地镜像时运行，如 alpine:3。
func TestDockerExecutor(t *testing.T) {
	image := os.Getenv("GOAGENT_ENV_DOCKER_IMAGE")
	if image == "" {
		t.Skip("GOAGENT_ENV_DOCKER_IMAGE not set")
	}
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "sub/in.txt"), []byte("from host"), 0o644))
	e := DockerExecutor{Root: root, Image: image}
	require.True(t, e.Capabilities().Container)
	require.False(t, e.Capabilities().Network)

	res, err := e.Run(context.Background(), Command{Argv: []string{"sh", "-c", "pwd; echo $HOME; cat in.txt; echo out > out.txt; ls /sys/class/net"}, Dir: "sub"})
	require.NoError(t, err)
	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, "/workspace/sub\n/tmp\nfrom hostlo\n", string(res.Output))
	data, err := os.ReadFile(filepath.Join(root, "sub/out.txt"))
	require.NoError(t, err)
	require.Equal(t, "out\n", string(data))
	info, err := os.Stat(filepath.Join(root, "sub/out.txt"))
	require.NoError(t, err)
	require.Equal(t, uint32(os.Getuid()), fileUID(info))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = e.Run(ctx, Command{Argv: []string{"sleep", "60"}})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	left, err := exec.Command("docker", "ps", "-aq", "--filter", "name=goagent-env-", "--filter", "volume="+root).Output()
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(string(left)), "timed out container must be removed")

	ro := DockerExecutor{Root: root, Image: image, ReadOnly: true}
	require.True(t, ro.Capabilities().ReadOnlyRoot)
	res, err = ro.Run(context.Background(), Command{Argv: []string{"sh", "-c", "cat in.txt; echo x > in.txt"}, Dir: "sub"})
	require.NoError(t, err)
	require.NotZero(t, res.ExitCode)
	require.Contains(t, string(res.Output), "from host")
	data, err = os.ReadFile(filepath.Join(root, "sub/in.txt"))
	require.NoError(t, err)
	require.Equal(t, "from host", string(data), "read-only mount must not be writable")
}

func TestReadOnly(t *testing.T) {
	ctx := context.Background()
	l := newLocal(t)
	put(t, l, "a.txt", "keep")
	ro := ReadOnly(l)
	data, _, err := ro.ReadFile(ctx, "a.txt")
	require.NoError(t, err)
	require.Equal(t, "keep", string(data))
	_, err = ro.WriteFile(ctx, "a.txt", []byte("x"), WriteIntent{})
	require.ErrorIs(t, err, ErrReadOnly)
	require.ErrorIs(t, ro.Remove(ctx, "a.txt"), ErrReadOnly)
	_, err = NewObserved(NewCheckpointed(ro)).WriteFile(ctx, "new.txt", []byte("x"), WriteIntent{})
	require.ErrorIs(t, err, ErrReadOnly)
	require.Equal(t, "keep", get(t, l, "a.txt"))
}

// 包装器删除的也是链接本身，不是工作区内的链接目标。
func TestWrappersRemoveLink(t *testing.T) {
	ctx := context.Background()
	for name, wrap := range map[string]func(FileSystem) FileSystem{
		"observed":     func(f FileSystem) FileSystem { return NewObserved(f) },
		"checkpointed": func(f FileSystem) FileSystem { return NewCheckpointed(f) },
	} {
		l := newLocal(t)
		put(t, l, "target.txt", "keep")
		require.NoError(t, os.Symlink(filepath.Join(l.Root(), "target.txt"), filepath.Join(l.Root(), "link")))
		require.NoError(t, wrap(l).Remove(ctx, "link"), name)
		_, err := os.Lstat(filepath.Join(l.Root(), "link"))
		require.True(t, os.IsNotExist(err), name)
		require.Equal(t, "keep", get(t, l, "target.txt"), name)
	}
}

// 新建后又删除的文件已复原，不再列入改动；原有文件删除后仍可回滚。
func TestCheckpointedRemoveCreated(t *testing.T) {
	ctx := context.Background()
	l := newLocal(t)
	put(t, l, "old.txt", "old")
	c := NewCheckpointed(l)
	_, err := c.WriteFile(ctx, "new.txt", []byte("x"), WriteIntent{})
	require.NoError(t, err)
	require.NoError(t, c.Remove(ctx, "new.txt"))
	require.NoError(t, c.Remove(ctx, "old.txt"))
	require.Equal(t, []Change{{Path: "old.txt"}}, c.Changes())
	require.NoError(t, c.Revert(ctx))
	require.Equal(t, "old", get(t, l, "old.txt"))
}
