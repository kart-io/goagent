package memory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/store"
)

// G0 在 store.Store 中的布局：记录与图谱以 JSON 文本存于 {"memory",类别,命名空间...}，任务存于 {"memory","jobs"}，键为任务 Key。
var (
	legacyKinds  = []string{"records", "entities", "edges", "communities"}
	legacyJobsNS = []string{"memory", "jobs"}
)

func legacyNS(kind string, ns []string) []string { return append([]string{"memory", kind}, ns...) }

// MigrationSource 是 G0 的旧数据。
type MigrationSource struct {
	// Store 是旧记录、图谱与任务所在的存储；Clear 须只清精确的命名空间，不递归。
	Store store.Store
	// Index 是旧向量索引（可为 nil）；ClearLegacy 时清空其中的旧命名空间（G0 的向量直接写在命名空间 ns 下）。
	Index Index
}

// MigrateOptions 是迁移的选项。
type MigrateOptions struct {
	// ClearLegacy 为真时，迁移成功（或此前已迁移）后删除这个命名空间的旧记录、图谱、任务与旧向量。
	ClearLegacy bool
}

// MigrationStatus 是一个命名空间的迁移结果。
type MigrationStatus string

const (
	// MigrationImported：本次导入了旧记录。
	MigrationImported MigrationStatus = "imported"
	// MigrationAlreadyDone：此前已迁移，不再导入。
	MigrationAlreadyDone MigrationStatus = "already_migrated"
	// MigrationConflict：新存储中已有数据（迁移前没有停止流量），不导入也不清理。
	MigrationConflict MigrationStatus = "conflict"
	// MigrationFailed：出错，重跑即可补完。
	MigrationFailed MigrationStatus = "failed"
)

// NamespaceMigration 是一个命名空间的迁移报告。
type NamespaceMigration struct {
	Namespace []string
	Status    MigrationStatus
	// Records 是旧存储中这个命名空间的记录数（导入时全部写入）。
	Records int
	// Cleared 表示旧数据已全部清理（ClearLegacy）。
	Cleared bool
	Err     error
}

// MigrationReport 是 MigrateFromStore 的结果。
type MigrationReport struct {
	Namespaces []NamespaceMigration
	// Jobs 是旧存储中的全部抽取任务，不迁入。它们没有作用域，原样重新登记会被丢弃（见 Enqueue）。
	Jobs []Job
	// Unlisted 是旧任务中出现、但不在本次命名空间清单中的命名空间，可能是宿主漏列的。
	Unlisted [][]string
}

// MigrateFromStore 把 G0 旧存储中给定命名空间的记录迁入 Durable（设计 docs/design/MEMORY_G1_STORAGE.md 第 5 节）。
// 关系、实体、社区不迁入；旧任务不迁入，列在报告中。调用前须停止 Agent 流量与 Worker。
// 每个命名空间两阶段提交：meta 写入迁移 ID → state 合并导入 → meta 标记已迁移；中途失败或崩溃后重跑会补完，不会重复导入。
// 已迁移的命名空间不再导入，Purge 之后重跑也不会让旧数据复活。导入的失效记录中，没有取代者的一律视为已遗忘（清空正文）。
// 导入之后为有效记录补写向量，失败时留给 Reindex(false)。返回的错误汇总各命名空间的冲突与失败，报告给出逐个结果。
func (m *Memory) MigrateFromStore(ctx context.Context, legacy MigrationSource, namespaces [][]string, opts MigrateOptions) (MigrationReport, error) {
	if legacy.Store == nil {
		return MigrationReport{}, agentErrors.Newf(agentErrors.CodeInvalidInput, "迁移需要旧存储").WithComponent("memory").WithOperation("migrate")
	}
	jobs, err := legacyJobs(ctx, legacy.Store)
	if err != nil {
		return MigrationReport{}, err
	}
	report := MigrationReport{Jobs: jobs, Unlisted: unlistedNamespaces(jobs, namespaces)}
	var errs []error
	for _, ns := range namespaces {
		r := m.migrateNamespace(ctx, legacy, slices.Clone(ns), jobs, opts)
		if r.Err != nil {
			errs = append(errs, fmt.Errorf("命名空间 %v：%w", ns, r.Err))
		}
		report.Namespaces = append(report.Namespaces, r)
	}
	return report, errors.Join(errs...)
}

