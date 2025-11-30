package multiagent

import (
	"context"
	"sync"

	agentErrors "github.com/kart-io/goagent/errors"
)

// 全局默认的 ChannelStore 实例
var (
	defaultStore ChannelStore
	defaultOnce  sync.Once
)

// getDefaultStore 获取全局默认的 ChannelStore
func getDefaultStore() ChannelStore {
	defaultOnce.Do(func() {
		defaultStore = NewInMemoryChannelStore()
	})
	return defaultStore
}

// SetDefaultChannelStore 设置全局默认的 ChannelStore（用于第三方实现）
func SetDefaultChannelStore(store ChannelStore) {
	defaultStore = store
}

// MemoryCommunicator 内存通信器（单机多Agent）
type MemoryCommunicator struct {
	agentID string
	store   ChannelStore
	closed  bool
	closeMu sync.RWMutex
}

// NewMemoryCommunicator 创建内存通信器，使用全局默认的 ChannelStore
func NewMemoryCommunicator(agentID string) *MemoryCommunicator {
	return &MemoryCommunicator{
		agentID: agentID,
		store:   getDefaultStore(),
	}
}

// NewMemoryCommunicatorWithStore 创建内存通信器，使用指定的 ChannelStore
func NewMemoryCommunicatorWithStore(agentID string, store ChannelStore) *MemoryCommunicator {
	return &MemoryCommunicator{
		agentID: agentID,
		store:   store,
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

	ch := c.store.GetOrCreateChannel(to)

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
	ch := c.store.GetOrCreateChannel(c.agentID)

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

	// 获取所有 channel 的 ID
	agentIDs := c.store.ListChannels()

	for _, id := range agentIDs {
		if id != c.agentID {
			ch := c.store.GetChannel(id)
			if ch != nil {
				select {
				case ch <- message:
				default:
					// Channel full, skip
				}
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

	return c.store.Subscribe(topic), nil
}

// Unsubscribe 取消订阅
func (c *MemoryCommunicator) Unsubscribe(ctx context.Context, topic string) error {
	c.store.Unsubscribe(topic)
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

	// Note: We don't close the store here because
	// other communicators might still be using the shared store.
	// In a real application, you'd want proper lifecycle management.

	return nil
}
