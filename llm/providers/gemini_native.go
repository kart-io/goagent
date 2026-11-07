package providers

import (
	"context"
	"math"
	"reflect"
	"strings"

	"github.com/kart-io/goagent/codec/json"

	"google.golang.org/genai"

	shared "github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	llm "github.com/kart-io/goagent/llm"
)

var _ llm.ToolStreamClient = (*GeminiProvider)(nil)
var _ llm.EmbeddingClient = (*GeminiProvider)(nil)

func (p *GeminiProvider) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	return p.stream(ctx, req, nil)
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
			return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "gemini: missing tool responses before next message").
				WithComponent("gemini").
				WithOperation("build_request")
		}
		if msg.Role != "assistant" && (msg.ProviderState != nil || len(msg.ToolCalls) > 0) {
			return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "gemini: native state and calls require assistant role").
				WithComponent("gemini").
				WithOperation("build_request")
		}
		if msg.Role != "tool" && msg.ToolCallID != "" {
			return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "gemini: tool_call_id requires tool role").
				WithComponent("gemini").
				WithOperation("build_request")
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
					return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "gemini: missing or duplicate tool call ID").
						WithComponent("gemini").
						WithOperation("build_request")
				}
				pending[call.ID] = calls[i]
				order = append(order, call.ID)
			}
		case "tool":
			call := pending[msg.ToolCallID]
			if call == nil || responses[msg.ToolCallID] != nil || (msg.Name != "" && msg.Name != call.Name) {
				return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "gemini: unmatched or duplicate tool response").
					WithComponent("gemini").
					WithOperation("build_request")
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
			return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "gemini: unsupported message role %q", msg.Role).
				WithComponent("gemini").
				WithOperation("build_request").
				WithContext("role", msg.Role)
		}
	}
	if len(pending) > 0 {
		return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "gemini: missing tool responses").
			WithComponent("gemini").
			WithOperation("build_request")
	}
	if len(contents) == 0 || contents[len(contents)-1].Role != "user" {
		return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "gemini: last message must be a user or completed tool response").
			WithComponent("gemini").
			WithOperation("build_request")
	}
	return contents, system, nil
}

// Replaying the original parts preserves signature placement, empty signed
// parts and reasoning parts. Never rebuild or merge signed parts from text.
func geminiAssistant(msg llm.Message) (*genai.Content, []*genai.FunctionCall, error) {
	content := &genai.Content{Role: "model"}
	if state := msg.ProviderState; state != nil {
		if state.Provider != shared.ProviderGemini || json.Unmarshal(state.Data, content) != nil || content.Role != "model" {
			return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "gemini: invalid provider state").
				WithComponent("gemini").
				WithOperation("build_request")
		}
	} else {
		if msg.Content != "" {
			content.Parts = append(content.Parts, &genai.Part{Text: msg.Content})
		}
		for _, call := range msg.ToolCalls {
			// genai 构造请求时经 encoding/json 转成 map，数字总会变成 float64，这里无法保留原文。
			args, ok := call.ArgumentsObject()
			if !ok || call.Function.Name == "" {
				return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "gemini: tool arguments must be an object with a function name").
					WithComponent("gemini").
					WithOperation("build_request")
			}
			content.Parts = append(content.Parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: call.ID, Name: call.Function.Name, Args: args}})
		}
	}
	var text strings.Builder
	var calls []*genai.FunctionCall
	for _, part := range content.Parts {
		if part == nil {
			return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "gemini: nil native content part").
				WithComponent("gemini").
				WithOperation("build_request")
		}
		if !part.Thought {
			text.WriteString(part.Text)
		}
		if part.FunctionCall != nil {
			calls = append(calls, part.FunctionCall)
		}
	}
	if text.String() != msg.Content || len(calls) != len(msg.ToolCalls) {
		return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "gemini: provider state does not match assistant content").
			WithComponent("gemini").
			WithOperation("build_request")
	}
	for i, call := range calls {
		logical := msg.ToolCalls[i]
		actual := call.Args
		if actual == nil {
			actual = map[string]any{}
		}
		args, ok := logical.ArgumentsObject()
		if logical.Type != "function" || call.Name != logical.Function.Name || (call.ID != "" && call.ID != logical.ID) || !ok || !reflect.DeepEqual(args, actual) {
			return nil, nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "gemini: provider state does not match tool call").
				WithComponent("gemini").
				WithOperation("build_request")
		}
	}
	return content, calls, nil
}

// Embed uses a separate embedding model; chat model settings never select it.
func (p *GeminiProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, geminiProviderError(err)
	}
	if strings.TrimSpace(text) == "" {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "gemini: embedding text is required").
			WithComponent("gemini").
			WithOperation("embed")
	}
	ctx, cancel := context.WithTimeout(ctx, p.GetTimeout())
	defer cancel()
	response, err := geminiSDKEmbed(func() (*genai.EmbedContentResponse, error) {
		return p.client.Models.EmbedContent(ctx, p.Config.EmbeddingModel, []*genai.Content{genai.NewContentFromText(text, genai.RoleUser)}, nil)
	})
	if err != nil {
		return nil, geminiProviderError(err)
	}
	if response == nil || len(response.Embeddings) != 1 || response.Embeddings[0] == nil || len(response.Embeddings[0].Values) == 0 {
		return nil, agentErrors.Newf(agentErrors.CodeLLMResponse, "gemini: missing embedding vector").
			WithComponent("gemini").
			WithOperation("embed")
	}
	values := response.Embeddings[0].Values
	nonzero := false
	for _, value := range values {
		v := float64(value)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, agentErrors.Newf(agentErrors.CodeLLMResponse, "gemini: non-finite embedding value").
				WithComponent("gemini").
				WithOperation("embed")
		}
		nonzero = nonzero || value != 0
	}
	if !nonzero {
		return nil, agentErrors.Newf(agentErrors.CodeLLMResponse, "gemini: zero embedding vector").WithComponent("gemini").WithOperation("embed")
	}
	return values, nil
}

func geminiSDKEmbed(call func() (*genai.EmbedContentResponse, error)) (response *genai.EmbedContentResponse, err error) {
	defer func() {
		if recover() != nil {
			response, err = nil, agentErrors.Newf(agentErrors.CodeLLMResponse, "gemini: SDK failed to decode embedding response").
				WithComponent("gemini").
				WithOperation("parse_embedding_response")
		}
	}()
	return call()
}
