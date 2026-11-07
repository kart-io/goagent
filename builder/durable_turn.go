package builder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools/toolargs"
)

// allowEmpty 为真时，正常结束的空回答按已完成记录，由调用方重问。
func readJournaledTurn(ctx context.Context, round int, client llm.Client, req *llm.CompletionRequest, tools []interfaces.Tool, allowEmpty, feedback bool, emit func(AgentEvent) error) (*llm.StreamChunk, error) {
	if err := llm.ValidateReasoningRequest(client, req); err != nil {
		return nil, err
	}

	ctx = withModelMetadata(ctx, client, req)
	ctx = withTokenRequest(ctx, req, tools)
	invoke := func(callCtx context.Context, emit func(AgentEvent) error) (*llm.StreamChunk, error) {
		var stream <-chan *llm.StreamChunk
		var err error
		if native, ok := client.(llm.ToolStreamClient); ok {
			stream, err = native.CompleteToolsStream(callCtx, req, tools)
		} else {
			stream, err = client.CompleteStream(callCtx, req)
		}
		if err != nil {
			return nil, err
		}
		return readToolTurn(callCtx, stream, emit)
	}
	if !durable.HasJournal(ctx) {
		return modelOperation(ctx, fmt.Sprintf("model/turn/%d", round), nil, invoke, nil, emit)
	}
	type definition struct{ Name, Description, Schema string }
	definitions := make([]definition, 0, len(tools))
	for _, tool := range tools {
		definitions = append(definitions, definition{tool.Name(), tool.Description(), tool.ArgsSchema()})
	}
	intent, err := json.Marshal(struct {
		Provider string
		Request  *llm.CompletionRequest
		Tools    []definition
	}{string(client.Provider()), req, definitions})
	if err != nil {
		return nil, err
	}
	return modelOperation(ctx, fmt.Sprintf("model/turn/%d", round), intent, invoke, func(turn *llm.StreamChunk) error { return validateDurableTurn(turn, tools, allowEmpty, feedback) }, emit)
}

// feedback 为真时未提供的工具与非 JSON 参数照常提交，由工具循环交还模型。
func validateDurableTurn(turn *llm.StreamChunk, tools []interfaces.Tool, allowEmpty, feedback bool) error {
	if turn.FinishReason != "stop" && turn.FinishReason != "tool_calls" {
		return agentErrors.Newf(agentErrors.CodeInvalidOutput, "incomplete model turn").WithComponent("builder").WithOperation("validate_durable_turn")
	}
	if allowEmpty && isEmptyAnswer(turn) {
		return nil
	}
	if len(turn.ToolCalls) == 0 {
		if turn.FinishReason == "tool_calls" || strings.TrimSpace(turn.Content) == "" {
			return agentErrors.Newf(agentErrors.CodeInvalidOutput, "model returned no final answer or tool calls").WithComponent("builder").WithOperation("validate_durable_turn")
		}
		return nil
	}
	known, ids, byName := map[string]*jsonschema.Resolved{}, map[string]bool{}, map[string]interfaces.Tool{}
	for _, t := range tools {
		byName[t.Name()] = t
		var schema jsonschema.Schema
		if err := json.Unmarshal([]byte(t.ArgsSchema()), &schema); err != nil {
			return agentErrors.Newf(agentErrors.CodeInvalidConfig, "invalid tool schema %q", t.Name()).WithComponent("builder").WithOperation("validate_durable_turn")
		}
		resolved, err := schema.Resolve(nil)
		if err != nil {
			return fmt.Errorf("invalid tool schema %q: %w", t.Name(), err)
		}
		known[t.Name()] = resolved
	}
	for _, call := range turn.ToolCalls {
		// 错误文本只返回给调用方，日志只记录 FailureKind。
		name := call.Function.Name
		switch {
		case call.ID == "":
			return agentErrors.Newf(agentErrors.CodeInvalidOutput, "模型返回的工具 %q 调用缺少 ID", name).WithComponent("builder").WithOperation("validate_durable_turn")
		case ids[call.ID]:
			return agentErrors.Newf(agentErrors.CodeInvalidOutput, "模型返回的工具 %q 调用 ID 重复", name).WithComponent("builder").WithOperation("validate_durable_turn")
		case known[name] == nil:
			if feedback {
				ids[call.ID] = true
				continue
			}
			return agentErrors.Newf(agentErrors.CodeInvalidOutput, "模型请求了未提供的工具 %q", name).WithComponent("builder").WithOperation("validate_durable_turn")
		}
		args, ok := toolargs.Parse(call.Function.Arguments)
		if !ok {
			if feedback {
				ids[call.ID] = true
				continue
			}
			return agentErrors.Newf(agentErrors.CodeInvalidOutput, "模型返回的工具 %q 参数不是 JSON 对象", name).WithComponent("builder").WithOperation("validate_durable_turn")
		}
		// 工具会把参数错误作为结果交还模型时，回合照常提交。
		returns, ok := byName[call.Function.Name].(interfaces.ArgumentErrorTool)
		if !ok || !returns.ReturnsArgumentErrors() {
			if err := toolargs.Validate(known[call.Function.Name], args); err != nil {
				return fmt.Errorf("model returned invalid arguments for %q: %w", call.Function.Name, err)
			}
		}
		ids[call.ID] = true
	}
	return nil
}

// scopeDurableExecution runs before middleware, retrieval and model calls so
// unsupported paths cannot silently bypass journaling or consume paid requests.
func (a *ConfigurableAgent[C, S]) scopeDurableExecution(ctx context.Context) (context.Context, error) {
	if !durable.HasJournal(ctx) {
		return ctx, nil
	}
	if len(a.tools) > 0 {
		if _, ok := a.llmClient.(llm.ToolStreamClient); !ok {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "durable tools require llm.ToolStreamClient; legacy tool selection is not journaled").WithComponent("builder").WithOperation("scope_durable_execution")
		}
		for _, tool := range a.tools {
			capability, ok := tool.(interfaces.DurableCallTool)
			if !ok || !capability.SupportsDurableCalls() {
				return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "durable tool %q requires a journaled adapter such as Dispatcher.Tools", tool.Name()).WithComponent("builder").WithOperation("scope_durable_execution")
			}
		}
	}
	scope := a.config.DurableScope
	if strings.TrimSpace(scope) == "" || strings.TrimSpace(scope) != scope {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "durable Agent requires a nonempty stable DurableScope; use WithDurableScope").WithComponent("builder").WithOperation("scope_durable_execution")
	}
	// Do not silently move an old unscoped run to new keys: that would rerun
	// completed tools under different idempotency keys.
	for _, id := range []string{"model/turn/0", "model/answer/0"} {
		if _, err := durable.InspectOperation(ctx, id); err == nil {
			return nil, agentErrors.Newf(agentErrors.CodeStateValidation, "unscoped checkpoint exists; reconcile its effects and migrate the workflow version before using DurableScope").WithComponent("builder").WithOperation("scope_durable_execution")
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	ctx = context.WithValue(ctx, hostExecutionKey{}, durable.ExecutionKey(ctx))
	return durable.WithExecutionKey(ctx, durable.ChildKey(durable.ExecutionKey(ctx), "agent/"+scope)), nil
}

// hostExecutionKey 保存宿主传入的执行键（DurableScope 派生之前），会话日志据此记录可直接用于恢复与重放的键。
type hostExecutionKey struct{}
