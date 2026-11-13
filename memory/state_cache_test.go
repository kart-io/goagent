package memory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
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

func fullState() state {
	now := time.Unix(1_800_000_000, 0).UTC()
	s := state{Seq: 7, CommitID: "c7", NextIncarnation: 42, LastForgetSeq: 3, MarkerHorizon: 2, MigrationID: "m1"}
	s.init()
	for i := range 20 {
		id := fmt.Sprintf("r%02d", i)
		s.Records[id] = Record{ID: id, Namespace: testNS, Kind: KindFact, Content: "用户喜欢喝茶" + id, Keywords: []string{"茶"},
			Importance: 0.5, ValidFrom: now, InvalidAt: now.Add(time.Duration(i%2) * time.Hour), CreatedAt: now,
			Sources: []Source{{Session: "s1", From: int64(i), To: int64(i + 1), Job: "s1/1"}}}
	}
	s.Entities["e1"] = Entity{ID: "e1", Namespace: testNS, Name: "张三", Type: "人物", Aliases: []string{"老张"}, CreatedAt: now}
	s.Edges["g1"] = Edge{ID: "g1", Namespace: testNS, Subject: "e1", Predicate: "喜欢", Object: "e1", Fact: "张三喜欢茶", ValidFrom: now, CreatedAt: now}
	s.Communities["c1"] = Community{ID: "c1", Namespace: testNS, Members: []string{"e1"}, Name: "茶", Summary: "饮品", UpdatedAt: now}
	s.Jobs["s1/2"] = Job{Key: "s1/2", Namespace: testNS, Session: "s1", From: 2, To: 3, Status: JobStatus("pending"), CreatedAt: now}
	s.DoneJobs["s1/1"] = now
	mk := rewindMarker{Session: "s1", To: 4, Seq: 6, At: now}
	s.Markers[mk.key()] = mk
	return s
}

func TestStateCodecRoundTrip(t *testing.T) {
	empty := state{}
	empty.init()
	for name, s := range map[string]state{"空 state": empty, "各映射都有数据": fullState()} {
		t.Run(name, func(t *testing.T) {
			data, size, err := encodeState(&s)
			require.NoError(t, err)
			require.Positive(t, size)
			require.True(t, json.Valid(data), "Data 是合法的 JSON，任何 durable.Store 都能原样保存")
			blob, err := decodeBlob(data)
			require.NoError(t, err)
			require.Equal(t, s.CommitID, blob.CommitID, "版本头不解码数据块即可读取")
			require.Equal(t, size, blob.RawBytes)
			got, err := decodeState(blob, testNS)
			require.NoError(t, err)
			got.init()
			require.Equal(t, s, got)
		})
	}
}

func TestStateCodecRejectsCorruption(t *testing.T) {
	s := fullState()
	data, _, err := encodeState(&s)
	require.NoError(t, err)
	for name, framing := range map[string][]byte{
		"缺少结尾":      data[:len(data)-1],
		"不是数组":      data[1:],
		"数据块不是字符串":  []byte(`[{"seq":1},1]`),
		"数据块没有结束引号": []byte(`[{"seq":1},"abc]`),
	} {
		_, err := decodeBlob(framing)
		require.Error(t, err, name)
		require.True(t, agentErrors.IsCode(err, agentErrors.CodeStateLoad), name)
	}
	blob, err := decodeBlob(data)
	require.NoError(t, err)
	for name, mangle := range map[string]func(chunk []byte) []byte{
		"截断的数据块": func(c []byte) []byte { return append(c[:len(c)/2:len(c)/2], '"') },
		"改动的数据块": func(c []byte) []byte {
			c = append([]byte(nil), c...)
			c[len(c)/2] ^= 0x01
			return c
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := blob
			bad.Chunks = append(bad.Chunks[:0:0], blob.Chunks...)
			bad.Chunks[0] = mangle(blob.Chunks[0])
			_, err := decodeState(bad, testNS)
			require.Error(t, err)
			require.True(t, agentErrors.IsCode(err, agentErrors.CodeStateLoad))
		})
	}
}

