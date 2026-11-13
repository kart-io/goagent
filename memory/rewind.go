package memory

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// 回退标记与任务丢弃，设计见 docs/design/MEMORY_G2_DERIVATION.md 第 4、5 节。

// rewindMarker 是一次回退：会话 Session 中序号 To 之后的消息在提交 Seq 时被遮蔽。
// 登记早于它、含被遮蔽消息的任务据此丢弃，包括回退时仍在进行、之后才登记的轮次。
type rewindMarker struct {
	Session string    `json:"session"`
	To      int64     `json:"to"`
	Seq     int64     `json:"seq"`
	At      time.Time `json:"at"`
}

func (m rewindMarker) key() string { return m.Session + "\x00" + strconv.FormatInt(m.Seq, 10) }

// rewound 报告是否有来源轮次在会话 sessionID 中有消息位于序号 to 之后，即这一轮有消息被遮蔽。
func rewound(sources []Source, sessionID string, to int64) bool {
	return slices.ContainsFunc(sources, func(s Source) bool { return s.Session == sessionID && max(s.From, s.To) > to })
}

// markerCut 返回提交 turnSeq 之后、遮蔽了会话 session 中序号 to 之前消息的回退标记里最小的 To；没有时 ok 为 false。
func markerCut(markers map[string]rewindMarker, session string, turnSeq, to int64) (cut int64, ok bool) {
	for _, m := range markers {
		if m.Session == session && m.Seq > turnSeq && to > m.To && (!ok || m.To < cut) {
			cut, ok = m.To, true
		}
	}
	return cut, ok
}

// jobStale 报告任务是否应当丢弃：登记之后发生了遗忘或遮蔽它的回退；TurnSeq 早于 MarkerHorizon 时相关标记可能已被回收，保守起见也丢弃。
func jobStale(s state, session string, turnSeq, to int64) bool {
	if s.LastForgetSeq > turnSeq || turnSeq < s.MarkerHorizon {
		return true
	}
	_, cut := markerCut(s.Markers, session, turnSeq, to)
	return cut
}

// cutJobKey 是轮次 [from, to] 可见部分的任务键。
func cutJobKey(session string, from, to int64) string {
	return fmt.Sprintf("%s/%d/%d", session, from, to)
}

// cutTurns 返回回退到 to 时被切开（From ≤ to < To）的轮次的补登任务，轮次取自有效记录、关系派生、实体名称来源与待处理任务，
// 按 (From, To) 去重，任务键已登记过的跳过。只读 t，Rewound 在修改之前调用。
// 已完成的轮次不再有任务可查 Aborted，按是否产出过教训记录（worker.learnFromFailure 的 ID）判断。
// 轮次中有记录被遗忘过、或待处理任务已因遗忘失效时不补登：补登任务的 TurnSeq 晚于遗忘，会重新学回被遗忘的内容。
func cutTurns(t *tx, session string, to int64, now time.Time) []Job {
	type turn [2]int64
	turns, aborted, forgotten := map[turn]bool{}, map[turn]bool{}, map[turn]bool{}
	cut := func(s Source) (turn, bool) {
		return turn{s.From, s.To}, s.Session == session && s.From > 0 && s.From <= to && to < s.To
	}
	for _, r := range t.Records.m {
		for _, s := range r.Sources {
			k, ok := cut(s)
			switch {
			case !ok:
			case erased(r):
				forgotten[k] = true
			case r.Valid():
				turns[k] = true
				if r.ID == stableID(s.Job, "lesson") {
					aborted[k] = true
				}
			}
		}
	}
	for _, e := range t.Edges.m {
		for _, d := range e.Derivations {
			if k, ok := cut(d.Source); ok {
				turns[k] = true
			}
		}
	}
	for _, e := range t.Entities.m {
		for _, n := range e.Names {
			for _, o := range n.Origins {
				if k, ok := cut(o.Source); ok {
					turns[k] = true
				}
			}
		}
	}
	for _, j := range t.Jobs.m {
		k, ok := cut(j.source())
		if !ok || j.Status != JobPending {
			continue
		}
		if t.LastForgetSeq() > j.TurnSeq {
			forgotten[k] = true
			continue
		}
		turns[k] = true
		aborted[k] = aborted[k] || j.Aborted
	}
	var out []Job
	for k := range turns {
		key := cutJobKey(session, k[0], to)
		_, pending := t.Jobs.m[key]
		_, done := t.DoneJobs.m[key]
		if forgotten[k] || pending || done {
			continue
		}
		out = append(out, Job{Key: key, Namespace: slices.Clone(t.ns), Session: session, From: k[0], To: to, Aborted: aborted[k],
			Scope: t.scope, TurnSeq: t.nextSeq(), Status: JobPending, CreatedAt: now})
	}
	slices.SortFunc(out, func(a, b Job) int { return strings.Compare(a.Key, b.Key) })
	return slices.CompactFunc(out, func(a, b Job) bool { return a.Key == b.Key })
}
