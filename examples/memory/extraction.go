package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/testing/mocks"
)

// 抽取、整理与教训三类请求的系统提示词开头，路由据此区分 Worker 发出的请求。
const (
	markExtract   = "你从一轮对话中抽取"
	markReconcile = "你维护长期记忆"
	markLesson    = "这一轮执行失败或被中止"
)

func extraction(ctx context.Context, w io.Writer, root string) error {
	return runSteps(w,
		step{"跨会话召回，召回正文不进会话日志", func() error { return crossSessionRecall(ctx, w, root) }},
		step{"重复登记幂等，失败后重试同一任务", func() error { return enqueueIdempotent(ctx, w, root) }},
		step{"新事实取代旧事实，重复事实不写入", func() error { return supersede(ctx, w, root) }},
		step{"抽取失败、非法候选与整理降级", func() error { return extractionFailures(ctx, w, root) }},
		step{"中止轮次只写教训，召回块与记忆工具不被抽取", func() error { return abortedAndToolSkipping(ctx, w, root) }},
	)
}

func crossSessionRecall(ctx context.Context, w io.Writer, root string) error {
	ext := mocks.NewScriptedClient(extractReply("会话A抽取", "用户住在上海"))
	e, err := newEnv(root, "cross-session", memory.Config{Model: ext})
	if err != nil {
		return err
	}
	// 会话 B 的请求必须带着以 RecallPrefix 开头、含会话 A 事实的召回块，否则严格脚本直接报错。
	chat := mocks.NewScriptedClient(
		mocks.TextReply("会话A", "好的，记住了"),
		mocks.ScriptedRule{Name: "会话B", Match: mocks.RequestContains(memory.RecallPrefix, "用户住在上海"), Reply: mocks.ScriptedReply{Text: "你住在上海"}},
	)
	a, err := e.agent(chat, nil)
	if err != nil {
		return err
	}
	if _, _, err := e.turn(ctx, a, alice, "sA", "我住在上海"); err != nil {
		return err
	}
	if _, err := e.mem.ProcessPending(ctx); err != nil {
		return err
	}
	_, out, err := e.turn(ctx, a, alice, "sB", "我住在哪个城市？")
	if err != nil {
		return err
	}
	evs, err := e.sessions.Read(ctx, "sB", 0)
	if err != nil {
		return err
	}
	var leaked []string
	for _, ev := range evs {
		if strings.Contains(string(ev.Data), strings.TrimSpace(memory.RecallPrefix)) || strings.Contains(string(ev.Data), "用户住在上海") {
			leaked = append(leaked, ev.Type)
		}
	}
	got, err := e.mem.Retrieve(memory.WithNamespace(ctx, alice...), "上海")
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "    会话 B 的回答：%v；召回依据的会话：%v\n", out.Result, got.Sessions())
	return verify(
		check{"会话 B 的回答", out.Result, "你住在上海"},
		check{"会话 B 日志中含召回正文的事件", leaked, []string(nil)},
		check{"召回依据的会话", got.Sessions(), []string{"sA"}},
	)
}

func enqueueIdempotent(ctx context.Context, w io.Writer, root string) error {
	// 第一次抽取失败，第二次成功；之后若再有请求，严格脚本会报错。
	ext := mocks.NewScriptedClient(
		mocks.ScriptedRule{Name: "首次失败", Respond: func(context.Context, *llm.CompletionRequest) (mocks.ScriptedReply, error) {
			return mocks.ScriptedReply{}, errors.New("模拟抽取超时")
		}},
		extractReply("重试成功", "用户每天早上跑步"),
	)
	e, err := newEnv(root, "idempotent", memory.Config{Model: ext, JobAttempts: 3})
	if err != nil {
		return err
	}
	a, err := e.agent(mocks.NewScriptedClient(mocks.TextReply("回答", "坚持得不错")), nil)
	if err != nil {
		return err
	}
	key, _, err := e.turn(ctx, a, alice, "s-idem", "我每天早上跑步")
	if err != nil {
		return err
	}
	actx := memory.WithNamespace(ctx, alice...)
	job, _, err := e.mem.Job(actx, key)
	if err != nil {
		return err
	}
	// 模拟轮末登记被重放：同一 Key 再登记两次。
	for range 2 {
		if err := e.mem.Enqueue(actx, job); err != nil {
			return err
		}
	}
	var processed []int
	var states []memory.JobStatus
	for range 3 {
		n, err := e.mem.ProcessPending(ctx)
		if err != nil {
			return err
		}
		j, _, err := e.mem.Job(actx, key)
		if err != nil {
			return err
		}
		processed, states = append(processed, n), append(states, j.Status)
		if n == 1 && j.Status == memory.JobPending {
			_, _ = fmt.Fprintf(w, "    第 1 次处理失败：Attempts=%d，Error=%q\n", j.Attempts, j.Error)
		}
	}
	// 完成之后再次登记（例如回放已完成的轮次）也不产生新任务。
	if err := e.mem.Enqueue(actx, job); err != nil {
		return err
	}
	again, err := e.mem.ProcessPending(ctx)
	if err != nil {
		return err
	}
	done, _, err := e.mem.Job(actx, key)
	if err != nil {
		return err
	}
	all, err := e.mem.Recall(actx, memory.Query{})
	if err != nil {
		return err
	}
	return verify(
		check{"三次 ProcessPending 的处理数", processed, []int{1, 1, 0}},
		check{"三次处理后的任务状态", states, []memory.JobStatus{memory.JobPending, memory.JobDone, memory.JobDone}},
		check{"完成后再登记的处理数", again, 0},
		check{"抽取模型调用次数（失败 1 + 成功 1）", ext.Calls(), 2},
		check{"完成统计 Stats.Records", done.Stats.Records, 1},
		check{"有效记录", contents(all), []string{"用户每天早上跑步"}},
	)
}

