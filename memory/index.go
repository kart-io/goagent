package memory

import (
	"context"
	"math"
	"slices"
	"strings"
	"sync"

	agentErrors "github.com/kart-io/goagent/errors"
)

// Hit 是一次向量检索的结果，Score 为余弦相似度。
type Hit struct {
	ID    string
	Score float64
}

// Index 是按命名空间隔离的向量索引，是记录库的投影：丢失后可用 Memory.Reindex 重建。
// Search 只在给定命名空间内检索，先过滤再取 k 个。
type Index interface {
	Upsert(ctx context.Context, namespace []string, id string, vector []float32) error
	Delete(ctx context.Context, namespace []string, id string) error
	Search(ctx context.Context, namespace []string, vector []float32, k int) ([]Hit, error)
}

// MemoryIndex 是进程内的精确余弦检索，适合测试与小规模部署。
type MemoryIndex struct {
	mu     sync.RWMutex
	spaces map[string]map[string][]float64
}

// NewMemoryIndex 创建空索引。
func NewMemoryIndex() *MemoryIndex { return &MemoryIndex{spaces: map[string]map[string][]float64{}} }

func spaceKey(ns []string) string { return strings.Join(ns, "\x00") }

// Upsert 归一化并复制向量；同一命名空间内维度须一致，拒绝零向量与非有限值。
func (x *MemoryIndex) Upsert(_ context.Context, ns []string, id string, vector []float32) error {
	if err := validNamespace(ns); err != nil {
		return err
	}
	unit, err := normalize(vector)
	if err != nil {
		return err
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	space := x.spaces[spaceKey(ns)]
	if space == nil {
		space = map[string][]float64{}
		x.spaces[spaceKey(ns)] = space
	}
	for other, v := range space {
		if other != id && len(v) != len(unit) {
			return agentErrors.Newf(agentErrors.CodeInvalidInput, "向量维度 %d 与索引中的 %d 不一致", len(unit), len(v)).WithComponent("memory").WithOperation("index_upsert")
		}
		break
	}
	space[id] = unit
	return nil
}

func (x *MemoryIndex) Delete(_ context.Context, ns []string, id string) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	delete(x.spaces[spaceKey(ns)], id)
	return nil
}

func (x *MemoryIndex) Search(_ context.Context, ns []string, vector []float32, k int) ([]Hit, error) {
	if k <= 0 {
		return nil, nil
	}
	unit, err := normalize(vector)
	if err != nil {
		return nil, err
	}
	x.mu.RLock()
	defer x.mu.RUnlock()
	var hits []Hit
	for id, v := range x.spaces[spaceKey(ns)] {
		if len(v) != len(unit) {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "查询向量维度 %d 与索引中的 %d 不一致", len(unit), len(v)).WithComponent("memory").WithOperation("index_search")
		}
		dot := 0.0
		for i := range v {
			dot += v[i] * unit[i]
		}
		hits = append(hits, Hit{ID: id, Score: dot})
	}
	slices.SortFunc(hits, func(a, b Hit) int {
		if a.Score != b.Score {
			if a.Score > b.Score {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	return hits[:min(k, len(hits))], nil
}

// normalize 以 float64 计算范数，避免大幅值 float32 平方溢出。
func normalize(v []float32) ([]float64, error) {
	out := make([]float64, len(v))
	scale := 0.0
	for i, f := range v {
		x := float64(f)
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "向量含非有限值").WithComponent("memory").WithOperation("normalize")
		}
		out[i] = x
		scale = max(scale, math.Abs(x))
	}
	if scale == 0 {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "向量为空或全为零").WithComponent("memory").WithOperation("normalize")
	}
	norm := 0.0
	for i := range out {
		out[i] /= scale
		norm += out[i] * out[i]
	}
	norm = math.Sqrt(norm)
	for i := range out {
		out[i] /= norm
	}
	return out, nil
}
