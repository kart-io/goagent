package execution

import (
	"context"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

// GovernanceReport 描述创建时的治理装配快照；后续Builder配置不回写到此报告。
type GovernanceReport struct {
	RunID, SessionID, AgentID      string
	Journaled, DurableBudget       bool
	MaxModelRequests, MaxToolCalls int
	RootTimeout                    time.Duration
	RootDeadlineUnixMilli          int64
	Dispatch                       tools.DispatchGovernance
}

// NewGovernedBuilder 以显式预算、期限、授权和有限准入装配标准Builder，宿主须使用同一Scope.Bind。
func NewGovernedBuilder(client llm.Client, s *Scope, agentID string, registry *tools.Registry, cfg tools.DispatchConfig) (*builder.SimpleAgentBuilder, GovernanceReport, error) {
	if s == nil || !s.valid || client == nil || typedNil(client) || !validID(agentID) {
		return nil, GovernanceReport{}, agentErrors.Newf(agentErrors.CodeInvalidConfig, "治理装配需要有效Scope、客户端和Agent身份").WithComponent("execution").WithOperation("new_governed_builder")
	}
	if s.config.Budget == nil {
		return nil, GovernanceReport{}, agentErrors.Newf(agentErrors.CodeInvalidConfig, "治理装配需要显式根预算").WithComponent("execution").WithOperation("new_governed_builder")
	}
	limits := s.config.Budget.Configuration()
	if limits.MaxToolCalls < 1 || (s.config.Timeout <= 0 && limits.DeadlineUnixMilli <= 0) {
		return nil, GovernanceReport{}, agentErrors.Newf(agentErrors.CodeInvalidConfig, "治理装配需要工具额度和根期限").WithComponent("execution").WithOperation("new_governed_builder")
	}
	if cfg.Authorize == nil || cfg.MaxAdmissionConcurrency < 1 || cfg.MaxQueued < 1 {
		return nil, GovernanceReport{}, agentErrors.Newf(agentErrors.CodeInvalidConfig, "治理装配需要授权回调和有限准入、排队额度").WithComponent("execution").WithOperation("new_governed_builder")
	}
	if _, ok := client.(llm.ToolStreamClient); !ok {
		return nil, GovernanceReport{}, agentErrors.Newf(agentErrors.CodeInvalidConfig, "治理工具循环需要ToolStreamClient").WithComponent("execution").WithOperation("new_governed_builder")
	}
	check := func(ctx context.Context) error {
		c, ok := budget.FromContext(ctx)
		if ctx.Value(scopeKey{}) != s || !ok || c != s.config.Budget || tools.SessionIDFrom(ctx) != s.SessionID() {
			return agentErrors.Newf(agentErrors.CodeInvalidConfig, "治理执行必须使用创建时的Scope.Bind上下文").WithComponent("execution").WithOperation("new_governed_builder")
		}
		return ctx.Err()
	}
	authorize := cfg.Authorize
	cfg.Authorize = func(ctx context.Context, name string, in *interfaces.ToolInput) error {
		if err := check(ctx); err != nil {
			return err
		}
		return authorize(ctx, name, in)
	}
	d, err := tools.NewDispatcher(registry, cfg)
	if err != nil {
		return nil, GovernanceReport{}, err
	}
	report := GovernanceReport{RunID: s.RunID(), SessionID: s.SessionID(), AgentID: agentID, Journaled: s.config.Journal != nil, DurableBudget: s.config.Budget.IsDurable(), MaxModelRequests: limits.MaxModelRequests, MaxToolCalls: limits.MaxToolCalls, RootTimeout: s.config.Timeout, RootDeadlineUnixMilli: limits.DeadlineUnixMilli, Dispatch: d.Governance()}
	if len(report.Dispatch.ToolNames) == 0 {
		return nil, GovernanceReport{}, agentErrors.Newf(agentErrors.CodeInvalidConfig, "治理工具循环至少需要一个已注册工具").WithComponent("execution").WithOperation("new_governed_builder")
	}
	b, err := ConfigureBuilder(builder.NewSimpleBuilder(client).WithDispatcher(d).WithMemoryFailurePolicy(builder.MemoryFailureStrict), s, agentID)
	if err != nil {
		return nil, GovernanceReport{}, err
	}
	b.WithRequestHook(func(ctx context.Context, _ *builder.ModelRequest) error { return check(ctx) })
	return b, report, nil
}
