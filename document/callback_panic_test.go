package document

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
)

type panicErrorCallback struct{ *core.BaseCallback }

func (panicErrorCallback) OnError(context.Context, error) error { panic("回调 panic") }

// 加载失败时 OnError 自身 panic，转为 *PanicError 替代原错误返回。
func TestLoaderOnErrorPanicIsPanicError(t *testing.T) {
	loader := NewTextLoader(TextLoaderConfig{
		FilePath:        filepath.Join(t.TempDir(), "missing.txt"),
		CallbackManager: core.NewCallbackManager(panicErrorCallback{BaseCallback: core.NewBaseCallback()}),
	})
	_, err := loader.Load(context.Background())
	var panicErr *agentErrors.PanicError
	require.ErrorAs(t, err, &panicErr)
	require.Equal(t, "回调 panic", panicErr.Value)
	require.NotContains(t, err.Error(), "failed to read file")
}
