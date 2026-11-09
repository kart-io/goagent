// Package a2a 将官方 A2A JSON-RPC/SSE 映射到统一管理任务服务。
package a2a

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	api "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/management/protocol"
)

type Config struct {
	AgentID          string
	URL              string
	Description      string
	ActorFromRequest func(*http.Request) (mg.Actor, error)
	PollInterval     time.Duration
	MaxInputBytes    int64
}
type Handler struct {
	service *protocol.Service
	cfg     Config
	rpc     http.Handler
	card    http.Handler
}
type actorKey struct{}
type rawPartsKey struct{}
type rawPart struct {
	Data json.RawMessage `json:"data"`
}

func New(s *mg.Service, c Config) (*Handler, error) {
	u, err := url.Parse(c.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || c.AgentID == "" || c.ActorFromRequest == nil {
		return nil, agentErrors.ErrManagementInvalid
	}
	if c.MaxInputBytes == 0 {
		c.MaxInputBytes = 1 << 20
	}
	if c.MaxInputBytes < 1 || c.MaxInputBytes > 1<<20 {
		return nil, agentErrors.ErrManagementInvalid
	}
	p, err := protocol.New(s, c.PollInterval)
	if err != nil {
		return nil, err
	}
	h := &Handler{service: p, cfg: c}
	h.rpc = a2asrv.NewJSONRPCHandler(h, a2asrv.WithTransportKeepAlive(15*time.Second))
	h.card = a2asrv.NewAgentCardHandler(a2asrv.AgentCardProducerFn(h.agentCard))
	return h, nil
}

var _ a2asrv.RequestHandler = (*Handler)(nil)

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a, err := h.cfg.ActorFromRequest(r)
	if err != nil || mg.ValidateActor(a) != nil {
		http.Error(w, "需要认证", http.StatusUnauthorized)
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), actorKey{}, a))
	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/.well-known/agent-card.json") {
		h.card.ServeHTTP(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxInputBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "请求体超出限制或读取失败", http.StatusRequestEntityTooLarge)
		return
	}
	// SDK 的 Data.Value 经 any 解码会变成 float64。保留业务原文，不能用已舍入的值作幂等哈希。
	if _, err = mg.CanonicalJSON(raw); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.Encode(w, map[string]any{"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": -32700, "message": "无效 JSON 请求"}})
		return
	}
	var envelope struct {
		Params struct {
			Message struct {
				Parts []rawPart `json:"parts"`
			} `json:"message"`
		} `json:"params"`
	}
	if json.Unmarshal(raw, &envelope) == nil {
		r = r.WithContext(context.WithValue(r.Context(), rawPartsKey{}, envelope.Params.Message.Parts))
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	h.rpc.ServeHTTP(w, r)
}

func actor(ctx context.Context, tenant string) (mg.Actor, error) {
	a, _ := ctx.Value(actorKey{}).(mg.Actor)
	if mg.ValidateActor(a) != nil {
		return a, api.ErrUnauthenticated
	}
	if tenant != "" && tenant != a.TenantID {
		return a, api.ErrUnauthorized
	}
	return a, nil
}
func (h *Handler) agentCard(ctx context.Context) (*api.AgentCard, error) {
	a, err := actor(ctx, "")
	if err != nil {
		return nil, err
	}
	ag, err := h.service.Management.GetAgent(ctx, a, h.cfg.AgentID)
	if err != nil {
		return nil, translate(err)
	}
	if !ag.Enabled || ag.PublishedVersion == "" {
		return nil, api.ErrUnsupportedOperation
	}
	return &api.AgentCard{Name: ag.Name, Description: h.cfg.Description, Version: ag.PublishedVersion,
		SupportedInterfaces: []*api.AgentInterface{api.NewAgentInterface(h.cfg.URL, api.TransportProtocolJSONRPC)},
		Capabilities:        api.AgentCapabilities{Streaming: true}, DefaultInputModes: []string{"text/plain", "application/json"}, DefaultOutputModes: []string{"application/json"},
		Skills: []api.AgentSkill{{ID: ag.ID, Name: ag.Name, Description: h.cfg.Description, Tags: []string{"goagent"}}}}, nil
}

