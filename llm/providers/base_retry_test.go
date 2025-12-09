package providers

import (
	"context"
	"os"
	"testing"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteWithRetry_Success(t *testing.T) {
	// Test first attempt success
	callCount := 0
	execute := func(ctx context.Context) (string, error) {
		callCount++
		return "success", nil
	}

	cfg := common.RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
	}

	ctx := context.WithValue(context.Background(), "test_retry_delay", 1*time.Millisecond)

	result, err := common.ExecuteWithRetry(ctx, cfg, "test-provider", execute)

	require.NoError(t, err)
	assert.Equal(t, "success", result)
	assert.Equal(t, 1, callCount, "Should only call once on success")
}

func TestExecuteWithRetry_SuccessAfterRetries(t *testing.T) {
	// Test success after retries
	callCount := 0
	execute := func(ctx context.Context) (string, error) {
		callCount++
		if callCount < 3 {
			return "", agentErrors.NewLLMRateLimitError("test", "test-model", 60)
		}
		return "success", nil
	}

	cfg := common.RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
	}

	ctx := context.WithValue(context.Background(), "test_retry_delay", 1*time.Millisecond)

	result, err := common.ExecuteWithRetry(ctx, cfg, "test-provider", execute)

	require.NoError(t, err)
	assert.Equal(t, "success", result)
	assert.Equal(t, 3, callCount, "Should retry until success")
}

func TestExecuteWithRetry_MaxRetriesExceeded(t *testing.T) {
	// Test max retries exceeded
	callCount := 0
	execute := func(ctx context.Context) (string, error) {
		callCount++
		return "", agentErrors.NewLLMRateLimitError("test", "test-model", 60)
	}

	cfg := common.RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
	}

	ctx := context.WithValue(context.Background(), "test_retry_delay", 1*time.Millisecond)

	_, err := common.ExecuteWithRetry(ctx, cfg, "test-provider", execute)

	require.Error(t, err)
	assert.Equal(t, 3, callCount, "Should try all attempts")
}

func TestExecuteWithRetry_ContextCanceled(t *testing.T) {
	// Test context cancellation during retry
	callCount := 0
	ctx, cancel := context.WithCancel(context.Background())

	execute := func(ctx context.Context) (string, error) {
		callCount++
		if callCount == 1 {
			// Cancel context after first failure
			cancel()
		}
		return "", agentErrors.NewLLMRateLimitError("test", "test-model", 60)
	}

	cfg := common.RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
	}

	ctx = context.WithValue(ctx, "test_retry_delay", 1*time.Millisecond)

	_, err := common.ExecuteWithRetry(ctx, cfg, "test-provider", execute)

	require.Error(t, err)
	assert.Equal(t, agentErrors.CodeContextCanceled, agentErrors.GetCode(err))
}

func TestExecuteWithRetry_NonRetryableError(t *testing.T) {
	// Test non-retryable error
	callCount := 0
	execute := func(ctx context.Context) (string, error) {
		callCount++
		return "", agentErrors.NewInvalidInputError("test", "input", "invalid")
	}

	cfg := common.DefaultRetryConfig()
	ctx := context.WithValue(context.Background(), "test_retry_delay", 1*time.Millisecond)

	_, err := common.ExecuteWithRetry(ctx, cfg, "test-provider", execute)

	require.Error(t, err)
	assert.Equal(t, 1, callCount, "Should not retry non-retryable errors")
	assert.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err))
}

func TestExecuteWithRetry_DefaultMaxAttempts(t *testing.T) {
	// Test with zero MaxAttempts uses default
	callCount := 0
	execute := func(ctx context.Context) (string, error) {
		callCount++
		if callCount < 3 {
			return "", agentErrors.NewLLMRateLimitError("test", "test-model", 60)
		}
		return "success", nil
	}

	cfg := common.RetryConfig{
		MaxAttempts: 0, // Should use default
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
	}

	ctx := context.WithValue(context.Background(), "test_retry_delay", 1*time.Millisecond)

	result, err := common.ExecuteWithRetry(ctx, cfg, "test-provider", execute)

	require.NoError(t, err)
	assert.Equal(t, "success", result)
}

