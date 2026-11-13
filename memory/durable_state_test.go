package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	storemem "github.com/kart-io/goagent/store/memory"
)

// storeHooks 在 hookStore 的每次调用前后插入回调，制造确定的交错与崩溃。
type storeHooks struct {
	// before 在调用前执行；返回错误时调用失败，不触达底层存储。
	before func(op, scope, id string) error
	// afterSave 在底层 Save 成功之后执行；返回错误时作为 Save 的结果（写入已生效、调用方看到失败）。
	afterSave func(scope, id string) error
	// held 记录当前持有的锁（作用域 + ID）：Save 未持锁的 ID 时报错，在 FileStore 上也强制存储契约（pgstore 会校验）。
	mu   sync.Mutex
	held map[string]int
	// saveCtx 在 Save 调用前收到其 ctx，用于检查提交的时间上限。
	saveCtx func(ctx context.Context)
}

// hookStore 包装一个实现了 Scoped 的 durable.Store；作用域视图共用同一组回调。
type hookStore struct {
	inner durable.Store
	root  durable.Scoped
	scope string
	h     *storeHooks
}

func newHookStore(t *testing.T) *hookStore {
	t.Helper()
	fs, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	return &hookStore{inner: fs, root: fs, h: &storeHooks{held: map[string]int{}}}
}

func (s *hookStore) call(op, id string) error {
	if s.h.before != nil {
		return s.h.before(op, s.scope, id)
	}
	return nil
}

func (s *hookStore) Lock(ctx context.Context, id string) (func() error, error) {
	if err := s.call("lock", id); err != nil {
		return nil, err
	}
	unlock, err := s.inner.Lock(ctx, id)
	if err != nil {
		return nil, err
	}
	key := s.scope + "\x00" + id
	s.h.mu.Lock()
	s.h.held[key]++
	s.h.mu.Unlock()
	return func() error {
		s.h.mu.Lock()
		s.h.held[key]--
		s.h.mu.Unlock()
		return unlock()
	}, nil
}

func (s *hookStore) requireHeld(id string) error {
	s.h.mu.Lock()
	defer s.h.mu.Unlock()
	if s.h.held[s.scope+"\x00"+id] <= 0 {
		return fmt.Errorf("%w：未持有 %q 的锁就写入（违反存储契约）", agentErrors.ErrLeaseLost, id)
	}
	return nil
}

func (s *hookStore) Load(ctx context.Context, id string) (*durable.Snapshot, error) {
	if err := s.call("load", id); err != nil {
		return nil, err
	}
	return s.inner.Load(ctx, id)
}

func (s *hookStore) Save(ctx context.Context, id string, snap *durable.Snapshot) error {
	if s.h.saveCtx != nil {
		s.h.saveCtx(ctx)
	}
	if err := s.requireHeld(id); err != nil {
		return err
	}
	if err := s.call("save", id); err != nil {
		return err
	}
	if err := s.inner.Save(ctx, id, snap); err != nil {
		return err
	}
	if s.h.afterSave != nil {
		return s.h.afterSave(s.scope, id)
	}
	return nil
}

func (s *hookStore) SaveAll(ctx context.Context, writes ...durable.Write) error {
	for _, w := range writes {
		if err := s.requireHeld(w.ID); err != nil {
			return err
		}
	}
	if err := s.call("saveall", ""); err != nil {
		return err
	}
	return s.inner.SaveAll(ctx, writes...)
}

func (s *hookStore) Scope(name string) durable.Store {
	return &hookStore{inner: s.root.Scope(name), root: s.root, scope: name, h: s.h}
}

func (s *hookStore) RemoveScope(ctx context.Context, name string) error {
	return s.root.RemoveScope(ctx, name)
}

func (s *hookStore) PruneRemoved(ctx context.Context, olderThan time.Duration) error {
	return s.root.PruneRemoved(ctx, olderThan)
}

func durableMemory(t *testing.T, st durable.Store, cfg Config) *Memory {
	t.Helper()
	cfg.Store, cfg.Durable = storemem.New(), st
	return newMemory(t, cfg)
}

// setMeta 持 meta 锁写入命名空间的 meta，模拟 Purge 的提交点。
func setMeta(t *testing.T, st durable.Store, ns []string, meta metaState) {
	t.Helper()
	snap, err := encodeSnapshot(metaID(ns), meta)
	require.NoError(t, err)
	unlock, err := st.Lock(context.Background(), metaID(ns))
	require.NoError(t, err)
	defer func() { require.NoError(t, unlock()) }()
	require.NoError(t, st.Save(context.Background(), metaID(ns), snap))
}

// addRecord 返回在 state 中加入一条记录的修改函数。
func addRecord(id string) func(*tx) error {
	return func(t *tx) error {
		t.SetNextIncarnation(t.NextIncarnation() + 1)
		t.Records.Put(id, Record{ID: id, Content: id})
		return nil
	}
}

var testNS = []string{"t1", "alice"}

