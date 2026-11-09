// Package remote adapts official MCP client sessions to goagent tools.
package remote

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kart-io/goagent/codec/json"

	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Connect negotiates MCP over an SDK transport (StreamableClientTransport or
// CommandTransport, for example). The caller must Close the returned session.
func Connect(ctx context.Context, transport mcp.Transport) (*mcp.ClientSession, error) {
	return ConnectWithOptions(ctx, transport, nil)
}

// ConnectWithOptions 允许宿主配置旧协议的 ElicitationHandler 或采样等 SDK 处理器。
// 默认将新协议输入交给可保存的续接检查点；显式 MultiRoundTrip 选项保持不变。
func ConnectWithOptions(ctx context.Context, transport mcp.Transport, options *mcp.ClientOptions) (*mcp.ClientSession, error) {
	var opts mcp.ClientOptions
	if options != nil {
		opts = *options
	}
	// 由适配器显式处理输入，避免 SDK 在没有人工处理器时提前丢失续接结果。
	if opts.MultiRoundTrip == nil {
		opts.MultiRoundTrip = &mcp.MultiRoundTripOptions{Disabled: true}
	}
	if opts.Capabilities == nil {
		opts.Capabilities = &mcp.ClientCapabilities{Elicitation: &mcp.ElicitationCapabilities{Form: &mcp.FormElicitationCapabilities{}}}
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "goagent", Version: "1.0.0"}, &opts)
	return client.Connect(ctx, transport, nil)
}

// Option 配置 Tools 适配出的工具。
type Option func(*options)

type options struct {
	name           func(string) (string, bool)
	rejectToolErrs bool
	policy         *durable.OperationPolicy
	inputHandler   InputHandler
	maxInputRounds int
}

// WithInputHandler 由宿主收集人工输入；不自动批准、不猜测采样或根目录请求。
// 最大轮数限制连续输入与空输入请求重试；每轮仍遵守调用方 context。
func WithInputHandler(handler InputHandler, maxRounds int) Option {
	return func(o *options) { o.inputHandler = handler; o.maxInputRounds = maxRounds }
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
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "MCP session is required").
			WithComponent("mcp_remote").
			WithOperation("list_tools")
	}
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.inputHandler != nil && (o.maxInputRounds < 1 || o.maxInputRounds > 64) {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "MCP 输入轮数必须在1到64之间")
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
		rt := &remoteTool{session: session, name: name, remoteName: tool.Name, description: tool.Description, schema: string(schema), rejectToolErrs: o.rejectToolErrs, inputHandler: o.inputHandler, maxInputRounds: o.maxInputRounds}
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
	inputHandler                          InputHandler
	maxInputRounds                        int
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
		return nil, agentErrors.Newf(agentErrors.CodeToolValidation, "MCP tool input is required").
			WithComponent("mcp_remote").
			WithOperation("invoke").
			WithContext("tool_name", t.name)
	}
	request := &mcp.CallToolParams{Name: t.remoteName, Arguments: input.Args}
	result, err := t.session.CallTool(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("MCP tool %s: %w", t.name, err)
	}
	if result == nil {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidOutput, "MCP tool %s returned nil result", t.name).
			WithComponent("mcp_remote").
			WithOperation("invoke").
			WithContext("tool_name", t.name)
	}
	if result.NeedsInput() {
		pending, err := pendingCall(request, result)
		if err != nil {
			return nil, err
		}
		for round := 0; ; round++ {
			if t.inputHandler == nil || round >= t.maxInputRounds {
				return nil, pending
			}
			if err = ctx.Err(); err != nil {
				return nil, errors.Join(err, pending)
			}
			responses, e := t.inputHandler(ctx, pending)
			if e != nil {
				return nil, errors.Join(fmt.Errorf("MCP 输入处理失败: %w", e), pending)
			}
			result, e = pending.Continue(ctx, t.session, responses)
			if e != nil {
				next, ok := e.(*InputRequiredError)
				if !ok {
					return nil, errors.Join(e, pending)
				}
				pending = next
				continue
			}
			break
		}
	}
	if result.IsError {
		if t.rejectToolErrs {
			return tools.Rejected(fmt.Sprintf("MCP tool %s returned an error: %s", t.name, errorText(result)), nil), nil
		}
		return nil, agentErrors.Newf(agentErrors.CodeToolExecution, "MCP tool %s: %s", t.name, errorText(result)).
			WithComponent("mcp_remote").
			WithOperation("invoke").
			WithContext("tool_name", t.name)
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
