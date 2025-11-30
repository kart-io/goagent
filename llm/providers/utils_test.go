package providers

import (
	"strings"
	"testing"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm/common"
	"github.com/stretchr/testify/assert"
)

func TestParseRetryAfter_Integer(t *testing.T) {
	// Test integer seconds
	seconds := common.ParseRetryAfter("120")
	assert.Equal(t, 120, seconds)
}

func TestParseRetryAfter_Empty(t *testing.T) {
	// Test empty header returns default
	seconds := common.ParseRetryAfter("")
	assert.Equal(t, 60, seconds)
}

func TestParseRetryAfter_RFC1123(t *testing.T) {
	// Test HTTP-date format
	future := time.Now().Add(90 * time.Second)
	httpDate := future.Format(time.RFC1123)

	seconds := common.ParseRetryAfter(httpDate)
	// Should be approximately 90 seconds (allow some tolerance)
	assert.Greater(t, seconds, 85)
	assert.Less(t, seconds, 95)
}

func TestParseRetryAfter_InvalidFormat(t *testing.T) {
	// Test invalid format returns default
	seconds := common.ParseRetryAfter("invalid")
	assert.Equal(t, 60, seconds)
}

func TestParseRetryAfter_PastDate(t *testing.T) {
	// Test past date
	past := time.Now().Add(-30 * time.Second)
	httpDate := past.Format(time.RFC1123)

	seconds := common.ParseRetryAfter(httpDate)
	// Should return negative or small value
	assert.LessOrEqual(t, seconds, 0)
}

func TestGenerateCallID_Uniqueness(t *testing.T) {
	// Test that generated IDs are unique
	ids := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := common.GenerateCallID()
		assert.NotEmpty(t, id)
		assert.False(t, ids[id], "Generated duplicate ID: %s", id)
		ids[id] = true
	}
}

func TestGenerateCallID_Format(t *testing.T) {
	// Test ID format
	id := common.GenerateCallID()
	assert.True(t, strings.HasPrefix(id, "call_"), "ID should start with 'call_'")
	parts := strings.Split(id, "_")
	assert.GreaterOrEqual(t, len(parts), 2, "ID should have at least 2 parts")
}

func TestIsRetryable_RateLimitError(t *testing.T) {
	err := agentErrors.NewLLMRateLimitError("test", "model", 60)
	assert.True(t, common.IsRetryable(err))
}

func TestIsRetryable_TimeoutError(t *testing.T) {
	err := agentErrors.NewLLMTimeoutError("test", "model", 30)
	assert.True(t, common.IsRetryable(err))
}

func TestIsRetryable_RequestError(t *testing.T) {
	err := agentErrors.NewLLMRequestError("test", "model", assert.AnError)
	assert.True(t, common.IsRetryable(err))
}

func TestIsRetryable_NonRetryableError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
	}{
		{
			name: "InvalidInput",
			err:  agentErrors.NewInvalidInputError("test", "input", "invalid"),
		},
		{
			name: "InvalidConfig",
			err:  agentErrors.NewInvalidConfigError("test", "config", "invalid"),
		},
		{
			name: "LLMResponse",
			err:  agentErrors.NewLLMResponseError("test", "model", "error"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.False(t, common.IsRetryable(tc.err))
		})
	}
}

func TestIsRetryable_NilError(t *testing.T) {
	assert.False(t, common.IsRetryable(nil))
}
