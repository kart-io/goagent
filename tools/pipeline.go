package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// Call is one tool call handed to the pipeline stages. Args is read-only: the
// arguments have already been shown to the model and humans, so the pipeline
// does not rewrite them; to take a different approach, declare a replacement
// call with PreSubstitute.
type Call struct {
	Tool         string
	CallID       string
	ExecutionKey string
	CallerID     string
	SessionID    string
	Args         map[string]any
}

// PreKind is the verdict of the pre-call stage. The zero value is PreAllow.
type PreKind int

const (
	// PreAllow raises no objection; when every PreHook allows, the call
	// proceeds to Guard.
	PreAllow PreKind = iota
	// PreDeny does not execute the tool and returns Rejected(Feedback) to the
	// model for revision.
	PreDeny
	// PreAsk hands the call to the Approver for human confirmation.
	PreAsk
	// PreStop terminates the whole run and returns *StopError.
	PreStop
	// PreSubstitute executes the other tool declared by Substitute in place of
	// this call; the result metadata records the substitution.
	PreSubstitute
)

// PreDecision is the verdict of a PreHook.
type PreDecision struct {
	Kind PreKind
	// Feedback is passed to the model on Deny, shown to the approver on Ask,
	// and used as the termination reason on Stop.
	Feedback string
	// Grant is only used with Ask: when the approver chooses "allow for this
	// session", it allows subsequent matching calls in the same session. When
	// nil, or when the call has no session ID, no session-level approval is
	// offered and AllowedSession is treated as AllowedOnce.
	Grant SessionGrant
	// Substitute is only used with PreSubstitute.
	Substitute *Substitution
}

// Substitution declares a replacement call. The replacement is validated
// against the target tool's schema and goes through the full pipeline again,
// but cannot itself be substituted.
type Substitution struct {
	Tool string
	Args map[string]any
}

// SessionGrant is the scope of calls allowed by a session-level approval. It
// should be narrowed to the argument level where possible (such as a command
// prefix or path prefix) rather than the whole tool. Describe presents it to
// the approver in human-readable form.
type SessionGrant interface {
	Describe() string
	Matches(call *Call) bool
}

// PreHook hooks all run in order, and verdicts are aggregated as Stop > Deny >
// Substitute > Ask > Allow regardless of hook order: feedback from multiple
// Denys is merged and passed to the model; multiple Asks are merged into one
// approval; multiple Substitutes are an error. Returning an error aborts the
// run.
type PreHook func(ctx context.Context, call *Call) (PreDecision, error)

// Guard can only deny, never allow, and is meant for non-bypassable hard
// constraints; returning a non-empty string denies the call, which is passed
// to the model as Rejected. It runs after PreHook and approval, so even human
// approval cannot override it.
type Guard func(ctx context.Context, call *Call) (denyReason string)

// Around wraps tool execution (within the per-call timeout) and composes like
// an onion; it must call next or return a result itself.
type Around func(ctx context.Context, call *Call, next func(context.Context) (*interfaces.ToolOutput, error)) (*interfaces.ToolOutput, error)

// PostDecision is the verdict of a PostHook. A nil Replace accepts the result.
type PostDecision struct {
	Replace *interfaces.ToolOutput
	// AdditionalContext is appended to the result's AdditionalContext; the
	// Builder injects it as a user message after this batch of tool results.
	AdditionalContext []string
}

// PostHook hooks process successful results and Rejected results (including
// denied calls) in order, and may replace the result or append context.
// Returning an error aborts the run.
type PostHook func(ctx context.Context, call *Call, out *interfaces.ToolOutput) (PostDecision, error)

// ApprovalOutcome is the outcome of an approval. The zero value is
// ApprovalUnavailable: when no one can answer, the call is treated as denied.
type ApprovalOutcome int

const (
	ApprovalUnavailable ApprovalOutcome = iota
	ApprovalAllowedOnce
	ApprovalAllowedSession
	ApprovalRejected
	ApprovalCancelled
)

// ApprovalRequest is a call awaiting confirmation, passed to the Approver.
type ApprovalRequest struct {
	Call   *Call
	Reason string
	// Grant, when non-nil, lets the approval UI offer "allow for this session"
	// and show Grant.Describe().
	Grant SessionGrant
}

// ApprovalResult is the approver's answer; Feedback is passed to the model on
// denial.
type ApprovalResult struct {
	Outcome  ApprovalOutcome
	Feedback string
}

