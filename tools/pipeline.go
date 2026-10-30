package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// Call 是交给管线各阶段的一次工具调用。Args 只读：参数已展示给模型和人，管线不改写参数，
// 需要换一种做法时用 PreSubstitute 声明替代调用。
type Call struct {
	Tool         string
	CallID       string
	ExecutionKey string
	CallerID     string
	SessionID    string
	Args         map[string]any
}

// PreKind 是调用前阶段的裁决。零值为 PreAllow。
type PreKind int

const (
	// PreAllow 不提出异议；全部 PreHook 都放行时进入 Guard。
	PreAllow PreKind = iota
	// PreDeny 不执行工具，以 Rejected(Feedback) 交给模型修订。
	PreDeny
	// PreAsk 交给 Approver 由人确认。
	PreAsk
	// PreStop 终止整个运行，返回 *StopError。
	PreStop
	// PreSubstitute 执行 Substitute 声明的另一个工具代替本次调用，结果元数据记录替代关系。
	PreSubstitute
)

// PreDecision 是 PreHook 的裁决。
type PreDecision struct {
	Kind PreKind
	// Feedback 在 Deny 时交给模型，在 Ask 时展示给审批人，在 Stop 时作为终止原因。
	Feedback string
	// Grant 只用于 Ask：审批人选择"本会话允许"时，按它放行同会话中后续匹配的调用。
	// 为 nil 或调用没有会话 ID 时不提供会话级批准，AllowedSession 按 AllowedOnce 处理。
	Grant SessionGrant
	// Substitute 只用于 PreSubstitute。
	Substitute *Substitution
}

// Substitution 声明替代调用。替代调用按目标工具的 Schema 校验并重新经过完整管线，但不能再被替代。
type Substitution struct {
	Tool string
	Args map[string]any
}

// SessionGrant 是会话级批准放行的调用范围，应尽量收窄到参数级（如命令前缀、路径前缀），
// 而不是整个工具。Describe 以人可读的形式展示给审批人。
type SessionGrant interface {
	Describe() string
	Matches(call *Call) bool
}

// PreHook 全部依次执行，裁决按 Stop > Deny > Substitute > Ask > Allow 聚合，与钩子顺序无关：
// 多个 Deny 的反馈合并交给模型；多个 Ask 合并为一次审批；多个 Substitute 视为错误。返回 error 中止运行。
type PreHook func(ctx context.Context, call *Call) (PreDecision, error)

// Guard 只能拒绝不能放行，用于不可绕过的硬性约束；返回非空串即拒绝，以 Rejected 交给模型。
// 它在 PreHook 与审批之后执行，人的批准也无法越过它。
type Guard func(ctx context.Context, call *Call) (denyReason string)

// Around 包住工具执行（单次超时之内），洋葱式组合，必须调用 next 或自行返回结果。
type Around func(ctx context.Context, call *Call, next func(context.Context) (*interfaces.ToolOutput, error)) (*interfaces.ToolOutput, error)

// PostDecision 是 PostHook 的裁决。Replace 为 nil 表示接受结果。
type PostDecision struct {
	Replace *interfaces.ToolOutput
	// AdditionalContext 追加到结果的 AdditionalContext，Builder 在本批工具结果之后作为 user 消息注入。
	AdditionalContext []string
}

// PostHook 依次处理成功结果和 Rejected 结果（含被拒绝的调用），可替换结果或追加上下文。返回 error 中止运行。
type PostHook func(ctx context.Context, call *Call, out *interfaces.ToolOutput) (PostDecision, error)

// ApprovalOutcome 是审批结果。零值为 ApprovalUnavailable：没有人能作答时按拒绝处理。
type ApprovalOutcome int

const (
	ApprovalUnavailable ApprovalOutcome = iota
	ApprovalAllowedOnce
	ApprovalAllowedSession
	ApprovalRejected
	ApprovalCancelled
)

