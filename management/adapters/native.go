package adapters

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/tools"
)

// NativeFactory 由宿主提供固定实现版本的 Builder；适配器强制绑定管理身份与日志。
type NativeFactory struct {
	Check      func(context.Context, mg.Version) error
	BuildAgent func(context.Context, mg.Version, mg.Resources, tools.Approver) (*builder.SimpleAgentBuilder, func(context.Context) error, error)
	// Observe 可选：Run 对每个收到的事件先调用它再折叠结果；Inspect 的只读重放不调用。
	// 钩子不影响任务结果，panic 会被忽略。
	Observe func(ctx context.Context, task mg.Task, ev builder.AgentEvent)
}

func (f NativeFactory) Validate(ctx context.Context, v mg.Version) (mg.Capabilities, error) {
	if f.BuildAgent == nil || f.Check == nil {
		return mg.Capabilities{}, agentErrors.ErrManagementInvalid
	}
	return mg.Capabilities{Reconcile: true, Approval: true, Fork: true, Input: true}, f.Check(ctx, v)
}

func (f NativeFactory) Build(ctx context.Context, v mg.Version, r mg.Resources) (mg.Binding, error) {
	b, cleanup, err := f.BuildAgent(ctx, v, r, approver{r: r})
	binding := mg.Binding{Cleanup: cleanup}
	if err != nil {
		return binding, err
	}
	if b == nil {
		return binding, agentErrors.ErrManagementInvalid
	}
	agent, err := b.WithSessionID(r.Session.LogID).WithSessionLog(r.Sessions).WithDurableScope(v.AgentID + "@" + v.Version + "/" + v.ConfigHash).WithAgentID(v.AgentID).BuildContext(ctx)
	if err != nil {
		return binding, err
	}
	binding.Runtime = &native{start: agent.ExecuteStream, r: r, observe: f.Observe}
	return binding, nil
}

type native struct {
	start func(context.Context, interface{}) (<-chan builder.AgentEvent, error)
	r     mg.Resources
	// observe 仅由 Run 使用；Inspect 通过 replay 调用 run 时传 nil。
	observe func(context.Context, mg.Task, builder.AgentEvent)
}

func callObserve(ctx context.Context, f func(context.Context, mg.Task, builder.AgentEvent), task mg.Task, ev builder.AgentEvent) {
	defer func() { _ = recover() }()
	f(ctx, task, ev)
}

func inputValue(raw json.RawMessage) (any, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	var value any
	err := json.Unmarshal(raw, &value, json.UseNumber)
	return value, err
}

func (n *native) Run(ctx context.Context, spec mg.RunSpec) (mg.RunResult, error) {
	return n.run(ctx, spec, n.observe)
}

func (n *native) run(ctx context.Context, spec mg.RunSpec, observe func(context.Context, mg.Task, builder.AgentEvent)) (mg.RunResult, error) {
	for k, v := range spec.ResumeInputs {
		ctx = durable.WithResume(ctx, k, v)
	}
	if rec := spec.Reconciliation; rec != nil {
		ctx = tools.WithReconciliation(ctx, tools.Reconciliation{Resolution: rec.Resolution, Operations: rec.OperationRefs})
	}
	input, err := inputValue(spec.Task.Input)
	if err != nil {
		return mg.RunResult{}, err
	}
	events, err := n.start(ctx, input)
	if err != nil {
		return mg.RunResult{}, err
	}
	var output *builder.AgentOutput
	for ev := range events {
		if observe != nil {
			callObserve(ctx, observe, spec.Task, ev)
		}
		if ev.Error != nil {
			err = ev.Error
		}
		if ev.Output != nil {
			output = ev.Output
		}
	}
	if err != nil {
		if waits := durable.Waits(err); waits != nil {
			return mg.RunResult{Outcome: "waiting", Waits: mg.WaitPoints(waits)}, err
		}
		if refs := toolOperations(err); len(refs) > 0 && errors.Is(err, agentErrors.ErrOperationAttention) {
			return mg.RunResult{Outcome: "attention", Operations: refs}, err
		}
		// 取消时终止事件可能被丢弃，按 builder 约定以 ctx.Err() 为准。
		if cerr := ctx.Err(); cerr != nil && !errors.Is(err, cerr) {
			err = errors.Join(err, cerr)
		}
		return mg.RunResult{}, err
	}
	if output == nil {
		if cerr := ctx.Err(); cerr != nil {
			return mg.RunResult{}, cerr
		}
		return mg.RunResult{}, agentErrors.Newf(agentErrors.CodeInvalidOutput, "生产者退出时没有确定结果").WithComponent("management")
	}
	raw, err := json.Marshal(output.Result)
	return mg.RunResult{Outcome: "completed", Output: raw}, err
}