func translate(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, agentErrors.ErrManagementUnauthenticated):
		return api.ErrUnauthenticated
	case errors.Is(err, agentErrors.ErrManagementForbidden):
		return api.ErrUnauthorized
	case errors.Is(err, agentErrors.ErrManagementNotFound):
		return api.ErrTaskNotFound
	case errors.Is(err, agentErrors.ErrManagementInvalid):
		return api.ErrInvalidParams
	case errors.Is(err, agentErrors.ErrManagementConflict), errors.Is(err, agentErrors.ErrManagementApprovalRequired), errors.Is(err, agentErrors.ErrManagementUnsupported):
		return api.ErrUnsupportedOperation
	default:
		return api.ErrInternalError
	}
}

func taskState(s mg.TaskState) api.TaskState {
	switch s {
	case mg.Queued:
		return api.TaskStateSubmitted
	case mg.Running, mg.Canceling:
		return api.TaskStateWorking
	case mg.WaitingInput, mg.WaitingApproval, mg.Attention:
		return api.TaskStateInputRequired
	case mg.Succeeded:
		return api.TaskStateCompleted
	case mg.Canceled:
		return api.TaskStateCanceled
	default:
		return api.TaskStateFailed
	}
}

func task(t mg.Task) *api.Task {
	v := protocol.View(t)
	result := &api.Task{ID: api.TaskID(t.ID), ContextID: t.SessionID, Status: api.TaskStatus{State: taskState(t.State), Timestamp: t.FinishedAt}, Metadata: map[string]any{"goagent": v}}
	if protocol.Paused(t) && !t.State.Terminal() {
		result.Status.Message = api.NewMessageForTask(api.MessageRoleAgent, result, api.NewDataPart(v))
		result.Status.Message.ID = fmt.Sprintf("%s/wait/%d", t.ID, t.Revision)
	}
	if len(t.Result) > 0 {
		var value any
		if json.Unmarshal(t.Result, &value, json.UseNumber) == nil {
			result.Artifacts = []*api.Artifact{{ID: api.ArtifactID(t.ID + "/result"), Parts: api.ContentParts{api.NewDataPart(value)}}}
		}
	}
	return result
}

// project 补充同一任务的公开审批身份，不在协议层复制审批状态。
func (h *Handler) project(ctx context.Context, a mg.Actor, t mg.Task) (*api.Task, error) {
	v, err := h.service.Describe(ctx, a, t)
	if err != nil {
		return nil, translate(err)
	}
	out := task(t)
	out.Metadata["goagent"] = v
	if out.Status.Message != nil {
		out.Status.Message.Parts = api.ContentParts{api.NewDataPart(v)}
	}
	return out, nil
}

func (h *Handler) GetTask(ctx context.Context, r *api.GetTaskRequest) (*api.Task, error) {
	if r == nil || r.ID == "" {
		return nil, api.ErrInvalidParams
	}
	if r.HistoryLength != nil && *r.HistoryLength != 0 {
		return nil, api.ErrUnsupportedOperation
	}
	a, err := actor(ctx, r.Tenant)
	if err != nil {
		return nil, err
	}
	t, err := h.service.Task(ctx, a, h.cfg.AgentID, string(r.ID))
	if err != nil {
		return nil, translate(err)
	}
	return h.project(ctx, a, t)
}