// ApprovalRequest 是交给 Approver 的待确认调用。
type ApprovalRequest struct {
	Call   *Call
	Reason string
	// Grant 非 nil 时审批界面可提供"本会话允许"，并展示 Grant.Describe()。
	Grant SessionGrant
}

// ApprovalResult 是审批人的答复；Feedback 在拒绝时交给模型。
type ApprovalResult struct {
	Outcome  ApprovalOutcome
	Feedback string
}

// Approver 由宿主实现（命令行提示、IM 卡片等）。返回 error 中止运行。
type Approver interface {
	Approve(ctx context.Context, req ApprovalRequest) (ApprovalResult, error)
}

// ApproverFunc 把函数适配为 Approver。
type ApproverFunc func(ctx context.Context, req ApprovalRequest) (ApprovalResult, error)

// Approve 实现 Approver。
func (f ApproverFunc) Approve(ctx context.Context, req ApprovalRequest) (ApprovalResult, error) {
	return f(ctx, req)
}

const defaultApprovalTimeout = 10 * time.Minute

// Pipeline 是 Dispatcher 对每次模型可见调用执行的管线：PreHook → 审批 → Guard → Around（含超时）→ 工具 → PostHook。
// 参数不合法的调用不进入管线。准入阶段（PreHook、审批、Guard）在操作日志之外执行：终止、取消和出错不留记录、
// 不消耗尝试次数；拒绝结果与执行结果一样写入日志，回放已完成的调用不再经过准入阶段。
// 准入阶段使用可配置的独立准入槽位，不占工具执行槽位，也不计入 DispatchConfig.Timeout；Around、工具与 PostHook 在超时之内。
type Pipeline struct {
	Pre      []PreHook
	Approver Approver
	// ApprovalTimeout 是单次等待审批的上限，默认 10 分钟，超时后迟到的批准也按拒绝处理。
	ApprovalTimeout time.Duration
	// GrantTTL 从批准时起限制新会话授权的有效期；小于等于零表示不过期。
	GrantTTL time.Duration
	Guards   []Guard
	Around   []Around
	Post     []PostHook

	grantsMu sync.Mutex // 串行协调授权映射的清理、新增和撤销
	grants   sync.Map   // SessionID -> *grantSet
	inflight sync.Map   // 审批合并键 -> *pendingApproval
}

// StopError 表示 PreStop 终止了运行。
type StopError struct {
	Tool   string
	Reason string
}

func (e *StopError) Error() string {
	return fmt.Sprintf("tool %q stopped the run: %s", e.Tool, e.Reason)
}

// grantSet 按提出 Ask 的 PreHook 分别记录授权，一个钩子的授权不会满足另一个钩子的 Ask。
type grantSet struct {
	mu     sync.Mutex
	grants []hookGrant
}

type hookGrant struct {
	hook      int
	grant     SessionGrant
	expiresAt time.Time
}

// granted 报告每个提出 Ask 的钩子是否都已有本会话内匹配的授权。
func (p *Pipeline) granted(call *Call, asks []hookAsk) bool {
	if call.SessionID == "" {
		return false
	}
	grants := p.grantSnapshot(call.SessionID)
	now := time.Now()
	for _, a := range asks {
		if !slices.ContainsFunc(grants, func(g hookGrant) bool { return !g.expired(now) && g.hook == a.hook && g.grant.Matches(call) }) {
			return false
		}
	}
	return len(grants) > 0
}

// 调用宿主 Matches/Describe 前取得快照，避免持锁执行宿主回调。
func (p *Pipeline) grantSnapshot(sessionID string) []hookGrant {
	p.grantsMu.Lock()
	defer p.grantsMu.Unlock()
	v, ok := p.grants.Load(sessionID)
	if !ok {
		return nil
	}
	set := v.(*grantSet)
	set.mu.Lock()
	defer set.mu.Unlock()
	return slices.Clone(set.grants)
}

