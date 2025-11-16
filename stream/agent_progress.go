package stream

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/core"
)

// ProgressAgent 带进度反馈的 Agent
//
// ProgressAgent 用于长时间运行的任务：
// - 实时进度更新
// - 阶段性状态报告
// - ETA 预估
type ProgressAgent struct {
	*core.BaseAgent
	config *ProgressConfig
}

// ProgressConfig 进度配置
type ProgressConfig struct {
	EnableProgress   bool          // 启用进度报告
	ProgressInterval time.Duration // 进度更新间隔
	EnableETA        bool          // 启用 ETA 计算
	EnablePhases     bool          // 启用阶段报告
}

// NewProgressAgent 创建进度 Agent
func NewProgressAgent(config *ProgressConfig) *ProgressAgent {
	if config == nil {
		config = DefaultProgressConfig()
	}

	return &ProgressAgent{
		BaseAgent: core.NewBaseAgent(
			"progress-agent",
			"Agent with real-time progress tracking",
			[]string{"progress", "monitoring", "streaming"},
		),
		config: config,
	}
}

// DefaultProgressConfig 返回默认配置
func DefaultProgressConfig() *ProgressConfig {
	return &ProgressConfig{
		EnableProgress:   true,
		ProgressInterval: time.Second,
		EnableETA:        true,
		EnablePhases:     true,
	}
}

// Execute 同步执行
func (a *ProgressAgent) Execute(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	streamOutput, err := a.ExecuteStream(ctx, input)
	if err != nil {
		return nil, err
	}
	defer streamOutput.Close()

	// 收集所有进度和结果
	reader := streamOutput.(*Reader)
	var lastProgress float64
	var result interface{}

	for {
		chunk, err := reader.Next()
		if err != nil {
			break
		}

		switch chunk.Type {
		case core.ChunkTypeProgress:
			lastProgress = chunk.Metadata.Progress
		case core.ChunkTypeJSON:
			result = chunk.Data
		}
	}

	return &core.AgentOutput{
		Result:    result,
		Status:    "success",
		Message:   fmt.Sprintf("Task completed (%.1f%%)", lastProgress),
		Timestamp: time.Now(),
		Latency:   time.Since(input.Timestamp),
	}, nil
}

// ExecuteStream 流式执行
func (a *ProgressAgent) ExecuteStream(ctx context.Context, input *core.AgentInput) (core.StreamOutput, error) {
	opts := core.DefaultStreamOptions()
	opts.EnableProgress = a.config.EnableProgress
	opts.ProgressInterval = a.config.ProgressInterval

	writer := NewWriter(ctx, opts)

	// 启动异步处理
	go a.processWithProgress(ctx, input, writer)

	reader := NewReader(ctx, writer.Channel(), opts)
	return reader, nil
}

