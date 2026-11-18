package common

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	agentllm "github.com/kart-io/goagent/llm"
)

// ParseRetryAfter parses Retry-After header (seconds or HTTP-date)
func ParseRetryAfter(header string) int {
	if header == "" {
		return 60 // Default 60 seconds
	}

	// Try parsing as integer (seconds)
	if seconds, err := strconv.Atoi(header); err == nil {
		return seconds
	}

	// Try parsing as HTTP-date (RFC1123)
	if t, err := time.Parse(time.RFC1123, header); err == nil {
		return int(time.Until(t).Seconds())
	}

	return 60 // Fallback
}

// RetryAfterDuration parses seconds or an HTTP date; missing, unparsable, or
// already-expired values all return 0, meaning none was given. Unlike
// ParseRetryAfter, it does not fall back to 60 seconds for unparsable values.
func RetryAfterDuration(header string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(header), 10, 64); err == nil {
		return time.Duration(min(max(seconds, 0), math.MaxInt64/int64(time.Second))) * time.Second
	}
	if at, err := http.ParseTime(header); err == nil {
		return max(at.Sub(now), 0)
	}
	return 0
}

// GenerateCallID generates a cryptographically secure unique ID for tool calls
func GenerateCallID() string {
	// Use crypto/rand for secure random number generation
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return fmt.Sprintf("call_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("call_%d_%s", time.Now().UnixNano(), hex.EncodeToString(b))
}

// IsRetryable prefers the provider's recovery information; only when that is
// missing does it fall back to the existing framework codes.
func IsRetryable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var providerErr *agentllm.ProviderError
	if errors.As(err, &providerErr) {
		return agentllm.ClassifyError(err).Retryable
	}

	code := agentErrors.GetCode(err)
	return code == agentErrors.CodeLLMRateLimit ||
		code == agentErrors.CodeLLMTimeout ||
		code == agentErrors.CodeLLMRequest
}
