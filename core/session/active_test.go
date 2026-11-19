package session

import (
	"fmt"
	"math/rand"
	"runtime"
	"slices"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
)

// referenceActive 是按检查点整体克隆视图的原始实现，作为语义基准。
func referenceActive(events []Event) []Event {
	type checkpoint struct {
		seq    int64
		active []Event
	}
	var active []Event
	marks := []checkpoint{{}}
	for i, ev := range events {
		if !undoes(ev) {
			active = append(active, ev)
			continue
		}
		if ev.Type == constants.SessionEventRewind {
			j := len(marks) - 1
			for marks[j].seq >= ev.Surface.Start {
				j--
			}
			active = slices.Clone(marks[j].active)
			for _, e := range events[:i] {
				if e.Seq > marks[j].seq && e.Seq < ev.Surface.Start {
					active = append(active, e)
				}
			}
		} else {
			active = slices.DeleteFunc(active, func(e Event) bool { return ev.Surface.covers(e.Seq) })
		}
		marks = append(marks, checkpoint{ev.Seq, slices.Clone(active)})
	}
	return active
}

var abortedEnd = func() Event {
	ev, err := NewEvent(constants.SessionEventTurnEnd, TurnEnd{Aborted: true})
	if err != nil {
		panic(err)
	}
	return ev
}()

// randomLog 生成序号连续的日志，撤销事件的区间随机落在之前的任意位置，含退回到更早退回之前与中止跨越退回。
func randomLog(r *rand.Rand, n int) []Event {
	events := make([]Event, 0, n)
	for seq := int64(1); seq <= int64(n); seq++ {
		ev := Event{Seq: seq, Type: constants.SessionEventUserMessage}
		if seq > 1 {
			switch r.Intn(6) {
			case 0:
				ev = abortedEnd
				ev.Seq = seq
				start := 1 + r.Int63n(seq-1)
				ev.Surface = &Surface{Start: start, End: start + r.Int63n(seq-start)}
			case 1:
				ev = Event{Seq: seq, Type: constants.SessionEventRewind, Surface: &Surface{Start: 1 + r.Int63n(seq-1), End: seq - 1}}
			}
		}
		events = append(events, ev)
	}
	return events
}

func activeSeqs(events []Event) []int64 {
	out := []int64{}
	for _, ev := range events {
		out = append(out, ev.Seq)
	}
	return out
}

func TestActiveEventsMatchesReference(t *testing.T) {
	for seed := int64(0); seed < 3000; seed++ {
		r := rand.New(rand.NewSource(seed))
		events := randomLog(r, 1+r.Intn(60))
		require.Equal(t, activeSeqs(referenceActive(events)), activeSeqs(activeEvents(events)), "seed %d", seed)
	}
}

// 检查点不能克隆整个视图：此前每次撤销都复制全部活动事件，5000 个事件、83 次中止分配约 17MB。
func TestActiveEventsDoesNotCloneViewPerUndo(t *testing.T) {
	events := make([]Event, 0, 5000)
	for seq := int64(1); seq <= 5000; seq++ {
		ev := Event{Seq: seq, Type: constants.SessionEventUserMessage}
		if seq%60 == 0 {
			ev = abortedEnd
			ev.Seq, ev.Surface = seq, &Surface{Start: seq - 5, End: seq - 1}
		}
		events = append(events, ev)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	active := activeEvents(events)
	runtime.ReadMemStats(&after)
	require.Len(t, active, 5000-83*6)
	limit := uint64(3 * len(events) * int(unsafe.Sizeof(Event{})))
	require.Less(t, after.TotalAlloc-before.TotalAlloc, limit, fmt.Sprintf("分配应不超过 3 份事件切片（%d 字节）", limit))
}
