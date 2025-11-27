package providers

import "github.com/kart-io/goagent/llm/common"

// ParseRetryAfter is now an alias to common.ParseRetryAfter for backward compatibility.
// Deprecated: Use common.ParseRetryAfter directly.
var parseRetryAfter = common.ParseRetryAfter

// GenerateCallID is now an alias to common.GenerateCallID for backward compatibility.
// Deprecated: Use common.GenerateCallID directly.
var generateCallID = common.GenerateCallID

// IsRetryable is now an alias to common.IsRetryable for backward compatibility.
// Deprecated: Use common.IsRetryable directly.
var isRetryable = common.IsRetryable
