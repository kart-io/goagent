// Package sessiontest 是 session.Store 实现的一致性测试，覆盖 session.Store 文档中的契约，并以 session.DurableStore
// 视图运行 core/durable/storetest。后端实现在自己的测试中调用 Run，用同一后端上的两个独立实例模拟两个进程。
package sessiontest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/durable/storetest"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/llm"
)

// Options 选择要运行的可选用例。
type Options struct {
	// Leases 非 nil 时，DurableStore 视图另运行 storetest 的租约用例，适用于以带过期时间的租约实现 Lock 的后端。
	Leases *Leases
}

// Leases 描述租约后端，钩子用于在测试中制造续约中断与进程崩溃。
type Leases struct {
	// TTL 是实例配置的租约时长。
	TTL time.Duration
	// Expire 使 s 持有的会话 id 上名为 name 的锁的租约立即过期，并停止 s 对它的续约；s 仍以为自己持有该锁。
	Expire func(t *testing.T, s session.Store, id, name string)
	// Crash 使 s 停止全部续约且不释放任何租约，模拟进程崩溃。
	Crash func(t *testing.T, s session.Store)
}

// durableSession 是 DurableStore 视图所在的会话。
const durableSession = "run"

// Run 对 newPair 返回的两个实例运行全部用例；每个用例调用一次 newPair，两个实例须共享同一后端且初始为空。
func Run(t *testing.T, newPair func(t *testing.T) (a, b session.Store), opts Options) {
	cases := []struct {
		name string
		run  func(*testing.T, session.Store, session.Store)
	}{
		{"不存在返回ErrNotFound", testNotFound},
		{"追加按期望序号比较并交换", testAppendCAS},
		{"并发追加只有一个成功", testConcurrentAppend},
		{"事件原样往返", testRoundTrip},
		{"按序号读取", testReadAfter},
		{"分叉", testFork},
		{"按字节序列出", testList},
		{"拒绝空会话ID", testEmptyID},
		{"锁", testLock},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, b := newPair(t)
			require.NotNil(t, a)
			require.NotNil(t, b)
			c.run(t, a, b)
		})
	}
	t.Run("DurableStore", func(t *testing.T) {
		// 租约钩子收到的是 DurableStore 视图，按创建时的对应关系找回底层实例。
		var mu sync.Mutex
		under := map[durable.Store]session.Store{}
		var leases *storetest.Leases
		if l := opts.Leases; l != nil {
			require.NotNil(t, l.Expire)
			require.NotNil(t, l.Crash)
			find := func(t *testing.T, s durable.Store) session.Store {
				mu.Lock()
				defer mu.Unlock()
				st, ok := under[s]
				require.True(t, ok, "租约钩子收到未知的实例")
				return st
			}
			leases = &storetest.Leases{
				TTL: l.TTL,
				Expire: func(t *testing.T, s durable.Store, id string) {
					l.Expire(t, find(t, s), durableSession, session.DurableLockName(id))
				},
				Crash: func(t *testing.T, s durable.Store) { l.Crash(t, find(t, s)) },
			}
		}
		storetest.Run(t, func(t *testing.T) (durable.Store, durable.Store) {
			a, b := newPair(t)
			da, db := session.DurableStore(a, durableSession), session.DurableStore(b, durableSession)
			mu.Lock()
			under[da], under[db] = a, b
			mu.Unlock()
			return da, db
		}, storetest.Options{Leases: leases})
	})
}

func user(t *testing.T, text string) session.Event {
	t.Helper()
	ev, err := session.NewEvent(session.TypeUserMessage, session.MessageData{Message: llm.UserMessage(text)})
	require.NoError(t, err)
	return ev
}

func read(t *testing.T, s session.Store, id string, after int64) []session.Event {
	t.Helper()
	events, err := s.Read(context.Background(), id, after)
	require.NoError(t, err)
	return events
}

func seqs(events []session.Event) []int64 {
	out := make([]int64, len(events))
	for i, ev := range events {
		out[i] = ev.Seq
	}
	return out
}

func testNotFound(t *testing.T, a, _ session.Store) {
	_, err := a.Read(context.Background(), "s", 0)
	require.ErrorIs(t, err, session.ErrNotFound)
}

