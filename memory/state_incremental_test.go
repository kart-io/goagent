package memory

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
)

// 增量提交的参考模型测试与故障注入，设计见 docs/design/MEMORY_G1_INCREMENTAL.md 第 3～6、8 节。

// smallCompaction 把压缩阈值调小，使测试中频繁压缩。
func smallCompaction(m *Memory) *Memory {
	m.overlaySoft, m.overlayHard, m.overlayItems = 2<<10, 64<<10, 12
	return m
}

// manualCompaction 关闭自动压缩（只保留硬上限），故障注入测试据此精确控制压缩的时机。
func manualCompaction(m *Memory) *Memory {
	m.overlaySoft, m.overlayItems, m.baseShare = 1<<30, 1<<30, 0
	return m
}

// model 是参考模型：逐笔应用同样的修改，与 materialize 的结果比较。
type model struct {
	s state
}

func newModel() *model {
	m := &model{}
	m.s.init()
	return m
}

// op 是一笔随机修改，同时作用于 tx 与参考模型。
type op struct {
	name  string
	apply func(t *tx)
	model func(s *state)
}

func randomOp(rng *rand.Rand, ns []string) op {
	id := fmt.Sprintf("k%02d", rng.IntN(40))
	text := strings.Repeat(string(rune(0x4e00+rng.IntN(2000))), 1+rng.IntN(60))
	switch rng.IntN(9) {
	case 0, 1, 2:
		r := Record{ID: id, Content: text, Keywords: []string{text[:3]}, Importance: rng.Float64()}
		norm := r
		recordKind.normalize(&norm, ns)
		return op{"put record " + id, func(t *tx) { t.Records.Put(id, r) }, func(s *state) { s.Records[id] = norm }}
	case 3:
		return op{"delete record " + id, func(t *tx) { t.Records.Delete(id) }, func(s *state) { delete(s.Records, id) }}
	case 4:
		e := Edge{ID: id, Subject: "a", Object: "b", Fact: text}
		norm := e
		edgeKind.normalize(&norm, ns)
		return op{"put edge " + id, func(t *tx) { t.Edges.Put(id, e) }, func(s *state) { s.Edges[id] = norm }}
	case 5:
		return op{"delete edge " + id, func(t *tx) { t.Edges.Delete(id) }, func(s *state) { delete(s.Edges, id) }}
	case 6:
		e := Entity{ID: id, Name: text, Aliases: []string{}}
		norm := e
		entityKind.normalize(&norm, ns)
		return op{"put entity " + id, func(t *tx) { t.Entities.Put(id, e) }, func(s *state) { s.Entities[id] = norm }}
	case 7:
		at := time.Unix(int64(rng.IntN(1e6)), 0).UTC()
		j := Job{Key: id, Session: text, Status: JobStatus("pending")}
		norm := j
		jobKind.normalize(&norm, ns)
		return op{"job " + id, func(t *tx) { t.Jobs.Delete(id); t.DoneJobs.Put(id, at) }, func(s *state) { delete(s.Jobs, id); s.DoneJobs[id] = at }}
	default:
		n := rng.Int64N(1000)
		return op{"scalars", func(t *tx) { t.SetNextIncarnation(n); t.SetLastForgetSeq(n / 2) }, func(s *state) { s.NextIncarnation, s.LastForgetSeq = n, n/2 }}
	}
}

// requireMatches 断言 got 与参考模型的内容相同（不比较 Seq、CommitID）。
func requireMatches(t *testing.T, want, got state, msg string) {
	t.Helper()
	got.init()
	require.Equal(t, want.Records, got.Records, "%s：记录", msg)
	require.Equal(t, want.Entities, got.Entities, "%s：实体", msg)
	require.Equal(t, want.Edges, got.Edges, "%s：关系", msg)
	require.Equal(t, want.Jobs, got.Jobs, "%s：任务", msg)
	require.Equal(t, want.DoneJobs, got.DoneJobs, "%s：已完成任务", msg)
	require.Equal(t, want.NextIncarnation, got.NextIncarnation, "%s：NextIncarnation", msg)
	require.Equal(t, want.LastForgetSeq, got.LastForgetSeq, "%s：LastForgetSeq", msg)
}

