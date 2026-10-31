package observe

import "context"

// RunIDFromContext 只读返回已绑定的运行身份；未绑定或 nil context 时返回空串。
func RunIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if s, ok := ctx.Value(scopeKey{}).(*scope); ok && s != nil {
		return s.id
	}
	return ""
}