func testAppendCAS(t *testing.T, a, b session.Store) {
	ctx := context.Background()
	_, err := a.Append(ctx, "s", 1, user(t, "不存在的会话"))
	require.ErrorIs(t, err, session.ErrConflict, "会话不存在时期望序号须为 0")
	last, err := a.Append(ctx, "s", 0, user(t, "一"), user(t, "二"))
	require.NoError(t, err)
	require.EqualValues(t, 2, last)
	_, err = b.Append(ctx, "s", 0, user(t, "冲突"))
	require.ErrorIs(t, err, session.ErrConflict, "过期的期望序号被拒")
	_, err = b.Append(ctx, "s", 3, user(t, "超前"))
	require.ErrorIs(t, err, session.ErrConflict)
	last, err = b.Append(ctx, "s", 2, user(t, "三"))
	require.NoError(t, err)
	require.EqualValues(t, 3, last)
	require.Equal(t, []int64{1, 2, 3}, seqs(read(t, a, "s", 0)), "a 读到 b 的追加，冲突的追加没有留下事件")
}

// 两个实例以同一期望序号并发追加，每一轮恰有一个成功，序号连续无重复。
func testConcurrentAppend(t *testing.T, a, b session.Store) {
	ctx := context.Background()
	const rounds, writers = 10, 4
	var expected int64
	for round := range rounds {
		var wg sync.WaitGroup
		lasts, errs := make([]int64, writers), make([]error, writers)
		for w := range writers {
			s := a
			if w%2 == 1 {
				s = b
			}
			batch := []session.Event{user(t, fmt.Sprintf("%d-%d", round, w)), user(t, "批内第二条")}
			wg.Add(1)
			go func() {
				defer wg.Done()
				lasts[w], errs[w] = s.Append(ctx, "s", expected, batch...)
			}()
		}
		wg.Wait()
		var won []int64
		for w, err := range errs {
			if err == nil {
				won = append(won, lasts[w])
				continue
			}
			require.ErrorIs(t, err, session.ErrConflict)
		}
		require.Equal(t, []int64{expected + 2}, won, "第 %d 轮", round)
		expected += 2
	}
	events := read(t, b, "s", 0)
	require.Len(t, events, rounds*2)
	for i, ev := range events {
		require.EqualValues(t, i+1, ev.Seq)
	}
}

func testRoundTrip(t *testing.T, a, b session.Store) {
	at := time.Date(2026, 10, 31, 8, 30, 1, 123456789, time.UTC)
	tool := llm.Message{Role: "tool", ToolCallID: "c1", Name: "lookup", Content: "{\"k\": [1, 2.50, \"\\u00e9\"]}\n"}
	msg, err := session.NewEvent(session.TypeToolResult, session.MessageData{Message: tool, Tool: &session.ToolMeta{Name: "lookup", Success: true}})
	require.NoError(t, err)
	msg.Time = at
	rewrite, err := session.NewEvent(session.TypeHistoryRewrite, session.RewriteData{Messages: []session.MessageData{{Message: llm.UserMessage("摘要")}}})
	require.NoError(t, err)
	rewrite.Surface = &session.Surface{Start: 1, End: 1}
	// Data 的键序与数字写法不变（空白可被压缩）：以 jsonb 等规范化格式保存的后端不满足。
	raw := session.Event{Type: "custom/raw", Data: json.RawMessage(`{"b": 1.50, "a":[ ]}`)}
	_, err = a.Append(context.Background(), "s", 0, msg, rewrite, raw)
	require.NoError(t, err)

	got := read(t, b, "s", 0)
	require.Len(t, got, 3)
	require.True(t, got[0].Time.Equal(at), "调用方给出的时间原样保存")
	require.False(t, got[1].Time.IsZero(), "未给出时间时由 Store 补全")
	msg.Seq, rewrite.Seq, raw.Seq = 1, 2, 3
	for i, want := range []session.Event{msg, rewrite, raw} {
		require.Equal(t, want.Seq, got[i].Seq)
		require.Equal(t, want.Type, got[i].Type)
		require.Equal(t, compact(t, want.Data), compact(t, got[i].Data))
		require.Equal(t, want.Surface, got[i].Surface)
	}
	derived, err := session.DeriveMessages(got)
	require.NoError(t, err)
	require.Equal(t, []llm.Message{llm.UserMessage("摘要")}, derived)
}