func (m *Memory) migrateNamespace(ctx context.Context, legacy MigrationSource, ns []string, jobs []Job, opts MigrateOptions) NamespaceMigration {
	r := NamespaceMigration{Namespace: ns}
	if r.Err = validNamespace(ns); r.Err != nil {
		r.Status = MigrationFailed
		return r
	}
	records, err := legacyRecords(ctx, legacy.Store, ns)
	if err != nil {
		r.Status, r.Err = MigrationFailed, err
		return r
	}
	r.Records = len(records)
	if r.Status, r.Err = m.importLegacy(ctx, ns, records); r.Err != nil {
		return r
	}
	// 向量失败不影响迁移结果：记录已导入，Reindex(false) 可以补写。
	if err := m.Reindex(WithNamespace(ctx, ns...), false); err != nil {
		r.Err = fmt.Errorf("记录已导入，补写向量失败（Reindex(ctx, false) 可修复）：%w", err)
	}
	if opts.ClearLegacy {
		if err := clearLegacy(ctx, legacy, ns, jobs); err != nil {
			r.Err = errors.Join(r.Err, fmt.Errorf("清理旧数据：%w", err))
		} else {
			r.Cleared = true
		}
	}
	if r.Err != nil {
		r.Status = MigrationFailed
	}
	return r
}

// importLegacy 在 meta 锁内完成两阶段提交（加锁顺序 meta → state）。
func (m *Memory) importLegacy(ctx context.Context, ns []string, records []Record) (MigrationStatus, error) {
	st := m.cfg.Durable
	unlock, err := st.Lock(ctx, metaID(ns))
	if err != nil {
		return MigrationFailed, err
	}
	defer func() { _ = unlock() }()
	meta, err := m.loadMeta(ctx, ns)
	if err != nil {
		return MigrationFailed, err
	}
	id := meta.Migrating
	// 先看 Migrating：阶段二已提交、阶段三未完成时 state 中已有导入的记录，不能误判为冲突。
	if id == "" {
		if !meta.Migrated.IsZero() {
			return MigrationAlreadyDone, nil
		}
		_, s, err := m.read(ctx, ns)
		if err != nil {
			return MigrationFailed, err
		}
		if hasData(s) {
			return MigrationConflict, conflict("新存储中已有这个命名空间的数据：迁移前须停止流量")
		}
		id = uuid.NewString()
		meta.Migrating = id
		if err := saveMeta(ctx, st, ns, meta); err != nil {
			return MigrationFailed, err
		}
	}
	_, err = m.mutate(ctx, ns, meta.Current, func(t *tx) error {
		if t.MigrationID() == id {
			return errNoChange // 阶段二已提交
		}
		if t.Records.Len()+t.Entities.Len()+t.Edges.Len() > 0 {
			return errMigrationConflict
		}
		for _, r := range records {
			r.Incarnation, r.Revision, r.ContentRev = t.incarnation(), 1, 1
			t.Records.Put(r.ID, r)
		}
		t.SetLastForgetSeq(t.nextSeq())
		t.SetMigrationID(id)
		return nil
	})
	if errors.Is(err, errMigrationConflict) {
		// 阶段二没有提交：撤销阶段一，否则 Purge 会一直被拒绝。
		meta.Migrating = ""
		return MigrationConflict, errors.Join(conflict("新存储中已有这个命名空间的数据：迁移前须停止流量"), saveMeta(ctx, st, ns, meta))
	}
	if err != nil {
		return MigrationFailed, err
	}
	meta.Migrating, meta.Migrated = "", m.now()
	if err := saveMeta(ctx, st, ns, meta); err != nil {
		return MigrationFailed, err
	}
	return MigrationImported, nil
}

