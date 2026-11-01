// Package budget bounds model request admission across a host-owned execution.
// Reservations are conservative: a crash never refunds an uncertain request.
package budget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"sync"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
)

// Unlimited 是未绑定预算（nil *Controller）时 Remaining 返回的余量，可直接参与大小比较。
const Unlimited = math.MaxInt

// DefaultMaxAgentCalls bounds topology when the host does not configure a limit.
const DefaultMaxAgentCalls = 1000
const ledgerFormat = "goagent-model-budget"

var (
	ErrBudgetExceeded        = errors.New("root model request budget exceeded")
	ErrReservationExists     = errors.New("model request reservation already exists")
	ErrTokenEstimateRequired = errors.New("token limit requires a host estimate")
)

// Config is supplied by the host. RootKey identifies one logical run.
type Config struct {
	RootKey, Version   string
	MaxModelRequests   int
	MaxReservedTokens  int    `json:",omitempty"`
	MaxToolCalls       int    `json:",omitempty"`
	DeadlineUnixMilli  int64  `json:",omitempty"`
	TokenPolicyVersion string `json:",omitempty"`
	MaxAgentCalls      int    `json:",omitempty"`
}

// Snapshot distinguishes conservative reservations from known token evidence.
type Snapshot struct {
	ReservedRequests, KnownUsageEntries, UnknownUsageEntries int
	KnownTokens                                              interfaces.TokenUsage
	ReservedTokens, ReservedToolCalls, KnownTokenDelta       int
}
type entry struct {
	Estimate     int `json:",omitempty"`
	Evidence     usage.Evidence
	AgentCallKey string `json:",omitempty"`
}
type ledger struct {
	Config     Config
	Entries    map[string]entry
	AgentCalls map[string]usage.Owner `json:",omitempty"`
	Tools      map[string]bool        `json:",omitempty"`
}

// Controller is safe for concurrent use. Durable controllers coordinate via Store locks.
type Controller struct {
	config Config
	store  durable.Store
	mu     sync.Mutex
	memory ledger
}

// New creates an in-memory request budget.
func New(config Config) (*Controller, error) {
	if config.RootKey == "" || config.Version == "" || config.MaxModelRequests < 1 || config.MaxReservedTokens < 0 || config.MaxToolCalls < 0 || config.DeadlineUnixMilli < 0 || (config.MaxReservedTokens > 0 && config.TokenPolicyVersion == "") {
		return nil, fmt.Errorf("budget requires root key, version and positive request limit")
	}
	if config.MaxAgentCalls < 0 {
		return nil, fmt.Errorf("negative agent call limit")
	}
	if config.MaxAgentCalls == 0 {
		config.MaxAgentCalls = DefaultMaxAgentCalls
	}
	return &Controller{config: config, memory: ledger{Config: config, Entries: map[string]entry{}}}, nil
}

// NewDurable binds an immutable budget policy to a stable root record.
func NewDurable(config Config, store durable.Store) (*Controller, error) {
	if store == nil {
		return nil, fmt.Errorf("budget store required")
	}
	c, err := New(config)
	if err != nil {
		return nil, err
	}
	c.store = store
	err = c.transaction(context.Background(), true, func(*ledger) error { return nil })
	return c, err
}

// IsDurable reports whether admission is persisted across processes.
func (c *Controller) IsDurable() bool { return c != nil && c.store != nil }

// Configuration returns a value copy for model intent identity.
func (c *Controller) Configuration() Config { return c.config }

type contextKey struct{}

// WithController shares one controller with child model and tool invocations.
func WithController(ctx context.Context, c *Controller) context.Context {
	return context.WithValue(ctx, contextKey{}, c)
}

// FromContext returns the host-provided budget, if any.
func FromContext(ctx context.Context) (*Controller, bool) {
	c, _ := ctx.Value(contextKey{}).(*Controller)
	return c, c != nil
}
func (c *Controller) storageID() string {
	b, _ := json.Marshal([]string{c.config.RootKey, "budget"})
	d := sha256.Sum256(b)
	return "goagent/budget/" + hex.EncodeToString(d[:])
}
func clone(l ledger) ledger {
	return ledger{Config: l.Config, Entries: maps.Clone(l.Entries), AgentCalls: maps.Clone(l.AgentCalls), Tools: maps.Clone(l.Tools)}
}
func (c *Controller) transaction(ctx context.Context, write bool, fn func(*ledger) error) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var l ledger
	if c.store == nil {
		l = clone(c.memory)
	}
	if c.store != nil {
		id := c.storageID()
		unlock, e := c.store.Lock(ctx, id)
		if e != nil {
			return e
		}
		defer func() { err = errors.Join(err, unlock()) }()
		s, e := c.store.Load(ctx, id)
		if errors.Is(e, os.ErrNotExist) {
			l = ledger{Config: c.config, Entries: map[string]entry{}}
		} else if e != nil {
			return e
		} else {
			// 从零值解码，避免JSON缺省字段继承当前配置或上次内存快照。
			l = ledger{}
			if s != nil && s.Version != ledgerFormat {
				return fmt.Errorf("unsupported budget record format %q; expected %q", s.Version, ledgerFormat)
			}
			if s == nil || s.RunID != id || json.Unmarshal(s.Data, &l) != nil {
				return fmt.Errorf("invalid budget record")
			}
		}
	}
	if l.Config != c.config || l.Entries == nil || len(l.Entries) > c.config.MaxModelRequests {
		return fmt.Errorf("budget configuration or record changed")
	}
	for key, item := range l.Entries {
		if key == "" || item.Estimate < 0 || (c.config.MaxReservedTokens > 0 && item.Estimate == 0) {
			return fmt.Errorf("invalid reservation record")
		}
		if err := item.Evidence.Validate(); err != nil {
			return err
		}
	}

	if reserved, e := reservedTokens(&l); e != nil || (c.config.MaxReservedTokens > 0 && reserved > c.config.MaxReservedTokens) {
		return fmt.Errorf("无效的 Token 预算记录")
	}
	if c.config.MaxToolCalls > 0 && len(l.Tools) > c.config.MaxToolCalls {
		return fmt.Errorf("无效的工具预算记录")
	}
	for key, used := range l.Tools {
		if key == "" || !used {
			return fmt.Errorf("无效的工具执行记录")
		}
	}
	if err = c.validateAttribution(&l); err != nil {
		return err
	}
	if err = fn(&l); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if write && c.store != nil {
		data, e := json.Marshal(l)
		if e != nil {
			return e
		}
		id := c.storageID()
		if e = c.store.Save(ctx, id, &durable.Snapshot{RunID: id, Version: ledgerFormat, Data: data}); e != nil {
			return e
		}
	}
	if write {
		c.memory = l
	}
	return nil
}