func supersede(ctx context.Context, w io.Writer, root string) error {
	var shanghaiID, beijingID string
	reconcile := func(op func() string) mocks.ScriptedRule {
		return mocks.ScriptedRule{Name: "整理", Match: mocks.RequestContains(markReconcile), Respond: func(context.Context, *llm.CompletionRequest) (mocks.ScriptedReply, error) {
			return mocks.ScriptedReply{Text: op()}, nil
		}}
	}
	ext := mocks.NewScriptedClient(
		extractReply("第1轮抽取", "用户住在上海"),
		extractReply("第2轮抽取", "用户住在北京"),
		reconcile(func() string {
			return fmt.Sprintf(`{"op":"update","target":%q,"content":"用户从上海搬到了北京，现在住在北京"}`, shanghaiID)
		}),
		extractReply("第3轮抽取", "用户现在住在北京"),
		reconcile(func() string { return fmt.Sprintf(`{"op":"noop","target":%q}`, beijingID) }),
	)
	e, err := newEnv(root, "supersede", memory.Config{Model: ext})
	if err != nil {
		return err
	}
	a, err := e.agent(mocks.NewScriptedClient(mocks.TextReply("1", "好的"), mocks.TextReply("2", "好的"), mocks.TextReply("3", "好的")), nil)
	if err != nil {
		return err
	}
	actx := memory.WithNamespace(ctx, alice...)
	valid := func() ([]memory.Scored, error) { return e.mem.Recall(actx, memory.Query{}) }
	inputs := []string{"我住在上海", "我搬到北京了", "我现在还在北京"}
	var keys []string
	for i, in := range inputs {
		key, _, err := e.turn(ctx, a, alice, "s-move", in)
		if err != nil {
			return err
		}
		if _, err := e.mem.ProcessPending(ctx); err != nil {
			return err
		}
		keys = append(keys, key)
		rs, err := valid()
		if err != nil {
			return err
		}
		if len(rs) != 1 {
			return fmt.Errorf("第 %d 轮后应只有 1 条有效记录，实际 %v", i+1, contents(rs))
		}
		if i == 0 {
			shanghaiID = rs[0].ID
		} else {
			beijingID = rs[0].ID
		}
	}
	old, err := e.mem.Get(actx, shanghaiID)
	if err != nil {
		return err
	}
	cur, err := e.mem.Get(actx, beijingID)
	if err != nil {
		return err
	}
	noop, _, err := e.mem.Job(actx, keys[2])
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "    旧记录 %s：%s → %s；新记录来源 %d 个\n", shortID(old.ID), old.InvalidReason, shortID(old.SupersededBy), len(cur.Sources))
	return verify(
		check{"旧记录失效原因", old.InvalidReason, memory.ReasonSuperseded},
		check{"旧记录 SupersededBy", old.SupersededBy, beijingID},
		check{"新记录正文", cur.Content, "用户从上海搬到了北京，现在住在北京"},
		check{"新记录继承旧记录的来源", sourceJobs(cur.Sources), []string{keys[0], keys[1]}},
		check{"新记录 MergedFrom 含旧记录", slices.ContainsFunc(cur.MergedFrom, func(r memory.RecordRef) bool { return r.ID == shanghaiID }), true},
		check{"noop 任务写入的记录数", noop.Stats.Records, 0},
		check{"脚本全部用完", ext.Remaining(), 0},
	)
}

