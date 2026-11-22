package memory

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/stretchr/testify/require"
)

func maintenanceConfig(t *testing.T, st durable.Store, root string, requests int) *BackgroundConfig {
	t.Helper()
	c, err := budget.NewDurable(budget.Config{RootKey: root, Version: "1", MaxModelRequests: requests, MaxReservedTokens: 100000, TokenPolicyVersion: "1"}, st)
	require.NoError(t, err)
	return &BackgroundConfig{Budget: c, MaxInputRunes: 10000, MaxOutputRunes: 1000, MaxOutputTokens: 32, MaxDerivationsPerPass: 1, EstimatorVersion: "1", EstimateChat: func(r *llm.CompletionRequest) (int, error) { require.Equal(t, 32, r.MaxTokens); return 64, nil }, EstimateEmbedding: func(string) (int, error) { return 8, nil }, SingleRequestClients: true}
}

func TestSameMaintenanceWindowCannotResetQuota(t *testing.T) {
	st := newHookStore(t)
	model := &scriptedModel{reply: func(string, string) (string, error) { return "{}", nil }}
	for i := 0; i < 2; i++ {
		b := maintenanceConfig(t, st, "same", 1)
		m := durableMemory(t, st, Config{Background: b, Model: model})
		ctx := m.maintenanceContext(context.Background(), testNS, initialScope)
		err := m.chatJSON(ctx, "system", "user", &struct{}{})
		if i == 0 {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, errBackgroundBudget)
		}
	}
	require.Len(t, model.calls, 1)
}

func TestDerivationFailureWithoutNewJobsRemainsDirty(t *testing.T) {
	st := newHookStore(t)
	now := time.Now()
	model := &scriptedModel{reply: func(string, string) (string, error) { return "", errors.New("失败") }}
	m := workerMemory(t, Config{Durable: st, Model: model, Now: func() time.Time { return now }, ReflectionThreshold: .5, Background: maintenanceConfig(t, st, "retry", 20)})
	ctx := WithNamespace(context.Background(), testNS...)
	_, err := m.Remember(ctx, Record{ID: "e", Kind: KindEpisode, Content: "一次经历", Importance: 1})
	require.NoError(t, err)
	for i := 1; i <= 5; i++ {
		_, err = m.ProcessPending(ctx)
		require.Error(t, err)
		_, s, err := m.read(ctx, testNS)
		require.NoError(t, err)
		w := s.Derivations["reflection"]
		require.Greater(t, w.DirtySeq, w.DoneSeq)
		require.Equal(t, i, w.Attempts)
		require.Len(t, model.calls, i)
		require.Equal(t, i == 5, w.Paused)
		if i < 5 {
			require.Equal(t, now.Add(time.Second<<(i-1)).Unix(), w.RetryAt.Unix())
			_, err = m.ProcessPending(ctx)
			require.NoError(t, err)
			now = w.RetryAt
		}
	}
	_, err = m.Remember(ctx, Record{ID: "e2", Kind: KindEpisode, Content: "另一次", Importance: 1})
	require.NoError(t, err)
	_, err = m.ProcessPending(ctx)
	require.NoError(t, err)
	require.Len(t, model.calls, 5)
}
func TestOversizedDerivationPausesBeforeRequest(t *testing.T) {
	st := newHookStore(t)
	b := maintenanceConfig(t, st, "oversize", 10)
	b.MaxInputRunes = 8
	m := workerMemory(t, Config{Durable: st, Background: b, ReflectionThreshold: .1})
	ctx := WithNamespace(context.Background(), testNS...)
	_, err := m.Remember(ctx, Record{ID: "e", Kind: KindEpisode, Content: "经历", Importance: 1})
	require.NoError(t, err)
	_, _ = m.ProcessPending(ctx)
	_, s, err := m.read(ctx, testNS)
	require.NoError(t, err)
	w := s.Derivations["reflection"]
	require.True(t, w.Paused)
	require.Equal(t, "input_oversized", w.LastErrorCode)
	require.Zero(t, s.RequestSeq)
	snap, err := b.Budget.Snapshot(ctx)
	require.NoError(t, err)
	require.Zero(t, snap.ReservedRequests)
}

