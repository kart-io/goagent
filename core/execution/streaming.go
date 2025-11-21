package execution

import (
	"context"
	"io"
	"time"
)

// StreamingAgent 支持流式输出的 Agent 接口
//
// StreamingAgent 扩展了 Agent 接口，提供流式输出能力：
// - 实时返回部分结果（如 LLM 逐字输出）
// - 处理大数据集的渐进式处理
// - 提供实时进度反馈
// - 支持长时间运行任务的中间结果
//
// Note: This is a legacy interface. New code should use the Runnable[I,O].Stream() pattern
// from the core package with generic StreamChunk[T] types.
type StreamingAgent interface {
	// ExecuteStream 以流式方式执行 Agent 逻辑
	// 返回一个可以持续读取输出块的 StreamOutput
	ExecuteStream(ctx context.Context, input interface{}) (StreamOutput, error)
}

// StreamOutput 流式输出接口
//
// StreamOutput 提供流式数据读取能力：
// - 通过 Next() 逐块读取数据
// - 支持暂停、恢复、取消
// - 提供进度和状态更新
type StreamOutput interface {
	// Next 读取下一个数据块
	// 返回 io.EOF 表示流结束
	Next() (*LegacyStreamChunk, error)

	// Close 关闭流并释放资源
	Close() error

	// IsClosed 检查流是否已关闭
	IsClosed() bool

	// Context 返回流的上下文
	Context() context.Context
}

// LegacyStreamChunk 流数据块
//
// LegacyStreamChunk 表示流中的一个数据单元：
// - 可以是文本片段、JSON 对象、二进制数据等
// - 包含类型信息和元数据
// - 支持进度和状态信息
//
// 注意：这是旧的流式输出实现，新代码应使用 Runnable[I,O].Stream() 和泛型 StreamChunk[T]
type LegacyStreamChunk struct {
	// 数据类型
	Type ChunkType `json:"type"` // 数据类型

	// 数据内容
	Data interface{} `json:"data"`           // 实际数据
	Text string      `json:"text,omitempty"` // 文本数据（仅 Type=ChunkTypeText 时）

	// 元数据
	Metadata ChunkMetadata `json:"metadata"` // 元数据

	// 控制信息
	IsLast bool  `json:"is_last"`         // 是否是最后一个块
	Error  error `json:"error,omitempty"` // 错误信息（如果有）
}

// ChunkType 数据块类型
type ChunkType string

const (
	ChunkTypeText     ChunkType = "text"     // 文本数据
	ChunkTypeJSON     ChunkType = "json"     // JSON 数据
	ChunkTypeBinary   ChunkType = "binary"   // 二进制数据
	ChunkTypeProgress ChunkType = "progress" // 进度更新
	ChunkTypeStatus   ChunkType = "status"   // 状态更新
	ChunkTypeError    ChunkType = "error"    // 错误信息
	ChunkTypeMetadata ChunkType = "metadata" // 元数据
	ChunkTypeControl  ChunkType = "control"  // 控制命令
)

// ChunkMetadata 数据块元数据
type ChunkMetadata struct {
	// 序列信息
	Sequence  int64     `json:"sequence"`  // 序列号
	Timestamp time.Time `json:"timestamp"` // 时间戳
	Source    string    `json:"source"`    // 数据源

	// 进度信息
	Progress float64       `json:"progress,omitempty"` // 进度百分比 (0-100)
	Current  int64         `json:"current,omitempty"`  // 当前处理项
	Total    int64         `json:"total,omitempty"`    // 总项数
	ETA      time.Duration `json:"eta,omitempty"`      // 预计剩余时间

	// 状态信息
	Status string `json:"status,omitempty"` // 状态描述
	Phase  string `json:"phase,omitempty"`  // 处理阶段

	// 性能信息
	Latency    time.Duration `json:"latency,omitempty"`    // 延迟
	Throughput float64       `json:"throughput,omitempty"` // 吞吐量

	// 扩展信息
	Extra map[string]interface{} `json:"extra,omitempty"` // 额外元数据
}

