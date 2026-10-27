package durable

import "context"

type executionKeyContext struct{}

// WithExecutionKey supplies a host-owned idempotency key for the current effect.
// It is not authentication. Nested effects must derive distinct keys or use
// their own business IDs; do not reuse a parent key for unrelated writes.
func WithExecutionKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, executionKeyContext{}, key)
}

// ExecutionKey returns the stable node/branch/step key, or empty outside a run.
func ExecutionKey(ctx context.Context) string {
	key, _ := ctx.Value(executionKeyContext{}).(string)
	return key
}