func TestCommunityProgressSurvivesPassLimitAndRestart(t *testing.T) {
	for _, repair := range []bool{false, true} {
		t.Run(fmt.Sprint(repair), func(t *testing.T) {
			st := newHookStore(t)
			calls := 0
			model := &scriptedModel{reply: func(string, string) (string, error) {
				calls++
				if repair && calls == 1 {
					return "bad", nil
				}
				return `{"name":"社区","summary":"摘要"}`, nil
			}}
			cfg := Config{Durable: st, Graph: true, Communities: true, Model: model, Background: maintenanceConfig(t, st, "pass", 12)}
			m := workerMemory(t, cfg)
			g := graphOf(m)
			ctx := WithNamespace(context.Background(), testNS...)
			for _, prefix := range []string{"a", "b", "c"} {
				for _, suffix := range []string{"1", "2", "3"} {
					id := prefix + suffix
					require.NoError(t, g.PutEntity(ctx, Entity{ID: id, Name: id, Namespace: testNS, Type: "x"}))
				}
				for i := 1; i < 3; i++ {
					id := fmt.Sprint(prefix, i)
					require.NoError(t, g.PutEdge(ctx, Edge{ID: id, Namespace: testNS, Subject: id, Object: fmt.Sprint(prefix, i+1), Predicate: "关联", Fact: id}))
				}
			}
			for i := 1; i <= 3; i++ {
				m = workerMemory(t, cfg)
				_, err := m.ProcessPending(ctx)
				require.NoError(t, err)
				_, s, err := m.read(ctx, testNS)
				require.NoError(t, err)
				require.Len(t, s.Communities, i)
				w := s.Derivations["community"]
				if i < 3 {
					require.Greater(t, w.DirtySeq, w.DoneSeq)
					require.Equal(t, "deferred_pass_limit", w.LastErrorCode)
				} else {
					require.Equal(t, w.DirtySeq, w.DoneSeq)
				}
				want := i
				if repair {
					want++
				}
				require.Equal(t, want, calls)
			}
		})
	}
}
func TestNewMaintenanceWindowUnblocksBudgetOnly(t *testing.T) {
	st := newHookStore(t)
	b := maintenanceConfig(t, st, "first", 1)
	m := workerMemory(t, Config{Durable: st, Background: b})
	ctx := WithNamespace(context.Background(), testNS...)
	require.NoError(t, m.MarkDerivationsDirty(ctx))
	_, err := m.mutate(ctx, testNS, initialScope, func(x *tx) error {
		for _, k := range []string{"community", "reflection"} {
			w, _ := x.Derivations.Get(k)
			w.BlockedBudgetRootKey = "first"
			w.Attempts = 3
			w.RetryAt = m.now().Add(time.Hour)
			w.Paused = k == "reflection"
			w.LastErrorCode = "generation_failed"
			x.Derivations.Put(k, w)
		}
		return nil
	})
	require.NoError(t, err)
	for _, root := range []string{"first", "second"} {
		m = workerMemory(t, Config{Durable: st, Background: maintenanceConfig(t, st, root, 1)})
		_, err = m.ProcessPending(ctx)
		require.NoError(t, err)
		_, s, err := m.read(ctx, testNS)
		require.NoError(t, err)
		for k, w := range s.Derivations {
			if root == "first" {
				require.Equal(t, "first", w.BlockedBudgetRootKey)
			} else {
				require.Empty(t, w.BlockedBudgetRootKey)
			}
			require.Equal(t, 3, w.Attempts)
			require.Equal(t, k == "reflection", w.Paused)
			require.True(t, w.RetryAt.After(m.now()))
			require.Greater(t, w.DirtySeq, w.DoneSeq)
			require.Equal(t, "generation_failed", w.LastErrorCode)
		}
	}
}

