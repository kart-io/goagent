package performance

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/core"
)

// AdaptivePoolStrategy 自适应池策略
// 根据使用频率和内存压力动态调整池行为
type AdaptivePoolStrategy struct {
	config          *PoolManagerConfig
	usageCounter    map[PoolType]*atomic.Int64
	lastUsage       map[PoolType]*atomic.Int64
	memoryThreshold float64 // 内存使用率阈值
	inactiveTimeout time.Duration
	minUsageForPool int64 // 最小使用次数才启用池
}

// NewAdaptivePoolStrategy 创建自适应池策略
func NewAdaptivePoolStrategy(config *PoolManagerConfig) *AdaptivePoolStrategy {
	s := &AdaptivePoolStrategy{
		config:          config,
		usageCounter:    make(map[PoolType]*atomic.Int64),
		lastUsage:       make(map[PoolType]*atomic.Int64),
		memoryThreshold: 0.8, // 80% 内存使用率
		inactiveTimeout: 5 * time.Minute,
		minUsageForPool: 100,
	}

	// 初始化计数器
	for _, poolType := range []PoolType{
		PoolTypeByteBuffer, PoolTypeMessage,
		PoolTypeToolInput, PoolTypeToolOutput,
		PoolTypeAgentInput, PoolTypeAgentOutput,
	} {
		s.usageCounter[poolType] = &atomic.Int64{}
		s.lastUsage[poolType] = &atomic.Int64{}
	}

	// 启动监控 goroutine
	go s.monitor()

	return s
}

// ShouldPool 决定是否池化
func (s *AdaptivePoolStrategy) ShouldPool(poolType PoolType, size int) bool {
	// 检查基本配置
	if !s.config.EnabledPools[poolType] {
		return false
	}

	// 检查使用频率
	if counter, ok := s.usageCounter[poolType]; ok {
		count := counter.Load()
		if count < s.minUsageForPool {
			return false // 使用次数不够，不池化
		}
	}

	// 检查大小限制
	switch poolType {
	case PoolTypeByteBuffer:
		if size > s.config.MaxBufferSize {
			return false
		}
	case PoolTypeToolInput, PoolTypeToolOutput, PoolTypeAgentInput, PoolTypeAgentOutput:
		if size > s.config.MaxMapSize {
			return false
		}
	}

	// TODO: 检查内存压力
	// if s.getMemoryUsage() > s.memoryThreshold {
	//     return false
	// }

	return true
}

// PreGet 获取前钩子
func (s *AdaptivePoolStrategy) PreGet(poolType PoolType) {
	// 更新使用计数
	if counter, ok := s.usageCounter[poolType]; ok {
		counter.Add(1)
	}

	// 更新最后使用时间
	if lastUsage, ok := s.lastUsage[poolType]; ok {
		lastUsage.Store(time.Now().Unix())
	}
}

// PostPut 归还后钩子
func (s *AdaptivePoolStrategy) PostPut(poolType PoolType) {
	// 可以在这里收集更多统计信息
}

// monitor 监控池使用情况
func (s *AdaptivePoolStrategy) monitor() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now().Unix()
		for poolType, lastUsage := range s.lastUsage {
			last := lastUsage.Load()
			if last > 0 && now-last > int64(s.inactiveTimeout.Seconds()) {
				// 长时间未使用，考虑禁用池
				if counter, ok := s.usageCounter[poolType]; ok {
					counter.Store(0) // 重置计数
				}
			}
		}
	}
}

// MetricsPoolStrategy 带指标收集的池策略
type MetricsPoolStrategy struct {
	base             PoolStrategy
	getLatency       map[PoolType]*atomic.Int64
	putLatency       map[PoolType]*atomic.Int64
	hitRate          map[PoolType]*atomic.Int64
	missRate         map[PoolType]*atomic.Int64
	metricsCollector MetricsCollector
}

// MetricsCollector 指标收集器接口
type MetricsCollector interface {
	RecordPoolGet(poolType PoolType, latency time.Duration)
	RecordPoolPut(poolType PoolType, latency time.Duration)
	RecordPoolHit(poolType PoolType)
	RecordPoolMiss(poolType PoolType)
}

// NewMetricsPoolStrategy 创建带指标的池策略
func NewMetricsPoolStrategy(base PoolStrategy, collector MetricsCollector) *MetricsPoolStrategy {
	s := &MetricsPoolStrategy{
		base:             base,
		getLatency:       make(map[PoolType]*atomic.Int64),
		putLatency:       make(map[PoolType]*atomic.Int64),
		hitRate:          make(map[PoolType]*atomic.Int64),
		missRate:         make(map[PoolType]*atomic.Int64),
		metricsCollector: collector,
	}

	// 初始化计数器
	for _, poolType := range []PoolType{
		PoolTypeByteBuffer, PoolTypeMessage,
		PoolTypeToolInput, PoolTypeToolOutput,
		PoolTypeAgentInput, PoolTypeAgentOutput,
	} {
		s.getLatency[poolType] = &atomic.Int64{}
		s.putLatency[poolType] = &atomic.Int64{}
		s.hitRate[poolType] = &atomic.Int64{}
		s.missRate[poolType] = &atomic.Int64{}
	}

	return s
}

