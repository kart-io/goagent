// Package execution combines the session, journal, budget and observation
// configuration supplied by the host into one execution scope.
package execution

import (
	"context"
	"reflect"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/observe"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/tools"
)

// Config is supplied by the host. Scope does not create default stores or
// quotas, and does not close these resources.
type Config struct {
	RunID     string
	SessionID string
	Journal   durable.Store
	Budget    *budget.Controller
	Timeout   time.Duration
	Observer  observe.Observer
	Tracer    trace.Tracer
}

// Scope holds the immutable wiring of one logical execution. Each call binds
// it again from the host context.
type Scope struct {
	config Config
	valid  bool
}
type scopeKey struct{}

func typedNil(v any) bool {
	if v == nil {
		return false
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflect.ValueOf(v).IsNil()
	}
	return false
}
func validID(id string) bool { return id != "" && strings.TrimSpace(id) == id }

// NewScope validates identities, budget and optional components before
// execution. It does not access the store.
func NewScope(config Config) (*Scope, error) {
	if !validID(config.RunID) {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "execution: RunID 必须非空且无首尾空白").WithComponent("execution").WithOperation("new_scope")
	}
	if config.Timeout < 0 {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "execution: Timeout 不能为负数").WithComponent("execution").WithOperation("new_scope")
	}
	if typedNil(config.Journal) || typedNil(config.Observer) || typedNil(config.Tracer) {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "execution: Journal、Observer 或 Tracer 不能为带类型的 nil").WithComponent("execution").WithOperation("new_scope")
	}
	if config.SessionID == "" {
		config.SessionID = config.RunID
	}
	if config.Budget != nil {
		if config.Budget.Configuration().RootKey != config.RunID {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "execution: 预算 RootKey 必须等于 RunID").WithComponent("execution").WithOperation("new_scope")
		}
		if config.Journal != nil && !config.Budget.IsDurable() {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "execution: 绑定日志时必须使用持久预算").WithComponent("execution").WithOperation("new_scope")
		}
	}
	return &Scope{config: config, valid: true}, nil
}

// RunID returns the root run identity the host passes to a graph RunRequest.
func (s *Scope) RunID() string {
	if s == nil {
		return ""
	}
	return s.config.RunID
}

// SessionID returns the host session identity.
func (s *Scope) SessionID() string {
	if s == nil {
		return ""
	}
	return s.config.SessionID
}

// Bind binds the root execution key; graphs and agents still derive their own
// durable keys. The returned cancel only cancels the derived context and does
// not close the host's journal or observation resources.
func (s *Scope) Bind(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if s == nil || !s.valid {
		return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "execution: Scope 尚未初始化").WithComponent("execution").WithOperation("bind")
	}
	if ctx == nil {
		return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "execution: context 不能为空").WithComponent("execution").WithOperation("bind")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if ctx.Value(scopeKey{}) != nil {
		return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "execution: context 已绑定 Scope").WithComponent("execution").WithOperation("bind")
	}
	if durable.JournalStore(ctx) != nil {
		return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "execution: context 已绑定日志").WithComponent("execution").WithOperation("bind")
	}
	if key := durable.ExecutionKey(ctx); key != "" && key != s.config.RunID {
		return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "execution: 执行键冲突").WithComponent("execution").WithOperation("bind")
	}
	if id := tools.SessionIDFrom(ctx); id != "" && id != s.config.SessionID {
		return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "execution: 会话身份冲突").WithComponent("execution").WithOperation("bind")
	}
	if id := observe.RunIDFromContext(ctx); id != "" && id != s.config.RunID {
		return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "execution: 观测 RunID 冲突").WithComponent("execution").WithOperation("bind")
	}
	if c, ok := budget.FromContext(ctx); ok && c != s.config.Budget {
		return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "execution: 预算控制器冲突").WithComponent("execution").WithOperation("bind")
	}
	ctx = context.WithValue(ctx, scopeKey{}, s)
	ctx = tools.WithSessionID(ctx, s.config.SessionID)
	ctx = durable.WithExecutionKey(ctx, s.config.RunID)
	if s.config.Journal != nil {
		ctx = durable.WithJournal(ctx, s.config.Journal)
	}
	ctx = observe.WithRunID(ctx, s.config.RunID)
	if s.config.Observer != nil {
		ctx = observe.WithObserver(ctx, s.config.Observer)
	}
	if s.config.Tracer != nil {
		ctx = observe.WithTracer(ctx, s.config.Tracer)
	}
	var timeoutCancel context.CancelFunc
	if s.config.Timeout > 0 {
		ctx, timeoutCancel = context.WithTimeout(ctx, s.config.Timeout)
	}
	ctx, budgetCancel := budget.Bind(ctx, s.config.Budget)
	cancel := func() {
		budgetCancel()
		if timeoutCancel != nil {
			timeoutCancel()
		}
	}
	if err := ctx.Err(); err != nil {
		cancel()
		return nil, nil, err
	}
	return ctx, cancel, nil
}

// ConfigureBuilder sets stable session and agent identities; it neither builds
// nor closes the agent. Callers should keep both settings afterwards and use
// the same agentID when rebuilding.
func ConfigureBuilder[S core.State](b *builder.AgentBuilder[S], s *Scope, agentID string) (*builder.AgentBuilder[S], error) {
	if b == nil {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "execution: builder 不能为空").WithComponent("execution").WithOperation("configure_builder")
	}
	if s == nil || !s.valid {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "execution: Scope 尚未初始化").WithComponent("execution").WithOperation("configure_builder")
	}
	if !validID(agentID) {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "execution: agentID 必须非空且无首尾空白").WithComponent("execution").WithOperation("configure_builder")
	}
	return b.WithSessionID(s.config.SessionID).WithDurableScope(agentID).WithAgentID(agentID), nil
}
