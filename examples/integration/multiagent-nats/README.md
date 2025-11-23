# NATS-Based Multi-Agent Communication Example

This example demonstrates how to implement a distributed multi-agent communication system using NATS as the message broker. It showcases the extensibility of the `ChannelStore` interface pattern.

**NEW:** Now includes AI-powered collaboration using DeepSeek LLM! 🤖

## Quick Start 🚀

### Option 1: Automated Test (Recommended)
```bash
# Run the complete test workflow (basic mode)
./test.sh test

# Run AI-powered collaboration (requires API key)
export DEEPSEEK_API_KEY='your-key-here'
./test.sh ai

# Or step by step
./test.sh start   # Start NATS
./test.sh run     # Run basic example
./test.sh ai      # Run AI collaboration (requires API key)
./test.sh logs    # View logs
./test.sh stop    # Stop NATS
```

### Option 2: Manual Setup
```bash
# Start NATS server
docker-compose up -d

# Verify NATS is running
docker ps | grep nats

# Run the basic example
NATS_URL=nats://localhost:4222 go run .

# Run AI collaboration
export DEEPSEEK_API_KEY='your-key-here'
MODE=ai NATS_URL=nats://localhost:4222 go run .

# Stop NATS
docker-compose down
```

### Option 3: Quick Docker
```bash
# Start NATS
docker run -d -p 4222:4222 --name nats-test nats:latest

# Run basic example
go run .

# Run AI collaboration
export DEEPSEEK_API_KEY='your-key-here'
MODE=ai go run .

# Stop NATS
docker stop nats-test && docker rm nats-test
```

## Overview

The example includes:
- **NATSChannelStore**: A custom implementation of the `ChannelStore` interface using NATS
- **Distributed Communication**: Agents can communicate across different processes and machines
- **Pub/Sub Pattern**: Support for topic-based messaging
- **Fallback Mode**: Graceful degradation to in-memory communication when NATS is unavailable
- **AI-Powered Collaboration**: Multiple AI agents with specialized roles collaborating via NATS (NEW!)

## Architecture

```
┌─────────────┐         ┌─────────────┐         ┌─────────────┐
│   Agent 1   │         │   Agent 2   │         │   Agent 3   │
│ (Process 1) │         │ (Process 2) │         │ (Process 3) │
└──────┬──────┘         └──────┬──────┘         └──────┬──────┘
       │                       │                        │
       │        ┌──────────────┴────────────┐          │
       └────────┤     NATS Server           ├──────────┘
                │  (Message Broker)         │
                └───────────────────────────┘
```

## Prerequisites

### Option 1: Docker (Recommended)
```bash
# Start NATS server
docker run -d -p 4222:4222 --name nats-server nats:latest

# Verify NATS is running
docker ps | grep nats
```

### Option 2: Native Installation
```bash
# macOS
brew install nats-server
nats-server

# Linux
wget https://github.com/nats-io/nats-server/releases/download/v2.10.7/nats-server-v2.10.7-linux-amd64.tar.gz
tar -xzf nats-server-v2.10.7-linux-amd64.tar.gz
./nats-server-v2.10.7-linux-amd64/nats-server
```

## Running the Example

### Basic Usage
```bash
cd examples/integration/multiagent-nats
go run .
```

### Custom NATS URL
```bash
NATS_URL=nats://your-nats-server:4222 go run .
```

### Fallback Mode (No NATS Required)
If NATS server is not available, the example automatically falls back to in-memory communication for demonstration purposes.

## Code Structure

### NATSChannelStore Implementation

```go
type NATSChannelStore struct {
    nc          *nats.Conn
    channels    map[string]chan *multiagent.AgentMessage
    subscribers map[string][]chan *multiagent.AgentMessage
    subs        map[string]*nats.Subscription
    mu          sync.RWMutex
}
```

**Key Methods:**
- `GetOrCreateChannel(agentID string)` - Creates a local channel and subscribes to NATS topic
- `PublishMessage(agentID, msg)` - Publishes message to NATS for specific agent
- `Subscribe(topic string)` - Subscribes to NATS topic for pub/sub pattern
- `PublishToTopic(topic, msg)` - Publishes to NATS topic

### Usage Pattern

```go
// Create NATS store
natsStore, err := NewNATSChannelStore("nats://localhost:4222")
if err != nil {
    log.Fatal(err)
}
defer natsStore.Close()

// Create communicator with NATS backend
agent1 := multiagent.NewMemoryCommunicatorWithStore("agent-1", natsStore)
agent2 := multiagent.NewMemoryCommunicatorWithStore("agent-2", natsStore)

// Send message (published via NATS)
msg := multiagent.NewAgentMessage("agent-1", "agent-2",
    multiagent.MessageTypeRequest, "Hello")
natsStore.PublishMessage("agent-2", msg)

// Receive message (from NATS subscription)
received, err := agent2.Receive(ctx)
```

## Features Demonstrated

