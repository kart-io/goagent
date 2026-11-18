package observe

import "context"

// RunIDFromContext returns the bound run identity read-only; it returns an
// empty string when none is bound or the context is nil.
func RunIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if s, ok := ctx.Value(scopeKey{}).(*scope); ok && s != nil {
		return s.id
	}
	return ""
}