// processWithProgress 带进度的处理
func (a *ProgressAgent) processWithProgress(ctx context.Context, input *core.AgentInput, writer *Writer) {
	defer writer.Close()

	// 从输入获取任务配置
	totalSteps := 100
	if steps, ok := input.Context["total_steps"].(int); ok {
		totalSteps = steps
	}

	stepDuration := 100 * time.Millisecond
	if duration, ok := input.Context["step_duration"].(time.Duration); ok {
		stepDuration = duration
	}

	startTime := time.Now()
	phases := []string{"Initialization", "Processing", "Validation", "Finalization"}
	currentPhase := 0

	writer.WriteStatus(fmt.Sprintf("Starting task: %d steps", totalSteps))

	// 执行任务
	for step := 0; step < totalSteps; step++ {
		select {
		case <-ctx.Done():
			writer.WriteError(ctx.Err())
			return
		default:
		}

		// 更新阶段
		if a.config.EnablePhases {
			newPhase := step * len(phases) / totalSteps
			if newPhase != currentPhase && newPhase < len(phases) {
				currentPhase = newPhase
				writer.WriteStatus(fmt.Sprintf("Phase: %s", phases[currentPhase]))
			}
		}

		// 计算进度
		progress := float64(step+1) / float64(totalSteps) * 100

		// 计算 ETA
		var eta time.Duration
		if a.config.EnableETA && step > 0 {
			elapsed := time.Since(startTime)
			avgTimePerStep := elapsed / time.Duration(step)
			remainingSteps := totalSteps - step
			eta = avgTimePerStep * time.Duration(remainingSteps)
		}

		// 发送进度更新
		if a.config.EnableProgress {
			chunk := &core.LegacyStreamChunk{
				Type: core.ChunkTypeProgress,
				Data: map[string]interface{}{
					"progress": progress,
					"step":     step + 1,
					"total":    totalSteps,
					"phase":    phases[currentPhase],
					"eta":      eta.String(),
				},
				Metadata: core.ChunkMetadata{
					Timestamp: time.Now(),
					Progress:  progress,
					Current:   int64(step + 1),
					Total:     int64(totalSteps),
					ETA:       eta,
					Phase:     phases[currentPhase],
				},
			}

			if err := writer.WriteChunk(chunk); err != nil {
				writer.WriteError(err)
				return
			}
		}

		// 模拟处理延迟
		time.Sleep(stepDuration)
	}

	// 发送最终结果
	result := map[string]interface{}{
		"status":       "completed",
		"total_steps":  totalSteps,
		"elapsed_time": time.Since(startTime).String(),
		"completion":   "100%",
	}

	resultChunk := &core.LegacyStreamChunk{
		Type: core.ChunkTypeJSON,
		Data: result,
		Metadata: core.ChunkMetadata{
			Timestamp: time.Now(),
			Progress:  100,
			Status:    "completed",
		},
	}

	writer.WriteChunk(resultChunk)
	writer.WriteStatus("Task completed successfully")
}

// ProgressTracker 进度跟踪器
type ProgressTracker struct {
	total     int64
	current   atomic.Int64
	startTime time.Time
	writer    *Writer
	config    *ProgressConfig
}

// NewProgressTracker 创建进度跟踪器
func NewProgressTracker(total int64, writer *Writer, config *ProgressConfig) *ProgressTracker {
	return &ProgressTracker{
		total:     total,
		startTime: time.Now(),
		writer:    writer,
		config:    config,
	}
}

// Increment 增加进度
func (pt *ProgressTracker) Increment(delta int64) error {
	current := pt.current.Add(delta)
	return pt.Report(current)
}

// Report 报告当前进度
func (pt *ProgressTracker) Report(current int64) error {
	if !pt.config.EnableProgress {
		return nil
	}

	progress := float64(current) / float64(pt.total) * 100

	// 计算 ETA
	var eta time.Duration
	if pt.config.EnableETA && current > 0 {
		elapsed := time.Since(pt.startTime)
		avgTimePerItem := elapsed / time.Duration(current)
		remaining := pt.total - current
		eta = avgTimePerItem * time.Duration(remaining)
	}

	chunk := &core.LegacyStreamChunk{
		Type: core.ChunkTypeProgress,
		Data: map[string]interface{}{
			"progress": progress,
			"current":  current,
			"total":    pt.total,
			"eta":      eta.String(),
		},
		Metadata: core.ChunkMetadata{
			Timestamp: time.Now(),
			Progress:  progress,
			Current:   current,
			Total:     pt.total,
			ETA:       eta,
		},
	}

	return pt.writer.WriteChunk(chunk)
}

// Complete 标记完成
func (pt *ProgressTracker) Complete() error {
	return pt.Report(pt.total)
}

// Current 返回当前进度
func (pt *ProgressTracker) Current() int64 {
	return pt.current.Load()
}

// Progress 返回进度百分比
func (pt *ProgressTracker) Progress() float64 {
	current := pt.current.Load()
	return float64(current) / float64(pt.total) * 100
}

// ETA 返回预计剩余时间
func (pt *ProgressTracker) ETA() time.Duration {
	current := pt.current.Load()
	if current == 0 {
		return 0
	}

	elapsed := time.Since(pt.startTime)
	avgTimePerItem := elapsed / time.Duration(current)
	remaining := pt.total - current
	return avgTimePerItem * time.Duration(remaining)
}
