package builder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

// WithSessionLog 把会话记入 store（会话 ID 同工具调用看到的会话 ID）：模型历史从日志推导，不再读取记忆；
// 每次模型请求前记录与日志不同的对话（新增消息逐条记录，被 PreStep 等改写的历史记为 history/rewrite），
// 每轮记录开始与结束。只支持原生工具流。
//
// 上一轮未结束（进程中断）时：持久运行以同一执行键恢复则续写该轮，否则该轮记为中止，其消息不进入历史。
// 持久运行可用 session.DurableStore(store, id) 作为 durable 日志，操作快照与对话位于同一份日志。
func (b *AgentBuilder[C, S]) WithSessionLog(store session.Store) *AgentBuilder[C, S] {
	b.config.SessionLog = store
	return b
}

type sessionLogKey struct{}

// sessionTurn 是一轮的会话日志记录。
type sessionTurn struct {
	rec   *session.Recorder
	tools map[string]session.ToolMeta
	// resumed 是续写的轮次的 turn/start 序号：回放到已记录位置的同步不再记录，直到对话超出日志。
	resumed int64
}

func sessionTurnFrom(ctx context.Context) *sessionTurn {
	t, _ := ctx.Value(sessionLogKey{}).(*sessionTurn)
	return t
}

func (a *ConfigurableAgent[C, S]) sessionLogID(ctx context.Context) string {
	if id := tools.SessionIDFrom(ctx); id != "" {
		return id
	}
	return a.config.SessionID
}

// openSessionTurn 读取日志、处理未结束的轮次并开始本轮，返回日志推导的历史。
func (a *ConfigurableAgent[C, S]) openSessionTurn(ctx context.Context, input string) (*sessionTurn, []llm.Message, error) {
	id := a.sessionLogID(ctx)
	if id == "" {
		return nil, nil, errors.New("会话日志需要会话 ID")
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
	if open, ok := session.OpenTurn(events); ok {
		var d session.TurnStart
		if err := json.Unmarshal(open.Data, &d); err != nil {
			return nil, nil, fmt.Errorf("会话日志事件 %d：%w", open.Seq, err)
		}
		if key != "" && d.ExecutionKey == key {
			// 恢复中的持久运行：历史是本轮之前的对话，本轮已记录的部分在同步时与回放的消息一致。
			var before []session.Event
			for _, ev := range events {
				if ev.Seq < open.Seq {
					before = append(before, ev)
				}
			}
			history, err := session.DeriveMessages(before)
			turn.resumed = open.Seq
			return turn, history, err
		}
		end, err := session.NewEvent(session.TypeTurnEnd, session.TurnEnd{Aborted: true})
		if err != nil {
			return nil, nil, err
		}
		end.Surface = &session.Surface{Start: open.Seq, End: rec.Seq()}
		if _, err := rec.Append(ctx, end); err != nil {
			return nil, nil, err
		}
	}
	history, err := session.DeriveMessages(rec.Events())
	if err != nil {
		return nil, nil, err
	}
	start, err := session.NewEvent(session.TypeTurnStart, session.TurnStart{Input: input, ExecutionKey: key})
	if err != nil {
		return nil, nil, err
	}
	_, err = rec.Append(ctx, start)
	return turn, history, err
}

// begin 记录本轮的 system 提示词与工具定义（与上次不同时）。
func (t *sessionTurn) begin(ctx context.Context, messages []llm.Message, offered []interfaces.Tool) error {
	if t == nil {
		return nil
	}
	if len(messages) > 0 && messages[0].Role == "system" {
		if err := t.rec.SyncSystem(ctx, messages[0].Content); err != nil {
			return err
		}
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
	d := session.TurnEnd{Answer: answer}
	if usage != nil {
		d.Usage = &session.TokenUsage{PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens, TotalTokens: usage.TotalTokens}
	}
	ev, err := session.NewEvent(session.TypeTurnEnd, d)
	if err != nil {
		return err
	}
	_, err = t.rec.Append(ctx, ev)
	return err
}
