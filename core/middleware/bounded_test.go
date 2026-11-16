package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/kart-io/goagent/constants"
	"github.com/stretchr/testify/require"
)

// 计时只保留最近的窗口，平均值仍按全部记录计算。
func TestTimingMiddlewareKeepsBoundedWindow(t *testing.T) {
	mw := NewTimingMiddleware()
	n := constants.DefaultTimingWindow + 100
	for i := 1; i <= n; i++ {
		_, err := mw.OnAfter(context.Background(), &MiddlewareResponse{Duration: time.Duration(i)})
		require.NoError(t, err)
	}
	require.Len(t, mw.GetTimings(), constants.DefaultTimingWindow)
	require.Equal(t, time.Duration((n+1)/2), mw.GetAverageLatency())
}
