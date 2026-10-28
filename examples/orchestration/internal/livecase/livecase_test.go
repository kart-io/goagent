package livecase

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecision(t *testing.T) {
	for _, tc := range []struct {
		answer, want string
		ok           bool
	}{
		{"结论：批准\n理由：未拆封", "批准", true},
		{"结论: 拒绝。超过 7 天", "拒绝", true},
		{"**结论：批准** 满足条件", "批准", true},
		{"前文\n结论：拒绝", "拒绝", true},
		{"结论：批准（金额未超限）", "批准", true},
		{"结论：待定", "", false},
		{"批准退款", "", false},
		{"", "", false},
	} {
		got, err := Decision(tc.answer, "批准", "拒绝")
		if tc.ok {
			require.NoError(t, err, tc.answer)
			require.Equal(t, tc.want, got)
		} else {
			require.Error(t, err, tc.answer)
		}
	}
}
