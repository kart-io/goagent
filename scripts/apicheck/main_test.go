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

func TestCoreDoesNotDependOnHarness(t *testing.T) {
	for _, file := range []string{"builder/x.go", "core/x.go", "harness/env/x.go", "tools/spill/x.go", "examples/demo/main.go"} {
		for _, dep := range []string{"github.com/kart-io/goagent-harness", "github.com/kart-io/goagent-harness/preset", "github.com/kart-io/goagent-harness/tools/coding"} {
			require.True(t, forbidden(file, dep), file+" -> "+dep)
		}
	}
	require.False(t, forbidden("core/x.go", "github.com/kart-io/goagent-harness-extra/client"))
}

func TestExecutionFacadeCannotBeImportedByLowerLayers(t *testing.T) {
	for _, file := range []string{"core/x.go", "builder/x.go", "tools/x.go", "workflow/x.go", "interfaces/x.go", "llm/x.go", "memory/x.go", "store/x.go", "cache/x.go", "utils/x.go", "parsers/x.go", "middleware/x.go", "observability/x.go", "devtools/x.go", "harness/env/x.go"} {
		require.True(t, forbidden(file, module+"execution"), file)
	}
	for _, file := range []string{"execution/scope.go", "examples/host/main.go", "evaluation/executionbench/probe.go", "cmd/host/main.go"} {
		require.False(t, forbidden(file, module+"execution"), file)
	}
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