func TestStateCacheLRU(t *testing.T) {
	ref := func(raw int, commit string) baseRef { return baseRef{CommitID: commit, RawBytes: raw} }
	at := func(seq int64) state { return state{Seq: seq} }
	hit := func(c *stateCache, key, scope, commit string) bool { _, ok := c.head(key, scope, commit); return ok }
	put := func(c *stateCache, key, scope string, r baseRef, commit string, cur state) {
		c.put(&cacheEntry{key: key, scope: scope, ref: r, headCommit: commit, cur: cur})
	}
	c := newStateCache(250) // 覆盖层为空时每份按解压后 40 字节 × heapFactor = 100 计
	put(c, "a", "0", ref(40, "ba"), "a1", at(1))
	put(c, "b", "0", ref(40, "bb"), "b1", at(1))
	require.True(t, hit(c, "a", "0", "a1"))
	put(c, "c", "0", ref(40, "bc"), "c1", at(1)) // 超出容量：淘汰最久未用的 b
	require.False(t, hit(c, "b", "0", "b1"))
	stats, size := c.snapshot()
	require.Equal(t, 1, stats.Evictions)
	require.LessOrEqual(t, size, 250)

	require.False(t, hit(c, "a", "0", "a0"), "head 的 CommitID 不同不命中")
	require.False(t, hit(c, "a", "1", "a1"), "作用域不同不命中")
	_, ok := c.base("a", "0", "ba")
	require.True(t, ok, "基础快照按自己的 commit_id 命中")
	_, ok = c.base("a", "0", "other")
	require.False(t, ok)

	put(c, "a", "0", ref(10, "ba"), "a0", at(0))
	require.True(t, hit(c, "a", "0", "a1"), "同一作用域不以旧提交覆盖新提交")
	put(c, "a", "new", ref(10, "bn"), "n0", at(0))
	require.True(t, hit(c, "a", "new", "n0"), "作用域改变后替换")
	put(c, "a", "new", ref(101, "bn"), "n1", at(1))
	require.False(t, hit(c, "a", "new", "n0"), "超过容量的条目不缓存，并移出旧条目")
	c.drop("c")
	_, size = c.snapshot()
	require.Zero(t, size)

	var off *stateCache
	put(off, "a", "0", ref(1, "x"), "a1", at(1))
	require.False(t, hit(off, "a", "0", "a1"), "关闭的缓存不缓存")
	require.Nil(t, newStateCache(-1))
}

func cacheStatsOf(m *Memory) CacheStats { s, _ := m.cache.snapshot(); return s }

// 两个实例共用存储：一方提交后另一方的读取与写入都基于最新提交，命中缓存只省去解码。
func TestStateCacheAcrossInstances(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	open := func() *Memory {
		fs, err := durable.NewFileStore(dir)
		require.NoError(t, err)
		return durableMemory(t, fs, Config{})
	}
	a, b := open(), open()
	_, err := a.mutate(ctx, testNS, "", addRecord("a1"))
	require.NoError(t, err)
	_, _, err = a.read(ctx, testNS)
	require.NoError(t, err)
	require.Equal(t, 1, cacheStatsOf(a).Hits, "自己提交的 state 直接进入缓存")

	_, err = b.mutate(ctx, testNS, "", addRecord("b1"))
	require.NoError(t, err)
	_, s, err := a.read(ctx, testNS)
	require.NoError(t, err)
	require.Contains(t, s.Records, "b1", "另一实例的提交立即可见")

	_, err = b.mutate(ctx, testNS, "", addRecord("b2"))
	require.NoError(t, err)
	committed, err := a.mutate(ctx, testNS, "", addRecord("a2"))
	require.NoError(t, err)
	require.Len(t, committed.Records, 4, "加锁后按 CommitID 核对，不在缓存的旧版本上修改")
	require.EqualValues(t, 4, committed.Seq)
}

// Purge 切换作用域（代际）后，缓存中旧作用域的 state 不再被采用。
func TestStateCachePurgeGeneration(t *testing.T) {
	ctx := context.Background()
	st := newHookStore(t)
	m := durableMemory(t, st, Config{})
	_, err := m.mutate(ctx, testNS, "", addRecord("old"))
	require.NoError(t, err)
	setMeta(t, st, testNS, metaState{Current: "g2", Purges: 1})
	scope, s, err := m.read(ctx, testNS)
	require.NoError(t, err)
	require.Equal(t, "g2", scope)
	require.Empty(t, s.Records)
	committed, err := m.mutate(ctx, testNS, "", addRecord("new"))
	require.NoError(t, err)
	require.Equal(t, []string{"new"}, recordIDs(committed))
}

