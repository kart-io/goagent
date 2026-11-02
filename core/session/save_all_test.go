package session

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/internal/txnlog"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

const appendAllCrashExit = 89

func userEvent(t *testing.T, text string) Event {
	t.Helper()
	ev, err := NewEvent(TypeUserMessage, MessageData{Message: llm.UserMessage(text)})
	require.NoError(t, err)
	return ev
}

// 提交点之后退出：重启后两个会话都有新事件，任一 Read 都不会看到只写了一半的结果。
func TestFileStoreAppendAllCrashAfterCommit(t *testing.T) {
	const env = "GOAGENT_APPENDALL_CRASH_DIR"
	if dir := os.Getenv(env); dir != "" {
		store, err := NewFileStore(dir)
		require.NoError(t, err)
		txnlog.AfterCommit = func() { os.Exit(appendAllCrashExit) }
		_, _ = store.AppendAll(context.Background(),
			AppendBatch{ID: "a", Events: []Event{userEvent(t, "a1")}},
			AppendBatch{ID: "b", Events: []Event{userEvent(t, "b1"), userEvent(t, "b2")}},
		)
		t.Fatal("期望进程在写入中退出")
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestFileStoreAppendAllCrashAfterCommit$")
	cmd.Env = append(os.Environ(), env+"="+dir)
	output, err := cmd.CombinedOutput()
	var exited *exec.ExitError
	require.ErrorAs(t, err, &exited, string(output))
	require.Equal(t, appendAllCrashExit, exited.ExitCode(), string(output))

	store, err := NewFileStore(dir)
	require.NoError(t, err)
	ctx := context.Background()
	ids, err := store.List(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, ids)
	b, err := store.Read(ctx, "b", 0)
	require.NoError(t, err)
	require.Len(t, b, 2)
	last, err := store.Append(ctx, "a", 1, userEvent(t, "a2"))
	require.NoError(t, err)
	require.Equal(t, int64(2), last)
}

// 前滚可能遇到已部分应用的提交：序号已到达的会话不重复写入。
func TestFileStoreApplyTxnIsIdempotent(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := context.Background()
	payload, err := json.Marshal([]AppendBatch{
		{ID: "a", Events: assign([]Event{userEvent(t, "a1")}, 0)},
		{ID: "b", Events: assign([]Event{userEvent(t, "b1")}, 0)},
	})
	require.NoError(t, err)
	_, err = store.Append(ctx, "a", 0, userEvent(t, "a1"))
	require.NoError(t, err)
	require.NoError(t, store.applyTxn(payload))
	require.NoError(t, store.applyTxn(payload))
	for _, id := range []string{"a", "b"} {
		events, err := store.Read(ctx, id, 0)
		require.NoError(t, err)
		require.Len(t, events, 1, id)
	}
}

// 快照位于同一会话：DurableStore 的批量写入是一次 Append，其他写入者造成的序号冲突按最新序号重试。
func TestDurableStoreSaveAllIsOneAppend(t *testing.T) {
	memory := NewMemoryStore()
	view := DurableStore(memory, "run").(*durableStore)
	ctx := context.Background()
	require.NoError(t, view.SaveAll(ctx, durableWrites()...))
	events, err := memory.Read(ctx, "run", 0)
	require.NoError(t, err)
	require.Len(t, events, 2)
	_, err = memory.Append(ctx, "run", 2, userEvent(t, "other writer"))
	require.NoError(t, err)
	require.NoError(t, view.SaveAll(ctx, durableWrites()...))
	events, err = memory.Read(ctx, "run", 0)
	require.NoError(t, err)
	require.Len(t, events, 5)
}

func durableWrites() []durable.Write {
	data := json.RawMessage(`{}`)
	return []durable.Write{
		{ID: "x", Snapshot: &durable.Snapshot{Version: "v", RunID: "x", Data: data}},
		{ID: "y", Snapshot: &durable.Snapshot{Version: "v", RunID: "y", Data: data}},
	}
}
