package devtools

import (
	"bytes"
	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOfflineReportEscapesContent(t *testing.T) {
	var out bytes.Buffer
	err := WriteReport(&out, workflow.GraphDescription{FormatVersion: "goagent-graph-v1", Nodes: []workflow.NodeDescription{{ID: `<script>alert(1)</script>`, Kind: "node"}}}, []observe.Event{{Type: "node_waiting", Path: `<img src=x onerror=alert(1)>`}, {Type: "node_error", ErrorKind: "execution"}, {Type: "node_replayed", Replayed: true}, {Type: "events_dropped", Dropped: 2}})
	require.NoError(t, err)
	require.NotContains(t, out.String(), "<script>")
	require.NotContains(t, out.String(), "<img")
	require.Contains(t, out.String(), "&lt;script&gt;")
	require.Contains(t, out.String(), "不是恢复依据")
	require.Contains(t, out.String(), "缺失")
	require.Contains(t, out.String(), "node_waiting")
}
