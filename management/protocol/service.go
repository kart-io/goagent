// Package protocol 为协议适配器复用管理任务，不维护独立任务状态。
package protocol

import (
	"context"
	"crypto/sha256"
	"fmt"
	"iter"
	"time"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
)

type Service struct {
	Management   *mg.Service
	PollInterval time.Duration
}

func New(s *mg.Service, interval time.Duration) (*Service, error) {
	if s == nil || interval < 0 {
		return nil, agentErrors.ErrManagementInvalid
	}
	if interval == 0 {
		interval = 100 * time.Millisecond
	}
	return &Service{Management: s, PollInterval: interval}, nil
}

// Key 将宿主身份纳入消息幂等键；原始业务输入仍由管理服务校验哈希。
func Key(a mg.Actor, agent, message string) string {
	return fmt.Sprintf("protocol/%x", sha256.Sum256([]byte(a.TenantID+"\x00"+a.Subject+"\x00"+agent+"\x00"+message)))
}

type StartRequest struct {
	AgentID   string          `json:"agent_id"`
	SessionID string          `json:"session_id,omitempty"`
	MessageID string          `json:"message_id"`
	Input     json.RawMessage `json:"input"`
}

func (s *Service) Start(ctx context.Context, a mg.Actor, r StartRequest) (mg.Task, error) {
	if r.AgentID == "" || r.MessageID == "" || len(r.MessageID) > 256 {
		return mg.Task{}, agentErrors.ErrManagementInvalid
	}
	if _, err := mg.CanonicalJSON(r.Input); err != nil {
		return mg.Task{}, err
	}
	key := Key(a, r.AgentID, r.MessageID)
	id := r.SessionID
	if id == "" {
		se, err := s.Management.CreateSession(ctx, a, mg.CreateSessionRequest{IdempotencyKey: key, AgentID: r.AgentID})
		if err != nil {
			return mg.Task{}, err
		}
		id = se.ID
	}
	for {
		se, err := s.Management.GetSession(ctx, a, id)
		if err != nil {
			return mg.Task{}, err
		}
		if se.AgentID != r.AgentID {
			return mg.Task{}, agentErrors.ErrManagementNotFound
		}
		if se.Status == "ready" {
			break
		}
		if se.Status != "preparing" {
			return mg.Task{}, agentErrors.ErrManagementConflict
		}
		if err = s.pause(ctx); err != nil {
			return mg.Task{}, err
		}
	}
	t, err := s.Management.SubmitTask(ctx, a, mg.SubmitTaskRequest{IdempotencyKey: key, SessionID: id, Input: r.Input})
	if err != nil {
		return mg.Task{}, err
	}
	return s.Management.GetTask(ctx, a, t.ID)
}

func (s *Service) Task(ctx context.Context, a mg.Actor, agent, id string) (mg.Task, error) {
	t, err := s.Management.GetTask(ctx, a, id)
	if err == nil && t.AgentID != agent {
		return mg.Task{}, agentErrors.ErrManagementNotFound
	}
	return t, err
}

// Response 同时保留任务修订与审批修订，普通输入不能代替审批决定。
type Response struct {
	MessageID        string          `json:"message_id"`
	ExpectedRevision int64           `json:"expected_revision"`
	WaitID           string          `json:"wait_id,omitempty"`
	Input            json.RawMessage `json:"input,omitempty"`
	ApprovalID       string          `json:"approval_id,omitempty"`
	ApprovalRevision int64           `json:"approval_revision,omitempty"`
	Decision         string          `json:"decision,omitempty"`
	Reason           string          `json:"reason,omitempty"`
}

func (s *Service) Respond(ctx context.Context, a mg.Actor, agent, id string, r Response) (string, error) {
	if r.MessageID == "" || len(r.MessageID) > 256 {
		return "", agentErrors.ErrManagementInvalid
	}
	if _, err := s.Task(ctx, a, agent, id); err != nil {
		return "", err
	}
	key := Key(a, agent, r.MessageID)
	if r.ApprovalID != "" {
		if r.WaitID != "" || len(r.Input) > 0 {
			return "", agentErrors.ErrManagementInvalid
		}
		ap, err := s.Management.GetApproval(ctx, a, r.ApprovalID)
		if err != nil {
			return "", err
		}
		if ap.TaskID != id {
			return "", agentErrors.ErrManagementNotFound
		}
		ap, err = s.Management.DecideApproval(ctx, a, mg.DecisionRequest{IdempotencyKey: key, ApprovalID: ap.ID, ExpectedRevision: r.ApprovalRevision, Decision: r.Decision, Reason: r.Reason})
		return ap.CommandID, err
	}
	if r.Decision != "" || r.ApprovalRevision != 0 {
		return "", agentErrors.ErrManagementInvalid
	}
	c, err := s.Management.ResumeTask(ctx, a, mg.ResumeTaskRequest{IdempotencyKey: key, TaskID: id, ExpectedRevision: r.ExpectedRevision, WaitID: r.WaitID, Input: r.Input})
	return c.ID, err
}

