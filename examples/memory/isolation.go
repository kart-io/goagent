package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/testing/mocks"
)

// step 是场景中的一个边界用例；runSteps 逐个执行并输出通过标记，失败时带上用例名返回。
type step struct {
	name string
	run  func() error
}

func runSteps(w io.Writer, steps ...step) error {
	for _, s := range steps {
		if err := s.run(); err != nil {
			return fmt.Errorf("%s：%w", s.name, err)
		}
		_, _ = fmt.Fprintf(w, "  - %s：通过\n", s.name)
	}
	return nil
}

var (
	alice = []string{"t1", "alice"}
	bob   = []string{"t1", "bob"}
)

func isolation(ctx context.Context, w io.Writer, root string) error {
	return runSteps(w,
		step{"条目1 未绑定命名空间时拒绝读写", func() error { return unboundRejected(ctx, root) }},
		step{"条目2 命名空间隔离", func() error { return namespaceIsolation(ctx, root) }},
		step{"条目4 Forget 后不召回、进行中的轮次不能让它复活", func() error { return forgetNoResurrect(ctx, w, root) }},
		step{"条目5 Purge 整个命名空间", func() error { return purgeNamespace(ctx, w, root) }},
		step{"条目7 Snapshot/Hydrate 的命名空间绑定与删除优先", func() error { return snapshotHydrate(ctx, root) }},
	)
}

func unboundRejected(ctx context.Context, root string) error {
	e, err := newEnv(root, "unbound", memory.Config{})
	if err != nil {
		return err
	}
	m := e.mem
	ops := []struct {
		name string
		do   func(context.Context) error
	}{
		{"Remember", func(c context.Context) error {
			_, err := m.Remember(c, memory.Record{Kind: memory.KindFact, Content: "未绑定时写入的内容"})
			return err
		}},
		{"Recall", func(c context.Context) error { _, err := m.Recall(c, memory.Query{Text: "内容"}); return err }},
		{"RecallBlock", func(c context.Context) error { _, err := m.RecallBlock(c, "内容"); return err }},
		{"Get", func(c context.Context) error { _, err := m.Get(c, "any"); return err }},
		{"Forget", func(c context.Context) error { return m.Forget(c, "any") }},
		{"Purge", func(c context.Context) error { return m.Purge(c) }},
		{"Pinned", func(c context.Context) error { _, err := m.Pinned(c); return err }},
		{"Snapshot", func(c context.Context) error { _, err := m.Snapshot(c, "内容"); return err }},
	}
	toolArgs := map[string]map[string]any{
		"memory_search": {"query": "内容"},
		"memory_save":   {"content": "未绑定时工具写入的内容"},
		"memory_forget": {"id": "any"},
	}
	for _, t := range m.Tools() {
		args, ok := toolArgs[t.Name()]
		if !ok {
			return fmt.Errorf("出现未覆盖的记忆工具 %s", t.Name())
		}
		ops = append(ops, struct {
			name string
			do   func(context.Context) error
		}{t.Name(), func(c context.Context) error {
			out, err := t.Invoke(c, &interfaces.ToolInput{Args: args})
			if err == nil && out != nil && !out.Success {
				err = errors.New(out.Error)
			}
			return err
		}})
	}
	var accepted []string
	for _, op := range ops {
		if op.do(ctx) == nil {
			accepted = append(accepted, op.name)
		}
	}
	all, err := m.Recall(memory.WithNamespace(ctx, alice...), memory.Query{})
	if err != nil {
		return err
	}
	return verify(
		check{"未绑定时仍被接受的操作", accepted, []string(nil)},
		check{"绑定后看到的记录数（未绑定的写入不应落到任何命名空间）", len(all), 0},
		check{"覆盖的操作数（8 个方法 + 3 个工具）", len(ops), 11},
	)
}

func namespaceIsolation(ctx context.Context, root string) error {
	e, err := newEnv(root, "isolation", memory.Config{})
	if err != nil {
		return err
	}
	// 四个命名空间写入同一个 ID：同 ID 在不同命名空间是互不相干的记录；alice 与 alice2 验证前缀不串。
	owners := []struct {
		ns      []string
		content string
	}{
		{alice, "用户最喜欢的颜色是红色"},
		{bob, "用户最喜欢的颜色是蓝色"},
		{[]string{"t2", "alice"}, "用户最喜欢的颜色是绿色"},
		{[]string{"t1", "alice2"}, "用户最喜欢的颜色是紫色"},
	}
	const id = "favorite-color"
	for _, o := range owners {
		if _, err := e.mem.Remember(memory.WithNamespace(ctx, o.ns...), memory.Record{ID: id, Kind: memory.KindFact, Content: o.content, Importance: 0.5}); err != nil {
			return fmt.Errorf("%v 写入：%w", o.ns, err)
		}
	}
	var checks []check
	for _, reader := range owners {
		rctx := memory.WithNamespace(ctx, reader.ns...)
		got, err := e.mem.Get(rctx, id)
		if err != nil {
			return err
		}
		checks = append(checks, check{fmt.Sprintf("%v 按 ID 读到的正文", reader.ns), got.Content, reader.content})
		for _, other := range owners {
			rs, err := e.mem.Recall(rctx, memory.Query{Text: other.content, K: 10})
			if err != nil {
				return err
			}
			checks = append(checks, check{fmt.Sprintf("%v 以 %v 的正文召回", reader.ns, other.ns), contents(rs), []string{reader.content}})
		}
	}
	return verify(checks...)
}

