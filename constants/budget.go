package constants

import "math"

// 预算默认策略；Unlimited 保留目标架构的 int 上限。
const (
	// BudgetUnlimited 是未绑定预算时 Remaining 返回的余量，可直接参与大小比较。
	BudgetUnlimited = math.MaxInt
	// DefaultMaxAgentCalls is the agent-call limit that budget.Config filled in
	// for 0 before limits became caller-owned. budget no longer applies it; it
	// is kept for ledgers and management/v1 tasks that persisted this value.
	DefaultMaxAgentCalls = 1000
)
