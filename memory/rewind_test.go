package memory

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/llm"
)

// rewindFixture 是切开轮次的回退测试的环境：抽取按对话中出现的关键词各产出一条事实。
type rewindFixture struct {
	t        *testing.T
	m        *Memory
	sessions session.Store
	ctx      context.Context
	last     int64 // 会话的最后序号，session.Rewind 的 CAS
	hook     func()
}

var rewindFacts = map[string]string{"上海": "用户住在上海", "杭州": "用户在杭州出差", "北京": "用户要去北京"}

func newRewindFixture(t *testing.T) *rewindFixture {
	f := &rewindFixture{t: t, sessions: session.NewMemoryStore(), ctx: WithNamespace(context.Background(), "u")}
	model := &scriptedModel{reply: func(system, user string) (string, error) {
		switch {
		case system == reconcilePrompt:
			return `{"op":"add"}`, nil
		case system == failurePrompt:
			return `{"lesson":"中止前先确认","importance":0.5}`, nil
		}
		if hook := f.hook; hook != nil {
			f.hook = nil
			hook()
		}
		var out struct {
			Memories []candidate `json:"memories"`
		}
		for _, k := range slices.Sorted(func(yield func(string) bool) {
			for k := range rewindFacts {
				if !yield(k) {
					return
				}
			}
		}) {
			if strings.Contains(user, k) {
				out.Memories = append(out.Memories, candidate{Kind: KindFact, Content: rewindFacts[k], Importance: 0.5})
			}
		}
		b, err := json.Marshal(out)
		return string(b), err
	}}
	f.m = newMemory(t, Config{Model: model, Sessions: f.sessions, Retention: Retention{InvalidRetention: time.Hour}})
	return f
}

// turn 追加一轮（用户消息与若干条助手回复），返回轮次区间与各消息的序号。
func (f *rewindFixture) turn(aborted bool, user string, replies ...string) (from, to int64, seqs []int64) {
	msgs := []llm.Message{llm.UserMessage(user)}
	for _, r := range replies {
		msgs = append(msgs, llm.Message{Role: "assistant", Content: r})
	}
	from, to = appendTurn(f.t, f.sessions, "s", aborted, msgs...)
	events, err := f.sessions.Read(context.Background(), "s", 0)
	require.NoError(f.t, err)
	for _, m := range msgs {
		for _, ev := range events {
			var d session.MessageData
			if ev.Seq >= from && ev.Seq <= to && json.Unmarshal(ev.Data, &d) == nil && d.Message.Content == m.Content {
				seqs = append(seqs, ev.Seq)
				break
			}
		}
	}
	require.Len(f.t, seqs, len(msgs))
	f.last = to
	return from, to, seqs
}

func (f *rewindFixture) enqueue(key string, from, to, turnSeq int64, aborted bool) {
	require.NoError(f.t, f.m.Enqueue(f.ctx, Job{Key: key, Namespace: []string{"u"}, Session: "s", Scope: "0", From: from, To: to, TurnSeq: turnSeq, Aborted: aborted}))
}

// rewind 先退回会话（宿主的顺序），再通知长期记忆。
func (f *rewindFixture) rewind(to int64) {
	seq, err := session.Rewind(context.Background(), f.sessions, "s", to, f.last)
	require.NoError(f.t, err)
	f.last = seq
	require.NoError(f.t, f.m.Rewound(f.ctx, "s", to))
}

func (f *rewindFixture) process() {
	_, err := f.m.ProcessPending(f.ctx)
	require.NoError(f.t, err)
}

func (f *rewindFixture) facts() []string {
	var out []string
	for _, r := range stateOf(f.t, f.m).Records {
		if r.Valid() {
			out = append(out, r.Content)
		}
	}
	slices.Sort(out)
	return out
}

func (f *rewindFixture) job(key string) Job {
	j, ok, err := f.m.Job(f.ctx, key)
	require.NoError(f.t, err)
	require.True(f.t, ok, "任务 %s 应存在", key)
	return j
}

