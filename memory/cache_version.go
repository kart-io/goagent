package memory

import (
	"context"
	"errors"
	"slices"

	agentErrors "github.com/kart-io/goagent/errors"
)

// NamespaceVersion 是一个读命名空间的版本：遗忘、回退与 Purge 都会改变它，模型缓存把它放进键里使旧条目失效。
type NamespaceVersion struct {
	Namespace     []string
	Scope         string
	LastForgetSeq int64
	LastRewindSeq int64
}

// CacheVersion 读取每个读命名空间的最新版本，按命名空间排序；不取锁，只读 meta 与 head。
// 两次读 meta 之间作用域改变（Purge）时重读，最多 readAttempts 次后返回冲突；不读基础快照，所以不像 read 那样因压缩重读。
// head 所在作用域已删除时按空 head 处理，是否采用由第二次读 meta 决定。
func (m *Memory) CacheVersion(ctx context.Context, read [][]string) ([]NamespaceVersion, error) {
	out := make([]NamespaceVersion, 0, len(read))
	for _, ns := range read {
		v, err := m.namespaceVersion(ctx, slices.Clone(ns))
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b NamespaceVersion) int { return slices.Compare(a.Namespace, b.Namespace) })
	return out, nil
}

func (m *Memory) namespaceVersion(ctx context.Context, ns []string) (NamespaceVersion, error) {
	for range readAttempts {
		meta, err := m.loadMeta(ctx, ns)
		if err != nil {
			return NamespaceVersion{}, err
		}
		h, err := loadHead(ctx, m.scoped(ns, meta.Current))
		if err != nil && !errors.Is(err, agentErrors.ErrScopeRemoved) {
			return NamespaceVersion{}, err
		}
		after, err := m.loadMeta(ctx, ns)
		if err != nil {
			return NamespaceVersion{}, err
		}
		if after.Current == meta.Current {
			return NamespaceVersion{Namespace: ns, Scope: meta.Current, LastForgetSeq: h.LastForgetSeq, LastRewindSeq: h.LastRewindSeq}, nil
		}
	}
	return NamespaceVersion{}, conflict("长期记忆的作用域在读取期间反复改变")
}
