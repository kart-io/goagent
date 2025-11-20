package multiagent

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/kart-io/goagent/utils/json"
	"sync"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/nats-io/nats.go"
)

// NATSCommunicator NATS 通信器（分布式）
type NATSCommunicator struct {
	agentID string
	conn    *nats.Conn
	js      nats.JetStreamContext
	subs    map[string]*nats.Subscription
	mu      sync.RWMutex
}

// NATSConfig NATS 配置
type NATSConfig struct {
	URL         string
	ClusterID   string
	Credentials string
	TLS         *tls.Config
}

// NewNATSCommunicator 创建 NATS 通信器
func NewNATSCommunicator(agentID string, config *NATSConfig) (*NATSCommunicator, error) {
	opts := []nats.Option{
		nats.Name(agentID),
	}

	if config.TLS != nil {
		opts = append(opts, nats.Secure(config.TLS))
	}

	if config.Credentials != "" {
		opts = append(opts, nats.UserCredentials(config.Credentials))
	}

	conn, err := nats.Connect(config.URL, opts...)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeStoreConnection, "failed to connect to NATS").
			WithComponent("nats_communicator").
			WithOperation("new_communicator").
			WithContext("url", config.URL)
	}

	js, err := conn.JetStream()
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeStoreConnection, "failed to create JetStream context").
			WithComponent("nats_communicator").
			WithOperation("new_communicator")
	}

	return &NATSCommunicator{
		agentID: agentID,
		conn:    conn,
		js:      js,
		subs:    make(map[string]*nats.Subscription),
	}, nil
}

// Send 发送消息
func (c *NATSCommunicator) Send(ctx context.Context, to string, message *AgentMessage) error {
	message.From = c.agentID
	message.To = to

	data, err := json.Marshal(message)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeDistributedSerialization, "failed to marshal message").
			WithComponent("nats_communicator").
			WithOperation("send").
			WithContext("to", to)
	}

	subject := fmt.Sprintf("agent.%s.inbox", to)
	return c.conn.Publish(subject, data)
}

// Receive 接收消息
func (c *NATSCommunicator) Receive(ctx context.Context) (*AgentMessage, error) {
	// NATS 使用订阅模式，这里返回错误
	return nil, agentErrors.New(agentErrors.CodeNotImplemented, "use Subscribe instead for NATS communicator").
		WithComponent("nats_communicator").
		WithOperation("receive")
}

// Broadcast 广播消息
func (c *NATSCommunicator) Broadcast(ctx context.Context, message *AgentMessage) error {
	message.From = c.agentID

	data, err := json.Marshal(message)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeDistributedSerialization, "failed to marshal message").
			WithComponent("nats_communicator").
			WithOperation("broadcast")
	}

	subject := "agent.broadcast"
	return c.conn.Publish(subject, data)
}

// Subscribe 订阅主题
func (c *NATSCommunicator) Subscribe(ctx context.Context, topic string) (<-chan *AgentMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ch := make(chan *AgentMessage, 100)

	sub, err := c.conn.Subscribe(topic, func(msg *nats.Msg) {
		var message AgentMessage
		if err := json.Unmarshal(msg.Data, &message); err == nil {
			select {
			case ch <- &message:
			default:
				// Channel full
			}
		}
	})
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeDistributedConnection, "failed to subscribe").
			WithComponent("nats_communicator").
			WithOperation("subscribe").
			WithContext("topic", topic)
	}

	c.subs[topic] = sub
	return ch, nil
}

// Unsubscribe 取消订阅
func (c *NATSCommunicator) Unsubscribe(ctx context.Context, topic string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if sub, exists := c.subs[topic]; exists {
		if err := sub.Unsubscribe(); err != nil {
			return err
		}
		delete(c.subs, topic)
	}

	return nil
}

// Close 关闭
func (c *NATSCommunicator) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for topic, sub := range c.subs {
		_ = sub.Unsubscribe()
		delete(c.subs, topic)
	}

	if c.conn != nil {
		c.conn.Close()
	}

	return nil
}
