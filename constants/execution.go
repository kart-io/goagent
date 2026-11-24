package constants

import "time"

// Durable model and inbox policy. Changing the resend count fails the policy
// check of existing runs. DefaultModelResendAttempts is the send count of the
// deprecated WithInterruptedModelResend; DefaultInboxWriteTimeout is the
// WithPersistTimeout default.
const (
	DefaultModelResendAttempts = 3
	DefaultInboxWriteTimeout   = 5 * time.Second
	// MaxModelRetryAfter bounds how long a turn lock is held waiting for the
	// server to recover.
	MaxModelRetryAfter = time.Minute
	// Backoff for transient request errors, separate from provider SDK retries.
	DefaultTransientRetryBaseDelay = 500 * time.Millisecond
	DefaultTransientRetryMaxDelay  = 8 * time.Second
)