// ShouldPool 决定是否池化（委托给基础策略）
func (s *MetricsPoolStrategy) ShouldPool(poolType PoolType, size int) bool {
	return s.base.ShouldPool(poolType, size)
}

// PreGet 获取前钩子（记录指标）
func (s *MetricsPoolStrategy) PreGet(poolType PoolType) {
	start := time.Now()
	s.base.PreGet(poolType)
	latency := time.Since(start)

	if s.metricsCollector != nil {
		s.metricsCollector.RecordPoolGet(poolType, latency)
	}

	if counter, ok := s.getLatency[poolType]; ok {
		counter.Add(int64(latency.Nanoseconds()))
	}
}

// PostPut 归还后钩子（记录指标）
func (s *MetricsPoolStrategy) PostPut(poolType PoolType) {
	start := time.Now()
	s.base.PostPut(poolType)
	latency := time.Since(start)

	if s.metricsCollector != nil {
		s.metricsCollector.RecordPoolPut(poolType, latency)
	}

	if counter, ok := s.putLatency[poolType]; ok {
		counter.Add(int64(latency.Nanoseconds()))
	}
}

// PriorityPoolStrategy 优先级池策略
// 根据池类型的优先级决定池化行为
type PriorityPoolStrategy struct {
	config     *PoolManagerConfig
	priorities map[PoolType]int
	maxPools   int // 最多同时启用的池数量
}

// NewPriorityPoolStrategy 创建优先级池策略
func NewPriorityPoolStrategy(config *PoolManagerConfig, maxPools int) *PriorityPoolStrategy {
	return &PriorityPoolStrategy{
		config: config,
		priorities: map[PoolType]int{
			PoolTypeByteBuffer:  1, // 最高优先级
			PoolTypeMessage:     2,
			PoolTypeToolInput:   3,
			PoolTypeToolOutput:  3,
			PoolTypeAgentInput:  4,
			PoolTypeAgentOutput: 4,
		},
		maxPools: maxPools,
	}
}

