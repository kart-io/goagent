package retrieval

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/observe"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

type panicOnCallback struct {
	*core.BaseCallback
	panicOn string
	calls   int
}

func (c *panicOnCallback) hit(name string) error {
	c.calls++
	if c.panicOn == name {
		panic("回调 panic")
	}
	return nil
}
func (c *panicOnCallback) OnStart(context.Context, any) error { return c.hit("start") }
func (c *panicOnCallback) OnEnd(context.Context, any) error   { return c.hit("end") }
func (c *panicOnCallback) OnError(context.Context, error) error {
	return c.hit("error")
}

// 回调 panic 转为 *PanicError 替代原结果返回，且不再调用后续回调。
func TestRetrieverCallbackPanicIsPanicError(t *testing.T) {
	retrieveErr := errors.New("检索失败")
	for _, panicOn := range []string{"start", "end", "error"} {
		t.Run(panicOn, func(t *testing.T) {
			var fail error
			if panicOn == "error" {
				fail = retrieveErr
			}
			r := NewFuncRetriever(func(context.Context, string) ([]*interfaces.Document, error) { return nil, fail })
			first := &panicOnCallback{BaseCallback: core.NewBaseCallback(), panicOn: panicOn}
			second := &panicOnCallback{BaseCallback: core.NewBaseCallback()}
			_, err := r.WithCallbacks(first, second).Invoke(context.Background(), "q")
			var panicErr *agentErrors.PanicError
			require.ErrorAs(t, err, &panicErr)
			require.Equal(t, "回调 panic", panicErr.Value)
			require.Equal(t, "panic", observe.ErrorKind(err))
			require.NotErrorIs(t, err, retrieveErr)
			require.Equal(t, map[string]int{"start": 0, "end": 1, "error": 1}[panicOn], second.calls)
		})
	}
}
