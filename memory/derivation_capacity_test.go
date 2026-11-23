package memory

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

func TestDerivationFailureAtStateCapacityPersistsBackoff(t *testing.T) {
	for _, mode := range []string{"技术失败", "输入超长", "预算阻塞", "正文超限"} {
		t.Run(mode, func(t *testing.T) {
			st := newHookStore(t)
			now := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
			model := &scriptedModel{reply: func(string, string) (string, error) {
				if mode == "正文超限" {
					return `{"insights":[{"content":"` + strings.Repeat("新", 100) + `"}]}`, nil
				}
				return "", errors.New("模型暂时失败")
			}}
			requests := 20
			if mode == "预算阻塞" {
				requests = 1
			}
			b := maintenanceConfig(t, st, "capacity", requests)
			if mode == "输入超长" {
				b.MaxInputRunes = 8
			}
			m := workerMemory(t, Config{Durable: st, Model: model, Background: b, ReflectionThreshold: .5, Now: func() time.Time { return now }})
			ctx := WithNamespace(context.Background(), testNS...)
			if mode == "预算阻塞" {
				require.Error(t, m.chatJSON(m.maintenanceContext(ctx, testNS, initialScope), "system", "user", &struct{}{}))
			}
			_, err := m.Remember(ctx, Record{ID: "episode", Kind: KindEpisode, Content: "一次经历", Importance: 1})
			require.NoError(t, err)
			_, before, err := m.read(ctx, testNS)
			require.NoError(t, err)
			cfg := m.cfg
			cfg.MaxStateBytes = before.logicalBytes()
			m = newMemory(t, cfg)
			callsBefore := len(model.calls)
			_, err = m.ProcessPending(ctx)
			if mode == "预算阻塞" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				if mode == "正文超限" {
					require.ErrorIs(t, err, errStateFull)
				}
			}
			// 重启读取，确认结算已经持久保存，而非仅留在写者缓存。
			m = newMemory(t, cfg)
			_, s, err := m.read(ctx, testNS)
			require.NoError(t, err)
			w := s.Derivations["reflection"]
			require.Greater(t, w.DirtySeq, w.DoneSeq)
			require.Equal(t, "1", w.GeneratorVersion)
			switch mode {
			case "输入超长":
				require.True(t, w.Paused)
				require.Equal(t, "input_oversized", w.LastErrorCode)
				require.Equal(t, callsBefore, len(model.calls))
			case "预算阻塞":
				require.Equal(t, b.Budget.Configuration().RootKey, w.BlockedBudgetRootKey)
				require.Equal(t, "budget_exhausted", w.LastErrorCode)
				require.Zero(t, w.Attempts)
				require.Equal(t, callsBefore, len(model.calls))
			case "正文超限":
				require.Len(t, s.Records, 1)
				require.False(t, s.Records["episode"].Reflected)
				require.Equal(t, 1, w.Attempts)
				require.Equal(t, callsBefore+1, len(model.calls))
			case "技术失败":
				for attempt := 1; attempt <= 5; attempt++ {
					require.Equal(t, attempt, w.Attempts)
					require.Equal(t, attempt == 5, w.Paused)
					require.Equal(t, "generation_failed", w.LastErrorCode)
					require.Equal(t, now.Add(time.Second<<(attempt-1)), w.RetryAt)
					require.Len(t, model.calls, attempt)
					// 未到重试时间不提交请求，也不再收费。
					budgetBefore, e := b.Budget.Snapshot(ctx)
					require.NoError(t, e)
					_, e = m.ProcessPending(ctx)
					require.NoError(t, e)
					budgetAfter, e := b.Budget.Snapshot(ctx)
					require.NoError(t, e)
					require.Equal(t, budgetBefore, budgetAfter)
					require.Len(t, model.calls, attempt)
					if attempt < 5 {
						now = w.RetryAt
						_, e = m.ProcessPending(ctx)
						require.Error(t, e)
						m = newMemory(t, cfg)
						_, s, e = m.read(ctx, testNS)
						require.NoError(t, e)
						w = s.Derivations["reflection"]
					}
				}
				now = now.Add(time.Hour)
			}
			callsBeforeRetry := len(model.calls)
			budgetBeforeRetry, err := b.Budget.Snapshot(ctx)
			require.NoError(t, err)
			_, err = m.ProcessPending(ctx)
			require.NoError(t, err)
			budgetAfterRetry, err := b.Budget.Snapshot(ctx)
			require.NoError(t, err)
			require.Equal(t, budgetBeforeRetry, budgetAfterRetry)
			require.Len(t, model.calls, callsBeforeRetry)
			if mode == "技术失败" {
				require.Len(t, model.calls, 5)
			}
			_, err = m.Remember(ctx, Record{ID: "extra", Kind: KindFact, Content: "正文仍受容量限制"})
			require.ErrorIs(t, err, errStateFull)
			_, s, err = m.read(ctx, testNS)
			require.NoError(t, err)
			require.Len(t, s.Records, 1)
		})
	}
}