### 1. Point-to-Point Communication
```go
// Agent 1 sends task to Agent 2
task := multiagent.NewAgentMessage("agent-1", "agent-2",
    multiagent.MessageTypeCommand, taskData)
natsStore.PublishMessage("agent-2", task)

// Agent 2 receives and responds
msg, _ := agent2Comm.Receive(ctx)
response := multiagent.NewAgentMessage("agent-2", "agent-1",
    multiagent.MessageTypeResponse, resultData)
natsStore.PublishMessage("agent-1", response)
```

### 2. Broadcast Communication
```go
// Coordinator broadcasts to all workers
broadcast := multiagent.NewAgentMessage("coordinator", "",
    multiagent.MessageTypeBroadcast, announcement)

for _, workerID := range workerIDs {
    natsStore.PublishMessage(workerID, broadcast)
}
```

### 3. Pub/Sub Pattern
```go
// Agents subscribe to alerts
alertCh, _ := agent.Subscribe(ctx, "alerts")

// Coordinator publishes alert
alert := multiagent.NewAgentMessage("coordinator", "",
    multiagent.MessageTypeNotification, alertData)
alert.Topic = "alerts"
natsStore.PublishToTopic("alerts", alert)

// Subscribers receive
msg := <-alertCh
```

## AI-Powered Collaboration (NEW!) 🤖

This example now includes a full AI collaboration scenario where multiple intelligent agents work together to solve complex problems using DeepSeek LLM.

### AI Agents Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     NATS Message Broker                      │
│                  (nats://localhost:4222)                     │
└────────────┬──────────────┬──────────────┬──────────────────┘
             │              │              │
             ▼              ▼              ▼
    ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
    │ Coordinator  │ │  Researcher  │ │ Tech Expert  │
    │   (AI)       │ │    (AI)      │ │    (AI)      │
    ├──────────────┤ ├──────────────┤ ├──────────────┤
    │ DeepSeek LLM │ │ DeepSeek LLM │ │ DeepSeek LLM │
    │ • Task split │ │ • Research   │ │ • Technical  │
    │ • Coordin.   │ │ • Analysis   │ │   Solutions  │
    │ • Synthesis  │ │ • Facts      │ │ • Best Prac. │
    └──────────────┘ └──────────────┘ └──────────────┘
```

### Running AI Collaboration

```bash
# 1. Get DeepSeek API Key
# Visit: https://platform.deepseek.com/
# Sign up and create an API key

# 2. Export API key
export DEEPSEEK_API_KEY='your-api-key-here'

# 3. Start NATS (if not running)
./test.sh start

# 4. Run AI collaboration
./test.sh ai

# Or manually
MODE=ai NATS_URL=nats://localhost:4222 go run .
```

### AI Agent Specializations

**1. Coordinator Agent**
- **Role**: Project management and orchestration
- **Capabilities**:
  - Break down complex tasks into subtasks
  - Assign tasks to appropriate specialists
  - Collect and synthesize results
  - Generate executive summaries
- **System Prompt**: Optimized for coordination and synthesis

**2. Research Agent**
- **Role**: Information gathering and analysis
- **Capabilities**:
  - Gather detailed research findings
  - Analyze information from multiple perspectives
  - Provide evidence-based responses
  - Verify facts and cite sources
- **System Prompt**: Optimized for thorough research

**3. Technical Expert Agent**
- **Role**: Technical problem solving
- **Capabilities**:
  - Provide technical solutions
  - Explain complex concepts clearly
  - Troubleshoot technical problems
  - Recommend best practices and tools
- **System Prompt**: Optimized for practical solutions

### Example Collaboration Scenario

The AI example demonstrates a real-world scenario:

**Problem**: Should we adopt microservices architecture?

**Workflow**:
1. **Coordinator** asks **Researcher** about microservices benefits and challenges
2. **Researcher** responds with detailed analysis (3 key points)
3. **Coordinator** asks **Tech Expert** for implementation recommendations
4. **Tech Expert** provides specific technology stack recommendations
5. **Coordinator** synthesizes all information into executive summary

**Sample Output**:
```
[coordinator] 📤 Sending request to researcher: What are the key benefits
              and challenges of microservices architecture?

[researcher] 📨 Received message from coordinator
[researcher] 🤖 Processing with AI...
[researcher] 💡 AI Response: Based on my research, microservices offer three
             key benefits: independent deployment, technology flexibility,
             and fault isolation. However, they also introduce complexity
             in distributed systems, require robust DevOps practices, and
             can lead to increased operational overhead...

[coordinator] 📤 Sending request to tech-expert: What technologies would
              you recommend for building a microservices system?

[tech-expert] 📨 Received message from coordinator
[tech-expert] 🤖 Processing with AI...
[tech-expert] 💡 AI Response: For microservices, I recommend: 1) Kubernetes
              for orchestration and scaling, 2) NATS or Kafka for async
              messaging, and 3) gRPC for efficient inter-service communication...

