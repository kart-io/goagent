package builder

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
)

// WithLongTermMemory 接入跨会话的长期记忆，需要 WithSessionLog，命名空间由宿主经 memory.WithNamespace 绑定在请求上下文中。
//   - 召回：每轮第一次模型请求前，以本轮输入召回记录，连同钉住的核心记忆作为一条低信任的用户消息插在输入之前（带身份的 PreStep，
//     持久运行回放时不再召回）。仅作用于原生工具流。
//   - 形成：每轮结束（含中止的轮次）在 m 中登记抽取任务，由宿主运行的 m.Run 异步处理；任务按"会话/轮次"幂等。
//
// 需要模型自行检索或保存时，另行注册 m.Tools()。
func (b *AgentBuilder[S]) WithLongTermMemory(m *memory.Memory) *AgentBuilder[S] {
	b.config.LongTermMemory = m
	if m == nil {
		return b
	}
	return b.WithIdentifiedPreStep("memory/"+m.Identity(), func(ctx context.Context, step *Step) (PreStepResult, error) {
		if step.Index != 0 || step.Inputs == 0 {
			return PreStepResult{}, nil
		}
		at := len(step.Messages) - step.Inputs
		var query []string
		for _, msg := range step.Messages[at:] {
			query = append(query, msg.Content)
		}
		block, err := m.RecallBlock(ctx, strings.Join(query, "\n"))
		if err != nil {
			return PreStepResult{}, fmt.Errorf("召回长期记忆: %w", err)
		}
		if block != "" {
			step.Messages = slices.Insert(slices.Clone(step.Messages), at, llm.UserMessage(block))
		}
		return PreStepResult{}, nil
	})
}

func (b *AgentBuilder[S]) validateLongTermMemory() error {
	if b.config.LongTermMemory != nil && b.config.SessionLog == nil {
		return agentErrors.NewInvalidConfigError("builder", "long_term_memory", "长期记忆需要 WithSessionLog：抽取任务从会话日志读取轮次")
	}
	return nil
}

// memoryJob 返回在轮次结束后登记抽取任务的回调；未配置长期记忆时为 nil。
func (a *ConfigurableAgent[S]) memoryJob(session string, start int64, aborted bool) func(context.Context, int64) error {
	m := a.config.LongTermMemory
	if m == nil {
		return nil
	}
	return func(ctx context.Context, end int64) error {
		err := m.Enqueue(ctx, memory.Job{
			Key: session + "/" + strconv.FormatInt(start, 10), Namespace: memory.NamespaceFrom(ctx),
			Session: session, From: start, To: end, Aborted: aborted,
		})
		if err != nil {
			return fmt.Errorf("登记长期记忆任务: %w", err)
		}
		return nil
	}
}
