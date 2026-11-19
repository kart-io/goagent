package session

import (
	"fmt"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
)

// Validate checks the structural invariants of a complete journal as returned
// by Store.Read(ctx, id, 0), so that a custom Store, a Fork or a hand-written
// event cannot silently change the derived history. It reports the first
// violation wrapped in errors.ErrSessionInvalidLog:
//   - sequence numbers start at 1 and are consecutive, and every type is
//     non-empty and every Data is valid JSON;
//   - known event types decode into their data types, message roles match the
//     event type, and boundary snapshots carry an ID;
//   - turn/start and turn/end alternate, an aborted turn/end masks its own
//     turn including every later message, and a completed turn/end has no
//     Surface;
//   - Surface appears only on history/rewrite, session/rewind and aborted
//     turn/end, is required there, and covers earlier events only; a rewrite
//     covers message events and a rewind ends right before itself.
//
// Event types it does not know are accepted with their data unchecked, but
// they must not carry a Surface, since Derive would mask messages with it.
func Validate(events []Event) error {
	v := validator{kinds: make(map[int64]string, len(events))}
	for i, ev := range events {
		if reason := v.check(int64(i)+1, ev); reason != "" {
			return fmt.Errorf("%w：事件 %d（%s）：%s", agentErrors.ErrSessionInvalidLog, ev.Seq, ev.Type, reason)
		}
		v.kinds[ev.Seq] = ev.Type
	}
	return nil
}

type validator struct {
	open  int64 // 未结束轮次的 turn/start 序号，0 表示没有
	kinds map[int64]string
}

// surfaceRequired 列出可带 Surface 的事件类型及是否必须带；turn/end 只在中止时必须带，由其规则检查。
var surfaceRequired = map[string]bool{
	constants.SessionEventHistoryRewrite: true,
	constants.SessionEventRewind:         true,
	constants.SessionEventTurnEnd:        false,
}

// messageTypes 是 Derive 产出消息的事件类型。
var messageTypes = map[string]bool{
	constants.SessionEventUserMessage:      true,
	constants.SessionEventAssistantMessage: true,
	constants.SessionEventToolResult:       true,
	constants.SessionEventHistoryRewrite:   true,
}

// roleAllowed 给出各消息事件允许的角色；Recorder 把助手与工具以外的角色都记为 user/message。
var roleAllowed = map[string]func(string) bool{
	constants.SessionEventUserMessage: func(r string) bool {
		return r != constants.RoleAssistant && r != "tool"
	},
	constants.SessionEventAssistantMessage: func(r string) bool { return r == constants.RoleAssistant },
	constants.SessionEventToolResult:       func(r string) bool { return r == "tool" },
	constants.SessionEventSystemMessage:    func(r string) bool { return r == constants.RoleSystem },
}

var eventRules = map[string]func(*validator, Event) string{
	constants.SessionEventTurnStart:        (*validator).turnStart,
	constants.SessionEventTurnEnd:          (*validator).turnEnd,
	constants.SessionEventUserMessage:      (*validator).message,
	constants.SessionEventAssistantMessage: (*validator).message,
	constants.SessionEventToolResult:       (*validator).message,
	constants.SessionEventSystemMessage:    (*validator).message,
	constants.SessionEventToolDefinitions:  func(_ *validator, ev Event) string { return decode[ToolsData](ev) },
	constants.SessionEventHistoryRewrite:   (*validator).rewrite,
	constants.SessionEventRewind:           (*validator).rewind,
	constants.SessionEventBoundarySnapshot: (*validator).boundary,
}

func (v *validator) check(seq int64, ev Event) string {
	if ev.Seq != seq {
		return fmt.Sprintf("序号应为 %d", seq)
	}
	if ev.Type == "" {
		return "类型为空"
	}
	if len(ev.Data) > 0 && !json.Valid(ev.Data) {
		return "数据不是合法 JSON"
	}
	required, allowed := surfaceRequired[ev.Type]
	switch {
	case ev.Surface == nil && required:
		return "缺少 Surface"
	case ev.Surface != nil && !allowed:
		return "该类型不能带 Surface"
	case ev.Surface != nil && (ev.Surface.Start < 1 || ev.Surface.Start > ev.Surface.End || ev.Surface.End >= ev.Seq):
		return fmt.Sprintf("Surface [%d, %d] 须是本事件之前的非空区间", ev.Surface.Start, ev.Surface.End)
	}
	if rule, ok := eventRules[ev.Type]; ok {
		return rule(v, ev)
	}
	return ""
}

// decode 返回数据无法解码为 T 时的原因。
func decode[T any](ev Event) string {
	var d T
	if err := json.Unmarshal(ev.Data, &d); err != nil {
		return err.Error()
	}
	return ""
}

func (v *validator) turnStart(ev Event) string {
	if v.open != 0 {
		return fmt.Sprintf("轮次 %d 尚未结束", v.open)
	}
	v.open = ev.Seq
	return decode[TurnStart](ev)
}

func (v *validator) turnEnd(ev Event) string {
	var d TurnEnd
	if err := json.Unmarshal(ev.Data, &d); err != nil {
		return err.Error()
	}
	open := v.open
	v.open = 0
	switch {
	case open == 0:
		return "没有未结束的轮次"
	case d.Aborted && (ev.Surface == nil || ev.Surface.Start != open):
		return fmt.Sprintf("中止的轮次须以 Surface 从 turn/start %d 起撤销", open)
	case !d.Aborted && ev.Surface != nil:
		return "完成的轮次不能带 Surface"
	}
	if d.Aborted {
		// 撤销只覆盖 [Start, End]；End 之后的本轮消息会漏进历史，Recorder 重试时只会夹入快照。
		for seq := ev.Surface.End + 1; seq < ev.Seq; seq++ {
			if messageTypes[v.kinds[seq]] {
				return fmt.Sprintf("中止的轮次未撤销其后的消息事件 %d", seq)
			}
		}
	}
	return ""
}

// rewind 要求区间止于本事件之前：Derive 只按 Start 退回，End 不同会让日志与实际撤销范围不符。
func (v *validator) rewind(ev Event) string {
	if ev.Surface.End != ev.Seq-1 {
		return fmt.Sprintf("退回区间须止于 %d", ev.Seq-1)
	}
	return ""
}

func (v *validator) message(ev Event) string {
	var d MessageData
	if err := json.Unmarshal(ev.Data, &d); err != nil {
		return err.Error()
	}
	if !roleAllowed[ev.Type](d.Message.Role) {
		return fmt.Sprintf("消息角色 %q 与事件类型不符", d.Message.Role)
	}
	return ""
}

func (v *validator) rewrite(ev Event) string {
	if r := decode[RewriteData](ev); r != "" {
		return r
	}
	for _, seq := range []int64{ev.Surface.Start, ev.Surface.End} {
		if !messageTypes[v.kinds[seq]] {
			return fmt.Sprintf("改写区间端点 %d 不是消息事件", seq)
		}
	}
	return ""
}

func (v *validator) boundary(ev Event) string {
	var d BoundaryData
	if err := json.Unmarshal(ev.Data, &d); err != nil {
		return err.Error()
	}
	if d.ID == "" {
		return "快照缺少 ID"
	}
	return ""
}
