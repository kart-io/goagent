package devtools

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/observe"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/testing/panictest"
)

type panicWriter struct{}

func (panicWriter) Write([]byte) (int, error) { panic("writer boom") }

// 宿主 Writer 在录制 goroutine 中 panic 时进程不得退出，Close 返回可识别的 PanicError。
func TestRecorderWriterPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		r, err := NewRecorder(RecorderConfig{Writer: panicWriter{}})
		require.NoError(t, err)
		r.Observe(context.Background(), observe.Event{Type: "node_start"})
		err = r.Close(context.Background())
		var p *agentErrors.PanicError
		require.True(t, errors.As(err, &p), "应返回 PanicError：%v", err)
		require.Equal(t, "writer boom", p.Value)
	})
}
