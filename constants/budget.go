package constants

import "math"

// 预算默认策略；Unlimited 保留目标架构的 int 上限。
const (
	// BudgetUnlimited 是未绑定预算时 Remaining 返回的余量，可直接参与大小比较。
	BudgetUnlimited = math.MaxInt
	// DefaultMaxAgentCalls 限制宿主未指定上限时的拓扑调用数量。
	DefaultMaxAgentCalls = 1000
)
