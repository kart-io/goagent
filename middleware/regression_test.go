package middleware

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/kart-io/goagent/core"
	coremiddleware "github.com/kart-io/goagent/core/middleware"
)

func TestLLMToolEmulatorMiddleware_MissingNameIsIgnored(t *testing.T) {
	m := NewLLMToolEmulatorMiddleware(&MockLLMClient{Response: "x"}).SetEmulateAll(true)

	for name, call := range map[string]map[string]interface{}{
		"缺少名称":   {"input": "x"},
		"名称类型错误": {"name": 1},
	} {
		state := core.NewAgentState()
		state.Set("pending_tool_call", call)

		assert.NotPanics(t, func() {
			got, err := m.Process(context.Background(), state)
			require.NoError(t, err, name)
			_, ok := got.Get("tool_result")
			assert.False(t, ok, name)
		}, name)
	}
}

func TestContextEnrichmentMiddleware_SyncEnricherFailureModes(t *testing.T) {
	m := NewContextEnrichmentMiddleware().AddEnricher(ContextEnricher{
		Name: "panic",
		Enrich: func(context.Context, core.State) (map[string]interface{}, error) {
			panic("富化崩溃")
		},
	}).AddEnricher(enricherReturning("ok", false, "kept"))

	var got core.State
	require.NotPanics(t, func() {
		var err error
		got, err = m.Process(context.Background(), core.NewAgentState())
		require.NoError(t, err)
	}, "同步富化器的 panic 应与异步一样记为失败")

	recorded, _ := got.Get("enrichment_errors")
	errs, ok := recorded.([]error)
	require.True(t, ok, "%T", recorded)
	assert.Len(t, errs, 1)
	merged, _ := got.Get("context")
	assert.Equal(t, map[string]interface{}{"kept": "ok"}, merged)
}

func TestContextEnrichmentMiddleware_SyncEnricherTimeout(t *testing.T) {
	m := NewContextEnrichmentMiddleware().AddEnricher(ContextEnricher{
		Name:    "slow",
		Timeout: 1,
		Enrich: func(ctx context.Context, _ core.State) (map[string]interface{}, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = m.Process(ctx, core.NewAgentState())
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cancel()
		<-done
		t.Fatal("同步富化器未应用 Timeout")
	}
}

func TestLLMToolEmulatorMiddleware_ConcurrentConfiguration(t *testing.T) {
	m := NewLLMToolEmulatorMiddleware(&MockLLMClient{Response: "x"})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); m.SetEmulateAll(true) }()
		go func() { defer wg.Done(); m.shouldEmulate("search") }()
	}
	wg.Wait()
}

func TestAdaptiveMiddleware_ConcurrentProcess(t *testing.T) {
	m := NewAdaptiveMiddleware().AddAdaptation(Adaptation{
		Condition: func(metrics *PerformanceMetrics) bool { return metrics.SuccessRate < 0 },
		Apply:     func(map[string]interface{}) {},
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = m.Process(context.Background(), core.NewAgentState())
			}
		}()
	}
	wg.Wait()
}

// 第一个样本之前没有历史，成功率不应从 0 爬升，否则“成功率过低”类规则会在启动时误触发。
func TestAdaptiveMiddleware_FirstSampleSetsSuccessRate(t *testing.T) {
	ctx := context.Background()

	m := NewAdaptiveMiddleware()
	_, err := m.Process(ctx, core.NewAgentState())
	require.NoError(t, err)
	assert.Equal(t, 1.0, m.Metrics.SuccessRate)

	failing := core.NewAgentState()
	failing.Set("error", errors.New("失败"))
	m = NewAdaptiveMiddleware()
	_, err = m.Process(ctx, failing)
	require.NoError(t, err)
	assert.Equal(t, 0.0, m.Metrics.SuccessRate)
}

func TestDynamicPromptMiddleware_NoBasePrompt(t *testing.T) {
	state := core.NewAgentState()
	_, err := NewDynamicPromptMiddleware().
		AddGenerator(PromptGenerator{Condition: always, Generate: staticPrompt("提示")}).
		Process(context.Background(), state)
	require.NoError(t, err)

	got, _ := state.Get("system_prompt")
	assert.Equal(t, "提示", got, "没有基础提示词时不应带前导空行")
}

