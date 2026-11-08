// Package httpapi 提供管理 API，不持有请求级 Agent 执行或自动监听端口。
package httpapi

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ActorFromRequest func(*http.Request) (mg.Actor, error)
	MaxInputBytes    int64
}
type Handler struct {
	s   *mg.Service
	cfg Config
	mux *http.ServeMux
}

var Routes = []string{"POST /v1/agents", "GET /v1/agents", "GET /v1/agents/{id}", "POST /v1/agents/{id}/versions", "GET /v1/agents/{id}/versions", "POST /v1/agents/{id}/publish", "POST /v1/agents/{id}/enable", "POST /v1/agents/{id}/disable", "POST /v1/sessions", "GET /v1/sessions", "GET /v1/sessions/{id}", "GET /v1/sessions/{id}/history", "POST /v1/sessions/{id}/fork", "POST /v1/tasks", "GET /v1/tasks", "GET /v1/tasks/{id}", "POST /v1/tasks/{id}/cancel", "POST /v1/tasks/{id}/resume", "GET /v1/approvals", "GET /v1/approvals/{id}", "POST /v1/approvals/{id}/decision", "GET /v1/runs", "GET /v1/runs/{id}", "GET /v1/commands/{id}", "GET /v1/runs/summary"}

func New(s *mg.Service, c Config) (*Handler, error) {
	if s == nil || c.ActorFromRequest == nil {
		return nil, agentErrors.ErrManagementInvalid
	}
	if c.MaxInputBytes == 0 {
		c.MaxInputBytes = 1 << 20
	}
	if c.MaxInputBytes < 1 || c.MaxInputBytes > 1<<20 {
		return nil, agentErrors.ErrManagementInvalid
	}
	h := &Handler{s: s, cfg: c, mux: http.NewServeMux()}
	h.register()
	return h, nil
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Request-ID", uuid.NewString())
	h.mux.ServeHTTP(w, r)
}
func decode(w http.ResponseWriter, r *http.Request, limit int64, dst any) error {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		return err
	}
	canonical, err := mg.CanonicalJSON(raw)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(canonical, dst, json.Strict, json.UseNumber); err != nil {
		return agentErrors.ErrManagementInvalid
	}
	return nil
}
func public(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var value any
	if err = json.Unmarshal(raw, &value, json.UseNumber); err != nil {
		return nil, err
	}
	var clean func(any)
	clean = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for _, key := range []string{"execution_key", "key", "interrupt_id", "log_id", "source_hash", "input_hash", "operations"} {
				delete(x, key)
			}
			// 只投影管理结构，业务 input/result/config/data 的同名字段必须原样保留。
			for _, field := range []string{"items", "task", "waits", "wait"} {
				if child, ok := x[field]; ok {
					clean(child)
				}
			}
		case []any:
			for _, v := range x {
				clean(v)
			}
		}
	}
	clean(value)
	return value, nil
}
func (h *Handler) write(w http.ResponseWriter, status int, v any) {
	value, err := public(v)
	if err != nil {
		h.problem(w, agentErrors.ErrManagementUnavailable)
		return
	}
	raw, err := json.Marshal(value)
	if err != nil {
		h.problem(w, agentErrors.ErrManagementUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}
func (h *Handler) problem(w http.ResponseWriter, err error) {
	status := 503
	code := "unavailable"
	message := "依赖暂不可用"
	var max *http.MaxBytesError
	switch {
	case errors.As(err, &max):
		status = 413
		code = "input_too_large"
		message = "请求体超出限制"
	case errors.Is(err, agentErrors.ErrManagementUnauthenticated):
		status = 401
		code = "unauthenticated"
		message = "需要认证"
	case errors.Is(err, agentErrors.ErrManagementForbidden):
		status = 403
		code = "forbidden"
		message = "没有操作权限"
	case errors.Is(err, agentErrors.ErrManagementNotFound):
		status = 404
		code = "not_found"
		message = "资源不存在"
	case errors.Is(err, agentErrors.ErrManagementApprovalRequired):
		status = 409
		code = "approval_decision_required"
		message = "审批等待必须通过审批决定处理"
	case errors.Is(err, agentErrors.ErrManagementConflict):
		status = 409
		code = "conflict"
		message = "修订号、状态或幂等内容冲突"
	case errors.Is(err, agentErrors.ErrManagementUnsupported):
		status = 422
		code = "unsupported_capability"
		message = "不支持所需能力"
	case errors.Is(err, agentErrors.ErrManagementBusy):
		status = 429
		code = "busy"
		message = "接纳容量已满"
	case errors.Is(err, agentErrors.ErrManagementInvalid):
		status = 400
		code = "invalid_request"
		message = "请求参数无效"
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.Encode(w, map[string]any{"error": map[string]any{"code": code, "message": message, "request_id": w.Header().Get("X-Request-ID")}})
}
func post[T any](h *Handler, pattern string, status int, call func(context.Context, mg.Actor, *http.Request, T) (any, error)) {
	h.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		a, err := h.cfg.ActorFromRequest(r)
		if err == nil {
			err = mg.ValidateActor(a)
		}
		if err != nil {
			h.problem(w, agentErrors.ErrManagementUnauthenticated)
			return
		}
		if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
			h.problem(w, agentErrors.ErrManagementInvalid)
			return
		}
		var input T
		if err = decode(w, r, h.cfg.MaxInputBytes, &input); err != nil {
			h.problem(w, err)
			return
		}
		out, err := call(r.Context(), a, r, input)
		if err != nil {
			h.problem(w, err)
			return
		}
		raw, _ := json.Marshal(out)
		var meta struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(raw, &meta)
		if meta.ID != "" {
			switch status {
			case 202:
				if strings.HasSuffix(pattern, "/resume") {
					w.Header().Set("Location", "/v1/commands/"+meta.ID)
				} else {
					w.Header().Set("Location", "/v1/tasks/"+meta.ID)
					if strings.Contains(pattern, "sessions") {
						w.Header().Set("Location", "/v1/sessions/"+meta.ID)
					}
				}
			case 201:
				w.Header().Set("Location", strings.TrimSuffix(r.URL.Path, "/")+"/"+meta.ID)
			}
		}
		h.write(w, status, out)
	})
}
func query(r *http.Request) (mg.Query, error) {
	values := r.URL.Query()
	q := mg.Query{AgentID: values.Get("agent_id"), SessionID: values.Get("session_id"), TaskID: values.Get("task_id"), Cursor: values.Get("cursor"), Statuses: values["status"]}
	var err error
	if values.Get("limit") != "" {
		q.Limit, err = strconv.Atoi(values.Get("limit"))
		if err != nil {
			return q, agentErrors.ErrManagementInvalid
		}
	}
	if values.Get("created_from") != "" {
		q.CreatedFrom, err = time.Parse(time.RFC3339Nano, values.Get("created_from"))
		if err != nil {
			return q, agentErrors.ErrManagementInvalid
		}
	}
	if values.Get("created_to") != "" {
		q.CreatedTo, err = time.Parse(time.RFC3339Nano, values.Get("created_to"))
		if err != nil {
			return q, agentErrors.ErrManagementInvalid
		}
	}
	return q, nil
}
func (h *Handler) get(pattern string, fn func(context.Context, mg.Actor, *http.Request, mg.Query) (any, error)) {
	h.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		a, err := h.cfg.ActorFromRequest(r)
		if err == nil {
			err = mg.ValidateActor(a)
		}
		if err != nil {
			h.problem(w, agentErrors.ErrManagementUnauthenticated)
			return
		}
		q, err := query(r)
		if err != nil {
			h.problem(w, err)
			return
		}
		out, err := fn(r.Context(), a, r, q)
		if err != nil {
			h.problem(w, err)
			return
		}
		h.write(w, 200, out)
	})
}
func (h *Handler) register() {
	post(h, Routes[0], 201, func(c context.Context, a mg.Actor, r *http.Request, in mg.RegisterAgentRequest) (any, error) {
		in.IdempotencyKey = r.Header.Get("Idempotency-Key")
		return h.s.RegisterAgent(c, a, in)
	})
	h.get(Routes[1], func(c context.Context, a mg.Actor, r *http.Request, q mg.Query) (any, error) {
		return h.s.ListAgents(c, a, q)
	})
	h.get(Routes[2], func(c context.Context, a mg.Actor, r *http.Request, q mg.Query) (any, error) {
		return h.s.GetAgent(c, a, r.PathValue("id"))
	})
	post(h, Routes[3], 201, func(c context.Context, a mg.Actor, r *http.Request, in mg.SaveVersionRequest) (any, error) {
		in.IdempotencyKey = r.Header.Get("Idempotency-Key")
		in.AgentID = r.PathValue("id")
		return h.s.SaveVersion(c, a, in)
	})
	h.get(Routes[4], func(c context.Context, a mg.Actor, r *http.Request, q mg.Query) (any, error) {
		q.AgentID = r.PathValue("id")
		return h.s.ListVersions(c, a, q)
	})
	post(h, Routes[5], 200, func(c context.Context, a mg.Actor, r *http.Request, in mg.PublishRequest) (any, error) {
		in.IdempotencyKey = r.Header.Get("Idempotency-Key")
		in.AgentID = r.PathValue("id")
		return h.s.PublishVersion(c, a, in)
	})
	for _, path := range Routes[6:8] {
		post(h, path, 200, func(c context.Context, a mg.Actor, r *http.Request, in mg.SetEnabledRequest) (any, error) {
			in.IdempotencyKey = r.Header.Get("Idempotency-Key")
			in.AgentID = r.PathValue("id")
			in.Enabled = strings.HasSuffix(r.URL.Path, "/enable")
			return h.s.SetAgentEnabled(c, a, in)
		})
	}
	post(h, Routes[8], 202, func(c context.Context, a mg.Actor, r *http.Request, in mg.CreateSessionRequest) (any, error) {
		in.IdempotencyKey = r.Header.Get("Idempotency-Key")
		return h.s.CreateSession(c, a, in)
	})
	h.get(Routes[9], func(c context.Context, a mg.Actor, r *http.Request, q mg.Query) (any, error) {
		return h.s.ListSessions(c, a, q)
	})
	h.get(Routes[10], func(c context.Context, a mg.Actor, r *http.Request, q mg.Query) (any, error) {
		return h.s.GetSession(c, a, r.PathValue("id"))
	})
	h.get(Routes[11], func(c context.Context, a mg.Actor, r *http.Request, q mg.Query) (any, error) {
		after := int64(0)
		var err error
		if r.URL.Query().Get("after_seq") != "" {
			after, err = strconv.ParseInt(r.URL.Query().Get("after_seq"), 10, 64)
			if err != nil {
				return nil, agentErrors.ErrManagementInvalid
			}
		}
		return h.s.History(c, a, mg.HistoryRequest{SessionID: r.PathValue("id"), AfterSeq: after, Limit: q.Limit, Types: r.URL.Query()["type"]})
	})
	post(h, Routes[12], 202, func(c context.Context, a mg.Actor, r *http.Request, in mg.ForkSessionRequest) (any, error) {
		in.IdempotencyKey = r.Header.Get("Idempotency-Key")
		in.SessionID = r.PathValue("id")
		return h.s.ForkSession(c, a, in)
	})
	post(h, Routes[13], 202, func(c context.Context, a mg.Actor, r *http.Request, in mg.SubmitTaskRequest) (any, error) {
		in.IdempotencyKey = r.Header.Get("Idempotency-Key")
		return h.s.SubmitTask(c, a, in)
	})
	h.get(Routes[14], func(c context.Context, a mg.Actor, r *http.Request, q mg.Query) (any, error) {
		return h.s.ListTasks(c, a, q)
	})
	h.get(Routes[15], func(c context.Context, a mg.Actor, r *http.Request, q mg.Query) (any, error) {
		return h.s.GetTask(c, a, r.PathValue("id"))
	})
	post(h, Routes[16], 202, func(c context.Context, a mg.Actor, r *http.Request, in mg.CancelTaskRequest) (any, error) {
		in.IdempotencyKey = r.Header.Get("Idempotency-Key")
		in.TaskID = r.PathValue("id")
		return h.s.CancelTask(c, a, in)
	})
	post(h, Routes[17], 202, func(c context.Context, a mg.Actor, r *http.Request, in mg.ResumeTaskRequest) (any, error) {
		in.IdempotencyKey = r.Header.Get("Idempotency-Key")
		in.TaskID = r.PathValue("id")
		return h.s.ResumeTask(c, a, in)
	})
	h.get(Routes[18], func(c context.Context, a mg.Actor, r *http.Request, q mg.Query) (any, error) {
		return h.s.ListApprovals(c, a, q)
	})
	h.get(Routes[19], func(c context.Context, a mg.Actor, r *http.Request, q mg.Query) (any, error) {
		return h.s.GetApproval(c, a, r.PathValue("id"))
	})
	post(h, Routes[20], 200, func(c context.Context, a mg.Actor, r *http.Request, in mg.DecisionRequest) (any, error) {
		in.IdempotencyKey = r.Header.Get("Idempotency-Key")
		in.ApprovalID = r.PathValue("id")
		return h.s.DecideApproval(c, a, in)
	})
	h.get(Routes[24], func(c context.Context, a mg.Actor, r *http.Request, q mg.Query) (any, error) {
		return h.s.SummarizeRuns(c, a, q)
	})
	h.get(Routes[21], func(c context.Context, a mg.Actor, r *http.Request, q mg.Query) (any, error) {
		return h.s.ListRuns(c, a, q)
	})
	h.get(Routes[22], func(c context.Context, a mg.Actor, r *http.Request, q mg.Query) (any, error) {
		return h.s.GetRun(c, a, r.PathValue("id"))
	})
	h.get(Routes[23], func(c context.Context, a mg.Actor, r *http.Request, q mg.Query) (any, error) {
		command, err := h.s.GetCommand(c, a, r.PathValue("id"))
		command.Input = nil
		return command, err
	})
}