func (n *native) Inspect(ctx context.Context, spec mg.RunSpec) (mg.RunEvidence, error) {
	events, _, completed, err := n.currentTurn(ctx, spec)
	if err != nil {
		return mg.RunEvidence{}, err
	}
	if !completed {
		// 中断的工具记录交给续跑中的 Do 按工具策略处理：可重跑的重跑，只执行一次的先核对，
		// 核对不出结果才以 OperationError 进入 attention，由 Run 报告具体操作。
		return mg.RunEvidence{State: "absent"}, nil
	}
	// 计量配置属于模型操作意图。保持同一配置，但把回放中的计量补记限制在内存副本。
	copyLog := session.NewMemoryStore()
	if _, err = copyLog.Append(ctx, n.r.Session.LogID, 0, events...); err != nil {
		return mg.RunEvidence{}, err
	}
	journal := session.DurableStore(copyLog, n.r.Session.LogID)
	controller, err := budget.NewDurable(spec.Task.Budget, journal)
	if err != nil {
		return mg.RunEvidence{}, err
	}
	ctx = budget.WithController(durable.WithJournal(ctx, journal), controller)
	result, err := n.run(durable.WithReplayOnly(ctx), mg.RunSpec{Task: spec.Task}, nil)
	if err != nil {
		return mg.RunEvidence{}, err
	}
	return mg.RunEvidence{State: "completed", Result: result}, nil
}

type approver struct{ r mg.Resources }

func hash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func (a approver) Approve(ctx context.Context, req tools.ApprovalRequest) (tools.ApprovalResult, error) {
	if req.Call == nil || req.Call.CallID == "" || a.r.ApprovalReader == nil {
		return tools.ApprovalResult{}, agentErrors.ErrManagementInvalid
	}
	intent, err := json.Marshal(struct {
		Tool, CallID, Key string
		Args              map[string]any
	}{req.Call.Tool, req.Call.CallID, req.Call.ExecutionKey, req.Call.Args})
	if err != nil {
		return tools.ApprovalResult{}, err
	}
	intentHash := hash(intent)
	info, err := json.Marshal(map[string]any{"kind": "approval", "intent_hash": intentHash, "tool": req.Call.Tool, "reason": req.Reason, "arguments": req.Call.Args})
	if err != nil {
		return tools.ApprovalResult{}, err
	}
	raw, err := durable.AwaitInput(durable.AllowInterrupt(ctx), "management/approval/"+req.Call.CallID, info, func(raw json.RawMessage) error {
		var answer mg.ApprovalAnswer
		if json.Unmarshal(raw, &answer) != nil || answer.IntentHash != intentHash || (answer.Decision != "approve" && answer.Decision != "reject") {
			return agentErrors.ErrManagementInvalid
		}
		ap, e := a.r.ApprovalReader(ctx, answer.ApprovalID)
		if e != nil {
			return e
		}
		if ap.TaskID != a.r.Task.ID || ap.Wait.IntentHash != intentHash || ap.Decision != answer.Decision || (ap.State != "approved" && ap.State != "rejected") {
			return agentErrors.ErrManagementForbidden
		}
		return nil
	})
	if err != nil {
		return tools.ApprovalResult{}, err
	}
	var answer mg.ApprovalAnswer
	if err = json.Unmarshal(raw, &answer); err != nil {
		return tools.ApprovalResult{}, err
	}
	switch answer.Decision {
	case "approve":
		return tools.ApprovalResult{Outcome: tools.ApprovalAllowedOnce}, nil
	case "reject":
		return tools.ApprovalResult{Outcome: tools.ApprovalRejected, Feedback: "审批人已拒绝此操作"}, nil
	}
	return tools.ApprovalResult{}, fmt.Errorf("%w：未知审批决定", agentErrors.ErrManagementInvalid)
}
