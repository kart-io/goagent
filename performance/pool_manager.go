package performance

import (
	"bytes"
	"context"
	"sync"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
)

// PoolType 定义池类型
type PoolType string

const (
	PoolTypeByteBuffer  PoolType = "bytebuffer"
	PoolTypeMessage     PoolType = "message"
	PoolTypeToolInput   PoolType = "toolinput"
	PoolTypeToolOutput  PoolType = "tooloutput"
	PoolTypeAgentInput  PoolType = "agentinput"
	PoolTypeAgentOutput PoolType = "agentoutput"
)

// PoolManager 对象池管理器接口
type PoolManager interface {
	// GetBuffer 获取 ByteBuffer
	GetBuffer() *bytes.Buffer
	PutBuffer(buf *bytes.Buffer)

	// GetMessage 获取 Message
	GetMessage() *interfaces.Message
	PutMessage(msg *interfaces.Message)

	// GetToolInput 获取 ToolInput
	GetToolInput() *interfaces.ToolInput
	PutToolInput(input *interfaces.ToolInput)

	// GetToolOutput 获取 ToolOutput
	GetToolOutput() *interfaces.ToolOutput
	PutToolOutput(output *interfaces.ToolOutput)

	// GetAgentInput 获取 AgentInput
	GetAgentInput() *core.AgentInput
	PutAgentInput(input *core.AgentInput)

	// GetAgentOutput 获取 AgentOutput
	GetAgentOutput() *core.AgentOutput
	PutAgentOutput(output *core.AgentOutput)

	// Configuration 配置管理
	Configure(config *PoolManagerConfig) error
	GetConfig() *PoolManagerConfig
	EnablePool(poolType PoolType)
	DisablePool(poolType PoolType)
	IsPoolEnabled(poolType PoolType) bool

	// Statistics 统计信息
	GetStats(poolType PoolType) *ObjectPoolStats
	GetAllStats() map[PoolType]*ObjectPoolStats
	ResetStats()

	// Lifecycle
	Close() error
}

// PoolManagerConfig 池管理器配置
type PoolManagerConfig struct {
	// 启用配置
	EnabledPools map[PoolType]bool

	// 池大小限制
	MaxBufferSize int // ByteBuffer 最大大小 (默认 64KB)
	MaxMapSize    int // Map 最大键数 (默认 100)
	MaxSliceSize  int // Slice 最大容量 (默认 100)

	// 策略配置
	UseStrategy PoolStrategy // 可选：自定义策略
}

// DefaultPoolManagerConfig 默认配置
func DefaultPoolManagerConfig() *PoolManagerConfig {
	return &PoolManagerConfig{
		EnabledPools: map[PoolType]bool{
			PoolTypeByteBuffer:  true,
			PoolTypeMessage:     true,
			PoolTypeToolInput:   true,
			PoolTypeToolOutput:  true,
			PoolTypeAgentInput:  true,
			PoolTypeAgentOutput: true,
		},
		MaxBufferSize: 64 * 1024,
		MaxMapSize:    100,
		MaxSliceSize:  100,
	}
}

// PoolStrategy 池策略接口
type PoolStrategy interface {
	// ShouldPool 决定是否应该池化对象
	ShouldPool(poolType PoolType, size int) bool

	// PreGet 获取对象前的钩子
	PreGet(poolType PoolType)

	// PostPut 归还对象后的钩子
	PostPut(poolType PoolType)
}

// defaultPoolStrategy 默认池策略
type defaultPoolStrategy struct {
	config *PoolManagerConfig
}

func (s *defaultPoolStrategy) ShouldPool(poolType PoolType, size int) bool {
	// 检查是否启用
	if enabled, ok := s.config.EnabledPools[poolType]; !ok || !enabled {
		return false
	}

	// 检查大小限制
	switch poolType {
	case PoolTypeByteBuffer:
		return size <= s.config.MaxBufferSize
	case PoolTypeToolInput, PoolTypeToolOutput, PoolTypeAgentInput, PoolTypeAgentOutput:
		return size <= s.config.MaxMapSize
	default:
		return true
	}
}

