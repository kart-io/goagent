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
		{"skills/catalog.go", "codec/yaml", false},
		{"skills/catalog.go", "errors", false},
		{"skills/catalog.go", "constants", false},
		{"skills/catalog.go", "builder", true},
		{"skills/catalog.go", "tools", true},
		{"skills/catalog.go", "interfaces", true},
		{"skills/catalog.go", "core", true},
		{"skills/catalog.go", "plugin", true},
		{"core/agent.go", "skills", true},
		{"core/agent.go", "revision", true},
		{"revision/archive.go", "skills", false},
		{"revision/archive.go", "codec/json", false},
		{"revision/archive.go", "builder", true},
		{"revision/archive.go", "management", true},
		{"management/types.go", "revision", false},
		{"tools/skill/tool.go", "skills", false},
	} {
		require.Equal(t, tc.bad, forbidden(tc.file, module+tc.dep), tc.file+" -> "+tc.dep)
	}
	require.False(t, forbidden("core/a.go", "example.com/tools"))
	require.True(t, forbidden("skills/catalog.go", "github.com/google/uuid"))
	require.False(t, forbidden("skills/catalog.go", "io/fs"))
	require.False(t, forbidden("skills/catalog_test.go", "github.com/stretchr/testify/require"))
}

func TestCoreDoesNotDependOnHarness(t *testing.T) {
	for _, file := range []string{"builder/x.go", "core/x.go", "harness/env/x.go", "tools/spill/x.go", "examples/demo/main.go"} {
		for _, dep := range []string{"github.com/kart-io/goagent-harness", "github.com/kart-io/goagent-harness/preset", "github.com/kart-io/goagent-harness/tools/coding"} {
			require.True(t, forbidden(file, dep), file+" -> "+dep)
		}
	}
	require.False(t, forbidden("core/x.go", "github.com/kart-io/goagent-harness-extra/client"))
}

// 共享常量只依赖标准库，基础层可导入它，不能经常量包反向依赖实现。
func TestConstantsAreAnIndependentLeaf(t *testing.T) {
	for _, source := range []string{"interfaces", "errors", "utils", "cache", "core", "builder", "llm", "tools", "store", "parsers"} {
		require.False(t, forbidden(source+"/x.go", module+"constants"), source)
	}
	for _, dep := range []string{"interfaces", "errors", "core", "builder", "llm/constants", "tools", "codec", "constants"} {
		require.True(t, forbidden("constants/defaults.go", module+dep), dep)
	}
	for _, dep := range []string{"time", "math", "net/http"} {
		require.False(t, forbidden("constants/defaults.go", dep), dep)
	}
	require.True(t, forbidden("constants/defaults.go", "example.com/provider/sdk"))
	require.True(t, forbidden("interfaces/x.go", module+"llm/constants"))
}

// 错误分类、提示和错误上下文字段属于 errors，协议字段与状态仍可留在 constants。
func TestErrorConstantsBelongToErrors(t *testing.T) {
	for _, tc := range []struct {
		file, src string
		bad       bool
	}{
		{"constants/tools.go", `package constants; const ErrInvalidInput = "invalid input"`, true},
		{"constants/llm.go", `package constants; const StatusInvalidAPIKey = "invalid API key"`, true},
		{"constants/llm.go", `package constants; const ErrorFieldAPIKey = "api_key"`, true},
		{"constants/codes.go", `package constants; const CodeInvalidInput = "INVALID_INPUT"`, true},
		{"errors/tools.go", `package errors; const ErrInvalidInput = "invalid input"`, false},
		{"constants/http.go", `package constants; const StatusCodeInternalServerError = 500`, false},
		{"constants/fields.go", `package constants; const FieldError = "error"`, false},
		{"constants/llm.go", `package constants; const StreamEventError = "error"`, false},
		{"constants/status.go", `package constants; const StatusError = "error"`, false},
	} {
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, tc.file, tc.src, 0)
		require.NoError(t, err)
		require.Equal(t, tc.bad, len(inspect(fset, tc.file, node)) > 0, tc.file+": "+tc.src)
	}
}

