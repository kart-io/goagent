package core

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
)

// Callback defines the callback handler interface.
//
// Modeled on LangChain's callback system, it provides flexible monitoring and
// debugging. Every callback method returns an error; a non-nil error aborts
// execution.
type Callback interface {
	// General callbacks
	OnStart(ctx context.Context, input interface{}) error
	OnEnd(ctx context.Context, output interface{}) error
	OnError(ctx context.Context, err error) error

	// LLM callbacks
	OnLLMStart(ctx context.Context, prompts []string, model string) error
	OnLLMEnd(ctx context.Context, output string, tokenUsage int) error
	OnLLMError(ctx context.Context, err error) error

	// Chain callbacks
	OnChainStart(ctx context.Context, chainName string, input interface{}) error
	OnChainEnd(ctx context.Context, chainName string, output interface{}) error
	OnChainError(ctx context.Context, chainName string, err error) error

	// Tool callbacks
	OnToolStart(ctx context.Context, toolName string, input interface{}) error
	OnToolEnd(ctx context.Context, toolName string, output interface{}) error
	OnToolError(ctx context.Context, toolName string, err error) error

	// Agent callbacks
	OnAgentAction(ctx context.Context, action *AgentAction) error
	OnAgentFinish(ctx context.Context, output interface{}) error
}

// AgentAction is an action performed by an agent.
type AgentAction struct {
	Tool      string                 // tool name
	ToolInput map[string]interface{} // tool input
	Log       string                 // log message
}

// BaseCallback provides a default no-op implementation of the callbacks.
//
// Users can embed BaseCallback and override only the methods they need.
type BaseCallback struct{}

// NewBaseCallback creates a base callback.
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

// CallbackManager manages multiple callback handlers.
type CallbackManager struct {
	callbacks []Callback
	mu        sync.RWMutex
}

// NewCallbackManager creates a callback manager.
func NewCallbackManager(callbacks ...Callback) *CallbackManager {
	return &CallbackManager{
		callbacks: callbacks,
	}
}

// AddCallback adds a callback.
func (m *CallbackManager) AddCallback(cb Callback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbacks = append(m.callbacks, cb)
}

// RemoveCallback removes a callback.
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

// TriggerCallbacks invokes the given method on every callback. If a callback
// panics, it returns *agentErrors.PanicError; see NotifyCallbacks for the
// semantics.
func (m *CallbackManager) TriggerCallbacks(fn func(Callback) error) error {
	_, err := m.notify(context.Background(), "trigger_callbacks", fn)
	return err
}

// notify 在锁外调用回调：回调内增删回调时持锁会死锁。本轮通知使用调用时的快照，增删从下一轮生效。
func (m *CallbackManager) notify(ctx context.Context, operation string, fn func(Callback) error) (panicked bool, err error) {
	m.mu.RLock()
	callbacks := slices.Clone(m.callbacks)
	m.mu.RUnlock()
	return NotifyCallbacks(ctx, "callback_manager", operation, callbacks, fn)
}

// OnStart triggers the OnStart callbacks.
func (m *CallbackManager) OnStart(ctx context.Context, input interface{}) error {
	_, err := m.notify(ctx, "on_start", func(cb Callback) error {
		return cb.OnStart(ctx, input)
	})
	return err
}

// OnEnd triggers the OnEnd callbacks.
func (m *CallbackManager) OnEnd(ctx context.Context, output interface{}) error {
	_, err := m.notify(ctx, "on_end", func(cb Callback) error {
		return cb.OnEnd(ctx, output)
	})
	return err
}

// OnError triggers the OnError callbacks.
func (m *CallbackManager) OnError(ctx context.Context, err error) error {
	_, cbErr := m.notify(ctx, "on_error", func(cb Callback) error {
		return cb.OnError(ctx, err)
	})
	return cbErr
}

// NotifyError triggers OnError for an operation that has already failed. Errors
// returned by callbacks are ignored; it returns *agentErrors.PanicError only
// when a callback itself panics, and the caller should return that in place of
// the original error. It returns nil when m is nil.
func (m *CallbackManager) NotifyError(ctx context.Context, err error) error {
	if m == nil {
		return nil
	}
	if panicked, panicErr := m.notify(ctx, "on_error", func(cb Callback) error {
		return cb.OnError(ctx, err)
	}); panicked {
		return panicErr
	}
	return nil
}

