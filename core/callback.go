package core

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Callback 定义回调处理器接口
//
// 借鉴 LangChain 的回调系统，提供灵活的监控和调试能力
// 所有回调方法都返回 error，如果返回非 nil 错误，执行将中止
type Callback interface {
	// 通用回调
	OnStart(ctx context.Context, input interface{}) error
	OnEnd(ctx context.Context, output interface{}) error
	OnError(ctx context.Context, err error) error

	// LLM 回调
	OnLLMStart(ctx context.Context, prompts []string, model string) error
	OnLLMEnd(ctx context.Context, output string, tokenUsage int) error
	OnLLMError(ctx context.Context, err error) error

	// Chain 回调
	OnChainStart(ctx context.Context, chainName string, input interface{}) error
	OnChainEnd(ctx context.Context, chainName string, output interface{}) error
	OnChainError(ctx context.Context, chainName string, err error) error

	// Tool 回调
	OnToolStart(ctx context.Context, toolName string, input interface{}) error
	OnToolEnd(ctx context.Context, toolName string, output interface{}) error
	OnToolError(ctx context.Context, toolName string, err error) error

	// Agent 回调
	OnAgentAction(ctx context.Context, action *AgentAction) error
	OnAgentFinish(ctx context.Context, output interface{}) error
}

// AgentAction Agent 执行的操作
type AgentAction struct {
	Tool      string                 // 工具名称
	ToolInput map[string]interface{} // 工具输入
	Log       string                 // 日志信息
}

// BaseCallback 提供回调的默认实现（什么都不做）
//
// 用户可以嵌入 BaseCallback 并只重写需要的方法
type BaseCallback struct{}

// NewBaseCallback 创建基础回调
func NewBaseCallback() *BaseCallback {
	return &BaseCallback{}
}

func (b *BaseCallback) OnStart(ctx context.Context, input interface{}) error { return nil }
func (b *BaseCallback) OnEnd(ctx context.Context, output interface{}) error  { return nil }
func (b *BaseCallback) OnError(ctx context.Context, err error) error         { return nil }
func (b *BaseCallback) OnLLMStart(ctx context.Context, prompts []string, model string) error {
	return nil
}

func (b *BaseCallback) OnLLMEnd(ctx context.Context, output string, tokenUsage int) error {
	return nil
}
func (b *BaseCallback) OnLLMError(ctx context.Context, err error) error { return nil }
func (b *BaseCallback) OnChainStart(ctx context.Context, chainName string, input interface{}) error {
	return nil
}

func (b *BaseCallback) OnChainEnd(ctx context.Context, chainName string, output interface{}) error {
	return nil
}

func (b *BaseCallback) OnChainError(ctx context.Context, chainName string, err error) error {
	return nil
}

func (b *BaseCallback) OnToolStart(ctx context.Context, toolName string, input interface{}) error {
	return nil
}

func (b *BaseCallback) OnToolEnd(ctx context.Context, toolName string, output interface{}) error {
	return nil
}

func (b *BaseCallback) OnToolError(ctx context.Context, toolName string, err error) error {
	return nil
}

func (b *BaseCallback) OnAgentAction(ctx context.Context, action *AgentAction) error {
	return nil
}

func (b *BaseCallback) OnAgentFinish(ctx context.Context, output interface{}) error {
	return nil
}

// CallbackManager 管理多个回调处理器
type CallbackManager struct {
	callbacks []Callback
	mu        sync.RWMutex
}

// NewCallbackManager 创建回调管理器
func NewCallbackManager(callbacks ...Callback) *CallbackManager {
	return &CallbackManager{
		callbacks: callbacks,
	}
}

// AddCallback 添加回调
func (m *CallbackManager) AddCallback(cb Callback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbacks = append(m.callbacks, cb)
}

// RemoveCallback 移除回调
func (m *CallbackManager) RemoveCallback(cb Callback) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, callback := range m.callbacks {
		if callback == cb {
			m.callbacks = append(m.callbacks[:i], m.callbacks[i+1:]...)
			break
		}
	}
}

// TriggerCallbacks 触发所有回调的指定方法
func (m *CallbackManager) TriggerCallbacks(fn func(Callback) error) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, cb := range m.callbacks {
		if err := fn(cb); err != nil {
			return err
		}
	}
	return nil
}

// OnStart 触发 OnStart 回调
func (m *CallbackManager) OnStart(ctx context.Context, input interface{}) error {
	return m.TriggerCallbacks(func(cb Callback) error {
		return cb.OnStart(ctx, input)
	})
}

// OnEnd 触发 OnEnd 回调
func (m *CallbackManager) OnEnd(ctx context.Context, output interface{}) error {
	return m.TriggerCallbacks(func(cb Callback) error {
		return cb.OnEnd(ctx, output)
	})
}

// OnError 触发 OnError 回调
func (m *CallbackManager) OnError(ctx context.Context, err error) error {
	return m.TriggerCallbacks(func(cb Callback) error {
		return cb.OnError(ctx, err)
	})
}

