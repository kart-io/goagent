package middleware

import (
	"context"
	"errors"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
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
