package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/cache"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/testing/mocks"
)

var tagKey = []byte(strings.Repeat("k", 32))

// untaggedCache 只暴露 cache.Bytes，隐藏 SimpleCache 的 Tagged 能力，模拟不支持按标签删除的后端。
type untaggedCache struct{ cache.Bytes }

func cacheConsistency(ctx context.Context, w io.Writer, root string) error {
	return runSteps(w,
		step{"ModelCache 的擦除模式在构建时校验", func() error { return eraseBuildRules(ctx, root) }},
		step{"Forget/Rewound/Purge 之后缓存不再命中", func() error { return eraseMatrix(ctx, w, root) }},
		step{"Rewound 之后不再召回，被取代的旧记录恢复", func() error { return rewoundRestores(ctx, w, root) }},
	)
}

func newCache() *cache.SimpleCache {
	return cache.NewSimpleCache(time.Hour, cache.WithoutBackgroundCleanup())
}

func eraseBuildRules(ctx context.Context, root string) error {
	// wantErr 为空表示应能构建；否则是构建错误中应出现的关键字，错误码一律为 CodeInvalidConfig。
	cases := []struct {
		name    string
		mc      builder.ModelCache
		onErase bool
		wantErr string
	}{
		{"EraseRequired 条件齐备", builder.ModelCache{Cache: newCache(), TTL: time.Minute, TagKey: tagKey, Erase: builder.EraseRequired}, true, ""},
		{"EraseRequired 且 TTL 超过 EraseWithin", builder.ModelCache{Cache: newCache(), TTL: time.Hour, TagKey: tagKey, Erase: builder.EraseRequired}, true, "EraseWithin"},
		{"EraseRequired 且缓存不支持 Tagged", builder.ModelCache{Cache: untaggedCache{newCache()}, TTL: time.Minute, TagKey: tagKey, Erase: builder.EraseRequired}, true, "cache.Tagged"},
		{"EraseRequired 且 TagKey 不足 32 字节", builder.ModelCache{Cache: newCache(), TTL: time.Minute, TagKey: []byte("short"), Erase: builder.EraseRequired}, true, "TagKey"},
		{"EraseRequired 且记忆未配置 OnErase", builder.ModelCache{Cache: newCache(), TTL: time.Minute, TagKey: tagKey, Erase: builder.EraseRequired}, false, "OnErase"},
		{"EraseAuto 且记忆未配置 OnErase（静默退回 TTL）", builder.ModelCache{Cache: newCache(), TTL: time.Minute, TagKey: tagKey}, false, ""},
		{"EraseOff 且 TTL 很长", builder.ModelCache{Cache: newCache(), TTL: time.Hour, Erase: builder.EraseOff}, true, ""},
	}
	var checks []check
	for i, c := range cases {
		cfg := memory.Config{}
		if c.onErase {
			cfg.OnErase = c.mc.Eraser()
		}
		e, err := newEnv(root, fmt.Sprintf("erase-build-%d", i), cfg)
		if err != nil {
			return fmt.Errorf("%s：memory.New：%w", c.name, err)
		}
		_, err = e.agent(mocks.NewScriptedClient(), func(b *builder.SimpleAgentBuilder) *builder.SimpleAgentBuilder {
			return b.WithModelCache(c.mc)
		})
		checks = append(checks, configErr(c.name+" 构建", err, c.wantErr))
	}
	short := builder.ModelCache{Cache: newCache(), TTL: time.Minute, TagKey: []byte("short"), Erase: builder.EraseRequired}
	checks = append(checks, configErr("独立 Worker 用 Validate 提前发现 TagKey 过短", short.Validate(), "TagKey"))
	return verify(checks...)
}

// configErr 断言 err 是含关键字 want 的配置错误；want 为空时断言没有错误。
func configErr(what string, err error, want string) check {
	type outcome struct {
		code    agentErrors.ErrorCode
		hasWant bool
	}
	got := outcome{code: agentErrors.GetCode(err), hasWant: err != nil && strings.Contains(err.Error(), want)}
	if want == "" {
		return check{what + "（错误：" + fmt.Sprint(err) + "）", got, outcome{}}
	}
	return check{what + "（错误：" + fmt.Sprint(err) + "）", got, outcome{code: agentErrors.CodeInvalidConfig, hasWant: true}}
}