func recordIDs(s state) []string {
	var ids []string
	for id := range s.Records {
		ids = append(ids, id)
	}
	return ids
}

// 修改函数出错、超过上限或提交结果未知时，缓存保持存储中的真实内容。
func TestStateCacheUnaffectedByFailedMutate(t *testing.T) {
	ctx := context.Background()
	st := newHookStore(t)
	m := durableMemory(t, st, Config{})
	_, err := m.mutate(ctx, testNS, "", addRecord("a"))
	require.NoError(t, err)

	_, err = m.mutate(ctx, testNS, "", func(t *tx) error {
		t.Records.Delete("a")
		t.Records.Put("x", Record{ID: "x"})
		return errors.New("拒绝")
	})
	require.Error(t, err)
	st.h.before = func(op, _, id string) error {
		if op == "save" && id == headID {
			st.h.before = nil
			return errors.New("连接中断")
		}
		return nil
	}
	_, err = m.mutate(ctx, testNS, "", addRecord("lost"))
	require.ErrorIs(t, err, agentErrors.ErrMemoryCommitUnknown)

	_, s, err := m.read(ctx, testNS)
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, recordIDs(s))
}

// 并发读写（-race）：读者看到的 Seq 单调不减，缓存占用不超过容量。
func TestStateCacheConcurrentReadersAndWriters(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	var ms []*Memory
	for range 2 {
		fs, err := durable.NewFileStore(dir)
		require.NoError(t, err)
		ms = append(ms, durableMemory(t, fs, Config{StateCacheBytes: 64 << 10}))
	}
	const writes = 20
	var wg sync.WaitGroup
	for w := range 2 {
		wg.Go(func() {
			for i := range writes {
				_, err := ms[w].mutate(ctx, testNS, "", addRecord(fmt.Sprintf("w%d-%d", w, i)))
				if !assert.NoError(t, err) {
					return
				}
			}
		})
	}
	done := make(chan struct{})
	for r := range 4 {
		wg.Go(func() {
			var last int64
			for {
				select {
				case <-done:
					return
				default:
				}
				_, s, err := ms[r%2].read(ctx, testNS)
				if !assert.NoError(t, err) || !assert.GreaterOrEqual(t, s.Seq, last, "读者看到的提交不回退") {
					return
				}
				last = s.Seq
				for id, rec := range s.Records {
					assert.Equal(t, id, rec.ID)
				}
			}
		})
	}
	require.Eventually(t, func() bool {
		_, s, err := ms[0].read(ctx, testNS)
		return err == nil && s.Seq == 2*writes
	}, 30*time.Second, 10*time.Millisecond)
	close(done)
	wg.Wait()
	_, s, err := ms[1].read(ctx, testNS)
	require.NoError(t, err)
	require.Len(t, s.Records, 2*writes)
	checkInvariants(t, testNS, s)
	for _, m := range ms {
		_, size := m.cache.snapshot()
		require.LessOrEqual(t, size, 64<<10)
	}
}

func TestStateCacheDisabled(t *testing.T) {
	ctx := context.Background()
	m := durableMemory(t, newHookStore(t), Config{StateCacheBytes: -1})
	require.Nil(t, m.cache)
	_, err := m.mutate(ctx, testNS, "", addRecord("a"))
	require.NoError(t, err)
	_, s, err := m.read(ctx, testNS)
	require.NoError(t, err)
	require.Contains(t, s.Records, "a")
}

