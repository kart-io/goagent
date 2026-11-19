package builder

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
)

// WithLongTermMemory attaches cross-session long-term memory. It requires
// WithSessionLog; the host binds the namespace in the request context via
// memory.WithNamespace.
//   - Binding: at the start of each turn the memory binding (memory.TurnMemory)
//     is determined and recorded in turn/start; resuming the turn reuses it,
//     and if the host rebinds the namespace it returns
//     ErrMemoryNamespaceChanged.
//   - Recall: before the first model request of each turn, memories are
//     recalled using the turn's input; the durable journal records only
//     references (memory/recall, memory/use/<step>). When each step's request
//     is assembled, the recall block is inserted as one low-trust user message
//     before the run context; it does not enter the session history or the
//     durable journal. It applies only to the native tool stream. Request hooks
//     must not rewrite or move the recall message; otherwise the intent will
//     not match when a durable run resumes.
//   - Formation: at the end of each turn (including aborted turns), an
//     extraction task is registered in m under the turn's binding and processed
//     asynchronously by the host-run m.Run; tasks are idempotent per
//     session/turn.
//
// To let the model search or save memories itself, register m.Tools()
// separately.
func (b *AgentBuilder[S]) WithLongTermMemory(m *memory.Memory) *AgentBuilder[S] {
	b.config.LongTermMemory = m
	return b
}

func (b *AgentBuilder[S]) validateLongTermMemory() error {
	if b.config.LongTermMemory == nil {
		return nil
	}
	if b.config.SessionLog == nil {
		return agentErrors.NewInvalidConfigError("builder", "long_term_memory", "长期记忆需要 WithSessionLog：抽取任务从会话日志读取轮次")
	}
	return nil
}

// turnMemory 返回本轮的记忆绑定：stored 非空时是已记录的绑定（恢复的轮次），核对宿主的命名空间未变；
// 否则按宿主当前的绑定生成。未配置长期记忆时为 nil。
func (a *ConfigurableAgent[S]) turnMemory(ctx context.Context, stored json.RawMessage) (*memory.TurnMemory, error) {
	m := a.config.LongTermMemory
	if m == nil {
		return nil, nil
	}
	if len(stored) > 0 {
		var t memory.TurnMemory
		if err := json.Unmarshal(stored, &t); err != nil {
			return nil, fmt.Errorf("轮次的记忆绑定：%w", err)
		}
		if err := memory.CheckTurn(ctx, t); err != nil {
			return nil, err
		}
		return &t, nil
	}
	t, err := m.Turn(ctx)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// deriveHistory 从会话日志推导历史；配置长期记忆时去掉升级前写入历史的召回块。
func (a *ConfigurableAgent[S]) deriveHistory(events []session.Event) ([]llm.Message, error) {
	history, err := session.DeriveMessages(events)
	if err != nil || a.config.LongTermMemory == nil {
		return history, err
	}
	return slices.DeleteFunc(history, func(msg llm.Message) bool {
		return msg.Role == constants.RoleUser && strings.HasPrefix(msg.Content, memory.RecallPrefix)
	}), nil
}

// memoryJob 返回在轮次结束后按绑定 t 登记抽取任务的回调；未配置长期记忆或没有绑定时为 nil。
func (a *ConfigurableAgent[S]) memoryJob(session string, start int64, aborted bool, t *memory.TurnMemory) func(context.Context, int64) error {
	m := a.config.LongTermMemory
	if m == nil || t == nil {
		return nil
	}
	return func(ctx context.Context, end int64) error {
		err := m.Enqueue(ctx, memory.Job{
			Key: session + "/" + strconv.FormatInt(start, 10), Namespace: slices.Clone(t.Write),
			Session: session, From: start, To: end, Aborted: aborted, Scope: t.Scope, TurnSeq: t.Seq,
		})
		if err != nil {
			return fmt.Errorf("登记长期记忆任务: %w", err)
		}
		return nil
	}
}