func (s *defaultPoolStrategy) PreGet(poolType PoolType)  {}
func (s *defaultPoolStrategy) PostPut(poolType PoolType) {}

// PoolAgent 池代理 - Agent 模式实现
type PoolAgent struct {
	config   *PoolManagerConfig
	strategy PoolStrategy
	mu       sync.RWMutex

	// 统计信息
	stats map[PoolType]*ObjectPoolStats

	// 实际的池
	byteBufferPool  *sync.Pool
	messagePool     *sync.Pool
	toolInputPool   *sync.Pool
	toolOutputPool  *sync.Pool
	agentInputPool  *sync.Pool
	agentOutputPool *sync.Pool
}

// NewPoolAgent 创建新的池代理
func NewPoolAgent(config *PoolManagerConfig) *PoolAgent {
	if config == nil {
		config = DefaultPoolManagerConfig()
	}

	agent := &PoolAgent{
		config: config,
		stats:  make(map[PoolType]*ObjectPoolStats),
	}

	// 设置策略
	if config.UseStrategy != nil {
		agent.strategy = config.UseStrategy
	} else {
		agent.strategy = &defaultPoolStrategy{config: config}
	}

	// 初始化池
	agent.initPools()

	// 初始化统计
	for _, poolType := range []PoolType{
		PoolTypeByteBuffer, PoolTypeMessage,
		PoolTypeToolInput, PoolTypeToolOutput,
		PoolTypeAgentInput, PoolTypeAgentOutput,
	} {
		agent.stats[poolType] = &ObjectPoolStats{}
	}

	return agent
}

// initPools 初始化所有池
func (a *PoolAgent) initPools() {
	a.byteBufferPool = &sync.Pool{
		New: func() interface{} {
			a.recordNew(PoolTypeByteBuffer)
			return new(bytes.Buffer)
		},
	}

	a.messagePool = &sync.Pool{
		New: func() interface{} {
			a.recordNew(PoolTypeMessage)
			return &interfaces.Message{}
		},
	}

	a.toolInputPool = &sync.Pool{
		New: func() interface{} {
			a.recordNew(PoolTypeToolInput)
			return &interfaces.ToolInput{
				Args: make(map[string]interface{}),
			}
		},
	}

	a.toolOutputPool = &sync.Pool{
		New: func() interface{} {
			a.recordNew(PoolTypeToolOutput)
			return &interfaces.ToolOutput{
				Metadata: make(map[string]interface{}),
			}
		},
	}

	a.agentInputPool = &sync.Pool{
		New: func() interface{} {
			a.recordNew(PoolTypeAgentInput)
			return &core.AgentInput{
				Context: make(map[string]interface{}),
			}
		},
	}

	a.agentOutputPool = &sync.Pool{
		New: func() interface{} {
			a.recordNew(PoolTypeAgentOutput)
			return &core.AgentOutput{
				ReasoningSteps: make([]core.ReasoningStep, 0, 4),
				ToolCalls:      make([]core.ToolCall, 0, 4),
				Metadata:       make(map[string]interface{}),
			}
		},
	}
}

// recordGet 记录获取统计
func (a *PoolAgent) recordGet(poolType PoolType) {
	if stats, ok := a.stats[poolType]; ok {
		stats.Gets.Add(1)
	}
}

// recordPut 记录归还统计
func (a *PoolAgent) recordPut(poolType PoolType) {
	if stats, ok := a.stats[poolType]; ok {
		stats.Puts.Add(1)
	}
}

// recordNew 记录新建统计
func (a *PoolAgent) recordNew(poolType PoolType) {
	if stats, ok := a.stats[poolType]; ok {
		stats.News.Add(1)
	}
}

