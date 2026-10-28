package stream

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
)

// TransformMiddleware 对每个块只转换一次：用追加字符并计数的非幂等转换验证。
func TestTransformMiddlewareTransformsEachChunkOnce(t *testing.T) {
	var calls atomic.Int32
	mw := NewTransformMiddleware(func(chunk *core.LegacyStreamChunk) (*core.LegacyStreamChunk, error) {
		calls.Add(1)
		chunk.Text += "!"
		return chunk, nil
	})
	out, err := mw.Apply(context.Background(), &mockStreamOutput{chunks: textChunks("a", "b")})
	require.NoError(t, err)

	var texts []string
	for _, chunk := range rawChunks(t, out) {
		if !chunk.IsLast {
			texts = append(texts, chunk.Text)
		}
	}
	require.Equal(t, []string{"a!", "b!"}, texts)
	require.Equal(t, int32(2), calls.Load())
}
