package main

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImportRulesUseCurrentModuleAndLayeredExceptions(t *testing.T) {
	for _, tc := range []struct {
		file, dep string
		bad       bool
	}{
		{"core/sub/x.go", "agents/react", true},
		{"builder/new.go", "agents/react", true},
		{"builder/reasoning_builders.go", "agents/react", false},
		{"builder/reasoning_presets.go", "agents/react", false},
		{"builder/reasoning_presets.go", "agents/executor", true},
		{"builder/reasoning_builders.go", "agents/new", true},
		{"builder/reasoning_builders.go", "middleware", true},
		{"interfaces/x.go", "utils/json", true},
		{"cache/x.go", "utils/json", false},
		{"cache/x.go", "utils/httpclient", true},
		{"utils/httpclient/client.go", "errors", false},
		{"utils/json/x.go", "errors", false},
		{"utils/x.go", "errors", false},
		{"utils/httpclient/client.go", "errors/sub", true},
		{"utils/httpclient/client.go", "core", true},
		{"tools/x.go", "examples/test", true},
		{"workflow/x.go", "core/durable", false},
	} {
		require.Equal(t, tc.bad, forbidden(tc.file, module+tc.dep), tc.file+" -> "+tc.dep)
	}
	require.False(t, forbidden("core/a.go", "example.com/agents"))
}
func TestMigrationCandidatesIgnoreCommentsAndStrings(t *testing.T) {
	source := `package demo
import (
 b "github.com/kart-io/goagent/builder"
 old "github.com/kart-io/goagent/interfaces"
)
// a.Execute(ctx,input)
const hint="a.ExecuteWithTools(ctx,input)"
var legacy old.Agent
func run(){a:=b.NewSimpleBuilder(nil); a.Execute(nil,nil); a.AsRunnable().Invoke(nil,nil)}
`
	set := token.NewFileSet()
	node, err := parser.ParseFile(set, "demo.go", source, 0)
	require.NoError(t, err)
	findings := inspect(set, "demo.go", node, "migration")
	require.Len(t, findings, 2)
	require.Contains(t, findings[0].message, "interfaces.Agent")
	require.Contains(t, findings[1].message, "candidate Execute")
}
