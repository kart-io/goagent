package constants

import "time"

// Timeouts. Integer-second and time.Duration values are named separately so
// older APIs never convert units implicitly.
const (
	// DefaultLLMTimeoutSeconds is the former llm client timeout. It is no longer
	// applied; 0 means no timeout.
	DefaultLLMTimeoutSeconds = 60
	DefaultToolTimeout       = DefaultToolTimeoutSeconds * time.Second
	// DefaultAgentExecutionTimeout and DefaultAgentMaxIterations are no longer
	// applied by builder (0 means unlimited); presets and callers that want the
	// old values pass them explicitly.
	DefaultAgentExecutionTimeout = 5 * time.Minute
	DefaultSessionCleanupTimeout = 5 * time.Second
	// DefaultUsageHookTimeout bounds one budget usage statistics query plus
	// hook call when the caller does not choose a timeout.
	DefaultUsageHookTimeout = 5 * time.Second
)

// Per-layer defaults. Each layer has its own name even when values coincide.
const (
	// DefaultClientMaxTokens is no longer sent by default (0 omits max_tokens);
	// it is only the default for Anthropic's required max_tokens.
	DefaultClientMaxTokens    = 2000
	DefaultAgentMaxIterations = 10
	// DefaultDispatcherConcurrency is no longer applied by Dispatcher (0 means
	// unlimited); callers that want the old value pass it explicitly.
	DefaultDispatcherConcurrency = 4
	DefaultDispatcherAttempts    = 3
	// DefaultImageTokens is the estimated token count of one attached image
	// when compaction is not given one.
	DefaultImageTokens = 1500
)