// 同一提交经缓存（写入方）与完整解码（另一实例）得到的 state 逐字节等价：Namespace 补齐、空切片为 nil。
func TestStateCacheEquivalentToFullDecode(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	open := func(cache int) *Memory {
		fs, err := durable.NewFileStore(dir)
		require.NoError(t, err)
		return durableMemory(t, fs, Config{StateCacheBytes: cache})
	}
	writer, cold := open(0), open(-1)
	_, err := writer.mutate(ctx, testNS, "", func(t *tx) error {
		t.Records.Put("a", Record{ID: "a", Content: "无命名空间、空切片", Keywords: []string{}, Sources: []Source{}})
		t.Communities.Put("c", Community{ID: "c", Namespace: testNS, Members: []string{}})
		t.Jobs.Put("j", Job{Key: "j"})
		return nil
	})
	require.NoError(t, err)
	_, hot, err := writer.read(ctx, testNS)
	require.NoError(t, err)
	require.Equal(t, 1, cacheStatsOf(writer).Hits, "写入方的读取命中缓存")
	_, full, err := cold.read(ctx, testNS)
	require.NoError(t, err)
	hb, err := json.Marshal(hot)
	require.NoError(t, err)
	fb, err := json.Marshal(full)
	require.NoError(t, err)
	require.JSONEq(t, string(fb), string(hb))
	require.Equal(t, string(fb), string(hb))
	checkInvariants(t, testNS, full)
}

func TestMutateRejectsForeignNamespace(t *testing.T) {
	m := durableMemory(t, newHookStore(t), Config{})
	_, err := m.mutate(context.Background(), testNS, "", func(t *tx) error {
		t.Records.Put("x", Record{ID: "x", Namespace: []string{"t1", "bob"}})
		return nil
	})
	require.True(t, agentErrors.IsCode(err, agentErrors.CodeInvalidInput), "对象属于其他命名空间时拒绝提交：%v", err)
}

// 调用方事后修改传入的 ns，不影响缓存中的对象。
func TestStateCacheDoesNotAliasCallerNamespace(t *testing.T) {
	ctx := context.Background()
	m := durableMemory(t, newHookStore(t), Config{})
	ns := []string{"t1", "alice"}
	_, err := m.mutate(ctx, ns, "", addRecord("a"))
	require.NoError(t, err)
	_, _, err = m.read(ctx, ns)
	require.NoError(t, err)
	ns[1] = "mallory"
	_, s, err := m.read(ctx, testNS)
	require.NoError(t, err)
	require.Equal(t, testNS, s.Records["a"].Namespace)
}

func TestStateCodecHeaderIntegrity(t *testing.T) {
	s := fullState()
	data, _, err := encodeState(&s)
	require.NoError(t, err)
	last := bytes.LastIndex(data, []byte(`,"`))
	cases := map[string][]byte{
		"少一个数据块":       append(append([]byte(nil), data[:last]...), ']'),
		"raw_bytes 被改": regexp.MustCompile(`"raw_bytes":\d+`).ReplaceAll(data, []byte(`"raw_bytes":1`)),
		"块长度被改":        regexp.MustCompile(`"chunk_bytes":\[\d+`).ReplaceAll(data, []byte(`"chunk_bytes":[1`)),
		"标量被改":         bytes.Replace(data, []byte(`"next_incarnation":42`), []byte(`"next_incarnation":43`), 1),
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			require.NotEqual(t, data, bad)
			blob, err := decodeBlob(bad)
			if err == nil {
				_, err = decodeState(blob, testNS)
			}
			require.Error(t, err)
			require.True(t, agentErrors.IsCode(err, agentErrors.CodeStateLoad))
		})
	}
	t.Run("容忍 JSON 空白", func(t *testing.T) {
		spaced := bytes.ReplaceAll(data, []byte(`,"`), []byte(" ,\n \""))
		spaced = append(spaced[:len(spaced)-1], []byte(" ]\n")...)
		require.True(t, json.Valid(spaced))
		blob, err := decodeBlob(spaced)
		require.NoError(t, err)
		got, err := decodeState(blob, testNS)
		require.NoError(t, err)
		require.Equal(t, s.Records, got.Records)
	})
}