// WaitCommand 等待命令开始应用；running 与 applying 在同一事务中发布，不等待整轮执行结束。
func (s *Service) WaitCommand(ctx context.Context, a mg.Actor, id string) error {
	for {
		c, err := s.Management.GetCommand(ctx, a, id)
		if err != nil {
			return err
		}
		switch c.State {
		case "applying", "applied":
			return nil
		case "pending":
		default:
			return agentErrors.ErrManagementConflict
		}
		if err = s.pause(ctx); err != nil {
			return err
		}
	}
}

func Paused(t mg.Task) bool {
	return t.State.Terminal() || t.State == mg.WaitingInput || t.State == mg.WaitingApproval || t.State == mg.Attention
}

// Watch 每次读取都经过授权；只观测持久状态，不因断开订阅取消任务。
// 重连从当前完整快照开始，快于轮询的中间修订可能合并。
func (s *Service) Watch(ctx context.Context, a mg.Actor, agent, id string) iter.Seq2[mg.Task, error] {
	return func(yield func(mg.Task, error) bool) {
		var revision int64
		for {
			t, err := s.Task(ctx, a, agent, id)
			if err != nil {
				yield(mg.Task{}, err)
				return
			}
			if t.Revision != revision {
				if !yield(t, nil) {
					return
				}
				revision = t.Revision
			}
			if Paused(t) {
				return
			}
			if err = s.pause(ctx); err != nil {
				yield(mg.Task{}, err)
				return
			}
		}
	}
}

func (s *Service) Wait(ctx context.Context, a mg.Actor, agent, id string) (mg.Task, error) {
	var last mg.Task
	for t, err := range s.Watch(ctx, a, agent, id) {
		if err != nil {
			return last, err
		}
		last = t
	}
	return last, nil
}

func (s *Service) pause(ctx context.Context) error {
	timer := time.NewTimer(s.PollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type Wait struct {
	ID    string          `json:"id"`
	Kind  string          `json:"kind"`
	Epoch int64           `json:"epoch"`
	Info  json.RawMessage `json:"info"`
}

// TaskView 不暴露内部日志、执行键和中断键，业务 input/result 原样保留。
type TaskView struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	AgentID   string          `json:"agent_id"`
	Revision  int64           `json:"revision"`
	State     mg.TaskState    `json:"state"`
	Input     json.RawMessage `json:"input"`
	Result    json.RawMessage `json:"result,omitempty"`
	ErrorKind string          `json:"error_kind,omitempty"`
	Waits     []Wait          `json:"waits,omitempty"`
	Approvals []ApprovalView  `json:"approvals,omitempty"`
}
type ApprovalView struct {
	ID       string `json:"id"`
	WaitID   string `json:"wait_id"`
	Revision int64  `json:"revision"`
}

func (s *Service) Describe(ctx context.Context, a mg.Actor, t mg.Task) (TaskView, error) {
	v := View(t)
	if t.State != mg.WaitingApproval {
		return v, nil
	}
	q := mg.Query{TaskID: t.ID, Statuses: []string{"pending"}}
	for {
		page, err := s.Management.ListApprovals(ctx, a, q)
		if err != nil {
			return v, err
		}
		for _, ap := range page.Items {
			for _, w := range t.Waits {
				if w.ID == ap.Wait.ID && w.Epoch == ap.Wait.Epoch {
					v.Approvals = append(v.Approvals, ApprovalView{ID: ap.ID, WaitID: w.ID, Revision: ap.Revision})
				}
			}
		}
		if page.NextCursor == "" {
			return v, nil
		}
		q.Cursor = page.NextCursor
	}
}
func View(t mg.Task) TaskView {
	v := TaskView{ID: t.ID, SessionID: t.SessionID, AgentID: t.AgentID, Revision: t.Revision, State: t.State, Input: t.Input, Result: t.Result, ErrorKind: t.ErrorKind}
	for _, w := range t.Waits {
		v.Waits = append(v.Waits, Wait{ID: w.ID, Kind: w.Kind, Epoch: w.Epoch, Info: w.Info})
	}
	return v
}
