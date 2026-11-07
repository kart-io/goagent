package usage

import agentErrors "github.com/kart-io/goagent/errors"

// ReasoningConfig is an explicit request setting, never an inferred server
// default or a claim that two providers apply equivalent reasoning policies.
// BudgetSet preserves an explicit zero budget; -1 requests a dynamic budget.
type ReasoningConfig struct {
	Effort       string `json:"effort,omitempty"`
	BudgetTokens int    `json:"budget_tokens,omitzero"`
	BudgetSet    bool   `json:"budget_set,omitzero"`
}

func (c ReasoningConfig) Validate() error {
	if c.BudgetSet && (c.BudgetTokens < -1 || int64(c.BudgetTokens) > 2147483647) {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "invalid thinking budget").WithComponent("reasoning").WithOperation("validate")
	}
	if !c.BudgetSet && c.BudgetTokens != 0 {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "thinking budget requires BudgetSet").WithComponent("reasoning").WithOperation("validate")
	}
	if c.BudgetSet && c.Effort != "" {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "reasoning effort and thinking budget are mutually exclusive").WithComponent("reasoning").WithOperation("validate")
	}
	switch c.Effort {
	case "", "none", "minimal", "low", "medium", "high", "xhigh", "max":
	default:
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "invalid reasoning effort").WithComponent("reasoning").WithOperation("validate")
	}
	return nil
}

// Clone copies explicit settings so mutable request hooks cannot change defaults.
func (c *ReasoningConfig) Clone() *ReasoningConfig {
	if c == nil {
		return nil
	}
	copy := *c
	return &copy
}