func compact(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, json.Compact(&buf, raw))
	return buf.String()
}

func testReadAfter(t *testing.T, a, b session.Store) {
	ctx := context.Background()
	_, err := a.Append(ctx, "s", 0, user(t, "一"), user(t, "二"), user(t, "三"))
	require.NoError(t, err)
	require.Equal(t, []int64{2, 3}, seqs(read(t, b, "s", 1)))
	require.Empty(t, read(t, b, "s", 3))
	require.Empty(t, read(t, b, "s", 9))
	_, err = b.Append(ctx, "s", 3, user(t, "四"))
	require.NoError(t, err)
	require.Equal(t, []int64{4}, seqs(read(t, a, "s", 3)), "增量读取看到其他实例的追加")
}

func testFork(t *testing.T, a, b session.Store) {
	ctx := context.Background()
	_, err := a.Append(ctx, "s", 0, user(t, "一"), user(t, "二"), user(t, "三"))
	require.NoError(t, err)
	require.NoError(t, a.Fork(ctx, "s", "f", 2))
	require.ErrorIs(t, b.Fork(ctx, "s", "f", 0), session.ErrExists)
	require.ErrorIs(t, b.Fork(ctx, "missing", "g", 0), session.ErrNotFound)
	_, err = b.Read(ctx, "g", 0)
	require.ErrorIs(t, err, session.ErrNotFound, "失败的分叉不创建会话")

	forked := read(t, b, "f", 0)
	require.Equal(t, []int64{1, 2}, seqs(forked))
	require.Equal(t, read(t, a, "s", 0)[:2], forked, "分叉原样复制事件")
	_, err = b.Append(ctx, "f", 3, user(t, "超前"))
	require.ErrorIs(t, err, session.ErrConflict, "分叉的最后序号是复制的最后一个事件")
	_, err = b.Append(ctx, "f", 2, user(t, "分支"))
	require.NoError(t, err)
	require.Len(t, read(t, a, "s", 0), 3, "分叉不影响来源")

	require.NoError(t, b.Fork(ctx, "s", "all", 0))
	require.Equal(t, []int64{1, 2, 3}, seqs(read(t, a, "all", 0)), "upTo 为 0 时复制全部")
	require.NoError(t, b.Fork(ctx, "s", "beyond", 99))
	_, err = a.Append(ctx, "beyond", 3, user(t, "四"))
	require.NoError(t, err)
}

func testList(t *testing.T, a, b session.Store) {
	ctx := context.Background()
	ids, err := a.List(ctx)
	require.NoError(t, err)
	require.Empty(t, ids)
	for _, id := range []string{"b", "B", "a/1", "a", "会话", "_x"} {
		_, err := a.Append(ctx, id, 0, user(t, id))
		require.NoError(t, err)
	}
	ids, err = b.List(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"B", "_x", "a", "a/1", "b", "会话"}, ids, "与 sort.Strings 相同的字节序")
}

func testEmptyID(t *testing.T, a, _ session.Store) {
	ctx := context.Background()
	_, err := a.Append(ctx, "", 0, user(t, "一"))
	require.Error(t, err)
	_, err = a.Append(ctx, "s", 0, user(t, "一"))
	require.NoError(t, err)
	require.Error(t, a.Fork(ctx, "s", "", 0))
}

func lock(t *testing.T, s session.Store, id, name string) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlock, err := s.Lock(ctx, id, name)
	require.NoError(t, err)
	return func() { require.NoError(t, unlock()) }
}

func requireBlocked(t *testing.T, s session.Store, id, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	unlock, err := s.Lock(ctx, id, name)
	if err == nil {
		_ = unlock()
	}
	require.ErrorIs(t, err, durable.ErrLockUnavailable)
	require.ErrorIs(t, err, context.DeadlineExceeded, "须保留 ctx 错误")
}

func testLock(t *testing.T, a, b session.Store) {
	unlock := lock(t, a, "s", "x")
	requireBlocked(t, b, "s", "x")
	requireBlocked(t, a, "s", "x")
	lock(t, b, "s", "y")()
	lock(t, b, "t", "x")()
	unlock()
	lock(t, b, "s", "x")()
}
