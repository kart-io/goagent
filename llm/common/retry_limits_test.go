package common_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm/common"
)

func countAttempts(t *testing.T, cfg common.RetryConfig) (int, error) {
	t.Helper()
	calls := 0
	_, err := common.ExecuteWithRetry(context.Background(), cfg, "test", func(context.Context) (string, error) {
		calls++
		return "", agentErrors.NewLLMRateLimitError("test", "m", 1)
	})
	return calls, err
}

func TestRetriesZeroMeansNoRetry(t *testing.T) {
	calls, err := countAttempts(t, common.RetryConfig{})
	require.Error(t, err)
	require.Equal(t, 1, calls, "零值配置旧实现会尝试 3 次")
}

func TestRetriesAndMaxAttemptsPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cfg     common.RetryConfig
		calls   int
		invalid bool
	}{
		{"两者都为 0：不重试", common.RetryConfig{}, 1, false},
		{"只设 Retries", common.RetryConfig{Retries: 2}, 3, false},
		{"只设 MaxAttempts：旧语义", common.RetryConfig{MaxAttempts: 4}, 4, false},
		{"只设负 MaxAttempts：旧语义 3 次", common.RetryConfig{MaxAttempts: -1}, 3, false},
		{"两者都为正：报错", common.RetryConfig{Retries: 1, MaxAttempts: 2}, 0, true},
		{"Retries 为负：报错", common.RetryConfig{Retries: -1}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, err := countAttempts(t, tc.cfg)
			require.Error(t, err)
			require.Equal(t, tc.calls, calls)
			if tc.invalid {
				require.True(t, agentErrors.IsCode(err, agentErrors.CodeInvalidConfig), err)
			}
		})
	}
}