// 任务完成后回退到用户消息：整轮的记录失效，补登 [From, to] 的任务；处理后只留下用户消息中的事实。
func TestRewoundCutTurnAfterDone(t *testing.T) {
	f := newRewindFixture(t)
	from, _, seqs := f.turn(false, "我住在上海", "北京见")
	f.enqueue("s/1", from, f.last, 0, false)
	f.process()
	require.Equal(t, []string{"用户住在上海", "用户要去北京"}, f.facts())

	f.rewind(seqs[0])
	s := stateOf(t, f.m)
	require.Empty(t, f.facts(), "切开的轮次整轮回退")
	patch := f.job(cutJobKey("s", from, seqs[0]))
	require.Equal(t, JobPending, patch.Status)
	require.Equal(t, Job{Key: patch.Key, Namespace: []string{"u"}, Session: "s", From: from, To: seqs[0], Scope: "0", TurnSeq: s.Seq,
		Status: JobPending, CreatedAt: patch.CreatedAt}, patch)
	require.Len(t, s.Markers, 1)

	f.process()
	require.Equal(t, JobDone, f.job(patch.Key).Status)
	require.Equal(t, []string{"用户住在上海"}, f.facts(), "被遮蔽回复中的事实不再出现")
}

// Worker 读完对话后、提交前回退到轮次中间：原任务的提交被拒绝并丢弃，补登任务完成后只含可见部分。
func TestRewoundCutTurnDuringProcessing(t *testing.T) {
	f := newRewindFixture(t)
	from, _, seqs := f.turn(false, "我住在上海", "北京见")
	f.enqueue("s/1", from, f.last, 0, false)
	f.hook = func() { f.rewind(seqs[0]) }
	dropped := f.m.DroppedJobs()
	f.process()
	require.Equal(t, JobDropped, f.job("s/1").Status)
	require.Equal(t, dropped+1, f.m.DroppedJobs())

	f.process()
	require.Equal(t, JobDone, f.job(cutJobKey("s", from, seqs[0])).Status)
	require.Equal(t, []string{"用户住在上海"}, f.facts())
}

// 回退时任务仍 pending：原任务置为 dropped 并计入 DroppedJobs，补登任务替代它。
func TestRewoundDropsPendingAndPatches(t *testing.T) {
	f := newRewindFixture(t)
	from, _, seqs := f.turn(false, "我住在上海", "北京见")
	f.enqueue("s/1", from, f.last, 0, false)
	dropped := f.m.DroppedJobs()
	f.rewind(seqs[0])
	require.Equal(t, JobDropped, f.job("s/1").Status)
	require.Equal(t, dropped+1, f.m.DroppedJobs())
	require.Equal(t, JobPending, f.job(cutJobKey("s", from, seqs[0])).Status)

	f.process()
	require.Equal(t, []string{"用户住在上海"}, f.facts())
}

// 先回退到 to、再回退到 to' < to：先后补登两个任务，最终只留下 [From, to'] 的内容；回退到 From 之前，整轮失效且不再补登。
func TestRewoundTwiceWithinTurn(t *testing.T) {
	f := newRewindFixture(t)
	from, _, seqs := f.turn(false, "我住在上海", "杭州这边", "北京见")
	f.enqueue("s/1", from, f.last, 0, false)
	f.process()
	require.Equal(t, []string{"用户住在上海", "用户在杭州出差", "用户要去北京"}, f.facts())

	f.rewind(seqs[1])
	f.process()
	require.Equal(t, []string{"用户住在上海", "用户在杭州出差"}, f.facts())

	f.rewind(seqs[0])
	f.process()
	require.Equal(t, JobDone, f.job(cutJobKey("s", from, seqs[1])).Status)
	require.Equal(t, JobDone, f.job(cutJobKey("s", from, seqs[0])).Status)
	require.Equal(t, []string{"用户住在上海"}, f.facts())

	jobs := len(stateOf(t, f.m).Jobs) + len(stateOf(t, f.m).DoneJobs)
	f.rewind(from - 1)
	require.Empty(t, f.facts())
	s := stateOf(t, f.m)
	require.Equal(t, jobs, len(s.Jobs)+len(s.DoneJobs), "回退到轮次之前不补登")
	require.False(t, hasPendingIn(s))
}

