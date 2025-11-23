# Redis ChannelStore Implementation Example

This is a conceptual example showing how to implement a Redis-based `ChannelStore`.

## Implementation Sketch

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/go-redis/redis/v8"
	"github.com/kart-io/goagent/multiagent"
)

// RedisChannelStore implements ChannelStore using Redis Pub/Sub
type RedisChannelStore struct {
	client      *redis.Client
	channels    map[string]chan *multiagent.AgentMessage
	subscribers map[string][]chan *multiagent.AgentMessage
	mu          sync.RWMutex
	ctx         context.Context
}

// NewRedisChannelStore creates a Redis-based ChannelStore
func NewRedisChannelStore(redisURL string) (*RedisChannelStore, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid Redis URL: %w", err)
	}

	client := redis.NewClient(opt)

	// Test connection
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisChannelStore{
		client:      client,
		channels:    make(map[string]chan *multiagent.AgentMessage),
		subscribers: make(map[string][]chan *multiagent.AgentMessage),
		ctx:         context.Background(),
	}, nil
}

// GetOrCreateChannel subscribes to Redis channel for agent messages
func (s *RedisChannelStore) GetOrCreateChannel(agentID string) chan *multiagent.AgentMessage {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ch, exists := s.channels[agentID]; exists {
		return ch
	}

	ch := make(chan *multiagent.AgentMessage, 100)
	s.channels[agentID] = ch

	// Subscribe to Redis channel
	channelName := fmt.Sprintf("agent:%s:messages", agentID)
	pubsub := s.client.Subscribe(s.ctx, channelName)

	// Start goroutine to receive messages
	go func() {
		for msg := range pubsub.Channel() {
			var agentMsg multiagent.AgentMessage
			if err := json.Unmarshal([]byte(msg.Payload), &agentMsg); err != nil {
				continue
			}

			select {
			case ch <- &agentMsg:
			default:
				// Channel full, drop message
			}
		}
	}()

	return ch
}

// PublishMessage publishes message to Redis channel
func (s *RedisChannelStore) PublishMessage(agentID string, msg *multiagent.AgentMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	channelName := fmt.Sprintf("agent:%s:messages", agentID)
	return s.client.Publish(s.ctx, channelName, data).Err()
}

// GetChannel returns existing channel or nil
func (s *RedisChannelStore) GetChannel(agentID string) chan *multiagent.AgentMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.channels[agentID]
}

// ListChannels returns all agent IDs
func (s *RedisChannelStore) ListChannels() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agentIDs := make([]string, 0, len(s.channels))
	for id := range s.channels {
		agentIDs = append(agentIDs, id)
	}
	return agentIDs
}

// Subscribe subscribes to a topic
func (s *RedisChannelStore) Subscribe(topic string) <-chan *multiagent.AgentMessage {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan *multiagent.AgentMessage, 100)
	s.subscribers[topic] = append(s.subscribers[topic], ch)

	// Subscribe to Redis topic
	topicName := fmt.Sprintf("topic:%s", topic)
	pubsub := s.client.Subscribe(s.ctx, topicName)

	go func() {
		for msg := range pubsub.Channel() {
			var agentMsg multiagent.AgentMessage
			if err := json.Unmarshal([]byte(msg.Payload), &agentMsg); err != nil {
				continue
			}

			s.mu.RLock()
			subs := s.subscribers[topic]
			s.mu.RUnlock()

			for _, subCh := range subs {
				select {
				case subCh <- &agentMsg:
				default:
				}
			}
		}
	}()

	return ch
}

// Unsubscribe removes subscription
func (s *RedisChannelStore) Unsubscribe(topic string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if subs, exists := s.subscribers[topic]; exists {
		for _, ch := range subs {
			close(ch)
		}
		delete(s.subscribers, topic)
	}
}

// GetSubscribers returns subscribers for topic
func (s *RedisChannelStore) GetSubscribers(topic string) []chan *multiagent.AgentMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.subscribers[topic]
}

// Close closes all connections
func (s *RedisChannelStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, ch := range s.channels {
		close(ch)
	}

	for _, subs := range s.subscribers {
		for _, ch := range subs {
			close(ch)
		}
	}

	return s.client.Close()
}
```

## Usage

```go
// Create Redis store
redisStore, err := NewRedisChannelStore("redis://localhost:6379")
if err != nil {
    log.Fatal(err)
}
defer redisStore.Close()

// Create communicators
agent1 := multiagent.NewMemoryCommunicatorWithStore("agent-1", redisStore)
agent2 := multiagent.NewMemoryCommunicatorWithStore("agent-2", redisStore)

// Send message
msg := multiagent.NewAgentMessage("agent-1", "agent-2",
    multiagent.MessageTypeRequest, "Hello via Redis")
redisStore.PublishMessage("agent-2", msg)

// Receive
received, _ := agent2.Receive(ctx)
fmt.Printf("Received: %v\n", received.Payload)
```

## Running Redis

```bash
# Docker
docker run -d -p 6379:6379 redis:alpine

# Verify
redis-cli ping
```

## Key Differences from NATS

### NATS
- Lightweight, in-memory messaging
- Better for real-time communication
- Lower latency
- Simpler deployment

### Redis
- Persistent storage available
- Rich data structures (lists, sets, hashes)
- Can combine messaging with caching
- Wider ecosystem

## When to Use Each

**Use NATS when:**
- Pure messaging system needed
- Highest performance required
- Multi-datacenter deployment
- Complex routing patterns

**Use Redis when:**
- Already using Redis for caching
- Need message persistence
- Want to leverage Redis data structures
- Simpler infrastructure preferred

## Performance Considerations

### Redis Pub/Sub
- Excellent for fan-out scenarios
- Messages not persisted by default
- Use Redis Streams for persistence
- Can handle 100K+ messages/second

### Redis Streams (Alternative)
```go
// Using Redis Streams for persistence
func (s *RedisChannelStore) PublishMessageStream(agentID string, msg *multiagent.AgentMessage) error {
    data, _ := json.Marshal(msg)
    streamName := fmt.Sprintf("agent:{%s}:stream", agentID)

    return s.client.XAdd(s.ctx, &redis.XAddArgs{
        Stream: streamName,
        Values: map[string]interface{}{
            "message": data,
        },
    }).Err()
}

// Consumer group for processing
func (s *RedisChannelStore) ConsumeStream(agentID string) {
    streamName := fmt.Sprintf("agent:{%s}:stream", agentID)
    groupName := fmt.Sprintf("{%s}:group", agentID)

    // Create consumer group
    s.client.XGroupCreateMkStream(s.ctx, streamName, groupName, "0")

    // Read messages
    for {
        entries, err := s.client.XReadGroup(s.ctx, &redis.XReadGroupArgs{
            Group:    groupName,
            Consumer: agentID,
            Streams:  []string{streamName, ">"},
            Count:    10,
            Block:    0,
        }).Result()

        if err != nil {
            continue
        }

        for _, entry := range entries {
            for _, message := range entry.Messages {
                // Process message
                // Acknowledge
                s.client.XAck(s.ctx, streamName, groupName, message.ID)
            }
        }
    }
}
```

## Next Steps

- Implement connection pooling
- Add retry mechanisms
- Implement Redis Streams for persistence
- Add monitoring and metrics
- Handle network partitions

## References

- [Redis Pub/Sub](https://redis.io/docs/manual/pubsub/)
- [Redis Streams](https://redis.io/docs/data-types/streams/)
- [go-redis Library](https://github.com/go-redis/redis)
