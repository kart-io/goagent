package durable

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInboxKeepsEntriesUntilAcked(t *testing.T) {
	store := mustNewFileStore(t, t.TempDir())
	ctx := WithJournal(WithExecutionKey(context.Background(), "run"), store)

	box, entries, err := OpenInbox(ctx)
	require.NoError(t, err)
	require.Empty(t, entries)
	first, err := box.Append(ctx, "steer", "a")
	require.NoError(t, err)
	second, err := box.Append(ctx, "followup", "b")
	require.NoError(t, err)
	require.Less(t, first, second)

	_, entries, err = OpenInbox(ctx)
	require.NoError(t, err)
	require.Equal(t, []InboxEntry{{Seq: first, Kind: "steer", Text: "a"}, {Seq: second, Kind: "followup", Text: "b"}}, entries)

	require.NoError(t, box.Ack(ctx, first))
	require.NoError(t, box.Ack(ctx, first), "重复确认被忽略")
	reopened, entries, err := OpenInbox(ctx)
	require.NoError(t, err)
	require.Equal(t, []InboxEntry{{Seq: second, Kind: "followup", Text: "b"}}, entries)

	// 序号不复用：全部确认后新条目仍递增，恢复时不会把新输入当成已领取。
	require.NoError(t, reopened.Ack(ctx, second))
	third, err := reopened.Append(ctx, "inject", "c")
	require.NoError(t, err)
	require.Greater(t, third, second)
}

func TestInboxOutsideDurableRun(t *testing.T) {
	box, entries, err := OpenInbox(context.Background())
	require.NoError(t, err)
	require.Nil(t, box)
	require.Nil(t, entries)
}

func TestInboxRejectsCorruptRecord(t *testing.T) {
	store := mustNewFileStore(t, t.TempDir())
	ctx := WithJournal(WithExecutionKey(context.Background(), "run"), store)
	id := "goagent/inbox/" + ExecutionKey(ctx)
	require.NoError(t, store.Save(ctx, id, &Snapshot{Version: "other", RunID: id, Data: json.RawMessage(`{}`)}))
	_, _, err := OpenInbox(ctx)
	require.Error(t, err)
}