// 共享协议值和错误码直接引用唯一入口，领域自己的枚举仍可独立定义。
func TestCanonicalConstantsDoNotGainForwardingEntrypoints(t *testing.T) {
	for _, tc := range []struct {
		file, src string
		bad       bool
	}{
		{"interfaces/roles.go", `package interfaces; import c "github.com/kart-io/goagent/constants"; const RoleUser = c.RoleUser`, true},
		{"core/timeout.go", `package core; import c "github.com/kart-io/goagent/constants"; const timeout = (c.DefaultToolTimeout)`, true},
		{"tools/codes.go", `package tools; import e "github.com/kart-io/goagent/errors"; const CodeToolFailure = e.CodeToolExecution`, true},
		{"constants/defaults.go", `package constants; import "time"; const Timeout = 5*time.Second`, false},
		{"tools/policy/policy.go", `package policy; type Decision string; const Allow Decision="allow"`, false},
		{"llm/custom.go", `package llm; import c "github.com/kart-io/goagent/constants"; const Custom c.Provider = "minimax"`, false},
		{"core/options.go", `package core; import c "github.com/kart-io/goagent/constants"; const Extra = c.DefaultToolTimeout * 2`, false},
	} {
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, tc.file, tc.src, 0)
		require.NoError(t, err)
		require.Equal(t, tc.bad, len(inspect(fset, tc.file, node)) > 0, tc.file+": "+tc.src)
	}
}

// 按真实导入路径识别错误构造，覆盖别名、局部遮蔽和独立工具边界。
func TestFrameworkErrorsUseStructuredConstruction(t *testing.T) {
	for _, tc := range []struct {
		file, src string
		bad       bool
	}{
		{"core/x.go", `package core; import "errors"; var err = errors.New("failure")`, true},
		{"memory/x.go", `package memory; import stdErrors "errors"; func f() error { return stdErrors.New("failure") }`, true},
		{"core/x.go", `package core; import "errors"; var makeError = errors.New`, true},
		{"core/x.go", `package core; import . "errors"; var err = New("failure")`, true},
		{"core/x.go", `package core; import "errors"; func f(err error) bool { return errors.Is(err, nil) }`, false},
		{"core/x.go", `package core; import "errors"; func f(err error) error { return errors.Join(err, nil) }`, false},
		{"core/x.go", `package core; import "errors"; func f(err error) bool { return errors.As(err, new(*structError)) }`, false},
		{"core/x.go", `package core; import errors "github.com/kart-io/goagent/errors"; var err = errors.New(errors.CodeInternal, "failure")`, false},
		{"core/x.go", `package core; import "errors"; func f() { errors := struct{ New func(string) error }{}; _ = errors.New("local") }`, false},
		{"core/x_test.go", `package core; import "errors"; var err = errors.New("injected")`, false},
		{"errors/sentinels.go", `package errors; import stdErrors "errors"; var err = stdErrors.New("sentinel")`, false},
		{"codec/yaml/yaml.go", `package yaml; import "errors"; var err = errors.New("format")`, false},
		{"harness/env/localfs.go", `package env; import "errors"; var err = errors.New("path")`, false},
		{"harness/env_extra/x.go", `package envextra; import "errors"; var err = errors.New("failure")`, true},
		{"examples/demo/main.go", `package main; import "errors"; var err = errors.New("flag")`, false},
		{"scripts/probe/main.go", `package main; import "errors"; var err = errors.New("flag")`, false},
		{"testing/panictest/helper.go", `package panictest; import "errors"; var err = errors.New("injected")`, false},
		{"cmd/host/main.go", `package main; import "errors"; var err = errors.New("flag")`, false},
		{"evaluation/hitlbench/cmd/run/main.go", `package main; import "errors"; var err = errors.New("flag")`, false},
		{"evaluation/budget.go", `package evaluation; import "errors"; var err = errors.New("budget")`, true},
		{"core/cmd/x.go", `package command; import "errors"; var err = errors.New("failure")`, true},
	} {
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, tc.file, tc.src, 0)
		require.NoError(t, err)
		require.Equal(t, tc.bad, len(inspect(fset, tc.file, node)) > 0, tc.file+": "+tc.src)
	}
}

func TestCacheCanImportOnlyRootErrors(t *testing.T) {
	require.False(t, forbidden("cache/base.go", module+"errors"))
	require.True(t, forbidden("cache/base.go", module+"errors/sub"))
	require.True(t, forbidden("errors/sentinels.go", module+"cache"))
}