// ShouldPool 根据优先级决定是否池化
func (s *PriorityPoolStrategy) ShouldPool(poolType PoolType, size int) bool {
	if !s.config.EnabledPools[poolType] {
		return false
	}

	priority, ok := s.priorities[poolType]
	if !ok {
		return false
	}

	// 统计当前启用的池数量
	enabledCount := 0
	for pt, enabled := range s.config.EnabledPools {
		if enabled && s.priorities[pt] < priority {
			enabledCount++
		}
	}

	// 如果高优先级池已经达到限制，不启用低优先级池
	if enabledCount >= s.maxPools {
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

func (s *PriorityPoolStrategy) PreGet(poolType PoolType)  {}
func (s *PriorityPoolStrategy) PostPut(poolType PoolType) {}

// ScenarioBasedStrategy 场景驱动的池策略
type ScenarioBasedStrategy struct {
	config       *PoolManagerConfig
	currentScene ScenarioType
	sceneConfigs map[ScenarioType]map[PoolType]bool
}

// ScenarioType 场景类型
type ScenarioType string

const (
	ScenarioLLMCalls    ScenarioType = "llm_calls"
	ScenarioToolExec    ScenarioType = "tool_execution"
	ScenarioJSONProcess ScenarioType = "json_processing"
	ScenarioStreaming   ScenarioType = "streaming"
	ScenarioGeneral     ScenarioType = "general"
)

// NewScenarioBasedStrategy 创建场景驱动策略
func NewScenarioBasedStrategy(config *PoolManagerConfig) *ScenarioBasedStrategy {
	return &ScenarioBasedStrategy{
		config:       config,
		currentScene: ScenarioGeneral,
		sceneConfigs: map[ScenarioType]map[PoolType]bool{
			ScenarioLLMCalls: {
				PoolTypeMessage:     true,
				PoolTypeAgentInput:  true,
				PoolTypeAgentOutput: true,
				// 其他池禁用
				PoolTypeByteBuffer: false,
				PoolTypeToolInput:  false,
				PoolTypeToolOutput: false,
			},
			ScenarioToolExec: {
				PoolTypeToolInput:  true,
				PoolTypeToolOutput: true,
				PoolTypeByteBuffer: true,
				// 其他池禁用
				PoolTypeMessage:     false,
				PoolTypeAgentInput:  false,
				PoolTypeAgentOutput: false,
			},
			ScenarioJSONProcess: {
				PoolTypeByteBuffer: true,
				// 其他池都禁用
				PoolTypeMessage:     false,
				PoolTypeToolInput:   false,
				PoolTypeToolOutput:  false,
				PoolTypeAgentInput:  false,
				PoolTypeAgentOutput: false,
			},
			ScenarioStreaming: {
				PoolTypeMessage:    true,
				PoolTypeByteBuffer: true,
				// 其他池禁用
				PoolTypeToolInput:   false,
				PoolTypeToolOutput:  false,
				PoolTypeAgentInput:  false,
				PoolTypeAgentOutput: false,
			},
			ScenarioGeneral: {
				// 通用场景，启用所有池
				PoolTypeByteBuffer:  true,
				PoolTypeMessage:     true,
				PoolTypeToolInput:   true,
				PoolTypeToolOutput:  true,
				PoolTypeAgentInput:  true,
				PoolTypeAgentOutput: true,
			},
		},
	}
}

// SetScenario 设置当前场景
func (s *ScenarioBasedStrategy) SetScenario(scenario ScenarioType) {
	s.currentScene = scenario
	log.Printf("Pool strategy switched to scenario: %s", scenario)
}

// ShouldPool 根据场景决定是否池化
func (s *ScenarioBasedStrategy) ShouldPool(poolType PoolType, size int) bool {
	// 获取当前场景配置
	sceneConfig, ok := s.sceneConfigs[s.currentScene]
	if !ok {
		// 未知场景，使用通用配置
		sceneConfig = s.sceneConfigs[ScenarioGeneral]
	}

	// 检查场景配置
	if enabled, ok := sceneConfig[poolType]; ok && !enabled {
		return false
	}

	// 检查大小限制
	switch poolType {
	case PoolTypeByteBuffer:
		return size <= s.config.MaxBufferSize
	default:
		return size <= s.config.MaxMapSize
	}
}

func (s *ScenarioBasedStrategy) PreGet(poolType PoolType)  {}
func (s *ScenarioBasedStrategy) PostPut(poolType PoolType) {}

// PoolManagerAgent 将 PoolAgent 包装为真正的 Agent
type PoolManagerAgent struct {
	manager PoolManager
	name    string
}

// NewPoolManagerAgent 创建池管理器 Agent
func NewPoolManagerAgent(name string, config *PoolManagerConfig) *PoolManagerAgent {
	return &PoolManagerAgent{
		manager: NewPoolAgent(config),
		name:    name,
	}
}

// Name 返回 Agent 名称
func (a *PoolManagerAgent) Name() string {
	return a.name
}

// Description 返回描述
func (a *PoolManagerAgent) Description() string {
	return "Pool manager agent for optimized object pooling"
}

// Capabilities 返回能力列表
func (a *PoolManagerAgent) Capabilities() []string {
	return []string{
		"object_pooling",
		"memory_optimization",
		"statistics_tracking",
		"dynamic_configuration",
	}
}

// Execute 执行 Agent
func (a *PoolManagerAgent) Execute(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	// 根据输入调整池配置
	if input.Task == "configure_pools" {
		if scenario, ok := input.Context["scenario"].(string); ok {
			a.configureForScenario(ScenarioType(scenario))
		}
	}

	// 获取输出对象（使用池）
	output := a.manager.GetAgentOutput()
	// 注意：不要 defer Put，因为我们要返回这个对象
	// 调用者需要负责归还

	// 设置结果
	output.Status = "success"
	output.Message = "Pool manager executed"
	output.Result = a.manager.GetAllStats()
	output.Metadata["configuration"] = a.manager.GetConfig()

	return output, nil
}

// configureForScenario 根据场景配置池
func (a *PoolManagerAgent) configureForScenario(scenario ScenarioType) {
	config := a.manager.GetConfig()

	// 根据场景调整配置
	switch scenario {
	case ScenarioLLMCalls:
		config.EnabledPools[PoolTypeMessage] = true
		config.EnabledPools[PoolTypeAgentInput] = true
		config.EnabledPools[PoolTypeAgentOutput] = true
		config.EnabledPools[PoolTypeByteBuffer] = false
		config.EnabledPools[PoolTypeToolInput] = false
		config.EnabledPools[PoolTypeToolOutput] = false

	case ScenarioToolExec:
		config.EnabledPools[PoolTypeToolInput] = true
		config.EnabledPools[PoolTypeToolOutput] = true
		config.EnabledPools[PoolTypeByteBuffer] = true
		config.EnabledPools[PoolTypeMessage] = false
		config.EnabledPools[PoolTypeAgentInput] = false
		config.EnabledPools[PoolTypeAgentOutput] = false

	case ScenarioJSONProcess:
		config.EnabledPools[PoolTypeByteBuffer] = true
		config.EnabledPools[PoolTypeMessage] = false
		config.EnabledPools[PoolTypeToolInput] = false
		config.EnabledPools[PoolTypeToolOutput] = false
		config.EnabledPools[PoolTypeAgentInput] = false
		config.EnabledPools[PoolTypeAgentOutput] = false

	case ScenarioStreaming:
		config.EnabledPools[PoolTypeMessage] = true
		config.EnabledPools[PoolTypeByteBuffer] = true
		config.EnabledPools[PoolTypeToolInput] = false
		config.EnabledPools[PoolTypeToolOutput] = false
		config.EnabledPools[PoolTypeAgentInput] = false
		config.EnabledPools[PoolTypeAgentOutput] = false

	default:
		// 启用所有池
		for poolType := range config.EnabledPools {
			config.EnabledPools[poolType] = true
		}
	}

	a.manager.Configure(config)
}
