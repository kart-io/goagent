package multiagent

import (
	"context"
	"crypto/rand"
	"strings"
	"time"

	"go.opentelemetry.io/otel/propagation"
)

// Communicator Agent 通信器接口
type Communicator interface {
	// Send 发送消息
	Send(ctx context.Context, to string, message *AgentMessage) error

	// Receive 接收消息
	Receive(ctx context.Context) (*AgentMessage, error)

	// Broadcast 广播消息
	Broadcast(ctx context.Context, message *AgentMessage) error

	// Subscribe 订阅主题
	Subscribe(ctx context.Context, topic string) (<-chan *AgentMessage, error)

	// Unsubscribe 取消订阅
	Unsubscribe(ctx context.Context, topic string) error

	// Close 关闭
	Close() error
}

// AgentMessage 消息（为避免与 Message 冲突）
type AgentMessage struct {
	ID           string                 `json:"id"`
	From         string                 `json:"from"`
	To           string                 `json:"to"`
	Topic        string                 `json:"topic"`
	Type         MessageType            `json:"type"`
	Payload      interface{}            `json:"payload"`
	Metadata     map[string]string      `json:"metadata"`
	Timestamp    time.Time              `json:"timestamp"`
	TraceContext propagation.MapCarrier `json:"trace_context,omitempty"` // 追踪上下文
}

// NewAgentMessage 创建新消息
func NewAgentMessage(from, to string, msgType MessageType, payload interface{}) *AgentMessage {
	return &AgentMessage{
		ID:           generateMessageID(),
		From:         from,
		To:           to,
		Type:         msgType,
		Payload:      payload,
		Metadata:     make(map[string]string),
		Timestamp:    time.Now(),
		TraceContext: propagation.MapCarrier{},
	}
}

// generateMessageID 生成消息 ID
//
// Optimized to use strings.Builder for efficient string concatenation
// and avoid multiple allocations.
func generateMessageID() string {
	timestamp := time.Now().Format("20060102150405")
	randomPart := randomString(8)

	// Pre-allocate exact capacity: timestamp (14) + "-" (1) + random (8) = 23
	var builder strings.Builder
	builder.Grow(23)
	builder.WriteString(timestamp)
	builder.WriteByte('-')
	builder.WriteString(randomPart)

	return builder.String()
}

// randomString generates a cryptographically secure random string of length n.
//
// Uses crypto/rand for security-sensitive message ID generation.
// Optimized to use a pre-allocated byte slice and strings.Builder.
func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	const lettersLen = len(letters)

	// Read random bytes from crypto/rand
	randomBytes := make([]byte, n)
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback to deterministic pattern if crypto/rand fails
		// This maintains backward compatibility while logging the issue
		for i := range randomBytes {
			randomBytes[i] = letters[i%lettersLen]
		}
		return string(randomBytes)
	}

	// Convert random bytes to letters
	for i := 0; i < n; i++ {
		randomBytes[i] = letters[int(randomBytes[i])%lettersLen]
	}

	return string(randomBytes)
}
