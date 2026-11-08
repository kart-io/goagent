package session

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
)

func TestFileStoreFirstHeaderCrashWindows(t *testing.T) {
	header, err := json.Marshal(fileHeader{Session: "s", Version: fileVersion})
	require.NoError(t, err)
	header = append(header, '\n')
	for cut := 0; cut <= len(header); cut++ {
		for _, batch := range []bool{false, true} {
			t.Run(fmt.Sprintf("窗口%d/批量%t", cut, batch), func(t *testing.T) {
				ctx := context.Background()
				s, err := NewFileStore(t.TempDir())
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(s.path("s"), header[:cut], 0o600))
				ev := Event{Type: constants.SessionEventTurnStart, Data: json.RawMessage(`{"input":"一"}`)}
				if batch {
					payload, e := json.Marshal([]AppendBatch{{ID: "s", Events: []Event{ev}}, {ID: "b", Events: []Event{ev}}})
					require.NoError(t, e)
					require.NoError(t, s.applyTxn(payload))
					require.NoError(t, s.applyTxn(payload), "重复前滚不能损坏文件")
				} else {
					last, e := s.Append(ctx, "s", 0, ev)
					require.NoError(t, e)
					require.EqualValues(t, 1, last)
				}
				fresh, err := NewFileStore(s.dir)
				require.NoError(t, err)
				events, err := fresh.Read(ctx, "s", 0)
				require.NoError(t, err)
				require.Len(t, events, 1)
				last, err := fresh.Append(ctx, "s", 1, ev)
				require.NoError(t, err)
				require.EqualValues(t, 2, last)
			})
		}
	}
}

func TestMemoryStoreEventOwnership(t *testing.T) {
	ctx := context.Background()
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprint(batch), func(t *testing.T) {
			s := NewMemoryStore()
			ev := Event{Type: constants.SessionEventRewind, Data: json.RawMessage(`{"v":1}`), Surface: &Surface{Start: 1, End: 2}}
			if batch {
				_, err := s.AppendAll(ctx, AppendBatch{ID: "s", Events: []Event{ev}})
				require.NoError(t, err)
			} else {
				_, err := s.Append(ctx, "s", 0, ev)
				require.NoError(t, err)
			}
			require.NoError(t, s.Fork(ctx, "s", "fork", 0))
			ev.Data[5] = '9'
			ev.Surface.Start = 9
			for _, id := range []string{"s", "fork"} {
				events, err := s.Read(ctx, id, 0)
				require.NoError(t, err)
				require.JSONEq(t, `{"v":1}`, string(events[0].Data), "输入不能改写日志")
				require.EqualValues(t, 1, events[0].Surface.Start)
				events[0].Data[5] = '8'
				events[0].Surface.Start = 8
			}
			for _, id := range []string{"s", "fork"} {
				events, err := s.Read(ctx, id, 0)
				require.NoError(t, err)
				require.JSONEq(t, `{"v":1}`, string(events[0].Data), "读取和分支不能共享可写数据")
				require.EqualValues(t, 1, events[0].Surface.Start)
			}
		})
	}
}

func TestRecorderEventOwnership(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	r, _, err := OpenRecorder(ctx, s, "s")
	require.NoError(t, err)
	ev := Event{Type: constants.SessionEventRewind, Data: json.RawMessage(`{"v":1}`), Surface: &Surface{Start: 1, End: 2}}
	added, err := r.Append(ctx, ev)
	require.NoError(t, err)
	ev.Data[5] = '9'
	added[0].Surface.Start = 9
	got := r.Events()
	require.JSONEq(t, `{"v":1}`, string(got[0].Data))
	require.EqualValues(t, 1, got[0].Surface.Start)
	got[0].Data[5] = '8'
	require.JSONEq(t, `{"v":1}`, string(r.Events()[0].Data))
	r2, opened, err := OpenRecorder(ctx, s, "s")
	require.NoError(t, err)
	opened[0].Data[5] = '7'
	require.JSONEq(t, `{"v":1}`, string(r2.Events()[0].Data))
}