[coordinator] 📋 Executive Summary: After evaluating both research findings
              and technical recommendations, microservices architecture is
              recommended for teams with mature DevOps capabilities and
              complex domain requirements. Start with Kubernetes, implement
              NATS for messaging, and use gRPC for service communication...
```

### AI Collaboration Code Example

```go
// Create AI agent with specialized role
researcher := NewAIAgent(
    "researcher",
    "Research Specialist",
    `You are a Research Specialist AI Agent. Your role is to:
- Gather and analyze information
- Provide detailed research findings
- Cite sources and verify facts
Be thorough but concise.`,
    "Specializes in information gathering and analysis",
    llmClient,
    natsStore,
)

// Start listening for messages
go researcher.Listen(ctx, &wg)

// Coordinator sends request to researcher
response, err := coordinator.SendRequest(ctx, "researcher",
    "What are the key benefits of microservices?")

// Researcher processes with LLM and responds automatically
```

### Benefits of AI Multi-Agent System

✅ **Specialized Expertise**: Each agent has a specific role and optimized prompts
✅ **Distributed Processing**: Agents can run on different machines
✅ **Scalable Collaboration**: Easy to add more specialized agents
✅ **Real-time Communication**: Instant message passing via NATS
✅ **Fault Tolerant**: Agents can be restarted independently
✅ **Production Ready**: Built on enterprise-grade NATS messaging

### Adding Your Own AI Agents

```go
// Create custom AI agent
customAgent := NewAIAgent(
    "data-analyst",
    "Data Analysis Specialist",
    `You are a Data Analysis Specialist. Your role is to:
- Analyze datasets and identify patterns
- Create data visualizations recommendations
- Provide statistical insights
Be data-driven and precise.`,
    "Specializes in data analysis and statistics",
    llmClient,
    natsStore,
)

// Start listening
go customAgent.Listen(ctx, &wg)

// Other agents can now send requests to it
coordinator.SendRequest(ctx, "data-analyst", "Analyze sales trends")
```

## Extending to Other Backends

The same pattern can be used to implement other backends:

### Redis Example
```go
type RedisChannelStore struct {
    client *redis.Client
    // ... implementation
}

func (s *RedisChannelStore) GetOrCreateChannel(agentID string) chan *multiagent.AgentMessage {
    ch := make(chan *multiagent.AgentMessage, 100)

    // Subscribe to Redis pub/sub
    pubsub := s.client.Subscribe(ctx, fmt.Sprintf("agent:%s", agentID))
    go func() {
        for msg := range pubsub.Channel() {
            var agentMsg multiagent.AgentMessage
            json.Unmarshal([]byte(msg.Payload), &agentMsg)
            ch <- &agentMsg
        }
    }()

    return ch
}
```

### Kafka Example
```go
type KafkaChannelStore struct {
    producer sarama.SyncProducer
    consumer sarama.Consumer
    // ... implementation
}

func (s *KafkaChannelStore) PublishMessage(agentID string, msg *multiagent.AgentMessage) error {
    data, _ := json.Marshal(msg)
    _, _, err := s.producer.SendMessage(&sarama.ProducerMessage{
        Topic: fmt.Sprintf("agent-%s", agentID),
        Value: sarama.ByteEncoder(data),
    })
    return err
}
```

## Benefits

### Scalability
- NATS handles millions of messages per second
- Agents can be distributed across multiple machines
- Horizontal scaling by adding more agent processes

### Reliability
- Message persistence (with NATS JetStream)
- Automatic reconnection
- At-least-once delivery guarantees

### Flexibility
- Easy to swap storage backends
- No changes to agent code
- Clean separation of concerns

## Production Considerations

1. **NATS JetStream**: For production, use NATS JetStream for persistence:
   ```go
   js, _ := nc.JetStream()
   js.Publish("agent.messages", data)
   ```

2. **Error Handling**: Add robust error handling and retry logic

3. **Monitoring**: Integrate NATS monitoring for observability

4. **Security**: Enable TLS and authentication:
   ```go
   nats.Connect(url,
       nats.UserInfo("username", "password"),
       nats.Secure(&tls.Config{}))
   ```

5. **Message Ordering**: Use NATS queue groups for load balancing

## Troubleshooting

### NATS Connection Failed
```
⚠️ Failed to connect to NATS: dial tcp: connection refused
```
**Solution**: Ensure NATS server is running on the specified URL

### Messages Not Received
- Check NATS subscriptions are active before publishing
- Verify agent IDs match between sender and receiver
- Add delays for subscription setup

### Performance Issues
- Increase channel buffer size in `GetOrCreateChannel()`
- Use NATS JetStream for persistence
- Monitor NATS server metrics

## Next Steps

- Implement Redis-based ChannelStore
- Add message persistence with NATS JetStream
- Create multi-node deployment example
- Add comprehensive error handling
- Implement message acknowledgment patterns

## References

- [NATS Documentation](https://docs.nats.io/)
- [GoAgent ChannelStore Interface](../../multiagent/channel_store.go)
- [Multi-Agent Patterns](../../multiagent/)
