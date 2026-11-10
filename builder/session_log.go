package builder

import (
	"context"
	"fmt"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

// WithSessionLog 把会话记入 store（会话 ID 同工具调用看到的会话 ID）：模型历史从日志推导，不再读取记忆；
// 每次模型请求前记录与日志不同的对话（新增消息逐条记录，被 PreStep 等改写的历史记为 history/rewrite），
// 每轮记录开始与结束。支持普通流式聊天与原生工具循环。
//
// 上一轮未结束（进程中断）时：持久运行以同一执行键恢复则续写该轮，否则该轮记为中止，其消息不进入历史。
// 持久运行可用 session.DurableStore(store, id) 作为 durable 日志，操作快照与对话位于同一份日志。
func (b *AgentBuilder[S]) WithSessionLog(store session.Store) *AgentBuilder[S] {
	b.config.SessionLog = store
	return b
}

// WithSessionState 让状态随会话日志跨进程恢复，需要 WithSessionLog。
// 每轮结束时状态以 JSON 记入该轮的 turn/end；开始一轮时若日志中的状态不是当前状态对应的那条，
// 先用它替换当前状态，没有记录的会话从构建时的状态开始。退回会话后状态随之退回。
// 状态类型必须实现 MarshalJSON 与 UnmarshalJSON，编码格式由状态类型自己决定；
// *core.AgentState 编码为 JSON 对象，恢复后的值为 JSON 通用类型。
func (b *AgentBuilder[S]) WithSessionState() *AgentBuilder[S] {
	b.config.SessionState = true
	return b
}

type stateCodec interface {
	MarshalJSON() ([]byte, error)
	UnmarshalJSON([]byte) error
}

// encodeSessionState 校验 WithSessionState 的前提并编码构建时的状态。
func encodeSessionState(log session.Store, state core.State) (json.RawMessage, error) {
	if log == nil {
		return nil, agentErrors.NewInvalidConfigError("builder", "session_state", "WithSessionState 需要 WithSessionLog")
	}
	if _, ok := state.(stateCodec); !ok {
		return nil, agentErrors.NewInvalidConfigError("builder", "session_state", fmt.Sprintf("WithSessionState 要求状态类型 %T 实现 MarshalJSON 与 UnmarshalJSON", state))
	}
	data, err := json.Marshal(state)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidConfig, "编码初始状态失败")
	}
	return data, nil
}

// restoreSessionState 让当前状态与会话 id 在 events 中的最后状态一致。
func (a *ConfigurableAgent[S]) restoreSessionState(id string, events []session.Event) error {
	if !a.config.SessionState {
		return nil
	}
	data, seq, err := session.LastState(events)
	if err != nil {
		return err
	}
	if id == a.stateSession && seq == a.stateSeq {
		return nil
	}
	if id == a.stateSession && a.stateBase != nil && seq == a.stateBaseSeq {
		// 上一轮没有写入 turn/end（失败或中断）且日志没有更新的状态：回到它开始前的状态。
		data = a.stateBase
	}
	if data == nil {
		if a.stateSession == "" {
			// 首次执行且会话没有记录：当前状态就是构建时的状态。
			a.stateSession = id
			return nil
		}
		data = a.initialState
	}
	if err := json.Unmarshal(data, a.state); err != nil {
		return fmt.Errorf("恢复会话状态：%w", err)
	}
	a.stateSession, a.stateSeq, a.stateBase = id, seq, nil
	return nil
}

// bindSessionTurn 记录轮次结束时编码状态，写入后把当前状态绑定到该条 turn/end。
func (a *ConfigurableAgent[S]) bindSessionTurn(turn *sessionTurn, id string) {
	if !a.config.SessionState {
		return
	}
	// 编码失败时不保留基线，下一轮改从会话日志恢复。
	a.stateBase, _ = json.Marshal(a.state)
	a.stateBaseSeq, a.stateSeq = a.stateSeq, -1
	turn.state = func() (json.RawMessage, error) { return json.Marshal(a.state) }
	turn.saved = func(seq int64) { a.stateSession, a.stateSeq, a.stateBase = id, seq, nil }
}

type sessionLogKey struct{}

// sessionTurn 是一轮的会话日志记录。
type sessionTurn struct {
	rec   *session.Recorder
	tools map[string]session.ToolMeta
	// resumed 是续写的轮次的 turn/start 序号：回放到已记录位置的同步不再记录，直到对话超出日志。
	resumed   int64
	completed bool
	// state 非空时 end 把它的结果记入 turn/end，写入后以该事件序号调用 saved。
	state func() (json.RawMessage, error)
	saved func(seq int64)
	// replayed 在已完成轮次重放结束时调用。
	replayed func() error
	// start 是本轮 turn/start 的序号；ended 在本轮的 turn/end 写入后以其序号调用（重放已完成轮次时同样调用，须幂等）。
	start int64
	ended func(ctx context.Context, end int64) error
}

