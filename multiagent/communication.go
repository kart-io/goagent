package multiagent

import (
	"context"
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
func generateMessageID() string {
	return time.Now().Format("20060102150405") + "-" + randomString(8)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[i%len(letters)]
	}
	return string(b)
}
