package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImportRulesUseCurrentModuleAndLayeredExceptions(t *testing.T) {
	for _, tc := range []struct {
		file, dep string
		bad       bool
	}{
		{"core/sub/x.go", "tools/compute", true},
		{"builder/new.go", "middleware", true},
		{"parsers/x.go", "middleware", true},
		{"tools/x.go", "core", false},
		{"interfaces/x.go", "utils", true},
		{"cache/x.go", "utils", true},
		{"cache/x.go", "utils/httpclient", true},
		{"utils/httpclient/client.go", "errors", false},
		{"utils/x.go", "errors", false},
		{"utils/httpclient/client.go", "errors/sub", true},
		{"utils/httpclient/client.go", "core", true},
		{"tools/x.go", "examples/test", true},
		{"workflow/x.go", "core/durable", false},
		{"utils/x.go", "codec", false},
		{"utils/httpclient/client.go", "codec", false},
		{"cache/x.go", "codec", false},
		{"codec/x.go", "core", true},
		{"codec/x.go", "utils", true},
		{"codec/yaml/yaml.go", "codec/json", false},
		{"codec/codec.go", "codec/yaml", false},
	} {
		require.Equal(t, tc.bad, forbidden(tc.file, module+tc.dep), tc.file+" -> "+tc.dep)
	}
	require.False(t, forbidden("core/a.go", "example.com/tools"))
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

func TestPluginIsTopLevel(t *testing.T) {
	for _, file := range []string{"builder/a.go", "core/a.go", "interfaces/a.go", "llm/a.go", "tools/a.go"} {
		require.True(t, forbidden(file, module+"plugin"), file)
	}
	for _, file := range []string{"plugin/install.go", "execution/scope.go", "cmd/host/main.go"} {
		require.False(t, forbidden(file, module+"plugin"), file)
	}
}

func TestMultiagentIsAboveMechanismLayers(t *testing.T) {
	for _, file := range []string{"interfaces/x.go", "errors/x.go", "utils/x.go", "cache/x.go", "core/x.go", "builder/x.go", "tools/x.go", "parsers/x.go", "llm/x.go", "workflow/x.go", "memory/x.go", "store/x.go", "middleware/x.go", "observability/x.go", "devtools/x.go", "harness/env/x.go"} {
		require.True(t, forbidden(file, module+"multiagent"), file)
	}
	for _, file := range []string{"examples/multi-agent-patterns/main.go", "execution/scope.go", "plugin/install.go", "evaluation/bench/x.go", "testing/mocks/x.go"} {
		require.False(t, forbidden(file, module+"multiagent"), file)
	}
	for _, dep := range []string{"core", "builder", "tools", "workflow", "interfaces"} {
		require.False(t, forbidden("multiagent/x.go", module+dep), dep)
	}
	require.True(t, forbidden("multiagent/x.go", module+"execution"), "不得上行导入宿主装配层")
	require.True(t, forbidden("multiagent/x.go", module+"plugin"), "不得上行导入能力装配层")
}

func TestFormatConversionGoesThroughCodec(t *testing.T) {
	for _, tc := range []struct {
		file, src string
		bad       bool
	}{
		{"core/x.go", `package x; import "encoding/json/v2"; var _, _ = json.Marshal(1)`, true},
		{"core/x.go", `package x; import "encoding/json/v2"; var _ = json.RejectUnknownMembers(true)`, true},
		{"core/x.go", `package x; import "encoding/json/jsontext"; var _ jsontext.Value`, true},
		{"core/x.go", `package x; import "encoding/json"; var _ json.Number`, true},
		{"core/x_test.go", `package x; import "encoding/json/v2"; var _, _ = json.Marshal(1)`, true},
		{"examples/demo/main.go", `package main; import "go.yaml.in/yaml/v3"; var _ = yaml.Marshal`, true},
		{"llm/x.go", `package x; import "gopkg.in/yaml.v3"; var _ = yaml.Marshal`, true},
		{"llm/x.go", `package x; import "sigs.k8s.io/yaml"; var _ = yaml.Marshal`, true},
		{"core/x.go", `package x; import "github.com/kart-io/goagent/codec/json"; var _ json.RawMessage`, false},
		{"core/x.go", `package x; import "github.com/kart-io/goagent/codec"; var _ = codec.ReadFile`, false},
		{"core/x.go", `package x; import "encoding/xml"; var _ = xml.Marshal`, true},
		{"core/x.go", `package x; import "github.com/go-viper/mapstructure/v2"; var _ = mapstructure.Decode`, true},
		{"core/x.go", `package x; import "github.com/bytedance/sonic"; var _ = sonic.Marshal`, true},
		{"core/x.go", `package x; import "github.com/pelletier/go-toml/v2"; var _ = toml.Marshal`, true},
		{"core/x.go", `package x; import "encoding/csv"; var _ = csv.NewReader`, false},
		{"core/x.go", `package x; import "encoding/hex"; var _ = hex.EncodeToString`, false},
		{"codec/convert.go", `package codec; import "github.com/go-viper/mapstructure/v2"; var _ = mapstructure.Decode`, false},
		{"codec/json/json.go", `package json; import "encoding/json/v2"; var _, _ = json.Marshal(1)`, false},
		{"codec/yaml/yaml_test.go", `package yaml; import "go.yaml.in/yaml/v3"; var _ = yaml.Marshal`, false},
	} {
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, tc.file, tc.src, 0)
		require.NoError(t, err)
		require.Equal(t, tc.bad, len(formatFindings(fset, tc.file, node)) > 0, tc.file+": "+tc.src)
	}
}

// 测试文件只检查格式库导入，分层规则仍只约束非测试代码。
func TestScanChecksTestFilesForFormatLibsOnly(t *testing.T) {
	root := t.TempDir()
	write := func(name, src string) {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	}
	write("core/a_test.go", "package core\nimport _ \"encoding/json/v2\"\n")
	write("core/b_test.go", "package core\nimport _ \"github.com/kart-io/goagent/tools\"\n")
	count, err := scan(root)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}
