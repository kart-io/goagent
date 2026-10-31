package common

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
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

// RetryAfterDuration 解析秒数或 HTTP 日期；缺失、无法解析或已过期都返回 0，即视为没有给出。
// 与 ParseRetryAfter 不同，它不对无法解析的值回退为 60 秒。
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

// IsRetryable checks if an error is retryable based on its error code.
// Retryable errors include rate limit errors, timeout errors, and general request errors.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	code := agentErrors.GetCode(err)
	return code == agentErrors.CodeLLMRateLimit ||
		code == agentErrors.CodeLLMTimeout ||
		code == agentErrors.CodeLLMRequest
}
