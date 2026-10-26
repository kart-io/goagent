// Package remote adapts official MCP client sessions to goagent tools.
package remote

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kart-io/goagent/interfaces"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Connect negotiates MCP over an SDK transport (StreamableClientTransport or
// CommandTransport, for example). The caller must Close the returned session.
func Connect(ctx context.Context, transport mcp.Transport) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "goagent", Version: "1.0.0"}, nil)
	return client.Connect(ctx, transport, nil)
}

// Tools lists all pages and adapts each remote tool. Session lifetime must exceed
// tool use. Authentication and transport limits are configured on the SDK client
// transport; remote error results become Go errors instead of successful outputs.
func Tools(ctx context.Context, session *mcp.ClientSession) ([]interfaces.Tool, error) {
	if session == nil {
		return nil, fmt.Errorf("MCP session is required")
	}
	var result []interfaces.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("MCP tool schema %s: %w", tool.Name, err)
		}
		result = append(result, &remoteTool{session: session, name: tool.Name, description: tool.Description, schema: string(schema)})
	}
	return result, nil
}

type remoteTool struct {
	session                   *mcp.ClientSession
	name, description, schema string
}

func (t *remoteTool) Name() string        { return t.name }
func (t *remoteTool) Description() string { return t.description }
func (t *remoteTool) ArgsSchema() string  { return t.schema }
func (t *remoteTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	if input == nil {
		return nil, fmt.Errorf("MCP tool input is required")
	}
	result, err := t.session.CallTool(ctx, &mcp.CallToolParams{Name: t.name, Arguments: input.Args})
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
		return nil, fmt.Errorf("MCP tool %s: %v", t.name, result.GetError())
	}
	// Keep the complete MCP result, including text, images and structured content.
	return &interfaces.ToolOutput{Result: result, Success: true}, nil
}
