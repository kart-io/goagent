package performance

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
)

const (
	// 容量上限，防止内存膨胀
	maxReasoningStepsCapacity = 100 // ReasoningSteps 最大容量
	maxToolCallsCapacity      = 50  // ToolCalls 最大容量
	maxContextMapSize         = 32  // Context Map 最大大小
	maxMetadataMapSize        = 32  // Metadata Map 最大大小
)

// DataPools 数据对象池集合
//
// 提供 Agent 执行过程中频繁分配的数据对象的复用，显著减轻 GC 压力。
// 实现零分配（Zero Allocation）目标，特别是在关键热路径上。
//
// 性能目标：
//   - Memory per agent: < 50MB
//   - Zero allocation in critical paths
//   - GC pressure reduction: > 80%
type DataPools struct {
	inputPool          *sync.Pool
	outputPool         *sync.Pool
	reasoningStepPool  *sync.Pool
	toolCallPool       *sync.Pool
	contextMapPool     *sync.Pool
	metadataMapPool    *sync.Pool
	stringSlicePool    *sync.Pool
	reasoningSlicePool *sync.Pool
	toolCallSlicePool  *sync.Pool

	// 统计信息
	stats poolingStats
}

// poolingStats 池化统计信息
//
// 使用 atomic 类型确保并发安全
type poolingStats struct {
	inputGetCount     atomic.Int64
	inputPutCount     atomic.Int64
	outputGetCount    atomic.Int64
	outputPutCount    atomic.Int64
	reasoningGetCount atomic.Int64
	reasoningPutCount atomic.Int64
	toolCallGetCount  atomic.Int64
	toolCallPutCount  atomic.Int64
	mapGetCount       atomic.Int64
	mapPutCount       atomic.Int64
	sliceGetCount     atomic.Int64
	slicePutCount     atomic.Int64
}

// DefaultDataPools 全局默认数据池实例
//
// 可以直接使用，也可以创建自定义实例
var DefaultDataPools = NewDataPools()

// NewDataPools 创建新的数据池集合
func NewDataPools() *DataPools {
	return &DataPools{
		// AgentInput 池
		inputPool: &sync.Pool{
			New: func() interface{} {
				return &core.AgentInput{
					Context: make(map[string]interface{}, 8), // 预分配容量
				}
			},
		},

		// AgentOutput 池
		outputPool: &sync.Pool{
			New: func() interface{} {
				return &core.AgentOutput{
					ReasoningSteps: make([]core.ReasoningStep, 0, 10), // 预分配容量
					ToolCalls:      make([]core.ToolCall, 0, 5),       // 预分配容量
					Metadata:       make(map[string]interface{}, 8),   // 预分配容量
				}
			},
		},

		// ReasoningStep 池
		reasoningStepPool: &sync.Pool{
			New: func() interface{} {
				return &core.ReasoningStep{}
			},
		},

		// ToolCall 池
		toolCallPool: &sync.Pool{
			New: func() interface{} {
				return &core.ToolCall{
					Input: make(map[string]interface{}, 4), // 预分配容量
				}
			},
		},

		// Context Map 池
		contextMapPool: &sync.Pool{
			New: func() interface{} {
				return make(map[string]interface{}, 8)
			},
		},

		// Metadata Map 池
		metadataMapPool: &sync.Pool{
			New: func() interface{} {
				return make(map[string]interface{}, 8)
			},
		},

		// String Slice 池 (用于 Capabilities 等)
		stringSlicePool: &sync.Pool{
			New: func() interface{} {
				return make([]string, 0, 10)
			},
		},

		// ReasoningStep Slice 池
		reasoningSlicePool: &sync.Pool{
			New: func() interface{} {
				return make([]core.ReasoningStep, 0, 10)
			},
		},

		// ToolCall Slice 池
		toolCallSlicePool: &sync.Pool{
			New: func() interface{} {
				return make([]core.ToolCall, 0, 5)
			},
		},
	}
}

