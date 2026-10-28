package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// 不调用模型：构图不发请求，拓扑包含并行分支与汇总节点，且导出顺序稳定。
func TestTopology(t *testing.T) {
	g, err := build(&livemodel.Client{})
	require.NoError(t, err)
	a, err := g.Describe()
	require.NoError(t, err)
	b, err := g.Describe()
	require.NoError(t, err)
	require.Equal(t, a, b)
	kinds := map[string]string{}
	for _, n := range a.Nodes {
		kinds[n.ID] = n.Kind
	}
	require.Equal(t, "parallel", kinds["diagnose"])
	require.Equal(t, "node", kinds["summarize"])
	require.Equal(t, []string{"cpu", "db", "network"}, a.Nodes[0].Branches)
}

func TestLiveParallelDiagnostics(t *testing.T) {
	client := livemodel.Require(t)
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	tracer := provider.Tracer("diagnostics-test")
	ctx, host := tracer.Start(context.Background(), "host")
	dir := t.TempDir()
	out, err := Run(observe.WithTracer(ctx, tracer), client, dir)
	host.End()
	t.Logf("%s/%s：%+v", client.Live.Provider, client.Live.Model, out)
	require.NoError(t, err)

	for area, want := range Expected {
		if area == "decision" {
			require.Equal(t, want, out.Decision)
			continue
		}
		require.Equal(t, want, out.Findings[area], area)
	}
	require.Equal(t, int64(4), out.NewModelCalls, "三个分支加一次汇总")
	require.Equal(t, 4, out.Budget.ReservedRequests)

	require.Zero(t, out.Dropped)
	for _, kind := range []string{"graph", "node", "branch", "agent", "model"} {
		require.Equal(t, out.EventCounts[kind+"_start"], out.EventCounts[kind+"_end"], kind+" 每个开始都有结束")
		require.Positive(t, out.EventCounts[kind+"_start"], kind)
	}
	require.Equal(t, 3, out.EventCounts["branch_start"])
	require.Equal(t, int(out.NewModelCalls), out.EventCounts["model_start"])

	events, err := os.ReadFile(out.EventsPath)
	require.NoError(t, err)
	for _, secret := range []string{"诊断专家", "值班负责人", "97%", "丢包率", "结论"} {
		require.NotContains(t, string(events), secret, "默认事件不记录提示词、输入或模型正文")
	}
	report, err := os.ReadFile(out.ReportPath)
	require.NoError(t, err)
	require.Contains(t, string(report), "diagnose")
	require.Contains(t, string(report), "summarize")
	require.NotContains(t, strings.ToLower(string(report)), "<script")

	spans := exporter.GetSpans()
	ids := map[trace.SpanID]bool{host.SpanContext().SpanID(): true}
	for _, s := range spans {
		require.Equal(t, host.SpanContext().TraceID(), s.SpanContext.TraceID(), s.Name)
		ids[s.SpanContext.SpanID()] = true
	}
	var branchModels []tracetest.SpanStub
	for _, s := range spans {
		if s.Name != "host" {
			require.True(t, ids[s.Parent.SpanID()], s.Name+" 丢失父 span")
		}
		for _, a := range s.Attributes {
			if a.Key == "goagent.path" && s.Name == "model" && strings.Contains(a.Value.AsString(), "diagnose") {
				branchModels = append(branchModels, s)
			}
		}
	}
	require.Len(t, branchModels, 3)
	latestStart, earliestEnd := branchModels[0].StartTime, branchModels[0].EndTime
	for _, s := range branchModels[1:] {
		if s.StartTime.After(latestStart) {
			latestStart = s.StartTime
		}
		if s.EndTime.Before(earliestEnd) {
			earliestEnd = s.EndTime
		}
	}
	require.True(t, latestStart.Before(earliestEnd), "三个分支的模型请求应并发重叠")
}
