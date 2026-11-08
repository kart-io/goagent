package middleware

import (
	"context"
	"errors"
	"github.com/kart-io/goagent/core/state"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestRuntimeErrorGateMiddlewareClassification(t *testing.T) {
	handler := func(context.Context, *MiddlewareRequest) (*MiddlewareResponse, error) {
		return &MiddlewareResponse{}, nil
	}
	_, err := NewMiddlewareChain(handler).Execute(context.Background(), nil)
	require.True(t, agentErrors.IsCode(err, agentErrors.CodeInvalidInput))
	for _, before := range []bool{true, false} {
		mw := NewMiddlewareFunc("空对象", func(_ context.Context, r *MiddlewareRequest) (*MiddlewareRequest, error) {
			if before {
				return nil, nil
			}
			return r, nil
		}, func(context.Context, *MiddlewareResponse) (*MiddlewareResponse, error) { return nil, nil }, nil)
		_, err := NewMiddlewareChain(handler).Use(mw).Execute(context.Background(), &MiddlewareRequest{})
		require.True(t, agentErrors.IsCode(err, agentErrors.CodeMiddlewareExecution))
	}
}

func TestReviewRT01CacheTransformIsolation(t *testing.T) {
	cache := NewCacheMiddleware(time.Minute)
	calls := 0
	chain := NewMiddlewareChain(func(_ context.Context, r *MiddlewareRequest) (*MiddlewareResponse, error) {
		calls++
		return &MiddlewareResponse{Output: map[string]any{"文本": "答案"}, Metadata: r.Metadata, Headers: map[string]string{"键": "值"}, TokenUsage: &interfaces.TokenUsage{TotalTokens: 7}}, nil
	})
	chain.Use(cache, NewMiddlewareFunc("转换", nil, func(_ context.Context, r *MiddlewareResponse) (*MiddlewareResponse, error) {
		r.Output.(map[string]any)["文本"] = r.Output.(map[string]any)["文本"].(string) + "!"
		return r, nil
	}, nil))
	for i := 0; i < 3; i++ {
		r, err := chain.Execute(context.Background(), &MiddlewareRequest{Input: "同键"})
		require.NoError(t, err)
		require.Equal(t, "答案!", r.Output.(map[string]any)["文本"])
		require.Equal(t, 7, r.TokenUsage.TotalTokens)
		r.Output.(map[string]any)["文本"] = "污染"
		r.TokenUsage.TotalTokens = 999
	}
	require.Equal(t, 1, calls)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := chain.Execute(context.Background(), &MiddlewareRequest{Input: "同键"})
			require.NoError(t, err)
			require.Equal(t, "答案!", r.Output.(map[string]any)["文本"])
		}()
	}
	wg.Wait()
}

func TestReviewRT04RecoveryObjects(t *testing.T) {
	for _, kind := range []string{"普通", "不可变", "快速"} {
		for _, stage := range []string{"before", "handler", "after"} {
			if kind == "快速" && stage == "after" {
				continue
			}
			t.Run(kind+stage, func(t *testing.T) {
				failure := errors.New("可恢复")
				mw := NewMiddlewareFunc("恢复", func(_ context.Context, r *MiddlewareRequest) (*MiddlewareRequest, error) {
					if stage == "before" {
						return nil, failure
					}
					return r, nil
				}, func(_ context.Context, r *MiddlewareResponse) (*MiddlewareResponse, error) {
					if stage == "after" {
						return nil, failure
					}
					return r, nil
				}, func(ctx context.Context, err error) error {
					r, ok := RequestFromContext(ctx)
					require.True(t, ok)
					require.Equal(t, "请求", r.Input)
					return nil
				})
				handler := func(_ context.Context, r *MiddlewareRequest) (*MiddlewareResponse, error) {
					require.NotNil(t, r)
					resp := &MiddlewareResponse{Output: r.Input}
					if stage == "handler" {
						return resp, failure
					}
					return resp, nil
				}
				var execute func(context.Context, *MiddlewareRequest) (*MiddlewareResponse, error)
				switch kind {
				case "普通":
					execute = NewMiddlewareChain(handler).Use(mw).Execute
				case "不可变":
					execute = NewImmutableMiddlewareChain(handler, mw).Execute
				case "快速":
					execute = NewFastMiddlewareChain(handler, mw).Execute
				}
				r, err := execute(context.Background(), &MiddlewareRequest{Input: "请求"})
				require.NoError(t, err)
				require.NotNil(t, r)
				require.Equal(t, "请求", r.Output)
			})
		}
	}
}

func TestReviewRT08PoolReset(t *testing.T) {
	middlewareResponsePool = sync.Pool{New: func() any { return &MiddlewareResponse{TokenUsage: &interfaces.TokenUsage{TotalTokens: 999}} }}
	defer func() {
		middlewareResponsePool = sync.Pool{New: func() any { return &MiddlewareResponse{Metadata: map[string]any{}, Headers: map[string]string{}} }}
	}()
	require.Nil(t, GetMiddlewareResponse().TokenUsage)
}

func TestReviewRT09SelectorMatches(t *testing.T) {
	m := NewToolSelectorMiddleware([]string{"calculator", "search"}, 2)
	for query, want := range map[string]int{"你好": 0, "PLEASE SEARCH": 1, "calculate 2": 1, "address": 0} {
		r, err := m.OnBefore(context.Background(), &MiddlewareRequest{Input: query})
		require.NoError(t, err)
		require.Len(t, r.Metadata["selected_tools"], want)
	}
}

func TestReviewRT01CacheSnapshotsStateAndUnsupportedValues(t *testing.T) {
	for _, unsupported := range []bool{false, true} {
		t.Run(map[bool]string{true: "不可隔离则跳过", false: "嵌套状态隔离"}[unsupported], func(t *testing.T) {
			calls := 0
			cache := NewCacheMiddleware(time.Minute)
			chain := NewMiddlewareChain(func(context.Context, *MiddlewareRequest) (*MiddlewareResponse, error) {
				calls++
				s := state.NewAgentStateWithData(map[string]any{"嵌套": map[string][]byte{"字节": {1}}})
				var output any = []byte{3}
				if unsupported {
					output = make(chan int)
				}
				return &MiddlewareResponse{Output: output, State: s, Headers: map[string]string{"键": "值"}, Metadata: map[string]any{"嵌套": []int{5}}}, nil
			}).Use(cache)
			for i := 0; i < 3; i++ {
				r, err := chain.Execute(context.Background(), &MiddlewareRequest{Input: "键"})
				require.NoError(t, err)
				require.Equal(t, "值", r.Headers["键"])
				require.Equal(t, []int{5}, r.Metadata["嵌套"])
				v, _ := r.State.Get("嵌套")
				require.Equal(t, byte(1), v.(map[string][]byte)["字节"][0])
				v.(map[string][]byte)["字节"][0] = 9
				r.Headers["键"] = "污染"
				r.Metadata["嵌套"].([]int)[0] = 9
			}
			if unsupported {
				require.Equal(t, 3, calls)
				require.Zero(t, cache.Size())
			} else {
				require.Equal(t, 1, calls)
			}
		})
	}
}