func sessionTurnFrom(ctx context.Context) *sessionTurn {
	t, _ := ctx.Value(sessionLogKey{}).(*sessionTurn)
	return t
}

func (a *ConfigurableAgent[S]) sessionLogID(ctx context.Context) string {
	if id := tools.SessionIDFrom(ctx); id != "" {
		return id
	}
	return a.config.SessionID
}

// openSessionTurn 读取日志、处理未结束的轮次并开始本轮，返回日志推导的历史。
func (a *ConfigurableAgent[S]) openSessionTurn(ctx context.Context, input string) (*sessionTurn, []llm.Message, error) {
	id := a.sessionLogID(ctx)
	if id == "" {
		return nil, nil, agentErrors.NewInvalidConfigError("builder", "session_id", "会话日志需要会话 ID")
	}
	rec, _, err := session.OpenRecorder(ctx, a.config.SessionLog, id)
	if err != nil {
		return nil, nil, err
	}
	key := ""
	if durable.HasJournal(ctx) {
		key, _ = ctx.Value(hostExecutionKey{}).(string)
	}
	turn := &sessionTurn{rec: rec, tools: map[string]session.ToolMeta{}}
	events := rec.Events()
	if key != "" {
		start, err := completedSessionTurn(events, key, input)
		if err != nil {
			return nil, nil, err
		}
		if start >= 0 {
			turn.resumed, turn.completed, turn.start = events[start].Seq, true, events[start].Seq
			turn.ended = a.memoryJob(id, turn.start, false)
			if err := a.restoreSessionState(id, events[:start]); err != nil {
				return nil, nil, err
			}
			// 重放期间状态从本轮开始前出发，结束后取本轮记录的状态，与原执行结束时一致。
			turn.replayed = func() error { return a.restoreSessionState(id, events) }
			history, err := session.DeriveMessages(events[:start])
			return turn, history, err
		}
	}
	if open, ok := session.OpenTurn(events); ok {
		var d session.TurnStart
		if err := json.Unmarshal(open.Data, &d); err != nil {
			return nil, nil, fmt.Errorf("会话日志事件 %d：%w", open.Seq, err)
		}
		if key != "" && d.ExecutionKey == key {
			if d.Input != input {
				return nil, nil, agentErrors.Newf(agentErrors.CodeConflict, "同一执行键的原始输入不能改变").WithComponent("builder").WithOperation("open_session_turn")
			}
			// 恢复中的持久运行：历史是本轮之前的对话，本轮已记录的部分在同步时与回放的消息一致。
			var before []session.Event
			for _, ev := range events {
				if ev.Seq < open.Seq {
					before = append(before, ev)
				}
			}
			if err := a.restoreSessionState(id, before); err != nil {
				return nil, nil, err
			}
			a.bindSessionTurn(turn, id)
			history, err := session.DeriveMessages(before)
			turn.resumed, turn.start = open.Seq, open.Seq
			turn.ended = a.memoryJob(id, turn.start, false)
			return turn, history, err
		}
		end, err := session.NewEvent(constants.SessionEventTurnEnd, session.TurnEnd{Aborted: true})
		if err != nil {
			return nil, nil, err
		}
		end.Surface = &session.Surface{Start: open.Seq, End: rec.Seq()}
		if _, err := rec.Append(ctx, end); err != nil {
			return nil, nil, err
		}
		if learn := a.memoryJob(id, open.Seq, true); learn != nil {
			if err := learn(ctx, rec.Seq()); err != nil {
				return nil, nil, err
			}
		}
	}
	if err := a.restoreSessionState(id, rec.Events()); err != nil {
		return nil, nil, err
	}
	a.bindSessionTurn(turn, id)
	history, err := session.DeriveMessages(rec.Events())
	if err != nil {
		return nil, nil, err
	}
	start, err := session.NewEvent(constants.SessionEventTurnStart, session.TurnStart{Input: input, ExecutionKey: key})
	if err != nil {
		return nil, nil, err
	}
	if _, err = rec.Append(ctx, start); err != nil {
		return nil, nil, err
	}
	turn.start = rec.Seq()
	turn.ended = a.memoryJob(id, turn.start, false)
	return turn, history, nil
}

