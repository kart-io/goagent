package storetest

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
)

// RunHistoryConformance checks the HistoryReader contract: hidden events are
// never returned, types narrow the visible ones, a positive limit is exact
// and limit 0 returns everything, NextSeq resumes after the last returned
// event, an empty page has non-nil Items and NextSeq equal to after, a
// session without a log is an empty page and a negative limit is invalid.
// newReader returns the reader and the session.Store it reads from.
func RunHistoryConformance(t *testing.T, newReader func(t *testing.T) (mg.HistoryReader, session.Store)) {
	// 序号 1..8；3 和 6 是业务历史不可见的类型，8 结尾也不可见。
	types := []string{
		"user/message", "assistant/message", mg.HistoryHiddenPrefix + "task", "turn/end",
		"user/message", constants.SessionEventBoundarySnapshot, "assistant/message", mg.HistoryHiddenPrefix + "audit",
	}
	cases := []struct {
		name    string
		after   int64
		limit   int
		types   []string
		want    []int64
		nextSeq int64
	}{
		{name: "no limit returns every visible event", want: []int64{1, 2, 4, 5, 7}, nextSeq: 7},
		{name: "limit is exact", limit: 2, want: []int64{1, 2}, nextSeq: 2},
		{name: "limit counts visible events only", after: 2, limit: 2, want: []int64{4, 5}, nextSeq: 5},
		{name: "after is exclusive", after: 4, want: []int64{5, 7}, nextSeq: 7},
		{name: "types narrow the page", types: []string{"user/message"}, want: []int64{1, 5}, nextSeq: 5},
		{name: "types and limit combine", types: []string{"assistant/message", "turn/end"}, limit: 2, want: []int64{2, 4}, nextSeq: 4},
		{name: "nothing newer keeps after", after: 7, want: []int64{}, nextSeq: 7},
		{name: "unmatched type keeps after", after: 1, types: []string{"tool/result"}, want: []int64{}, nextSeq: 1},
	}
	ctx := context.Background()
	reader, sessions := newReader(t)
	id := "storetest-history-" + rand.Text()
	events := make([]session.Event, len(types))
	for i, typ := range types {
		ev, err := session.NewEvent(typ, map[string]int{"n": i + 1})
		require.NoError(t, err)
		events[i] = ev
	}
	// 快照事件受持久运行锁保护，写入时必须持有对应的锁。
	unlock, err := sessions.Lock(ctx, id, session.DurableLockName(""))
	require.NoError(t, err)
	last, err := sessions.Append(ctx, id, 0, events...)
	require.NoError(t, err)
	require.NoError(t, unlock())
	require.EqualValues(t, len(types), last)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			page, err := reader.History(ctx, id, c.after, c.limit, c.types)
			require.NoError(t, err)
			require.NotNil(t, page.Items)
			seqs := make([]int64, len(page.Items))
			for i, ev := range page.Items {
				seqs[i] = ev.Seq
				require.Equal(t, types[ev.Seq-1], ev.Type)
				require.JSONEq(t, string(events[ev.Seq-1].Data), string(ev.Data))
			}
			require.Equal(t, c.want, seqs)
			require.Equal(t, c.nextSeq, page.NextSeq)
		})
	}
	t.Run("pages resume from NextSeq", func(t *testing.T) {
		var seqs []int64
		after := int64(0)
		for {
			page, err := reader.History(ctx, id, after, 2, nil)
			require.NoError(t, err)
			for _, ev := range page.Items {
				seqs = append(seqs, ev.Seq)
			}
			if len(page.Items) < 2 {
				break
			}
			after = page.NextSeq
		}
		require.Equal(t, []int64{1, 2, 4, 5, 7}, seqs)
	})
	t.Run("session without a log is an empty page", func(t *testing.T) {
		page, err := reader.History(ctx, "storetest-history-missing-"+rand.Text(), 3, 0, nil)
		require.NoError(t, err)
		require.NotNil(t, page.Items)
		require.Empty(t, page.Items)
		require.EqualValues(t, 3, page.NextSeq)
	})
	t.Run("negative limit is invalid", func(t *testing.T) {
		_, err := reader.History(ctx, id, 0, -1, nil)
		require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	})
}