func (p *Pipeline) addGrants(sessionID string, asks []hookAsk) {
	p.grantsMu.Lock()
	defer p.grantsMu.Unlock()
	var expiresAt time.Time
	if p.GrantTTL > 0 {
		expiresAt = time.Now().Add(p.GrantTTL)
	}
	v, _ := p.grants.LoadOrStore(sessionID, &grantSet{})
	set := v.(*grantSet)
	set.mu.Lock()
	defer set.mu.Unlock()
	for _, a := range asks {
		set.grants = append(set.grants, hookGrant{hook: a.hook, grant: a.decision.Grant, expiresAt: expiresAt})
	}
}

// Grants 返回会话中已批准的会话级授权说明，供宿主展示或撤销前核对。
func (p *Pipeline) Grants(sessionID string) []string {
	grants := p.grantSnapshot(sessionID)
	if grants == nil {
		return nil
	}
	out := make([]string, 0, len(grants))
	now := time.Now()
	for _, g := range grants {
		if !g.expired(now) {
			out = append(out, g.grant.Describe())
		}
	}
	return out
}

// RevokeGrants 清除会话的全部会话级授权。
func (p *Pipeline) RevokeGrants(sessionID string) {
	p.grantsMu.Lock()
	defer p.grantsMu.Unlock()
	p.grants.Delete(sessionID)
}

func (g hookGrant) expired(now time.Time) bool {
	return !g.expiresAt.IsZero() && !now.Before(g.expiresAt)
}

// PruneGrants 删除过期授权和空会话条目，返回删除的授权条目数。
// 此方法不修改或取消在途审批；过期授权即使尚未清理，也不能继续放行调用。
func (p *Pipeline) PruneGrants() int {
	p.grantsMu.Lock()
	defer p.grantsMu.Unlock()
	now := time.Now()
	removed := 0
	p.grants.Range(func(key, value any) bool {
		set := value.(*grantSet)
		set.mu.Lock()
		before := len(set.grants)
		set.grants = slices.DeleteFunc(set.grants, func(g hookGrant) bool { return g.expired(now) })
		removed += before - len(set.grants)
		if len(set.grants) == 0 {
			p.grants.Delete(key)
		}
		set.mu.Unlock()
		return true
	})
	return removed
}

type pendingApproval struct {
	done   chan struct{}
	result ApprovalResult
	err    error
}

// approve 向审批人请求确认。同一会话中工具与参数完全相同的并发请求只询问一次；没有会话 ID 时无法区分运行，不合并。
func (p *Pipeline) approve(ctx context.Context, req ApprovalRequest) (ApprovalResult, error) {
	if p.Approver == nil {
		return ApprovalResult{Outcome: ApprovalUnavailable}, nil
	}
	if req.Call.SessionID == "" {
		return p.askApprover(ctx, req)
	}
	// encoding/json 按键排序，相同参数得到相同的合并键。
	args, err := json.Marshal(req.Call.Args)
	if err != nil {
		return ApprovalResult{}, err
	}
	key := req.Call.SessionID + "\x00" + req.Call.Tool + "\x00" + string(args)
	for {
		pending := &pendingApproval{done: make(chan struct{})}
		v, loaded := p.inflight.LoadOrStore(key, pending)
		if !loaded {
			return p.leadApproval(ctx, key, pending, req)
		}
		other := v.(*pendingApproval)
		select {
		case <-other.done:
			// 发起询问的调用自身 ctx 结束时，结果不是审批人的答复，仍在等待的调用重新发起询问。
			if isContextError(other.err) && ctx.Err() == nil {
				continue
			}
			return other.result, other.err
		case <-ctx.Done():
			return ApprovalResult{}, ctx.Err()
		}
	}
}

func (p *Pipeline) leadApproval(ctx context.Context, key string, pending *pendingApproval, req ApprovalRequest) (ApprovalResult, error) {
	defer func() {
		p.inflight.Delete(key)
		close(pending.done)
	}()
	pending.result, pending.err = p.askApprover(ctx, req)
	return pending.result, pending.err
}

