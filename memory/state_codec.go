package memory

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"errors"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
)

// 基础快照的编码，设计见 docs/design/MEMORY_G1_STORAGE.md 3.4（增量提交后用于两个基础快照槽位）。Data 是一个 JSON 数组 [版本头, "数据块", ...]：
// 版本头在前，读写方只解析它就能核对 CommitID；数据块是 gob → zstd → base64 的字符串，按引号定位、不经 JSON 扫描，
// 分 stateChunks 块并行编解码。版本头与数据块在同一个快照中，一次 Save 原子提交。
const (
	stateBlobVersion = "memory-state-blob/3"
	// stateBlobV2 是 G1 的格式，仍可读取：gob 忽略已删除的字段，没有派生的关系与没有名称的实体按 G2 规则过滤与清理。
	stateBlobV2 = "memory-state-blob/2"
	stateChunks = 16
)

// stateScalars 是 state 的标量字段；版本头与第一个数据块各存一份，完整解码时互相核对。
type stateScalars struct {
	Seq             int64  `json:"seq"`
	CommitID        string `json:"commit_id"`
	NextIncarnation int64  `json:"next_incarnation"`
	LastForgetSeq   int64  `json:"last_forget_seq"`
	MarkerHorizon   int64  `json:"marker_horizon,omitzero"`
	MigrationID     string `json:"migration_id,omitempty"`
}

// stateHeader 是版本头。
type stateHeader struct {
	stateScalars
	// RawBytes 是数据块解压后的总字节数，MaxStateBytes 按它判断；ChunkBytes 是各块解压后的字节数，解码时逐块限长并核对。
	RawBytes   int   `json:"raw_bytes"`
	ChunkBytes []int `json:"chunk_bytes"`
}

// stateBlob 是解析后的快照：版本头与各数据块的 base64 原文（指向 Data，不复制）。
type stateBlob struct {
	stateHeader
	Chunks [][]byte
}

// stateChunk 是 state 的一块：编码时把各映射的条目轮流分到各块，解码后按键合并。
type stateChunk struct {
	Scalars     *stateScalars // 只在第一块
	Records     []Record
	Entities    []Entity
	Edges       []Edge
	Communities []Community
	Jobs        []Job
	DoneJobs    []doneJob
	Markers     []rewindMarker
	Finished    []finishedJob
}

type finishedJob struct {
	Key   string
	Stats JobStats
}

type doneJob struct {
	Key string
	At  time.Time
}

// zstd 的解压比标准库 gzip 快约 3 倍、压缩率更高，冷读的 CPU 大头原是 gzip 解压（G1 7.8）。
// EncodeAll、DecodeAll 可并发调用；解压以 dst 的容量为上限（WithDecodeAllCapLimit），挡住解压炸弹。
var (
	zstdEncoder, _ = zstd.NewWriter(nil)
	zstdDecoder, _ = zstd.NewReader(nil, zstd.WithDecoderConcurrency(0), zstd.WithDecodeAllCapLimit(true))
	buffers        = sync.Pool{New: func() any { return new(bytes.Buffer) }}
)

// decompress 解压 packed，解压后必须恰好为 size 字节：既校验截断，也限制输出长度。帧内的校验和在这里核对。
func decompress(packed []byte, size int) ([]byte, error) {
	plain, err := zstdDecoder.DecodeAll(packed, make([]byte, 0, size))
	if err != nil {
		return nil, err
	}
	if len(plain) != size {
		return nil, badFraming("解压后的长度与记录的不符")
	}
	return plain, nil
}

func (s *state) scalars() stateScalars {
	return stateScalars{Seq: s.Seq, CommitID: s.CommitID, NextIncarnation: s.NextIncarnation, LastForgetSeq: s.LastForgetSeq, MarkerHorizon: s.MarkerHorizon, MigrationID: s.MigrationID}
}

