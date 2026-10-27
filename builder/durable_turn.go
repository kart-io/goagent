package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

func readJournaledTurn(ctx context.Context, round int, client llm.ToolStreamClient, req *llm.CompletionRequest, tools []interfaces.Tool, emit func(AgentEvent) error) (*llm.StreamChunk, error) {
	invoke := func(callCtx context.Context) (*llm.StreamChunk, error) {
		stream, err := client.CompleteToolsStream(callCtx, req, tools)
		if err != nil {
			return nil, err
		}
		return readToolTurn(callCtx, stream, emit)
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
	known, ids := map[string]*jsonschema.Resolved{}, map[string]bool{}
	for _, t := range tools {
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
		if err := known[call.Function.Name].Validate(args); err != nil {
			return fmt.Errorf("model returned invalid arguments for %q: %w", call.Function.Name, err)
		}
		ids[call.ID] = true
	}
	return nil
}
