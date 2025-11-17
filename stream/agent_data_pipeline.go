package stream

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
)

// DataPipelineAgent 数据流处理 Agent
//
// DataPipelineAgent 用于处理大数据集的渐进式处理：
// - 逐批次处理数据
// - 避免一次性加载所有数据到内存
// - 实时返回处理结果
type DataPipelineAgent struct {
	*core.BaseAgent
	config *DataPipelineConfig
}

// DataPipelineConfig 数据管道配置
type DataPipelineConfig struct {
	BatchSize        int           // 批次大小
	ProcessDelay     time.Duration // 处理延迟
	EnableProgress   bool          // 启用进度报告
	ProgressInterval time.Duration // 进度更新间隔
	MaxWorkers       int           // 最大工作协程数
}

// NewDataPipelineAgent 创建数据管道 Agent
func NewDataPipelineAgent(config *DataPipelineConfig) *DataPipelineAgent {
	if config == nil {
		config = DefaultDataPipelineConfig()
	}

	return &DataPipelineAgent{
		BaseAgent: core.NewBaseAgent(
			"data-pipeline-agent",
			"Agent for streaming data processing",
			[]string{"data", "pipeline", "streaming", "batch"},
		),
		config: config,
	}
}

// DefaultDataPipelineConfig 返回默认配置
func DefaultDataPipelineConfig() *DataPipelineConfig {
	return &DataPipelineConfig{
		BatchSize:        100,
		ProcessDelay:     100 * time.Millisecond,
		EnableProgress:   true,
		ProgressInterval: time.Second,
		MaxWorkers:       4,
	}
}

// Execute 同步执行
func (a *DataPipelineAgent) Execute(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	streamOutput, err := a.ExecuteStream(ctx, input)
	if err != nil {
		return nil, err
	}
	defer func() { _ = streamOutput.Close() }()

	reader := streamOutput.(*Reader)
	chunks, err := reader.Collect()
	if err != nil {
		return nil, err
	}

	return &core.AgentOutput{
		Result:    chunks,
		Status:    "success",
		Message:   fmt.Sprintf("Processed %d chunks", len(chunks)),
		Timestamp: time.Now(),
		Latency:   time.Since(input.Timestamp),
	}, nil
}

// ExecuteStream 流式执行
func (a *DataPipelineAgent) ExecuteStream(ctx context.Context, input *core.AgentInput) (core.StreamOutput, error) {
	opts := core.DefaultStreamOptions()
	opts.EnableProgress = a.config.EnableProgress
	opts.ProgressInterval = a.config.ProgressInterval

	writer := NewWriter(ctx, opts)

	// 启动异步处理
	go a.processDataPipeline(ctx, input, writer)

	reader := NewReader(ctx, writer.Channel(), opts)
	return reader, nil
}

// processDataPipeline 处理数据管道
func (a *DataPipelineAgent) processDataPipeline(ctx context.Context, input *core.AgentInput, writer *Writer) {
	defer func() {
		if err := writer.Close(); err != nil {
			fmt.Printf("failed to close writer: %v", err)
		}
	}()

	// 从输入获取数据源
	dataSource, ok := input.Context["data_source"].([]interface{})
	if !ok {
		_ = writer.WriteError(agentErrors.New(agentErrors.CodeInvalidConfig, "invalid data source").
			WithComponent("data_pipeline_agent").
			WithOperation("processDataPipeline"))
		return
	}

	totalItems := len(dataSource)
	var processedItems int
	startTime := time.Now()

	_ = writer.WriteStatus(fmt.Sprintf("Starting pipeline: %d items", totalItems))

	// 分批处理
	for i := 0; i < totalItems; i += a.config.BatchSize {
		select {
		case <-ctx.Done():
			if err := writer.WriteError(ctx.Err()); err != nil {
				fmt.Printf("failed to write error: %v", err)
			}
			return
		default:
		}

		end := i + a.config.BatchSize
		if end > totalItems {
			end = totalItems
		}

		batch := dataSource[i:end]

		// 处理批次
		result, err := a.processBatch(ctx, batch)
		if err != nil {
			_ = writer.WriteError(agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "batch processing failed").
				WithComponent("data_pipeline_agent").
				WithOperation("processDataPipeline"))
			return
		}

		// 发送结果
		chunk := &core.LegacyStreamChunk{
			Type: core.ChunkTypeJSON,
			Data: result,
			Metadata: core.ChunkMetadata{
				Timestamp: time.Now(),
				Current:   int64(end),
				Total:     int64(totalItems),
				Progress:  float64(end) / float64(totalItems) * 100,
			},
		}

		if err := writer.WriteChunk(chunk); err != nil {
			if err := writer.WriteError(err); err != nil {
				fmt.Printf("failed to write error: %v", err)
			}
			return
		}

		processedItems = end

		// 发送进度更新
		if a.config.EnableProgress {
			progress := float64(processedItems) / float64(totalItems) * 100
			if err := writer.WriteProgress(progress, fmt.Sprintf("Processed %d/%d items", processedItems, totalItems)); err != nil {
				fmt.Printf("failed to write progress: %v", err)
			}
		}

		// 处理延迟（避免过载）
		if a.config.ProcessDelay > 0 {
			time.Sleep(a.config.ProcessDelay)
		}
	}

	elapsed := time.Since(startTime)
	throughput := float64(totalItems) / elapsed.Seconds()

	_ = writer.WriteStatus(fmt.Sprintf("Pipeline complete: %d items in %v (%.2f items/sec)",
		totalItems, elapsed, throughput))
}