// askApprover 在 ApprovalTimeout 内等待答复。审批人不响应 ctx 时也按时返回，迟到的答复被丢弃。
func (p *Pipeline) askApprover(ctx context.Context, req ApprovalRequest) (ApprovalResult, error) {
	timeout := p.ApprovalTimeout
	if timeout <= 0 {
		timeout = defaultApprovalTimeout
	}
	actx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type answer struct {
		result ApprovalResult
		err    error
	}
	answered := make(chan answer, 1)
	go func() {
		res, err := callApprover(actx, p.Approver, req)
		answered <- answer{res, err}
	}()
	select {
	case a := <-answered:
		if actx.Err() == nil {
			return a.result, a.err
		}
	case <-actx.Done():
	}
	if err := ctx.Err(); err != nil {
		return ApprovalResult{}, err
	}
	return ApprovalResult{Outcome: ApprovalRejected, Feedback: "not approved in time"}, nil
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func callApprover(ctx context.Context, a Approver, req ApprovalRequest) (res ApprovalResult, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = agentErrors.NewPanicError(fmt.Sprintf("工具 %q 的审批回调", req.Call.Tool), v)
		}
	}()
	return a.Approve(ctx, req)
}

// hookAsk 是第 hook 个 PreHook 给出的 Ask。
type hookAsk struct {
	hook     int
	decision PreDecision
}

// gate 执行全部 PreHook 并聚合裁决，再执行 Guard。返回非 nil 的 out 表示调用不执行（拒绝），返回 sub 表示改为执行替代调用。
func (p *Pipeline) gate(ctx context.Context, call *Call, allowSubstitute bool) (out *interfaces.ToolOutput, sub *Substitution, err error) {
	var (
		stop   *StopError
		denied []string
		subs   []*Substitution
		asks   []hookAsk
	)
	for i, hook := range p.Pre {
		d, err := callPre(ctx, hook, call)
		if err != nil {
			return nil, nil, err
		}
		switch d.Kind {
		case PreAllow:
		case PreDeny:
			denied = append(denied, nonEmpty(d.Feedback, call.Tool+" is denied by policy"))
		case PreStop:
			if stop == nil {
				stop = &StopError{Tool: call.Tool, Reason: d.Feedback}
			}
		case PreSubstitute:
			subs = append(subs, d.Substitute)
		case PreAsk:
			asks = append(asks, hookAsk{hook: i, decision: d})
		default:
			return nil, nil, fmt.Errorf("tool %q: unknown pre decision %d", call.Tool, d.Kind)
		}
	}
	switch {
	case stop != nil:
		return nil, nil, stop
	case len(denied) > 0:
		return Rejected(strings.Join(denied, "; "), nil), nil, nil
	case len(subs) > 0:
		if !allowSubstitute || len(subs) > 1 || subs[0] == nil {
			return nil, nil, fmt.Errorf("tool %q: invalid substitution", call.Tool)
		}
		return nil, subs[0], nil
	case len(asks) > 0:
		if out, err = p.ask(ctx, call, asks); out != nil || err != nil {
			return out, nil, err
		}
	}
	for _, guard := range p.Guards {
		reason, gerr := callGuard(ctx, guard, call)
		if gerr != nil {
			return nil, nil, gerr
		}
		if reason != "" {
			return Rejected(reason, nil), nil, nil
		}
	}
	return nil, nil, nil
}

