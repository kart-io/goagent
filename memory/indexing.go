package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// indexKey 是索引中的版本化键 ID#实例号.ContentRev#模型摘要（设计 4.4）：对象重建、正文修改或换嵌入模型之后，
// 旧键不再匹配当前版本，不会被采用。
type indexKey struct {
	ID                      string
	Incarnation, ContentRev int64
	Model                   string
}

func (k indexKey) String() string {
	return k.ID + "#" + strconv.FormatInt(k.Incarnation, 10) + "." + strconv.FormatInt(k.ContentRev, 10) + "#" + k.Model
}

// parseIndexKey 从右侧解析，ID 本身可以含 #。
func parseIndexKey(s string) (indexKey, bool) {
	i := strings.LastIndexByte(s, '#')
	if i < 0 {
		return indexKey{}, false
	}
	j := strings.LastIndexByte(s[:i], '#')
	if j <= 0 {
		return indexKey{}, false
	}
	a, b, ok := strings.Cut(s[j+1:i], ".")
	inc, err1 := strconv.ParseInt(a, 10, 64)
	rev, err2 := strconv.ParseInt(b, 10, 64)
	if !ok || err1 != nil || err2 != nil {
		return indexKey{}, false
	}
	return indexKey{ID: s[:j], Incarnation: inc, ContentRev: rev, Model: s[i+1:]}, true
}

// modelTag 是嵌入模型名称的摘要：模型名可能含 # 等任意字符，不直接放进键。
func modelTag(model string) string {
	sum := sha256.Sum256([]byte(model))
	return hex.EncodeToString(sum[:6])
}

// indexSpace 是一类写入索引的对象。记录放在 ns#scope，实体名称向量放在它下面的 #entity 子命名空间；
// 两者都按作用域划分，Purge 后用 Clear 整体删除。
type indexSpace struct {
	sub string
	// current 报告 state 中的对象是否仍是键所写的版本。
	current func(s state, k indexKey) bool
	// objects 返回应当有向量的对象及其嵌入文本，按 ID 排序。
	objects func(s state) []indexObject
}

type indexObject struct {
	key  indexKey
	text string
}

var (
	recordSpace = indexSpace{
		current: func(s state, k indexKey) bool {
			r, ok := s.Records[k.ID]
			return ok && r.Valid() && r.Incarnation == k.Incarnation && r.ContentRev == k.ContentRev
		},
		objects: func(s state) []indexObject {
			var out []indexObject
			for _, id := range slices.Sorted(maps.Keys(s.Records)) {
				if r := s.Records[id]; r.Valid() {
					out = append(out, indexObject{recordKey(r), r.Content})
				}
			}
			return out
		},
	}
	// 实体向量只取决于名称，名称在一个实例内不变（合并别名会增加 ContentRev），所以只比较实例号，
	// 键固定取创建时的 ContentRev（1），重建不会为同一实体留下多个键。
	entitySpace = indexSpace{
		sub: "#entity",
		current: func(s state, k indexKey) bool {
			e, ok := s.Entities[k.ID]
			return ok && e.Incarnation == k.Incarnation
		},
		objects: func(s state) []indexObject {
			var out []indexObject
			for _, id := range slices.Sorted(maps.Keys(s.Entities)) {
				e := s.Entities[id]
				out = append(out, indexObject{entityKey(e), e.Name})
			}
			return out
		},
	}
	indexSpaces = []indexSpace{recordSpace, entitySpace}
)

func (sp indexSpace) namespace(ns []string, scope string) []string {
	out := append(slices.Clone(ns), "#"+scope)
	if sp.sub != "" {
		out = append(out, sp.sub)
	}
	return out
}

func recordKey(r Record) indexKey {
	return indexKey{ID: r.ID, Incarnation: r.Incarnation, ContentRev: r.ContentRev}
}

func keysOf(rs []Record) []indexKey {
	out := make([]indexKey, len(rs))
	for i, r := range rs {
		out[i] = recordKey(r)
	}
	return out
}

func entityKey(e Entity) indexKey {
	return indexKey{ID: e.ID, Incarnation: e.Incarnation, ContentRev: 1}
}

// adopted 报告键是否可以被采用：用当前模型写入，且对象仍是该版本（不变量 6）。
func (m *Memory) adopted(sp indexSpace, s state, k indexKey) bool {
	return k.Model == m.modelTag && sp.current(s, k)
}

// embed 返回正文的向量；没有配置索引时为 nil。
func (m *Memory) embed(ctx context.Context, text string) ([]float32, error) {
	if m.cfg.Index == nil {
		return nil, nil
	}
	vec, err := m.cfg.Embedder.Embed(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("生成记忆向量：%w", err)
	}
	return vec, nil
}