func forgetNoResurrect(ctx context.Context, w io.Writer, root string) error {
	// 抽取模型如果被调用，会把被遗忘的车牌号原样抽回去；严格模式下 Hits(0)==0 证明根本没调用。
	ext := mocks.NewScriptedClient(extractReply("会复述车牌号的抽取", "用户的车牌号是沪A12345"))
	e, err := newEnv(root, "forget", memory.Config{Model: ext})
	if err != nil {
		return err
	}
	actx := memory.WithNamespace(ctx, alice...)
	r, err := e.mem.Remember(actx, memory.Record{Kind: memory.KindFact, Content: "用户的车牌号是沪A12345", Importance: 0.9})
	if err != nil {
		return err
	}
	before, err := e.mem.Recall(actx, memory.Query{Text: "车牌号"})
	if err != nil {
		return err
	}
	// 主模型回答期间用户在另一个入口要求遗忘：本轮已在遗忘之前开始，它的抽取任务必须被丢弃。
	chat := mocks.NewScriptedClient(mocks.ScriptedRule{Name: "回答期间遗忘", Respond: func(context.Context, *llm.CompletionRequest) (mocks.ScriptedReply, error) {
		return mocks.ScriptedReply{Text: "好的，车牌号是沪A12345"}, e.mem.Forget(actx, r.ID)
	}})
	a, err := e.agent(chat, nil)
	if err != nil {
		return err
	}
	dropped := e.mem.DroppedJobs()
	key, _, err := e.turn(ctx, a, alice, "s-forget", "我的车牌号是沪A12345，记一下")
	if err != nil {
		return err
	}
	processed, err := e.mem.ProcessPending(ctx)
	if err != nil {
		return err
	}
	got, err := e.mem.Get(actx, r.ID)
	if err != nil {
		return err
	}
	after, err := e.mem.Recall(actx, memory.Query{Text: "车牌号"})
	if err != nil {
		return err
	}
	block, err := e.mem.RecallBlock(actx, "车牌号")
	if err != nil {
		return err
	}
	job, ok, err := e.mem.Job(actx, key)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "    遗忘后：InvalidReason=%s，正文=%q，任务 %s 状态=%s，丢弃计数 +%d\n", got.InvalidReason, got.Content, key, job.Status, e.mem.DroppedJobs()-dropped)
	return verify(
		check{"遗忘前召回到车牌号", contents(before), []string{"用户的车牌号是沪A12345"}},
		check{"InvalidReason", got.InvalidReason, memory.ReasonForgotten},
		check{"InvalidAt 已写入", got.InvalidAt.IsZero(), false},
		check{"正文已清空", got.Content, ""},
		check{"遗忘后召回", len(after), 0},
		check{"遗忘后召回块", block, ""},
		check{"任务存在", ok, true},
		check{"任务状态", job.Status, memory.JobDropped},
		check{"DroppedJobs 增量", e.mem.DroppedJobs() - dropped, int64(1)},
		check{"抽取模型被调用次数", ext.Calls(), 0},
		// ProcessPending 把在处理时丢弃的任务也计入返回值。
		check{"ProcessPending 返回的任务数（含丢弃）", processed, 1},
	)
}