func TestCommunityWorkerDoesNotAcknowledgeUnsupportedWork(t *testing.T) {
	for _, enabledFirst := range []bool{false, true} {
		name := "无能力先运行"
		if enabledFirst {
			name = "有能力先运行"
		}
		t.Run(name, func(t *testing.T) {
			st := newHookStore(t)
			ctx := WithNamespace(context.Background(), testNS...)
			seedModel := &scriptedModel{reply: func(string, string) (string, error) {
				return `{"memories":[{"kind":"fact","content":"Alice works at Acme in Singapore","importance":1}],"relations":[{"from":0,"subject":{"name":"Alice","type":"person"},"predicate":"works at","object":{"name":"Acme","type":"org"},"fact":"Alice works at Acme"},{"from":0,"subject":{"name":"Acme","type":"org"},"predicate":"located in","object":{"name":"Singapore","type":"place"},"fact":"Acme is in Singapore"}]}`, nil
			}}
			seed := workerMemory(t, Config{Durable: st, Model: seedModel, Graph: true})
			from, to := appendTurn(t, seed.cfg.Sessions, "seed", false, llm.UserMessage("Alice works at Acme in Singapore"))
			require.NoError(t, seed.Enqueue(ctx, Job{Key: "seed", Namespace: testNS, Scope: initialScope, Session: "seed", From: from, To: to}))
			_, err := seed.ProcessPending(ctx)
			require.NoError(t, err)
			model := &scriptedModel{reply: func(string, string) (string, error) { return `{"name":"社区","summary":"摘要"}`, nil }}
			b := maintenanceConfig(t, st, "community", 20)
			capable := workerMemory(t, Config{Durable: st, Model: model, Graph: true, Communities: true, Background: b})
			unsupported := workerMemory(t, Config{Durable: st, Model: model, Background: b})
			require.NoError(t, capable.MarkDerivationsDirty(ctx))
			_, s, err := capable.read(ctx, testNS)
			require.NoError(t, err)
			require.Len(t, s.Entities, 3)
			require.Len(t, s.Edges, 2)
			before := s.Derivations["community"]
			if enabledFirst {
				_, err = capable.ProcessPending(ctx)
				require.NoError(t, err)
			}
			n, err := unsupported.ProcessPending(ctx)
			require.NoError(t, err)
			require.Zero(t, n)
			_, s, err = capable.read(ctx, testNS)
			require.NoError(t, err)
			if !enabledFirst {
				require.Equal(t, before, s.Derivations["community"])
				require.Empty(t, model.calls)
				entries, e := capable.queued(ctx)
				require.NoError(t, e)
				require.Len(t, entries, 1)
			}
			_, err = capable.ProcessPending(ctx)
			require.NoError(t, err)
			_, s, err = capable.read(ctx, testNS)
			require.NoError(t, err)
			require.Equal(t, before.DirtySeq, s.Derivations["community"].DoneSeq)
			require.Len(t, s.Communities, 1)
			require.Len(t, model.calls, 1)
			requireQueueEmpty(t, capable)
		})
	}
	t.Run("默认部署不空转", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			st := newHookStore(t)
			model := &scriptedModel{reply: func(string, string) (string, error) { return "{}", nil }}
			b := maintenanceConfig(t, st, "default", 20)
			m := workerMemory(t, Config{Durable: st, Model: model, Background: b})
			ctx := WithNamespace(context.Background(), testNS...)
			require.NoError(t, m.MarkDerivationsDirty(ctx))
			var rounds, saves atomic.Int32
			st.h.before = func(op, scope, id string) error {
				if op == "lock" && id == workerID {
					rounds.Add(1)
				}
				if op == "save" {
					saves.Add(1)
				}
				return nil
			}
			runCtx, cancel := context.WithCancel(ctx)
			done := make(chan error, 1)
			go func() { done <- m.Run(runCtx, nil) }()
			synctest.Wait()
			require.EqualValues(t, 1, rounds.Load())
			writes := saves.Load()
			time.Sleep(3 * constants.DefaultMemoryWorkerInterval)
			synctest.Wait()
			require.GreaterOrEqual(t, rounds.Load(), int32(3))
			require.LessOrEqual(t, rounds.Load(), int32(4))
			require.Equal(t, writes, saves.Load())
			cancel()
			require.ErrorIs(t, <-done, context.Canceled)
			require.Empty(t, model.calls)
			_, s, err := m.read(ctx, testNS)
			require.NoError(t, err)
			require.Greater(t, s.Derivations["community"].DirtySeq, s.Derivations["community"].DoneSeq)
			snap, err := b.Budget.Snapshot(ctx)
			require.NoError(t, err)
			require.Zero(t, snap.ReservedRequests)
		})
	})
}
