// Package remote adapts official MCP client sessions to goagent tools.
package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Connect negotiates MCP over an SDK transport (StreamableClientTransport or
// CommandTransport, for example). The caller must Close the returned session.
func Connect(ctx context.Context, transport mcp.Transport) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "goagent", Version: "1.0.0"}, nil)
	return client.Connect(ctx, transport, nil)
}

// Option 配置 Tools 适配出的工具。
type Option func(*options)

type options struct {
	name           func(string) (string, bool)
	rejectToolErrs bool
	policy         *durable.OperationPolicy
}

// WithToolName 决定每个远端工具暴露给模型的名称；返回 false 时不提供该工具。
func WithToolName(name func(remote string) (string, bool)) Option {
	return func(o *options) { o.name = name }
}

// WithToolErrorsAsRejected 把远端工具返回的错误结果（IsError）作为 tools.Rejected 交给模型，而不是中止运行。
// 连接与协议错误仍以 Go error 返回。
func WithToolErrorsAsRejected() Option {
	return func(o *options) { o.rejectToolErrs = true }
}

// WithDurablePolicy 声明工具在持久运行中的日志策略（tools.DurablePolicyProvider），
// 如 MaxAttempts 为 1 时，中断或失败的调用不会在恢复时重跑，副作用无法确认的远端调用不被盲目重试。
func WithDurablePolicy(p durable.OperationPolicy) Option {
	return func(o *options) { o.policy = &p }
}

// Tools lists all pages and adapts each remote tool. Session lifetime must exceed
// tool use. Authentication and transport limits are configured on the SDK client
// transport; remote error results become Go errors instead of successful outputs
// unless WithToolErrorsAsRejected is given.
func Tools(ctx context.Context, session *mcp.ClientSession, opts ...Option) ([]interfaces.Tool, error) {
	if session == nil {
		return nil, fmt.Errorf("MCP session is required")
	}
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	var result []interfaces.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		name := tool.Name
		if o.name != nil {
			var ok bool
			if name, ok = o.name(tool.Name); !ok {
				continue
			}
		}
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("MCP tool schema %s: %w", tool.Name, err)
		}
		rt := &remoteTool{session: session, name: name, remoteName: tool.Name, description: tool.Description, schema: string(schema), rejectToolErrs: o.rejectToolErrs}
		if o.policy != nil {
			result = append(result, &policyTool{remoteTool: rt, policy: *o.policy})
		} else {
			result = append(result, rt)
		}
	}
	return result, nil
}

type remoteTool struct {
	session                               *mcp.ClientSession
	name, remoteName, description, schema string
	rejectToolErrs                        bool
}

type policyTool struct {
	*remoteTool
	policy durable.OperationPolicy
}

func (t *policyTool) DurableOperationPolicy() durable.OperationPolicy { return t.policy }

var _ tools.DurablePolicyProvider = (*policyTool)(nil)

func (t *remoteTool) Name() string        { return t.name }
func (t *remoteTool) Description() string { return t.description }
func (t *remoteTool) ArgsSchema() string  { return t.schema }
func (t *remoteTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	if input == nil {
		return nil, fmt.Errorf("MCP tool input is required")
	}
	result, err := t.session.CallTool(ctx, &mcp.CallToolParams{Name: t.remoteName, Arguments: input.Args})
	if err != nil {
		return nil, fmt.Errorf("MCP tool %s: %w", t.name, err)
	}
	if result == nil {
		return nil, fmt.Errorf("MCP tool %s returned nil result", t.name)
	}
	if result.NeedsInput() {
		return nil, fmt.Errorf("MCP tool %s requires an input response; use the SDK session to continue", t.name)
	}
	if result.IsError {
		if t.rejectToolErrs {
			return tools.Rejected(fmt.Sprintf("MCP tool %s returned an error: %s", t.name, errorText(result)), nil), nil
		}
		return nil, fmt.Errorf("MCP tool %s: %s", t.name, errorText(result))
	}
	// Keep the complete MCP result, including text, images and structured content.
	return &interfaces.ToolOutput{Result: result, Success: true}, nil
}

// errorText 取错误结果的说明：服务端以文本内容返回错误信息，GetError 只在客户端侧设置时非空。
func errorText(result *mcp.CallToolResult) string {
	if err := result.GetError(); err != nil {
		return err.Error()
	}
	var parts []string
	for _, c := range result.Content {
		if text, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	if len(parts) == 0 {
		return "no error message"
	}
	return strings.Join(parts, "\n")
}