// split 把 s 的条目轮流分到 stateChunks 块。state 中的对象都属于它的命名空间（不变量 4），对象的 Namespace 不写入，
// 解码时补回：命名空间通常是两个较长的 ID，每个对象都重复一份会使数据量增加一成以上。
func (s *state) split() []stateChunk {
	chunks := make([]stateChunk, stateChunks)
	per := func(n int) int { return n/stateChunks + 1 }
	for i := range chunks {
		chunks[i] = stateChunk{Records: make([]Record, 0, per(len(s.Records))), Entities: make([]Entity, 0, per(len(s.Entities))),
			Edges: make([]Edge, 0, per(len(s.Edges))), Communities: make([]Community, 0, per(len(s.Communities))),
			Jobs: make([]Job, 0, per(len(s.Jobs))), DoneJobs: make([]doneJob, 0, per(len(s.DoneJobs)))}
	}
	i := 0
	next := func() *stateChunk { c := &chunks[i%stateChunks]; i++; return c }
	for _, v := range s.Records {
		c := next()
		v.Namespace = nil
		c.Records = append(c.Records, v)
	}
	for _, v := range s.Entities {
		c := next()
		v.Namespace = nil
		c.Entities = append(c.Entities, v)
	}
	for _, v := range s.Edges {
		c := next()
		v.Namespace = nil
		c.Edges = append(c.Edges, v)
	}
	for _, v := range s.Communities {
		c := next()
		v.Namespace = nil
		c.Communities = append(c.Communities, v)
	}
	for _, v := range s.Jobs {
		c := next()
		v.Namespace = nil
		c.Jobs = append(c.Jobs, v)
	}
	for k, v := range s.DoneJobs {
		c := next()
		c.DoneJobs = append(c.DoneJobs, doneJob{Key: k, At: v})
	}
	for _, v := range s.Markers {
		c := next()
		c.Markers = append(c.Markers, v)
	}
	for k, v := range s.Finished {
		c := next()
		c.Finished = append(c.Finished, finishedJob{Key: k, Stats: v})
	}
	return chunks
}

// encodeState 编码 s，返回快照的 Data 与解压后的字节数。
func encodeState(s *state) (json.RawMessage, int, error) {
	chunks := s.split()
	scalars := s.scalars()
	chunks[0].Scalars = &scalars
	packed := make([][]byte, stateChunks)
	raw := make([]int, stateChunks)
	errs := make([]error, stateChunks)
	var wg sync.WaitGroup
	for i := range chunks {
		wg.Go(func() { packed[i], raw[i], errs[i] = packChunk(&chunks[i]) })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, 0, err
	}
	h := stateHeader{stateScalars: scalars, ChunkBytes: raw}
	for _, n := range raw {
		h.RawBytes += n
	}
	head, err := json.Marshal(h)
	if err != nil {
		return nil, 0, err
	}
	// 先排好各块在输出中的位置，再并行写入 base64。
	size := 1 + len(head)
	offsets := make([]int, stateChunks)
	for i, b := range packed {
		offsets[i] = size + 2
		size += 3 + base64.StdEncoding.EncodedLen(len(b))
	}
	size++
	out := make([]byte, size)
	out[0] = '['
	copy(out[1:], head)
	for i, b := range packed {
		n := base64.StdEncoding.EncodedLen(len(b))
		out[offsets[i]-2], out[offsets[i]-1], out[offsets[i]+n] = ',', '"', '"'
		wg.Go(func() { base64.StdEncoding.Encode(out[offsets[i]:offsets[i]+n], b) })
	}
	out[size-1] = ']'
	wg.Wait()
	return out, h.RawBytes, nil
}

// packChunk 返回 c 的 gob 经 zstd 压缩后的字节与 gob 的字节数。
func packChunk(c *stateChunk) ([]byte, int, error) {
	plain := buffers.Get().(*bytes.Buffer)
	defer func() { plain.Reset(); buffers.Put(plain) }()
	if err := gob.NewEncoder(plain).Encode(c); err != nil {
		return nil, 0, err
	}
	return zstdEncoder.EncodeAll(plain.Bytes(), nil), plain.Len(), nil
}

// decodeBlob 解析版本头并定位各数据块，不解码数据块；缓存命中时到此为止。
func decodeBlob(data []byte) (stateBlob, error) {
	var blob stateBlob
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.ReadToken(); err != nil || tok.Kind() != '[' {
		return stateBlob{}, badFraming("缺少数组开头")
	}
	if err := json.DecodeNext(dec, &blob.stateHeader); err != nil {
		return stateBlob{}, corruptState(err)
	}
	// 数据块是不含引号与转义的 base64，直接按引号切分；容忍 JSON 允许的空白，以适应会重排 JSON 的存储。
	rest := skipSpace(data[dec.InputOffset():])
	for len(rest) > 0 && rest[0] == ',' {
		rest = skipSpace(rest[1:])
		if len(rest) == 0 || rest[0] != '"' {
			return stateBlob{}, badFraming("数据块不是字符串")
		}
		end := bytes.IndexByte(rest[1:], '"')
		if end < 0 {
			return stateBlob{}, badFraming("数据块没有结束引号")
		}
		blob.Chunks = append(blob.Chunks, rest[1:1+end])
		rest = skipSpace(rest[2+end:])
	}
	if len(rest) == 0 || rest[0] != ']' || len(skipSpace(rest[1:])) != 0 {
		return stateBlob{}, badFraming("数组没有正确结束")
	}
	total := 0
	for _, n := range blob.ChunkBytes {
		total += n
	}
	if len(blob.Chunks) != len(blob.ChunkBytes) || len(blob.Chunks) == 0 || total != blob.RawBytes {
		return stateBlob{}, badFraming("数据块的数量或长度与版本头不符")
	}
	return blob, nil
}

