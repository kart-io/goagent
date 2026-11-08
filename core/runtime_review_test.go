package core

import (
	"context"
	"errors"
	"sync"
	"testing"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/stretchr/testify/require"
)

type reviewEndCallback struct {
	*BaseCallback
	failure error
}

func TestRuntimeErrorGateCallbackClassification(t *testing.T) {
	c := NewCostTrackingCallback(nil)
	ctx := context.Background()
	require.NoError(t, c.OnLLMStart(ctx, nil, "模型"))
	require.True(t, agentErrors.IsCode(c.OnLLMStart(ctx, nil, "模型"), agentErrors.CodeInvalidInput))
}

func (c reviewEndCallback) OnEnd(context.Context, interface{}) error { return c.failure }

func TestReviewRT03RunnableEndFailure(t *testing.T) {
	failure := errors.New("拒绝结果")
	f := NewRunnableFunc(func(context.Context, string) (string, error) { return "结果", nil })
	r := f.WithCallbacks(reviewEndCallback{NewBaseCallback(), failure})
	_, err := r.Invoke(context.Background(), "输入")
	require.ErrorIs(t, err, failure)
	stream, err := r.Stream(context.Background(), "输入")
	require.NoError(t, err)
	require.ErrorIs(t, (<-stream).Error, failure)
}

func TestReviewRT10CostRejectsAmbiguousContext(t *testing.T) {
	pricing := map[string]float64{"甲": 2, "乙": 3}
	c := NewCostTrackingCallback(pricing)
	pricing["甲"] = 1000
	ctx := context.Background()
	require.NoError(t, c.OnLLMStart(ctx, nil, "甲"))
	require.Error(t, c.OnLLMStart(ctx, nil, "乙"))
	require.NoError(t, c.OnLLMEnd(ctx, "", 10))
	require.Equal(t, 20.0, c.GetTotalCost())
	require.NoError(t, c.OnLLMStart(ctx, nil, "乙"))
	require.NoError(t, c.OnLLMError(ctx, errors.New("模型失败")))
	require.NoError(t, c.OnLLMStart(ctx, nil, "甲"))
	require.NoError(t, c.OnLLMEnd(ctx, "", 1))
	require.Equal(t, 22.0, c.GetTotalCost())
}

func TestReviewRT05ChainCancellation(t *testing.T) {
	for _, initial := range []bool{true, false} {
		t.Run(map[bool]string{true: "进入前取消", false: "步骤期间取消"}[initial], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			first := NewMockStep("首步", "", func(context.Context, interface{}) (interface{}, error) { calls++; cancel(); return nil, nil })
			second := NewMockStep("后续", "", func(context.Context, interface{}) (interface{}, error) { calls++; return nil, nil })
			if initial {
				cancel()
			}
			_, err := NewBaseChain("测试", []Step{first, second}).Invoke(ctx, &ChainInput{})
			require.ErrorIs(t, err, context.Canceled)
			if initial {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestReviewRT10CostUsesRequestModel(t *testing.T) {
	c := NewCostTrackingCallback(map[string]float64{"甲": 2, "乙": 3, "gpt-4": 100})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			model := "甲"
			if i%2 == 1 {
				model = "乙"
			}
			require.NoError(t, c.OnLLMStart(ctx, nil, model))
			require.NoError(t, c.OnLLMEnd(ctx, "", 10))
		}(i)
	}
	wg.Wait()
	require.Equal(t, 500.0, c.GetTotalCost())
	require.Equal(t, 200, c.GetTotalTokens())
}