// eraseOp 是一种删除操作；session 已先被退回到空（重新生成），Rewound 按宿主契约紧跟其后调用。
type eraseOp struct {
	name string
	do   func(context.Context, *env, string) error
}

var eraseOps = []eraseOp{
	{"Forget", func(ctx context.Context, e *env, id string) error { return e.mem.Forget(ctx, id) }},
	{"Rewound", func(ctx context.Context, e *env, _ string) error { return e.mem.Rewound(ctx, "s-cache", 0) }},
	{"Purge", func(ctx context.Context, e *env, _ string) error { return e.mem.Purge(ctx) }},
}

func eraseMatrix(ctx context.Context, w io.Writer, root string) error {
	modes := []struct {
		name    string
		erase   builder.EraseMode
		onErase bool
		// physical 为真时删除后缓存条目被物理清除；否则残留到 TTL，但键含记忆版本，逻辑上不可达。
		physical bool
	}{
		{"EraseAuto+OnErase", builder.EraseAuto, true, true},
		{"EraseOff", builder.EraseOff, true, false},
		{"EraseAuto 无 OnErase", builder.EraseAuto, false, false},
	}
	var checks []check
	for _, mode := range modes {
		for _, op := range eraseOps {
			name := mode.name + " × " + op.name
			res, err := eraseCase(ctx, root, name, mode.erase, mode.onErase, op)
			if err != nil {
				return fmt.Errorf("%s：%w", name, err)
			}
			_, _ = fmt.Fprintf(w, "    %s：重新生成命中=%s，删除前条目=%d，删除后条目=%d，删除后命中=%s，模型调用=%d\n",
				name, res.regenHits, res.sizeBefore, res.sizeAfter, res.afterHits, res.calls)
			wantAfter := res.sizeBefore
			if mode.physical {
				wantAfter = 0
			}
			checks = append(checks,
				check{name + " 重新生成命中缓存", res.regenHits, "1"},
				check{name + " 删除前缓存条目", res.sizeBefore, int64(1)},
				check{name + " 删除后缓存条目", res.sizeAfter, wantAfter},
				check{name + " 删除后不命中", res.afterHits, "0"},
				check{name + " 模型调用（首次 + 删除后各 1 次）", res.calls, 2},
			)
		}
	}
	return verify(checks...)
}

type eraseResult struct {
	regenHits, afterHits  string
	sizeBefore, sizeAfter int64
	calls                 int
}

func eraseCase(ctx context.Context, root, name string, mode builder.EraseMode, onErase bool, op eraseOp) (eraseResult, error) {
	var res eraseResult
	c := newCache()
	mc := builder.ModelCache{Cache: c, TTL: time.Minute, TagKey: tagKey, Erase: mode}
	cfg := memory.Config{}
	if onErase {
		cfg.OnErase = mc.Eraser()
	}
	e, err := newEnv(root, "erase-"+strings.NewReplacer(" ", "", "×", "-", "+", "-").Replace(name), cfg)
	if err != nil {
		return res, err
	}
	chat := routedClient(4, route{reply: "你喜欢喝咖啡"})
	a, err := e.agent(chat, func(b *builder.SimpleAgentBuilder) *builder.SimpleAgentBuilder { return b.WithModelCache(mc) })
	if err != nil {
		return res, err
	}
	actx := memory.WithNamespace(ctx, alice...)
	r, err := e.mem.Remember(actx, memory.Record{Kind: memory.KindFact, Content: "用户喜欢喝咖啡", Importance: 0.6})
	if err != nil {
		return res, err
	}
	ask := func() (string, error) {
		_, out, err := e.turn(ctx, a, alice, "s-cache", "我喜欢喝什么？")
		if err != nil {
			return "", err
		}
		return fmt.Sprint(out.Metadata["model_cache_hits"]), nil
	}
	if _, err := ask(); err != nil {
		return res, err
	}
	if err := e.regenerate(ctx, "s-cache"); err != nil {
		return res, err
	}
	if res.regenHits, err = ask(); err != nil {
		return res, err
	}
	res.sizeBefore = c.GetStats().Size
	if err := e.regenerate(ctx, "s-cache"); err != nil {
		return res, err
	}
	if err := op.do(actx, e, r.ID); err != nil {
		return res, err
	}
	res.sizeAfter = c.GetStats().Size
	if res.afterHits, err = ask(); err != nil {
		return res, err
	}
	res.calls = chat.Calls()
	return res, nil
}

