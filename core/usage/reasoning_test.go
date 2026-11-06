package usage

import (
	"testing"

	"github.com/kart-io/goagent/codec/json"
)

func TestReasoningConfigValidationAndZero(t *testing.T) {
	good := ReasoningConfig{BudgetSet: true, BudgetTokens: 0}
	data, err := json.Marshal(good)
	if err != nil {
		t.Fatal(err)
	}
	var round ReasoningConfig
	if err = json.Unmarshal(data, &round); err != nil || round != good {
		t.Fatalf("lost explicit zero: %+v %v", round, err)
	}
	for _, c := range []ReasoningConfig{{Effort: "invalid"}, {BudgetSet: true, BudgetTokens: -2}, {BudgetTokens: 5}, {Effort: "low", BudgetSet: true}} {
		if c.Validate() == nil {
			t.Fatalf("invalid settings accepted: %+v", c)
		}
	}
}
