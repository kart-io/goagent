package multiagent

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
)

func fixed(name string, err error) func(context.Context, record) (string, error) {
	return func(context.Context, record) (string, error) { return name, err }
}

func TestGuardRouteValidation(t *testing.T) {
	next := fixed("a", nil)
	for name, tc := range map[string]struct {
		next     func(context.Context, record) (string, error)
		members  []string
		fallback string
	}{
		"next 为空":    {nil, []string{"a"}, ""},
		"没有成员":       {next, nil, ""},
		"成员为空":       {next, []string{""}, ""},
		"成员重复":       {next, []string{"a", "a"}, ""},
		"成员为 FINISH": {next, []string{constants.MultiagentFinish}, ""},
		"成员为 END":    {next, []string{constants.WorkflowEnd}, ""},
		"兜底与成员同名":    {next, []string{"a"}, "a"},
	} {
		_, _, err := GuardRoute(tc.next, tc.members, tc.fallback)
		require.Error(t, err, name)
	}
}

func TestGuardRouteTargets(t *testing.T) {
	_, targets, err := GuardRoute(fixed("a", nil), []string{"a", "b"}, "")
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b", constants.WorkflowEnd}, targets)
	_, targets, err = GuardRoute(fixed("a", nil), []string{"a", "b"}, "human")
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b", constants.WorkflowEnd, "human"}, targets)
}

func TestGuardRouteOrder(t *testing.T) {
	invalid := fmt.Errorf("%w：无法识别", agentErrors.ErrInvalidRoute)
	other := errors.New("存储不可用")
	limit := fmt.Errorf("%w: review loop limit 3", agentErrors.ErrWorkflowMaxSteps)
	for _, tc := range []struct {
		name     string
		next     func(context.Context, record) (string, error)
		fallback string
		want     string
		wantErr  error
	}{
		{"无效错误转兜底", fixed("a", invalid), "human", "human", nil},
		{"无效错误无兜底", fixed("a", invalid), "", "", agentErrors.ErrInvalidRoute},
		{"其他错误原样返回", fixed("a", other), "human", "", other},
		{"ErrMaxSteps 原样返回", fixed("", limit), "human", "", agentErrors.ErrWorkflowMaxSteps},
		{"FINISH 映射为 END", fixed(constants.MultiagentFinish, nil), "human", constants.WorkflowEnd, nil},
		{"成员", fixed("b", nil), "human", "b", nil},
		{"未知名转兜底", fixed("x", nil), "human", "human", nil},
		{"未知名无兜底", fixed("x", nil), "", "", agentErrors.ErrInvalidRoute},
		{"END 字面值按无效", fixed(constants.WorkflowEnd, nil), "", "", agentErrors.ErrInvalidRoute},
		{"Supervisor 名按无效", fixed("sup", nil), "", "", agentErrors.ErrInvalidRoute},
		{"兜底名按无效路由转兜底", fixed("human", nil), "human", "human", nil},
	} {
		route, _, err := GuardRoute(tc.next, []string{"a", "b"}, tc.fallback)
		require.NoError(t, err)
		got, err := route(context.Background(), record{})
		if tc.wantErr != nil {
			require.ErrorIs(t, err, tc.wantErr, tc.name)
			continue
		}
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.want, got, tc.name)
	}
}