// processBatch 处理单个批次
func (a *DataPipelineAgent) processBatch(ctx context.Context, batch []interface{}) (map[string]interface{}, error) {
	// 模拟批次处理
	result := map[string]interface{}{
		"batch_size": len(batch),
		"items":      batch,
		"processed":  true,
		"timestamp":  time.Now(),
	}

	return result, nil
}

// ProcessWithTransform 使用转换函数处理数据
func (a *DataPipelineAgent) ProcessWithTransform(
	ctx context.Context,
	dataSource []interface{},
	transform func(interface{}) (interface{}, error),
) (core.StreamOutput, error) {
	opts := core.DefaultStreamOptions()
	writer := NewWriter(ctx, opts)

	go func() {
		defer func() { _ = writer.Close() }()

		totalItems := len(dataSource)
		for i, item := range dataSource {
			select {
			case <-ctx.Done():
				if err := writer.WriteError(ctx.Err()); err != nil {
					fmt.Printf("failed to write error: %v", err)
				}
				return
			default:
			}

			// 应用转换
			transformed, err := transform(item)
			if err != nil {
				if err := writer.WriteError(agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "transform failed").
					WithComponent("data_pipeline_agent").
					WithOperation("ProcessWithTransform").
					WithContext("item_index", i)); err != nil {
					fmt.Printf("failed to write error: %v", err)
				}
				return
			}

			// 发送结果
			chunk := &core.LegacyStreamChunk{
				Type: core.ChunkTypeJSON,
				Data: transformed,
				Metadata: core.ChunkMetadata{
					Timestamp: time.Now(),
					Current:   int64(i + 1),
					Total:     int64(totalItems),
					Progress:  float64(i+1) / float64(totalItems) * 100,
				},
			}

			if err := writer.WriteChunk(chunk); err != nil {
				_ = writer.WriteError(err)
				return
			}
		}
	}()

	reader := NewReader(ctx, writer.Channel(), opts)
	return reader, nil
}

// StreamFilter 流式过滤器
func (a *DataPipelineAgent) StreamFilter(
	ctx context.Context,
	source core.StreamOutput,
	filter func(*core.LegacyStreamChunk) bool,
) (core.StreamOutput, error) {
	opts := core.DefaultStreamOptions()
	writer := NewWriter(ctx, opts)

	go func() {
		defer func() {
			if err := writer.Close(); err != nil {
				fmt.Printf("failed to close writer: %v", err)
			}
		}()

		for {
			chunk, err := source.Next()
			if err != nil {
				break
			}

			// 应用过滤器
			if filter(chunk) {
				if err := writer.WriteChunk(chunk); err != nil {
					_ = writer.WriteError(err)
					return
				}
			}
		}
	}()

	reader := NewReader(ctx, writer.Channel(), opts)
	return reader, nil
}

// StreamMap 流式映射
func (a *DataPipelineAgent) StreamMap(
	ctx context.Context,
	source core.StreamOutput,
	mapper func(*core.LegacyStreamChunk) (*core.LegacyStreamChunk, error),
) (core.StreamOutput, error) {
	opts := core.DefaultStreamOptions()
	writer := NewWriter(ctx, opts)

	go func() {
		defer func() { _ = writer.Close() }()

		for {
			chunk, err := source.Next()
			if err != nil {
				break
			}

			// 应用映射
			mapped, err := mapper(chunk)
			if err != nil {
				if err := writer.WriteError(err); err != nil {
					fmt.Printf("failed to write error: %v", err)
				}
				return
			}

			if err := writer.WriteChunk(mapped); err != nil {
				_ = writer.WriteError(err)
				return
			}
		}
	}()

	reader := NewReader(ctx, writer.Channel(), opts)
	return reader, nil
}

// StreamReduce 流式归约
func (a *DataPipelineAgent) StreamReduce(
	ctx context.Context,
	source core.StreamOutput,
	initial interface{},
	reducer func(accumulator, current interface{}) (interface{}, error),
) (interface{}, error) {
	accumulator := initial
	var lastErr error

	for {
		chunk, err := source.Next()
		if err != nil {
			lastErr = err
			break
		}

		accumulator, err = reducer(accumulator, chunk.Data)
		if err != nil {
			return nil, err
		}
	}

	// 如果是 EOF，说明正常结束，返回 nil 错误
	if lastErr != nil && lastErr.Error() != "EOF" && lastErr.Error() != "context canceled" {
		return nil, lastErr
	}

	return accumulator, nil
}