func stateJSON(t *testing.T, s state) string {
	t.Helper()
	s.init()
	b, err := json.Marshal(s)
	require.NoError(t, err)
	return string(b)
}

// 参考模型：随机修改序列在两个实例上交替执行，频繁压缩（自动与强制），每一步后三条读取路径都与模型相同：
// 写入方的缓存、另一个实例的缓存、不用缓存的完整还原；缓存路径与完整还原逐字节等价。
func TestIncrementalMatchesReferenceModel(t *testing.T) {
	for _, seed := range []uint64{1, 2, 3, 4} {
		t.Run(fmt.Sprint("seed", seed), func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			open := func(cache int) *Memory {
				fs, err := durable.NewFileStore(dir)
				require.NoError(t, err)
				return smallCompaction(durableMemory(t, fs, Config{StateCacheBytes: cache}))
			}
			ms := []*Memory{open(0), open(0)}
			fresh := open(-1)
			rng := rand.New(rand.NewPCG(seed, 99))
			ref := newModel()
			compactions := 0
			for step := range 300 {
				m := ms[rng.IntN(2)]
				switch r := rng.IntN(20); {
				case r == 0:
					require.NoError(t, m.compact(ctx, testNS, true, true), "强制压缩")
					compactions++
				case r == 1:
					_, err := m.mutate(ctx, testNS, "", func(t *tx) error {
						randomOp(rng, testNS).apply(t)
						return errors.New("放弃")
					})
					require.Error(t, err, "修改函数出错时不提交")
				default:
					ops := make([]op, 1+rng.IntN(4))
					for i := range ops {
						ops[i] = randomOp(rng, testNS)
					}
					_, err := m.mutate(ctx, testNS, "", func(t *tx) error {
						for _, o := range ops {
							o.apply(t)
						}
						return nil
					})
					require.NoError(t, err, "第 %d 步", step)
					for _, o := range ops {
						o.model(&ref.s)
					}
				}
				_, full, err := fresh.read(ctx, testNS)
				require.NoError(t, err)
				requireMatches(t, ref.s, full, fmt.Sprintf("第 %d 步，完整还原", step))
				for i, mm := range ms {
					_, s, err := mm.read(ctx, testNS)
					require.NoError(t, err)
					require.Equal(t, stateJSON(t, full), stateJSON(t, s), "第 %d 步，实例 %d 的缓存路径与完整还原逐字节等价", step, i)
				}
			}
			require.Positive(t, compactions)
			h, err := loadHead(ctx, ms[0].scoped(testNS, initialScope))
			require.NoError(t, err)
			require.NotEmpty(t, h.Base.CommitID, "发生过压缩")
			require.Zero(t, ms[0].compactErrors.Load()+ms[1].compactErrors.Load(), "自动压缩没有失败")
		})
	}
}

