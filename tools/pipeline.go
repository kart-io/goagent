package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	// PreAllow 交给后续 PreHook，全部放行后进入 Guard。
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

// PreHook 依次执行，第一个给出非 PreAllow 裁决者生效。返回 error 中止运行。
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
// 准入阶段占用并发槽位，但不计入 DispatchConfig.Timeout；Around、工具与 PostHook 在超时之内。
type Pipeline struct {
	Pre      []PreHook
	Approver Approver
	// ApprovalTimeout 是单次等待审批的上限，默认 10 分钟，超时后迟到的批准也按拒绝处理。
	ApprovalTimeout time.Duration
	Guards          []Guard
	Around          []Around
	Post            []PostHook

	grants   sync.Map // SessionID -> *grantSet
	inflight sync.Map // 审批合并键 -> *pendingApproval
}

// StopError 表示 PreStop 终止了运行。
type StopError struct {
	Tool   string
	Reason string
}

func (e *StopError) Error() string {
	return fmt.Sprintf("tool %q stopped the run: %s", e.Tool, e.Reason)
}

type grantSet struct {
	mu     sync.Mutex
	grants []SessionGrant
}

func (p *Pipeline) granted(call *Call) bool {
	if call.SessionID == "" {
		return false
	}
	v, ok := p.grants.Load(call.SessionID)
	if !ok {
		return false
	}
	set := v.(*grantSet)
	set.mu.Lock()
	defer set.mu.Unlock()
	for _, g := range set.grants {
		if g.Matches(call) {
			return true
		}
	}
	return false
}

func (p *Pipeline) addGrant(sessionID string, g SessionGrant) {
	v, _ := p.grants.LoadOrStore(sessionID, &grantSet{})
	set := v.(*grantSet)
	set.mu.Lock()
	set.grants = append(set.grants, g)
	set.mu.Unlock()
}

// Grants 返回会话中已批准的会话级授权说明，供宿主展示或撤销前核对。
func (p *Pipeline) Grants(sessionID string) []string {
	v, ok := p.grants.Load(sessionID)
	if !ok {
		return nil
	}
	set := v.(*grantSet)
	set.mu.Lock()
	defer set.mu.Unlock()
	out := make([]string, len(set.grants))
	for i, g := range set.grants {
		out[i] = g.Describe()
	}
	return out
}

// RevokeGrants 清除会话的全部会话级授权。
func (p *Pipeline) RevokeGrants(sessionID string) { p.grants.Delete(sessionID) }

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

// gate 执行 PreHook、审批与 Guard。返回非 nil 的 out 表示调用不执行（拒绝），返回 sub 表示改为执行替代调用。
func (p *Pipeline) gate(ctx context.Context, call *Call, allowSubstitute bool) (out *interfaces.ToolOutput, sub *Substitution, err error) {
	for _, hook := range p.Pre {
		var d PreDecision
		d, err = callPre(ctx, hook, call)
		if err != nil {
			return nil, nil, err
		}
		switch d.Kind {
		case PreAllow:
			continue
		case PreDeny:
			return Rejected(nonEmpty(d.Feedback, call.Tool+" is denied by policy"), nil), nil, nil
		case PreStop:
			return nil, nil, &StopError{Tool: call.Tool, Reason: d.Feedback}
		case PreSubstitute:
			if !allowSubstitute || d.Substitute == nil {
				return nil, nil, fmt.Errorf("tool %q: invalid substitution", call.Tool)
			}
			return nil, d.Substitute, nil
		case PreAsk:
			if out, err = p.ask(ctx, call, d); out != nil || err != nil {
				return out, nil, err
			}
		default:
			return nil, nil, fmt.Errorf("tool %q: unknown pre decision %d", call.Tool, d.Kind)
		}
		break
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

// ask 处理 PreAsk：会话已批准的范围直接放行，否则询问审批人。返回 nil, nil 表示放行。
func (p *Pipeline) ask(ctx context.Context, call *Call, d PreDecision) (*interfaces.ToolOutput, error) {
	if p.granted(call) {
		return nil, nil
	}
	res, err := p.approve(ctx, ApprovalRequest{Call: call, Reason: d.Feedback, Grant: d.Grant})
	if err != nil {
		return nil, err
	}
	switch res.Outcome {
	case ApprovalAllowedSession:
		if d.Grant != nil && call.SessionID != "" {
			p.addGrant(call.SessionID, d.Grant)
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
