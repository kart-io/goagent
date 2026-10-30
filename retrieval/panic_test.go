package retrieval

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/testing/panictest"
)

// panicRetriever 的检索在框架 goroutine 中 panic，模拟宿主实现的检索器。
type panicRetriever struct{ *BaseRetriever }

func (panicRetriever) GetRelevantDocuments(context.Context, string) ([]*interfaces.Document, error) {
	panic("retriever boom")
}

// okRetriever 正常返回一篇文档。
type okRetriever struct{ *BaseRetriever }

func (okRetriever) GetRelevantDocuments(context.Context, string) ([]*interfaces.Document, error) {
	return []*interfaces.Document{{ID: "ok", PageContent: "ok"}}, nil
}

// 集成检索与混合检索在 goroutine 中调用检索器：panic 转为可识别的 PanicError 作为检索错误返回，进程不得退出。
func TestRetrieverGoroutinePanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		bad := panicRetriever{NewBaseRetriever()}
		good := okRetriever{NewBaseRetriever()}
		for name, r := range map[string]Retriever{
			"ensemble": NewEnsembleRetriever([]Retriever{good, bad}, []float64{1, 1}, RetrieverConfig{TopK: 4}),
			"hybrid":   NewHybridRetriever(bad, good, 0.5, 0.5, RetrieverConfig{TopK: 4}),
		} {
			_, err := r.GetRelevantDocuments(context.Background(), "q")
			var p *agentErrors.PanicError
			require.True(t, errors.As(err, &p), "%s: %v", name, err)
			require.Equal(t, "retriever boom", p.Value, name)
		}
	})
}
