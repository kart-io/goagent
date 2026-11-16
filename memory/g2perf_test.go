package memory

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/llm"
)

// TestG2PerfLock 测量 Forget 与 Update（正文变化）在 5k 数据集上的加锁时长（head 加锁到 head 写入）。
func TestG2PerfLock(t *testing.T) {
	if os.Getenv("MEMORY_G2_PERF") != "1" {
		t.Skip()
	}
	ctx := WithNamespace(context.Background(), benchNS...)
	for _, ds := range benchDatasets(t) {
		dir := benchDir(t)
		fs, err := durable.NewFileStore(dir)
		require.NoError(t, err)
		hs := &hookStore{inner: fs, root: fs, h: &storeHooks{held: map[string]int{}}}
		m := durableMemory(t, hs, Config{MaxStateBytes: 1 << 30, Graph: true, Communities: true,
			Model: &scriptedModel{reply: func(string, string) (string, error) { return `{"name":"x","summary":"y"}`, nil }}})
		_, err = m.mutate(ctx, benchNS, "", benchState(5000, ds.gen))
		require.NoError(t, err)
		var locked time.Time
		var held []time.Duration
		hs.h.before = func(op, _, id string) error {
			if op == "lock" && id == headID {
				locked = time.Now()
			}
			return nil
		}
		hs.h.afterSave = func(_, id string) error {
			if id == headID && !locked.IsZero() {
				held, locked = append(held, time.Since(locked)), time.Time{}
			}
			return nil
		}
		measure := func(name string, n int, op func(i int)) {
			held = nil
			start := time.Now()
			for i := range n {
				op(i)
			}
			t.Logf("| %s | %s | %d | p50 %s | p95 %s | 端到端均值 %s |", ds.name, name, len(held), ms(percentile(held, 0.5)), ms(percentile(held, 0.95)),
				ms(time.Since(start)/time.Duration(n)))
		}
		valid := func(i int) string { return fmt.Sprintf("rec-%06d", (i/7)*10+3+i%7) }
		measure("Update 正文", 150, func(i int) {
			_, s, err := m.read(ctx, benchNS)
			require.NoError(t, err)
			r := s.Records[valid(i)]
			content := r.Content + "改"
			_, err = m.Update(ctx, r.ID, r.Revision, Patch{Content: &content})
			require.NoError(t, err)
		})
		measure("Forget", 150, func(i int) { require.NoError(t, m.Forget(ctx, valid(i+2000))) })
	}
}

// TestG2PerfWorker 测量 Worker 处理一轮（5 条记录、10 条关系）各次 head 提交的加锁时长之和，5k 数据集（设计第 8 节）。
func TestG2PerfWorker(t *testing.T) {
	if os.Getenv("MEMORY_G2_PERF") != "1" {
		t.Skip()
	}
	ctx := WithNamespace(context.Background(), benchNS...)
	for _, ds := range benchDatasets(t) {
		dir := benchDir(t)
		fs, err := durable.NewFileStore(dir)
		require.NoError(t, err)
		hs := &hookStore{inner: fs, root: fs, h: &storeHooks{held: map[string]int{}}}
		sessions := session.NewMemoryStore()
		var extract string
		model := &scriptedModel{reply: func(system, _ string) (string, error) {
			switch system {
			case reconcilePrompt:
				return `{"op":"add"}`, nil
			case resolvePrompt:
				return `{"resolutions":[]}`, nil
			}
			return extract, nil
		}}
		m := durableMemory(t, hs, Config{MaxStateBytes: 1 << 30, Graph: true, Model: model, Sessions: sessions, ReflectionThreshold: 1e9})
		_, err = m.mutate(ctx, benchNS, "", benchState(5000, ds.gen))
		require.NoError(t, err)
		var locked time.Time
		var held []time.Duration
		hs.h.before = func(op, _, id string) error {
			if op == "lock" && id == headID {
				locked = time.Now()
			}
			return nil
		}
		hs.h.afterSave = func(_, id string) error {
			if id == headID && !locked.IsZero() {
				held, locked = append(held, time.Since(locked)), time.Time{}
			}
			return nil
		}
		rng := rand.New(rand.NewPCG(11, 13))
		var sums []time.Duration
		commits := 0
		for i := range 60 {
			var mems, rels []string
			for k := range 5 {
				mems = append(mems, fmt.Sprintf(`{"kind":"fact","content":%q,"importance":0.6}`, ds.gen(rng, 40+rng.IntN(80))))
				for r := range 2 {
					rels = append(rels, fmt.Sprintf(`{"from":%d,"subject":{"name":%q,"type":"person"},"predicate":"关联%d","object":{"name":%q,"type":"concept"},"fact":%q}`,
						k, ds.gen(rng, 4), r, ds.gen(rng, 4), ds.gen(rng, 20)))
				}
			}
			extract = `{"memories":[` + strings.Join(mems, ",") + `],"relations":[` + strings.Join(rels, ",") + `]}`
			from, to := appendTurn(t, sessions, "s", false, llm.UserMessage("这一轮的对话"), llm.Message{Role: "assistant", Content: "好的"})
			_, s, err := m.read(ctx, benchNS)
			require.NoError(t, err)
			require.NoError(t, m.Enqueue(ctx, Job{Key: fmt.Sprintf("perf/%d", i), Namespace: benchNS, Session: "s", Scope: initialScope, From: from, To: to, TurnSeq: s.Seq}))
			held = nil
			_, err = m.ProcessPending(ctx)
			require.NoError(t, err)
			var sum time.Duration
			for _, d := range held {
				sum += d
			}
			sums, commits = append(sums, sum), commits+len(held)
		}
		t.Logf("| %s | Worker 5 记录 10 关系 | 每轮提交 %.1f 次 | 加锁之和 p50 %s | p95 %s |", ds.name, float64(commits)/60, ms(percentile(sums, 0.5)), ms(percentile(sums, 0.95)))
	}
}
