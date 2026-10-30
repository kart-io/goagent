package preset

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

const mcpServerEnv = "GOAGENT_PRESET_MCP_SERVER"

// TestMain 让测试二进制在设置 mcpServerEnv 时作为 stdio MCP 服务运行。
func TestMain(m *testing.M) {
	if os.Getenv(mcpServerEnv) == "1" {
		if err := newTestServer().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func newTestServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "1.2"}, nil)
	type input struct {
		Name string `json:"name"`
	}
	text := func(s string) *mcp.CallToolResult {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
	}
	mcp.AddTool(server, &mcp.Tool{Name: "getenv"}, func(_ context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, any, error) {
		return text(os.Getenv(in.Name)), nil, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "fail"}, func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, any, error) {
		return nil, nil, fmt.Errorf("deliberate failure")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "read_file"}, func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, any, error) {
		return text("remote read_file"), nil, nil
	})
	return server
}

func stdioRow(extra string) string {
	cfg := fmt.Sprintf(`{transport: stdio, command: %q, env: {%s: "1", TOKEN_FOR_SERVER: {secret: TOKEN}}%s}`, os.Args[0], mcpServerEnv, extra)
	return `{id: srv, use: mcp, config: ` + cfg + `}`
}

func callText(t *testing.T, tool interfaces.Tool, args map[string]any) (*interfaces.ToolOutput, string) {
	t.Helper()
	out, err := tool.Invoke(context.Background(), &interfaces.ToolInput{Args: args})
	require.NoError(t, err)
	if r, ok := out.Result.(*mcp.CallToolResult); ok && len(r.Content) > 0 {
		return out, r.Content[0].(*mcp.TextContent).Text
	}
	return out, ""
}

func byToolName(asm *Assembly) map[string]interfaces.Tool {
	m := map[string]interfaces.Tool{}
	for _, tool := range asm.Tools {
		m[tool.Name()] = tool
	}
	return m
}

func TestMCPStdio(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("GOAGENT_HOST_ONLY", "leak")
	host := Host{Root: root, ArtifactDir: t.TempDir(), Secret: func(n string) (string, bool) { return "s3cret", n == "TOKEN" }}
	src := trusted(`presets: [{id: p, components: [
  {id: w, use: workspace, config: {sandbox: danger-full-access, executor: local, network: true}},
  {id: c, use: coding-tools, config: {tools: [read_file]}},
  ` + stdioRow("") + `,
  {id: perms, use: permissions, config: {rules: [{tool: "*", decision: allow}]}}]}]`)
	asm, err := Build(ctx, mustGet(t, src), host)
	require.NoError(t, err)

	// 命名空间避免与 coding 的 read_file 冲突。
	byName := byToolName(asm)
	require.ElementsMatch(t, []string{"read_file", "mcp__srv__getenv", "mcp__srv__fail", "mcp__srv__read_file"}, toolNames(asm))
	_, got := callText(t, byName["mcp__srv__getenv"], map[string]any{"name": "TOKEN_FOR_SERVER"})
	require.Equal(t, "s3cret", got, "secrets reach the server through env")
	_, got = callText(t, byName["mcp__srv__getenv"], map[string]any{"name": "GOAGENT_HOST_ONLY"})
	require.Empty(t, got, "the server does not inherit the host environment")
	out, _ := callText(t, byName["mcp__srv__fail"], map[string]any{})
	msg, rejected := tools.RejectedResult(out)
	require.True(t, rejected, "remote error results go back to the model")
	require.Contains(t, msg, "deliberate failure")

	// 清单记录服务信息与工具来源，不含秘密的值。
	manifest, err := json.Marshal(asm.Manifest)
	require.NoError(t, err)
	require.NotContains(t, string(manifest), "s3cret")
	require.Contains(t, string(manifest), `"server":"test-server 1.2"`)
	require.Contains(t, string(manifest), `"name":"mcp__srv__getenv","component":"srv"`)

	require.NoError(t, asm.Close())
	_, err = byName["mcp__srv__getenv"].Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"name": "X"}})
	require.Error(t, err, "tools stop working after Close")

	// 子集、别名与重名。
	asm, err = Build(ctx, mustGet(t, trusted(`presets: [{id: p, components: [`+stdioRow(", tools: [getenv], aliases: {getenv: env_lookup}")+`]}]`)), host)
	require.NoError(t, err)
	require.Equal(t, []string{"env_lookup"}, toolNames(asm))
	require.NoError(t, asm.Close())
	for name, extra := range map[string]string{
		"missing tool":  ", tools: [nope]",
		"missing alias": ", aliases: {nope: x}",
	} {
		_, err := Build(ctx, mustGet(t, trusted(`presets: [{id: p, components: [`+stdioRow(extra)+`]}]`)), host)
		require.Error(t, err, name)
	}
	_, err = Build(ctx, mustGet(t, trusted(`presets: [{id: p, components: [
  {id: w, use: workspace, config: {sandbox: danger-full-access, executor: local, network: true}},
  {id: c, use: coding-tools, config: {tools: [read_file]}},
  `+stdioRow(", aliases: {read_file: read_file}")+`]}]`)), host)
	require.ErrorContains(t, err, "工具名 read_file 重复")

	// 秘密缺失时报错，信息只含名称。
	_, err = Build(ctx, mustGet(t, trusted(`presets: [{id: p, components: [`+stdioRow("")+`]}]`)), Host{ArtifactDir: t.TempDir(), Secret: func(string) (string, bool) { return "", false }})
	require.ErrorContains(t, err, "秘密 TOKEN 未设置")
}