// 解压的输出以记录的长度为上限：长度不符一律拒绝，声明很小的解压炸弹不会解出全部内容。
func TestDecompressBoundsOutput(t *testing.T) {
	plain := bytes.Repeat([]byte("长期记忆"), 1024)
	packed := zstdEncoder.EncodeAll(plain, nil)
	bomb := zstdEncoder.EncodeAll(make([]byte, 64<<20), nil)
	for _, tc := range []struct {
		name   string
		packed []byte
		size   int
		ok     bool
	}{
		{"长度恰好", packed, len(plain), true},
		{"记录的长度偏短", packed, len(plain) - 1, false},
		{"记录的长度偏长", packed, len(plain) + 1, false},
		{"解压炸弹", bomb, 1024, false},
		{"截断", packed[:len(packed)-4], len(plain), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decompress(tc.packed, tc.size)
			if tc.ok {
				require.NoError(t, err)
				require.Equal(t, plain, got)
				return
			}
			require.Error(t, err)
			require.Nil(t, got)
		})
	}
}

// 提交不随调用方取消，但带时间上限：存储挂起时在上限内返回结果未知，不无限持锁。
func TestMutateCommitDeadline(t *testing.T) {
	st := newHookStore(t)
	m := durableMemory(t, st, Config{})
	m.commitTimeout = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	st.h.saveCtx = func(saveCtx context.Context) {
		st.h.saveCtx = nil
		cancel() // 调用方此时取消，不影响提交
		deadline, ok := saveCtx.Deadline()
		assert.True(t, ok, "提交带时间上限")
		assert.WithinDuration(t, time.Now().Add(50*time.Millisecond), deadline, 50*time.Millisecond)
		assert.NoError(t, saveCtx.Err(), "调用方取消不传到提交")
		<-saveCtx.Done() // 模拟存储挂起直到超时
	}
	st.h.before = func(op, _, id string) error {
		if op == "save" && id == headID {
			st.h.before = nil
			return context.DeadlineExceeded
		}
		return nil
	}
	start := time.Now()
	_, err := m.mutate(ctx, testNS, "", addRecord("a"))
	require.ErrorIs(t, err, agentErrors.ErrMemoryCommitUnknown)
	require.Less(t, time.Since(start), 2*time.Second)
}

// 建议 8：同一提交的并发未命中只解码一次，其余读者等待并共享结果；统计计入解码次数与耗时。
func TestConcurrentMissesDecodeOnce(t *testing.T) {
	ctx := context.Background()
	st := newHookStore(t)
	writer := durableMemory(t, st, Config{})
	for i := range 50 {
		_, err := writer.mutate(ctx, testNS, "", addRecord(fmt.Sprintf("r%02d", i)))
		require.NoError(t, err)
	}
	require.NoError(t, writer.compact(ctx, testNS, true, true))

	reader := durableMemory(t, st, Config{})
	release := make(chan struct{})
	var loads atomic.Int32
	st.h.before = func(op, _, id string) error {
		if op == "load" && strings.HasPrefix(id, "memory/base/") {
			loads.Add(1)
			<-release
		}
		return nil
	}
	const readers = 8
	var wg sync.WaitGroup
	for range readers {
		wg.Go(func() {
			_, s, err := reader.read(ctx, testNS)
			assert.NoError(t, err)
			assert.Len(t, s.Records, 50)
		})
	}
	require.Eventually(t, func() bool { return cacheStatsOf(reader).Misses == readers }, 5*time.Second, time.Millisecond)
	close(release)
	wg.Wait()
	st.h.before = nil
	require.EqualValues(t, 1, loads.Load(), "基础快照只读取一次")
	stats := reader.CacheStats()
	require.Equal(t, 1, stats.Decodes)
	require.Positive(t, stats.DecodeTime)
	require.Positive(t, stats.Bytes)

	// 等待中的读者取消时立即返回，不影响进行中的解码。
	cold := durableMemory(t, st, Config{})
	block := make(chan struct{})
	st.h.before = func(op, _, id string) error {
		if op == "load" && strings.HasPrefix(id, "memory/base/") {
			<-block
		}
		return nil
	}
	cctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { _, _, err := cold.read(cctx, testNS); done <- err }()
	require.Eventually(t, func() bool { return cacheStatsOf(cold).Misses == 1 }, 5*time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	close(block)
	require.Eventually(t, func() bool { return cold.CacheStats().Decodes == 1 }, 5*time.Second, time.Millisecond, "解码照常完成并进入缓存")
}
