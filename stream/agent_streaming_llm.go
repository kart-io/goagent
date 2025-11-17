package stream

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

// StreamingLLMAgent LLM 流式响应 Agent
//
// StreamingLLMAgent 支持 LLM 的流式输出：
// - 逐字返回生成的文本
// - 实时显示思考过程
// - 降低首字符延迟
type StreamingLLMAgent struct {
	*core.BaseAgent
	llmClient llm.Client
	config    *StreamingLLMConfig
}

// StreamingLLMConfig LLM 流式配置
type StreamingLLMConfig struct {
	// LLM 配置
	Model       string
	Temperature float64
	MaxTokens   int

	// 流式配置
	ChunkSize        int           // 每次发送的字符数
	ChunkDelay       time.Duration // 块之间的延迟（模拟打字效果）
	EnableProgress   bool          // 是否发送进度更新
	ProgressInterval time.Duration // 进度更新间隔

	// 错误处理
	RetryOnError bool
	MaxRetries   int
}

// NewStreamingLLMAgent 创建 LLM 流式 Agent
func NewStreamingLLMAgent(llmClient llm.Client, config *StreamingLLMConfig) *StreamingLLMAgent {
	if config == nil {
		config = DefaultStreamingLLMConfig()
	}

	return &StreamingLLMAgent{
		BaseAgent: core.NewBaseAgent(
			"streaming-llm-agent",
			"LLM agent with streaming support",
			[]string{"llm", "chat", "streaming"},
		),
		llmClient: llmClient,
		config:    config,
	}
}

// DefaultStreamingLLMConfig 返回默认配置
func DefaultStreamingLLMConfig() *StreamingLLMConfig {
	return &StreamingLLMConfig{
		Model:            "gpt-3.5-turbo",
		Temperature:      0.7,
		MaxTokens:        2000,
		ChunkSize:        10,
		ChunkDelay:       50 * time.Millisecond,
		EnableProgress:   true,
		ProgressInterval: time.Second,
		RetryOnError:     true,
		MaxRetries:       3,
	}
}

// Execute 同步执行（兼容 Agent 接口）
func (a *StreamingLLMAgent) Execute(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	// 通过流式执行并收集所有结果
	streamOutput, err := a.ExecuteStream(ctx, input)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := streamOutput.Close(); err != nil {
			fmt.Printf("failed to close stream output: %v", err)
		}
	}()

	reader, ok := streamOutput.(*Reader)
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInternal, "invalid stream output type").
			WithComponent("streaming_llm_agent").
			WithOperation("Execute")
	}

	// 收集所有文本
	text, err := reader.CollectText()
	if err != nil {
		return nil, err
	}

	return &core.AgentOutput{
		Result:    text,
		Status:    "success",
		Message:   "LLM response generated",
		Timestamp: time.Now(),
		Latency:   time.Since(input.Timestamp),
	}, nil
}

// ExecuteStream 流式执行
func (a *StreamingLLMAgent) ExecuteStream(ctx context.Context, input *core.AgentInput) (core.StreamOutput, error) {
	// 创建流
	opts := core.DefaultStreamOptions()
	opts.EnableProgress = a.config.EnableProgress
	opts.ProgressInterval = a.config.ProgressInterval

	writer := NewWriter(ctx, opts)

	// 启动异步处理
	go a.processStreamAsync(ctx, input, writer)

	// 返回 Reader
	reader := NewReader(ctx, writer.Channel(), opts)
	return reader, nil
}

// processStreamAsync 异步处理流式输出
func (a *StreamingLLMAgent) processStreamAsync(ctx context.Context, input *core.AgentInput, writer *Writer) {
	defer func() { _ = writer.Close() }()

	startTime := time.Now()

	// 准备 LLM 请求
	messages := []llm.Message{
		llm.UserMessage(input.Task),
	}

	if input.Instruction != "" {
		messages = append([]llm.Message{llm.SystemMessage(input.Instruction)}, messages...)
	}

	// 发送状态更新
	_ = writer.WriteStatus("Starting LLM generation...")

	// 调用 LLM（这里模拟流式输出，实际需要 LLM 客户端支持）
	response, err := a.llmClient.Chat(ctx, messages)
	if err != nil {
		if err := writer.WriteError(agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "LLM call failed").
			WithComponent("streaming_llm_agent").
			WithOperation("processStreamAsync")); err != nil {
			fmt.Printf("failed to write error: %v", err)
		}
		return
	}

	// 模拟流式输出（将响应分块发送）
	text := response.Content
	totalChunks := (len(text) + a.config.ChunkSize - 1) / a.config.ChunkSize

	for i := 0; i < len(text); i += a.config.ChunkSize {
		// 检查上下文取消
		select {
		case <-ctx.Done():
			_ = writer.WriteError(ctx.Err())
			return
		default:
		}

		end := i + a.config.ChunkSize
		if end > len(text) {
			end = len(text)
		}

		chunk := text[i:end]

		// 发送文本块
		if err := writer.WriteText(chunk); err != nil {
			if err := writer.WriteError(err); err != nil {
				fmt.Printf("failed to write error: %v", err)
			}
			return
		}

		// 发送进度更新
		if a.config.EnableProgress {
			progress := float64(i+a.config.ChunkSize) / float64(len(text)) * 100
			if progress > 100 {
				progress = 100
			}
			chunkNum := i/a.config.ChunkSize + 1
			if err := writer.WriteProgress(progress, fmt.Sprintf("Chunk %d/%d", chunkNum, totalChunks)); err != nil {
				fmt.Printf("failed to write progress: %v", err)
			}
		}

		// 添加延迟（模拟打字效果）
		if a.config.ChunkDelay > 0 {
			time.Sleep(a.config.ChunkDelay)
		}
	}

	// 发送完成状态
	elapsed := time.Since(startTime)
	_ = writer.WriteStatus(fmt.Sprintf("Completed in %v", elapsed))
}

