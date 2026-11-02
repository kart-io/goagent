package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	coremiddleware "github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/observability"
)

func always(core.State) bool { return true }

func staticPrompt(text string) func(core.State) string {
	return func(core.State) string { return text }
}

func TestDynamicPromptMiddleware(t *testing.T) {
	ctx := context.Background()
	m := NewDynamicPromptMiddleware().
		AddGenerator(PromptGenerator{Name: "low", Priority: 1, Condition: always, Generate: staticPrompt("低")}).
		AddGenerator(PromptGenerator{Name: "high", Priority: 9, Condition: always, Generate: staticPrompt("高")}).
		AddGenerator(PromptGenerator{Name: "empty", Priority: 5, Condition: always, Generate: staticPrompt("")}).
		AddGenerator(PromptGenerator{
			Name:      "off",
			Priority:  7,
			Condition: func(core.State) bool { return false },
			Generate:  staticPrompt("不应出现"),
		})

	state := core.NewAgentState()
	state.Set("system_prompt", "基础")
	got, err := m.Process(ctx, state)
	require.NoError(t, err)

	prompt, _ := got.Get("system_prompt")
	assert.Equal(t, "基础\n\n高\n\n低", prompt, "按优先级排序，跳过未命中条件和空内容")
	enhancements, _ := got.Get("prompt_enhancements")
	assert.Equal(t, []string{"高", "低"}, enhancements)

	untouched := core.NewAgentState()
	untouched.Set("system_prompt", "基础")
	_, err = NewDynamicPromptMiddleware().Process(ctx, untouched)
	require.NoError(t, err)
	prompt, _ = untouched.Get("system_prompt")
	assert.Equal(t, "基础", prompt)
	_, ok := untouched.Get("prompt_enhancements")
	assert.False(t, ok)
}

func pendingToolState(name string) core.State {
	state := core.NewAgentState()
	state.Set("pending_tool_call", map[string]interface{}{"name": name, "input": "参数"})
	return state
}

func TestLLMToolEmulatorMiddleware_Selection(t *testing.T) {
	ctx := context.Background()
	model := &MockLLMClient{Response: "模拟结果"}

	tests := []struct {
		name     string
		mw       *LLMToolEmulatorMiddleware
		tool     string
		emulated bool
	}{
		{"默认不模拟", NewLLMToolEmulatorMiddleware(model), "search", false},
		{"指定工具", NewLLMToolEmulatorMiddleware(model).SetEmulatedTools("search"), "search", true},
		{"其他工具", NewLLMToolEmulatorMiddleware(model).SetEmulatedTools("search"), "calc", false},
		{"模拟全部", NewLLMToolEmulatorMiddleware(model).SetEmulateAll(true), "calc", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.mw.Process(ctx, pendingToolState(tt.tool))
			require.NoError(t, err)

			result, hasResult := got.Get("tool_result")
			_, hasFlag := got.Get("tool_emulated")
			assert.Equal(t, tt.emulated, hasResult)
			assert.Equal(t, tt.emulated, hasFlag)
			if tt.emulated {
				assert.Equal(t, "模拟结果", result)
			}
		})
	}
}

func TestLLMToolEmulatorMiddleware_PassThroughAndError(t *testing.T) {
	ctx := context.Background()
	m := NewLLMToolEmulatorMiddleware(&MockLLMClient{Response: "x"}).SetEmulateAll(true)

	got, err := m.Process(ctx, core.NewAgentState())
	require.NoError(t, err)
	_, ok := got.Get("tool_result")
	assert.False(t, ok, "没有待处理调用时不做任何事")

	wrongType := core.NewAgentState()
	wrongType.Set("pending_tool_call", "不是 map")
	got, err = m.Process(ctx, wrongType)
	require.NoError(t, err)
	_, ok = got.Get("tool_result")
	assert.False(t, ok)

	cause := errors.New("模型不可用")
	failing := NewLLMToolEmulatorMiddleware(&MockLLMClient{Error: cause}).SetEmulateAll(true)
	_, err = failing.Process(ctx, pendingToolState("search"))
	assert.ErrorIs(t, err, cause)
}