// 回退到轮次结尾之后（to ≥ To）：这一轮不受影响，不补登。
func TestRewoundAfterTurnEnd(t *testing.T) {
	f := newRewindFixture(t)
	from, to, _ := f.turn(false, "我住在上海", "北京见")
	f.enqueue("s/1", from, to, 0, false)
	f.process()
	f.turn(false, "杭州这边")
	f.rewind(to)
	require.Equal(t, []string{"用户住在上海", "用户要去北京"}, f.facts())
	require.False(t, hasPendingIn(stateOf(t, f.m)))
}

// 回退时轮次仍在进行、之后才登记：被切开的截到 m.To，整轮被遮蔽的登记为 dropped；重放登记不产生新任务。
func TestEnqueueAfterRewind(t *testing.T) {
	tests := []struct {
		name string
		to   func(from int64, seqs []int64) int64
		// want 是登记后应存在的任务键与状态。
		want func(from int64, seqs []int64) (string, JobStatus)
	}{
		{"切开截断", func(_ int64, seqs []int64) int64 { return seqs[0] },
			func(from int64, seqs []int64) (string, JobStatus) { return cutJobKey("s", from, seqs[0]), JobPending }},
		{"整轮遮蔽", func(from int64, _ []int64) int64 { return from - 1 },
			func(int64, []int64) (string, JobStatus) { return "s/1", JobDropped }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newRewindFixture(t)
			f.turn(false, "热身")
			turnSeq := stateOf(t, f.m).Seq
			from, to, seqs := f.turn(false, "我住在上海", "北京见")
			f.rewind(tt.to(from, seqs))
			dropped := f.m.DroppedJobs()
			f.enqueue("s/1", from, to, turnSeq, false)
			key, status := tt.want(from, seqs)
			require.Equal(t, status, f.job(key).Status)
			if status == JobDropped {
				require.Equal(t, dropped+1, f.m.DroppedJobs())
			} else {
				_, ok, err := f.m.Job(f.ctx, "s/1")
				require.NoError(t, err)
				require.False(t, ok, "截断后按新键登记")
				require.Equal(t, stateOf(t, f.m).Seq, f.job(key).TurnSeq)
			}
			seq := stateOf(t, f.m).Seq
			f.enqueue("s/1", from, to, turnSeq, false)
			require.Equal(t, seq, stateOf(t, f.m).Seq, "重放登记不提交")

			f.process()
			want := []string(nil)
			if status == JobPending {
				want = []string{"用户住在上海"}
			}
			require.Equal(t, want, f.facts())
		})
	}
}

// 中止的轮次被切开时补登的任务仍是中止的：已完成的轮次按教训记录认出。
func TestRewoundPatchKeepsAborted(t *testing.T) {
	f := newRewindFixture(t)
	from, _, seqs := f.turn(true, "我住在上海", "北京见")
	f.enqueue("s/1", from, f.last, 0, true)
	f.process()
	f.rewind(seqs[0])
	require.True(t, f.job(cutJobKey("s", from, seqs[0])).Aborted)
}

// 轮次中有记录被遗忘过时不补登：补登任务的 TurnSeq 晚于遗忘，会重新学回被遗忘的内容。
func TestRewoundSkipsPatchAfterForget(t *testing.T) {
	f := newRewindFixture(t)
	from, _, seqs := f.turn(false, "我住在上海", "北京见")
	f.enqueue("s/1", from, f.last, 0, false)
	f.process()
	for id, r := range stateOf(t, f.m).Records {
		if r.Content == "用户住在上海" {
			require.NoError(t, f.m.Forget(f.ctx, id))
		}
	}
	f.rewind(seqs[0])
	require.False(t, hasPendingIn(stateOf(t, f.m)))
	require.Empty(t, f.facts())
}