// LoggingCallback 日志记录回调
//
// 将所有事件记录到日志
type LoggingCallback struct {
	*BaseCallback
	logger  Logger // 日志接口
	verbose bool
}

// Logger 日志接口
type Logger interface {
	Info(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
	Debug(msg string, fields ...interface{})
}

// NewLoggingCallback 创建日志回调
func NewLoggingCallback(logger Logger, verbose bool) *LoggingCallback {
	return &LoggingCallback{
		BaseCallback: NewBaseCallback(),
		logger:       logger,
		verbose:      verbose,
	}
}

func (l *LoggingCallback) OnStart(ctx context.Context, input interface{}) error {
	if l.verbose {
		l.logger.Info("Execution started", "input", input)
	}
	return nil
}

func (l *LoggingCallback) OnEnd(ctx context.Context, output interface{}) error {
	if l.verbose {
		l.logger.Info("Execution completed", "output", output)
	}
	return nil
}

func (l *LoggingCallback) OnError(ctx context.Context, err error) error {
	l.logger.Error("Execution error", "error", err)
	return nil
}

func (l *LoggingCallback) OnLLMStart(ctx context.Context, prompts []string, model string) error {
	l.logger.Info("LLM call started", "model", model, "num_prompts", len(prompts))
	if l.verbose {
		l.logger.Debug("LLM prompts", "prompts", prompts)
	}
	return nil
}

func (l *LoggingCallback) OnLLMEnd(ctx context.Context, output string, tokenUsage int) error {
	l.logger.Info("LLM call completed", "token_usage", tokenUsage)
	if l.verbose {
		l.logger.Debug("LLM output", "output", output)
	}
	return nil
}

func (l *LoggingCallback) OnToolStart(ctx context.Context, toolName string, input interface{}) error {
	l.logger.Info("Tool execution started", "tool", toolName, "input", input)
	return nil
}

func (l *LoggingCallback) OnToolEnd(ctx context.Context, toolName string, output interface{}) error {
	l.logger.Info("Tool execution completed", "tool", toolName, "output", output)
	return nil
}

// MetricsCallback 指标收集回调
//
// 收集执行指标（延迟、调用次数等）
type MetricsCallback struct {
	*BaseCallback
	metrics MetricsCollector
	timers  sync.Map // map[string]time.Time
}

// MetricsCollector 指标收集器接口
type MetricsCollector interface {
	IncrementCounter(name string, value int64, tags map[string]string)
	RecordHistogram(name string, value float64, tags map[string]string)
	RecordGauge(name string, value float64, tags map[string]string)
}

// NewMetricsCallback 创建指标回调
func NewMetricsCallback(metrics MetricsCollector) *MetricsCallback {
	return &MetricsCallback{
		BaseCallback: NewBaseCallback(),
		metrics:      metrics,
	}
}

func (m *MetricsCallback) OnLLMStart(ctx context.Context, prompts []string, model string) error {
	// 记录开始时间
	key := fmt.Sprintf("llm_%p", ctx)
	m.timers.Store(key, time.Now())

	// 增加调用计数
	m.metrics.IncrementCounter("llm.calls", 1, map[string]string{
		"model": model,
	})

	return nil
}

func (m *MetricsCallback) OnLLMEnd(ctx context.Context, output string, tokenUsage int) error {
	// 记录延迟
	key := fmt.Sprintf("llm_%p", ctx)
	if startTime, ok := m.timers.Load(key); ok {
		duration := time.Since(startTime.(time.Time))
		m.metrics.RecordHistogram("llm.latency", duration.Seconds(), nil)
		m.timers.Delete(key)
	}

	// 记录 token 使用量
	m.metrics.RecordHistogram("llm.tokens", float64(tokenUsage), nil)

	return nil
}

func (m *MetricsCallback) OnToolStart(ctx context.Context, toolName string, input interface{}) error {
	key := fmt.Sprintf("tool_%s_%p", toolName, ctx)
	m.timers.Store(key, time.Now())

	m.metrics.IncrementCounter("tool.calls", 1, map[string]string{
		"tool": toolName,
	})

	return nil
}

func (m *MetricsCallback) OnToolEnd(ctx context.Context, toolName string, output interface{}) error {
	key := fmt.Sprintf("tool_%s_%p", toolName, ctx)
	if startTime, ok := m.timers.Load(key); ok {
		duration := time.Since(startTime.(time.Time))
		m.metrics.RecordHistogram("tool.latency", duration.Seconds(), map[string]string{
			"tool": toolName,
		})
		m.timers.Delete(key)
	}

	return nil
}

// TracingCallback 分布式追踪回调
//
// 集成 OpenTelemetry 或其他追踪系统
type TracingCallback struct {
	*BaseCallback
	tracer Tracer
	spans  sync.Map // map[context.Context]Span
}

// Tracer 追踪器接口
type Tracer interface {
	StartSpan(ctx context.Context, name string, attrs map[string]interface{}) (context.Context, Span)
}

// Span 追踪 span 接口
type Span interface {
	End()
	SetAttribute(key string, value interface{})
	SetStatus(code StatusCode, description string)
	RecordError(err error)
}

// StatusCode 状态码
type StatusCode int

const (
	StatusCodeOK StatusCode = iota
	StatusCodeError
)

// NewTracingCallback 创建追踪回调
func NewTracingCallback(tracer Tracer) *TracingCallback {
	return &TracingCallback{
		BaseCallback: NewBaseCallback(),
		tracer:       tracer,
	}
}

func (t *TracingCallback) OnLLMStart(ctx context.Context, prompts []string, model string) error {
	_, span := t.tracer.StartSpan(ctx, "llm_call", map[string]interface{}{
		"model":       model,
		"num_prompts": len(prompts),
	})
	t.spans.Store(ctx, span)
	return nil
}

func (t *TracingCallback) OnLLMEnd(ctx context.Context, output string, tokenUsage int) error {
	if span, ok := t.spans.Load(ctx); ok {
		s := span.(Span)
		s.SetAttribute("token_usage", tokenUsage)
		s.SetStatus(StatusCodeOK, "LLM call completed")
		s.End()
		t.spans.Delete(ctx)
	}
	return nil
}

func (t *TracingCallback) OnLLMError(ctx context.Context, err error) error {
	if span, ok := t.spans.Load(ctx); ok {
		s := span.(Span)
		s.RecordError(err)
		s.SetStatus(StatusCodeError, err.Error())
		s.End()
		t.spans.Delete(ctx)
	}
	return nil
}

// CostTrackingCallback 成本追踪回调
//
// 追踪 LLM 调用的成本
type CostTrackingCallback struct {
	*BaseCallback
	totalCost   float64
	totalTokens int
	mu          sync.Mutex
	pricing     map[string]float64 // model -> cost per token
}

// NewCostTrackingCallback 创建成本追踪回调
func NewCostTrackingCallback(pricing map[string]float64) *CostTrackingCallback {
	return &CostTrackingCallback{
		BaseCallback: NewBaseCallback(),
		pricing:      pricing,
	}
}

func (c *CostTrackingCallback) OnLLMEnd(ctx context.Context, output string, tokenUsage int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalTokens += tokenUsage

	// 计算成本（简化版本，实际需要根据模型）
	// 这里假设从 context 中可以获取模型信息
	model := "gpt-4" // 从 context 获取
	if pricePerToken, ok := c.pricing[model]; ok {
		cost := float64(tokenUsage) * pricePerToken
		c.totalCost += cost
	}

	return nil
}

// GetTotalCost 获取总成本
func (c *CostTrackingCallback) GetTotalCost() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalCost
}

