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
	"github.com/kart-io/goagent/interfaces"
)

// Unlimited 是未绑定预算（nil *Controller）时 Remaining 返回的余量，可直接参与大小比较。
const Unlimited = math.MaxInt

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
}

// Snapshot distinguishes conservative reservations from known token evidence.
type Snapshot struct {
	ReservedRequests, KnownUsageEntries, UnknownUsageEntries int
	KnownTokens                                              interfaces.TokenUsage
	ReservedTokens, ReservedToolCalls, KnownTokenDelta       int
}
type ledger struct {
	Config    Config
	Entries   map[string]*interfaces.TokenUsage
	Estimates map[string]int  `json:",omitempty"`
	Tools     map[string]bool `json:",omitempty"`
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
	return &Controller{config: config, memory: ledger{Config: config, Entries: map[string]*interfaces.TokenUsage{}}}, nil
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
	out := ledger{Config: l.Config, Estimates: maps.Clone(l.Estimates), Tools: maps.Clone(l.Tools)}
	if l.Entries != nil {
		out.Entries = make(map[string]*interfaces.TokenUsage, len(l.Entries))
		for key, usage := range l.Entries {
			if usage == nil {
				out.Entries[key] = nil
				continue
			}
			copied := *usage
			out.Entries[key] = &copied
		}
	}
	return out
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
			l = ledger{Config: c.config, Entries: map[string]*interfaces.TokenUsage{}}
		} else if e != nil {
			return e
		} else {
			// 从零值解码，避免JSON缺省字段继承当前配置或上次内存快照。
			l = ledger{}
			if s == nil || s.Version != "goagent-model-budget-v1" || s.RunID != id || json.Unmarshal(s.Data, &l) != nil {
				return fmt.Errorf("invalid budget record")
			}
		}
	}
	if l.Config != c.config || l.Entries == nil || len(l.Entries) > c.config.MaxModelRequests {
		return fmt.Errorf("budget configuration or record changed")
	}
	for key, u := range l.Entries {
		if c.config.MaxReservedTokens > 0 && l.Estimates[key] <= 0 {
			return fmt.Errorf("token 限额记录缺少正数预留")
		}
		if key == "" || !validUsage(u) {
			return fmt.Errorf("invalid budget usage record")
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
		if e = c.store.Save(ctx, id, &durable.Snapshot{RunID: id, Version: "goagent-model-budget-v1", Data: data}); e != nil {
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
func validUsage(u *interfaces.TokenUsage) bool {
	return u == nil || (u.PromptTokens >= 0 && u.CompletionTokens >= 0 && u.TotalTokens >= 0 && u.CachedTokens >= 0)
}

// RecordUsage attaches immutable known evidence, or records unknown usage as nil.
// A later saved model record may upgrade unknown usage to known without another call.
func (c *Controller) RecordUsage(ctx context.Context, key string, u *interfaces.TokenUsage) error {
	if !validUsage(u) {
		return fmt.Errorf("negative model usage")
	}
	return c.transaction(ctx, true, func(l *ledger) error {
		old, ok := l.Entries[key]
		if !ok {
			return fmt.Errorf("usage has no reservation")
		}
		if old != nil && u != nil && *old != *u {
			return fmt.Errorf("model usage evidence changed")
		}
		if old == nil && u != nil {
			copy := *u
			l.Entries[key] = &copy
		}
		return nil
	})
}

// Snapshot returns cumulative evidence; unknown entries are not estimated as zero usage.
func (c *Controller) Snapshot(ctx context.Context) (out Snapshot, err error) {
	err = c.transaction(ctx, false, func(l *ledger) error {
		out.ReservedRequests = len(l.Entries)
		out.ReservedToolCalls = len(l.Tools)
		var err error
		out.ReservedTokens, err = reservedTokens(l)
		if err != nil {
			return err
		}
		for key, u := range l.Entries {
			if u == nil {
				out.UnknownUsageEntries++
			} else {
				out.KnownUsageEntries++
				out.KnownTokens.Add(u)
				out.KnownTokenDelta += u.TotalTokens - l.Estimates[key]
			}
		}
		return nil
	})
	return
}

// ReserveEstimated 预留一次请求与宿主估计的总 Token；取消、失败和实际用量较少均不退款。
// 估计须覆盖输入、工具定义与输出预算；实际用量仍可能超过估计，因此不是提供商费用硬上限。
func (c *Controller) ReserveEstimated(ctx context.Context, key string, tokens int) error {
	if key == "" || tokens < 0 {
		return fmt.Errorf("请求键和非负 Token 估计必填")
	}
	if c.config.MaxReservedTokens > 0 && tokens == 0 {
		return ErrTokenEstimateRequired
	}
	return c.transaction(ctx, true, func(l *ledger) error {
		if err := c.checkDeadline(ctx); err != nil {
			return err
		}
		if _, ok := l.Entries[key]; ok {
			return ErrReservationExists
		}
		if len(l.Entries) >= c.config.MaxModelRequests {
			return ErrBudgetExceeded
		}
		reserved, err := reservedTokens(l)
		if err != nil {
			return err
		}
		if tokens > math.MaxInt-reserved || (c.config.MaxReservedTokens > 0 && tokens > c.config.MaxReservedTokens-reserved) {
			return ErrBudgetExceeded
		}
		l.Entries[key] = nil
		if tokens > 0 {
			if l.Estimates == nil {
				l.Estimates = map[string]int{}
			}
			l.Estimates[key] = tokens
		}
		return nil
	})
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
	for key, amount := range l.Estimates {
		if _, ok := l.Entries[key]; !ok || amount < 0 || amount > math.MaxInt-total {
			return 0, fmt.Errorf("无效的 Token 预留记录")
		}
		total += amount
	}
	return total, nil
}