// GetBuffer 获取 ByteBuffer
func (a *PoolAgent) GetBuffer() *bytes.Buffer {
	a.recordGet(PoolTypeByteBuffer)
	a.strategy.PreGet(PoolTypeByteBuffer)

	if !a.IsPoolEnabled(PoolTypeByteBuffer) {
		a.recordNew(PoolTypeByteBuffer)
		return new(bytes.Buffer)
	}

	buf := a.byteBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// PutBuffer 归还 ByteBuffer
func (a *PoolAgent) PutBuffer(buf *bytes.Buffer) {
	if buf == nil {
		return
	}

	defer a.strategy.PostPut(PoolTypeByteBuffer)

	if !a.strategy.ShouldPool(PoolTypeByteBuffer, buf.Cap()) {
		return
	}

	buf.Reset()
	a.recordPut(PoolTypeByteBuffer)
	a.byteBufferPool.Put(buf)
}

// GetMessage 获取 Message
func (a *PoolAgent) GetMessage() *interfaces.Message {
	a.recordGet(PoolTypeMessage)
	a.strategy.PreGet(PoolTypeMessage)

	if !a.IsPoolEnabled(PoolTypeMessage) {
		a.recordNew(PoolTypeMessage)
		return &interfaces.Message{}
	}

	msg := a.messagePool.Get().(*interfaces.Message)
	// 重置字段
	msg.Role = ""
	msg.Content = ""
	msg.Name = ""
	return msg
}

// PutMessage 归还 Message
func (a *PoolAgent) PutMessage(msg *interfaces.Message) {
	if msg == nil {
		return
	}

	defer a.strategy.PostPut(PoolTypeMessage)

	if !a.IsPoolEnabled(PoolTypeMessage) {
		return
	}

	// 清理字段
	msg.Role = ""
	msg.Content = ""
	msg.Name = ""

	a.recordPut(PoolTypeMessage)
	a.messagePool.Put(msg)
}

// GetToolInput 获取 ToolInput
func (a *PoolAgent) GetToolInput() *interfaces.ToolInput {
	a.recordGet(PoolTypeToolInput)
	a.strategy.PreGet(PoolTypeToolInput)

	if !a.IsPoolEnabled(PoolTypeToolInput) {
		a.recordNew(PoolTypeToolInput)
		return &interfaces.ToolInput{
			Args: make(map[string]interface{}),
		}
	}

	input := a.toolInputPool.Get().(*interfaces.ToolInput)
	// 清理 map
	for k := range input.Args {
		delete(input.Args, k)
	}
	input.Context = nil
	return input
}

// PutToolInput 归还 ToolInput
func (a *PoolAgent) PutToolInput(input *interfaces.ToolInput) {
	if input == nil {
		return
	}

	defer a.strategy.PostPut(PoolTypeToolInput)

	if !a.strategy.ShouldPool(PoolTypeToolInput, len(input.Args)) {
		return
	}

	// 清理 map
	if len(input.Args) > a.config.MaxMapSize {
		input.Args = make(map[string]interface{})
	} else {
		for k := range input.Args {
			delete(input.Args, k)
		}
	}
	input.Context = nil

	a.recordPut(PoolTypeToolInput)
	a.toolInputPool.Put(input)
}

// GetToolOutput 获取 ToolOutput
func (a *PoolAgent) GetToolOutput() *interfaces.ToolOutput {
	a.recordGet(PoolTypeToolOutput)
	a.strategy.PreGet(PoolTypeToolOutput)

	if !a.IsPoolEnabled(PoolTypeToolOutput) {
		a.recordNew(PoolTypeToolOutput)
		return &interfaces.ToolOutput{
			Metadata: make(map[string]interface{}),
		}
	}

	output := a.toolOutputPool.Get().(*interfaces.ToolOutput)
	// 清理字段
	output.Result = nil
	output.Success = false
	output.Error = ""
	for k := range output.Metadata {
		delete(output.Metadata, k)
	}
	return output
}

// PutToolOutput 归还 ToolOutput
func (a *PoolAgent) PutToolOutput(output *interfaces.ToolOutput) {
	if output == nil {
		return
	}

	defer a.strategy.PostPut(PoolTypeToolOutput)

	if !a.strategy.ShouldPool(PoolTypeToolOutput, len(output.Metadata)) {
		return
	}

	// 清理字段
	output.Result = nil
	output.Success = false
	output.Error = ""

	// 清理 metadata map
	if len(output.Metadata) > a.config.MaxMapSize {
		output.Metadata = make(map[string]interface{})
	} else {
		for k := range output.Metadata {
			delete(output.Metadata, k)
		}
	}

	a.recordPut(PoolTypeToolOutput)
	a.toolOutputPool.Put(output)
}

// GetAgentInput 获取 AgentInput
func (a *PoolAgent) GetAgentInput() *core.AgentInput {
	a.recordGet(PoolTypeAgentInput)
	a.strategy.PreGet(PoolTypeAgentInput)

	if !a.IsPoolEnabled(PoolTypeAgentInput) {
		a.recordNew(PoolTypeAgentInput)
		return &core.AgentInput{
			Context: make(map[string]interface{}),
		}
	}

	input := a.agentInputPool.Get().(*core.AgentInput)
	// 重置字段
	input.Task = ""
	input.Instruction = ""
	input.SessionID = ""
	// 清理 map
	for k := range input.Context {
		delete(input.Context, k)
	}
	return input
}

// PutAgentInput 归还 AgentInput
func (a *PoolAgent) PutAgentInput(input *core.AgentInput) {
	if input == nil {
		return
	}

	defer a.strategy.PostPut(PoolTypeAgentInput)

	if !a.strategy.ShouldPool(PoolTypeAgentInput, len(input.Context)) {
		return
	}

	// 重置字段
	input.Task = ""
	input.Instruction = ""
	input.SessionID = ""

	// 清理 map
	if len(input.Context) > a.config.MaxMapSize {
		input.Context = make(map[string]interface{})
	} else {
		for k := range input.Context {
			delete(input.Context, k)
		}
	}

	a.recordPut(PoolTypeAgentInput)
	a.agentInputPool.Put(input)
}

// GetAgentOutput 获取 AgentOutput
func (a *PoolAgent) GetAgentOutput() *core.AgentOutput {
	a.recordGet(PoolTypeAgentOutput)
	a.strategy.PreGet(PoolTypeAgentOutput)

	if !a.IsPoolEnabled(PoolTypeAgentOutput) {
		a.recordNew(PoolTypeAgentOutput)
		return &core.AgentOutput{
			ReasoningSteps: make([]core.ReasoningStep, 0, 4),
			ToolCalls:      make([]core.ToolCall, 0, 4),
			Metadata:       make(map[string]interface{}),
		}
	}

	output := a.agentOutputPool.Get().(*core.AgentOutput)
	// 重置字段
	output.Result = nil
	output.Status = ""
	output.Message = ""
	// 重置 slices
	output.ReasoningSteps = output.ReasoningSteps[:0]
	output.ToolCalls = output.ToolCalls[:0]
	// 清理 map
	for k := range output.Metadata {
		delete(output.Metadata, k)
	}
	output.TokenUsage = nil
	return output
}

// PutAgentOutput 归还 AgentOutput
func (a *PoolAgent) PutAgentOutput(output *core.AgentOutput) {
	if output == nil {
		return
	}

	defer a.strategy.PostPut(PoolTypeAgentOutput)

	maxSize := cap(output.ReasoningSteps)
	if cap(output.ToolCalls) > maxSize {
		maxSize = cap(output.ToolCalls)
	}

	if !a.strategy.ShouldPool(PoolTypeAgentOutput, maxSize) {
		return
	}

	// 重置字段
	output.Result = nil
	output.Status = ""
	output.Message = ""
	output.TokenUsage = nil

	// 只池化小 slice
	if cap(output.ReasoningSteps) > a.config.MaxSliceSize ||
		cap(output.ToolCalls) > a.config.MaxSliceSize {
		output.ReasoningSteps = make([]core.ReasoningStep, 0, 4)
		output.ToolCalls = make([]core.ToolCall, 0, 4)
	} else {
		output.ReasoningSteps = output.ReasoningSteps[:0]
		output.ToolCalls = output.ToolCalls[:0]
	}

	// 清理 map
	if len(output.Metadata) > a.config.MaxMapSize {
		output.Metadata = make(map[string]interface{})
	} else {
		for k := range output.Metadata {
			delete(output.Metadata, k)
		}
	}

	a.recordPut(PoolTypeAgentOutput)
	a.agentOutputPool.Put(output)
}

// Configure 配置管理器
func (a *PoolAgent) Configure(config *PoolManagerConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if config == nil {
		return nil
	}

	a.config = config

	// 更新策略
	if config.UseStrategy != nil {
		a.strategy = config.UseStrategy
	} else {
		a.strategy = &defaultPoolStrategy{config: config}
	}

	return nil
}

// GetConfig 获取当前配置
func (a *PoolAgent) GetConfig() *PoolManagerConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config
}

