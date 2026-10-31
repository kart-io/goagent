// Package execution 组合宿主提供的会话、日志、预算和观测配置。
package execution

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/tools"
	"go.opentelemetry.io/otel/trace"
)

// Config 由宿主提供；Scope 不创建默认存储或额度，也不关闭这些资源。
type Config struct {
	RunID     string
	SessionID string
	Journal   durable.Store
	Budget    *budget.Controller
	Timeout   time.Duration
	Observer  observe.Observer
	Tracer    trace.Tracer
}

// Scope 保存一次逻辑执行的不可变装配配置；每次调用从宿主 context 重新 Bind。
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

// NewScope 在执行前验证身份、预算和可选组件，不访问存储。
func NewScope(config Config) (*Scope, error) {
	if !validID(config.RunID) {
		return nil, fmt.Errorf("execution: RunID 必须非空且无首尾空白")
	}
	if config.Timeout < 0 {
		return nil, fmt.Errorf("execution: Timeout 不能为负数")
	}
	if typedNil(config.Journal) || typedNil(config.Observer) || typedNil(config.Tracer) {
		return nil, fmt.Errorf("execution: Journal、Observer 或 Tracer 不能为带类型的 nil")
	}
	if config.SessionID == "" {
		config.SessionID = config.RunID
	}
	if config.Budget != nil {
		if config.Budget.Configuration().RootKey != config.RunID {
			return nil, fmt.Errorf("execution: 预算 RootKey 必须等于 RunID")
		}
		if config.Journal != nil && !config.Budget.IsDurable() {
			return nil, fmt.Errorf("execution: 绑定日志时必须使用持久预算")
		}
	}
	return &Scope{config: config, valid: true}, nil
}

// RunID 返回宿主传给图 RunRequest 的根运行身份。
func (s *Scope) RunID() string {
	if s == nil {
		return ""
	}
	return s.config.RunID
}

// SessionID 返回宿主会话身份。
func (s *Scope) SessionID() string {
	if s == nil {
		return ""
	}
	return s.config.SessionID
}

// Bind 绑定根执行键；图和 Agent 仍负责派生自己的持久键。
// cancel 仅取消派生 context，不关闭宿主的日志或观测资源。
func (s *Scope) Bind(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if s == nil || !s.valid {
		return nil, nil, fmt.Errorf("execution: Scope 尚未初始化")
	}
	if ctx == nil {
		return nil, nil, fmt.Errorf("execution: context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if ctx.Value(scopeKey{}) != nil {
		return nil, nil, fmt.Errorf("execution: context 已绑定 Scope")
	}
	if durable.JournalStore(ctx) != nil {
		return nil, nil, fmt.Errorf("execution: context 已绑定日志")
	}
	if key := durable.ExecutionKey(ctx); key != "" && key != s.config.RunID {
		return nil, nil, fmt.Errorf("execution: 执行键冲突")
	}
	if id := tools.SessionIDFrom(ctx); id != "" && id != s.config.SessionID {
		return nil, nil, fmt.Errorf("execution: 会话身份冲突")
	}
	if id := observe.RunIDFromContext(ctx); id != "" && id != s.config.RunID {
		return nil, nil, fmt.Errorf("execution: 观测 RunID 冲突")
	}
	if c, ok := budget.FromContext(ctx); ok && c != s.config.Budget {
		return nil, nil, fmt.Errorf("execution: 预算控制器冲突")
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

// ConfigureBuilder 设置稳定的会话和 Agent 身份，不构建或关闭 Agent。
// 调用方应在此之后保留这两个设置，重建时使用相同的 agentID。
func ConfigureBuilder[C any, S core.State](b *builder.AgentBuilder[C, S], s *Scope, agentID string) (*builder.AgentBuilder[C, S], error) {
	if b == nil {
		return nil, fmt.Errorf("execution: builder 不能为空")
	}
	if s == nil || !s.valid {
		return nil, fmt.Errorf("execution: Scope 尚未初始化")
	}
	if !validID(agentID) {
		return nil, fmt.Errorf("execution: agentID 必须非空且无首尾空白")
	}
	return b.WithSessionID(s.config.SessionID).WithDurableScope(agentID), nil
}