// 并发（-race）：两个实例各 3 个写者（键空间互不相交）与 2 个读者，压缩频繁发生；结束时与按写者分别应用的模型相同，
// 读者看到的 Seq 单调不减。
func TestIncrementalConcurrentWithCompaction(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	open := func() *Memory {
		fs, err := durable.NewFileStore(dir)
		require.NoError(t, err)
		return smallCompaction(durableMemory(t, fs, Config{StateCacheBytes: 1 << 20}))
	}
	ms := []*Memory{open(), open()}
	refs := make([]*model, 6)
	// 删除不存在的键没有修改，不提交。
	var commits atomic.Int64
	var wg sync.WaitGroup
	for w := range refs {
		refs[w] = newModel()
		wg.Go(func() {
			rng := rand.New(rand.NewPCG(uint64(w), 7))
			for range 25 {
				id := fmt.Sprintf("w%d-%02d", w, rng.IntN(10))
				r := Record{ID: id, Content: fmt.Sprint(rng.Int())}
				norm := r
				recordKind.normalize(&norm, testNS)
				del := rng.IntN(4) == 0
				_, err := ms[w%2].mutate(ctx, testNS, "", func(t *tx) error {
					if del {
						t.Records.Delete(id)
					} else {
						t.Records.Put(id, r)
					}
					return nil
				})
				if !assert.NoError(t, err) {
					return
				}
				if _, had := refs[w].s.Records[id]; had || !del {
					commits.Add(1)
				}
				if del {
					delete(refs[w].s.Records, id)
				} else {
					refs[w].s.Records[id] = norm
				}
			}
		})
	}
	done := make(chan struct{})
	var readers sync.WaitGroup
	for r := range 4 {
		readers.Go(func() {
			var last int64
			for {
				select {
				case <-done:
					return
				default:
				}
				_, s, err := ms[r%2].read(ctx, testNS)
				if !assert.NoError(t, err) || !assert.GreaterOrEqual(t, s.Seq, last) {
					return
				}
				last = s.Seq
			}
		})
	}
	wg.Wait()
	close(done)
	readers.Wait()
	want := newModel()
	for _, r := range refs {
		maps.Copy(want.s.Records, r.s.Records)
	}
	_, got, err := ms[0].read(ctx, testNS)
	require.NoError(t, err)
	require.Equal(t, want.s.Records, got.Records)
	require.Equal(t, commits.Load(), got.Seq)
	checkInvariants(t, testNS, got)
}

// seedAndCompact 写入 n 条记录并强制压缩，返回存储与实例。
func seedAndCompact(t *testing.T, n int) (*hookStore, *Memory) {
	t.Helper()
	ctx := context.Background()
	st := newHookStore(t)
	m := manualCompaction(durableMemory(t, st, Config{}))
	for i := range n {
		_, err := m.mutate(ctx, testNS, "", addRecord(fmt.Sprintf("r%02d", i)))
		require.NoError(t, err)
	}
	require.NoError(t, m.compact(ctx, testNS, true, true))
	return st, m
}

func headOf(t *testing.T, m *Memory) head {
	t.Helper()
	h, err := loadHead(context.Background(), m.scoped(testNS, initialScope))
	require.NoError(t, err)
	return h
}