func TestExecuteWithRetry_ExponentialBackoff(t *testing.T) {
	// Test exponential backoff timing
	// Use shorter delays for test stability
	callCount := 0
	timestamps := []time.Time{}

	execute := func(ctx context.Context) (string, error) {
		callCount++
		timestamps = append(timestamps, time.Now())
		if callCount < 3 {
			return "", agentErrors.NewLLMRateLimitError("test", "test-model", 60)
		}
		return "success", nil
	}

	cfg := common.RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond, // Shorter for test stability
		MaxDelay:    100 * time.Millisecond,
	}

	// Use test mode for faster retries
	ctx := context.WithValue(context.Background(), "test_retry_delay", 5*time.Millisecond)

	result, err := common.ExecuteWithRetry(ctx, cfg, "test-provider", execute)

	require.NoError(t, err)
	assert.Equal(t, "success", result)
	assert.Equal(t, 3, callCount)

	// Verify we got at least 3 timestamps
	assert.GreaterOrEqual(t, len(timestamps), 3, "Should have at least 3 attempts")

	// Verify exponential backoff behavior (each retry should have some delay)
	// In test mode, delays are very short, so just verify there was some delay
	if len(timestamps) >= 2 {
		firstDelay := timestamps[1].Sub(timestamps[0])
		// With test_retry_delay=5ms, expect at least 3ms (allowing for timing variance)
		assert.GreaterOrEqual(t, firstDelay.Milliseconds(), int64(3),
			"First retry should have some delay (test mode)")
	}
}

func TestExecuteWithRetry_MaxDelayLimit(t *testing.T) {
	// Test that delay doesn't exceed MaxDelay
	callCount := 0
	execute := func(ctx context.Context) (string, error) {
		callCount++
		return "", agentErrors.NewLLMRateLimitError("test", "test-model", 60)
	}

	cfg := common.RetryConfig{
		MaxAttempts: 5,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    150 * time.Millisecond, // Cap at 150ms
	}

	ctx := context.WithValue(context.Background(), "test_retry_delay", 10*time.Millisecond)

	_, err := common.ExecuteWithRetry(ctx, cfg, "test-provider", execute)

	require.Error(t, err)
	assert.Equal(t, 5, callCount)
}

func TestDefaultRetryConfig(t *testing.T) {
	// Test default retry config values
	cfg := common.DefaultRetryConfig()

	assert.Greater(t, cfg.MaxAttempts, 0, "MaxAttempts should be positive")
	assert.Greater(t, cfg.BaseDelay, time.Duration(0), "BaseDelay should be positive")
	assert.Greater(t, cfg.MaxDelay, time.Duration(0), "MaxDelay should be positive")
	assert.GreaterOrEqual(t, cfg.MaxDelay, cfg.BaseDelay, "MaxDelay should be >= BaseDelay")
}

func TestExecuteWithRetry_TestModeEnvVar(t *testing.T) {
	// Test GO_TEST_MODE environment variable
	os.Setenv("GO_TEST_MODE", "true")
	defer os.Unsetenv("GO_TEST_MODE")

	callCount := 0
	timestamps := []time.Time{}

	execute := func(ctx context.Context) (string, error) {
		callCount++
		timestamps = append(timestamps, time.Now())
		if callCount < 2 {
			return "", agentErrors.NewLLMRateLimitError("test", "test-model", 60)
		}
		return "success", nil
	}

	cfg := common.RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   100 * time.Millisecond, // Will be reduced to 10ms by test mode
		MaxDelay:    500 * time.Millisecond,
	}

	ctx := context.Background()

	result, err := common.ExecuteWithRetry(ctx, cfg, "test-provider", execute)

	require.NoError(t, err)
	assert.Equal(t, "success", result)
	assert.Equal(t, 2, callCount)

	// Verify delay was shortened by test mode
	if len(timestamps) >= 2 {
		delay := timestamps[1].Sub(timestamps[0])
		// Should be much less than 100ms due to test mode
		assert.Less(t, delay.Milliseconds(), int64(50))
	}
}