func TestAdaptiveMiddleware_AppliesAdaptationOnNextProcess(t *testing.T) {
	ctx := context.Background()
	m := NewAdaptiveMiddleware().
		AddAdaptation(Adaptation{
			Name:      "降温",
			Condition: func(*PerformanceMetrics) bool { return true },
			Apply:     func(cfg map[string]interface{}) { cfg["temperature"] = 0.1 },
		}).
		AddAdaptation(Adaptation{
			Name:      "不触发",
			Condition: func(*PerformanceMetrics) bool { return false },
			Apply:     func(cfg map[string]interface{}) { cfg["never"] = true },
		})

	first := core.NewAgentState()
	_, err := m.Process(ctx, first)
	require.NoError(t, err)
	_, ok := first.Get("temperature")
	assert.False(t, ok, "规则在本次处理之后才生效")

	second := core.NewAgentState()
	_, err = m.Process(ctx, second)
	require.NoError(t, err)
	got, _ := second.Get("temperature")
	assert.Equal(t, 0.1, got)
	_, ok = second.Get("never")
	assert.False(t, ok)
}

func TestAdaptiveMiddleware_Metrics(t *testing.T) {
	ctx := context.Background()
	m := NewAdaptiveMiddleware()

	for i := 0; i < 105; i++ {
		_, err := m.Process(ctx, core.NewAgentState())
		require.NoError(t, err)
	}
	assert.Len(t, m.Metrics.ResponseTimes, 100, "只保留最近 100 次")

	afterSuccess := m.Metrics.SuccessRate
	assert.Greater(t, afterSuccess, 0.0)

	failed := core.NewAgentState()
	failed.Set("error", errors.New("失败"))
	_, err := m.Process(ctx, failed)
	require.NoError(t, err)
	assert.Less(t, m.Metrics.SuccessRate, afterSuccess, "错误应降低成功率")
}

func enricherReturning(name string, async bool, key string) ContextEnricher {
	return ContextEnricher{
		Name:  name,
		Async: async,
		Enrich: func(context.Context, core.State) (map[string]interface{}, error) {
			return map[string]interface{}{key: name}, nil
		},
	}
}

func TestContextEnrichmentMiddleware_Merges(t *testing.T) {
	m := NewContextEnrichmentMiddleware().
		AddEnricher(enricherReturning("sync", false, "a")).
		AddEnricher(enricherReturning("async", true, "b"))

	state := core.NewAgentState()
	state.Set("context", map[string]interface{}{"existing": 1})
	got, err := m.Process(context.Background(), state)
	require.NoError(t, err)

	merged, _ := got.Get("context")
	assert.Equal(t, map[string]interface{}{"existing": 1, "a": "sync", "b": "async"}, merged)
	flag, _ := got.Get("context_enriched")
	assert.Equal(t, true, flag)
	_, hasErrors := got.Get("enrichment_errors")
	assert.False(t, hasErrors)
}

func TestContextEnrichmentMiddleware_NoData(t *testing.T) {
	got, err := NewContextEnrichmentMiddleware().Process(context.Background(), core.NewAgentState())
	require.NoError(t, err)
	_, ok := got.Get("context_enriched")
	assert.False(t, ok)
}

