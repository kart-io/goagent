package multiagent

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/workflow"
)

// GuardRoute 把返回成员名或 constants.MultiagentFinish 的 next 包装成带兜底的 workflow.Router。fallback 为空表示不兜底。
// targets 是应交给 AddBranch 的全部目标（成员、END 与 fallback）。运行时依次：
// next 返回包装 ErrInvalidRoute 的错误且有兜底时转兜底，其他错误原样返回；完成信号映射为 constants.WorkflowEnd；
// 不在成员集合中的名字（含 Supervisor 名、兜底名与 END 字面值）按无效处理；否则返回该成员名。
// next 必须是纯函数：持久恢复会对同一份已提交状态重新路由。
func GuardRoute[T any](next func(context.Context, T) (string, error), members []string, fallback string) (route workflow.Router[T], targets []string, err error) {
	if next == nil {
		return nil, nil, agentErrors.NewInvalidConfigError("multiagent", "next", "GuardRoute 需要 next")
	}
	if len(members) == 0 {
		return nil, nil, agentErrors.NewInvalidConfigError("multiagent", "members", "GuardRoute 需要至少一个成员")
	}
	set := make(map[string]bool, len(members))
	for _, m := range members {
		if m == "" || m == constants.MultiagentFinish || m == constants.WorkflowEnd || set[m] {
			return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "GuardRoute 的成员名 %q 为空、保留或重复", m).WithComponent("multiagent").WithOperation("guard_route")
		}
		set[m] = true
	}
	if fallback != "" && (set[fallback] || fallback == constants.MultiagentFinish || fallback == constants.WorkflowEnd) {
		return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "兜底名 %q 不能与成员同名或使用保留名", fallback).WithComponent("multiagent").WithOperation("guard_route")
	}
	targets = append(slices.Clone(members), constants.WorkflowEnd)
	if fallback != "" {
		targets = append(targets, fallback)
	}
	route = func(ctx context.Context, state T) (string, error) {
		name, err := next(ctx, state)
		if err != nil {
			if fallback != "" && errors.Is(err, agentErrors.ErrInvalidRoute) {
				return fallback, nil
			}
			return "", err
		}
		if name == constants.MultiagentFinish {
			return constants.WorkflowEnd, nil
		}
		if !set[name] {
			if fallback != "" {
				return fallback, nil
			}
			return "", fmt.Errorf("%w：%q 不是成员名或 %s", agentErrors.ErrInvalidRoute, name, constants.MultiagentFinish)
		}
		return name, nil
	}
	return route, targets, nil
}
