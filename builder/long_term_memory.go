package builder

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
)

// WithLongTermMemory 接入跨会话的长期记忆，需要 WithSessionLog，命名空间由宿主经 memory.WithNamespace 绑定在请求上下文中。
//   - 绑定：每轮开始时确定记忆绑定（memory.TurnMemory），记入 turn/start；恢复本轮时沿用它，宿主改绑了命名空间时
//     返回 ErrMemoryNamespaceChanged。
//   - 召回：每轮第一次模型请求前以本轮输入召回，持久日志只记引用（memory/recall、memory/use/<步骤>）。召回块在组装
//     每步请求时作为一条低信任的用户消息插在运行上下文之前，不进入会话历史与持久日志。仅作用于原生工具流。
//     Request 钩子不得改写或移动召回消息，否则持久运行恢复时意图不一致。
//   - 形成：每轮结束（含中止的轮次）按本轮绑定在 m 中登记抽取任务，由宿主运行的 m.Run 异步处理；任务按"会话/轮次"幂等。
//
// 需要模型自行检索或保存时，另行注册 m.Tools()。不能与 middleware.CacheMiddleware 同时使用：它按输入缓存回答，
// 会跨命名空间命中。
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
	for _, mw := range b.middlewares {
		if _, ok := mw.(*middleware.CacheMiddleware); ok {
			return agentErrors.NewInvalidConfigError("builder", "long_term_memory", "长期记忆不能与 CacheMiddleware 同时使用：缓存按输入命中，会把一个命名空间的回答返回给另一个")
		}
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
		return msg.Role == "user" && strings.HasPrefix(msg.Content, memory.RecallPrefix)
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
