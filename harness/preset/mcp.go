package preset

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/mcp/remote"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ComponentMCP 是 MCP 客户端组件名。
const ComponentMCP = "mcp"

// MCPConfig 是 mcp 组件的配置。一行连接一个 MCP 服务，工具名为 mcp__<组件 id>__<远端工具名>。
// 工具列表在 Build 时取快照，服务端之后的变更在下次 Build 才生效。远端返回的错误结果作为拒绝交给模型；
// 持久运行中中断或失败的调用不会在恢复时重跑，因为远端副作用无法确认。
type MCPConfig struct {
	// Transport 为 stdio 或 streamable-http。
	Transport string `yaml:"transport"`
	// Command 与 Args 是 stdio 服务的命令，在宿主机工作区根目录启动，不受沙箱约束。
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	// Env 是传给 stdio 服务的环境变量；服务只继承宿主的 PATH、HOME、USER、LANG、TMPDIR，其余须在此列出。
	Env map[string]Value `yaml:"env"`
	// URL 与 Headers 用于 streamable-http 服务。
	URL     string           `yaml:"url"`
	Headers map[string]Value `yaml:"headers"`
	// Tools 列出要提供的远端工具，缺省为全部；列出服务没有的工具时 Build 失败。
	Tools []string `yaml:"tools"`
	// Aliases 把远端工具名映射为暴露给模型的名称，须全局唯一。
	Aliases map[string]string `yaml:"aliases"`
	// StartupTimeout 是启动、握手与列出工具的上限，默认 30s。
	StartupTimeout time.Duration `yaml:"startupTimeout"`
}

// toolNamePattern 是模型提供方普遍接受的工具名。
var toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// inheritedEnv 是 stdio 服务从宿主继承的环境变量，避免把宿主的全部环境（含凭据）交给第三方进程。
var inheritedEnv = []string{"PATH", "HOME", "USER", "LANG", "TMPDIR", "SystemRoot"}

func init() {
	Register(Spec[MCPConfig]{
		Name: ComponentMCP, Version: "1",
		Validate: func(c *MCPConfig) error {
			switch c.Transport {
			case "stdio":
				if c.Command == "" {
					return errors.New("stdio 需要 command")
				}
				if c.URL != "" || len(c.Headers) > 0 {
					return errors.New("stdio 不使用 url 与 headers")
				}
			case "streamable-http":
				if c.URL == "" {
					return errors.New("streamable-http 需要 url")
				}
				if c.Command != "" || len(c.Args) > 0 || len(c.Env) > 0 {
					return errors.New("streamable-http 不使用 command、args 与 env")
				}
			default:
				return fmt.Errorf("transport %q 无效，可选 stdio、streamable-http", c.Transport)
			}
			for remoteName, alias := range c.Aliases {
				if !toolNamePattern.MatchString(alias) {
					return fmt.Errorf("工具 %s 的别名 %q 不合法，须匹配 %s", remoteName, alias, toolNamePattern)
				}
			}
			if c.StartupTimeout == 0 {
				c.StartupTimeout = 30 * time.Second
			}
			if c.StartupTimeout < 0 {
				return errors.New("startupTimeout 不能为负")
			}
			return nil
		},
		Needs: func(c *MCPConfig) Needs {
			if c.Transport == "stdio" {
				return Needs{HostProcesses: []string{c.Command}}
			}
			return Needs{Network: true}
		},
		// 允许列表中没有本服务的工具时不连接服务，也就不启动进程。
		FilterTools: func(c *MCPConfig, rowID string, f ToolFilter) bool {
			if f.Allow == nil {
				return true
			}
			for _, name := range f.Allow {
				if strings.HasPrefix(name, "mcp__"+rowID+"__") || slices.Contains(slices.Collect(maps.Values(c.Aliases)), name) {
					return true
				}
			}
			return false
		},
		Build: buildMCP,
	})
}

func buildMCP(ctx context.Context, a *Assembler, c *MCPConfig) error {
	id := a.ComponentID()
	sctx, cancel := context.WithTimeout(ctx, c.StartupTimeout)
	defer cancel()
	var transport mcp.Transport
	switch c.Transport {
	case "stdio":
		env, err := resolveValues(a, c.Env)
		if err != nil {
			return err
		}
		cmd := exec.Command(c.Command, c.Args...)
		cmd.Dir = a.Root()
		for _, name := range inheritedEnv {
			if v, ok := os.LookupEnv(name); ok {
				cmd.Env = append(cmd.Env, name+"="+v)
			}
		}
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		transport = &mcp.CommandTransport{Command: cmd}
	case "streamable-http":
		headers, err := resolveValues(a, c.Headers)
		if err != nil {
			return err
		}
		transport = &mcp.StreamableClientTransport{Endpoint: c.URL, HTTPClient: &http.Client{Transport: headerTransport{headers: headers, base: http.DefaultTransport}}}
	}
	session, err := remote.Connect(sctx, transport)
	if err != nil {
		return fmt.Errorf("连接 MCP 服务: %w", err)
	}
	a.OnClose(session.Close)
	if res := session.InitializeResult(); res != nil && res.ServerInfo != nil {
		a.SetInfo("server", res.ServerInfo.Name+" "+res.ServerInfo.Version)
	}
	seen := map[string]bool{}
	tools, err := remote.Tools(sctx, session,
		remote.WithToolName(func(name string) (string, bool) {
			seen[name] = true
			if len(c.Tools) > 0 && !slices.Contains(c.Tools, name) {
				return "", false
			}
			if alias, ok := c.Aliases[name]; ok {
				return alias, true
			}
			return "mcp__" + id + "__" + name, true
		}),
		remote.WithToolErrorsAsRejected(),
		remote.WithDurablePolicy(durable.OperationPolicy{MaxAttempts: 1}))
	if err != nil {
		return fmt.Errorf("列出 MCP 工具: %w", err)
	}
	for _, name := range c.Tools {
		if !seen[name] {
			return fmt.Errorf("MCP 服务没有工具 %s", name)
		}
	}
	for name := range c.Aliases {
		if !seen[name] {
			return fmt.Errorf("别名引用的 MCP 工具 %s 不存在", name)
		}
	}
	for _, t := range tools {
		if !toolNamePattern.MatchString(t.Name()) {
			return fmt.Errorf("工具名 %q 不合法，须匹配 %s；用 aliases 指定其他名称", t.Name(), toolNamePattern)
		}
	}
	a.AddTools(tools...)
	return nil
}

func resolveValues(a *Assembler, values map[string]Value) (map[string]string, error) {
	out := make(map[string]string, len(values))
	for k, v := range values {
		s, err := a.Value(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
		out[k] = s
	}
	return out, nil
}

// headerTransport 给每个请求加上配置的请求头。
type headerTransport struct {
	headers map[string]string
	base    http.RoundTripper
}

func (t headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return t.base.RoundTrip(req)
}
