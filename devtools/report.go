package devtools

import (
	_ "embed"
	"fmt"
	"html/template"
	"io"
	"strings"

	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/workflow"
)

//go:embed assets/report.html
var reportHTML string

// WriteReport writes a standalone, escaped, script-free observation report.
// It cannot resume a workflow or infer business state from missing events.
func WriteReport(w io.Writer, graph workflow.GraphDescription, events []observe.Event) error {
	if w == nil {
		return fmt.Errorf("report writer required")
	}
	if graph.FormatVersion != "goagent-graph-v1" {
		return fmt.Errorf("unsupported graph description format")
	}
	var dropped uint64
	for _, e := range events {
		dropped += e.Dropped
	}
	t, err := template.New("report").Funcs(template.FuncMap{"eventClass": func(e observe.Event) string {
		if e.ErrorKind == "waiting" || strings.HasSuffix(e.Type, "_waiting") {
			return "waiting"
		}
		if e.ErrorKind != "" || strings.HasSuffix(e.Type, "_error") {
			return "error"
		}
		if e.Replayed {
			return "replayed"
		}
		return ""
	}}).Parse(reportHTML)
	if err != nil {
		return err
	}
	return t.Execute(w, struct {
		Graph   workflow.GraphDescription
		Events  []observe.Event
		Dropped uint64
	}{graph, events, dropped})
}