func TestMutateCommitsAndReadSeesIt(t *testing.T) {
	ctx := context.Background()
	m := durableMemory(t, newHookStore(t), Config{})

	_, s, err := m.read(ctx, testNS)
	require.NoError(t, err)
	require.Empty(t, s.Records, "新命名空间读到空 state")

	committed, err := m.mutate(ctx, testNS, "", addRecord("a"))
	require.NoError(t, err)
	require.EqualValues(t, 1, committed.Seq)
	require.NotEmpty(t, committed.CommitID)

	scope, s, err := m.read(ctx, testNS)
	require.NoError(t, err)
	require.Equal(t, initialScope, scope, "首写落在初始作用域")
	require.Contains(t, s.Records, "a")
	require.Equal(t, committed.CommitID, s.CommitID)

	_, err = m.mutate(ctx, testNS, "", func(*tx) error { return errors.New("拒绝") })
	require.ErrorContains(t, err, "拒绝")
	_, s, err = m.read(ctx, testNS)
	require.NoError(t, err)
	require.EqualValues(t, 1, s.Seq, "修改函数出错时不提交")
}

// 两个 Memory 实例共用一个目录、并发首写同一个新命名空间：没有丢失更新，Seq 连续。
func TestMutateConcurrentWritersLoseNothing(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	var ms []*Memory
	for range 2 {
		fs, err := durable.NewFileStore(dir)
		require.NoError(t, err)
		ms = append(ms, durableMemory(t, fs, Config{}))
	}
	const n = 40
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ms[i%2].mutate(ctx, testNS, "", addRecord(fmt.Sprintf("r%02d", i)))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	_, s, err := ms[0].read(ctx, testNS)
	require.NoError(t, err)
	require.Len(t, s.Records, n)
	require.EqualValues(t, n, s.Seq)
	require.EqualValues(t, n, s.NextIncarnation)
	checkInvariants(t, testNS, s)
}

func TestMutateScopeChecks(t *testing.T) {
	ctx := context.Background()
	t.Run("期望的作用域不是当前作用域", func(t *testing.T) {
		m := durableMemory(t, newHookStore(t), Config{})
		_, err := m.mutate(ctx, testNS, "other", addRecord("a"))
		require.ErrorIs(t, err, errScopeChanged)
	})
	t.Run("加锁期间发生 Purge：放弃写入，两个作用域都没有数据", func(t *testing.T) {
		st := newHookStore(t)
		m := durableMemory(t, st, Config{})
		st.h.before = func(op, scope, id string) error {
			if op == "lock" && id == headID {
				st.h.before = nil
				setMeta(t, st, testNS, metaState{Current: "new", Purges: 1})
			}
			return nil
		}
		_, err := m.mutate(ctx, testNS, "", addRecord("a"))
		require.ErrorIs(t, err, errScopeChanged)
		for _, scope := range []string{initialScope, "new"} {
			_, err := st.Scope(scopeName(testNS, scope)).Load(ctx, headID)
			require.ErrorIs(t, err, os.ErrNotExist, "作用域 %s 不应被写入", scope)
		}
	})
	t.Run("作用域已删除：视为作用域改变", func(t *testing.T) {
		st := newHookStore(t)
		m := durableMemory(t, st, Config{})
		require.NoError(t, st.RemoveScope(ctx, scopeName(testNS, initialScope)))
		_, err := m.mutate(ctx, testNS, "", addRecord("a"))
		require.ErrorIs(t, err, errScopeChanged)
	})
}

// 读取期间发生 Purge：两次读到的作用域不同时重读，返回新作用域的数据。
func TestReadRetriesAcrossPurge(t *testing.T) {
	ctx := context.Background()
	st := newHookStore(t)
	m := durableMemory(t, st, Config{})
	_, err := m.mutate(ctx, testNS, "", addRecord("old"))
	require.NoError(t, err)
	st.h.before = func(op, scope, id string) error {
		if op == "load" && id == headID && scope == scopeName(testNS, initialScope) {
			st.h.before = nil
			setMeta(t, st, testNS, metaState{Current: "new", Purges: 1})
		}
		return nil
	}
	scope, s, err := m.read(ctx, testNS)
	require.NoError(t, err)
	require.Equal(t, "new", scope)
	require.Empty(t, s.Records, "不返回 Purge 之前的数据")
}