// GetAgentInput 从池中获取 AgentInput
//
// 使用完毕后必须调用 PutAgentInput 归还到池中
func (p *DataPools) GetAgentInput() *core.AgentInput {
	input := p.inputPool.Get().(*core.AgentInput)
	p.stats.inputGetCount.Add(1)
	return input
}

// PutAgentInput 将 AgentInput 归还到池中
//
// 会重置所有字段，准备下次复用
func (p *DataPools) PutAgentInput(input *core.AgentInput) {
	if input == nil {
		return
	}

	// 重置字段
	input.Task = ""
	input.Instruction = ""
	input.SessionID = ""
	input.Timestamp = time.Time{}

	// 清空 Context map
	// 如果 map 太大，重新创建以释放内存
	if len(input.Context) > maxContextMapSize {
		input.Context = make(map[string]interface{}, 8)
	} else {
		// 使用 Go 1.21+ 的 clear 函数（高效清空）
		// 如果使用旧版本 Go，会回退到循环删除
		clearMap(input.Context)
	}

	// 重置 Options 为零值
	input.Options = core.AgentOptions{}

	p.stats.inputPutCount.Add(1)
	p.inputPool.Put(input)
}

// GetAgentOutput 从池中获取 AgentOutput
//
// 使用完毕后必须调用 PutAgentOutput 归还到池中
func (p *DataPools) GetAgentOutput() *core.AgentOutput {
	output := p.outputPool.Get().(*core.AgentOutput)
	p.stats.outputGetCount.Add(1)
	return output
}

// PutAgentOutput 将 AgentOutput 归还到池中
//
// 会重置所有字段，并使用切片零分配技巧 (slice[:0])
func (p *DataPools) PutAgentOutput(output *core.AgentOutput) {
	if output == nil {
		return
	}

	// 如果切片容量过大，不放回池中，让 GC 回收
	// 这防止了内存膨胀问题
	if cap(output.ReasoningSteps) > maxReasoningStepsCapacity ||
		cap(output.ToolCalls) > maxToolCallsCapacity {
		return
	}

	// 重置字段
	output.Result = nil
	output.Status = ""
	output.Message = ""
	output.TokenUsage = nil
	output.Latency = 0
	output.Timestamp = time.Time{}

	// 切片零分配：重置长度但保留容量
	// 这是关键优化点，避免了切片的重新分配
	output.ReasoningSteps = output.ReasoningSteps[:0]
	output.ToolCalls = output.ToolCalls[:0]

	// 清空 Metadata map
	if len(output.Metadata) > maxMetadataMapSize {
		output.Metadata = make(map[string]interface{}, 8)
	} else {
		clearMap(output.Metadata)
	}

	p.stats.outputPutCount.Add(1)
	p.outputPool.Put(output)
}

// GetReasoningStep 从池中获取 ReasoningStep
func (p *DataPools) GetReasoningStep() *core.ReasoningStep {
	step := p.reasoningStepPool.Get().(*core.ReasoningStep)
	p.stats.reasoningGetCount.Add(1)
	return step
}

// PutReasoningStep 将 ReasoningStep 归还到池中
func (p *DataPools) PutReasoningStep(step *core.ReasoningStep) {
	if step == nil {
		return
	}

	// 重置所有字段
	*step = core.ReasoningStep{}

	p.stats.reasoningPutCount.Add(1)
	p.reasoningStepPool.Put(step)
}

// GetToolCall 从池中获取 ToolCall
func (p *DataPools) GetToolCall() *core.ToolCall {
	tc := p.toolCallPool.Get().(*core.ToolCall)
	p.stats.toolCallGetCount.Add(1)
	return tc
}

