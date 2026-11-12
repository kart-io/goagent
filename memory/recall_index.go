package memory

import (
	"context"
	"runtime"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/kart-io/goagent/constants"
)

// 预计算的 bigram 索引，设计见 docs/design/MEMORY_G1_BIGRAM_INDEX.md：结果与 recall 的逐条打分逐项相同。
// 基础快照的索引在解码时构建、与之同缓存；覆盖层另建小索引，并遮蔽基础快照中被覆盖层改动过的记录。

// indexVersion 在字符对的规范化或打包方式改变时递增；索引不落盘，旧版本只存在于缓存中。
const indexVersion = 1

// minIndexedRecords 以下不建索引，查询时逐条计算。
const minIndexedRecords = 200

// bigram 是两个字符打包成的键：与 string(rune 对) 一一对应，不分配字符串。
type bigram uint64

// eachRune 按 bigrams 的规范化逐字给出字符：逐字 unicode.ToLower（与 strings.ToLower 等价），只保留字母与数字。
func eachRune(text string, f func(rune)) {
	for _, r := range text {
		r = unicode.ToLower(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			f(r)
		}
	}
}

// recordBigrams 返回记录文本（正文与关键词，与 recall 的拼接方式相同：分隔符被丢弃，正文末字与关键词首字相连）的去重字符对。
func recordBigrams(r Record, dst []bigram) []bigram {
	dst = dst[:0]
	var prev rune
	have := false
	add := func(c rune) {
		if have {
			dst = append(dst, bigram(uint64(uint32(prev))<<32|uint64(uint32(c))))
		}
		prev, have = c, true
	}
	eachRune(r.Content, add)
	for _, k := range r.Keywords {
		eachRune(k, add)
	}
	slices.Sort(dst)
	return slices.Compact(dst)
}

// queryBigrams 返回查询的去重字符对，与 bigrams(q) 的集合相同。
func queryBigrams(text string) []bigram {
	return recordBigrams(Record{Content: text}, nil)
}

// bigramIndex 是一组有效记录的倒排表，以排序数组存储：每项是 (字符对, 记录序号) 打包成的 uint64
// （首字 21 位、次字 21 位、序号 22 位，Unicode 码点不超过 21 位）。查询一个字符对是对 [字符对<<22, (字符对+1)<<22) 的二分查找；
// 构建是并行的"逐条提取 + 分段排序 + 两两归并"，没有映射与小切片的分配。
type bigramIndex struct {
	version int
	ids     []string
	ord     map[string]int32
	keys    []uint64
	bytes   int // 估计的堆占用，计入缓存容量
}

const ordBits = 22

func packKey(b bigram, ord int32) uint64 {
	r1, r2 := uint64(b>>32), uint64(uint32(b))
	return r1<<(64-21) | r2<<ordBits | uint64(ord)
}