// EnablePool 启用指定池
func (a *PoolAgent) EnablePool(poolType PoolType) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config.EnabledPools[poolType] = true
}

// DisablePool 禁用指定池
func (a *PoolAgent) DisablePool(poolType PoolType) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config.EnabledPools[poolType] = false
}

// IsPoolEnabled 检查池是否启用
func (a *PoolAgent) IsPoolEnabled(poolType PoolType) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config.EnabledPools[poolType]
}

// GetStats 获取指定池的统计信息
func (a *PoolAgent) GetStats(poolType PoolType) *ObjectPoolStats {
	if stats, ok := a.stats[poolType]; ok {
		// 返回拷贝
		return &ObjectPoolStats{
			Gets:    stats.Gets,
			Puts:    stats.Puts,
			News:    stats.News,
			Current: stats.Current,
		}
	}
	return &ObjectPoolStats{}
}

// GetAllStats 获取所有池的统计信息
func (a *PoolAgent) GetAllStats() map[PoolType]*ObjectPoolStats {
	result := make(map[PoolType]*ObjectPoolStats)
	for poolType, stats := range a.stats {
		result[poolType] = &ObjectPoolStats{
			Gets:    stats.Gets,
			Puts:    stats.Puts,
			News:    stats.News,
			Current: stats.Current,
		}
	}
	return result
}

