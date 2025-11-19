package providers

import (
	"fmt"
	"math/rand"
	"strconv"
	"time"
)

// parseRetryAfter parses Retry-After header (seconds or HTTP-date)
func parseRetryAfter(header string) int {
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

// generateCallID generates a unique ID for tool calls
func generateCallID() string {
	return fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), rand.Intn(100000))
}
