package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
)

func TestCacheConfigRejectsRetiredFields(t *testing.T) {
	for _, input := range []string{
		`{"enabled":true,"type":"memory"}`,
		`{"enabled":true,"max_size":100}`,
		`{"enabled":true,"cleanup_interval":"1m"}`,
	} {
		t.Run(input, func(t *testing.T) {
			config := DefaultCacheConfig()
			require.Error(t, json.Unmarshal([]byte(input), &config, json.Strict), "已退役配置必须显式报错，不能被接受后忽略")
		})
	}
}

func TestCacheConfigUsesDeclaredTTL(t *testing.T) {
	config := DefaultCacheConfig()
	require.NoError(t, json.Unmarshal([]byte(`{"default_ttl":"10ms"}`), &config, json.Strict))
	configured := NewCacheFromConfig(config)
	cache, ok := configured.(*SimpleCache)
	require.True(t, ok)
	t.Cleanup(cache.Close)
	ctx := context.Background()
	require.NoError(t, configured.Set(ctx, "configured", "value", 0))
	require.Eventually(t, func() bool {
		_, err := configured.Get(ctx, "configured")
		return err == agentErrors.ErrCacheMiss
	}, time.Second, time.Millisecond, "默认TTL必须作用于实际写入的条目")
}