// completedSessionTurn 仅恢复当前最后一轮；退回或后续新轮次之后须使用新的执行键。
func completedSessionTurn(events []session.Event, key, input string) (int, error) {
	laterTurn := false
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != constants.SessionEventTurnStart {
			continue
		}
		var start session.TurnStart
		if err := json.Unmarshal(events[i].Data, &start); err != nil {
			return -1, err
		}
		if start.ExecutionKey != key {
			laterTurn = true
			continue
		}
		if start.Input != input || laterTurn {
			return -1, agentErrors.Newf(agentErrors.CodeConflict, "执行键已绑定其他输入或历史轮次，须使用新的执行键").WithComponent("builder").WithOperation("completed_session_turn")
		}
		for _, ev := range events[i+1:] {
			if ev.Type == constants.SessionEventRewind {
				return -1, agentErrors.Newf(agentErrors.CodeConflict, "退回后的会话须使用新的执行键").WithComponent("builder").WithOperation("completed_session_turn")
			}
		}
		for _, ev := range events[i+1:] {
			if ev.Type != constants.SessionEventTurnEnd {
				continue
			}
			var end session.TurnEnd
			if err := json.Unmarshal(ev.Data, &end); err != nil {
				return -1, err
			}
			if end.Aborted {
				return -1, agentErrors.Newf(agentErrors.CodeConflict, "已中止的会话轮次须使用新的执行键").WithComponent("builder").WithOperation("completed_session_turn")
			}
			return i, nil
		}
		return -1, nil
	}
	return -1, nil
}

// begin 记录本轮的 system 提示词与工具定义（与上次不同时）。
func (t *sessionTurn) begin(ctx context.Context, messages []llm.Message, offered []interfaces.Tool) error {
	if t == nil || t.completed {
		return nil
	}
	system := ""
	if len(messages) > 0 && messages[0].Role == "system" {
		system = messages[0].Content
	}
	if err := t.rec.SyncSystem(ctx, system); err != nil {
		return err
	}
	defs := make([]session.ToolDefinition, 0, len(offered))
	for _, tool := range offered {
		defs = append(defs, session.ToolDefinition{Name: tool.Name(), Description: tool.Description(), Parameters: json.RawMessage(tool.ArgsSchema())})
	}
	return t.rec.SyncTools(ctx, defs)
}

// sync 记录 messages（去掉 system）与日志的差异。
func (t *sessionTurn) sync(ctx context.Context, messages []llm.Message) error {
	if t == nil {
		return nil
	}
	if len(messages) > 0 && messages[0].Role == "system" {
		messages = messages[1:]
	}
	if t.resumed > 0 {
		replayed, err := t.rec.Replayed(t.resumed, messages)
		if err != nil || replayed {
			return err
		}
		if t.completed {
			return agentErrors.Newf(agentErrors.CodeStateValidation, "已完成轮次的重放对话与会话日志不一致").WithComponent("builder").WithOperation("sync")
		}
		t.resumed = 0
	}
	return t.rec.Sync(ctx, messages, t.tools)
}

func (t *sessionTurn) toolResult(id, name string, success bool) {
	if t != nil {
		t.tools[id] = session.ToolMeta{Name: name, Success: success}
	}
}

// end 记录最终对话与轮次结束。
func (t *sessionTurn) end(ctx context.Context, messages []llm.Message, answer string, usage *interfaces.TokenUsage) error {
	if t == nil {
		return nil
	}
	if err := t.sync(ctx, messages); err != nil {
		return err
	}
	if t.completed {
		if t.replayed != nil {
			if err := t.replayed(); err != nil {
				return err
			}
		}
		return t.notifyEnded(ctx)
	}
	d := session.TurnEnd{Answer: answer}
	if usage != nil {
		copy := *usage
		d.Usage = &copy
	}
	if t.state != nil {
		var err error
		if d.State, err = t.state(); err != nil {
			return fmt.Errorf("编码会话状态：%w", err)
		}
	}
	ev, err := session.NewEvent(constants.SessionEventTurnEnd, d)
	if err != nil {
		return err
	}
	appended, err := t.rec.Append(ctx, ev)
	if err == nil && t.saved != nil && len(appended) > 0 {
		t.saved(appended[len(appended)-1].Seq)
	}
	if err == nil {
		err = t.notifyEnded(ctx)
	}
	return err
}

// notifyEnded 以本轮 turn/end 的序号调用 ended。
func (t *sessionTurn) notifyEnded(ctx context.Context) error {
	if t.ended == nil {
		return nil
	}
	for _, ev := range t.rec.Events() {
		if ev.Seq > t.start && ev.Type == constants.SessionEventTurnEnd {
			return t.ended(ctx, ev.Seq)
		}
	}
	return nil
}
