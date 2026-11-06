package durable

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"

	"github.com/kart-io/goagent/core/internal/txnlog"
	"github.com/stretchr/testify/require"
)

const saveAllCrashExit = 88

func writesFor(version string) []Write {
	data := json.RawMessage(`{}`)
	return []Write{
		{ID: "x", Snapshot: &Snapshot{Version: version, RunID: "x", Data: data}},
		{ID: "y", Snapshot: &Snapshot{Version: version, RunID: "y", Data: data}},
	}
}

func crashSaveAll(t *testing.T, name string, point func(*func())) string {
	t.Helper()
	const env = "GOAGENT_SAVEALL_CRASH_DIR"
	if dir := os.Getenv(env); dir != "" {
		store, err := NewFileStore(dir)
		require.NoError(t, err)
		ctx := context.Background()
		require.NoError(t, store.SaveAll(ctx, writesFor("old")...))
		hook := func() { os.Exit(saveAllCrashExit) }
		point(&hook)
		_ = store.SaveAll(ctx, writesFor("new")...)
		t.Fatal("期望进程在写入中退出")
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^"+name+"$")
	cmd.Env = append(os.Environ(), env+"="+dir)
	output, err := cmd.CombinedOutput()
	var exited *exec.ExitError
	require.ErrorAs(t, err, &exited, string(output))
	require.Equal(t, saveAllCrashExit, exited.ExitCode(), string(output))
	return dir
}

func requireVersions(t *testing.T, store Store, version string) {
	t.Helper()
	for _, id := range []string{"x", "y"} {
		snapshot, err := store.Load(context.Background(), id)
		require.NoError(t, err)
		require.Equal(t, version, snapshot.Version, id)
	}
}

// 提交点之前退出：两个 ID 都保持旧值。
func TestFileStoreSaveAllCrashBeforeCommit(t *testing.T) {
	dir := crashSaveAll(t, "TestFileStoreSaveAllCrashBeforeCommit", func(hook *func()) { txnlog.BeforeCommit = *hook })
	store, err := NewFileStore(dir)
	require.NoError(t, err)
	requireVersions(t, store, "old")
}

// 提交点之后、应用之前退出：重启后两个 ID 都是新值，之后的写入不被迟到的前滚覆盖。
func TestFileStoreSaveAllCrashAfterCommit(t *testing.T) {
	dir := crashSaveAll(t, "TestFileStoreSaveAllCrashAfterCommit", func(hook *func()) { txnlog.AfterCommit = *hook })
	raw, err := os.ReadFile(mustNewFileStore(t, dir).path("x"))
	require.NoError(t, err)
	require.Contains(t, string(raw), `"version":"old"`, "退出时尚未应用")

	store := mustNewFileStore(t, dir)
	requireVersions(t, store, "new")
	entries, err := os.ReadDir(filepath.Join(dir, ".txn"))
	require.NoError(t, err)
	require.Empty(t, entries, "前滚后不留下提交文件")

	require.NoError(t, store.Save(context.Background(), "x", &Snapshot{Version: "later", RunID: "x", Data: json.RawMessage(`{}`)}))
	snapshot, err := store.Load(context.Background(), "x")
	require.NoError(t, err)
	require.Equal(t, "later", snapshot.Version)
}

// 崩溃后第一个操作是单独写入：先前滚，单独写入的值不被覆盖。
func TestFileStoreSaveAfterCrashKeepsNewerWrite(t *testing.T) {
	dir := crashSaveAll(t, "TestFileStoreSaveAfterCrashKeepsNewerWrite", func(hook *func()) { txnlog.AfterCommit = *hook })
	store := mustNewFileStore(t, dir)
	require.NoError(t, store.Save(context.Background(), "x", &Snapshot{Version: "later", RunID: "x", Data: json.RawMessage(`{}`)}))
	snapshot, err := store.Load(context.Background(), "x")
	require.NoError(t, err)
	require.Equal(t, "later", snapshot.Version)
	snapshot, err = store.Load(context.Background(), "y")
	require.NoError(t, err)
	require.Equal(t, "new", snapshot.Version)
}

func TestFileStoreSaveAllHonoursCancellation(t *testing.T) {
	store := mustNewFileStore(t, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	require.ErrorIs(t, store.SaveAll(ctx, writesFor("new")...), context.DeadlineExceeded)
	_, err := store.Load(context.Background(), "x")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func mustNewFileStore(t *testing.T, dir string) *FileStore {
	t.Helper()
	store, err := NewFileStore(dir)
	require.NoError(t, err)
	return store
}
