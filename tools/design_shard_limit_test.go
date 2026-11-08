package tools

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDesignShardCountRejectsOverflow(t *testing.T) {
	require.Panics(t, func() { NewShardedToolCacheWithOptions(WithShardCount(math.MaxUint32)) }, "过大分片数必须构造期拒绝，不得循环溢出")
}

func TestDesignRawShardConfigurationRejectsOverflow(t *testing.T) {
	for _, count := range []uint32{65537, math.MaxUint32} {
		require.Panics(t, func() { NewShardedToolCache(ShardedCacheConfig{ShardCount: count}) })
		require.Panics(t, func() { NewShardedToolCacheWithOptions(func(c *ShardedCacheConfig) { c.ShardCount = count }) })
	}
}
