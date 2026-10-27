package contextpolicy

import (
	"context"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// Descriptor identifies fixed policy configuration in a persisted model intent.
type Descriptor struct {
	ID, Version, ConfigHash string
	Durable                 bool
}

// Turn is one bounded model turn. Policies must not modify Round or RemainingCalls.
type Turn struct {
	Round, RemainingCalls int
	Messages              []llm.Message
}

// Policy transforms allowed message content without directly invoking a model.
type Policy interface {
	Descriptor() Descriptor
	BeforeTurn(context.Context, Turn) (Turn, error)
}

// Factory isolates mutable transformation state for each execution.
type Factory func() Policy

// ToolProvider exposes registered evidence-reader tools to the host adapter.
type ToolProvider interface{ Tools() []interfaces.Tool }

// FinalTurnRestorer restores evidence when no further reader call can fit.
type FinalTurnRestorer interface {
	BeforeFinalTurn(context.Context, Turn) (Turn, error)
}

// StatsProvider provides policy-scoped integer statistics.
type StatsProvider interface{ Stats() map[string]int64 }
