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

// Releaser 释放一次执行写入的资源。Builder 只在本次执行使用它自动生成的临时结果作用域时
// （无持久日志且宿主未调用 WithResultScope），于执行结束后调用一次，返回的错误计入执行结果。
// 持久执行或宿主提供作用域时不调用：作用域可能被恢复或复用，由宿主在不再需要时调用 ResultStore.Release。
type Releaser interface {
	Release(context.Context) error
}