// StreamingLLMAgentWithRealStreaming 支持真实 LLM 流式 API 的 Agent
//
// 注意：这需要 LLM 客户端支持真实的流式 API（如 OpenAI streaming API）
type StreamingLLMAgentWithRealStreaming struct {
	*StreamingLLMAgent
}

// NewStreamingLLMAgentWithRealStreaming 创建支持真实流式的 Agent
func NewStreamingLLMAgentWithRealStreaming(llmClient llm.Client, config *StreamingLLMConfig) *StreamingLLMAgentWithRealStreaming {
	return &StreamingLLMAgentWithRealStreaming{
		StreamingLLMAgent: NewStreamingLLMAgent(llmClient, config),
	}
}

// ExecuteStream 使用真实的流式 API
func (a *StreamingLLMAgentWithRealStreaming) ExecuteStream(ctx context.Context, input *core.AgentInput) (core.StreamOutput, error) {
	opts := core.DefaultStreamOptions()
	writer := NewWriter(ctx, opts)

	go func() {
		defer func() { _ = writer.Close() }()

		// TODO: 实现真实的 LLM 流式 API 调用
		// 这需要 LLM 客户端支持类似以下的接口：
		//
		// streamChan, err := a.llmClient.ChatStream(ctx, messages)
		// for chunk := range streamChan {
		//     writer.WriteText(chunk.Text)
		// }

		if err := writer.WriteError(agentErrors.New(agentErrors.CodeInternal, "real streaming API not implemented yet").
			WithComponent("streaming_llm_agent").
			WithOperation("ExecuteStream")); err != nil {
			fmt.Printf("failed to write error: %v", err)
		}
	}()

	reader := NewReader(ctx, writer.Channel(), opts)
	return reader, nil
}

// SimpleStreamConsumer 简单的流消费者实现
type SimpleStreamConsumer struct {
	OnChunkFunc    func(*core.LegacyStreamChunk) error
	OnStartFunc    func() error
	OnCompleteFunc func() error
	OnErrorFunc    func(error) error
}

func (c *SimpleStreamConsumer) OnChunk(chunk *core.LegacyStreamChunk) error {
	if c.OnChunkFunc != nil {
		return c.OnChunkFunc(chunk)
	}
	return nil
}

func (c *SimpleStreamConsumer) OnStart() error {
	if c.OnStartFunc != nil {
		return c.OnStartFunc()
	}
	return nil
}

func (c *SimpleStreamConsumer) OnComplete() error {
	if c.OnCompleteFunc != nil {
		return c.OnCompleteFunc()
	}
	return nil
}

func (c *SimpleStreamConsumer) OnError(err error) error {
	if c.OnErrorFunc != nil {
		return c.OnErrorFunc(err)
	}
	return nil
}

// TextAccumulatorConsumer 累积文本的消费者
type TextAccumulatorConsumer struct {
	builder strings.Builder
	mu      chan struct{}
}

func NewTextAccumulatorConsumer() *TextAccumulatorConsumer {
	return &TextAccumulatorConsumer{
		mu: make(chan struct{}, 1),
	}
}

func (c *TextAccumulatorConsumer) OnChunk(chunk *core.LegacyStreamChunk) error {
	if chunk.Type == core.ChunkTypeText && chunk.Text != "" {
		c.mu <- struct{}{}
		c.builder.WriteString(chunk.Text)
		<-c.mu
	}
	return nil
}

func (c *TextAccumulatorConsumer) OnStart() error {
	c.builder.Reset()
	return nil
}

func (c *TextAccumulatorConsumer) OnComplete() error {
	return nil
}

func (c *TextAccumulatorConsumer) OnError(err error) error {
	return err
}

func (c *TextAccumulatorConsumer) Text() string {
	return c.builder.String()
}
