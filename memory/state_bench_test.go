package memory

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
)

// textGen 生成 n 个字的中文文本。
type textGen func(rng *rand.Rand, n int) string

// benchDatasets 是基准的两组数据（docs/design/MEMORY_G1_STORAGE.md 7.3）：
//   - 真实中文：从本仓库 docs/design 的中文正文中截取片段，接近真实记忆的用词重复度；
//   - 低压缩率：均匀随机的常用汉字，几乎没有可压缩的重复，是压缩率的下界。
//
// 两组的 ID、会话与任务键都是 UUID，与 Remember 生成的 ID 相同。
func benchDatasets(t testing.TB) []struct {
	name string
	gen  textGen
} {
	t.Helper()
	files, err := filepath.Glob("../docs/design/*.md")
	require.NoError(t, err)
	var corpus []rune
	for _, f := range files {
		data, err := os.ReadFile(f)
		require.NoError(t, err)
		for line := range strings.SplitSeq(string(data), "\n") {
			han := 0
			for _, r := range line {
				if unicode.Is(unicode.Han, r) {
					han++
				}
			}
			if han > 10 && han*2 > len([]rune(line)) {
				corpus = append(corpus, []rune(strings.TrimLeft(line, "-*#> 0123456789."))...)
			}
		}
	}
	require.Greater(t, len(corpus), 10_000, "中文语料不足")
	return []struct {
		name string
		gen  textGen
	}{
		{"真实中文", func(rng *rand.Rand, n int) string { o := rng.IntN(len(corpus) - n); return string(corpus[o : o+n]) }},
		{"低压缩率", func(rng *rand.Rand, n int) string {
			r := make([]rune, n)
			for i := range r {
				r[i] = rune(0x4e00 + rng.IntN(0x9fa5-0x4e00))
			}
			return string(r)
		}},
	}
}

// benchState 按 7.3 的规模构造 state：n 条记录（30% 失效）、n 个实体、2n 条关系、n/20 个社区、3000 个已完成任务键。
func benchState(n int, gen textGen) func(*tx) error {
	return func(t *tx) error {
		rng := rand.New(rand.NewPCG(uint64(n), 7))
		now := time.Unix(1_800_000_000, 0).UTC()
		at := func() time.Time { return now.Add(time.Duration(rng.IntN(1e7)) * time.Second) }
		ents, names := make([]string, n), make([]string, n)
		for i := range n {
			ents[i], names[i] = uuid.NewString(), gen(rng, 4)
			alias := gen(rng, 3)
			origin := func() []Origin {
				return []Origin{{Record: RecordRef{ID: fmt.Sprintf("rec-%06d", rng.IntN(n))}, Source: Source{Session: uuid.NewString(), From: rng.Int64N(1e5)}}}
			}
			t.Entities.Put(ents[i], Entity{ID: ents[i], Name: names[i], Type: gen(rng, 2), Aliases: []string{alias}, CreatedAt: at(), Live: true,
				Names: []EntityName{{Name: names[i], Origins: origin(), Live: true}, {Name: alias, Origins: origin(), Live: true}}})
		}
		for i := range n {
			r := Record{ID: fmt.Sprintf("rec-%06d", i), Kind: KindFact, Content: gen(rng, 40+rng.IntN(120)),
				Keywords: []string{gen(rng, 2), gen(rng, 3), gen(rng, 2), gen(rng, 4)}, Entities: []string{names[rng.IntN(n)], names[rng.IntN(n)]},
				Importance: rng.Float64(), ValidFrom: at(), CreatedAt: at(),
				Sources: []Source{{Session: uuid.NewString(), From: rng.Int64N(1e5), To: rng.Int64N(1e5), Job: uuid.NewString()}}}
			if i%10 < 3 {
				r.InvalidAt, r.InvalidReason = at(), ReasonExpired
			}
			t.Records.Put(r.ID, r)
		}
		for range 2 * n {
			id := uuid.NewString()
			e := Edge{ID: id, Subject: ents[rng.IntN(n)], Predicate: gen(rng, 2), Object: ents[rng.IntN(n)],
				Fact: gen(rng, 15+rng.IntN(30)), ValidFrom: at(), CreatedAt: at()}
			src := Source{Session: uuid.NewString(), From: rng.Int64N(1e5)}
			e.Sources = []Source{src}
			e.Derivations = []Derivation{{Origin: Origin{Record: RecordRef{ID: fmt.Sprintf("rec-%06d", rng.IntN(n))}, Source: src}, Fact: e.Fact, At: e.ValidFrom}}
			t.Edges.Put(id, e)
		}
		for range n / 20 {
			id := uuid.NewString()
			t.Communities.Put(id, Community{ID: id, Members: []string{ents[rng.IntN(n)], ents[rng.IntN(n)], ents[rng.IntN(n)]},
				Name: gen(rng, 6), Summary: gen(rng, 200), UpdatedAt: now})
		}
		for i := range 3000 {
			t.DoneJobs.Put(fmt.Sprintf("%s/%d", uuid.NewString(), i), now)
		}
		t.SetNextIncarnation(int64(n*4 + n/20))
		return nil
	}
}