func sourceJobs(src []memory.Source) []string {
	out := make([]string, len(src))
	for i, s := range src {
		out[i] = s.Job
	}
	slices.Sort(out)
	return out
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func extractionFailures(ctx context.Context, w io.Writer, root string) error {
	var e *env
	actx := memory.WithNamespace(ctx, alice...)
	// 整理请求发出后、写回之前，"home" 被并发修改：写回时复核不成立，决定降级为新增并计入 Conflicts。
	concurrentEdit := func() error {
		home, err := e.mem.Get(actx, "home")
		if err != nil {
			return err
		}
		content := "用户住在杭州"
		_, err = e.mem.Update(actx, "home", home.Revision, memory.Patch{Content: &content})
		return err
	}
	// 任务处理顺序不固定，按请求内容路由；整理请求的标记放在前面，避免被抽取标记抢先匹配。
	ext := routedClient(12,
		route{marker: "新信息：用户养了一只猫", reply: `{"op":"add"}`},
		route{marker: "新信息：用户住在广州", reply: `{"op":"update","target":"home","content":"用户住在广州"}`, before: concurrentEdit},
		route{marker: "新信息：用户把猫送给了朋友", reply: `{"op":"invalidate","target":"no-such-record"}`},
		route{marker: "我要订去成都的票", err: errors.New("模拟模型持续不可用")},
		route{marker: "随便聊聊", reply: "这不是 JSON"},
		route{marker: "我养了一只猫", reply: `{"memories":[{"kind":"bogus","content":"类别非法"},{"kind":"fact","content":"","importance":0.5},{"kind":"fact","content":"用户养了一只猫","importance":0.6}]}`},
		route{marker: "我在广州上班", reply: extractJSON("用户住在广州")},
		route{marker: "我把猫送给朋友了", reply: extractJSON("用户把猫送给了朋友")},
	)
	var err error
	if e, err = newEnv(root, "failures", memory.Config{Model: ext, JobAttempts: 2}); err != nil {
		return err
	}
	if _, err := e.mem.Remember(actx, memory.Record{ID: "home", Kind: memory.KindFact, Content: "用户住在上海", Importance: 0.8}); err != nil {
		return err
	}
	a, err := e.agent(routedClient(5, route{reply: "好的"}), nil)
	if err != nil {
		return err
	}
	// 非法 JSON 时 Worker 在同一次尝试内要求模型重写一次，所以每次尝试有两次请求。
	cases := []struct {
		session, input string
		status         memory.JobStatus
		records        int
		conflicts      int
		invalidTargets int
	}{
		{"s-down", "我要订去成都的票", memory.JobFailed, 0, 0, 0},
		{"s-not-json", "随便聊聊", memory.JobFailed, 0, 0, 0},
		{"s-bad-candidates", "我养了一只猫", memory.JobDone, 1, 0, 0},
		{"s-concurrent-edit", "我在广州上班", memory.JobDone, 1, 1, 0},
		{"s-unknown-target", "我把猫送给朋友了", memory.JobDone, 1, 0, 1},
	}
	keys := make([]string, len(cases))
	for i, c := range cases {
		if keys[i], _, err = e.turn(ctx, a, alice, c.session, c.input); err != nil {
			return err
		}
	}
	for range 2 {
		if _, err := e.mem.ProcessPending(ctx); err != nil {
			return err
		}
	}
	var checks []check
	for i, c := range cases {
		j, _, err := e.mem.Job(actx, keys[i])
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(w, "    %s：%s，Attempts=%d，Stats=%+v，Error=%q\n", c.session, j.Status, j.Attempts, j.Stats, j.Error)
		checks = append(checks,
			check{c.session + " 状态", j.Status, c.status},
			check{c.session + " 写入记录", j.Stats.Records, c.records},
			check{c.session + " 复核不成立而降级的整理决定", j.Stats.Conflicts, c.conflicts},
			check{c.session + " target 不存在而降级的整理决定", j.Stats.InvalidTargets, c.invalidTargets},
			check{c.session + " 失败时记录了错误", j.Status == memory.JobFailed && j.Error == "", false},
		)
	}
	all, err := e.mem.Recall(actx, memory.Query{})
	if err != nil {
		return err
	}
	got := contents(all)
	slices.Sort(got)
	return verify(append(checks,
		check{"有效记录（非法候选丢弃，降级与未知目标都按新增）", got, []string{"用户住在广州", "用户住在杭州", "用户养了一只猫", "用户把猫送给了朋友"}},
		check{"模型请求数（失败 2 + 非 JSON 4 + 三个任务各抽取与整理 1 次）", ext.Calls(), 12},
	)...)
}

func abortedAndToolSkipping(ctx context.Context, w io.Writer, root string) error {
	ext := routedClient(2,
		route{marker: markLesson, reply: `{"lesson":"订票前先确认出发日期，避免请求超时被中止","keywords":["订票"],"importance":0.6}`},
		route{marker: markExtract, reply: `{"memories":[]}`},
	)
	e, err := newEnv(root, "aborted", memory.Config{Model: ext})
	if err != nil {
		return err
	}
	actx := memory.WithNamespace(ctx, alice...)
	if _, err := e.mem.Remember(actx, memory.Record{Kind: memory.KindFact, Content: "用户常用的航空公司是东航", Importance: 0.8}); err != nil {
		return err
	}
	abortCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	chat := mocks.NewScriptedClient(
		// 回答过程中宿主取消请求，本轮没有 turn/end，下一轮开始时记为中止。
		mocks.ScriptedRule{Name: "被取消", Respond: func(context.Context, *llm.CompletionRequest) (mocks.ScriptedReply, error) {
			cancel()
			return mocks.ScriptedReply{}, context.Canceled
		}},
		mocks.ToolCallReply("检索记忆", mocks.ToolCall("call-1", "memory_search", `{"query":"航空公司"}`)),
		mocks.TextReply("回答", "查到了，你常坐东航"),
	)
	a, err := e.agent(chat, nil)
	if err != nil {
		return err
	}
	if _, _, err := e.turn(abortCtx, a, alice, "s-abort", "帮我订明天的机票"); err == nil {
		return errors.New("被取消的轮次应返回错误")
	}
	abortedKey, err := e.lastTurnKey(ctx, "s-abort")
	if err != nil {
		return err
	}
	toolKey, _, err := e.turn(ctx, a, alice, "s-abort", "我常用哪家航空公司？")
	if err != nil {
		return err
	}
	if _, err := e.mem.ProcessPending(ctx); err != nil {
		return err
	}
	aborted, _, err := e.mem.Job(actx, abortedKey)
	if err != nil {
		return err
	}
	lessons, err := e.mem.Recall(actx, memory.Query{Kinds: []memory.Kind{memory.KindReflection}})
	if err != nil {
		return err
	}
	// 系统提示词本身提到了召回块的标记，只检查对话记录部分。
	var extractReq string
	for _, r := range ext.Requests() {
		if strings.Contains(requestText(r), markExtract) {
			r.Messages = slices.DeleteFunc(r.Messages, func(m llm.Message) bool { return m.Role == "system" })
			extractReq = requestText(r)
		}
	}
	toolJob, _, err := e.mem.Job(actx, toolKey)
	if err != nil {
		return err
	}
	reqs := chat.Requests()
	if len(reqs) != 3 {
		return fmt.Errorf("主模型应收到 3 次请求（取消、调用工具、回答），实际 %d 次", len(reqs))
	}
	_, _ = fmt.Fprintf(w, "    中止轮次 %s 写入 %d 条教训：%v\n", abortedKey, aborted.Stats.Records, contents(lessons))
	forbidden := []string{strings.TrimSpace(memory.RecallPrefix), "memory_search", "用户常用的航空公司是东航"}
	var found []string
	for _, f := range forbidden {
		if strings.Contains(extractReq, f) {
			found = append(found, f)
		}
	}
	return verify(
		check{"中止轮次的任务状态", aborted.Status, memory.JobDone},
		check{"中止轮次只写一条教训", aborted.Stats.Records, 1},
		check{"教训类别的记录", contents(lessons), []string{"订票前先确认出发日期，避免请求超时被中止"}},
		check{"工具轮次的任务状态", toolJob.Status, memory.JobDone},
		check{"主模型脚本全部用完（工具确实被调用）", chat.Remaining(), 0},
		check{"memory_search 的结果里确有记忆正文", slices.ContainsFunc(reqs[2].Messages, func(m llm.Message) bool {
			return m.Role == "tool" && strings.Contains(m.Content, "用户常用的航空公司是东航")
		}), true},
		check{"抽取请求含本轮用户原话", strings.Contains(extractReq, "我常用哪家航空公司？"), true},
		check{"抽取请求中出现的召回块或记忆工具内容", found, []string(nil)},
	)
}