func TestBudgetPausePreservesPendingExtraction(t *testing.T) {
	st := newHookStore(t)
	b := maintenanceConfig(t, st, "old", 1)
	model := &scriptedModel{reply: func(system, user string) (string, error) {
		if strings.Contains(user, "合法 JSON") {
			return "{}", nil
		}
		return "bad", nil
	}}
	m := workerMemory(t, Config{Durable: st, Background: b, Model: model})
	ctx := WithNamespace(context.Background(), testNS...)
	from, to := appendTurn(t, m.cfg.Sessions, "s", false, llm.UserMessage("一个事实"))
	require.NoError(t, m.Enqueue(ctx, Job{Key: "k", Namespace: testNS, Scope: initialScope, Session: "s", From: from, To: to}))
	for i := 0; i < 2; i++ {
		_, err := m.ProcessPending(ctx)
		require.NoError(t, err)
		j, ok, err := m.Job(ctx, "k")
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, JobPending, j.Status)
		require.Zero(t, j.Attempts)
		require.Len(t, model.calls, 1)
	}
	cfg := m.cfg
	cfg.Background = maintenanceConfig(t, st, "new", 2)
	m = newMemory(t, cfg)
	_, err := m.ProcessPending(ctx)
	require.NoError(t, err)
	j, _, err := m.Job(ctx, "k")
	require.NoError(t, err)
	require.Equal(t, JobDone, j.Status)
	for _, tc := range []struct {
		b *BackgroundConfig
		n int
	}{{b, 1}, {cfg.Background, 2}} {
		s, err := tc.b.Budget.Snapshot(ctx)
		require.NoError(t, err)
		require.Equal(t, tc.n, s.ReservedRequests)
	}
}
func TestEraseStillCompletesWhenEmbeddingBudgetExhausted(t *testing.T) {
	st := newHookStore(t)
	b := maintenanceConfig(t, st, "erase", 1)
	base := NewMemoryIndex()
	crash := false
	idx := hookIndex{Index: base, before: func(op string, _ []string, _ string) error {
		if crash && op == "delete" {
			return errors.New("进程中断")
		}
		return nil
	}}
	cfg := Config{Durable: st, Background: b, Index: idx, Embedder: bigramEmbedder{}}
	m := newMemory(t, cfg)
	ctx := WithNamespace(context.Background(), testNS...)
	for _, id := range []string{"secret", "keep"} {
		_, err := m.Remember(ctx, Record{ID: id, Content: id})
		require.NoError(t, err)
	}
	crash = true
	require.Error(t, m.Forget(ctx, "secret"))
	crash = false
	ins := recordSpace.namespace(testNS, initialScope)
	ids, err := base.IDs(ctx, ins)
	require.NoError(t, err)
	for _, raw := range ids {
		k, ok := parseIndexKey(raw)
		require.True(t, ok)
		if k.ID == "keep" {
			require.NoError(t, base.Delete(ctx, ins, raw))
		}
	}
	require.NoError(t, b.Budget.ReserveAttributed(ctx, "spent", 5, nil))
	_ = m.Maintain(ctx)
	ids, err = base.IDs(ctx, ins)
	require.NoError(t, err)
	require.Empty(t, ids)
	entries, err := m.queued(ctx)
	require.NoError(t, err)
	require.Contains(t, entries, queueEntry{Namespace: testNS, Scope: initialScope, Reason: queueIndex, BlockedBudgetRootKey: "erase"})
	_, before, err := m.read(ctx, testNS)
	require.NoError(t, err)
	m = newMemory(t, cfg)
	_ = m.Maintain(ctx)
	_, after, err := m.read(ctx, testNS)
	require.NoError(t, err)
	require.Equal(t, before.RequestSeq, after.RequestSeq, "同一窗口重启不重复尝试付费修补")
	cfg.Background = maintenanceConfig(t, st, "next", 1)
	m = newMemory(t, cfg)
	require.NoError(t, m.Maintain(ctx))
	ids, err = base.IDs(ctx, ins)
	require.NoError(t, err)
	require.Len(t, ids, 1)
	k, ok := parseIndexKey(ids[0])
	require.True(t, ok)
	require.Equal(t, "keep", k.ID)
	requireQueueEmpty(t, m)
}

type observingTransport func(*http.Request) (*http.Response, error)

func (rt observingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return rt(r) }

