package builder

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
)

// recallSlot 是一步请求中的召回消息：实际请求带正文，持久意图中换成由日志子集决定的占位消息，
// 使意图只由日志决定，恢复时引用被删除、召回消息消失也不改变意图。
type recallSlot struct {
	// text 是插入请求的召回消息正文，为空时请求中没有召回消息。
	text string
	// placeholder 是意图中代替召回消息的内容；日志中本步的引用子集为空时为空。
	placeholder string
	// index 是召回消息在组装后请求中的位置。
	index int
}

// snapshotRecall 在 PreStep 之前以本轮输入（末尾 inputs 条消息）召回；持久运行中只把引用记入 memory/recall。
func (a *ConfigurableAgent[S]) snapshotRecall(ctx context.Context, messages []llm.Message, inputs int) (*memory.Snapshot, error) {
	m := a.config.LongTermMemory
	if m == nil || inputs <= 0 {
		return nil, nil
	}
	var query []string
	for _, msg := range messages[max(len(messages)-inputs, 0):] {
		query = append(query, msg.Content)
	}
	s, err := journaled(ctx, "memory/recall", func(context.Context) (memory.Snapshot, error) {
		return m.Snapshot(ctx, strings.Join(query, "\n"))
	})
	if err != nil {
		return nil, fmt.Errorf("召回长期记忆: %w", err)
	}
	return &s, nil
}

// hydrateRecall 还原第 round 步的召回块，持久运行中把采用的引用子集记入 memory/use/<round>。
// 回放时按日志中的子集宽松还原：已删除的项不再发送。
func (a *ConfigurableAgent[S]) hydrateRecall(ctx context.Context, s *memory.Snapshot, round int) (recallSlot, error) {
	if s == nil {
		return recallSlot{}, nil
	}
	m := a.config.LongTermMemory
	var fresh *memory.Retrieval
	refs, err := journaled(ctx, fmt.Sprintf("memory/use/%d", round), func(context.Context) ([]memory.Ref, error) {
		r, adopted, err := m.Hydrate(ctx, *s, s.Refs)
		fresh = &r
		return adopted, err
	})
	if err != nil {
		return recallSlot{}, fmt.Errorf("还原长期记忆: %w", err)
	}
	if fresh == nil {
		r, _, err := m.Hydrate(ctx, *s, refs)
		if err != nil {
			return recallSlot{}, fmt.Errorf("还原长期记忆: %w", err)
		}
		fresh = &r
	}
	slot := recallSlot{text: fresh.Text}
	if len(refs) > 0 {
		digest, err := s.Digest(refs)
		if err != nil {
			return recallSlot{}, err
		}
		slot.placeholder = memory.RecallPrefix + digest
	}
	return slot, nil
}

// recallIntent 返回意图使用的消息：去掉召回消息（按正文相等认出），子集非空时在原位置放占位消息。
func recallIntent(msgs []llm.Message, slot recallSlot) []llm.Message {
	if slot.text == "" && slot.placeholder == "" {
		return msgs
	}
	out := slices.Clone(msgs)
	at := min(slot.index, len(out))
	if slot.text != "" {
		if i := slices.IndexFunc(out, func(m llm.Message) bool { return m.Role == "user" && m.Content == slot.text }); i >= 0 {
			at, out = i, slices.Delete(out, i, i+1)
		}
	}
	if slot.placeholder != "" {
		out = slices.Insert(out, at, llm.UserMessage(slot.placeholder))
	}
	return out
}