// decodeState 并行解码 blob 的数据块并合并为命名空间 ns 的 state。
func decodeState(blob stateBlob, ns []string) (state, error) {
	chunks := make([]stateChunk, len(blob.Chunks))
	errs := make([]error, len(blob.Chunks))
	var wg sync.WaitGroup
	for i := range chunks {
		wg.Go(func() {
			if errs[i] = decodeChunk(blob.Chunks[i], blob.ChunkBytes[i], &chunks[i]); errs[i] == nil {
				chunks[i].setNamespace(ns)
			}
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return state{}, corruptState(err)
	}
	h := blob.stateScalars
	if chunks[0].Scalars == nil || *chunks[0].Scalars != h {
		return state{}, badFraming("版本头与数据块不一致")
	}
	s := state{Seq: h.Seq, CommitID: h.CommitID, NextIncarnation: h.NextIncarnation, LastForgetSeq: h.LastForgetSeq, MarkerHorizon: h.MarkerHorizon, MigrationID: h.MigrationID}
	merges := []func(){
		func() {
			s.Records = mergeChunks(chunks, func(c *stateChunk) []Record { return c.Records }, func(v Record) string { return v.ID })
		},
		func() {
			s.Entities = mergeChunks(chunks, func(c *stateChunk) []Entity { return c.Entities }, func(v Entity) string { return v.ID })
		},
		func() {
			s.Edges = mergeChunks(chunks, func(c *stateChunk) []Edge { return c.Edges }, func(v Edge) string { return v.ID })
		},
		func() {
			s.Communities = mergeChunks(chunks, func(c *stateChunk) []Community { return c.Communities }, func(v Community) string { return v.ID })
		},
		func() {
			s.Jobs = mergeChunks(chunks, func(c *stateChunk) []Job { return c.Jobs }, func(v Job) string { return v.Key })
		},
		func() {
			done := mergeChunks(chunks, func(c *stateChunk) []doneJob { return c.DoneJobs }, func(v doneJob) string { return v.Key })
			s.DoneJobs = make(map[string]time.Time, len(done))
			for k, v := range done {
				s.DoneJobs[k] = v.At
			}
		},
		func() {
			s.Markers = mergeChunks(chunks, func(c *stateChunk) []rewindMarker { return c.Markers }, rewindMarker.key)
		},
		func() {
			finished := mergeChunks(chunks, func(c *stateChunk) []finishedJob { return c.Finished }, func(v finishedJob) string { return v.Key })
			s.Finished = make(map[string]JobStats, len(finished))
			for k, v := range finished {
				s.Finished[k] = v.Stats
			}
		},
	}
	for _, merge := range merges {
		wg.Go(merge)
	}
	wg.Wait()
	return s, nil
}

// setNamespace 补回编码时省去的 Namespace；各对象共享 ns，只读。
func (c *stateChunk) setNamespace(ns []string) {
	for i := range c.Records {
		c.Records[i].Namespace = ns
	}
	for i := range c.Entities {
		c.Entities[i].Namespace = ns
	}
	for i := range c.Edges {
		c.Edges[i].Namespace = ns
	}
	for i := range c.Communities {
		c.Communities[i].Namespace = ns
	}
	for i := range c.Jobs {
		c.Jobs[i].Namespace = ns
	}
}

func mergeChunks[T any](chunks []stateChunk, items func(*stateChunk) []T, key func(T) string) map[string]T {
	n := 0
	for i := range chunks {
		n += len(items(&chunks[i]))
	}
	out := make(map[string]T, n)
	for i := range chunks {
		for _, v := range items(&chunks[i]) {
			out[key(v)] = v
		}
	}
	return out
}

// decodeChunk 解码一个数据块；解压后的长度必须恰好为 size。
func decodeChunk(text []byte, size int, c *stateChunk) error {
	packed := make([]byte, base64.StdEncoding.DecodedLen(len(text)))
	n, err := base64.StdEncoding.Decode(packed, text)
	if err != nil {
		return err
	}
	plain, err := decompress(packed[:n], size)
	if err != nil {
		return err
	}
	return gob.NewDecoder(bytes.NewReader(plain)).Decode(c)
}

func skipSpace(b []byte) []byte {
	return bytes.TrimLeft(b, " \t\r\n")
}

func badFraming(what string) error {
	return agentErrors.Newf(agentErrors.CodeStateLoad, "长期记忆的 state 快照损坏：%s", what).WithComponent("memory").WithOperation("decode")
}

func corruptState(err error) error {
	return agentErrors.Wrap(err, agentErrors.CodeStateLoad, "长期记忆的 state 快照损坏").WithComponent("memory").WithOperation("decode")
}