// LoggingCallback is a callback that logs events.
//
// It writes every event to the log.
type LoggingCallback struct {
	*BaseCallback
	logger  Logger // 日志接口
	verbose bool
}

// Logger is the logging interface.
type Logger interface {
	Info(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
	Debug(msg string, fields ...interface{})
}

// NewLoggingCallback creates a logging callback.
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

// MetricsCallback is a callback that collects metrics.
//
// It collects execution metrics such as latency and call counts.
type MetricsCallback struct {
	*BaseCallback
	metrics MetricsCollector
	timers  sync.Map // map[string]time.Time
}

// MetricsCollector is the metrics collector interface.
type MetricsCollector interface {
	IncrementCounter(name string, value int64, tags map[string]string)
	RecordHistogram(name string, value float64, tags map[string]string)
	RecordGauge(name string, value float64, tags map[string]string)
}

// NewMetricsCallback creates a metrics callback.
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

// TracingCallback is a distributed tracing callback.
//
// It integrates with OpenTelemetry or other tracing systems.
type TracingCallback struct {
	*BaseCallback
	tracer Tracer
	spans  sync.Map // map[context.Context]Span
}

// Tracer is the tracer interface.
type Tracer interface {
	StartSpan(ctx context.Context, name string, attrs map[string]interface{}) (context.Context, Span)
}

// Span is the tracing span interface.
type Span interface {
	End()
	SetAttribute(key string, value interface{})
	SetStatus(code StatusCode, description string)
	RecordError(err error)
}

// StatusCode is a span status code.
type StatusCode int

const (
	StatusCodeOK StatusCode = iota
	StatusCodeError
)

// NewTracingCallback creates a tracing callback.
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

// CostTrackingCallback is a cost tracking callback.
//
// It estimates cost from the model and unit price of paired OnLLMStart/OnLLMEnd
// events. Without a start event or a price for the model it only accumulates
// tokens; this callback does not hook into Builder model events by itself.
type CostTrackingCallback struct {
	*BaseCallback
	totalCost   float64
	totalTokens int
	mu          sync.Mutex
	pricing     map[string]float64 // model -> cost per token
	models      map[context.Context]string
}

// NewCostTrackingCallback creates a cost tracking callback.
func NewCostTrackingCallback(pricing map[string]float64) *CostTrackingCallback {
	prices := make(map[string]float64, len(pricing))
	for model, price := range pricing {
		prices[model] = price
	}
	return &CostTrackingCallback{
		BaseCallback: NewBaseCallback(),
		pricing:      prices,
		models:       make(map[context.Context]string),
	}
}

// OnLLMStart requires each concurrent call to use its own context; overlapping
// calls on the same context are rejected as an ambiguous correlation.
func (c *CostTrackingCallback) OnLLMStart(ctx context.Context, prompts []string, model string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.models[ctx]; exists {
		return agentErrors.New(agentErrors.CodeInvalidInput, "成本回调的并发模型调用必须使用独立 context").WithComponent("cost_tracking_callback").WithOperation("on_llm_start")
	}
	c.models[ctx] = model
	return nil
}

func (c *CostTrackingCallback) OnLLMError(ctx context.Context, err error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.models, ctx)
	return nil
}

func (c *CostTrackingCallback) OnLLMEnd(ctx context.Context, output string, tokenUsage int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalTokens += tokenUsage

	model, associated := c.models[ctx]
	delete(c.models, ctx)
	if pricePerToken, ok := c.pricing[model]; ok && associated {
		cost := float64(tokenUsage) * pricePerToken
		c.totalCost += cost
	}

	return nil
}

// GetTotalCost returns the total cost.
func (c *CostTrackingCallback) GetTotalCost() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalCost
}

// GetTotalTokens returns the total number of tokens.
func (c *CostTrackingCallback) GetTotalTokens() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalTokens
}

// Reset resets the statistics.
func (c *CostTrackingCallback) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalCost = 0
	c.totalTokens = 0
	clear(c.models)
}

// StdoutCallback is a callback that writes to standard output.
//
// It prints events to stdout for debugging.
type StdoutCallback struct {
	*BaseCallback
	color bool // 是否使用颜色
}

// NewStdoutCallback creates a stdout callback.
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
