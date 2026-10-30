package search

import (
	"context"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/testing/panictest"
)

type engineFunc func(ctx context.Context, query string, maxResults int) ([]SearchResult, error)

func (f engineFunc) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	return f(ctx, query, maxResults)
}

// 聚合搜索在 goroutine 中查询各引擎：某个引擎 panic 时进程不得退出，记为该引擎失败（PanicHandler 收到 panic），其他引擎的结果照常返回。
func TestAggregatedSearchEnginePanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		recorder := &panictest.Recorder{}
		core.SetGlobalMetricsCollector(recorder)
		ok := engineFunc(func(context.Context, string, int) ([]SearchResult, error) {
			return []SearchResult{{Title: "ok", URL: "https://example.com", Score: 1}}, nil
		})
		boom := engineFunc(func(context.Context, string, int) ([]SearchResult, error) { panic("search boom") })
		results, err := NewAggregatedSearchEngine(ok, boom).Search(context.Background(), "q", 5)
		if err != nil || len(results) != 1 || results[0].Title != "ok" {
			t.Fatalf("其他引擎的结果应照常返回：%v %+v", err, results)
		}
		if events := recorder.Wait(1, 2*time.Second); len(events) != 1 || events[0].Value != "search boom" {
			t.Fatalf("PanicHandler 未收到 panic：%+v", events)
		}
	})
}