func (h *Handler) CancelTask(ctx context.Context, r *api.CancelTaskRequest) (*api.Task, error) {
	if r == nil || r.ID == "" {
		return nil, api.ErrInvalidParams
	}
	a, err := actor(ctx, r.Tenant)
	if err != nil {
		return nil, err
	}
	t, err := h.service.Task(ctx, a, h.cfg.AgentID, string(r.ID))
	if err != nil {
		return nil, translate(err)
	}
	if t.State == mg.Canceled {
		return h.project(ctx, a, t)
	}
	if t.State.Terminal() {
		return nil, api.ErrTaskNotCancelable
	}
	t, err = h.service.Management.CancelTask(ctx, a, mg.CancelTaskRequest{TaskID: t.ID, IdempotencyKey: protocol.Key(a, h.cfg.AgentID, "cancel/"+uuid.NewString()), ExpectedRevision: t.Revision, Reason: "A2A 请求取消"})
	if err != nil {
		return nil, translate(err)
	}
	// 取消接纳不等于生产者退出；返回 canceled 前等待持久状态确认。
	if !t.State.Terminal() {
		t, err = h.service.Wait(ctx, a, h.cfg.AgentID, t.ID)
	}
	if err != nil {
		return nil, translate(err)
	}
	if t.State != mg.Canceled {
		return nil, api.ErrTaskNotCancelable
	}
	return h.project(ctx, a, t)
}

func messageInput(ctx context.Context, m *api.Message) (json.RawMessage, error) {
	if len(m.Parts) != 1 || m.Parts[0] == nil {
		return nil, api.ErrUnsupportedContentType
	}
	switch c := m.Parts[0].Content.(type) {
	case api.Text:
		if m.Parts[0].MediaType != "" && m.Parts[0].MediaType != "text/plain" {
			return nil, api.ErrUnsupportedContentType
		}
		return json.Marshal(string(c))
	case api.Data:
		if m.Parts[0].MediaType != "" && m.Parts[0].MediaType != "application/json" {
			return nil, api.ErrUnsupportedContentType
		}
		if parts, ok := ctx.Value(rawPartsKey{}).([]rawPart); ok && len(parts) == 1 && len(parts[0].Data) > 0 {
			return mg.CanonicalJSON(parts[0].Data)
		}
		return json.Marshal(c.Value)
	default:
		return nil, api.ErrUnsupportedContentType
	}
}
func (h *Handler) send(ctx context.Context, r *api.SendMessageRequest) (mg.Actor, mg.Task, error) {
	if r == nil || r.Message == nil || r.Message.ID == "" || r.Message.Role != api.MessageRoleUser {
		return mg.Actor{}, mg.Task{}, api.ErrInvalidParams
	}
	a, err := actor(ctx, r.Tenant)
	if err != nil {
		return a, mg.Task{}, err
	}
	if r.Config != nil {
		if r.Config.PushConfig != nil {
			return a, mg.Task{}, api.ErrPushNotificationNotSupported
		}
		if r.Config.HistoryLength != nil && *r.Config.HistoryLength != 0 {
			return a, mg.Task{}, api.ErrUnsupportedOperation
		}
		if len(r.Config.AcceptedOutputModes) > 0 && !slices.Contains(r.Config.AcceptedOutputModes, "application/json") {
			return a, mg.Task{}, api.ErrUnsupportedContentType
		}
	}
	m := r.Message
	if len(m.Extensions) > 0 || len(m.ReferenceTasks) > 0 {
		return a, mg.Task{}, api.ErrUnsupportedOperation
	}
	input, err := messageInput(ctx, m)
	if err != nil {
		return a, mg.Task{}, err
	}
	if m.TaskID == "" {
		t, err := h.service.Start(ctx, a, protocol.StartRequest{AgentID: h.cfg.AgentID, SessionID: m.ContextID, MessageID: m.ID, Input: input})
		return a, t, translate(err)
	}
	t, err := h.service.Task(ctx, a, h.cfg.AgentID, string(m.TaskID))
	if err != nil {
		return a, t, translate(err)
	}
	if m.ContextID != "" && m.ContextID != t.SessionID {
		return a, t, api.ErrInvalidParams
	}
	var response protocol.Response
	if json.Unmarshal(input, &response, json.Strict) != nil {
		return a, t, api.ErrInvalidParams
	}
	if response.MessageID != "" && response.MessageID != m.ID {
		return a, t, api.ErrInvalidParams
	}
	response.MessageID = m.ID
	command, err := h.service.Respond(ctx, a, h.cfg.AgentID, t.ID, response)
	if err != nil {
		return a, t, translate(err)
	}
	if err = h.service.WaitCommand(ctx, a, command); err != nil {
		return a, t, translate(err)
	}
	t, err = h.service.Task(ctx, a, h.cfg.AgentID, t.ID)
	return a, t, translate(err)
}