// ResetStats 重置统计信息
func (a *PoolAgent) ResetStats() {
	for _, stats := range a.stats {
		stats.Gets.Store(0)
		stats.Puts.Store(0)
		stats.News.Store(0)
		stats.Current.Store(0)
	}
}

// Close 关闭池管理器
func (a *PoolAgent) Close() error {
	// 清理资源（如果需要）
	return nil
}

// Execute Agent 模式执行接口
func (a *PoolAgent) Execute(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	// 这个方法可以用于演示 Agent 模式
	// 在实际使用中，池管理器不需要实现 Agent 接口
	// 但这里展示了如何将池管理器作为 Agent 使用

	output := a.GetAgentOutput()
	defer a.PutAgentOutput(output)

	output.Status = "success"
	output.Message = "Pool manager executed successfully"
	output.Metadata["pool_stats"] = a.GetAllStats()

	return output, nil
}

// 全局默认池管理器
var defaultPoolManager PoolManager

// InitDefaultPoolManager 初始化默认池管理器
func InitDefaultPoolManager(config *PoolManagerConfig) {
	defaultPoolManager = NewPoolAgent(config)
}

// GetDefaultPoolManager 获取默认池管理器
func GetDefaultPoolManager() PoolManager {
	if defaultPoolManager == nil {
		defaultPoolManager = NewPoolAgent(DefaultPoolManagerConfig())
	}
	return defaultPoolManager
}

// CreateIsolatedPoolManager 创建隔离的池管理器
// 用于测试或特定场景，不影响全局状态
func CreateIsolatedPoolManager(config *PoolManagerConfig) PoolManager {
	return NewPoolAgent(config)
}
