package multiagent

import (
	"context"
	"fmt"
	"sync"
)

// MemoryCommunicator 内存通信器（单机多Agent）
type MemoryCommunicator struct {
	agentID     string
	channels    map[string]chan *AgentMessage
	subscribers map[string][]chan *AgentMessage
	mu          sync.RWMutex
	closed      bool
}

// NewMemoryCommunicator 创建内存通信器
func NewMemoryCommunicator(agentID string) *MemoryCommunicator {
	return &MemoryCommunicator{
		agentID:     agentID,
		channels:    make(map[string]chan *AgentMessage),
		subscribers: make(map[string][]chan *AgentMessage),
	}
}

// Send 发送消息
func (c *MemoryCommunicator) Send(ctx context.Context, to string, message *AgentMessage) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return fmt.Errorf("communicator is closed")
	}

	ch, exists := c.channels[to]
	c.mu.RUnlock()

	if !exists {
		c.mu.Lock()
		ch = make(chan *AgentMessage, 100)
		c.channels[to] = ch
		c.mu.Unlock()
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
	c.mu.Lock()
	ch, exists := c.channels[c.agentID]
	if !exists {
		ch = make(chan *AgentMessage, 100)
		c.channels[c.agentID] = ch
	}
	c.mu.Unlock()

	select {
	case msg := <-ch:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Broadcast 广播消息
func (c *MemoryCommunicator) Broadcast(ctx context.Context, message *AgentMessage) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return fmt.Errorf("communicator is closed")
	}

	message.From = c.agentID

	for id, ch := range c.channels {
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
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil, fmt.Errorf("communicator is closed")
	}

	ch := make(chan *AgentMessage, 100)
	c.subscribers[topic] = append(c.subscribers[topic], ch)

	return ch, nil
}

// Unsubscribe 取消订阅
func (c *MemoryCommunicator) Unsubscribe(ctx context.Context, topic string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.subscribers, topic)
	return nil
}

// Close 关闭
func (c *MemoryCommunicator) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true

	for _, ch := range c.channels {
		close(ch)
	}

	for _, subs := range c.subscribers {
		for _, ch := range subs {
			close(ch)
		}
	}

	return nil
}