// Approver is implemented by the host (command-line prompt, IM card, etc.).
// Returning an error aborts the run.
type Approver interface {
	Approve(ctx context.Context, req ApprovalRequest) (ApprovalResult, error)
}

// ApproverFunc adapts a function to an Approver.
type ApproverFunc func(ctx context.Context, req ApprovalRequest) (ApprovalResult, error)

// Approve implements Approver.
func (f ApproverFunc) Approve(ctx context.Context, req ApprovalRequest) (ApprovalResult, error) {
	return f(ctx, req)
}

// Pipeline is the pipeline the Dispatcher runs for each model-visible call:
// PreHook → approval → Guard → Around (including timeout) → tool → PostHook.
// Calls with invalid arguments do not enter the pipeline. The admission stages
// (PreHook, approval, Guard) run outside the operation journal: termination,
// cancellation and errors leave no record and consume no attempts; rejection
// results are journaled like execution results, and replayed completed calls
// do not pass through admission again. Admission uses separately configurable
// admission slots, occupies no tool execution slot, and does not count toward
// DispatchConfig.Timeout; Around, the tool and PostHook run within the
// timeout. Hooks and the Approver must not modify Call.Args: the Dispatcher
// fails the call if they do. Use PreSubstitute to change arguments.
type Pipeline struct {
	Pre      []PreHook
	Approver Approver
	// ApprovalTimeout bounds a single wait for approval; after it expires, a
	// late approval is still treated as denied. 0 means no timeout, so the wait
	// ends only with the call's context (it previously meant 10 minutes);
	// NewDispatcher rejects negative values.
	ApprovalTimeout time.Duration
	// GrantTTL bounds the validity of new session grants from the time of
	// approval; zero or less means they never expire.
	GrantTTL time.Duration
	Guards   []Guard
	Around   []Around
	Post     []PostHook

	grantsMu sync.Mutex // 串行协调授权映射的清理、新增和撤销
	grants   sync.Map   // SessionID -> *grantSet
	inflight sync.Map   // 审批合并键 -> *pendingApproval
}

// StopError reports that PreStop terminated the run.
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

// Grants returns descriptions of the session-level grants approved in the
// session, for the host to display or check before revoking.
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

// RevokeGrants clears all session-level grants of the session.
func (p *Pipeline) RevokeGrants(sessionID string) {
	p.grantsMu.Lock()
	defer p.grantsMu.Unlock()
	p.grants.Delete(sessionID)
}

func (g hookGrant) expired(now time.Time) bool {
	return !g.expiresAt.IsZero() && !now.Before(g.expiresAt)
}

// PruneGrants removes expired grants and empty session entries and returns the
// number of grant entries removed. It does not modify or cancel in-flight
// approvals; expired grants cannot allow calls even before they are pruned.
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

// askApprover 在 ApprovalTimeout 内等待答复（0 = 只受 ctx 约束）。审批人不响应 ctx 时也按时返回，迟到的答复被丢弃。
func (p *Pipeline) askApprover(ctx context.Context, req ApprovalRequest) (ApprovalResult, error) {
	actx, cancel := context.WithCancel(ctx)
	if p.ApprovalTimeout > 0 {
		actx, cancel = context.WithTimeout(ctx, p.ApprovalTimeout)
	}
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
			return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidOutput, "tool %q: unknown pre decision %d", call.Tool, d.Kind).
				WithComponent("tool_pipeline").
				WithOperation("pre").
				WithContext("tool_name", call.Tool)
		}
	}
	switch {
	case stop != nil:
		return nil, nil, stop
	case len(denied) > 0:
		return Rejected(strings.Join(denied, "; "), nil), nil, nil
	case len(subs) > 0:
		if !allowSubstitute || len(subs) > 1 || subs[0] == nil {
			return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidOutput, "tool %q: invalid substitution", call.Tool).
				WithComponent("tool_pipeline").
				WithOperation("pre").
				WithContext("tool_name", call.Tool)
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

// WithSessionID declares the session that calls in ctx belong to; session-level
// approvals only apply within the same session. The Builder sets it via
// SessionID.
func WithSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionKey{}, id)
}

// SessionIDFrom returns the session ID set by WithSessionID.
func SessionIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(sessionKey{}).(string)
	return id
}