// PutToolCall 将 ToolCall 归还到池中
func (p *DataPools) PutToolCall(tc *core.ToolCall) {
	if tc == nil {
		return
	}

	// 重置字段
	tc.ToolName = ""
	tc.Output = nil
	tc.Duration = 0
	tc.Success = false
	tc.Error = ""

	// 清空 Input map
	clearMap(tc.Input)

	p.stats.toolCallPutCount.Add(1)
	p.toolCallPool.Put(tc)
}

// GetContextMap 从池中获取 Context map
func (p *DataPools) GetContextMap() map[string]interface{} {
	m := p.contextMapPool.Get().(map[string]interface{})
	p.stats.mapGetCount.Add(1)
	return m
}

// PutContextMap 将 Context map 归还到池中
func (p *DataPools) PutContextMap(m map[string]interface{}) {
	if m == nil {
		return
	}

	// 如果 map 太大，不放回池中
	if len(m) > maxContextMapSize {
		return
	}

	// 清空 map，但保留底层存储
	clearMap(m)

	p.stats.mapPutCount.Add(1)
	p.contextMapPool.Put(m)
}

// GetMetadataMap 从池中获取 Metadata map
func (p *DataPools) GetMetadataMap() map[string]interface{} {
	m := p.metadataMapPool.Get().(map[string]interface{})
	p.stats.mapGetCount.Add(1)
	return m
}

// PutMetadataMap 将 Metadata map 归还到池中
func (p *DataPools) PutMetadataMap(m map[string]interface{}) {
	if m == nil {
		return
	}

	// 如果 map 太大，不放回池中
	if len(m) > maxMetadataMapSize {
		return
	}

	// 清空 map，但保留底层存储
	clearMap(m)

	p.stats.mapPutCount.Add(1)
	p.metadataMapPool.Put(m)
}

// GetStringSlice 从池中获取 string slice
func (p *DataPools) GetStringSlice() []string {
	s := p.stringSlicePool.Get().([]string)
	p.stats.sliceGetCount.Add(1)
	return s[:0] // 重置长度
}

// PutStringSlice 将 string slice 归还到池中
func (p *DataPools) PutStringSlice(s []string) {
	if s == nil {
		return
	}

	// 切片零分配：重置长度但保留容量
	s = s[:0]

	p.stats.slicePutCount.Add(1)
	p.stringSlicePool.Put(s)
}

// GetReasoningSlice 从池中获取 ReasoningStep slice
func (p *DataPools) GetReasoningSlice() []core.ReasoningStep {
	s := p.reasoningSlicePool.Get().([]core.ReasoningStep)
	p.stats.sliceGetCount.Add(1)
	return s[:0] // 重置长度
}

// PutReasoningSlice 将 ReasoningStep slice 归还到池中
func (p *DataPools) PutReasoningSlice(s []core.ReasoningStep) {
	if s == nil {
		return
	}

	// 如果容量过大，不放回池中
	if cap(s) > maxReasoningStepsCapacity {
		return
	}

	// 切片零分配：重置长度但保留容量
	s = s[:0]

	p.stats.slicePutCount.Add(1)
	p.reasoningSlicePool.Put(s)
}

// GetToolCallSlice 从池中获取 ToolCall slice
func (p *DataPools) GetToolCallSlice() []core.ToolCall {
	s := p.toolCallSlicePool.Get().([]core.ToolCall)
	p.stats.sliceGetCount.Add(1)
	return s[:0] // 重置长度
}

// PutToolCallSlice 将 ToolCall slice 归还到池中
func (p *DataPools) PutToolCallSlice(s []core.ToolCall) {
	if s == nil {
		return
	}

	// 如果容量过大，不放回池中
	if cap(s) > maxToolCallsCapacity {
		return
	}

	// 切片零分配：重置长度但保留容量
	s = s[:0]

	p.stats.slicePutCount.Add(1)
	p.toolCallSlicePool.Put(s)
}