// writeIndex 在提交之后写入版本化键并复核（设计 4.4）：重新 read，作用域已不是 scope 或对象已不是所写版本时删除刚写的键。
// vecs[i] 为 nil 的跳过。写入失败时对象已经保存，返回的错误提示用 Reindex 修复。
func (m *Memory) writeIndex(ctx context.Context, ns []string, scope string, sp indexSpace, keys []indexKey, vecs [][]float32) error {
	if m.cfg.Index == nil {
		return nil
	}
	ins := sp.namespace(ns, scope)
	var written []indexKey
	var errs []error
	for i, k := range keys {
		if vecs[i] == nil {
			continue
		}
		k.Model = m.modelTag
		if err := m.cfg.Index.Upsert(ctx, ins, k.String(), vecs[i]); err != nil {
			errs = append(errs, fmt.Errorf("%q 已保存，写入向量索引失败（Reindex(ctx, false) 可修复）：%w", k.ID, err))
			continue
		}
		written = append(written, k)
	}
	if len(written) == 0 {
		return errors.Join(errs...)
	}
	cur, s, err := m.read(ctx, ns)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, k := range written {
		if cur != scope || !sp.current(s, k) {
			errs = append(errs, m.cfg.Index.Delete(ctx, ins, k.String()))
		}
	}
	return errors.Join(errs...)
}

// dropIndex 删除确认过期的键（对象已失效或已有新版本），只删当前模型写入的那个版本；其余残留由 Reindex 清理。
func (m *Memory) dropIndex(ctx context.Context, ns []string, scope string, sp indexSpace, keys ...indexKey) error {
	if m.cfg.Index == nil {
		return nil
	}
	var errs []error
	for _, k := range keys {
		k.Model = m.modelTag
		errs = append(errs, m.cfg.Index.Delete(ctx, sp.namespace(ns, scope), k.String()))
	}
	return errors.Join(errs...)
}

// vectorHits 在 s 所属作用域中检索查询，返回可采用键的相似度（按记录 ID）；没有索引或查询为空时为 nil。
func (m *Memory) vectorHits(ctx context.Context, ns []string, scope string, s state, q Query) (map[string]float64, error) {
	if m.cfg.Index == nil || strings.TrimSpace(q.Text) == "" {
		return nil, nil
	}
	vec, err := m.cfg.Embedder.Embed(ctx, q.Text)
	if err != nil {
		return nil, fmt.Errorf("生成查询向量：%w", err)
	}
	hits, err := m.cfg.Index.Search(ctx, recordSpace.namespace(ns, scope), vec, m.recallK(q)*4)
	if err != nil {
		return nil, err
	}
	vector := make(map[string]float64, len(hits))
	for _, h := range hits {
		if k, ok := parseIndexKey(h.ID); ok && m.adopted(recordSpace, s, k) {
			vector[k.ID] = max(h.Score, 0)
		}
	}
	return vector, nil
}

// Reindex 重建当前作用域的向量索引（设计 4.2）：为没有可采用键（force 时为全部）的有效记录与实体在锁外嵌入，
// 写入版本化键并复核，再删除列出的键中确认过期的（对象不存在、已失效、版本或嵌入模型已变化）。不写 state。
// 与并发写入交错时不采用旧键，也不删除别人刚写的当前键。
func (m *Memory) Reindex(ctx context.Context, force bool) error {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return err
	}
	if m.cfg.Index == nil {
		return nil
	}
	scope, s, err := m.read(ctx, ns)
	if err != nil {
		return err
	}
	for _, sp := range indexSpaces {
		if err := m.reindex(ctx, ns, scope, s, sp, force); err != nil {
			return err
		}
	}
	return nil
}

func (m *Memory) reindex(ctx context.Context, ns []string, scope string, s state, sp indexSpace, force bool) error {
	ins := sp.namespace(ns, scope)
	listed, err := m.cfg.Index.IDs(ctx, ins)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, raw := range listed {
		if k, ok := parseIndexKey(raw); ok && m.adopted(sp, s, k) {
			have[k.ID] = true
		}
	}
	var keys []indexKey
	var vecs [][]float32
	for _, o := range sp.objects(s) {
		if !force && have[o.key.ID] {
			continue
		}
		vec, err := m.embed(ctx, o.text)
		if err != nil {
			return err
		}
		keys, vecs = append(keys, o.key), append(vecs, vec)
	}
	if err := m.writeIndex(ctx, ns, scope, sp, keys, vecs); err != nil {
		return err
	}
	// 在列出之后重新读取再判定：列出时已存在的键，只有在此后的 state 中仍不可采用才确认过期。
	cur, s, err := m.read(ctx, ns)
	if err != nil || cur != scope {
		return err // 作用域已切换：旧作用域整体由 retiring 清理
	}
	var errs []error
	for _, raw := range listed {
		if k, ok := parseIndexKey(raw); !ok || !m.adopted(sp, s, k) {
			errs = append(errs, m.cfg.Index.Delete(ctx, ins, raw))
		}
	}
	return errors.Join(errs...)
}
