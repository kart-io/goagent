package main

import (
	"fmt"
	"sync"

	"github.com/kart-io/goagent/multiagent"
	"github.com/kart-io/goagent/utils/json"
	"github.com/nats-io/nats.go"
)

// NATSChannelStore 实现基于 NATS 的分布式 ChannelStore
// 支持跨进程、跨机器的 Agent 通信
type NATSChannelStore struct {
	nc          *nats.Conn
	channels    map[string]chan *multiagent.AgentMessage
	subscribers map[string][]chan *multiagent.AgentMessage
	subs        map[string]*nats.Subscription
	mu          sync.RWMutex
}

// NewNATSChannelStore 创建 NATS ChannelStore
func NewNATSChannelStore(natsURL string) (*NATSChannelStore, error) {
	nc, err := nats.Connect(natsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	store := &NATSChannelStore{
		nc:          nc,
		channels:    make(map[string]chan *multiagent.AgentMessage),
		subscribers: make(map[string][]chan *multiagent.AgentMessage),
		subs:        make(map[string]*nats.Subscription),
	}

	return store, nil
}

// GetOrCreateChannel 获取或创建指定 agent 的消息 channel
func (s *NATSChannelStore) GetOrCreateChannel(agentID string) chan *multiagent.AgentMessage {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ch, exists := s.channels[agentID]; exists {
		return ch
	}

	ch := make(chan *multiagent.AgentMessage, 100)
	s.channels[agentID] = ch

	// 订阅 NATS 主题，接收发给该 agent 的消息
	subject := fmt.Sprintf("agent.%s.messages", agentID)
	sub, err := s.nc.Subscribe(subject, func(msg *nats.Msg) {
		var agentMsg multiagent.AgentMessage
		if err := json.Unmarshal(msg.Data, &agentMsg); err != nil {
			fmt.Printf("Failed to unmarshal message: %v\n", err)
			return
		}

		// 将消息发送到本地 channel
		select {
		case ch <- &agentMsg:
		default:
			// Channel full, drop message
			fmt.Printf("Warning: channel full for agent %s, dropping message\n", agentID)
		}
	})
	if err != nil {
		fmt.Printf("Failed to subscribe to %s: %v\n", subject, err)
		return ch
	}

	s.subs[agentID] = sub
	return ch
}

// GetChannel 获取指定 agent 的消息 channel，如果不存在返回 nil
func (s *NATSChannelStore) GetChannel(agentID string) chan *multiagent.AgentMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.channels[agentID]
}

// ListChannels 列出所有 channel 的 agent ID
func (s *NATSChannelStore) ListChannels() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agentIDs := make([]string, 0, len(s.channels))
	for id := range s.channels {
		agentIDs = append(agentIDs, id)
	}
	return agentIDs
}

// Subscribe 订阅主题，返回接收消息的 channel
func (s *NATSChannelStore) Subscribe(topic string) <-chan *multiagent.AgentMessage {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan *multiagent.AgentMessage, 100)
	s.subscribers[topic] = append(s.subscribers[topic], ch)

	// 订阅 NATS 主题
	subject := fmt.Sprintf("topic.%s", topic)
	sub, err := s.nc.Subscribe(subject, func(msg *nats.Msg) {
		var agentMsg multiagent.AgentMessage
		if err := json.Unmarshal(msg.Data, &agentMsg); err != nil {
			fmt.Printf("Failed to unmarshal topic message: %v\n", err)
			return
		}

		// 将消息发送到所有订阅者的 channel
		s.mu.RLock()
		subs := s.subscribers[topic]
		s.mu.RUnlock()

		for _, subCh := range subs {
			select {
			case subCh <- &agentMsg:
			default:
				// Channel full, skip
			}
		}
	})

	if err != nil {
		fmt.Printf("Failed to subscribe to topic %s: %v\n", subject, err)
	} else {
		s.subs[subject] = sub
	}

	return ch
}

// Unsubscribe 取消订阅主题
func (s *NATSChannelStore) Unsubscribe(topic string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 取消 NATS 订阅
	subject := fmt.Sprintf("topic.%s", topic)
	if sub, exists := s.subs[subject]; exists {
		if err := sub.Unsubscribe(); err != nil {
			fmt.Printf("Warning: failed to unsubscribe from %s: %v\n", subject, err)
		}
		delete(s.subs, subject)
	}

	// 关闭所有订阅者的 channel
	if subs, exists := s.subscribers[topic]; exists {
		for _, ch := range subs {
			close(ch)
		}
		delete(s.subscribers, topic)
	}
}

// GetSubscribers 获取指定主题的所有订阅者 channel
func (s *NATSChannelStore) GetSubscribers(topic string) []chan *multiagent.AgentMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.subscribers[topic]
}

// Close 关闭存储
func (s *NATSChannelStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 取消所有 NATS 订阅
	for _, sub := range s.subs {
		if err := sub.Unsubscribe(); err != nil {
			fmt.Printf("Warning: failed to unsubscribe: %v\n", err)
		}
	}

	// 关闭所有 channels
	for _, ch := range s.channels {
		close(ch)
	}

	// 关闭所有订阅者 channels
	for _, subs := range s.subscribers {
		for _, ch := range subs {
			close(ch)
		}
	}

	// 关闭 NATS 连接
	if s.nc != nil {
		s.nc.Close()
	}

	return nil
}

// PublishMessage 发布消息到 NATS（供 MemoryCommunicator 使用）
func (s *NATSChannelStore) PublishMessage(agentID string, msg *multiagent.AgentMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	subject := fmt.Sprintf("agent.%s.messages", agentID)
	return s.nc.Publish(subject, data)
}

// PublishToTopic 发布消息到主题
func (s *NATSChannelStore) PublishToTopic(topic string, msg *multiagent.AgentMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	subject := fmt.Sprintf("topic.%s", topic)
	return s.nc.Publish(subject, data)
}
