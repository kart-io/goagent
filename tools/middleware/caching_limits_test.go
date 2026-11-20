package middleware

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/cache"
	"github.com/kart-io/goagent/interfaces"
)

func TestCachingZeroTTLAndCapacityAreUnlimited(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []CachingOption
		want int64
	}{
		{"默认不限条目", nil, 20},
		{"显式上限", []CachingOption{WithCacheMaxEntries(5)}, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewCachingMiddleware(tc.opts...)
			require.Zero(t, m.config.TTL, "旧默认 5 分钟")
			wrapped := m.Wrap(newMockTool(), func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				return &interfaces.ToolOutput{Success: true, Result: "r"}, nil
			})
			for i := range 20 {
				_, err := wrapped(context.Background(), &interfaces.ToolInput{Args: map[string]any{"k": fmt.Sprint(i)}})
				require.NoError(t, err)
			}
			require.Equal(t, tc.want, m.config.Cache.(*cache.SimpleCache).GetStats().Size)
		})
	}
}