func TestMCPTrust(t *testing.T) {
	base := trusted(`presets: [{id: p}]`)
	stdio := mustGet(t, base, project(`presets: [{id: p, components: [`+stdioRow("")+`]}]`))
	var reasons []string
	for _, e := range stdio.Expansions {
		reasons = append(reasons, e.Reason)
	}
	require.Len(t, reasons, 2)
	require.Contains(t, reasons[0], "在宿主机启动进程")
	require.Contains(t, reasons[1], "引用秘密 TOKEN")

	http := mustGet(t, base, project(`presets: [{id: p, components: [{id: srv, use: mcp, config: {transport: streamable-http, url: "http://x"}}]}]`))
	require.Len(t, http.Expansions, 1)
	require.Contains(t, http.Expansions[0].Reason, "联网")
	require.Empty(t, mustGet(t, trusted("limits: {network: true}"), base, project(`presets: [{id: p, components: [{id: srv, use: mcp, config: {transport: streamable-http, url: "http://x"}}]}]`)).Expansions)

	for name, cfg := range map[string]string{
		"no transport":   `{command: x}`,
		"stdio with url": `{transport: stdio, command: x, url: "http://x"}`,
		"http with env":  `{transport: streamable-http, url: "http://x", env: {A: b}}`,
		"bad alias":      `{transport: stdio, command: x, aliases: {a: "has space"}}`,
		"bad value":      `{transport: stdio, command: x, env: {A: {secret: X, extra: y}}}`,
	} {
		_, err := Load(trusted(`presets: [{id: p, components: [{id: srv, use: mcp, config: ` + cfg + `}]}]`))
		require.Error(t, err, name)
	}
}

func TestMCPStreamableHTTP(t *testing.T) {
	server := newTestServer()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer s3cret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer httpServer.Close()
	src := trusted(`presets: [{id: p, components: [{id: web, use: mcp, config: {transport: streamable-http, url: "` + httpServer.URL + `", headers: {Authorization: {secret: AUTH}}, tools: [fail]}}]}]`)
	asm, err := Build(context.Background(), mustGet(t, src), Host{ArtifactDir: t.TempDir(), Secret: func(n string) (string, bool) { return "Bearer s3cret", n == "AUTH" }})
	require.NoError(t, err)
	defer func() { require.NoError(t, asm.Close()) }()
	require.Equal(t, []string{"mcp__web__fail"}, toolNames(asm))

	_, err = Build(context.Background(), mustGet(t, src), Host{ArtifactDir: t.TempDir(), Secret: func(string) (string, bool) { return "wrong", true }})
	require.ErrorContains(t, err, "连接 MCP 服务")
	require.NotContains(t, err.Error(), "wrong")
}

func TestPinManifest(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "run"), store)
	build := func(prompt string) *Assembly {
		asm, err := Build(context.Background(), mustGet(t, trusted(`presets: [{id: p, components: [{id: persona, use: persona, config: {prompt: `+prompt+`}}]}]`)), Host{ArtifactDir: t.TempDir()})
		require.NoError(t, err)
		return asm
	}
	require.NoError(t, build("a").PinManifest(context.Background()), "no journal: nothing to pin")
	require.NoError(t, build("a").PinManifest(ctx))
	require.NoError(t, build("a").PinManifest(ctx), "same configuration resumes")
	err = build("b").PinManifest(ctx)
	require.ErrorIs(t, err, ErrManifestChanged)
	require.True(t, strings.Contains(err.Error(), build("a").Manifest.Digest()))
	other := durable.WithJournal(durable.WithExecutionKey(context.Background(), "other-run"), store)
	require.NoError(t, build("b").PinManifest(other), "a different run pins its own manifest")
}
