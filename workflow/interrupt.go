package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kart-io/goagent/core/durable"
)

// ErrInterruptAnswered 表示该中断已经回应，新的输入与已固定的输入不同，被拒绝。
var ErrInterruptAnswered = errors.New("中断已回应")

// InterruptPoint 是一处等待人工输入的中断。Key 只用于 Resume 定位，不是授权凭证；
// 宿主必须先认证回应者并确认其有权回应。
type InterruptPoint struct {
	Key      string          `json:"key"`
	ID       string          `json:"id"`
	Info     json.RawMessage `json:"info"`
	Rejected bool            `json:"rejected,omitempty"`
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
			return ctx, "", &ResumeRejectedError{Err: fmt.Errorf("人工输入需要中断键")}
		}
		for _, p := range points {
			if _, ok := durable.ResumeInputFor(ctx, p.Key); ok {
				return ctx, "", nil
			}
			_, answered, err := durable.InterruptAnswer(ctx, store, p.Key, p.ID)
			if err != nil {
				return ctx, "", err
			}
			if answered {
				return ctx, "", nil
			}
		}
		return ctx, "", durable.ErrApprovalRequired
	}
	i := slices.IndexFunc(points, func(p InterruptPoint) bool { return p.Key == key })
	if i < 0 {
		return ctx, "", ErrApprovalMismatch
	}
	recorded, answered, err := durable.InterruptAnswer(ctx, store, key, points[i].ID)
	if err != nil {
		return ctx, "", err
	}
	if answered {
		if input != nil && !sameJSON(input, recorded) {
			return ctx, "", &ResumeRejectedError{Err: ErrInterruptAnswered}
		}
		return ctx, "", nil
	}
	if input == nil {
		return ctx, "", &ResumeRejectedError{Err: fmt.Errorf("中断 %q 需要输入", points[i].ID)}
	}
	if !json.Valid(input) {
		return ctx, "", &ResumeRejectedError{Err: fmt.Errorf("中断输入必须是合法 JSON")}
	}
	return durable.WithResume(ctx, key, string(input)), key, nil
}

// sameJSON 按 json.Compact 后的字节比较：空白差异相同，键顺序或数字写法不同算不同。
func sameJSON(a, b json.RawMessage) bool {
	var x, y bytes.Buffer
	if json.Compact(&x, a) != nil || json.Compact(&y, b) != nil {
		return false
	}
	return bytes.Equal(x.Bytes(), y.Bytes())
}