// GetTotalTokens 获取总 token 数
func (c *CostTrackingCallback) GetTotalTokens() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalTokens
}

// Reset 重置统计
func (c *CostTrackingCallback) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalCost = 0
	c.totalTokens = 0
}

// StdoutCallback 标准输出回调
//
// 将事件输出到标准输出，用于调试
type StdoutCallback struct {
	*BaseCallback
	color bool // 是否使用颜色
}

// NewStdoutCallback 创建标准输出回调
func NewStdoutCallback(color bool) *StdoutCallback {
	return &StdoutCallback{
		BaseCallback: NewBaseCallback(),
		color:        color,
	}
}

func (s *StdoutCallback) OnLLMStart(ctx context.Context, prompts []string, model string) error {
	if s.color {
		fmt.Printf("\033[34m[LLM START]\033[0m Model: %s, Prompts: %d\n", model, len(prompts))
	} else {
		fmt.Printf("[LLM START] Model: %s, Prompts: %d\n", model, len(prompts))
	}
	return nil
}

func (s *StdoutCallback) OnLLMEnd(ctx context.Context, output string, tokenUsage int) error {
	if s.color {
		fmt.Printf("\033[32m[LLM END]\033[0m Tokens: %d\n", tokenUsage)
	} else {
		fmt.Printf("[LLM END] Tokens: %d\n", tokenUsage)
	}
	return nil
}

func (s *StdoutCallback) OnToolStart(ctx context.Context, toolName string, input interface{}) error {
	if s.color {
		fmt.Printf("\033[33m[TOOL START]\033[0m Tool: %s\n", toolName)
	} else {
		fmt.Printf("[TOOL START] Tool: %s\n", toolName)
	}
	return nil
}

func (s *StdoutCallback) OnToolEnd(ctx context.Context, toolName string, output interface{}) error {
	if s.color {
		fmt.Printf("\033[32m[TOOL END]\033[0m Tool: %s\n", toolName)
	} else {
		fmt.Printf("[TOOL END] Tool: %s\n", toolName)
	}
	return nil
}

func (s *StdoutCallback) OnError(ctx context.Context, err error) error {
	if s.color {
		fmt.Printf("\033[31m[ERROR]\033[0m %v\n", err)
	} else {
		fmt.Printf("[ERROR] %v\n", err)
	}
	return nil
}