func purgeNamespace(ctx context.Context, w io.Writer, root string) error {
	ext := mocks.NewScriptedClient(extractReply("不应被调用的抽取", "用户在上海工作"))
	e, err := newEnv(root, "purge", memory.Config{Model: ext, RollbackWindow: 24 * time.Hour})
	if err != nil {
		return err
	}
	actx, bctx := memory.WithNamespace(ctx, alice...), memory.WithNamespace(ctx, bob...)
	for _, r := range []struct {
		ctx context.Context
		rec memory.Record
	}{
		{actx, memory.Record{Kind: memory.KindFact, Content: "用户叫爱丽丝", Pinned: true, Importance: 1}},
		{actx, memory.Record{Kind: memory.KindFact, Content: "用户在上海工作", Importance: 0.7}},
		{bctx, memory.Record{ID: "bob-city", Kind: memory.KindFact, Content: "用户在深圳工作", Importance: 0.7}},
	} {
		if _, err := e.mem.Remember(r.ctx, r.rec); err != nil {
			return err
		}
	}
	// 第 1 轮正常结束、任务待处理；第 2 轮回答期间执行 Purge，本轮的任务登记在旧作用域上。
	chat := mocks.NewScriptedClient(
		mocks.TextReply("第1轮", "记住了"),
		mocks.ScriptedRule{Name: "回答期间 Purge", Respond: func(context.Context, *llm.CompletionRequest) (mocks.ScriptedReply, error) {
			return mocks.ScriptedReply{Text: "好的"}, e.mem.Purge(actx)
		}},
	)
	a, err := e.agent(chat, nil)
	if err != nil {
		return err
	}
	key1, _, err := e.turn(ctx, a, alice, "s-purge", "我在上海工作")
	if err != nil {
		return err
	}
	pending, _, err := e.mem.Job(actx, key1)
	if err != nil {
		return err
	}
	dropped := e.mem.DroppedJobs()
	if _, _, err = e.turn(ctx, a, alice, "s-purge", "请删除我的全部数据"); err != nil {
		return err
	}
	processed, err := e.mem.ProcessPending(ctx)
	if err != nil {
		return err
	}
	if err := e.mem.Maintain(ctx); err != nil {
		return err
	}
	all, err := e.mem.Recall(actx, memory.Query{})
	if err != nil {
		return err
	}
	pinned, err := e.mem.Pinned(actx)
	if err != nil {
		return err
	}
	_, job1Found, err := e.mem.Job(actx, key1)
	if err != nil {
		return err
	}
	dl, err := e.mem.Deletions(actx)
	if err != nil {
		return err
	}
	bobRec, err := e.mem.Get(bctx, "bob-city")
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "    Purge 后：记录 %d 条，核心记忆 %d 条，删除日志 Purges=%d，丢弃计数 +%d，bob 仍可读：%q\n", len(all), len(pinned), len(dl.Purges), e.mem.DroppedJobs()-dropped, bobRec.Content)
	return verify(
		check{"Purge 前第 1 轮任务待处理", pending.Status, memory.JobPending},
		check{"Purge 后记录", len(all), 0},
		check{"Purge 后核心记忆", len(pinned), 0},
		check{"旧作用域的第 1 轮任务仍可查到", job1Found, false},
		check{"Purge 之前开始的第 2 轮任务被丢弃（DroppedJobs 增量）", e.mem.DroppedJobs() - dropped, int64(1)},
		check{"ProcessPending 处理的任务数", processed, 0},
		check{"抽取模型被调用次数", ext.Calls(), 0},
		check{"删除日志中的 Purge 条目", len(dl.Purges), 1},
		check{"其他命名空间不受影响", bobRec.Content, "用户在深圳工作"},
	)
}

func snapshotHydrate(ctx context.Context, root string) error {
	e, err := newEnv(root, "snapshot", memory.Config{})
	if err != nil {
		return err
	}
	actx := memory.WithNamespace(ctx, alice...)
	keep, err := e.mem.Remember(actx, memory.Record{Kind: memory.KindFact, Content: "用户对花生过敏", Importance: 0.9})
	if err != nil {
		return err
	}
	gone, err := e.mem.Remember(actx, memory.Record{Kind: memory.KindFact, Content: "用户对花粉过敏", Importance: 0.9})
	if err != nil {
		return err
	}
	snap, err := e.mem.Snapshot(actx, "过敏")
	if err != nil {
		return err
	}
	// 恢复时宿主把同一轮绑到了另一个用户：必须拒绝，且不得返回任何正文。
	crossed, used, crossErr := e.mem.Hydrate(memory.WithNamespace(ctx, bob...), snap, snap.Refs)
	if err := e.mem.Forget(actx, gone.ID); err != nil {
		return err
	}
	after, kept, err := e.mem.Hydrate(actx, snap, snap.Refs)
	if err != nil {
		return err
	}
	return verify(
		check{"快照引用数", len(snap.Refs), 2},
		check{"换命名空间：errors.Is(ErrMemoryNamespaceChanged)", errors.Is(crossErr, agentErrors.ErrMemoryNamespaceChanged), true},
		check{"换命名空间：错误码", agentErrors.GetCode(crossErr), agentErrors.CodeConflict},
		check{"换命名空间：召回正文", crossed.Text, ""},
		check{"换命名空间：采用的引用", len(used), 0},
		check{"遗忘后还原：只采用未遗忘的引用", refIDs(kept), []string{keep.ID}},
		check{"遗忘后还原：块中不含被遗忘的正文", strings.Contains(after.Text, "花粉"), false},
		check{"遗忘后还原：块中仍有其余正文", strings.Contains(after.Text, "花生"), true},
	)
}

func refIDs(refs []memory.Ref) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.ID
	}
	return out
}