func (h *Handler) SendMessage(ctx context.Context, r *api.SendMessageRequest) (api.SendMessageResult, error) {
	a, t, err := h.send(ctx, r)
	if err != nil {
		return nil, err
	}
	if r.Config == nil || !r.Config.ReturnImmediately {
		t, err = h.service.Wait(ctx, a, h.cfg.AgentID, t.ID)
	}
	if err != nil {
		return nil, translate(err)
	}
	return h.project(ctx, a, t)
}
func (h *Handler) stream(ctx context.Context, a mg.Actor, id string) iter.Seq2[api.Event, error] {
	return func(yield func(api.Event, error) bool) {
		first := true
		for t, err := range h.service.Watch(ctx, a, h.cfg.AgentID, id) {
			if err != nil {
				yield(nil, translate(err))
				return
			}
			out, err := h.project(ctx, a, t)
			if err != nil {
				yield(nil, err)
				return
			}
			if first {
				if !yield(out, nil) {
					return
				}
				first = false
				continue
			}
			for _, art := range out.Artifacts {
				if !yield(&api.TaskArtifactUpdateEvent{TaskID: out.ID, ContextID: out.ContextID, Artifact: art, LastChunk: true}, nil) {
					return
				}
			}
			if !yield(&api.TaskStatusUpdateEvent{TaskID: out.ID, ContextID: out.ContextID, Status: out.Status, Metadata: out.Metadata}, nil) {
				return
			}
		}
	}
}
func (h *Handler) SendStreamingMessage(ctx context.Context, r *api.SendMessageRequest) iter.Seq2[api.Event, error] {
	return func(yield func(api.Event, error) bool) {
		a, t, err := h.send(ctx, r)
		if err != nil {
			yield(nil, err)
			return
		}
		h.stream(ctx, a, t.ID)(yield)
	}
}
func (h *Handler) SubscribeToTask(ctx context.Context, r *api.SubscribeToTaskRequest) iter.Seq2[api.Event, error] {
	return func(yield func(api.Event, error) bool) {
		if r == nil || r.ID == "" {
			yield(nil, api.ErrInvalidParams)
			return
		}
		a, err := actor(ctx, r.Tenant)
		if err != nil {
			yield(nil, err)
			return
		}
		t, err := h.service.Task(ctx, a, h.cfg.AgentID, string(r.ID))
		if err != nil {
			yield(nil, translate(err))
			return
		}
		if t.State.Terminal() {
			yield(nil, api.ErrUnsupportedOperation)
			return
		}
		h.stream(ctx, a, string(r.ID))(yield)
	}
}

// ListTasks 需要按状态更新时间进行稳定分页；管理存储当前没有该排序契约。
func (h *Handler) ListTasks(context.Context, *api.ListTasksRequest) (*api.ListTasksResponse, error) {
	return nil, api.ErrUnsupportedOperation
}
func (h *Handler) GetTaskPushConfig(context.Context, *api.GetTaskPushConfigRequest) (*api.PushConfig, error) {
	return nil, api.ErrPushNotificationNotSupported
}
func (h *Handler) ListTaskPushConfigs(context.Context, *api.ListTaskPushConfigRequest) (*api.ListTaskPushConfigResponse, error) {
	return nil, api.ErrPushNotificationNotSupported
}
func (h *Handler) CreateTaskPushConfig(context.Context, *api.PushConfig) (*api.PushConfig, error) {
	return nil, api.ErrPushNotificationNotSupported
}
func (h *Handler) DeleteTaskPushConfig(context.Context, *api.DeleteTaskPushConfigRequest) error {
	return api.ErrPushNotificationNotSupported
}
func (h *Handler) GetExtendedAgentCard(context.Context, *api.GetExtendedAgentCardRequest) (*api.AgentCard, error) {
	return nil, api.ErrExtendedCardNotConfigured
}