func TestBackgroundPhysicalRequestsMatchReservations(t *testing.T) {
	st := newHookStore(t)
	b := maintenanceConfig(t, st, "http", 5)
	ctx := WithNamespace(context.Background(), testNS...)
	var n, chats atomic.Int32
	var online atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := n.Add(1)
		snap, err := b.Budget.Snapshot(context.Background())
		if err != nil || !online.Load() && snap.ReservedRequests != int(i) {
			t.Errorf("第 %d 个物理请求前的账本为 %+v, %v", i, snap, err)
		}
		if strings.HasSuffix(r.URL.Path, "/embeddings") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[1,0]}]}`)
			return
		}
		var req struct {
			MaxTokens int           `json:"max_tokens"`
			Messages  []llm.Message `json:"messages"`
		}
		if err := json.Decode(r.Body, &req); err != nil || req.MaxTokens != 32 {
			t.Errorf("请求参数 %+v, %v", req, err)
		}
		i = chats.Add(1)
		answer := "bad"
		if i == 2 {
			answer = `{"memories":[{"kind":"fact","content":"一次旅行","importance":0.8}]}`
			if len(req.Messages) != 4 {
				t.Errorf("修复消息数量 %d", len(req.Messages))
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		encoded, _ := json.Marshal(answer)
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1,\"total_tokens\":3}}\n\ndata: [DONE]\n\n", encoded)
	}))
	defer server.Close()
	client := *server.Client()
	transport := client.Transport
	client.Transport = observingTransport(func(r *http.Request) (*http.Response, error) {
		if !online.Load() {
			if durable.HasJournal(r.Context()) || len(usage.Lineage(r.Context())) != 0 {
				t.Error("后台请求继承在线归属")
			}
			if c, ok := budget.FromContext(r.Context()); !ok || c != b.Budget {
				t.Error("后台请求缺维护预算")
			}
		}
		return transport.RoundTrip(r)
	})
	model, err := providers.NewOpenAIWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL), llm.WithEmbeddingRetries(0), llm.WithHTTPClient(&client))
	require.NoError(t, err)
	m := workerMemory(t, Config{Durable: st, Background: b, Model: model, Index: NewMemoryIndex(), Embedder: model})
	from, to := appendTurn(t, m.cfg.Sessions, "s", false, llm.UserMessage("我去旅行"))
	require.NoError(t, m.Enqueue(ctx, Job{Key: "k", Namespace: testNS, Scope: initialScope, Session: "s", From: from, To: to}))
	ctx = usage.WithInvocation(durable.WithJournal(ctx, st), usage.Invocation{Owner: usage.Owner{AgentID: "online", AgentCallKey: "online", RunID: "run"}})
	_, err = m.ProcessPending(ctx)
	require.NoError(t, err)
	j, _, err := m.Job(ctx, "k")
	require.NoError(t, err)
	require.Equal(t, JobDone, j.Status, j.Error)
	require.EqualValues(t, 4, n.Load())
	require.EqualValues(t, 2, chats.Load())
	a, err := b.Budget.Attribution(ctx)
	require.NoError(t, err)
	require.Equal(t, 4, a.Root.ReservedRequests)
	require.Equal(t, 2, a.Root.UnknownUsageEntries)
	require.Equal(t, 6, a.Root.KnownTokens.TotalTokens)
	require.Equal(t, 144, a.Root.ReservedTokens)
	require.Empty(t, a.Calls)
	_, s, err := m.read(ctx, testNS)
	require.NoError(t, err)
	require.EqualValues(t, 4, s.RequestSeq)
	online.Store(true)
	_, err = m.Remember(ctx, Record{ID: "online", Content: "在线内容"})
	require.NoError(t, err)
	_, err = m.RecallBlock(ctx, "在线")
	require.NoError(t, err)
	require.Greater(t, n.Load(), int32(4))
	snap, err := b.Budget.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, 4, snap.ReservedRequests)
}

func TestBackgroundConfigValidationAndIdentity(t *testing.T) {
	st := newHookStore(t)
	valid := maintenanceConfig(t, st, "valid", 10)
	mem, err := budget.New(valid.Budget.Configuration())
	require.NoError(t, err)
	for _, edit := range []func(*BackgroundConfig){
		func(b *BackgroundConfig) { b.Budget = nil }, func(b *BackgroundConfig) { b.Budget = mem }, func(b *BackgroundConfig) { b.MaxInputRunes = 0 }, func(b *BackgroundConfig) { b.MaxOutputRunes = 0 }, func(b *BackgroundConfig) { b.MaxOutputTokens = 0 }, func(b *BackgroundConfig) { b.MaxDerivationsPerPass = 0 }, func(b *BackgroundConfig) { b.EstimatorVersion = "other" }, func(b *BackgroundConfig) { b.EstimateChat = nil }, func(b *BackgroundConfig) { b.EstimateEmbedding = nil }, func(b *BackgroundConfig) { b.SingleRequestClients = false },
		func(b *BackgroundConfig) {
			c := b.Budget.Configuration()
			c.RootKey = "no-requests"
			c.MaxModelRequests = 0
			b.Budget, err = budget.NewDurable(c, st)
			require.NoError(t, err)
		},
		func(b *BackgroundConfig) {
			c := b.Budget.Configuration()
			c.RootKey = "no-tokens"
			c.MaxReservedTokens = 0
			b.Budget, err = budget.NewDurable(c, st)
			require.NoError(t, err)
		},
	} {
		b := *valid
		edit(&b)
		_, err := New(Config{Durable: st, Background: &b})
		require.Error(t, err)
	}
	m := newMemory(t, Config{Durable: st, Background: valid})
	plain := newMemory(t, Config{Durable: st})
	require.Equal(t, plain.Identity(), m.Identity())
}

func TestReflectionVersionConflictLeavesDirty(t *testing.T) {
	st := newHookStore(t)
	var m *Memory
	ctx := WithNamespace(context.Background(), testNS...)
	fired := false
	model := &scriptedModel{reply: func(string, string) (string, error) {
		if !fired {
			fired = true
			r, err := m.Get(ctx, "r")
			require.NoError(t, err)
			_, err = m.Update(ctx, r.ID, r.Revision, Patch{Pinned: new(true)})
			require.NoError(t, err)
		}
		return `{"insights":[{"content":"反思"}]}`, nil
	}}
	m = workerMemory(t, Config{Durable: st, Background: maintenanceConfig(t, st, "concurrent", 10), Model: model, ReflectionThreshold: .5})
	_, err := m.Remember(ctx, Record{ID: "r", Kind: KindEpisode, Content: "经历", Importance: 1})
	require.NoError(t, err)
	_, err = m.ProcessPending(ctx)
	require.NoError(t, err)
	_, s, err := m.read(ctx, testNS)
	require.NoError(t, err)
	w := s.Derivations["reflection"]
	require.Greater(t, w.DirtySeq, w.DoneSeq)
	require.False(t, s.Records["r"].Reflected)
	_, err = m.ProcessPending(ctx)
	require.NoError(t, err)
	_, s, err = m.read(ctx, testNS)
	require.NoError(t, err)
	require.True(t, s.Records["r"].Reflected)
	require.Len(t, s.Records, 2)
	require.Len(t, model.calls, 2)
}

func TestStrictRefreshAdoptsExistingGraph(t *testing.T) {
	st := newHookStore(t)
	ctx := WithNamespace(context.Background(), testNS...)
	cfg := Config{Durable: st, Graph: true, Communities: true, Model: &scriptedModel{reply: func(string, string) (string, error) { return `{"summary":"概括"}`, nil }}}
	m := newMemory(t, cfg)
	g := graphOf(m)
	for _, id := range []string{"a", "b", "c"} {
		require.NoError(t, g.PutEntity(ctx, Entity{Namespace: testNS, ID: id, Name: id, Type: "x"}))
	}
	for _, id := range []string{"a", "c"} {
		require.NoError(t, g.PutEdge(ctx, Edge{ID: id, Namespace: testNS, Subject: id, Object: "b", Predicate: "关联", Fact: id}))
	}
	cfg.Background = maintenanceConfig(t, st, "refresh", 2)
	m = newMemory(t, cfg)
	require.NoError(t, m.RefreshCommunities(ctx))
	_, s, err := m.read(ctx, testNS)
	require.NoError(t, err)
	require.Len(t, s.Communities, 1)
	require.EqualValues(t, 1, s.RequestSeq)
}
