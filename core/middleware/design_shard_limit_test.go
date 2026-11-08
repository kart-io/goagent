package middleware

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDesignCacheShardCountRejectsOverflow(t *testing.T) {
	require.Panics(t, func() { NewCacheMiddlewareWithShards(time.Minute, math.MaxUint32) }, "过大分片数必须构造期拒绝，不得产生零分片缓存")
}
