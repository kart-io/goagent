package durable

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
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

// 投递方崩溃后重投：键与条目一起落盘，确认之后再次写入也不产生第二条。
func TestInboxAppendOnceDeduplicatesAcrossAckAndRestart(t *testing.T) {
	store := mustNewFileStore(t, t.TempDir())
	ctx := WithJournal(WithExecutionKey(context.Background(), "run"), store)
	box, _, err := OpenInbox(ctx)
	require.NoError(t, err)

	seq, duplicate, err := box.AppendOnce(ctx, "notice-1", "steer", "a")
	require.NoError(t, err)
	require.False(t, duplicate)
	_, duplicate, err = box.AppendOnce(ctx, "notice-1", "steer", "a")
	require.NoError(t, err)
	require.True(t, duplicate)

	require.NoError(t, box.Ack(ctx, seq))
	reopened, entries, err := OpenInbox(ctx)
	require.NoError(t, err)
	require.Empty(t, entries)
	_, duplicate, err = reopened.AppendOnce(ctx, "notice-1", "steer", "a")
	require.NoError(t, err)
	require.True(t, duplicate, "确认和重启之后仍然去重")

	_, duplicate, err = reopened.AppendOnce(ctx, "notice-2", "steer", "b")
	require.NoError(t, err)
	require.False(t, duplicate)
	_, entries, err = OpenInbox(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestInboxAppendOnceBoundsRememberedKeys(t *testing.T) {
	store := mustNewFileStore(t, t.TempDir())
	ctx := WithJournal(WithExecutionKey(context.Background(), "run"), store)
	box, _, err := OpenInbox(ctx)
	require.NoError(t, err)
	for i := range constants.DurableInboxSeenLimit + 1 {
		_, duplicate, err := box.AppendOnce(ctx, fmt.Sprintf("k%d", i), "steer", "x")
		require.NoError(t, err)
		require.False(t, duplicate)
	}
	state, err := box.load(ctx)
	require.NoError(t, err)
	require.Len(t, state.Seen, constants.DurableInboxSeenLimit)
	_, duplicate, err := box.AppendOnce(ctx, fmt.Sprintf("k%d", constants.DurableInboxSeenLimit), "steer", "x")
	require.NoError(t, err)
	require.True(t, duplicate, "最近的键仍然记得")
}
