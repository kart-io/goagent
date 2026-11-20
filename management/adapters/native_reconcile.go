package adapters

import (
	"context"
	"errors"
	"strings"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
)

// currentTurn 读取会话日志，只收集本任务所在轮次的工具记录；followup 轮次与之前的轮次不混入。
func (n *native) currentTurn(ctx context.Context, spec mg.RunSpec) ([]session.Event, map[string]*durable.OperationRecord, bool, error) {
	events, err := n.r.Sessions.Read(ctx, n.r.Session.LogID, 0)
	if err != nil {
		return nil, nil, false, err
	}
	active := false
	completed := false
	records := map[string]*durable.OperationRecord{}
	for _, ev := range events {
		switch ev.Type {
		case constants.SessionEventTurnStart:
			var turn session.TurnStart
			if json.Unmarshal(ev.Data, &turn) != nil {
				return nil, nil, false, agentErrors.ErrManagementInvalid
			}
			active = turn.ExecutionKey == spec.Task.ExecutionKey
			completed = false
			if active {
				records = map[string]*durable.OperationRecord{}
			}
		case constants.SessionEventBoundarySnapshot:
			if !active {
				continue
			}
			var boundary session.BoundaryData
			var snapshot durable.Snapshot
			if json.Unmarshal(ev.Data, &boundary) != nil || json.Unmarshal(boundary.Snapshot, &snapshot) != nil {
				return nil, nil, false, agentErrors.ErrManagementInvalid
			}
			var identity struct {
				ID  string `json:"id"`
				Key string `json:"key"`
			}
			if json.Unmarshal(snapshot.Data, &identity) != nil || !strings.HasPrefix(identity.ID, "tool/") {
				continue
			}
			record, e := durable.DecodeOperationRecord(&snapshot, identity.Key, identity.ID)
			if e != nil {
				return nil, nil, false, e
			}
			records[record.Key] = record
		case constants.SessionEventTurnEnd:
			var end session.TurnEnd
			if json.Unmarshal(ev.Data, &end) != nil {
				return nil, nil, false, agentErrors.ErrManagementInvalid
			}
			if active && !end.Aborted {
				completed = true
			}
		}
	}
	return events, records, completed, nil
}

func interruptedRecord(r *durable.OperationRecord) bool {
	switch r.Status {
	case "pending":
		return r.FailureKind != "nested_attention"
	case "failed":
		return r.FailureKind == "canceled" || r.FailureKind == "deadline" || r.FailureKind == "effect_unknown"
	}
	return false
}

// toolOperations 收集错误树中工具调用的 OperationError；模型请求等其他操作不交给宿主核对。
func toolOperations(err error) []durable.OperationRef {
	var refs []durable.OperationRef
	seen := map[durable.OperationRef]bool{}
	var walk func(error)
	walk = func(e error) {
		if e == nil {
			return
		}
		if op, ok := e.(*durable.OperationError); ok && strings.HasPrefix(op.ID, "tool/") && op.ParentKey != "" {
			ref := durable.OperationRef{ParentKey: op.ParentKey, ID: op.ID}
			if !seen[ref] {
				seen[ref] = true
				refs = append(refs, ref)
			}
		}
		switch u := e.(type) {
		case interface{ Unwrap() []error }:
			for _, c := range u.Unwrap() {
				walk(c)
			}
		case interface{ Unwrap() error }:
			walk(u.Unwrap())
		}
	}
	walk(err)
	return refs
}

// Reconcile checks that rec covers exactly the unresolved tool calls of the
// task's current turn and returns absent, so that the worker resumes Run with
// spec.Reconciliation and the Dispatcher settles the listed calls with the
// host's conclusion. It never reports completed: the turn still has to finish.
func (n *native) Reconcile(ctx context.Context, spec mg.RunSpec, rec mg.Reconciliation) (mg.RunEvidence, error) {
	if rec.Resolution != "applied" && rec.Resolution != "not_applied" {
		return mg.RunEvidence{}, agentErrors.ErrManagementConflict
	}
	_, records, completed, err := n.currentTurn(ctx, spec)
	if err != nil {
		return mg.RunEvidence{}, err
	}
	if completed {
		return mg.RunEvidence{}, agentErrors.ErrManagementConflict
	}
	listed := map[string]bool{}
	for _, ref := range rec.OperationRefs {
		key := durable.ChildKey(ref.ParentKey, ref.ID)
		record, ok := records[key]
		if key == "" || listed[key] || !ok || !interruptedRecord(record) {
			return mg.RunEvidence{}, conflict("核对的操作不是本轮未决的工具调用")
		}
		listed[key] = true
	}
	// 尝试次数已用尽的中断记录只能由核对定案，必须全部在核对范围内。
	for key, record := range records {
		if interruptedRecord(record) && record.Attempts >= record.MaxAttempts && !listed[key] {
			return mg.RunEvidence{}, conflict("核对未覆盖全部未决的工具调用")
		}
	}
	return mg.RunEvidence{State: "absent"}, nil
}

func conflict(msg string) error {
	return errors.Join(agentErrors.ErrManagementConflict, errors.New(msg))
}
