package durable

import "context"

type resumeContext struct{}

// WithResume supplies resume input (such as an approval key) for a waiting
// operation. key is the Key of the last element of WaitError.Path, that is, the
// execution key where the wait occurred; call it multiple times to supply input
// for different waits. The host is responsible for whether the input is
// authorized.
func WithResume(ctx context.Context, key, input string) context.Context {
	prev, _ := ctx.Value(resumeContext{}).(map[string]string)
	next := make(map[string]string, len(prev)+1)
	for k, v := range prev {
		next[k] = v
	}
	next[key] = input
	return context.WithValue(ctx, resumeContext{}, next)
}

// ResumeInput returns the resume input the host supplied for the current
// execution key.
func ResumeInput(ctx context.Context) (string, bool) {
	return ResumeInputFor(ctx, ExecutionKey(ctx))
}

// ResumeInputFor returns the resume input the host supplied for key, letting
// upper layers that host a wait decide whether to re-enter.
func ResumeInputFor(ctx context.Context, key string) (string, bool) {
	if key == "" {
		return "", false
	}
	inputs, _ := ctx.Value(resumeContext{}).(map[string]string)
	input, ok := inputs[key]
	return input, ok
}
