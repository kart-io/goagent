package common

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
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
