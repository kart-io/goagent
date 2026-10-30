package durable

import "context"

type resumeContext struct{}

// WithResume 为一个等待中的操作提供续跑输入（如审批键）。key 取 WaitError.Path 最后一项的 Key，
// 即等待发生处的执行键；可多次调用为不同等待分别提供。输入是否被授权由宿主负责。
func WithResume(ctx context.Context, key, input string) context.Context {
	prev, _ := ctx.Value(resumeContext{}).(map[string]string)
	next := make(map[string]string, len(prev)+1)
	for k, v := range prev {
		next[k] = v
	}
	next[key] = input
	return context.WithValue(ctx, resumeContext{}, next)
}

// ResumeInput 返回宿主为当前执行键提供的续跑输入。
func ResumeInput(ctx context.Context) (string, bool) {
	inputs, _ := ctx.Value(resumeContext{}).(map[string]string)
	key := ExecutionKey(ctx)
	if key == "" {
		return "", false
	}
	input, ok := inputs[key]
	return input, ok
}