// Reserve persists admission before a network call. Reusing an uncertain key is rejected.
func (c *Controller) Reserve(ctx context.Context, key string) error {
	if err := c.checkDeadline(ctx); err != nil {
		return err
	}
	if c.config.MaxReservedTokens > 0 {
		return ErrTokenEstimateRequired
	}
	return c.ReserveEstimated(ctx, key, 0)
}

// Remaining 返回尚可预留的模型请求次数；持久预算在 Store 锁内读取，计入其他进程的预留。
// 结果只是时点快照，并发预留可能随即消耗余量，准入仍以 Reserve 为准。
// nil 接收者表示未绑定预算，返回 Unlimited，可与 FromContext 直接配合使用。
func (c *Controller) Remaining(ctx context.Context) (remaining int, err error) {
	if c == nil {
		return Unlimited, nil
	}
	err = c.transaction(ctx, false, func(l *ledger) error {
		remaining = c.config.MaxModelRequests - len(l.Entries)
		return nil
	})
	return
}

// RecordUsage records complete basic usage through the same evidence path.
// nil records unknown usage. Source remains unspecified unless the caller uses RecordEvidence.
func (c *Controller) RecordUsage(ctx context.Context, key string, u *interfaces.TokenUsage) error {
	e := usage.Evidence{}
	if u != nil {
		e.Tokens = *u
		e.KnownFields = usage.BasicFields
		if u.ReasoningTokens > 0 {
			e.KnownFields |= usage.Reasoning
		}
		if u.ToolUsePromptTokens > 0 {
			e.KnownFields |= usage.ToolUsePrompt
		}
		if u.CachedTokens > 0 {
			e.KnownFields |= usage.Cached
		}
	}
	_, err := c.RecordEvidence(ctx, key, usage.Lineage(ctx), e)
	return err
}

// Snapshot returns the same root aggregation as Attribution.
func (c *Controller) Snapshot(ctx context.Context) (out Snapshot, err error) {
	err = c.transaction(ctx, false, func(l *ledger) error { var e error; out, e = rootSnapshot(l); return e })
	return
}

// ReserveEstimated conservatively reserves one request and its host token estimate.
func (c *Controller) ReserveEstimated(ctx context.Context, key string, tokens int) error {
	return c.ReserveAttributed(ctx, key, tokens, usage.Lineage(ctx))
}

// ReserveTool 预留一次实际工具执行；日志回放不应调用它。重试属于新的实际执行，应使用新键。
func (c *Controller) ReserveTool(ctx context.Context, key string) error {
	if durable.HasJournal(ctx) && !c.IsDurable() {
		return fmt.Errorf("持久工具执行必须绑定持久根预算")
	}
	if key == "" {
		return fmt.Errorf("工具执行键必填")
	}
	return c.transaction(ctx, true, func(l *ledger) error {
		if err := c.checkDeadline(ctx); err != nil {
			return err
		}
		if l.Tools[key] {
			return ErrReservationExists
		}
		if c.config.MaxToolCalls > 0 && len(l.Tools) >= c.config.MaxToolCalls {
			return ErrBudgetExceeded
		}
		if l.Tools == nil {
			l.Tools = map[string]bool{}
		}
		l.Tools[key] = true
		return nil
	})
}

// Bind 绑定根预算和绝对截止时间；用于把期限传播到图节点及所有子任务。
func Bind(ctx context.Context, c *Controller) (context.Context, context.CancelFunc) {
	ctx = WithController(ctx, c)
	if c != nil && c.config.DeadlineUnixMilli > 0 {
		return context.WithDeadline(ctx, time.UnixMilli(c.config.DeadlineUnixMilli))
	}
	return context.WithCancel(ctx)
}

func (c *Controller) checkDeadline(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.config.DeadlineUnixMilli > 0 && !time.Now().Before(time.UnixMilli(c.config.DeadlineUnixMilli)) {
		return fmt.Errorf("根预算截止时间已到: %w", context.DeadlineExceeded)
	}
	return nil
}

func reservedTokens(l *ledger) (int, error) {
	total := 0
	for _, item := range l.Entries {
		if item.Estimate < 0 || item.Estimate > math.MaxInt-total {
			return 0, fmt.Errorf("invalid token reservation")
		}
		total += item.Estimate
	}
	return total, nil
}
