package providers

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strings"

	"google.golang.org/genai"

	"github.com/kart-io/goagent/interfaces"
	llm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/utils/json"
)

var _ llm.ToolStreamClient = (*GeminiProvider)(nil)
var _ llm.EmbeddingClient = (*GeminiProvider)(nil)

func (p *GeminiProvider) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	return p.stream(ctx, req, nil)
}
func (p *GeminiProvider) ChatStream(ctx context.Context, messages []llm.Message) (<-chan *llm.StreamChunk, error) {
	return p.CompleteStream(ctx, &llm.CompletionRequest{Messages: messages})
}
func (p *GeminiProvider) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, tools []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	return p.stream(ctx, req, tools)
}

// geminiMessages reconstructs tool responses from the matching assistant calls.
// Parallel results form one user content, in call order, including native IDs.
func geminiMessages(messages []llm.Message, configuredSystem string) ([]*genai.Content, []string, error) {
	var contents []*genai.Content
	var system []string
	if configuredSystem != "" {
		system = append(system, configuredSystem)
	}
	pending := map[string]*genai.FunctionCall{}
	var order []string
	responses := map[string]*genai.Part{}
	for _, msg := range messages {
		if msg.Role != "tool" && len(pending) > 0 {
			return nil, nil, fmt.Errorf("gemini: missing tool responses before next message")
		}
		if msg.Role != "assistant" && (msg.ProviderState != nil || len(msg.ToolCalls) > 0) {
			return nil, nil, fmt.Errorf("gemini: native state and calls require assistant role")
		}
		if msg.Role != "tool" && msg.ToolCallID != "" {
			return nil, nil, fmt.Errorf("gemini: tool_call_id requires tool role")
		}
		switch msg.Role {
		case "system":
			system = append(system, msg.Content)
		case "user":
			contents = append(contents, &genai.Content{Role: "user", Parts: []*genai.Part{{Text: msg.Content}}})
		case "assistant":
			content, calls, err := geminiAssistant(msg)
			if err != nil {
				return nil, nil, err
			}
			contents = append(contents, content)
			order = nil
			responses = map[string]*genai.Part{}
			for i, call := range msg.ToolCalls {
				if call.ID == "" || pending[call.ID] != nil {
					return nil, nil, fmt.Errorf("gemini: missing or duplicate tool call ID")
				}
				pending[call.ID] = calls[i]
				order = append(order, call.ID)
			}
		case "tool":
			call := pending[msg.ToolCallID]
			if call == nil || responses[msg.ToolCallID] != nil || (msg.Name != "" && msg.Name != call.Name) {
				return nil, nil, fmt.Errorf("gemini: unmatched or duplicate tool response")
			}
			// Preserve the tool's exact text, even when it happens to be valid JSON.
			responses[msg.ToolCallID] = &genai.Part{FunctionResponse: &genai.FunctionResponse{ID: call.ID, Name: call.Name, Response: map[string]any{"output": msg.Content}}}
			if len(responses) == len(pending) {
				content := &genai.Content{Role: "user"}
				for _, id := range order {
					content.Parts = append(content.Parts, responses[id])
				}
				contents = append(contents, content)
				pending = map[string]*genai.FunctionCall{}
			}
		default:
			return nil, nil, fmt.Errorf("gemini: unsupported message role %q", msg.Role)
		}
	}
	if len(pending) > 0 {
		return nil, nil, fmt.Errorf("gemini: missing tool responses")
	}
	if len(contents) == 0 || contents[len(contents)-1].Role != "user" {
		return nil, nil, fmt.Errorf("gemini: last message must be a user or completed tool response")
	}
	return contents, system, nil
}

// Replaying the original parts preserves signature placement, empty signed
// parts and reasoning parts. Never rebuild or merge signed parts from text.
func geminiAssistant(msg llm.Message) (*genai.Content, []*genai.FunctionCall, error) {
	content := &genai.Content{Role: "model"}
	if state := msg.ProviderState; state != nil {
		if state.Provider != constants.ProviderGemini || json.Unmarshal(state.Data, content) != nil || content.Role != "model" {
			return nil, nil, fmt.Errorf("gemini: invalid provider state")
		}
	} else {
		if msg.Content != "" {
			content.Parts = append(content.Parts, &genai.Part{Text: msg.Content})
		}
		for _, call := range msg.ToolCalls {
			var args map[string]any
			if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || args == nil || call.Function.Name == "" {
				return nil, nil, fmt.Errorf("gemini: tool arguments must be an object with a function name")
			}
			content.Parts = append(content.Parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: call.ID, Name: call.Function.Name, Args: args}})
		}
	}
	var text strings.Builder
	var calls []*genai.FunctionCall
	for _, part := range content.Parts {
		if part == nil {
			return nil, nil, fmt.Errorf("gemini: nil native content part")
		}
		if !part.Thought {
			text.WriteString(part.Text)
		}
		if part.FunctionCall != nil {
			calls = append(calls, part.FunctionCall)
		}
	}
	if text.String() != msg.Content || len(calls) != len(msg.ToolCalls) {
		return nil, nil, fmt.Errorf("gemini: provider state does not match assistant content")
	}
	for i, call := range calls {
		logical := msg.ToolCalls[i]
		var args map[string]any
		actual := call.Args
		if actual == nil {
			actual = map[string]any{}
		}
		if logical.Type != "function" || call.Name != logical.Function.Name || (call.ID != "" && call.ID != logical.ID) || json.Unmarshal([]byte(logical.Function.Arguments), &args) != nil || args == nil || !reflect.DeepEqual(args, actual) {
			return nil, nil, fmt.Errorf("gemini: provider state does not match tool call")
		}
	}
	return content, calls, nil
}

// Embed uses a separate embedding model; chat model settings never select it.
func (p *GeminiProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("gemini: embedding text is required")
	}
	ctx, cancel := context.WithTimeout(ctx, p.GetTimeout())
	defer cancel()
	response, err := geminiSDKEmbed(func() (*genai.EmbedContentResponse, error) {
		return p.client.Models.EmbedContent(ctx, p.Config.EmbeddingModel, []*genai.Content{genai.NewContentFromText(text, genai.RoleUser)}, nil)
	})
	if err != nil {
		return nil, err
	}
	if response == nil || len(response.Embeddings) != 1 || response.Embeddings[0] == nil || len(response.Embeddings[0].Values) == 0 {
		return nil, fmt.Errorf("gemini: missing embedding vector")
	}
	values := response.Embeddings[0].Values
	vector := make([]float64, len(values))
	nonzero := false
	for i, value := range values {
		vector[i] = float64(value)
		if math.IsNaN(vector[i]) || math.IsInf(vector[i], 0) {
			return nil, fmt.Errorf("gemini: non-finite embedding value")
		}
		nonzero = nonzero || value != 0
	}
	if !nonzero {
		return nil, fmt.Errorf("gemini: zero embedding vector")
	}
	return vector, nil
}

func geminiSDKEmbed(call func() (*genai.EmbedContentResponse, error)) (response *genai.EmbedContentResponse, err error) {
	defer func() {
		if recover() != nil {
			response, err = nil, fmt.Errorf("gemini: SDK failed to decode embedding response")
		}
	}()
	return call()
}
