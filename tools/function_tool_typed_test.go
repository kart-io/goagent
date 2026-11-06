package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools/toolargs"
)

// 类型化工具解码 toolargs.Parse 的结果，int64 ID 不经 float64 改写。
func TestTypedFunctionToolDecodesExactArguments(t *testing.T) {
	args, ok := toolargs.Parse(`{"id":1234567890123456789,"n":1}`)
	require.True(t, ok)
	type in struct {
		ID int64 `json:"id"`
		N  int   `json:"n"`
	}
	typed := NewTypedFunctionTool("lookup", "lookup", `{"type":"object"}`, func(_ context.Context, v in) (in, error) { return v, nil })
	out, err := typed.Invoke(context.Background(), &interfaces.ToolInput{Args: args})
	require.NoError(t, err)
	require.Equal(t, in{ID: 1234567890123456789, N: 1}, out.Result)
	require.Equal(t, json.Number("1234567890123456789"), args["id"])
}
