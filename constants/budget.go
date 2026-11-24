package constants

import "math"

// Budget defaults.
const (
	// BudgetUnlimited is the remaining amount reported when no budget is bound;
	// it compares correctly with any int.
	BudgetUnlimited = math.MaxInt
	// DefaultMaxAgentCalls is the agent-call limit that budget.Config filled in
	// for 0 before limits became caller-owned. budget no longer applies it; it
	// is kept for ledgers and management/v1 tasks that persisted this value.
	DefaultMaxAgentCalls = 1000
)
