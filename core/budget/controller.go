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
	"os"
	"sync"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
)

var (
	ErrBudgetExceeded    = errors.New("root model request budget exceeded")
	ErrReservationExists = errors.New("model request reservation already exists")
)

// Config is supplied by the host. RootKey identifies one logical run.
type Config struct {
	RootKey, Version string
	MaxModelRequests int
}

// Snapshot distinguishes conservative reservations from known token evidence.
type Snapshot struct {
	ReservedRequests, KnownUsageEntries, UnknownUsageEntries int
	KnownTokens                                              interfaces.TokenUsage
}
type ledger struct {
	Config  Config
	Entries map[string]*interfaces.TokenUsage
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
	if config.RootKey == "" || config.Version == "" || config.MaxModelRequests < 1 {
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
	b, _ := json.Marshal(l)
	var out ledger
	_ = json.Unmarshal(b, &out)
	return out
}
func (c *Controller) transaction(ctx context.Context, write bool, fn func(*ledger) error) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	l := clone(c.memory)
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
			if s == nil || s.Version != "goagent-model-budget-v1" || s.RunID != id || json.Unmarshal(s.Data, &l) != nil {
				return fmt.Errorf("invalid budget record")
			}
		}
	}
	if l.Config != c.config || l.Entries == nil || len(l.Entries) > c.config.MaxModelRequests {
		return fmt.Errorf("budget configuration or record changed")
	}
	for key, u := range l.Entries {
		if key == "" || !validUsage(u) {
			return fmt.Errorf("invalid budget usage record")
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
	if key == "" {
		return fmt.Errorf("model operation key required")
	}
	return c.transaction(ctx, true, func(l *ledger) error {
		if _, ok := l.Entries[key]; ok {
			return ErrReservationExists
		}
		if len(l.Entries) >= c.config.MaxModelRequests {
			return ErrBudgetExceeded
		}
		l.Entries[key] = nil
		return nil
	})
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
		for _, u := range l.Entries {
			if u == nil {
				out.UnknownUsageEntries++
			} else {
				out.KnownUsageEntries++
				out.KnownTokens.Add(u)
			}
		}
		return nil
	})
	return
}