func TestObservabilityMiddleware_HandlerErrorEndsSpan(t *testing.T) {
	mw := newObservabilityForTest(t)
	cause := errors.New("执行失败")

	var span sdktrace.ReadOnlySpan
	chain := coremiddleware.NewMiddlewareChain(func(_ context.Context, r *coremiddleware.MiddlewareRequest) (*coremiddleware.MiddlewareResponse, error) {
		span = r.Metadata["_otel_span"].(sdktrace.ReadOnlySpan)
		return nil, cause
	})
	chain.Use(mw)

	request := &coremiddleware.MiddlewareRequest{Metadata: map[string]interface{}{}}
	_, err := chain.Execute(context.Background(), request)
	require.ErrorIs(t, err, cause)

	require.NotNil(t, span)
	assert.False(t, span.EndTime().IsZero(), "span 未结束")
	assert.Equal(t, codes.Error, span.Status().Code)
	assert.NotContains(t, request.Metadata, "_otel_span")
}

func TestObservabilityMiddleware_ErrorLabelIsBounded(t *testing.T) {
	assert.Equal(t, errorTypeLabel(errors.New("请求 1 失败")), errorTypeLabel(errors.New("请求 2 失败")),
		"错误消息含动态内容，不能作为指标标签")
	assert.NotContains(t, errorTypeLabel(errors.New("secret")), "secret")
}

// 错误应记录在工具或 LLM 自己的 span 上，不能污染调用方的父 span。
func TestObservabilityMiddleware_RecordHelpersUseOwnSpan(t *testing.T) {
	mw := newObservabilityForTest(t)
	callErr := errors.New("失败")

	ctx, parent := mw.tracer.StartAgentSpan(context.Background(), "agent")
	defer parent.End()
	readable := parent.(sdktrace.ReadOnlySpan)

	mw.RecordToolCall(ctx, "search", 0, callErr)
	assert.Equal(t, codes.Unset, readable.Status().Code)

	mw.RecordLLMCall(ctx, "model", "provider", 1, 0, callErr)
	assert.Equal(t, codes.Unset, readable.Status().Code)
}

type failingBeforeMiddleware struct {
	*coremiddleware.BaseMiddleware
	err  error
	read func(sdktrace.ReadOnlySpan)
}

func (m *failingBeforeMiddleware) OnBefore(_ context.Context, r *coremiddleware.MiddlewareRequest) (*coremiddleware.MiddlewareRequest, error) {
	span, _ := r.Metadata["_otel_span"].(sdktrace.ReadOnlySpan)
	m.read(span)
	return nil, m.err
}

func (m *failingBeforeMiddleware) OnError(_ context.Context, err error) error { return err }

// 靠后的中间件在 OnBefore 失败时，已进入的中间件也要有机会收尾。
func TestObservabilityMiddleware_LaterOnBeforeFailureEndsSpan(t *testing.T) {
	mw := newObservabilityForTest(t)
	cause := errors.New("拒绝请求")

	var span sdktrace.ReadOnlySpan
	chain := coremiddleware.NewMiddlewareChain(func(context.Context, *coremiddleware.MiddlewareRequest) (*coremiddleware.MiddlewareResponse, error) {
		t.Error("OnBefore 失败后不应执行 handler")
		return nil, nil
	})
	chain.Use(mw, &failingBeforeMiddleware{BaseMiddleware: coremiddleware.NewBaseMiddleware("failing"), err: cause, read: func(s sdktrace.ReadOnlySpan) { span = s }})

	metadata := map[string]interface{}{}
	_, err := chain.Execute(context.Background(), &coremiddleware.MiddlewareRequest{Metadata: metadata})
	require.ErrorIs(t, err, cause)
	require.NotNil(t, span)
	assert.False(t, span.EndTime().IsZero(), "span 未结束")
	assert.Equal(t, codes.Error, span.Status().Code)
}
