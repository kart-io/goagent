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
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/tools"
)

// WithSessionLog records the session in store (the session ID is the same one
// tool calls see): model history is derived from the log and memory is no
// longer read. Before each model request, conversation that differs from the
// log is recorded (new messages one by one; history rewritten by PreStep and
// similar is recorded as history/rewrite), and the start and end of each turn
// are recorded. It supports both plain streaming chat and the native tool loop.
//
// When the previous turn did not end (the process was interrupted): if a
// durable run resumes with the same execution key, it continues writing that
// turn; otherwise the turn is recorded as aborted and its messages do not enter
// history. Durable runs can use session.DurableStore(store, id) as the durable
// journal, so operation snapshots and the conversation live in the same log.
func (b *AgentBuilder[S]) WithSessionLog(store session.Store) *AgentBuilder[S] {
	b.config.SessionLog = store
	return b
}

// WithSessionState makes state recoverable across processes along with the
// session log; it requires WithSessionLog. At the end of each turn the state is
// recorded as JSON in that turn's turn/end; when a turn starts, if the state in
// the log is not the entry corresponding to the current state, it replaces the
// current state first; sessions with no record start from the state at build
// time. Rewinding the session rewinds the state with it. The state type must
// implement MarshalJSON and UnmarshalJSON, and the encoding is up to the state
// type; *core.AgentState encodes as a JSON object, and restored values are
// generic JSON types.
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
	// memory 是本轮的长期记忆绑定；未配置长期记忆时为 nil。
	memory     *memory.TurnMemory
	recallMode string
}

const (
	plainMemoryMode = "plain-memory/1"
	plainNoneMode   = "plain-none/1"
)

func (a *ConfigurableAgent[S]) restoreTurnMemory(ctx context.Context, turn *sessionTurn, start session.TurnStart) error {
	turn.recallMode = start.RecallMode
	native := nativeToolLoop(a.llmClient, len(a.tools)) != nil
	if start.RecallMode != "" && native {
		return agentErrors.Newf(agentErrors.CodeStateValidation, "已开始的纯聊天轮次不能切换为原生工具循环")
	}
	switch start.RecallMode {
	case plainNoneMode:
		return nil
	case plainMemoryMode:
		if a.config.LongTermMemory == nil || len(start.Memory) == 0 {
			return agentErrors.Newf(agentErrors.CodeStateValidation, "纯聊天轮次缺少长期记忆配置或绑定")
		}
	case "":
		if len(start.Memory) == 0 && !native {
			return nil
		}
	default:
		return agentErrors.Newf(agentErrors.CodeStateValidation, "未知的纯聊天召回模式 %q", start.RecallMode)
	}
	var err error
	turn.memory, err = a.turnMemory(ctx, start.Memory)
	return err
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
			var d session.TurnStart
			if err := json.Unmarshal(events[start].Data, &d); err != nil {
				return nil, nil, err
			}
			if err = a.restoreTurnMemory(ctx, turn, d); err != nil {
				return nil, nil, err
			}
			turn.ended = a.memoryJob(id, turn.start, false, turn.memory)
			if err := a.restoreSessionState(id, events[:start]); err != nil {
				return nil, nil, err
			}
			// 重放期间状态从本轮开始前出发，结束后取本轮记录的状态，与原执行结束时一致。
			turn.replayed = func() error { return a.restoreSessionState(id, events) }
			history, err := turn.deriveHistory(events[:start])
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
			if err = a.restoreTurnMemory(ctx, turn, d); err != nil {
				return nil, nil, err
			}
			if err := a.restoreSessionState(id, before); err != nil {
				return nil, nil, err
			}
			a.bindSessionTurn(turn, id)
			history, err := turn.deriveHistory(before)
			turn.resumed, turn.start = open.Seq, open.Seq
			turn.ended = a.memoryJob(id, turn.start, false, turn.memory)
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
		// 中止轮次的任务按它开始时的绑定登记；升级前的轮次没有绑定，不登记。
		var binding *memory.TurnMemory
		if len(d.Memory) > 0 {
			binding = new(memory.TurnMemory)
			if err := json.Unmarshal(d.Memory, binding); err != nil {
				return nil, nil, fmt.Errorf("会话日志事件 %d：%w", open.Seq, err)
			}
		}
		if learn := a.memoryJob(id, open.Seq, true, binding); learn != nil {
			if err := learn(ctx, rec.Seq()); err != nil {
				return nil, nil, err
			}
		}
	}
	if err := a.restoreSessionState(id, rec.Events()); err != nil {
		return nil, nil, err
	}
	if turn.memory, err = a.turnMemory(ctx, nil); err != nil {
		return nil, nil, err
	}
	var binding json.RawMessage
	if turn.memory != nil {
		if binding, err = json.Marshal(turn.memory); err != nil {
			return nil, nil, err
		}
	}
	a.bindSessionTurn(turn, id)
	history, err := turn.deriveHistory(rec.Events())
	if err != nil {
		return nil, nil, err
	}
	if nativeToolLoop(a.llmClient, len(a.tools)) == nil {
		turn.recallMode = plainNoneMode
		if turn.memory != nil {
			turn.recallMode = plainMemoryMode
		}
	}
	start, err := session.NewEvent(constants.SessionEventTurnStart, session.TurnStart{Input: input, ExecutionKey: key, Memory: binding, RecallMode: turn.recallMode})
	if err != nil {
		return nil, nil, err
	}
	if _, err = rec.Append(ctx, start); err != nil {
		return nil, nil, err
	}
	turn.start = rec.Seq()
	turn.ended = a.memoryJob(id, turn.start, false, turn.memory)
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
