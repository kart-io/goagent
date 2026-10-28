package builder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/kart-io/goagent/builder/contextpolicy"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

func readJournaledTurn(ctx context.Context, round int, client llm.StreamClient, req *llm.CompletionRequest, tools []interfaces.Tool, emit func(AgentEvent) error) (*llm.StreamChunk, error) {
	invoke := func(callCtx context.Context) (*llm.StreamChunk, error) {
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
	if descriptors, ok := ctx.Value(policyDescriptorsKey{}).([]contextpolicy.Descriptor); ok {
		intent, err = json.Marshal(struct {
			Original json.RawMessage
			Policies []contextpolicy.Descriptor
		}{intent, descriptors})
		if err != nil {
			return nil, err
		}
	}
	return modelOperation(ctx, fmt.Sprintf("model/turn/%d", round), intent, invoke, func(turn *llm.StreamChunk) error { return validateDurableTurn(turn, tools) }, emit)
}

func validateDurableTurn(turn *llm.StreamChunk, tools []interfaces.Tool) error {
	if turn.FinishReason != "stop" && turn.FinishReason != "tool_calls" {
		return fmt.Errorf("incomplete model turn")
	}
	if len(turn.ToolCalls) == 0 {
		if turn.FinishReason == "tool_calls" || strings.TrimSpace(turn.Content) == "" {
			return fmt.Errorf("model returned no final answer or tool calls")
		}
		return nil
	}
	known, ids, byName := map[string]*jsonschema.Resolved{}, map[string]bool{}, map[string]interfaces.Tool{}
	for _, t := range tools {
		byName[t.Name()] = t
		var schema jsonschema.Schema
		if err := json.Unmarshal([]byte(t.ArgsSchema()), &schema); err != nil {
			return fmt.Errorf("invalid tool schema %q", t.Name())
		}
		resolved, err := schema.Resolve(nil)
		if err != nil {
			return fmt.Errorf("invalid tool schema %q: %w", t.Name(), err)
		}
		known[t.Name()] = resolved
	}
	for _, call := range turn.ToolCalls {
		var args map[string]any
		if call.ID == "" || ids[call.ID] || known[call.Function.Name] == nil || json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || args == nil {
			return fmt.Errorf("model returned invalid tool plan")
		}
		// 工具会把参数错误作为结果交还模型时，回合照常提交。
		returns, ok := byName[call.Function.Name].(interface{ ReturnsArgumentErrors() bool })
		if !ok || !returns.ReturnsArgumentErrors() {
			if err := known[call.Function.Name].Validate(args); err != nil {
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
	if a.config.ContextPolicy != nil {
		return nil, fmt.Errorf("durable execution requires a fixed context; ContextPolicy is not checkpointed")
	}
	if len(a.tools) > 0 {
		if _, ok := a.llmClient.(llm.ToolStreamClient); !ok {
			return nil, fmt.Errorf("durable tools require llm.ToolStreamClient; legacy tool selection is not journaled")
		}
		for _, tool := range a.tools {
			capability, ok := tool.(interfaces.DurableCallTool)
			if !ok || !capability.SupportsDurableCalls() {
				return nil, fmt.Errorf("durable tool %q requires a journaled adapter such as Dispatcher.Tools", tool.Name())
			}
		}
	}
	scope := a.config.DurableScope
	if strings.TrimSpace(scope) == "" || strings.TrimSpace(scope) != scope {
		return nil, fmt.Errorf("durable Agent requires a nonempty stable DurableScope; use WithDurableScope")
	}
	// Do not silently move an old unscoped run to new keys: that would rerun
	// completed tools under different idempotency keys.
	for _, id := range []string{"model/turn/0", "model/answer/0"} {
		if _, err := durable.InspectOperation(ctx, id); err == nil {
			return nil, fmt.Errorf("unscoped checkpoint exists; reconcile its effects and migrate the workflow version before using DurableScope")
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return durable.WithExecutionKey(ctx, durable.ChildKey(durable.ExecutionKey(ctx), "agent/"+scope)), nil
}