func TestContextEnrichmentMiddleware_FailuresDoNotFailProcess(t *testing.T) {
	cause := errors.New("富化失败")
	failing := func(context.Context, core.State) (map[string]interface{}, error) { return nil, cause }

	m := NewContextEnrichmentMiddleware().
		AddEnricher(ContextEnricher{Name: "sync", Enrich: failing}).
		AddEnricher(ContextEnricher{Name: "async", Async: true, Enrich: failing}).
		AddEnricher(ContextEnricher{
			Name:  "panic",
			Async: true,
			Enrich: func(context.Context, core.State) (map[string]interface{}, error) {
				panic("富化崩溃")
			},
		}).
		AddEnricher(enricherReturning("ok", true, "kept"))

	got, err := m.Process(context.Background(), core.NewAgentState())
	require.NoError(t, err)

	recorded, _ := got.Get("enrichment_errors")
	errs, ok := recorded.([]error)
	require.True(t, ok, "%T", recorded)
	assert.Len(t, errs, 3)

	merged, _ := got.Get("context")
	assert.Equal(t, map[string]interface{}{"kept": "ok"}, merged, "其他富化器的结果保留")
}

func TestContextEnrichmentMiddleware_AsyncTimeout(t *testing.T) {
	m := NewContextEnrichmentMiddleware().AddEnricher(ContextEnricher{
		Name:    "slow",
		Async:   true,
		Timeout: 10 * time.Millisecond,
		Enrich: func(ctx context.Context, _ core.State) (map[string]interface{}, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})

	got, err := m.Process(context.Background(), core.NewAgentState())
	require.NoError(t, err)

	recorded, _ := got.Get("enrichment_errors")
	errs, ok := recorded.([]error)
	require.True(t, ok, "%T", recorded)
	require.Len(t, errs, 1)
	assert.ErrorIs(t, errs[0], context.DeadlineExceeded)
}

func newObservabilityForTest(t *testing.T) *ObservabilityMiddleware {
	t.Helper()
	provider, err := observability.NewTelemetryProvider(&observability.TelemetryConfig{
		ServiceName: "test", TraceEnabled: true, TraceExporter: "noop", TraceSampleRate: 1,
		MetricsEnabled: true, MetricsExporter: "noop", MetricsInterval: time.Minute,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	mw, err := NewObservabilityMiddleware(provider)
	require.NoError(t, err)
	return mw
}

func TestObservabilityMiddleware_OnAfterCleansMetadata(t *testing.T) {
	ctx := context.Background()
	mw := newObservabilityForTest(t)

	for name, respErr := range map[string]error{"成功": nil, "失败": errors.New("失败")} {
		t.Run(name, func(t *testing.T) {
			req, err := mw.OnBefore(ctx, &coremiddleware.MiddlewareRequest{
				Timestamp: time.Now(),
				Metadata:  map[string]interface{}{"agent.name": "agent"},
			})
			require.NoError(t, err)
			require.Contains(t, req.Metadata, "_otel_span")

			metadata := req.Metadata
			metadata["agent.type"] = "custom"
			resp, err := mw.OnAfter(ctx, &coremiddleware.MiddlewareResponse{Metadata: metadata, Error: respErr})
			require.NoError(t, err)
			assert.NotContains(t, resp.Metadata, "_otel_span")
			assert.NotContains(t, resp.Metadata, "_otel_ctx")
		})
	}

	resp, err := mw.OnAfter(ctx, &coremiddleware.MiddlewareResponse{})
	require.NoError(t, err, "没有 metadata 的响应也不应出错")
	assert.Nil(t, resp.Metadata)
}

func TestObservabilityMiddleware_OnErrorReturnsOriginalError(t *testing.T) {
	cause := errors.New("执行失败")
	assert.Equal(t, cause, newObservabilityForTest(t).OnError(context.Background(), cause))
}

func TestObservabilityMiddleware_RecordHelpersAcceptBothOutcomes(t *testing.T) {
	ctx := context.Background()
	mw := newObservabilityForTest(t)

	for _, callErr := range []error{nil, errors.New("失败")} {
		assert.NotPanics(t, func() {
			mw.RecordToolCall(ctx, "search", time.Millisecond, callErr)
			mw.RecordLLMCall(ctx, "model", "provider", 10, time.Millisecond, callErr)
		})
	}
}