// Prune 按保留期回收标记并推进 MarkerHorizon；TurnSeq 早于它的任务无从核对，登记即丢弃。
func TestPruneReclaimsMarkers(t *testing.T) {
	f := newRewindFixture(t)
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	f.m.now = func() time.Time { return now }
	from, to, _ := f.turn(false, "我住在上海")
	f.turn(false, "杭州这边")
	f.rewind(to)
	mk := stateOf(t, f.m).Markers
	require.Len(t, mk, 1)
	now = now.Add(2 * time.Hour)
	require.NoError(t, f.m.Prune(f.ctx))
	s := stateOf(t, f.m)
	require.Empty(t, s.Markers)
	for _, m := range mk {
		require.Equal(t, m.Seq, s.MarkerHorizon)
	}
	f.enqueue("s/1", from, to, 0, false)
	require.Equal(t, JobDropped, f.job("s/1").Status)
}

// 任务是否因回退标记丢弃：同一会话、标记晚于轮次、轮次有消息被遮蔽才丢弃；TurnSeq 早于 MarkerHorizon 也丢弃。
func TestJobStale(t *testing.T) {
	marker := rewindMarker{Session: "s", To: 10, Seq: 5}
	tests := []struct {
		name    string
		s       state
		session string
		turnSeq int64
		to      int64
		want    bool
	}{
		{"遮蔽本轮", state{Markers: map[string]rewindMarker{marker.key(): marker}}, "s", 4, 11, true},
		{"另一会话", state{Markers: map[string]rewindMarker{marker.key(): marker}}, "t", 4, 11, false},
		{"回退早于轮次", state{Markers: map[string]rewindMarker{marker.key(): marker}}, "s", 5, 11, false},
		{"回退不覆盖本轮", state{Markers: map[string]rewindMarker{marker.key(): marker}}, "s", 4, 10, false},
		{"标记已回收", state{MarkerHorizon: 5}, "s", 4, 11, true},
		{"遗忘", state{LastForgetSeq: 5}, "s", 4, 3, true},
		{"无事发生", state{}, "s", 4, 11, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, jobStale(tt.s, tt.session, tt.turnSeq, tt.to))
		})
	}
}

func hasPendingIn(s state) bool { return len(pendingJobs(s)) > 0 }

// 回退标记与 MarkerHorizon 经覆盖层与基础快照都能还原：另一个不用缓存的实例读到的与写入的相同，压缩前后一致，
// 覆盖层中的删除遮住基础快照中的标记。
func TestMarkersPersistThroughOverlayAndCompaction(t *testing.T) {
	ctx := context.Background()
	st, writer := seedAndCompact(t, 3)
	reader := durableMemory(t, st, Config{StateCacheBytes: -1})
	a := rewindMarker{Session: "s", To: 4, Seq: 5, At: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)}
	b := rewindMarker{Session: "s", To: 2, Seq: 6, At: a.At}
	put := func(fn func(t *tx)) {
		t.Helper()
		_, err := writer.mutate(ctx, testNS, "", func(t *tx) error { fn(t); return nil })
		require.NoError(t, err)
	}
	check := func(msg string, horizon int64, want ...rewindMarker) {
		t.Helper()
		_, s, err := reader.read(ctx, testNS)
		require.NoError(t, err)
		wantMap := map[string]rewindMarker{}
		for _, m := range want {
			wantMap[m.key()] = m
		}
		require.Equal(t, wantMap, s.Markers, msg)
		require.Equal(t, horizon, s.MarkerHorizon, msg)
	}

	put(func(t *tx) { t.Markers.Put(a.key(), a); t.Markers.Put(b.key(), b); t.SetMarkerHorizon(3) })
	require.Positive(t, headOf(t, writer).OverlayItems, "标记在覆盖层中")
	check("覆盖层", 3, a, b)
	require.NoError(t, writer.compact(ctx, testNS, true, true))
	require.Zero(t, headOf(t, writer).OverlayItems)
	check("基础快照", 3, a, b)
	put(func(t *tx) { t.Markers.Delete(a.key()); t.SetMarkerHorizon(5) })
	check("覆盖层删除基础快照中的标记", 5, b)
}
