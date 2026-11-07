package common_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
	"github.com/stretchr/testify/require"
)

func TestExecuteWithRetry真实延迟边界(t *testing.T) {
	t.Setenv("GO_TEST_MODE", "")
	for _, delay := range []time.Duration{0, time.Nanosecond, -time.Nanosecond} {
		t.Run(delay.String(), func(t *testing.T) {
			calls := 0
			require.NotPanics(t, func() {
				value, err := common.ExecuteWithRetry(context.Background(), common.RetryConfig{MaxAttempts: 2, BaseDelay: delay}, "test", func(context.Context) (int, error) {
					calls++
					if calls == 1 {
						return 0, agentErrors.NewLLMRateLimitError("test", "m", 0)
					}
					return 42, nil
				})
				if delay < 0 {
					require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
					require.Zero(t, calls)
				} else {
					require.NoError(t, err)
					require.Equal(t, 42, value)
					require.Equal(t, 2, calls)
				}
			})
		})
	}
}

func TestExecuteWithRetry取消后不再调用(t *testing.T) {
	t.Setenv("GO_TEST_MODE", "")
	for _, canceledBefore := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		if canceledBefore {
			cancel()
		}
		calls := 0
		_, err := common.ExecuteWithRetry(ctx, common.RetryConfig{MaxAttempts: 3}, "test", func(context.Context) (int, error) {
			calls++
			cancel()
			return 0, agentErrors.NewLLMRateLimitError("test", "m", 0)
		})
		require.ErrorIs(t, err, context.Canceled)
		if canceledBefore {
			require.Zero(t, calls)
		} else {
			require.Equal(t, 1, calls)
		}
	}
}

func TestIsRetryable尊重提供方与标准取消(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, &llm.ProviderError{Err: errors.New("证书验证失败")}} {
		err := agentErrors.NewLLMRequestError("test", "m", cause)
		require.False(t, common.IsRetryable(err), "%v", err)
	}
	require.True(t, common.IsRetryable(&llm.ProviderError{StatusCode: 503, Err: errors.New("服务暂不可用")}))
}

func TestExecuteWithRetry超时与退避饱和(t *testing.T) {
	t.Setenv("GO_TEST_MODE", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	calls := 0
	_, err := common.ExecuteWithRetry(ctx, common.RetryConfig{MaxAttempts: 3, BaseDelay: time.Duration(1<<63 - 1)}, "test", func(context.Context) (int, error) {
		calls++
		return 0, &llm.ProviderError{StatusCode: 503, Err: errors.New("服务暂不可用")}
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, agentErrors.CodeContextTimeout, agentErrors.GetCode(err))
	require.Equal(t, 1, calls)
}

func TestExecuteWithRetry真实HTTP保留重试信息(t *testing.T) {
	t.Setenv("GO_TEST_MODE", "")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	_, err := common.ExecuteWithRetry(context.Background(), common.RetryConfig{MaxAttempts: 2}, "test", func(ctx context.Context) (int, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		require.NoError(t, err)
		response, err := server.Client().Do(req)
		require.NoError(t, err)
		response.Body.Close()
		return 0, common.MapHTTPError(common.HTTPError{StatusCode: response.StatusCode, Headers: map[string]string{"Retry-After": response.Header.Get("Retry-After")}}, "test", "m", nil)
	})
	require.Equal(t, 2, calls)
	var pe *llm.ProviderError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, 503, pe.StatusCode)
	require.Equal(t, 2*time.Second, pe.RetryAfter)
	require.Equal(t, 2, agentErrors.GetContext(err)["retry_attempt"])
	require.True(t, common.IsRetryable(err))
}
