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
	durableInput   *string
}

// WithDurableInput 让远端工具在持久运行中以 durable 等待处理输入请求：每次远端调用记入日志，
// 远端要求输入时以 durable.AwaitInput 暂停，Continuation 的 Info 是 InputRequiredError 的 JSON；
// 宿主以 mcp.InputResponseMap 的 JSON 恢复后发送续接，已完成的远端调用回放而不重发。
// version 是持久 intent 版本（tools.InterruptibleTool），不能与 WithDurablePolicy 同用。
// 不在持久运行中时沿用 WithInputHandler 或返回 InputRequiredError。
func WithDurableInput(version string) Option {
	return func(o *options) { o.durableInput = &version }
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
	if o.inputHandler != nil && (o.maxInputRounds < 1 || o.maxInputRounds > maxInputRounds) {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "MCP 输入轮数必须在1到64之间")
	}
	if o.durableInput != nil && (*o.durableInput == "" || o.policy != nil) {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "MCP 持久输入需要版本，且不能与持久策略同用")
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
		if o.durableInput != nil {
			rt.durableInput = *o.durableInput
		}
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
	durableInput                          string
}

// maxInputRounds 限制单次调用的连续输入轮数。
const maxInputRounds = 64

// InterruptibleVersion 实现 tools.InterruptibleTool，仅在 WithDurableInput 时开启。
func (t *remoteTool) InterruptibleVersion() (string, bool) {
	return t.durableInput, t.durableInput != ""
}

var _ tools.InterruptibleTool = (*remoteTool)(nil)

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
	if t.durableInput != "" && durable.CanWait(ctx) {
		result, err := t.invokeDurable(ctx, request)
		if err != nil {
			return nil, err
		}
		return t.output(result)
	}
	result, err := t.call(ctx, request)
	if err != nil {
		return nil, err
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
	return t.output(result)
}

func (t *remoteTool) call(ctx context.Context, request *mcp.CallToolParams) (*mcp.CallToolResult, error) {
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
	return result, nil
}

// invokeDurable 把每次远端调用记为子操作，输入请求以 durable.AwaitInput 等待；恢复时重新进入，
// 已完成的调用与已收到的输入从日志回放。远端调用结果未知时（MaxAttempts 1）交由宿主处理，不盲目重发。
func (t *remoteTool) invokeDurable(ctx context.Context, request *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	call := func(step int, request *mcp.CallToolParams) (*mcp.CallToolResult, error) {
		intent, err := json.Marshal(request)
		if err != nil {
			return nil, err
		}
		record, err := durable.Do(ctx, fmt.Sprintf("mcp/call/%d", step), intent, durable.OperationPolicy{MaxAttempts: 1}, func(ctx context.Context) (json.RawMessage, error) {
			result, err := t.call(ctx, request)
			if err != nil {
				return nil, err
			}
			return json.Marshal(result)
		})
		if err != nil {
			return nil, err
		}
		var result mcp.CallToolResult
		if err = json.Unmarshal(record.Value, &result); err != nil {
			return nil, err
		}
		return &result, nil
	}
	result, err := call(0, request)
	for step := 0; err == nil && result.NeedsInput(); step++ {
		if step >= maxInputRounds {
			return nil, agentErrors.Newf(agentErrors.CodeToolExecution, "MCP tool %s 输入轮数超过 %d", t.name, maxInputRounds)
		}
		pending, e := pendingCall(request, result)
		if e != nil {
			return nil, e
		}
		info, e := json.Marshal(pending)
		if e != nil {
			return nil, e
		}
		decode := func(raw json.RawMessage) (*mcp.CallToolParams, error) {
			var responses mcp.InputResponseMap
			if err := json.Unmarshal(raw, &responses); err != nil {
				return nil, err
			}
			return pending.params(responses)
		}
		raw, e := durable.AwaitInput(ctx, fmt.Sprintf("mcp/input/%d", step), info, func(raw json.RawMessage) error {
			_, err := decode(raw)
			return err
		})
		if e != nil {
			return nil, e
		}
		if request, e = decode(raw); e != nil {
			return nil, e
		}
		result, err = call(step+1, request)
	}
	return result, err
}

func (t *remoteTool) output(result *mcp.CallToolResult) (*interfaces.ToolOutput, error) {
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