var errMigrationConflict = errors.New("新存储中已有数据")

func hasData(s state) bool { return len(s.Records)+len(s.Entities)+len(s.Edges) > 0 }

// saveMeta 写入 meta；调用方持有 meta 锁。
func saveMeta(ctx context.Context, st durable.Store, ns []string, meta metaState) error {
	snap, err := encodeSnapshot(metaID(ns), meta)
	if err != nil {
		return err
	}
	return st.Save(context.WithoutCancel(ctx), metaID(ns), snap)
}

// legacyRecords 读取并转换旧记录：命名空间取 ns；失效记录有取代者的记为 superseded，否则记为已遗忘并清空正文
// （G0 不区分遗忘、退回与过期，按最严格的处理，不让被遗忘的内容在迁移后可读）。
func legacyRecords(ctx context.Context, src store.Store, ns []string) ([]Record, error) {
	values, err := src.Search(ctx, legacyNS("records", ns), nil)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(values))
	for _, v := range values {
		var r Record
		if err := decodeLegacy(v, &r); err != nil {
			return nil, err
		}
		r.Namespace, r.InvalidReason = ns, ""
		r.Keywords, r.Entities, r.Sources = nilIfEmpty(r.Keywords), nilIfEmpty(r.Entities), nilIfEmpty(r.Sources)
		if !r.InvalidAt.IsZero() {
			if r.SupersededBy != "" {
				r.InvalidReason = ReasonSuperseded
			} else {
				r.InvalidReason = ReasonForgotten
				r.Content, r.Keywords, r.Entities = "", nil, nil
			}
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Record) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

func legacyJobs(ctx context.Context, src store.Store) ([]Job, error) {
	values, err := src.Search(ctx, legacyJobsNS, nil)
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(values))
	for _, v := range values {
		var j Job
		if err := decodeLegacy(v, &j); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	slices.SortFunc(out, func(a, b Job) int { return strings.Compare(a.Key, b.Key) })
	return out, nil
}

func decodeLegacy(v *store.Value, out any) error {
	text, ok := v.Value.(string)
	if !ok {
		return agentErrors.Newf(agentErrors.CodeStoreSerialization, "旧记忆数据 %q 不是 JSON 文本", v.Key).WithComponent("memory").WithOperation("migrate")
	}
	return json.Unmarshal([]byte(text), out)
}

func unlistedNamespaces(jobs []Job, listed [][]string) [][]string {
	seen := map[string]bool{}
	for _, ns := range listed {
		seen[store.NamespaceKey(ns)] = true
	}
	var out [][]string
	for _, j := range jobs {
		if key := store.NamespaceKey(j.Namespace); !seen[key] {
			seen[key] = true
			out = append(out, slices.Clone(j.Namespace))
		}
	}
	return out
}

// clearLegacy 删除这个命名空间的旧记录与图谱、属于它的旧任务键与旧向量；每一步都可重复执行。
func clearLegacy(ctx context.Context, legacy MigrationSource, ns []string, jobs []Job) error {
	var errs []error
	for _, kind := range legacyKinds {
		errs = append(errs, legacy.Store.Clear(ctx, legacyNS(kind, ns)))
	}
	for _, j := range jobs {
		if slices.Equal(j.Namespace, ns) {
			if err := legacy.Store.Delete(ctx, legacyJobsNS, j.Key); err != nil && !agentErrors.IsCode(err, agentErrors.CodeStoreNotFound) {
				errs = append(errs, err)
			}
		}
	}
	if legacy.Index != nil {
		errs = append(errs, legacy.Index.Clear(ctx, ns))
	}
	return errors.Join(errs...)
}