// benchNS 是基准的命名空间：与生产相同，由租户与用户两个较长的 ID 组成。
var benchNS = []string{"tenant-6f1c2b9e-3a7d-4f0e-9b1a-2c3d4e5f6a7b", "user-0a1b2c3d-4e5f-6789-abcd-ef0123456789"}

func percentile(d []time.Duration, p float64) time.Duration {
	d = slices.Clone(d)
	slices.Sort(d)
	return d[int(float64(len(d)-1)*p)]
}

func mean(d []time.Duration) time.Duration {
	var sum time.Duration
	for _, v := range d {
		sum += v
	}
	return sum / time.Duration(len(d))
}

func timed(n int, f func()) []time.Duration {
	out := make([]time.Duration, n)
	for i := range out {
		t0 := time.Now()
		f()
		out[i] = time.Since(t0)
	}
	return out
}

func ms(d time.Duration) string { return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond)) }

// benchDir 返回基准使用的目录：MEMORY_STATE_BENCH_DIR（应在 SSD 上，tmpfs 会低估 fsync）下的临时目录。
func benchDir(t *testing.T) string {
	t.Helper()
	root := os.Getenv("MEMORY_STATE_BENCH_DIR")
	if root == "" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp(root, "memory-state-bench-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// benchMemory 在 dir 上打开一个新的实例（独立的 FileStore，等同另一个进程的视图）。
func benchMemory(t *testing.T, dir string, cacheBytes int) *Memory {
	t.Helper()
	fs, err := durable.NewFileStore(dir)
	require.NoError(t, err)
	return durableMemory(t, fs, Config{MaxStateBytes: 1 << 30, StateCacheBytes: cacheBytes, Graph: true})
}

// TestStateBenchmarkReport 是 G1a 的原型基准，需要 MEMORY_STATE_BENCH=1。5k 条记录时两组数据都按 7.3 的硬性阈值判定：
// mutate p95 ≤ 100ms，召回读取（冷、热）p95 ≤ 30ms，单命名空间串行 ≥ 20 次/秒。
func TestStateBenchmarkReport(t *testing.T) {
	if os.Getenv("MEMORY_STATE_BENCH") != "1" {
		t.Skip("设置 MEMORY_STATE_BENCH=1 运行原型基准")
	}
	ctx := context.Background()
	t.Logf("GOMAXPROCS=%d", runtime.GOMAXPROCS(0))
	t.Logf("| 数据 | 记录数 | 解压后 | 落盘 | mutate p50 | mutate p95 | p99 | 压缩次数/耗时 | 串行吞吐 | 4 写者吞吐 | 热读 均值/p95 | 冷读 均值/p95 | 跨实例读 均值/p95 | 1写5读命中率 | 缓存堆占用 | 索引构建 | 召回计算 p95 | 端到端召回 热 p95 | 端到端召回 跨实例 p95 | 冷召回 p95 | 完整召回 热 p95 | 完整召回 跨实例 p95 |")
	sizes := []int{1000, 5000, 10000}
	if v := os.Getenv("MEMORY_STATE_BENCH_SIZES"); v != "" {
		sizes = nil
		for f := range strings.SplitSeq(v, ",") {
			n, err := strconv.Atoi(f)
			require.NoError(t, err)
			sizes = append(sizes, n)
		}
	}
	for _, ds := range benchDatasets(t) {
		for _, n := range sizes {
			dir := benchDir(t)
			m := benchMemory(t, dir, 0)
			_, err := m.mutate(ctx, benchNS, "", benchState(n, ds.gen))
			require.NoError(t, err)

			const writes = 600
			compactions0 := m.compactions.Load()
			i := 0
			start := time.Now()
			lat := timed(writes, func() {
				_, err := m.mutate(ctx, benchNS, "", func(x *tx) error {
					r, _ := x.Records.Get(fmt.Sprintf("rec-%06d", i%n))
					r.Importance += 0.001
					x.Records.Put(r.ID, r)
					i++
					return nil
				})
				require.NoError(t, err)
			})
			serial := float64(writes) / time.Since(start).Seconds()
			compactions := m.compactions.Load() - compactions0
			compactT := timed(3, func() { require.NoError(t, m.compact(ctx, benchNS, true, true)) })

			// 热读：本实例刚提交，缓存命中，只 Load 与解析版本头。
			warm := timed(30, func() { _, _, err := m.read(ctx, benchNS); require.NoError(t, err) })
			// 冷读：关闭缓存的实例，每次完整解码（进程重启后、或缓存被淘汰后的首次读取）。
			coldM := benchMemory(t, dir, -1)
			cold := timed(30, func() { _, _, err := coldM.read(ctx, benchNS); require.NoError(t, err) })

			// 跨实例：writer 每提交一次，reader 读一次，reader 每次都未命中。
			writer, reader := benchMemory(t, dir, 0), benchMemory(t, dir, 0)
			var cross []time.Duration
			for range 15 {
				_, err := writer.mutate(ctx, benchNS, "", addRecord(uuid.NewString()))
				require.NoError(t, err)
				cross = append(cross, timed(1, func() { _, _, err := reader.read(ctx, benchNS); require.NoError(t, err) })...)
			}
			// 一写五读：Worker 在一个实例提交，召回在另一个实例上每轮读取。
			before := cacheStatsOf(reader)
			for range 10 {
				_, err := writer.mutate(ctx, benchNS, "", addRecord(uuid.NewString()))
				require.NoError(t, err)
				for range 5 {
					_, _, err := reader.read(ctx, benchNS)
					require.NoError(t, err)
				}
			}
			after := cacheStatsOf(reader)
			hitRate := float64(after.Hits-before.Hits) / float64(after.Hits-before.Hits+after.Misses-before.Misses)

			// 召回（G1a 门槛：端到端，含读取、查候选、打分、排序）：热（本实例刚提交）与跨实例（另一实例刚提交）。
			_, v, err := m.readView(ctx, benchNS)
			require.NoError(t, err)
			q := Query{Text: string([]rune(v.cur.Records["rec-000015"].Content)[:16])}
			build := timed(3, func() { buildIndex(v.base.Records) })
			compute := timed(40, func() { m.rankState(v.cur, v.idx, q, nil, nil) })
			recall := timed(40, func() { _, err := m.recallState(ctx, benchNS, q); require.NoError(t, err) })
			// 冷召回：不用缓存，每次都读取、解码基础快照并等待索引构建（进程启动后的第一次召回）。
			coldRecall := timed(10, func() { _, err := coldM.recallState(ctx, benchNS, q); require.NoError(t, err) })
			var crossRecall []time.Duration
			for range 20 {
				_, err := writer.mutate(ctx, benchNS, "", addRecord(uuid.NewString()))
				require.NoError(t, err)
				crossRecall = append(crossRecall, timed(1, func() { _, err := reader.recallState(ctx, benchNS, q); require.NoError(t, err) })...)
			}
			// 完整召回（7.3 测量局限第 2 条）：一轮对话的 Snapshot（钉住、召回、图谱 PageRank）加 Hydrate 的再读。
			nsCtx := WithNamespace(ctx, benchNS...)
			turn := func(mm *Memory) {
				snap, err := mm.Snapshot(nsCtx, q.Text)
				require.NoError(t, err)
				_, _, err = mm.Hydrate(WithTurn(nsCtx, snap.Turn), snap, snap.Refs)
				require.NoError(t, err)
			}
			full := timed(40, func() { turn(m) })
			var crossFull []time.Duration
			for range 20 {
				_, err := writer.mutate(ctx, benchNS, "", addRecord(uuid.NewString()))
				require.NoError(t, err)
				crossFull = append(crossFull, timed(1, func() { turn(reader) })...)
			}

			var wg sync.WaitGroup
			start = time.Now()
			for w := range 4 {
				wg.Go(func() {
					for j := range writes / 4 {
						_, err := m.mutate(ctx, benchNS, "", addRecord(fmt.Sprintf("w%d-%d", w, j)))
						assert.NoError(t, err)
					}
				})
			}
			wg.Wait()
			concurrent := float64(writes) / time.Since(start).Seconds()

			blob, stored := benchSizes(t, m, benchNS)
			heap := heapOfDecoded(t, blob)

			t.Logf("| %s | %d | %.1f MiB | %.1f MiB | %s | %s | %s | %d / %s | %.1f/s | %.1f/s | %s / %s | %s / %s | %s / %s | %.0f%% | %.1f MiB（%.1f×） | %s | %s | %s | %s | %s | %s | %s |",
				ds.name, n, float64(blob.RawBytes)/(1<<20), float64(stored)/(1<<20),
				ms(percentile(lat, 0.5)), ms(percentile(lat, 0.95)), ms(percentile(lat, 0.99)), compactions, ms(mean(compactT)), serial, concurrent,
				ms(mean(warm)), ms(percentile(warm, 0.95)), ms(mean(cold)), ms(percentile(cold, 0.95)),
				ms(mean(cross)), ms(percentile(cross, 0.95)), hitRate*100, float64(heap)/(1<<20), float64(heap)/float64(blob.RawBytes),
				ms(mean(build)), ms(percentile(compute, 0.95)), ms(percentile(recall, 0.95)), ms(percentile(crossRecall, 0.95)), ms(percentile(coldRecall, 0.95)),
				ms(percentile(full, 0.95)), ms(percentile(crossFull, 0.95)))
			if n == 5000 {
				assert.LessOrEqual(t, percentile(lat, 0.95), 100*time.Millisecond, "%s：5k 条记录时 mutate p95 超过 100ms", ds.name)
				assert.LessOrEqual(t, percentile(warm, 0.95), 30*time.Millisecond, "%s：5k 条记录时热读 p95 超过 30ms", ds.name)
				assert.LessOrEqual(t, percentile(cold, 0.95), 30*time.Millisecond, "%s：5k 条记录时冷读 p95 超过 30ms", ds.name)
				assert.LessOrEqual(t, percentile(cross, 0.95), 30*time.Millisecond, "%s：5k 条记录时跨实例读 p95 超过 30ms", ds.name)
				assert.GreaterOrEqual(t, serial, 20.0, "%s：5k 条记录时单命名空间吞吐低于 20 次/秒", ds.name)
				assert.LessOrEqual(t, percentile(recall, 0.95), 30*time.Millisecond, "%s：5k 条记录时端到端召回（热）p95 超过 30ms", ds.name)
				assert.LessOrEqual(t, percentile(crossRecall, 0.95), 30*time.Millisecond, "%s：5k 条记录时端到端召回（跨实例）p95 超过 30ms", ds.name)
				assert.LessOrEqual(t, percentile(full, 0.95), 30*time.Millisecond, "%s：5k 条记录时完整召回（热）p95 超过 30ms", ds.name)
				assert.LessOrEqual(t, percentile(crossFull, 0.95), 30*time.Millisecond, "%s：5k 条记录时完整召回（跨实例）p95 超过 30ms", ds.name)
			}
		}
	}
}

// benchSizes 返回基础快照（解析后）与落盘的总字节数（基础快照 + head）。
func benchSizes(t *testing.T, m *Memory, ns []string) (stateBlob, int) {
	t.Helper()
	ctx := context.Background()
	st := m.scoped(ns, initialScope)
	h, err := loadHead(ctx, st)
	require.NoError(t, err)
	require.NotEmpty(t, h.Base.CommitID, "已压缩出基础快照")
	snap, err := st.Load(ctx, baseID(h.Base.Slot))
	require.NoError(t, err)
	blob, err := decodeBlob(snap.Data)
	require.NoError(t, err)
	hs, err := st.Load(ctx, headID)
	require.NoError(t, err)
	return blob, len(snap.Data) + len(hs.Data)
}

// heapOfDecoded 返回解码一份 state 增加的存活堆，即缓存一份 state 的实际内存；取 5 份的均值以减小 GC 的抖动。
func heapOfDecoded(t *testing.T, blob stateBlob) int64 {
	t.Helper()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	kept := make([]state, 5)
	for i := range kept {
		s, err := decodeState(blob, benchNS)
		require.NoError(t, err)
		kept[i] = s
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(kept)
	return (int64(after.HeapAlloc) - int64(before.HeapAlloc)) / int64(len(kept))
}

// TestStateBenchmarkEviction 测量缓存容量小于工作集时的淘汰：3 个 5k 条记录的命名空间轮流读取，缓存只容得下一份。
func TestStateBenchmarkEviction(t *testing.T) {
	if os.Getenv("MEMORY_STATE_BENCH") != "1" {
		t.Skip("设置 MEMORY_STATE_BENCH=1 运行原型基准")
	}
	ctx := context.Background()
	gen := benchDatasets(t)[0].gen
	dir := benchDir(t)
	nss := [][]string{{"t1", "a"}, {"t1", "b"}, {"t1", "c"}}
	m := benchMemory(t, dir, 0)
	for _, ns := range nss {
		_, err := m.mutate(ctx, ns, "", benchState(5000, gen))
		require.NoError(t, err)
	}
	_, s, err := m.read(ctx, nss[0])
	require.NoError(t, err)
	_, size, err := encodeState(&s)
	require.NoError(t, err)
	capacity := int(float64(size) * heapFactor * 3 / 2) // 只容得下一份
	small := benchMemory(t, dir, capacity)
	i := 0
	lat := timed(30, func() { _, _, err := small.read(ctx, nss[i%3]); require.NoError(t, err); i++ })
	stats, used := small.cache.snapshot()
	t.Logf("| 容量 %.1f MiB | 读取 %d 次 | 命中 %d | 未命中 %d | 淘汰 %d | 均值 %s | p95 %s | 占用 %.1f MiB |",
		float64(capacity)/(1<<20), len(lat), stats.Hits, stats.Misses, stats.Evictions, ms(mean(lat)), ms(percentile(lat, 0.95)), float64(used)/(1<<20))
	require.LessOrEqual(t, used, capacity, "占用不超过容量")
	require.Positive(t, stats.Evictions)
	assert.LessOrEqual(t, percentile(lat, 0.95), 30*time.Millisecond, "持续淘汰（全部冷读）时读取 p95 超过 30ms")
}

// TestStateStress10k 是 10k 条记录的压力测试，需要 MEMORY_STATE_STRESS=1（MEMORY_STATE_STRESS_SECONDS 指定时长，默认 20 秒）。
// 两个实例共用存储，共 4 个写者（新增与删除）与 4 个读者，缓存容量只够一份 state。提交时注入故障（设计 11 节 I8）：
// 写入前返回 ErrLeaseLost（mutate 重做；概率取 25%，使重做耗尽的确定失败也会出现）、写入前返回其他错误（结果未知、实际未写入）、写入后返回错误（按 CommitID 判为成功）。
// 结束时校验：每次成功的新增都在、成功删除的不再出现（无丢失更新）、确定失败的写入没有生效、Seq 等于成功写入次数、
// 读者看到的 Seq 单调不减、缓存占用不超过容量、不变量成立。Purge 的故障由 scope_test.go 的逐步崩溃测试覆盖。
func TestStateStress10k(t *testing.T) {
	if os.Getenv("MEMORY_STATE_STRESS") != "1" {
		t.Skip("设置 MEMORY_STATE_STRESS=1 运行 10k 压力测试")
	}
	duration := 20 * time.Second
	if v := os.Getenv("MEMORY_STATE_STRESS_SECONDS"); v != "" {
		d, err := time.ParseDuration(v + "s")
		require.NoError(t, err)
		duration = d
	}
	ctx := context.Background()
	dir := benchDir(t)
	seed := benchMemory(t, dir, -1)
	base, err := seed.mutate(ctx, benchNS, "", benchState(10000, benchDatasets(t)[0].gen))
	require.NoError(t, err)
	_, size, err := encodeState(&base)
	require.NoError(t, err)
	capacity := int(float64(size) * heapFactor * 3 / 2) // 只容得下一份
	fs, err := durable.NewFileStore(dir)
	require.NoError(t, err)
	hs := &hookStore{inner: fs, root: fs, h: &storeHooks{held: map[string]int{}}}
	errUnknown := errors.New("注入：连接在提交前断开")
	var leaseLost, unknown, lateFail atomic.Int64
	hs.h.before = func(op, scope, id string) error {
		if op != "save" || scope == "" || id != headID {
			return nil
		}
		switch n := rand.IntN(100); {
		case n < 25:
			leaseLost.Add(1)
			return agentErrors.ErrLeaseLost
		case n < 27:
			unknown.Add(1)
			return errUnknown
		}
		return nil
	}
	hs.h.afterSave = func(scope, id string) error {
		if scope != "" && id == headID && rand.IntN(100) < 2 {
			lateFail.Add(1)
			return errUnknown
		}
		return nil
	}
	ms2 := []*Memory{durableMemory(t, hs, Config{MaxStateBytes: 1 << 30, StateCacheBytes: capacity, Graph: true}),
		durableMemory(t, hs, Config{MaxStateBytes: 1 << 30, StateCacheBytes: capacity, Graph: true})}

	var (
		mu      sync.Mutex
		added   = map[string]bool{}
		deleted = map[string]bool{}
		// 确定失败（mutateRetries 次尝试都租约丢失）的新增与删除：都不应生效。
		notAdded   = map[string]bool{}
		notDeleted = map[string]bool{}
		commits    atomic.Int64
		reads      atomic.Int64
		failed     atomic.Int64
		writeLs    []time.Duration
		readLs     []time.Duration
	)
	deadline := time.Now().Add(duration)
	var wg sync.WaitGroup
	for w := range 4 {
		m := ms2[w%2]
		wg.Go(func() {
			for j := 0; time.Now().Before(deadline); j++ {
				id := fmt.Sprintf("w%d-%d", w, j)
				var fn func(*tx) error
				var opts []mutateOption
				switch j % 3 {
				case 0, 1:
					fn = addRecord(id)
				default:
					victim := fmt.Sprintf("rec-%06d", w*2000+j)
					id = victim
					fn = func(x *tx) error { x.Records.Delete(victim); return nil }
					opts = append(opts, shrinking())
				}
				t0 := time.Now()
				_, err := m.mutate(ctx, benchNS, "", fn, opts...)
				d := time.Since(t0)
				if errors.Is(err, agentErrors.ErrLeaseLost) {
					mu.Lock()
					if j%3 == 2 {
						notDeleted[id] = true
					} else {
						notAdded[id] = true
					}
					mu.Unlock()
					continue
				}
				if errors.Is(err, agentErrors.ErrMemoryCommitUnknown) {
					continue // 结果未知：不断言是否生效
				}
				if !assert.NoError(t, err) {
					failed.Add(1)
					return
				}
				commits.Add(1)
				mu.Lock()
				writeLs = append(writeLs, d)
				if j%3 == 2 {
					deleted[id] = true
				} else {
					added[id] = true
				}
				mu.Unlock()
			}
		})
	}
	for r := range 4 {
		m := ms2[r%2]
		wg.Go(func() {
			var last int64
			for time.Now().Before(deadline) {
				t0 := time.Now()
				_, s, err := m.read(ctx, benchNS)
				d := time.Since(t0)
				if !assert.NoError(t, err) || !assert.GreaterOrEqual(t, s.Seq, last, "读者看到的提交不回退") {
					failed.Add(1)
					return
				}
				last = s.Seq
				reads.Add(1)
				mu.Lock()
				readLs = append(readLs, d)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	require.Zero(t, failed.Load())

	_, s, err := seed.read(ctx, benchNS)
	require.NoError(t, err)
	require.Equal(t, base.Seq+commits.Load(), s.Seq, "Seq 等于成功写入次数")
	for id := range added {
		require.Contains(t, s.Records, id, "成功写入的记录 %s 丢失", id)
	}
	for id := range deleted {
		require.NotContains(t, s.Records, id, "已删除的记录 %s 复活", id)
	}
	for id := range notAdded {
		require.NotContains(t, s.Records, id, "确定失败的新增 %s 生效了", id)
	}
	for id := range notDeleted {
		require.Contains(t, s.Records, id, "确定失败的删除 %s 生效了", id)
	}
	checkInvariants(t, benchNS, s)
	for _, m := range ms2 {
		_, used := m.cache.snapshot()
		require.LessOrEqual(t, used, capacity)
	}
	t.Logf("10k 压力：%v，提交 %d 次（新增 %d、删除 %d），读取 %d 次；写 p50 %s p95 %s，读 p50 %s p95 %s；最终 %d 条记录、Seq %d",
		duration, commits.Load(), len(added), len(deleted), reads.Load(), ms(percentile(writeLs, 0.5)), ms(percentile(writeLs, 0.95)),
		ms(percentile(readLs, 0.5)), ms(percentile(readLs, 0.95)), len(s.Records), s.Seq)
	t.Logf("注入故障：租约丢失 %d 次（其中确定失败 %d 次），提交前断开 %d 次，写入后报错 %d 次", leaseLost.Load(), len(notAdded)+len(notDeleted), unknown.Load(), lateFail.Load())
}
