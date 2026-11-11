// Package memory 是跨会话的长期记忆：记录库保存正文，向量索引是可重建的投影，抽取与整理由 Worker 异步完成。
// 会话内的历史与压缩由会话日志与 contextpolicy.Compaction 负责，不在本包。设计见 docs/design/MEMORY_DESIGN.md。
package memory

import (
	"context"
	"slices"
	"strings"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
)

// Kind 是记录的功能类别。
type Kind string

const (
	// KindFact 是跨会话成立的事实与偏好（语义记忆）。
	KindFact Kind = "fact"
	// KindEpisode 是发生过的事件（情景记忆）。
	KindEpisode Kind = "episode"
	// KindReflection 是从经历中提炼的结论与教训（Generative Agents 反思、Reflexion）。
	KindReflection Kind = "reflection"
	// KindSkill 是可复用的做法（程序记忆）。
	KindSkill Kind = "skill"
)

var kinds = []Kind{KindFact, KindEpisode, KindReflection, KindSkill}

// Source 是记录或关系的一个来源：会话、事件序号区间与生成它的抽取任务。
type Source struct {
	Session string `json:"session,omitempty"`
	From    int64  `json:"from,omitzero"`
	To      int64  `json:"to,omitzero"`
	Job     string `json:"job,omitempty"`
}

// Record 是一条长期记忆。InvalidAt 非零表示已失效：被新事实否定或合并（SupersededBy 指向新记录）、被遗忘或来源轮次被退回；
// 失效记录不再被召回，保留 Retention.InvalidRetention 后删除。
type Record struct {
	ID           string    `json:"id"`
	Namespace    []string  `json:"namespace"`
	Kind         Kind      `json:"kind"`
	Content      string    `json:"content"`
	Keywords     []string  `json:"keywords,omitempty"`
	Entities     []string  `json:"entities,omitempty"`
	Importance   float64   `json:"importance"`
	Pinned       bool      `json:"pinned,omitzero"`
	ValidFrom    time.Time `json:"valid_from"`
	InvalidAt    time.Time `json:"invalid_at,omitzero"`
	SupersededBy string    `json:"superseded_by,omitempty"`
	// Sources 是内容所依据的全部轮次；合并更新时继承旧记录的来源，手动写入时为空。
	Sources     []Source  `json:"sources,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	LastAccess  time.Time `json:"last_access"`
	AccessCount int       `json:"access_count,omitzero"`
	// EmbedModel 是写入索引时的嵌入模型；与当前模型不同时 Reindex 重建。
	EmbedModel string `json:"embed_model,omitempty"`
	// Reflected 表示该情景已参与过反思。
	Reflected bool `json:"reflected,omitzero"`
}

// rewound 报告是否有来源位于会话 sessionID 中序号 to 之后。
func rewound(sources []Source, sessionID string, to int64) bool {
	return slices.ContainsFunc(sources, func(s Source) bool { return s.Session == sessionID && s.From > to })
}

// Valid 报告记录是否仍然有效。
func (r Record) Valid() bool { return r.InvalidAt.IsZero() }

func (r Record) validate() error {
	if r.ID == "" || strings.TrimSpace(r.Content) == "" || !slices.Contains(kinds, r.Kind) || r.Importance < 0 || r.Importance > 1 {
		return agentErrors.Newf(agentErrors.CodeInvalidInput, "记录需要 ID、内容、合法类别与 [0,1] 内的重要度").WithComponent("memory").WithOperation("validate")
	}
	return validNamespace(r.Namespace)
}

type namespaceKey struct{}

// WithNamespace 绑定记忆的命名空间，如 {租户, 用户}。命名空间须由宿主依据可信身份给出：读写只在该命名空间内进行。
func WithNamespace(ctx context.Context, namespace ...string) context.Context {
	return context.WithValue(ctx, namespaceKey{}, slices.Clone(namespace))
}

// NamespaceFrom 返回绑定的命名空间；未绑定时为 nil。
func NamespaceFrom(ctx context.Context) []string {
	ns, _ := ctx.Value(namespaceKey{}).([]string)
	return slices.Clone(ns)
}

// requireNamespace 返回绑定的合法命名空间；没有时拒绝读写，避免落入所有调用方共享的空间。
// context 中有轮次绑定时取它的写命名空间，轮次内的读写不随宿主之后的绑定改变。
func requireNamespace(ctx context.Context) ([]string, error) {
	ns := NamespaceFrom(ctx)
	if t, ok := TurnFrom(ctx); ok {
		ns = slices.Clone(t.Write)
	}
	if err := validNamespace(ns); err != nil {
		return nil, err
	}
	return ns, nil
}

func validNamespace(ns []string) error {
	if len(ns) == 0 || slices.ContainsFunc(ns, func(s string) bool { return strings.TrimSpace(s) == "" }) {
		return agentErrors.Newf(agentErrors.CodeInvalidInput, "长期记忆需要命名空间：用 memory.WithNamespace 绑定可信身份").WithComponent("memory").WithOperation("namespace")
	}
	return nil
}