// buildIndex 为 records 中的有效记录建索引；记录少于 minIndexedRecords 或多于序号能表示的上限时返回 nil（查询时逐条计算）。
func buildIndex(records map[string]Record) *bigramIndex {
	if len(records) < minIndexedRecords || len(records) >= 1<<ordBits {
		return nil
	}
	ids := make([]string, 0, len(records))
	for id, r := range records {
		if r.Valid() {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	idx := &bigramIndex{version: indexVersion, ids: ids, ord: make(map[string]int32, len(ids))}
	for i, id := range ids {
		idx.ord[id] = int32(i)
	}
	workers := max(1, min(runtime.GOMAXPROCS(0), len(ids)/64))
	runs := make([][]uint64, workers)
	var wg sync.WaitGroup
	for w := range workers {
		lo, hi := w*len(ids)/workers, (w+1)*len(ids)/workers
		wg.Go(func() {
			var buf []bigram
			run := make([]uint64, 0, (hi-lo)*96)
			for o := lo; o < hi; o++ {
				buf = recordBigrams(records[ids[o]], buf)
				for _, b := range buf {
					run = append(run, packKey(b, int32(o)))
				}
			}
			slices.Sort(run)
			runs[w] = run
		})
	}
	wg.Wait()
	for len(runs) > 1 {
		next := make([][]uint64, (len(runs)+1)/2)
		for i := range next {
			if 2*i+1 == len(runs) {
				next[i] = runs[2*i]
				continue
			}
			wg.Go(func() { next[i] = mergeSorted(runs[2*i], runs[2*i+1]) })
		}
		wg.Wait()
		runs = next
	}
	idx.keys = runs[0]
	idx.bytes = 8*len(idx.keys) + 64*len(ids)
	return idx
}

func mergeSorted(a, b []uint64) []uint64 {
	out := make([]uint64, 0, len(a)+len(b))
	for len(a) > 0 && len(b) > 0 {
		if a[0] <= b[0] {
			out, a = append(out, a[0]), a[1:]
		} else {
			out, b = append(out, b[0]), b[1:]
		}
	}
	return append(append(out, a...), b...)
}

// each 对包含字符对 b 的每条记录调用 f(序号)。
func (idx *bigramIndex) each(b bigram, f func(int32)) {
	lo := packKey(b, 0)
	i, _ := slices.BinarySearch(idx.keys, lo)
	for ; i < len(idx.keys) && idx.keys[i]>>ordBits == lo>>ordBits; i++ {
		f(int32(idx.keys[i] & (1<<ordBits - 1)))
	}
}

// lazyIndex 是基础快照的索引：有缓存时解码后在后台开始构建（不阻塞读取），召回时若尚未完成则等待。
type lazyIndex struct {
	once    sync.Once
	records map[string]Record
	idx     *bigramIndex
}

// estIndexBytesPerRecord 是索引堆占用的估计（按记录数），在构建完成之前用于缓存容量；按 G1 7.3 的实测标定。
const estIndexBytesPerRecord = 1200

// newLazyIndex 返回 records 的索引；warm 为真时在后台开始构建（结果进入缓存时才值得预热），否则在第一次召回时构建。
// 记录太少时返回 nil。
func newLazyIndex(records map[string]Record, warm bool) *lazyIndex {
	if len(records) < minIndexedRecords {
		return nil
	}
	li := &lazyIndex{records: records}
	if warm {
		go li.get()
	}
	return li
}

func (li *lazyIndex) get() *bigramIndex {
	if li == nil {
		return nil
	}
	li.once.Do(func() { li.idx, li.records = buildIndex(li.records), nil })
	return li.idx
}

// recallIndex 是一次还原出的 state 的查询结构：基础快照索引、被覆盖层遮蔽的基础序号、覆盖层中仍有效的记录。
// 遮蔽与覆盖层的字符对在第一次查询时计算，写入路径不付出这部分开销。
type recallIndex struct {
	base    *lazyIndex
	ov      overlay
	once    sync.Once
	idx     *bigramIndex
	masked  []bool
	overlay map[string][]bigram
}

func newRecallIndex(base *lazyIndex, ov overlay) *recallIndex {
	return &recallIndex{base: base, ov: ov}
}

func (ri *recallIndex) prepare() {
	ri.once.Do(func() {
		ri.idx = ri.base.get()
		ri.overlay = map[string][]bigram{}
		if ri.idx != nil {
			ri.masked = make([]bool, len(ri.idx.ids))
		}
		for id, c := range ri.ov.Records {
			if ri.idx != nil {
				if o, ok := ri.idx.ord[id]; ok {
					ri.masked[o] = true
				}
			}
			if !c.Deleted && c.Value.Valid() {
				ri.overlay[id] = recordBigrams(c.Value, nil)
			}
		}
	})
}

// indexed 报告查询是否可以走索引（基础快照有索引）。
func (ri *recallIndex) indexed() bool {
	if ri == nil {
		return false
	}
	ri.prepare()
	return ri.idx != nil
}

// matches 返回与查询字符对 q 至少有一个相同字符对的记录及相同的个数。
func (ri *recallIndex) matches(q []bigram) map[string]int {
	ri.prepare()
	out := map[string]int{}
	counts := make([]uint16, len(ri.idx.ids))
	for _, b := range q {
		ri.idx.each(b, func(o int32) {
			if !ri.masked[o] {
				counts[o]++
			}
		})
	}
	for o, n := range counts {
		if n > 0 {
			out[ri.idx.ids[o]] = int(n)
		}
	}
	for id, bs := range ri.overlay {
		n := 0
		for _, b := range q {
			if _, found := slices.BinarySearch(bs, b); found {
				n++
			}
		}
		if n > 0 {
			out[id] = n
		}
	}
	return out
}

// rankState 是 recall 的打分与排序在 state 上的实现：结果与 recall 对同一组记录逐项相同。
// ri 为 nil 时逐条计算（小 state 或没有索引）。
func (m *Memory) rankState(s state, ri *recallIndex, q Query, vector map[string]float64) []Scored {
	k := q.K
	if k <= 0 {
		k = m.cfg.RecallK
	}
	k = min(k, constants.MaxMemoryRecallK)
	now := m.now()
	query := queryBigrams(q.Text)
	var candidates map[string]int
	switch {
	case len(query) == 0:
		candidates = nil
	case ri.indexed():
		candidates = ri.matches(query)
	default:
		candidates = map[string]int{}
		var buf []bigram
		for id, r := range s.Records {
			buf = recordBigrams(r, buf)
			n := 0
			for _, b := range query {
				if _, found := slices.BinarySearch(buf, b); found {
					n++
				}
			}
			if n > 0 {
				candidates[id] = n
			}
		}
	}
	score := func(r Record, n int) (Scored, bool) {
		if !r.Valid() || (len(q.Kinds) > 0 && !slices.Contains(q.Kinds, r.Kind)) {
			return Scored{}, false
		}
		rel := vector[r.ID]
		if len(query) > 0 {
			rel = max(rel, float64(n)/float64(len(query)))
		}
		if len(query) > 0 && rel == 0 {
			return Scored{}, false
		}
		return Scored{Record: r, Relevance: rel, Score: m.cfg.Weights.Relevance*rel + m.cfg.Weights.Recency*m.recency(r, now) + m.cfg.Weights.Importance*r.Importance}, true
	}
	var out []Scored
	if candidates == nil {
		for _, r := range s.Records {
			if sc, ok := score(r, 0); ok {
				out = append(out, sc)
			}
		}
	} else {
		for id := range vector {
			if _, ok := candidates[id]; !ok {
				candidates[id] = 0
			}
		}
		for id, n := range candidates {
			if r, ok := s.Records[id]; ok {
				if sc, ok := score(r, n); ok {
					out = append(out, sc)
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b Scored) int {
		if a.Score != b.Score {
			if a.Score > b.Score {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out[:min(k, len(out))]
}

// recallState 是端到端的召回（G1a 门槛的口径）：读取 state、查候选、打分、排序，返回排好序的结果。
// 不含向量召回（需要嵌入模型）；G1b 把公开的 Recall 接到这里。
func (m *Memory) recallState(ctx context.Context, ns []string, q Query) ([]Scored, error) {
	_, v, err := m.readView(ctx, ns)
	if err != nil {
		return nil, err
	}
	return m.rankState(v.cur, v.idx, q, nil), nil
}
