// Package mcpserver 将统一管理服务发布为官方 MCP 工具服务。
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/management/protocol"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Agent struct{ ID, Name, Description string }
type Config struct {
	Agents           []Agent
	ActorFromRequest func(context.Context, *mcp.CallToolRequest) (mg.Actor, error)
	PollInterval     time.Duration
}
type server struct {
	service *protocol.Service
	cfg     Config
	agents  map[string]bool
}

// NewHTTPHandler 使用支持 MCP 2026-07-28 多轮输入的无状态 HTTP transport。
// 认证中间件仍由宿主装配；每次工具调用均通过 Config.ActorFromRequest 获取身份。
func NewHTTPHandler(s *mg.Service, c Config) (http.Handler, error) {
	srv, err := New(s, c)
	if err != nil {
		return nil, err
	}
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, PropagateRequestCancellation: true, MaxRequestBodyBytes: 1 << 20}), nil
}

// New 不启动 Worker 或监听端口；同一个管理服务可同时交给 HTTP/A2A 适配器。
func New(s *mg.Service, c Config) (*mcp.Server, error) {
	if c.ActorFromRequest == nil || len(c.Agents) == 0 {
		return nil, agentErrors.ErrManagementInvalid
	}
	p, err := protocol.New(s, c.PollInterval)
	if err != nil {
		return nil, err
	}
	h := &server{service: p, cfg: c, agents: map[string]bool{}}
	names := map[string]bool{"task_get": true, "task_wait": true, "task_cancel": true, "task_respond": true}
	validName := regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	for _, a := range c.Agents {
		if a.ID == "" || !validName.MatchString(a.Name) || names[a.Name] || h.agents[a.ID] {
			return nil, agentErrors.ErrManagementInvalid
		}
		names[a.Name] = true
		h.agents[a.ID] = true
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "goagent", Version: "1.0.0"}, nil)
	for _, a := range c.Agents {
		srv.AddTool(&mcp.Tool{Name: a.Name, Description: a.Description, InputSchema: schema(map[string]any{"message_id": textSchema(), "session_id": textSchema(), "input": map[string]any{}}, "message_id", "input")}, h.handle(func(ctx context.Context, actor mg.Actor, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var in struct {
				MessageID string          `json:"message_id"`
				SessionID string          `json:"session_id,omitempty"`
				Input     json.RawMessage `json:"input"`
			}
			if err := decode(req, &in); err != nil {
				return nil, err
			}
			t, err := p.Start(ctx, actor, protocol.StartRequest{AgentID: a.ID, MessageID: in.MessageID, SessionID: in.SessionID, Input: in.Input})
			if err != nil {
				return nil, err
			}
			return h.result(ctx, actor, t)
		}))
	}
	for _, name := range []string{"task_get", "task_wait"} {
		srv.AddTool(&mcp.Tool{Name: name, Description: map[string]string{"task_get": "查询共同任务状态、输入和审批请求", "task_wait": "等待任务完成或通过人工输入与审批继续运行"}[name], InputSchema: schema(map[string]any{"task_id": textSchema()}, "task_id")}, h.handle(func(ctx context.Context, a mg.Actor, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var in taskInput
			if err := decode(req, &in); err != nil {
				return nil, err
			}
			t, err := h.get(ctx, a, in.TaskID)
			if err != nil {
				return nil, err
			}
			if name == "task_get" {
				return h.result(ctx, a, t)
			}
			return h.wait(ctx, a, req, t)
		}))
	}
	srv.AddTool(&mcp.Tool{Name: "task_cancel", Description: "请求取消任务；canceling 表示生产者尚未退出", InputSchema: schema(map[string]any{"task_id": textSchema(), "message_id": textSchema(), "expected_revision": integerSchema(), "reason": textSchema()}, "task_id", "message_id", "expected_revision")}, h.handle(func(ctx context.Context, a mg.Actor, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var in struct {
			TaskID           string `json:"task_id"`
			MessageID        string `json:"message_id"`
			ExpectedRevision int64  `json:"expected_revision"`
			Reason           string `json:"reason,omitempty"`
		}
		if err := decode(req, &in); err != nil {
			return nil, err
		}
		t, err := h.get(ctx, a, in.TaskID)
		if err != nil {
			return nil, err
		}
		if in.MessageID == "" || len(in.MessageID) > 256 {
			return nil, agentErrors.ErrManagementInvalid
		}
		t, err = p.Management.CancelTask(ctx, a, mg.CancelTaskRequest{TaskID: t.ID, IdempotencyKey: protocol.Key(a, t.AgentID, in.MessageID), ExpectedRevision: in.ExpectedRevision, Reason: in.Reason})
		if err != nil {
			return nil, err
		}
		return h.result(ctx, a, t)
	}))
	srv.AddTool(&mcp.Tool{Name: "task_respond", Description: "按修订提交输入或审批决定，返回持久命令身份", InputSchema: schema(map[string]any{"task_id": textSchema(), "response": map[string]any{"type": "object"}}, "task_id", "response")}, h.handle(func(ctx context.Context, a mg.Actor, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var in struct {
			TaskID   string            `json:"task_id"`
			Response protocol.Response `json:"response"`
		}
		if err := decode(req, &in); err != nil {
			return nil, err
		}
		t, err := h.get(ctx, a, in.TaskID)
		if err != nil {
			return nil, err
		}
		id, err := p.Respond(ctx, a, t.AgentID, t.ID, in.Response)
		if err != nil {
			return nil, err
		}
		return complete(map[string]any{"task_id": t.ID, "command_id": id})
	}))
	return srv, nil
}