// 锁丢失（pgstore 的租约被接管）后接管者先提交：结果是确定的失败，mutate 整体重做，两次写入都生效。
func TestMutateRedoesAfterLeaseLost(t *testing.T) {
	ctx := context.Background()
	st := newHookStore(t)
	m := durableMemory(t, st, Config{})
	// 接管者用独立的钩子，与本方共享底层存储。
	other := durableMemory(t, &hookStore{inner: st.inner, root: st.root, h: &storeHooks{held: map[string]int{}}}, Config{})
	type result struct {
		s   state
		err error
	}
	taken := make(chan result, 1)
	st.h.before = func(op, _, id string) error {
		if op != "save" || id != headID {
			return nil
		}
		// 模拟接管者在本次 Save 之前拿到锁：它在本方释放锁后提交，本方重做时等它提交完再加锁。
		st.h.before = func(op, _, id string) error {
			if op == "lock" && id == headID {
				st.h.before = nil
				r := <-taken
				taken <- r
			}
			return nil
		}
		go func() {
			s, err := other.mutate(ctx, testNS, "", addRecord("taker"))
			taken <- result{s, err}
		}()
		return agentErrors.ErrLeaseLost
	}
	mine, err := m.mutate(ctx, testNS, "", addRecord("mine"))
	require.NoError(t, err)
	r := <-taken
	require.NoError(t, r.err)
	require.Equal(t, int64(1), r.s.Seq, "接管者先提交")
	require.Equal(t, int64(2), mine.Seq, "本方在接管者的提交之上重做")
	_, s, err := m.read(ctx, testNS)
	require.NoError(t, err)
	require.Len(t, s.Records, 2, "被接管后的写入与本方重做的写入都存在")
}

func TestMutateUnknownCommitOutcome(t *testing.T) {
	ctx := context.Background()
	t.Run("实际已提交：按 CommitID 判定为成功", func(t *testing.T) {
		st := newHookStore(t)
		m := durableMemory(t, st, Config{})
		st.h.afterSave = func(string, string) error { st.h.afterSave = nil; return errors.New("连接中断") }
		committed, err := m.mutate(ctx, testNS, "", addRecord("a"))
		require.NoError(t, err)
		_, s, err := m.read(ctx, testNS)
		require.NoError(t, err)
		require.Equal(t, committed.CommitID, s.CommitID)
	})
	t.Run("没有提交：返回 ErrMemoryCommitUnknown，不误判", func(t *testing.T) {
		st := newHookStore(t)
		m := durableMemory(t, st, Config{})
		st.h.before = func(op, _, id string) error {
			if op == "save" && id == headID {
				st.h.before = nil
				return errors.New("连接中断")
			}
			return nil
		}
		_, err := m.mutate(ctx, testNS, "", addRecord("a"))
		require.ErrorIs(t, err, agentErrors.ErrMemoryCommitUnknown)
		require.ErrorContains(t, err, "连接中断")
	})
	t.Run("提交时作用域已被删除：视为作用域改变", func(t *testing.T) {
		st := newHookStore(t)
		m := durableMemory(t, st, Config{})
		st.h.before = func(op, _, id string) error {
			if op == "save" && id == headID {
				st.h.before = nil
				return agentErrors.ErrScopeRemoved
			}
			return nil
		}
		_, err := m.mutate(ctx, testNS, "", addRecord("a"))
		require.ErrorIs(t, err, errScopeChanged)
	})
}

func TestMutateStateSizeLimit(t *testing.T) {
	ctx := context.Background()
	big := func(id string) func(*tx) error {
		return func(t *tx) error {
			t.Records.Put(id, Record{ID: id, Content: strings.Repeat("长", 300)})
			return nil
		}
	}
	// 上限按逻辑大小：容得下一条大记录、容不下两条。
	one := recordKind.size("a", Record{ID: "a", Content: strings.Repeat("长", 300)})
	m := durableMemory(t, newHookStore(t), Config{MaxStateBytes: one + 300})
	var err error
	_, err = m.mutate(ctx, testNS, "", big("a"))
	require.NoError(t, err)
	_, err = m.mutate(ctx, testNS, "", big("b"))
	require.Error(t, err, "变大并且超过上限时拒绝")
	require.True(t, agentErrors.IsCode(err, agentErrors.CodeStateSave))

	// 已经超限的 state：删除类操作总能提交，不变大的修改同样放行。
	_, err = m.mutate(ctx, testNS, "", big("b"), shrinking())
	require.NoError(t, err)
	_, err = m.mutate(ctx, testNS, "", func(t *tx) error {
		r, _ := t.Records.Get("a")
		r.InvalidAt = time.Unix(1, 0)
		t.Records.Put("a", r)
		return nil
	}, shrinking())
	require.NoError(t, err)
	_, err = m.mutate(ctx, testNS, "", func(t *tx) error { t.Records.Delete("a"); return nil })
	require.NoError(t, err, "让 state 变小的修改不受限制")
}

func TestStateSnapshotVersionAndConfig(t *testing.T) {
	ctx := context.Background()
	st := newHookStore(t)
	m := durableMemory(t, st, Config{})
	unlock, err := st.Lock(ctx, metaID(testNS))
	require.NoError(t, err)
	require.NoError(t, st.Save(ctx, metaID(testNS), &durable.Snapshot{Version: "memory-state/99", Plan: statePlan, Data: []byte(`{}`)}))
	require.NoError(t, unlock())
	_, _, err = m.read(ctx, testNS)
	require.ErrorContains(t, err, "无法识别")

	fs, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	_, err = New(Config{Store: storemem.New(), Durable: notScoped{fs}})
	require.Error(t, err, "Durable 须实现 Scoped")
}

// notScoped 只暴露 durable.Store 的方法。
type notScoped struct{ durable.Store }
