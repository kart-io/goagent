package workflow

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
)

// InterruptPoint 是一处等待人工输入的中断。Key 只用于 Resume 定位，不是授权凭证；
// 宿主必须先认证回应者并确认其有权回应。
type InterruptPoint struct {
	Key      string          `json:"key"`
	ID       string          `json:"id"`
	Info     json.RawMessage `json:"info"`
	Rejected bool            `json:"rejected,omitzero"`
	Reason   string          `json:"reason,omitempty"`
}

// interruptPoints 把节点收到的等待转换为按 Key 排序的中断列表；含非中断等待时返回 false，节点按失败处理。
func interruptPoints(waits []*durable.WaitError) ([]InterruptPoint, bool) {
	var points []InterruptPoint
	for _, w := range waits {
		var probe struct {
			Kind       string           `json:"kind"`
			Interrupts []InterruptPoint `json:"interrupts"`
		}
		if json.Unmarshal(w.Continuation, &probe) != nil {
			return nil, false
		}
		switch {
		case probe.Kind == durable.ContinuationKindInterrupt:
			var c durable.InterruptContinuation
			if json.Unmarshal(w.Continuation, &c) != nil || len(w.Path) == 0 {
				return nil, false
			}
			points = append(points, InterruptPoint{Key: w.Path[len(w.Path)-1].Key, ID: c.ID, Info: c.Info, Rejected: c.Rejected, Reason: c.Reason})
		case len(probe.Interrupts) > 0:
			points = append(points, probe.Interrupts...)
		default:
			return nil, false
		}
	}
	slices.SortFunc(points, func(a, b InterruptPoint) int { return strings.Compare(a.Key, b.Key) })
	return points, len(points) > 0
}

// prepareInterruptResume 决定中断等待的图是否重新执行节点，返回注入输入后的 ctx 与本次回应的键。
// 不需要执行时返回 durable.ErrApprovalRequired；拒绝时检查点不变。
func (d *DurableGraph[T]) prepareInterruptResume(ctx context.Context, store durable.Store, points []InterruptPoint, key string, input json.RawMessage) (context.Context, string, error) {
	if key == "" {
		if input != nil {
			return ctx, "", &ResumeRejectedError{Err: agentErrors.Newf(agentErrors.CodeInvalidInput, "人工输入需要中断键").WithComponent("workflow").WithOperation("prepare_interrupt_resume")}
		}
		ready, err := interruptReady(ctx, store, points)
		if err != nil || ready {
			return ctx, "", err
		}
		return ctx, "", agentErrors.ErrApprovalRequired
	}
	i := slices.IndexFunc(points, func(p InterruptPoint) bool { return p.Key == key })
	if i < 0 {
		return ctx, "", agentErrors.ErrApprovalMismatch
	}
	recorded, answered, err := durable.InterruptAnswer(ctx, store, key, points[i].ID)
	if err != nil {
		return ctx, "", err
	}
	if answered {
		if input != nil && !sameJSON(input, recorded) {
			return ctx, "", &ResumeRejectedError{Err: agentErrors.ErrInterruptAnswered}
		}
		return ctx, "", nil
	}
	if input == nil {
		return ctx, "", &ResumeRejectedError{Err: agentErrors.Newf(agentErrors.CodeInvalidInput, "中断 %q 需要输入", points[i].ID).WithComponent("workflow").WithOperation("prepare_interrupt_resume")}
	}
	if !json.Valid(input) {
		return ctx, "", &ResumeRejectedError{Err: agentErrors.Newf(agentErrors.CodeInvalidInput, "中断输入必须是合法 JSON").WithComponent("workflow").WithOperation("prepare_interrupt_resume")}
	}
	return durable.WithResume(ctx, key, string(input)), key, nil
}

// interruptReady 判断不带回应键时节点能否推进：ctx 中带有某处中断的输入，或某处中断已回应。
func interruptReady(ctx context.Context, store durable.Store, points []InterruptPoint) (bool, error) {
	for _, p := range points {
		if _, ok := durable.ResumeInputFor(ctx, p.Key); ok {
			return true, nil
		}
		_, answered, err := durable.InterruptAnswer(ctx, store, p.Key, p.ID)
		if err != nil || answered {
			return answered, err
		}
	}
	return false, nil
}

// waitCause 汇总等待携带的进程内诊断（如 validate 的 panic），没有时返回 nil。
func waitCause(waits []*durable.WaitError) error {
	var causes []error
	for _, w := range waits {
		if w.Cause != nil {
			causes = append(causes, w.Cause)
		}
	}
	return errors.Join(causes...)
}

// sameJSON 按压缩后的字节比较：空白差异相同，键顺序或数字写法不同算不同。
func sameJSON(a, b json.RawMessage) bool {
	x, errX := json.Compact(a)
	y, errY := json.Compact(b)
	return errX == nil && errY == nil && bytes.Equal(x, y)
}