// StreamOptions 流配置选项
//
// StreamOptions 控制流的行为：
// - 缓冲和性能设置
// - 超时和重试配置
// - 背压和流控制
type StreamOptions struct {
	// 缓冲配置
	BufferSize   int  `json:"buffer_size,omitempty"`   // 缓冲区大小（块数）
	EnableBuffer bool `json:"enable_buffer,omitempty"` // 是否启用缓冲

	// 超时配置
	ChunkTimeout  time.Duration `json:"chunk_timeout,omitempty"`  // 单个块的超时
	StreamTimeout time.Duration `json:"stream_timeout,omitempty"` // 整个流的超时

	// 重试配置
	MaxRetries   int           `json:"max_retries,omitempty"`    // 最大重试次数
	RetryDelay   time.Duration `json:"retry_delay,omitempty"`    // 重试延迟
	RetryOnError bool          `json:"retry_on_error,omitempty"` // 错误时是否重试

	// 背压控制
	EnableBackpressure bool `json:"enable_backpressure,omitempty"` // 是否启用背压
	BackpressureWindow int  `json:"backpressure_window,omitempty"` // 背压窗口大小
	MaxPendingChunks   int  `json:"max_pending_chunks,omitempty"`  // 最大待处理块数

	// 流控制
	EnableThrottle  bool          `json:"enable_throttle,omitempty"`    // 是否启用限流
	MaxChunksPerSec float64       `json:"max_chunks_per_sec,omitempty"` // 最大块速率
	MinChunkDelay   time.Duration `json:"min_chunk_delay,omitempty"`    // 最小块间隔

	// 监控配置
	EnableProgress   bool          `json:"enable_progress,omitempty"`   // 是否发送进度更新
	ProgressInterval time.Duration `json:"progress_interval,omitempty"` // 进度更新间隔

	// 转换配置
	EnableTransform bool               `json:"enable_transform,omitempty"` // 是否启用数据转换
	TransformFunc   ChunkTransformFunc `json:"-"`                          // 数据转换函数

	// 多路复用
	EnableMultiplex bool `json:"enable_multiplex,omitempty"` // 是否支持多个消费者
	MaxConsumers    int  `json:"max_consumers,omitempty"`    // 最大消费者数

	// 内存保护
	MaxCollectSize int64 `json:"max_collect_size,omitempty"` // Collect/CollectText 最大字节数限制（默认 100MB），防止 OOM
}

// ChunkTransformFunc 数据块转换函数
type ChunkTransformFunc func(*LegacyStreamChunk) (*LegacyStreamChunk, error)

// DefaultStreamOptions 返回默认流选项
func DefaultStreamOptions() *StreamOptions {
	return &StreamOptions{
		BufferSize:         100,
		EnableBuffer:       true,
		ChunkTimeout:       5 * time.Second,
		StreamTimeout:      5 * time.Minute,
		MaxRetries:         3,
		RetryDelay:         time.Second,
		RetryOnError:       true,
		EnableBackpressure: true,
		BackpressureWindow: 10,
		MaxPendingChunks:   1000,
		EnableThrottle:     false,
		EnableProgress:     true,
		ProgressInterval:   time.Second,
		EnableMultiplex:    false,
		MaxConsumers:       10,
		MaxCollectSize:     100 * 1024 * 1024, // 100MB default limit
	}
}

// StreamStatus 流状态
type StreamStatus struct {
	State       StreamState   `json:"state"`                // 流状态
	ChunksRead  int64         `json:"chunks_read"`          // 已读取块数
	BytesRead   int64         `json:"bytes_read"`           // 已读取字节数
	StartTime   time.Time     `json:"start_time"`           // 开始时间
	ElapsedTime time.Duration `json:"elapsed_time"`         // 已用时间
	Progress    float64       `json:"progress"`             // 进度百分比
	ErrorCount  int           `json:"error_count"`          // 错误计数
	LastError   error         `json:"last_error,omitempty"` // 最后的错误
}

// StreamState 流状态枚举
type StreamState string