func TestScanEnforcesErrorConstructionOutsideIndependentTools(t *testing.T) {
	root := t.TempDir()
	for name, src := range map[string]string{
		"core/a.go":                    "package core\nimport plain \"errors\"\nvar err = plain.New(\"failure\")\n",
		"evaluation/budget.go":         "package evaluation\nimport \"errors\"\nvar err = errors.New(\"budget\")\n",
		"evaluation/x/cmd/run/main.go": "package main\nimport \"errors\"\nvar err = errors.New(\"flag\")\n",
		"core/a_test.go":               "package core\nimport \"errors\"\nvar err = errors.New(\"injected\")\n",
		"harness/env/localfs.go":       "package env\nimport \"errors\"\nvar err = errors.New(\"path\")\n",
	} {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	}
	count, err := scan(root)
	require.NoError(t, err)
	require.Equal(t, 2, count)
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
	write("core/c_test.go", "package core\nimport \"errors\"\nvar err = errors.New(\"injected\")\n")
	count, err := scan(root)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

// 格式化业务错误必须有分类，标准错误链包装保持原构造语义。
func TestFormattedBusinessErrorConstruction(t *testing.T) {
	for _, tc := range []struct {
		file, src string
		bad       bool
	}{
		{"core/x.go", `package x; import "fmt"; var e=fmt.Errorf("invalid input %d",1)`, true},
		{"core/x.go", `package x; import f "fmt"; var e=f.Errorf("invalid input")`, true},
		{"core/x.go", `package x; import "fmt"; var e=fmt.Errorf("literal %%w")`, true},
		{"core/x.go", `package x; import "fmt"; var e=fmt.Errorf("% %w")`, true},
		{"core/x.go", `package x; import "fmt"; var e=fmt.Errorf("%*%w",1)`, true},
		{"core/x.go", `package x; import "fmt"; var cause error; var e=fmt.Errorf("wrapped: %w",cause)`, false},
		{"core/x.go", `package x; import "fmt"; var cause error; var e=(fmt.Errorf)("wrapped: %w",cause)`, false},
		{"core/x.go", `package x; import f "fmt"; var cause error; var e=(((f.Errorf)))("wrapped: %w",cause)`, false},
		{"core/x.go", `package x; import "fmt"; var e=(fmt.Errorf)("invalid input")`, true},
		{"core/x.go", `package x; import "fmt"; var cause error; var e=fmt.Errorf("wrapped: %[1]w",cause)`, false},
		{"core/x.go", `package x; import "fmt"; var cause error; var e=fmt.Errorf("%[1]2w",cause)`, true},
		{"core/x.go", `package x; import "fmt"; var cause error; var e=fmt.Errorf("%[1].2w",cause)`, true},
		{"core/x.go", `package x; import "fmt"; var cause error; var e=fmt.Errorf("%[1][1]w",cause)`, true},
		{"core/x.go", `package x; import "fmt"; var cause error; var e=fmt.Errorf("%[0]w",cause)`, true},
		{"core/x.go", `package x; import "fmt"; var cause error; var e=fmt.Errorf("%10000010w",cause)`, true},
		{"core/x.go", `package x; import "fmt"; var cause error; var e=fmt.Errorf("%.10000010w",cause)`, true},
		{"core/x.go", `package x; import "fmt"; var cause error; var e=fmt.Errorf("%w %10000010w",cause,cause)`, false},
		{"core/x.go", `package x; import "fmt"; var cause error; var e=fmt.Errorf("%[2]*[1]w",cause,1)`, false},
		{"core/x.go", `package x; import "fmt"; var cause error; var e=fmt.Errorf("%[2]*.[3]*[1]w",cause,4,2)`, false},
		{"core/x.go", `package x; import "fmt"; var cause error; var e=fmt.Errorf("prefix " + "%w",cause)`, false},
		{"core/x.go", `package x; import "fmt"; var a,b error; var e=fmt.Errorf("%w: %w",a,b)`, false},
		{"core/x.go", `package x; import "fmt"; var newError=fmt.Errorf`, true},
		{"core/x.go", `package x; import . "fmt"; var e=Errorf("invalid")`, true},
		{"core/x.go", `package x; import "fmt"; var format string; var e=fmt.Errorf(format)`, true},
		{"core/x.go", `package x; import "fmt"; func f(){fmt:=struct{Errorf func(string)error}{}; _=fmt.Errorf("local")}`, false},
		{"core/x.go", `package x; import e "github.com/kart-io/goagent/errors"; var err=e.Newf(e.CodeInvalidInput,"invalid %d",1)`, false},
		{"codec/x.go", `package x; import "fmt"; var e=fmt.Errorf("invalid %d",1)`, false},
		{"core/durable/storetest/x.go", `package x; import "fmt"; var e=fmt.Errorf("injected %d",1)`, false},
		{"evaluation/agentbench/run.go", `package x; import "fmt"; var e=fmt.Errorf("invalid %d",1)`, true},
	} {
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, tc.file, tc.src, 0)
		require.NoError(t, err)
		require.Equal(t, tc.bad, len(inspect(fset, tc.file, node)) > 0, tc.file+": "+tc.src)
	}
}