func rewoundRestores(ctx context.Context, w io.Writer, root string) error {
	var shanghaiID string
	ext := mocks.NewScriptedClient(
		extractReply("第1轮", "用户住在上海"),
		extractReply("第2轮", "用户住在北京"),
		mocks.ScriptedRule{Name: "第2轮整理", Match: mocks.RequestContains(markReconcile), Respond: func(context.Context, *llm.CompletionRequest) (mocks.ScriptedReply, error) {
			return mocks.ScriptedReply{Text: fmt.Sprintf(`{"op":"invalidate","target":%q}`, shanghaiID)}, nil
		}},
		// 回退切开第 2 轮后补登的任务只看到用户原话，回答已被遮蔽。
		mocks.ScriptedRule{Name: "补登任务", Match: func(r *llm.CompletionRequest) bool {
			t := requestText(*r)
			return strings.Contains(t, "我搬到北京了") && !strings.Contains(t, "好的，已更新")
		}, Reply: mocks.ScriptedReply{Text: `{"memories":[]}`}},
	)
	e, err := newEnv(root, "rewound", memory.Config{Model: ext})
	if err != nil {
		return err
	}
	a, err := e.agent(mocks.NewScriptedClient(mocks.TextReply("1", "好的"), mocks.TextReply("2", "好的，已更新")), nil)
	if err != nil {
		return err
	}
	actx := memory.WithNamespace(ctx, alice...)
	const sid = "s-rewind"
	for i, in := range []string{"我住在上海", "我搬到北京了"} {
		if _, _, err := e.turn(ctx, a, alice, sid, in); err != nil {
			return err
		}
		if _, err := e.mem.ProcessPending(ctx); err != nil {
			return err
		}
		if i == 0 {
			rs, err := e.mem.Recall(actx, memory.Query{})
			if err != nil || len(rs) != 1 {
				return fmt.Errorf("第 1 轮后应有 1 条记录：%v %v", contents(rs), err)
			}
			shanghaiID = rs[0].ID
		}
	}
	beforeRewind, err := e.mem.Recall(actx, memory.Query{})
	if err != nil {
		return err
	}
	// 退回到第 2 轮的用户消息之后：回答被遮蔽，第 2 轮被切开。
	evs, err := e.sessions.Read(ctx, sid, 0)
	if err != nil {
		return err
	}
	var to int64
	for _, ev := range evs {
		if ev.Type == constants.SessionEventUserMessage {
			to = ev.Seq
		}
	}
	if _, err := session.Rewind(ctx, e.sessions, sid, to, session.LastSeq(evs)); err != nil {
		return err
	}
	if err := e.mem.Rewound(actx, sid, to); err != nil {
		return err
	}
	afterRewind, err := e.mem.Recall(actx, memory.Query{})
	if err != nil {
		return err
	}
	if len(beforeRewind) != 1 {
		return fmt.Errorf("回退前应只有 1 条有效记录，实际 %#v", contents(beforeRewind))
	}
	beijing := beforeRewind[0].ID
	rewound, err := e.mem.Get(actx, beijing)
	if err != nil {
		return err
	}
	restored, err := e.mem.Get(actx, shanghaiID)
	if err != nil {
		return err
	}
	refill, err := e.mem.ProcessPending(ctx)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "    回退到序号 %d：北京记录 %s，上海记录有效=%v，补登任务处理 %d 个\n", to, rewound.InvalidReason, restored.Valid(), refill)
	return verify(
		check{"回退前的有效记录", contents(beforeRewind), []string{"用户住在北京"}},
		check{"回退后的有效记录", contents(afterRewind), []string{"用户住在上海"}},
		check{"第 2 轮记录的失效原因", rewound.InvalidReason, memory.ReasonRewound},
		check{"被取代的旧记录恢复有效", restored.Valid(), true},
		check{"切开的轮次补登并处理的任务", refill, 1},
		check{"抽取脚本全部用完（补登任务只看到可见部分）", ext.Remaining(), 0},
	)
}