// GetStats 返回池化统计信息
func (p *DataPools) GetStats() DataPoolStats {
	return DataPoolStats{
		InputGetCount:     p.stats.inputGetCount.Load(),
		InputPutCount:     p.stats.inputPutCount.Load(),
		OutputGetCount:    p.stats.outputGetCount.Load(),
		OutputPutCount:    p.stats.outputPutCount.Load(),
		ReasoningGetCount: p.stats.reasoningGetCount.Load(),
		ReasoningPutCount: p.stats.reasoningPutCount.Load(),
		ToolCallGetCount:  p.stats.toolCallGetCount.Load(),
		ToolCallPutCount:  p.stats.toolCallPutCount.Load(),
		MapGetCount:       p.stats.mapGetCount.Load(),
		MapPutCount:       p.stats.mapPutCount.Load(),
		SliceGetCount:     p.stats.sliceGetCount.Load(),
		SlicePutCount:     p.stats.slicePutCount.Load(),
		PoolHitRate:       p.calculateHitRate(),
	}
}

// calculateHitRate 计算池命中率
func (p *DataPools) calculateHitRate() float64 {
	totalGet := p.stats.inputGetCount.Load() + p.stats.outputGetCount.Load() +
		p.stats.reasoningGetCount.Load() + p.stats.toolCallGetCount.Load() +
		p.stats.mapGetCount.Load() + p.stats.sliceGetCount.Load()

	totalPut := p.stats.inputPutCount.Load() + p.stats.outputPutCount.Load() +
		p.stats.reasoningPutCount.Load() + p.stats.toolCallPutCount.Load() +
		p.stats.mapPutCount.Load() + p.stats.slicePutCount.Load()

	if totalGet == 0 {
		return 0
	}

	// 命中率 = Put / Get * 100
	// Put 越接近 Get，说明复用率越高
	return float64(totalPut) / float64(totalGet) * 100
}

// DataPoolStats 数据池统计信息
type DataPoolStats struct {
	InputGetCount     int64   // AgentInput Get 次数
	InputPutCount     int64   // AgentInput Put 次数
	OutputGetCount    int64   // AgentOutput Get 次数
	OutputPutCount    int64   // AgentOutput Put 次数
	ReasoningGetCount int64   // ReasoningStep Get 次数
	ReasoningPutCount int64   // ReasoningStep Put 次数
	ToolCallGetCount  int64   // ToolCall Get 次数
	ToolCallPutCount  int64   // ToolCall Put 次数
	MapGetCount       int64   // Map Get 次数
	MapPutCount       int64   // Map Put 次数
	SliceGetCount     int64   // Slice Get 次数
	SlicePutCount     int64   // Slice Put 次数
	PoolHitRate       float64 // 池命中率 (%)
}

// PooledAgentInput 池化的 AgentInput 辅助结构
//
// 自动管理对象的生命周期，使用 defer 自动归还
type PooledAgentInput struct {
	Input *core.AgentInput
	pool  *DataPools
}

// NewPooledAgentInput 创建池化的 AgentInput
//
// 使用示例：
//
//	input := NewPooledAgentInput(pool)
//	defer input.Release()
//	// 使用 input.Input
func NewPooledAgentInput(pool *DataPools) *PooledAgentInput {
	if pool == nil {
		pool = DefaultDataPools
	}

	return &PooledAgentInput{
		Input: pool.GetAgentInput(),
		pool:  pool,
	}
}

// Release 释放 AgentInput 回池中
func (p *PooledAgentInput) Release() {
	if p.Input != nil {
		p.pool.PutAgentInput(p.Input)
		p.Input = nil
	}
}

// PooledAgentOutput 池化的 AgentOutput 辅助结构
//
// 自动管理对象的生命周期，使用 defer 自动归还
type PooledAgentOutput struct {
	Output *core.AgentOutput
	pool   *DataPools
}

// NewPooledAgentOutput 创建池化的 AgentOutput
//
// 使用示例：
//
//	output := NewPooledAgentOutput(pool)
//	defer output.Release()
//	// 使用 output.Output
func NewPooledAgentOutput(pool *DataPools) *PooledAgentOutput {
	if pool == nil {
		pool = DefaultDataPools
	}

	return &PooledAgentOutput{
		Output: pool.GetAgentOutput(),
		pool:   pool,
	}
}

