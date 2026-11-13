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

// InvalidReason 是记录失效的原因。
type InvalidReason string

const (
	// ReasonSuperseded：被新事实否定或合并，SupersededBy 指向新记录。
	ReasonSuperseded InvalidReason = "superseded"
	// ReasonRewound：来源轮次被退回。
	ReasonRewound InvalidReason = "rewound"
	// ReasonExpired：TTL 到期或超出容量被淘汰。
	ReasonExpired InvalidReason = "expired"
	// ReasonForgotten：被遗忘，正文已清空，是终态。
	ReasonForgotten InvalidReason = "forgotten"
	// ReasonUnderived：所依据的记录被遗忘或回退。依据被遗忘时正文同时清空。
	ReasonUnderived InvalidReason = "underived"
)

// Record 是一条长期记忆。InvalidAt 非零表示已失效，InvalidReason 给出原因；失效记录不再被召回，保留 Retention.InvalidRetention 后删除。
// Incarnation 是记录在作用域内的实例号，创建时分配、不再改变；Revision 每次修改加一，ContentRev 只在正文改变时加一，二者从 1 起。
type Record struct {
	ID            string        `json:"id"`
	Namespace     []string      `json:"namespace"`
	Kind          Kind          `json:"kind"`
	Content       string        `json:"content"`
	Keywords      []string      `json:"keywords,omitempty"`
	Entities      []string      `json:"entities,omitempty"`
	Importance    float64       `json:"importance"`
	Pinned        bool          `json:"pinned,omitzero"`
	ValidFrom     time.Time     `json:"valid_from"`
	InvalidAt     time.Time     `json:"invalid_at,omitzero"`
	InvalidReason InvalidReason `json:"invalid_reason,omitempty"`
	SupersededBy  string        `json:"superseded_by,omitempty"`
	// Sources 是内容所依据的全部轮次；合并更新时继承旧记录的来源，手动写入时为空。
	Sources []Source `json:"sources,omitempty"`
	// MergedFrom 是整理合并时展示给模型的全部候选；其中任一条被擦除，本记录随之擦除（underived）。
	MergedFrom []RecordRef `json:"merged_from,omitempty"`
	// ReflectedFrom 是反思所依据的情景；其中任一条被擦除，本记录随之擦除，被回退时本记录失效（underived）。
	ReflectedFrom []RecordRef `json:"reflected_from,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
	Incarnation   int64       `json:"incarnation,omitzero"`
	Revision      int64       `json:"revision,omitzero"`
	ContentRev    int64       `json:"content_rev,omitzero"`
	// Reflected 表示该情景已参与过反思。
	Reflected bool `json:"reflected,omitzero"`
}

// rewound 报告是否有来源位于会话 sessionID 中序号 to 之后。
func rewound(sources []Source, sessionID string, to int64) bool {
	return slices.ContainsFunc(sources, func(s Source) bool { return s.Session == sessionID && s.From > to })
}

// Valid 报告记录是否仍然有效。
func (r Record) Valid() bool { return r.InvalidAt.IsZero() }

// invalidated 返回在 at 以 reason 失效的副本；by 是取代它的记录（只用于 superseded）。
func (r Record) invalidated(at time.Time, reason InvalidReason, by string) Record {
	r.InvalidAt, r.InvalidReason, r.SupersededBy = at, reason, by
	r.Revision++
	return r
}

func (r Record) validate() error {
	if r.ID == "" || strings.TrimSpace(r.Content) == "" || !slices.Contains(kinds, r.Kind) || r.Importance < 0 || r.Importance > 1 {
		return agentErrors.Newf(agentErrors.CodeInvalidInput, "记录需要 ID、内容、合法类别与 [0,1] 内的重要度").WithComponent("memory").WithOperation("validate")
	}
	if r.InvalidAt.IsZero() != (r.InvalidReason == "") {
		return agentErrors.Newf(agentErrors.CodeInvalidInput, "记录的失效时间与失效原因须同时设置").WithComponent("memory").WithOperation("validate")
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
	// 索引与缓存以 NUL 连接命名空间组件，组件含 NUL 时不同的命名空间会得到同一个键。
	if slices.ContainsFunc(ns, func(s string) bool { return strings.ContainsRune(s, 0) }) {
		return agentErrors.Newf(agentErrors.CodeInvalidInput, "长期记忆的命名空间组件不能包含 NUL").WithComponent("memory").WithOperation("namespace")
	}
	return nil
}