// ask 把全部 Ask 合并为一次审批：每个钩子都已有匹配的会话授权时直接放行，否则询问审批人。
// 只有每个钩子都给出 Grant 时才提供"本会话允许"。返回 nil, nil 表示放行。
func (p *Pipeline) ask(ctx context.Context, call *Call, asks []hookAsk) (*interfaces.ToolOutput, error) {
	if p.granted(call, asks) {
		return nil, nil
	}
	var reasons []string
	grants := make(combinedGrant, 0, len(asks))
	for _, a := range asks {
		if a.decision.Feedback != "" {
			reasons = append(reasons, a.decision.Feedback)
		}
		if a.decision.Grant != nil {
			grants = append(grants, a.decision.Grant)
		}
	}
	var grant SessionGrant
	switch {
	case len(grants) < len(asks):
	case len(grants) == 1:
		grant = grants[0]
	default:
		grant = grants
	}
	res, err := p.approve(ctx, ApprovalRequest{Call: call, Reason: strings.Join(reasons, "; "), Grant: grant})
	if err != nil {
		return nil, err
	}
	switch res.Outcome {
	case ApprovalAllowedSession:
		if grant != nil && call.SessionID != "" {
			p.addGrants(call.SessionID, asks)
		}
		return nil, nil
	case ApprovalAllowedOnce:
		return nil, nil
	case ApprovalUnavailable:
		return Rejected(call.Tool+" requires approval but no approver is available", nil), nil
	}
	msg := "the user denied " + call.Tool
	if res.Feedback != "" {
		msg += ": " + res.Feedback
	}
	return Rejected(msg, nil), nil
}

// combinedGrant 是多个钩子的 Grant 的组合，全部匹配才算匹配。
type combinedGrant []SessionGrant

func (g combinedGrant) Describe() string {
	parts := make([]string, len(g))
	for i, x := range g {
		parts[i] = x.Describe()
	}
	return strings.Join(parts, "；")
}

func (g combinedGrant) Matches(call *Call) bool {
	for _, x := range g {
		if !x.Matches(call) {
			return false
		}
	}
	return true
}

// around 按注册顺序由外到内包住 body。
func (p *Pipeline) around(ctx context.Context, call *Call, body func(context.Context) (*interfaces.ToolOutput, error)) (*interfaces.ToolOutput, error) {
	next := body
	for i := len(p.Around) - 1; i >= 0; i-- {
		wrap, inner := p.Around[i], next
		next = func(c context.Context) (out *interfaces.ToolOutput, err error) {
			defer func() {
				if v := recover(); v != nil {
					out, err = nil, agentErrors.NewPanicError(fmt.Sprintf("工具 %q 的执行包装", call.Tool), v)
				}
			}()
			return wrap(c, call, inner)
		}
	}
	return next(ctx)
}

// post 依次执行 PostHook。
func (p *Pipeline) post(ctx context.Context, call *Call, out *interfaces.ToolOutput) (*interfaces.ToolOutput, error) {
	for _, hook := range p.Post {
		d, err := callPost(ctx, hook, call, out)
		if err != nil {
			return nil, err
		}
		if d.Replace != nil {
			out = d.Replace
		}
		if len(d.AdditionalContext) > 0 {
			copied := *out
			copied.AdditionalContext = append(append([]string(nil), out.AdditionalContext...), d.AdditionalContext...)
			out = &copied
		}
	}
	return out, nil
}

func callPre(ctx context.Context, hook PreHook, call *Call) (d PreDecision, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = agentErrors.NewPanicError(fmt.Sprintf("工具 %q 的调用前钩子", call.Tool), v)
		}
	}()
	return hook(ctx, call)
}

func callGuard(ctx context.Context, guard Guard, call *Call) (reason string, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = agentErrors.NewPanicError(fmt.Sprintf("工具 %q 的守卫", call.Tool), v)
		}
	}()
	return guard(ctx, call), nil
}

func callPost(ctx context.Context, hook PostHook, call *Call, out *interfaces.ToolOutput) (d PostDecision, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = agentErrors.NewPanicError(fmt.Sprintf("工具 %q 的调用后钩子", call.Tool), v)
		}
	}()
	return hook(ctx, call, out)
}

func nonEmpty(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

// sessionKey 承载 Pipeline 会话级授权使用的会话 ID。
type sessionKey struct{}

// WithSessionID 声明 ctx 中调用所属的会话，会话级批准只在同一会话内生效。Builder 以 SessionID 设置它。
func WithSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionKey{}, id)
}

// SessionIDFrom 返回 WithSessionID 设置的会话 ID。
func SessionIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(sessionKey{}).(string)
	return id
}

var errSubstituteUnknown = errors.New("substitute tool is not registered")