// 4.4 的每一行：压缩在任何一步失败或崩溃，state 都是最后一次成功提交的内容，下一次压缩成功。
func TestCompactionFaults(t *testing.T) {
	ctx := context.Background()
	fail := func(st *hookStore, op, id string, err error) {
		st.h.before = func(o, _, i string) error {
			if o == op && i == id {
				st.h.before = nil
				return err
			}
			return nil
		}
	}
	verify := func(t *testing.T, m *Memory, want int) {
		t.Helper()
		_, s, err := durableMemory(t, m.cfg.Durable, Config{StateCacheBytes: -1}).read(ctx, testNS)
		require.NoError(t, err)
		require.Len(t, s.Records, want)
		require.NoError(t, m.compact(ctx, testNS, true, true), "下一次压缩成功")
		_, s, err = durableMemory(t, m.cfg.Durable, Config{StateCacheBytes: -1}).read(ctx, testNS)
		require.NoError(t, err)
		require.Len(t, s.Records, want)
	}
	t.Run("写基础快照失败：head 不变", func(t *testing.T) {
		st, m := seedAndCompact(t, 5)
		_, err := m.mutate(ctx, testNS, "", addRecord("x"))
		require.NoError(t, err)
		before := headOf(t, m)
		fail(st, "save", baseID(1-before.Base.Slot), errors.New("磁盘满"))
		require.Error(t, m.compact(ctx, testNS, true, true))
		require.Equal(t, before.CommitID, headOf(t, m).CommitID)
		verify(t, m, 6)
	})
	t.Run("切换 head 时连接中断且未提交：结果未知，新槽位不被引用", func(t *testing.T) {
		st, m := seedAndCompact(t, 5)
		_, err := m.mutate(ctx, testNS, "", addRecord("x"))
		require.NoError(t, err)
		before := headOf(t, m)
		fail(st, "save", headID, errors.New("连接中断"))
		require.ErrorIs(t, m.compact(ctx, testNS, true, true), agentErrors.ErrMemoryCommitUnknown)
		require.Equal(t, before.CommitID, headOf(t, m).CommitID)
		verify(t, m, 6)
	})
	t.Run("切换 head 已提交但调用方看到失败：按 CommitID 判为成功", func(t *testing.T) {
		st, m := seedAndCompact(t, 5)
		_, err := m.mutate(ctx, testNS, "", addRecord("x"))
		require.NoError(t, err)
		before := headOf(t, m)
		st.h.afterSave = func(_, id string) error {
			if id == headID {
				st.h.afterSave = nil
				return errors.New("连接中断")
			}
			return nil
		}
		require.NoError(t, m.compact(ctx, testNS, true, true))
		after := headOf(t, m)
		require.NotEqual(t, before.Base.CommitID, after.Base.CommitID)
		require.Zero(t, after.OverlayItems)
		verify(t, m, 6)
	})
	t.Run("切换之后、擦除旧槽位之前崩溃：标记保留，下一次写入触发擦除", func(t *testing.T) {
		st, m := seedAndCompact(t, 5)
		_, err := m.mutate(ctx, testNS, "", addRecord("x"))
		require.NoError(t, err)
		old := headOf(t, m).Base.Slot
		fail(st, "save", baseID(old), errors.New("进程退出"))
		require.Error(t, m.compact(ctx, testNS, true, true))
		h := headOf(t, m)
		require.NotNil(t, h.StaleSlot)
		require.Equal(t, old, *h.StaleSlot)
		_, err = m.mutate(ctx, testNS, "", addRecord("y"))
		require.NoError(t, err)
		h = headOf(t, m)
		require.Nil(t, h.StaleSlot, "写入之后的压缩完成了擦除")
		// 之后的压缩把新基础快照写进了旧槽位（覆盖旧内容），并擦除了刚切换走的另一个槽位：非活动槽位总是占位快照。
		snap, err := m.scoped(testNS, initialScope).Load(ctx, baseID(1-h.Base.Slot))
		require.NoError(t, err)
		blob, err := decodeBlob(snap.Data)
		require.NoError(t, err)
		require.Equal(t, "scrubbed", blob.CommitID)
		verify(t, m, 7)
	})
	t.Run("压缩期间发生 Purge：放弃切换，新作用域没有数据", func(t *testing.T) {
		st, m := seedAndCompact(t, 5)
		_, err := m.mutate(ctx, testNS, "", addRecord("x"))
		require.NoError(t, err)
		h := headOf(t, m)
		// 在新基础快照写入之后切换代际（Purge 的提交点）；FileStore 的 RemoveScope 会等待持锁者，所以在压缩返回之后再删除旧作用域。
		st.h.afterSave = func(_, id string) error {
			if id == baseID(1-h.Base.Slot) {
				st.h.afterSave = nil
				setMeta(t, st, testNS, metaState{Current: "g2", Purges: 1})
			}
			return nil
		}
		require.ErrorIs(t, m.compact(ctx, testNS, true, true), errScopeChanged)
		require.NoError(t, st.RemoveScope(ctx, scopeName(testNS, initialScope)))
		_, s, err := m.read(ctx, testNS)
		require.NoError(t, err)
		require.Empty(t, s.Records)
		_, err = st.Scope(scopeName(testNS, "g2")).Load(ctx, headID)
		require.ErrorIs(t, err, os.ErrNotExist)
	})
	t.Run("同一作用域并发压缩：只有一个压缩者，另一个跳过", func(t *testing.T) {
		st, m := seedAndCompact(t, 5)
		other := manualCompaction(durableMemory(t, st, Config{}))
		_, err := m.mutate(ctx, testNS, "", addRecord("x"))
		require.NoError(t, err)
		st.h.before = func(o, _, id string) error {
			if o == "save" && strings.HasPrefix(id, "memory/base/") {
				st.h.before = nil
				require.NoError(t, other.compact(ctx, testNS, true, false), "拿不到压缩锁时跳过而不是等待或报错")
			}
			return nil
		}
		require.NoError(t, m.compact(ctx, testNS, true, true))
		verify(t, m, 6)
	})
}

