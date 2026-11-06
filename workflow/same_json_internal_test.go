package workflow

import (
	"testing"

	"github.com/kart-io/goagent/codec/json"
	"github.com/stretchr/testify/require"
)

// 恢复输入按压缩后字节比较：与 v1 一样容忍重复键，只忽略空白差异。
func TestSameJSONToleratesDuplicateKeys(t *testing.T) {
	dup := json.RawMessage(`{"decision":"approve","decision":"approve"}`)
	require.True(t, sameJSON(dup, dup))
	require.True(t, sameJSON(json.RawMessage(`{"a": 1}`), json.RawMessage(`{"a":1}`)), "空白差异相同")
	require.False(t, sameJSON(json.RawMessage(`{"a":1,"b":2}`), json.RawMessage(`{"b":2,"a":1}`)), "键顺序不同算不同")
	require.False(t, sameJSON(json.RawMessage(`{"a":`), json.RawMessage(`{"a":`)), "非法 JSON 不相同")
}
