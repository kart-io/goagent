package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/testing/panictest"
)

// 异步 enricher 在 goroutine 中 panic 时进程不得退出：记为该 enricher 的失败（可识别为 PanicError），其他 enricher 的数据照常写入。
func TestContextEnrichmentAsyncPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		m := NewContextEnrichmentMiddleware().
			AddEnricher(ContextEnricher{Name: "boom", Async: true, Enrich: func(context.Context, core.State) (map[string]interface{}, error) {
				panic("enrich boom")
			}}).
			AddEnricher(ContextEnricher{Name: "ok", Async: true, Enrich: func(context.Context, core.State) (map[string]interface{}, error) {
				return map[string]interface{}{"k": "v"}, nil
			}})
		state, err := m.Process(context.Background(), core.NewAgentState())
		if err != nil {
			t.Fatal(err)
		}
		errs, _ := state.Get("enrichment_errors")
		list, _ := errs.([]error)
		var p *agentErrors.PanicError
		if len(list) != 1 || !errors.As(list[0], &p) || p.Value != "enrich boom" {
			t.Fatalf("应记录一个 PanicError：%v", errs)
		}
		ctxVal, _ := state.Get("context")
		if m, _ := ctxVal.(map[string]interface{}); m["k"] != "v" {
			t.Fatalf("其他 enricher 的数据应照常写入：%v", ctxVal)
		}
	})
}
