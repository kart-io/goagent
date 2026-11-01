package usage

import "fmt"

// ReasoningConfig is an explicit request setting, never an inferred server
// default or a claim that two providers apply equivalent reasoning policies.
// BudgetSet preserves an explicit zero budget; -1 requests a dynamic budget.
type ReasoningConfig struct {
	Effort       string `json:"effort,omitempty"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
	BudgetSet    bool   `json:"budget_set,omitempty"`
}

func (c ReasoningConfig) Validate() error {
	if c.BudgetSet && (c.BudgetTokens < -1 || int64(c.BudgetTokens) > 2147483647) {
		return fmt.Errorf("invalid thinking budget")
	}
	if !c.BudgetSet && c.BudgetTokens != 0 {
		return fmt.Errorf("thinking budget requires BudgetSet")
	}
	if c.BudgetSet && c.Effort != "" {
		return fmt.Errorf("reasoning effort and thinking budget are mutually exclusive")
	}
	switch c.Effort {
	case "", "none", "minimal", "low", "medium", "high", "xhigh", "max":
	default:
		return fmt.Errorf("invalid reasoning effort")
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