// Release 释放 AgentOutput 回池中
func (p *PooledAgentOutput) Release() {
	if p.Output != nil {
		p.pool.PutAgentOutput(p.Output)
		p.Output = nil
	}
}

// 便捷函数：使用全局默认池

// GetAgentInput 从默认池获取 AgentInput
func GetAgentInput() *core.AgentInput {
	return DefaultDataPools.GetAgentInput()
}

// PutAgentInput 归还 AgentInput 到默认池
func PutAgentInput(input *core.AgentInput) {
	DefaultDataPools.PutAgentInput(input)
}

// GetAgentOutput 从默认池获取 AgentOutput
func GetAgentOutput() *core.AgentOutput {
	return DefaultDataPools.GetAgentOutput()
}

// PutAgentOutput 归还 AgentOutput 到默认池
func PutAgentOutput(output *core.AgentOutput) {
	DefaultDataPools.PutAgentOutput(output)
}

// CloneAgentInput 克隆 AgentInput (使用池化对象)
//
// 用于需要保留输入数据的场景
func CloneAgentInput(src *core.AgentInput, pool *DataPools) *core.AgentInput {
	if pool == nil {
		pool = DefaultDataPools
	}

	dst := pool.GetAgentInput()

	// 复制简单字段
	dst.Task = src.Task
	dst.Instruction = src.Instruction
	dst.SessionID = src.SessionID
	dst.Timestamp = src.Timestamp
	dst.Options = src.Options

	// 深拷贝 Context
	for k, v := range src.Context {
		dst.Context[k] = v
	}

	return dst
}

// CloneAgentOutput 克隆 AgentOutput (使用池化对象)
//
// 用于需要保留输出数据的场景
func CloneAgentOutput(src *core.AgentOutput, pool *DataPools) *core.AgentOutput {
	if pool == nil {
		pool = DefaultDataPools
	}

	dst := pool.GetAgentOutput()

	// 复制简单字段
	dst.Result = src.Result
	dst.Status = src.Status
	dst.Message = src.Message
	dst.Latency = src.Latency
	dst.Timestamp = src.Timestamp

	// 复制 TokenUsage
	if src.TokenUsage != nil {
		dst.TokenUsage = &interfaces.TokenUsage{
			PromptTokens:     src.TokenUsage.PromptTokens,
			CompletionTokens: src.TokenUsage.CompletionTokens,
			TotalTokens:      src.TokenUsage.TotalTokens,
		}
	}

	// 深拷贝 ReasoningSteps
	for _, step := range src.ReasoningSteps {
		dst.ReasoningSteps = append(dst.ReasoningSteps, step)
	}

	// 深拷贝 ToolCalls
	for _, tc := range src.ToolCalls {
		newTC := core.ToolCall{
			ToolName: tc.ToolName,
			Input:    make(map[string]interface{}, len(tc.Input)),
			Output:   tc.Output,
			Duration: tc.Duration,
			Success:  tc.Success,
			Error:    tc.Error,
		}
		for k, v := range tc.Input {
			newTC.Input[k] = v
		}
		dst.ToolCalls = append(dst.ToolCalls, newTC)
	}

	// 深拷贝 Metadata
	for k, v := range src.Metadata {
		dst.Metadata[k] = v
	}

	return dst
}

// clearMap 清空 map
//
// 使用 Go 1.21+ 的 clear 内置函数（如果可用）
// 否则回退到循环删除
func clearMap(m map[string]interface{}) {
	// Go 1.21+ 支持 clear 内置函数，更高效
	// 编译器会自动优化这个操作
	clear(m)

	// 注意：如果使用 Go 1.20 或更早版本，可能需要：
	// for k := range m {
	//     delete(m, k)
	// }
}
