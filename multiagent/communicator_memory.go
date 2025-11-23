package multiagent

import (
	"context"
	"sync"

	agentErrors "github.com/kart-io/goagent/errors"
)

// 全局共享的 channels map，让所有 MemoryCommunicator 实例可以互相通信
var (
	globalChannels    = make(map[string]chan *AgentMessage)
	globalSubscribers = make(map[string][]chan *AgentMessage)
	globalMu          sync.RWMutex
)

// MemoryCommunicator 内存通信器（单机多Agent）
type MemoryCommunicator struct {
	agentID string
	closed  bool
	closeMu sync.RWMutex
}

// NewMemoryCommunicator 创建内存通信器
func NewMemoryCommunicator(agentID string) *MemoryCommunicator {
	return &MemoryCommunicator{
		agentID: agentID,
	}
}

// Send 发送消息
func (c *MemoryCommunicator) Send(ctx context.Context, to string, message *AgentMessage) error {
	c.closeMu.RLock()
	if c.closed {
		c.closeMu.RUnlock()
		return agentErrors.New(agentErrors.CodeInvalidConfig, "communicator is closed").
			WithComponent("memory_communicator").
			WithOperation("send").
			WithContext("to", to)
	}
	c.closeMu.RUnlock()

	globalMu.RLock()
	ch, exists := globalChannels[to]
	globalMu.RUnlock()

	if !exists {
		globalMu.Lock()
		// Double-check after acquiring write lock
		if ch, exists = globalChannels[to]; !exists {
			ch = make(chan *AgentMessage, 100)
			globalChannels[to] = ch
		}
		globalMu.Unlock()
	}

	message.From = c.agentID
	message.To = to

	select {
	case ch <- message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Receive 接收消息
func (c *MemoryCommunicator) Receive(ctx context.Context) (*AgentMessage, error) {
	globalMu.Lock()
	ch, exists := globalChannels[c.agentID]
	if !exists {
		ch = make(chan *AgentMessage, 100)
		globalChannels[c.agentID] = ch
	}
	globalMu.Unlock()

	select {
	case msg := <-ch:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Broadcast 广播消息
func (c *MemoryCommunicator) Broadcast(ctx context.Context, message *AgentMessage) error {
	c.closeMu.RLock()
	if c.closed {
		c.closeMu.RUnlock()
		return agentErrors.New(agentErrors.CodeInvalidConfig, "communicator is closed").
			WithComponent("memory_communicator").
			WithOperation("broadcast")
	}
	c.closeMu.RUnlock()

	message.From = c.agentID

	globalMu.RLock()
	defer globalMu.RUnlock()

	for id, ch := range globalChannels {
		if id != c.agentID {
			select {
			case ch <- message:
			default:
				// Channel full, skip
			}
		}
	}

	return nil
}

// Subscribe 订阅主题
func (c *MemoryCommunicator) Subscribe(ctx context.Context, topic string) (<-chan *AgentMessage, error) {
	c.closeMu.RLock()
	if c.closed {
		c.closeMu.RUnlock()
		return nil, agentErrors.New(agentErrors.CodeInvalidConfig, "communicator is closed").
			WithComponent("memory_communicator").
			WithOperation("subscribe").
			WithContext("topic", topic)
	}
	c.closeMu.RUnlock()

	globalMu.Lock()
	defer globalMu.Unlock()

	ch := make(chan *AgentMessage, 100)
	globalSubscribers[topic] = append(globalSubscribers[topic], ch)

	return ch, nil
}

// Unsubscribe 取消订阅
func (c *MemoryCommunicator) Unsubscribe(ctx context.Context, topic string) error {
	globalMu.Lock()
	defer globalMu.Unlock()

	delete(globalSubscribers, topic)
	return nil
}

// Close 关闭
func (c *MemoryCommunicator) Close() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true

	// Note: We don't close channels in the global map because
	// other communicators might still be using them.
	// In a real application, you'd want proper lifecycle management.

	return nil
}