type taskInput struct {
	TaskID string `json:"task_id"`
}
type action func(context.Context, mg.Actor, *mcp.CallToolRequest) (*mcp.CallToolResult, error)

func (h *server) handle(fn action) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		a, err := h.cfg.ActorFromRequest(ctx, req)
		if err != nil || mg.ValidateActor(a) != nil {
			return toolError(agentErrors.ErrManagementUnauthenticated), nil
		}
		out, err := h.invoke(ctx, a, req, fn)
		if err != nil {
			var rpc *jsonrpc.Error
			if errors.As(err, &rpc) {
				return nil, rpc
			}
			return toolError(err), nil
		}
		return out, nil
	}
}

// invoke 使用官方反向调用桥接旧协议；SDK 内置兼容层只重入一次，无法处理连续等待。
// 新协议把续接状态交还客户端，旧协议在同一请求中有界等待宿主的人工处理器。
func (h *server) invoke(ctx context.Context, a mg.Actor, req *mcp.CallToolRequest, fn action) (*mcp.CallToolResult, error) {
	for round := 0; ; round++ {
		out, err := fn(ctx, a, req)
		if err != nil || out == nil || out.InputRequests == nil || req.ProtocolVersion() >= "2026-07-28" {
			return out, err
		}
		if round >= 64 {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "人工输入轮数超限，请通过 task_get 查询并继续任务")
		}
		responses := mcp.InputResponseMap{}
		for id, request := range out.InputRequests {
			params, ok := request.(*mcp.ElicitParams)
			if !ok {
				return nil, agentErrors.ErrManagementUnsupported
			}
			answer, err := req.Session.Elicit(ctx, params)
			if err != nil {
				return nil, err
			}
			responses[id] = answer
		}
		params := *req.Params
		params.RequestState, params.InputResponses = out.RequestState, responses
		next := *req
		next.Params = &params
		req = &next
	}
}
func decode(req *mcp.CallToolRequest, out any) error {
	if req == nil || req.Params == nil || len(req.Params.Arguments) > 1<<20 {
		return agentErrors.ErrManagementInvalid
	}
	if err := json.Unmarshal(req.Params.Arguments, out, json.Strict, json.UseNumber); err != nil {
		return agentErrors.ErrManagementInvalid
	}
	return nil
}
func (h *server) get(ctx context.Context, a mg.Actor, id string) (mg.Task, error) {
	t, err := h.service.Management.GetTask(ctx, a, id)
	if err != nil {
		return t, err
	}
	if !h.agents[t.AgentID] {
		return mg.Task{}, agentErrors.ErrManagementNotFound
	}
	return t, nil
}
func (h *server) result(ctx context.Context, a mg.Actor, t mg.Task) (*mcp.CallToolResult, error) {
	v, err := h.service.Describe(ctx, a, t)
	if err != nil {
		return nil, err
	}
	return complete(v)
}
func complete(v any) (*mcp.CallToolResult, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{StructuredContent: v, Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}, nil
}
func toolError(err error) *mcp.CallToolResult {
	msg := "依赖暂不可用"
	switch {
	case errors.Is(err, agentErrors.ErrManagementUnauthenticated):
		msg = "需要认证"
	case errors.Is(err, agentErrors.ErrManagementForbidden):
		msg = "没有操作权限"
	case errors.Is(err, agentErrors.ErrManagementNotFound):
		msg = "任务或审批不存在"
	case errors.Is(err, agentErrors.ErrManagementInvalid):
		msg = "请求参数无效"
	case errors.Is(err, agentErrors.ErrManagementConflict):
		msg = "状态、修订或幂等内容冲突"
	case errors.Is(err, agentErrors.ErrManagementApprovalRequired):
		msg = "必须提交审批决定"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		msg = "请求已取消或超时，任务状态请另行查询"
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
}
func textSchema() map[string]any    { return map[string]any{"type": "string"} }
func integerSchema() map[string]any { return map[string]any{"type": "integer"} }
func schema(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

// continuation 只携带定位与 CAS 参数，不作为授权凭据；每次重试重新验证 Actor 和任务归属。
type continuation struct {
	TaskID           string `json:"task_id"`
	WaitID           string `json:"wait_id"`
	Revision         int64  `json:"revision"`
	ApprovalID       string `json:"approval_id,omitempty"`
	ApprovalRevision int64  `json:"approval_revision,omitempty"`
}

func (h *server) wait(ctx context.Context, a mg.Actor, req *mcp.CallToolRequest, t mg.Task) (*mcp.CallToolResult, error) {
	if len(req.Params.InputResponses) > 0 {
		if err := requireForm(req); err != nil {
			return nil, err
		}
		var state continuation
		if json.Unmarshal([]byte(req.Params.RequestState), &state, json.Strict) != nil || state.TaskID != t.ID || state.WaitID == "" || len(req.Params.InputResponses) != 1 {
			return nil, agentErrors.ErrManagementInvalid
		}
		answer, ok := req.Params.InputResponses[state.WaitID].(*mcp.ElicitResult)
		if !ok || answer == nil {
			return nil, agentErrors.ErrManagementInvalid
		}
		if answer.Action == "cancel" || (answer.Action == "decline" && state.ApprovalID == "") {
			return h.result(ctx, a, t)
		}
		response := protocol.Response{MessageID: fmt.Sprintf("mcp-input/%s/%d/%s", t.ID, state.Revision, state.WaitID), ExpectedRevision: state.Revision}
		if state.ApprovalID != "" {
			response.ApprovalID = state.ApprovalID
			response.ApprovalRevision = state.ApprovalRevision
			switch answer.Action {
			case "decline":
				response.Decision = "reject"
			case "accept":
				response.Decision, _ = answer.Content["decision"].(string)
			default:
				return nil, agentErrors.ErrManagementInvalid
			}
			response.Reason, _ = answer.Content["reason"].(string)
		} else {
			if answer.Action != "accept" {
				return nil, agentErrors.ErrManagementInvalid
			}
			value, ok := answer.Content["input"].(string)
			if !ok {
				return nil, agentErrors.ErrManagementInvalid
			}
			raw, err := mg.CanonicalJSON([]byte(value))
			if err != nil {
				return nil, err
			}
			response.WaitID = state.WaitID
			response.Input = raw
		}
		command, err := h.service.Respond(ctx, a, t.AgentID, t.ID, response)
		if err != nil {
			return nil, err
		}
		if err = h.service.WaitCommand(ctx, a, command); err != nil {
			return nil, err
		}
	}
	t, err := h.service.Wait(ctx, a, t.AgentID, t.ID)
	if err != nil {
		return nil, err
	}
	if t.State != mg.WaitingApproval && t.State != mg.WaitingInput {
		return h.result(ctx, a, t)
	}
	v, err := h.service.Describe(ctx, a, t)
	if err != nil {
		return nil, err
	}
	if err := requireForm(req); err != nil {
		return nil, err
	}
	if len(t.Waits) == 0 {
		return nil, agentErrors.ErrManagementConflict
	}
	w := t.Waits[0]
	state := continuation{TaskID: t.ID, WaitID: w.ID, Revision: t.Revision}
	params := &mcp.ElicitParams{Mode: "form", Message: "任务需要补充输入：" + string(w.Info), RequestedSchema: schema(map[string]any{"input": map[string]any{"type": "string", "description": "输入值的 JSON 编码，例如字符串需包含双引号"}}, "input")}
	if w.Kind == "approval" {
		for _, ap := range v.Approvals {
			if ap.WaitID == w.ID {
				state.ApprovalID = ap.ID
				state.ApprovalRevision = ap.Revision
				break
			}
		}
		if state.ApprovalID == "" {
			return nil, agentErrors.ErrManagementConflict
		}
		params.Message = "请决定是否批准任务操作：" + string(w.Info)
		params.RequestedSchema = schema(map[string]any{"decision": map[string]any{"type": "string", "enum": []string{"approve", "reject"}}, "reason": textSchema()}, "decision")
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{InputRequests: mcp.InputRequestMap{w.ID: params}, RequestState: string(raw)}, nil
}

func requireForm(req *mcp.CallToolRequest) error {
	// 新协议能力来自每次请求的 _meta；旧协议由 SDK 回退到初始化能力。
	caps := req.ClientCapabilities()
	if caps == nil || caps.Elicitation == nil || (caps.Elicitation.Form == nil && caps.Elicitation.URL != nil) {
		raw, _ := json.Marshal(mcp.MissingRequiredClientCapabilityData{RequiredCapabilities: &mcp.ClientCapabilities{Elicitation: &mcp.ElicitationCapabilities{Form: &mcp.FormElicitationCapabilities{}}}})
		return &jsonrpc.Error{Code: mcp.CodeMissingRequiredClientCapabilities, Message: "task_wait 需要 form elicitation 能力；可使用 task_get 查询", Data: raw}
	}
	return nil
}