const (
	StreamStateIdle     StreamState = "idle"     // 空闲
	StreamStateRunning  StreamState = "running"  // 运行中
	StreamStatePaused   StreamState = "paused"   // 暂停
	StreamStateError    StreamState = "error"    // 错误
	StreamStateComplete StreamState = "complete" // 完成
	StreamStateClosed   StreamState = "closed"   // 已关闭
)

// StreamWriter 流写入器接口
//
// StreamWriter 用于生成流数据：
// - Agent 通过 StreamWriter 向流中写入数据
// - 支持批量写入和控制信号
type StreamWriter interface {
	io.Writer

	// WriteChunk 写入数据块
	WriteChunk(chunk *LegacyStreamChunk) error

	// WriteBatch 批量写入数据块
	WriteBatch(chunks []*LegacyStreamChunk) error

	// WriteText 写入文本数据
	WriteText(text string) error

	// WriteProgress 写入进度更新
	WriteProgress(progress float64, message string) error

	// WriteStatus 写入状态更新
	WriteStatus(status string) error

	// WriteError 写入错误信息
	WriteError(err error) error

	// Close 关闭写入器并标记流结束
	Close() error

	// IsClosed 检查写入器是否已关闭
	IsClosed() bool
}

// StreamController 流控制器接口
//
// StreamController 提供流的控制能力：
// - 暂停、恢复、取消流
// - 查询流状态
// - 流的监控和调试
type StreamController interface {
	// Pause 暂停流
	Pause() error

	// Resume 恢复流
	Resume() error

	// Cancel 取消流
	Cancel() error

	// Status 获取流状态
	Status() *StreamStatus

	// IsRunning 检查流是否运行中
	IsRunning() bool

	// IsPaused 检查流是否暂停
	IsPaused() bool
}

// StreamConsumer 流消费者接口
//
// StreamConsumer 定义流数据的消费者：
// - 接收流中的数据块
// - 处理流事件（开始、结束、错误）
type StreamConsumer interface {
	// OnChunk 处理数据块
	OnChunk(chunk *LegacyStreamChunk) error

	// OnStart 流开始时调用
	OnStart() error

	// OnComplete 流完成时调用
	OnComplete() error

	// OnError 发生错误时调用
	OnError(err error) error
}

// StreamMultiplexer 流多路复用器接口
//
// StreamMultiplexer 支持多个消费者同时读取同一个流：
// - 广播数据到多个消费者
// - 管理消费者的生命周期
type StreamMultiplexer interface {
	// AddConsumer 添加消费者
	AddConsumer(consumer StreamConsumer) (id string, err error)

	// RemoveConsumer 移除消费者
	RemoveConsumer(id string) error

	// Consumers 返回所有消费者
	Consumers() []string

	// Start 开始多路复用
	Start(ctx context.Context, source StreamOutput) error

	// Close 关闭多路复用器
	Close() error
}

// NewStreamChunk 创建新的流数据块
func NewStreamChunk(typ ChunkType, data interface{}) *LegacyStreamChunk {
	return &LegacyStreamChunk{
		Type: typ,
		Data: data,
		Metadata: ChunkMetadata{
			Timestamp: time.Now(),
		},
	}
}

// NewTextChunk 创建文本数据块
func NewTextChunk(text string) *LegacyStreamChunk {
	return &LegacyStreamChunk{
		Type: ChunkTypeText,
		Text: text,
		Data: text,
		Metadata: ChunkMetadata{
			Timestamp: time.Now(),
		},
	}
}

// NewProgressChunk 创建进度数据块
func NewProgressChunk(progress float64, message string) *LegacyStreamChunk {
	return &LegacyStreamChunk{
		Type: ChunkTypeProgress,
		Data: map[string]interface{}{
			"progress": progress,
			"message":  message,
		},
		Metadata: ChunkMetadata{
			Timestamp: time.Now(),
			Progress:  progress,
			Status:    message,
		},
	}
}

// NewErrorChunk 创建错误数据块
func NewErrorChunk(err error) *LegacyStreamChunk {
	return &LegacyStreamChunk{
		Type:  ChunkTypeError,
		Data:  err.Error(),
		Error: err,
		Metadata: ChunkMetadata{
			Timestamp: time.Now(),
		},
	}
}
