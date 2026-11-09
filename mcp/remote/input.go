package remote

import (
	"context"
	"fmt"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type InputHandler func(context.Context, *InputRequiredError) (mcp.InputResponseMap, error)

// InputRequiredError 是可序列化的续接检查点；宿主可保存后用重新连接的会话继续。
// 远端是否允许跨 Session 续接仍由远端服务约定；这里不重发不带状态的初始调用。
type InputRequiredError struct {
	Name         string              `json:"name"`
	Arguments    json.RawMessage     `json:"arguments"`
	RequestState string              `json:"request_state"`
	Requests     mcp.InputRequestMap `json:"requests"`
}

func (e *InputRequiredError) Error() string {
	return fmt.Sprintf("MCP 工具 %s 等待宿主输入", e.Name)
}
func (e *InputRequiredError) Unwrap() error {
	return agentErrors.Newf(agentErrors.CodeToolExecution, "MCP 需要输入续接")
}
func pendingCall(r *mcp.CallToolParams, result *mcp.CallToolResult) (*InputRequiredError, error) {
	args, err := json.Marshal(r.Arguments)
	if err != nil {
		return nil, err
	}
	return &InputRequiredError{Name: r.Name, Arguments: args, RequestState: result.RequestState, Requests: result.InputRequests}, nil
}

func (e *InputRequiredError) Continue(ctx context.Context, s *mcp.ClientSession, responses mcp.InputResponseMap) (*mcp.CallToolResult, error) {
	if s == nil {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "MCP 续接参数无效")
	}
	r, err := e.params(responses)
	if err != nil {
		return nil, err
	}
	result, err := s.CallTool(ctx, r)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidOutput, "MCP 返回空结果")
	}
	if result.NeedsInput() {
		next, err := pendingCall(r, result)
		if err != nil {
			return nil, err
		}
		return nil, next
	}
	return result, nil
}

// params 校验输入响应与等待中的请求一一对应，构造携带原业务参数与请求状态的续接调用。
func (e *InputRequiredError) params(responses mcp.InputResponseMap) (*mcp.CallToolParams, error) {
	if e == nil || e.Name == "" || !json.Valid(e.Arguments) || len(responses) != len(e.Requests) {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "MCP 续接参数无效")
	}
	for id, req := range e.Requests {
		response, ok := responses[id]
		if !ok || response == nil {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "MCP 续接缺少输入响应")
		}
		valid := false
		switch req.(type) {
		case *mcp.ElicitParams:
			r, ok := response.(*mcp.ElicitResult)
			valid = ok && r != nil && (r.Action == "accept" || r.Action == "decline" || r.Action == "cancel")
		default:
			valid = legacyResponseMatches(req, response)
		}
		if !valid {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "MCP 输入响应类型不匹配")
		}
	}
	return &mcp.CallToolParams{Name: e.Name, Arguments: e.Arguments, RequestState: e.RequestState, InputResponses: responses}, nil
}

// legacyResponseMatches 保留官方仍支持的 roots/sampling 续接，不能把旧协议响应当成无效类型。
//
//nolint:staticcheck // MCP 2026-07-28 起弃用但仍处于至少十二个月过渡期；宿主显式授权后继续兼容。
func legacyResponseMatches(req mcp.InputRequest, response mcp.InputResponse) bool {
	switch req.(type) {
	case *mcp.ListRootsParams:
		r, ok := response.(*mcp.ListRootsResult)
		return ok && r != nil
	case *mcp.CreateMessageParams:
		r, ok := response.(*mcp.CreateMessageResult)
		return ok && r != nil
	case *mcp.CreateMessageWithToolsParams:
		r, ok := response.(*mcp.CreateMessageWithToolsResult)
		return ok && r != nil
	default:
		return false
	}
}
