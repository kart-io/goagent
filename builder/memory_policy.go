package builder

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/interfaces"
)

// MemoryFailurePolicy 指定对话记忆不可用时的处理方式。
type MemoryFailurePolicy string

const (
	// MemoryFailureBestEffort 保持默认兼容行为，观察失败后继续执行。
	MemoryFailureBestEffort MemoryFailurePolicy = ""
	// MemoryFailureStrict 在对话记忆读写失败时返回原始错误链。
	MemoryFailureStrict MemoryFailurePolicy = "strict"
)

// WithMemoryFailurePolicy 设置对话记忆失败策略；默认尽力模式。
func (b *AgentBuilder[C, S]) WithMemoryFailurePolicy(policy MemoryFailurePolicy) *AgentBuilder[C, S] {
	b.config.MemoryFailurePolicy = policy
	return b
}

// readMemory 与 writeMemory 为所有执行入口提供相同的失败策略。
func readMemory(ctx context.Context, manager interfaces.MemoryManager, config *AgentConfig) ([]*interfaces.Conversation, error) {
	if manager == nil || config.SessionID == "" {
		return nil, nil
	}
	memoryCtx, end := observe.Start(ctx, observe.Event{Type: "memory_read", Path: "memory/read"})
	history, err := manager.GetConversationHistory(memoryCtx, config.SessionID, config.MaxConversationHistory)
	end(err, false)
	if err != nil {
		return nil, memoryFailure(config, "read", err)
	}
	return history, nil
}

func writeMemory(ctx context.Context, manager interfaces.MemoryManager, config *AgentConfig, userInput, assistantResponse string) error {
	if manager == nil || config.SessionID == "" {
		return nil
	}
	for _, turn := range []struct{ role, content string }{{"user", userInput}, {"assistant", assistantResponse}} {
		memoryCtx, end := observe.Start(ctx, observe.Event{Type: "memory_write", Path: "memory/write"})
		err := manager.AddConversation(memoryCtx, &interfaces.Conversation{SessionID: config.SessionID, Role: turn.role, Content: turn.content})
		end(err, false)
		if err := memoryFailure(config, "write", err); err != nil {
			return err
		}
	}
	return nil
}

func memoryFailure(config *AgentConfig, operation string, err error) error {
	if err != nil && config.MemoryFailurePolicy == MemoryFailureStrict {
		return fmt.Errorf("conversation memory %s: %w", operation, err)
	}
	return nil
}
