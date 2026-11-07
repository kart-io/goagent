package tools

import (
	"context"
	"errors"
	"fmt"
	"testing"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func TestDispatcherArgumentErrorCodeAndIdentity(t *testing.T) {
	for _, cause := range []error{nil, errors.New("内部解析细节")} {
		name := "无原因"
		if cause != nil {
			name = "有原因"
		}
		t.Run(name, func(t *testing.T) {
			argument := &ArgumentError{Message: "id 必填", Err: cause}
			tool := NewBaseTool("lookup", "查询", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				return nil, argument
			})
			input := func() *interfaces.ToolInput { return &interfaces.ToolInput{Args: map[string]any{}} }
			d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool)
			out, err := d.Invoke(context.Background(), ToolRequest{Name: "lookup", Input: input()})
			require.Nil(t, out)
			require.Same(t, argument, err, "宿主调用保持原始错误身份")
			require.Equal(t, agentErrors.CodeToolValidation, agentErrors.GetCode(err))
			require.Equal(t, agentErrors.CodeToolValidation, agentErrors.GetCode(fmt.Errorf("包装: %w", err)))
			var extracted *ArgumentError
			require.ErrorAs(t, err, &extracted)
			require.Same(t, argument, extracted)
			require.ErrorIs(t, err, argument)
			require.EqualError(t, err, "id 必填", "原因不得流入模型可见文本")
			if cause == nil {
				require.Nil(t, errors.Unwrap(err), "无原因时不补哨兵")
			} else {
				require.ErrorIs(t, err, cause)
			}

			out, err = d.Tools()[0].Invoke(context.Background(), input())
			requireArgumentResult(t, out, err, "id 必填")
			message, recoverable := ArgumentErrorResult(out)
			require.True(t, recoverable)
			require.Equal(t, "id 必填", message)
		})
	}
}

func TestArgumentErrorCodeDoesNotGrantModelRecovery(t *testing.T) {
	business := agentErrors.New(agentErrors.CodeToolValidation, "业务校验失败")
	_, marked := business.ModelArgumentMessage()
	require.False(t, marked)
	tool := NewBaseTool("op", "执行", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return nil, business
	})
	d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool)
	out, err := d.Tools()[0].Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{}})
	require.Nil(t, out)
	require.Same(t, business, err, "相同错误码的业务错误不得成为模型可恢复错误")
}

func TestArgumentErrorNilReceiverCode(t *testing.T) {
	var argument *ArgumentError
	coded, ok := any(argument).(agentErrors.CodedError)
	require.True(t, ok, "参数错误必须参与已有分类协议")
	require.Equal(t, agentErrors.CodeInternal, coded.ErrorCode())
}
