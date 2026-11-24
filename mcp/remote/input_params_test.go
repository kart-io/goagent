package remote

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
)

//nolint:staticcheck // 覆盖仍处于过渡期的 roots/sampling 续接。
func TestInputRequiredParams(t *testing.T) {
	pending := func(req mcp.InputRequest) *InputRequiredError {
		return &InputRequiredError{Name: "review", Arguments: []byte(`{"id":1}`), RequestState: "s", Requests: mcp.InputRequestMap{"a": req}}
	}
	cases := []struct {
		name     string
		e        *InputRequiredError
		response mcp.InputResponse
		ok       bool
	}{
		{"elicit 接受", pending(&mcp.ElicitParams{}), &mcp.ElicitResult{Action: "accept"}, true},
		{"elicit 动作无效", pending(&mcp.ElicitParams{}), &mcp.ElicitResult{Action: "maybe"}, false},
		{"elicit 回应类型不符", pending(&mcp.ElicitParams{}), &mcp.ListRootsResult{}, false},
		{"roots", pending(&mcp.ListRootsParams{}), &mcp.ListRootsResult{}, true},
		{"roots 回应类型不符", pending(&mcp.ListRootsParams{}), &mcp.ElicitResult{Action: "accept"}, false},
		{"sampling", pending(&mcp.CreateMessageParams{}), &mcp.CreateMessageResult{}, true},
		{"sampling 带工具", pending(&mcp.CreateMessageWithToolsParams{}), &mcp.CreateMessageWithToolsResult{}, true},
		{"sampling 带工具回应类型不符", pending(&mcp.CreateMessageWithToolsParams{}), &mcp.CreateMessageResult{}, false},
		{"缺少回应", pending(&mcp.ElicitParams{}), nil, false},
		{"参数不是 JSON", &InputRequiredError{Name: "review", Arguments: []byte("{"), Requests: mcp.InputRequestMap{"a": &mcp.ElicitParams{}}}, &mcp.ElicitResult{Action: "accept"}, false},
		{"缺少工具名", &InputRequiredError{Arguments: []byte(`{}`), Requests: mcp.InputRequestMap{"a": &mcp.ElicitParams{}}}, &mcp.ElicitResult{Action: "accept"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			responses := mcp.InputResponseMap{"a": c.response}
			if c.response == nil {
				responses = mcp.InputResponseMap{"b": &mcp.ElicitResult{Action: "accept"}}
			}
			p, err := c.e.params(responses)
			if !c.ok {
				require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err))
				return
			}
			require.NoError(t, err)
			require.Equal(t, "review", p.Name)
			args, err := json.Marshal(p.Arguments)
			require.NoError(t, err)
			require.JSONEq(t, `{"id":1}`, string(args))
			require.Equal(t, "s", p.RequestState)
			require.Equal(t, responses, p.InputResponses)
		})
	}

	var nilErr *InputRequiredError
	_, err := nilErr.params(nil)
	require.Error(t, err, "nil 检查点不可续接")
	_, err = pending(&mcp.ElicitParams{}).params(mcp.InputResponseMap{})
	require.Error(t, err, "回应数量须与请求一致")
}

func TestInputRequiredErrorChain(t *testing.T) {
	e := &InputRequiredError{Name: "review"}
	require.Contains(t, e.Error(), "review")
	require.Equal(t, agentErrors.CodeToolExecution, agentErrors.GetCode(e.Unwrap()))

	_, err := e.Continue(context.Background(), nil, nil)
	require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err), "会话为 nil 时拒绝续接")
}
