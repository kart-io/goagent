package multiagent

import "sync"

// ChannelStore 定义 channel 存储接口，允许第三方实现不同的存储后端
// 例如：内存、Redis、NATS、Kafka 等
type ChannelStore interface {
	// GetOrCreateChannel 获取或创建指定 agent 的消息 channel
	GetOrCreateChannel(agentID string) chan *AgentMessage

	// GetChannel 获取指定 agent 的消息 channel，如果不存在返回 nil
	GetChannel(agentID string) chan *AgentMessage

	// ListChannels 列出所有 channel 的 agent ID
	ListChannels() []string

	// Subscribe 订阅主题，返回接收消息的 channel
	Subscribe(topic string) <-chan *AgentMessage

	// Unsubscribe 取消订阅主题
	Unsubscribe(topic string)

	// GetSubscribers 获取指定主题的所有订阅者 channel
	GetSubscribers(topic string) []chan *AgentMessage

	// Close 关闭存储（可选的清理操作）
	Close() error
}

// InMemoryChannelStore 内存实现的 ChannelStore
type InMemoryChannelStore struct {
	channels    map[string]chan *AgentMessage
	subscribers map[string][]chan *AgentMessage
	mu          sync.RWMutex
}

// NewInMemoryChannelStore 创建内存 channel 存储
func NewInMemoryChannelStore() *InMemoryChannelStore {
	return &InMemoryChannelStore{
		channels:    make(map[string]chan *AgentMessage),
		subscribers: make(map[string][]chan *AgentMessage),
	}
}

// GetOrCreateChannel 实现 ChannelStore 接口
func (s *InMemoryChannelStore) GetOrCreateChannel(agentID string) chan *AgentMessage {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ch, exists := s.channels[agentID]; exists {
		return ch
	}

	ch := make(chan *AgentMessage, 100)
	s.channels[agentID] = ch
	return ch
}

// GetChannel 实现 ChannelStore 接口
func (s *InMemoryChannelStore) GetChannel(agentID string) chan *AgentMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.channels[agentID]
}

// ListChannels 实现 ChannelStore 接口
func (s *InMemoryChannelStore) ListChannels() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agentIDs := make([]string, 0, len(s.channels))
	for id := range s.channels {
		agentIDs = append(agentIDs, id)
	}
	return agentIDs
}

// Subscribe 实现 ChannelStore 接口
func (s *InMemoryChannelStore) Subscribe(topic string) <-chan *AgentMessage {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan *AgentMessage, 100)
	s.subscribers[topic] = append(s.subscribers[topic], ch)
	return ch
}

// Unsubscribe 实现 ChannelStore 接口
func (s *InMemoryChannelStore) Unsubscribe(topic string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subscribers, topic)
}

// GetSubscribers 实现 ChannelStore 接口
func (s *InMemoryChannelStore) GetSubscribers(topic string) []chan *AgentMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.subscribers[topic]
}

// Close 实现 ChannelStore 接口
func (s *InMemoryChannelStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 关闭所有 channel
	for _, ch := range s.channels {
		close(ch)
	}

	// 关闭所有订阅者 channel
	for _, subs := range s.subscribers {
		for _, ch := range subs {
			close(ch)
		}
	}

	return nil
}