// 读者在两次压缩之间读取：它依据的基础快照槽位已被改写，按 commit_id 发现后从 Load(head) 重来，结果正确。
func TestReadRetriesWhenBaseRewritten(t *testing.T) {
	ctx := context.Background()
	st, m := seedAndCompact(t, 5)
	reader := durableMemory(t, st, Config{StateCacheBytes: -1})
	writer := manualCompaction(durableMemory(t, st, Config{}))
	_, err := m.mutate(ctx, testNS, "", addRecord("x"))
	require.NoError(t, err)
	active := headOf(t, m).Base.Slot
	st.h.before = func(o, _, id string) error {
		if o == "load" && id == baseID(active) {
			st.h.before = nil
			// 两次压缩：第二次改写读者正要读的槽位。
			for i := range 2 {
				_, err := writer.mutate(ctx, testNS, "", addRecord(fmt.Sprintf("c%d", i)))
				require.NoError(t, err)
				require.NoError(t, writer.compact(ctx, testNS, true, true))
			}
		}
		return nil
	}
	_, s, err := reader.read(ctx, testNS)
	require.NoError(t, err)
	require.Len(t, s.Records, 8)
	require.Contains(t, s.Records, "c1")
}

// scopeText 返回作用域内全部快照（head 的覆盖层与两个槽位）解码后的文本，用于检查物理擦除。
func scopeText(t *testing.T, m *Memory) string {
	t.Helper()
	ctx := context.Background()
	st := m.scoped(testNS, initialScope)
	var b strings.Builder
	h, err := loadHead(ctx, st)
	require.NoError(t, err)
	ov, err := decodeOverlay(h, testNS)
	require.NoError(t, err)
	b.WriteString(fmt.Sprintf("%+v", ov))
	for slot := range 2 {
		snap, err := st.Load(ctx, baseID(slot))
		if err != nil {
			continue
		}
		blob, err := decodeBlob(snap.Data)
		require.NoError(t, err)
		s, err := decodeState(blob, testNS)
		require.NoError(t, err)
		b.WriteString(stateJSON(t, s))
	}
	return b.String()
}

// 删除优先：被删除或标记擦除的内容在 EraseWithin 之后从所有快照中消失（增量提交第 6 节）。
func TestErasureWithinDeadline(t *testing.T) {
	ctx := context.Background()
	for name, forget := range map[string]func(*tx){
		"删除": func(t *tx) { t.Records.Delete("secret") },
		"遗忘（保留记录、清空内容）": func(t *tx) { t.Records.Put("secret", Record{ID: "secret"}); t.Records.MarkErasure("secret") },
	} {
		t.Run(name, func(t *testing.T) {
			st := newHookStore(t)
			m := manualCompaction(durableMemory(t, st, Config{}))
			now := time.Unix(1_800_000_000, 0)
			m.now = func() time.Time { return now }
			_, err := m.mutate(ctx, testNS, "", func(t *tx) error {
				t.Records.Put("secret", Record{ID: "secret", Content: "机密内容甲乙丙"})
				return nil
			})
			require.NoError(t, err)
			require.NoError(t, m.compact(ctx, testNS, true, true))
			// 再压缩一次，让两个槽位都含有这段内容。
			_, err = m.mutate(ctx, testNS, "", addRecord("other"))
			require.NoError(t, err)
			require.NoError(t, m.compact(ctx, testNS, true, true))
			require.Contains(t, scopeText(t, m), "机密内容甲乙丙")

			_, err = m.mutate(ctx, testNS, "", func(t *tx) error { forget(t); return nil })
			require.NoError(t, err)
			h := headOf(t, m)
			require.False(t, h.ErasePending.IsZero(), "基础快照仍有旧内容：标记待擦除")
			_, s, err := m.read(ctx, testNS)
			require.NoError(t, err)
			require.NotContains(t, stateJSON(t, s), "机密内容甲乙丙", "读取立即看不到")

			now = now.Add(m.eraseWithin)
			_, err = m.mutate(ctx, testNS, "", addRecord("trigger"))
			require.NoError(t, err)
			require.NotContains(t, scopeText(t, m), "机密内容甲乙丙", "时限到达后的写入触发压缩与擦除")
			h = headOf(t, m)
			require.True(t, h.ErasePending.IsZero())
			require.Nil(t, h.StaleSlot)
		})
	}
}