func TestScanEnforcesFormattedErrorBoundary(t *testing.T) {
	root := t.TempDir()
	for file, src := range map[string]string{
		"core/plain.go":                     "package core\nimport f \"fmt\"\nvar e=f.Errorf(\"input %d\",1)\n",
		"core/wrapped.go":                   "package core\nimport \"fmt\"\nvar cause error\nvar e=fmt.Errorf(\"operation: %w\",cause)\n",
		"core/injected_test.go":             "package core\nimport \"fmt\"\nvar e=fmt.Errorf(\"injected %d\",1)\n",
		"core/durable/storetest/helpers.go": "package storetest\nimport \"fmt\"\nvar e=fmt.Errorf(\"injected %d\",1)\n",
	} {
		p := filepath.Join(root, file)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(src), 0o644))
	}
	findings, err := scan(root)
	require.NoError(t, err)
	require.Equal(t, 1, findings)
}

// 管理层可以装配执行能力，执行基础层不能反向依赖它或外部具体后端。
func TestManagementLayerIsAboveExecution(t *testing.T) {
	for _, source := range []string{"core", "execution", "builder", "workflow", "tools", "multiagent", "plugin"} {
		require.True(t, forbidden(source+"/x.go", module+"management"), source)
		require.True(t, forbidden(source+"/x.go", module+"management/httpapi"), source)
	}
	for _, dep := range []string{"execution", "core/durable", "builder", "workflow", "codec/json", "errors"} {
		require.False(t, forbidden("management/x.go", module+dep), dep)
	}
	for _, dep := range []string{"github.com/kart-io/goagent-contrib/management/pgstore", "github.com/jackc/pgx/v5", "github.com/jackc/pgx/v5/pgxpool"} {
		require.True(t, forbidden("management/x.go", dep), dep)
	}
	require.False(t, forbidden("management/x.go", "github.com/jackc/pgx-extra"))
}

// 分层表约束此前没有专门规则的包：只能导入同层或更低层；测试文件可以向上导入做集成验证。
func TestLayerTableForbidsUpwardImports(t *testing.T) {
	for _, tc := range []struct {
		file, dep string
		bad       bool
	}{
		{"tools/x.go", "builder", true},
		{"llm/x.go", "tools", true},
		{"llm/providers/x.go", "core/usage", false},
		{"core/session/x.go", "llm", false},
		{"core/usage/x.go", "llm", true},
		{"store/x.go", "workflow", true},
		{"workflow/x.go", "builder", true},
		{"memory/x.go", "llm", false},
		{"builder/x.go", "workflow", false},
		{"multiagent/x.go", "builder", false},
		{"management/x.go", "multiagent", false},
		{"tools/x_test.go", "builder", false},
	} {
		require.Equal(t, tc.bad, forbidden(tc.file, module+tc.dep), tc.file+" -> "+tc.dep)
	}
}

// 新的顶层目录必须先在分层表中声明所属层。
func TestUnlayeredPackageIsReported(t *testing.T) {
	_, ok := layerOf("newpkg/sub")
	require.False(t, ok)
	_, ok = layerOf("core/anything")
	require.True(t, ok)
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "newpkg/x.go", "package newpkg\n", 0)
	require.NoError(t, err)
	require.NotEmpty(t, inspect(fset, "newpkg/x.go", node))
	node, err = parser.ParseFile(fset, "newpkg/x_test.go", "package newpkg\n", 0)
	require.NoError(t, err)
	require.Empty(t, inspect(fset, "newpkg/x_test.go", node))
}