// 幂等键：同一个键重试不再修改；只保留最近 opsKeep 个。
func TestMutateIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	m := durableMemory(t, newHookStore(t), Config{})
	first, err := m.mutate(ctx, testNS, "", addRecord("a"), withOpKey("turn-1/save-1"))
	require.NoError(t, err)
	again, err := m.mutate(ctx, testNS, "", func(t *tx) error {
		t.Records.Put("dup", Record{ID: "dup"})
		return nil
	}, withOpKey("turn-1/save-1"))
	require.NoError(t, err)
	require.Equal(t, first.Seq, again.Seq, "重试不产生新提交")
	require.NotContains(t, again.Records, "dup")
	for i := range opsKeep + 5 {
		_, err := m.mutate(ctx, testNS, "", addRecord(fmt.Sprint("r", i)), withOpKey(fmt.Sprint("op-", i)))
		require.NoError(t, err)
	}
	h := headOf(t, m)
	require.Len(t, h.Ops, opsKeep)
	require.NotContains(t, h.Ops, "turn-1/save-1", "最早的键被裁剪")
}

// 覆盖层达到硬上限：写入方在释放 head 锁后同步压缩，head 的覆盖层清空。
func TestOverlayHardLimitCompactsSynchronously(t *testing.T) {
	ctx := context.Background()
	m := manualCompaction(durableMemory(t, newHookStore(t), Config{}))
	m.overlayHard = 4 << 10
	for i := 0; headOf(t, m).Base.CommitID == ""; i++ {
		require.Less(t, i, 200)
		_, err := m.mutate(ctx, testNS, "", func(t *tx) error {
			t.Records.Put(fmt.Sprint("r", i), Record{ID: fmt.Sprint("r", i), Content: strings.Repeat("字", 100)})
			return nil
		})
		require.NoError(t, err)
	}
	require.Zero(t, headOf(t, m).OverlayItems)
}

func TestOverlayIntegrity(t *testing.T) {
	ctx := context.Background()
	m := durableMemory(t, newHookStore(t), Config{})
	_, err := m.mutate(ctx, testNS, "", addRecord("a"))
	require.NoError(t, err)
	h := headOf(t, m)
	for name, mangle := range map[string]func(*head){
		"长度被改":     func(h *head) { h.OverlayBytes-- },
		"项数被改":     func(h *head) { h.OverlayItems++ },
		"变更不在序号区间": func(h *head) { h.Seq = 0 },
		"数据被改":     func(h *head) { h.Overlay = h.Overlay[:len(h.Overlay)-8] + "AAAAAAA=" },
	} {
		bad := h
		mangle(&bad)
		_, err := decodeOverlay(bad, testNS)
		require.Error(t, err, name)
		require.True(t, agentErrors.IsCode(err, agentErrors.CodeStateLoad), name)
	}
}
