package providers

import (
	"context"
	"fmt"
	"io"
	"iter"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kart-io/goagent/codec/json"

	"google.golang.org/genai"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
	"github.com/kart-io/goagent/llm/constants"
)

// GeminiProvider retains the legacy public API and uses Google's native Gemini API.
// Native structured streams retain Gemini content parts for tool continuation.
type GeminiProvider struct {
	*common.BaseProvider
	*common.ProviderCapabilities
	client *genai.Client
}

func NewGeminiWithOptions(opts ...agentllm.ClientOption) (*GeminiProvider, error) {
	base := common.NewBaseProvider(opts...)
	base.ApplyProviderDefaults(constants.ProviderGemini, "https://generativelanguage.googleapis.com", "gemini-2.5-flash", constants.EnvGeminiBaseURL, constants.EnvGeminiModel)
	if err := base.EnsureAPIKey(constants.EnvGeminiAPIKey, constants.ProviderGemini); err != nil {
		return nil, err
	}
	// Explicit backend prevents ambient Vertex settings from changing authentication.
	// No implicit SDK retries: one provider invocation is one upstream request.
	attempts := int32(1)
	timeout := base.GetTimeout()
	headers := http.Header{}
	for key, value := range base.Config.CustomHeaders {
		headers.Set(key, value)
	}
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey: base.Config.APIKey, Backend: genai.BackendGeminiAPI,
		HTTPOptions: genai.HTTPOptions{BaseURL: base.Config.BaseURL, APIVersion: "v1beta", Timeout: &timeout, RetryOptions: &genai.HTTPRetryOptions{Attempts: &attempts}, Headers: headers},
	})
	if err != nil {
		return nil, agentErrors.NewAgentInitializationError("gemini_provider", err)
	}
	if base.Config.EmbeddingModel == "" {
		base.Config.EmbeddingModel = os.Getenv(constants.EnvGeminiEmbeddingModel)
	}
	if base.Config.EmbeddingModel == "" {
		base.Config.EmbeddingModel = "gemini-embedding-001"
	}
	return &GeminiProvider{BaseProvider: base, client: client, ProviderCapabilities: common.NewProviderCapabilities(agentllm.CapabilityCompletion, agentllm.CapabilityChat, agentllm.CapabilityStreaming, agentllm.CapabilityToolCalling, agentllm.CapabilityStructuredStreaming, agentllm.CapabilityToolStreaming, agentllm.CapabilityEmbedding)}, nil
}

// request builds fresh configuration for every invocation; concurrent requests
// never mutate a shared model. System messages stay outside conversation history.
func (p *GeminiProvider) request(req *agentllm.CompletionRequest, tools []interfaces.Tool) ([]*genai.Content, *genai.GenerateContentConfig, error) {
	if req == nil || len(req.Messages) == 0 {
		return nil, nil, fmt.Errorf("gemini: messages are required")
	}
	contents, system, err := geminiMessages(req.Messages, p.Config.SystemPrompt)
	if err != nil {
		return nil, nil, err
	}
	maxTokens := min(p.GetMaxTokens(req.MaxTokens), 0x7fffffff)
	temperature, topP := float32(p.ResolveTemperature(req)), float32(p.GetTopP(req.TopP))
	thinking, err := p.thinkingConfig(req)
	if err != nil {
		return nil, nil, err
	}
	cfg := &genai.GenerateContentConfig{ThinkingConfig: thinking, MaxOutputTokens: int32(maxTokens), Temperature: &temperature, TopP: &topP, StopSequences: append([]string(nil), req.Stop...)}
	if len(system) > 0 {
		cfg.SystemInstruction = &genai.Content{Parts: []*genai.Part{{Text: strings.Join(system, "\n\n")}}}
	}
	if len(tools) > 0 {
		declarations, err := p.convertToolsToFunctions(tools)
		if err != nil {
			return nil, nil, err
		}
		cfg.Tools = []*genai.Tool{{FunctionDeclarations: declarations}}
	}
	return contents, cfg, nil
}

func geminiUsage(usage *agentllm.Usage) *interfaces.TokenUsage {
	if usage == nil {
		return nil
	}
	tokens := usage.Tokens()
	return &tokens
}

// geminiReplayableParts 去掉不含任何字段的 part（如流收尾帧的空文本）：回放时 API 要求每个 part
// 都有数据字段。只带 thoughtSignature 的 part 不为空，原样保留。
func geminiReplayableParts(parts []*genai.Part) []*genai.Part {
	kept := parts[:0:0]
	for _, part := range parts {
		// 按编码结果判断：解码 "thoughtSignature":"" 得到的空切片字段非零，但按 omitempty 仍编码为 {}。
		if part != nil {
			if data, err := json.Marshal(part); err == nil && string(data) == "{}" {
				continue
			}
		}
		kept = append(kept, part)
	}
	return kept
}

// decodeGeminiResponse does not assume candidates, content or usage are present.
// Thought parts are not exposed as answer text. FunctionCall is a field of Part
// in the Google GenAI SDK, rather than a pointer type assertion on a legacy Part.
func decodeGeminiResponse(resp *genai.GenerateContentResponse) (*agentllm.StreamChunk, error) {
	if resp == nil {
		return nil, io.ErrUnexpectedEOF
	}
	chunk := &agentllm.StreamChunk{ResponseModel: resp.ModelVersion}
	if u := resp.UsageMetadata; u != nil {
		chunk.Usage = sdkUsageDetails(sdkUsage(int(u.PromptTokenCount), int(u.CandidatesTokenCount), int(u.TotalTokenCount), int(u.CachedContentTokenCount)), int(u.ThoughtsTokenCount), int(u.ToolUsePromptTokenCount))
	}
	if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
		return chunk, fmt.Errorf("gemini: prompt blocked (%s)", resp.PromptFeedback.BlockReason)
	}
	if len(resp.Candidates) == 0 {
		return chunk, nil
	} // A stream may have a usage-only frame.
	candidate := resp.Candidates[0]
	if candidate == nil {
		return chunk, fmt.Errorf("gemini: nil candidate")
	}
	chunk.FinishReason = string(candidate.FinishReason)
	if chunk.FinishReason != "" && chunk.FinishReason != "STOP" {
		return chunk, fmt.Errorf("gemini: incomplete or blocked response (finish=%q)", chunk.FinishReason)
	}
	if chunk.FinishReason == "STOP" {
		chunk.FinishReason = "stop"
	}
	if candidate.Content == nil {
		return chunk, nil
	}
	nativeContent := *candidate.Content
	nativeContent.Role = "model"
	nativeContent.Parts = geminiReplayableParts(nativeContent.Parts)
	data, err := json.Marshal(&nativeContent)
	if err != nil {
		return chunk, err
	}
	chunk.ProviderState = &agentllm.ProviderState{Provider: constants.ProviderGemini, Data: data}
	for _, part := range candidate.Content.Parts {
		if part == nil || part.Thought {
			continue
		}
		chunk.Content += part.Text
		if f := part.FunctionCall; f != nil {
			if len(f.PartialArgs) > 0 || (f.WillContinue != nil && *f.WillContinue) {
				return chunk, fmt.Errorf("gemini: incomplete function arguments")
			}
			if f.Name == "" {
				return chunk, fmt.Errorf("gemini: function call without a name")
			}
			args := f.Args
			if args == nil {
				args = map[string]any{}
			}
			data, err := json.Marshal(args)
			if err != nil {
				return chunk, err
			}
			call := agentllm.ToolCall{ID: f.ID, Type: "function"}
			if call.ID == "" {
				call.ID = common.GenerateCallID()
			}
			call.Function.Name, call.Function.Arguments = f.Name, string(data)
			chunk.ToolCalls = append(chunk.ToolCalls, call)
		}
	}
	if chunk.FinishReason == "stop" && len(chunk.ToolCalls) > 0 {
		chunk.FinishReason = "tool_calls"
	}
	chunk.Delta = chunk.Content
	return chunk, nil
}

// stream prefetches the first response so initial HTTP failures reach legacy
// callers synchronously. Subsequent errors remain observable on typed streams.
func (p *GeminiProvider) stream(ctx context.Context, req *agentllm.CompletionRequest, tools []interfaces.Tool) (<-chan *agentllm.StreamChunk, error) {
	contents, cfg, err := p.request(req, tools)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, p.GetTimeout())
	next, stop := iter.Pull2(p.client.Models.GenerateContentStream(ctx, p.GetModel(req.Model), contents, cfg))
	first, err, ok := geminiSDKNext(next)
	if err != nil || !ok {
		stop()
		cancel()
		if err == nil {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, geminiProviderError(err)
	}
	// One terminal slot retains evidence even when the caller cancels before
	// reading the failure. Cancellation may discard a pending text delta.
	out := make(chan *agentllm.StreamChunk, 1)
	go func() {
		defer close(out)
		defer cancel()
		defer stop()
		index := 0
		send := func(c *agentllm.StreamChunk) bool {
			c.Index = index
			index++
			c.Timestamp = time.Now()
			c.Role = "assistant"
			select {
			case out <- c:
				return true
			case <-ctx.Done():
				if c.Done {
					// This goroutine is the only producer. Make room without
					// waiting for a caller that may already have stopped.
					select {
					case <-out:
					default:
					}
					out <- c
					return true
				}
				return false
			}
		}
		total := &agentllm.StreamChunk{Done: true}
		fail := func(err error) {
			total.Error = err
			total.ToolCalls = nil
			total.ProviderState = nil
			send(total)
		}
		resp := first
		nativeContent := &genai.Content{Role: "model"}
		for {
			chunk, decodeErr := decodeGeminiResponse(resp)
			if chunk != nil && chunk.ResponseModel != "" {
				total.ResponseModel = chunk.ResponseModel
			}
			if chunk != nil && chunk.Usage != nil {
				total.Usage = mergeSDKUsage(total.Usage, chunk.Usage)
			}
			if decodeErr != nil {
				fail(decodeErr)
				return
			}
			if chunk.ProviderState != nil {
				var content genai.Content
				if err := json.Unmarshal(chunk.ProviderState.Data, &content); err != nil {
					fail(err)
					return
				}
				nativeContent.Parts = append(nativeContent.Parts, geminiReplayableParts(content.Parts)...)
			}
			total.Content += chunk.Content
			if chunk.FinishReason != "" {
				total.FinishReason = chunk.FinishReason
			}
			total.ToolCalls = append(total.ToolCalls, chunk.ToolCalls...)
			if chunk.Delta != "" && !send(&agentllm.StreamChunk{Delta: chunk.Delta, Content: total.Content}) {
				fail(ctx.Err())
				return
			}
			resp, err, ok = geminiSDKNext(next)
			if err != nil {
				fail(geminiProviderError(err))
				return
			}
			if !ok {
				break
			}
		}
		if total.FinishReason != "stop" && total.FinishReason != "tool_calls" {
			total.ToolCalls = nil
			total.Error = io.ErrUnexpectedEOF
		} else {
			total.FinishReason = "stop"
			if len(total.ToolCalls) > 0 {
				total.FinishReason = "tool_calls"
			}
			data, err := json.Marshal(nativeContent)
			if err != nil {
				total.Error = err
				total.ToolCalls = nil
			} else {
				total.ProviderState = &agentllm.ProviderState{Provider: constants.ProviderGemini, Data: data}
			}
		}
		send(total)
	}()
	return out, nil
}

func geminiSDKNext(next func() (*genai.GenerateContentResponse, error, bool)) (response *genai.GenerateContentResponse, err error, ok bool) {
	defer func() {
		if recover() != nil {
			response, err, ok = nil, fmt.Errorf("gemini: SDK failed to decode stream response"), false
		}
	}()
	return next()
}

func (p *GeminiProvider) convertToolsToFunctions(tools []interfaces.Tool) ([]*genai.FunctionDeclaration, error) {
	functions := make([]*genai.FunctionDeclaration, 0, len(tools))
	for _, tool := range tools {
		if tool == nil || tool.Name() == "" {
			return nil, fmt.Errorf("gemini: tool name is required")
		}
		schema, err := p.toolSchemaToGeminiSchema(tool.ArgsSchema())
		if err != nil {
			return nil, fmt.Errorf("gemini: tool %s schema: %w", tool.Name(), err)
		}
		functions = append(functions, &genai.FunctionDeclaration{Name: tool.Name(), Description: tool.Description(), ParametersJsonSchema: schema})
	}
	return functions, nil
}

// Preserve JSON Schema constraints, including enum, required and additionalProperties.
func (p *GeminiProvider) toolSchemaToGeminiSchema(schema any) (map[string]any, error) {
	if schema == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}, nil
	}
	var data []byte
	var err error
	switch s := schema.(type) {
	case string:
		if strings.TrimSpace(s) == "" {
			return map[string]any{"type": "object", "properties": map[string]any{}}, nil
		}
		data = []byte(s)
	case []byte:
		data = s
	case json.RawMessage:
		data = s
	default:
		data, err = json.Marshal(s)
	}
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if result == nil || result["type"] != "object" {
		return nil, fmt.Errorf("parameters must be an object schema")
	}
	return result, nil
}

func (p *GeminiProvider) Provider() constants.Provider { return constants.ProviderGemini }
func (p *GeminiProvider) ProviderName() string         { return string(constants.ProviderGemini) }
func (p *GeminiProvider) ModelName() string            { return p.GetModel("") }
func (p *GeminiProvider) MaxTokens() int               { return p.GetMaxTokens(0) }

type GeminiStreamingProvider struct{ *GeminiProvider }

func NewGeminiStreamingWithOptions(opts ...agentllm.ClientOption) (*GeminiStreamingProvider, error) {
	p, err := NewGeminiWithOptions(opts...)
	if err != nil {
		return nil, err
	}
	return &GeminiStreamingProvider{p}, nil
}
func NewGeminiStreaming(config *agentllm.LLMOptions) (*GeminiStreamingProvider, error) {
	return NewGeminiStreamingWithOptions(common.ConfigToOptions(config)...)
}
func (p *GeminiStreamingProvider) StreamWithContext(ctx context.Context, prompt string) (<-chan StreamEvent, error) {
	chunks, err := p.stream(ctx, &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage(prompt)}}, nil)
	if err != nil {
		return nil, err
	}
	out := make(chan StreamEvent)
	go func() {
		defer close(out)
		send := func(e StreamEvent) bool {
			e.Timestamp = time.Now()
			select {
			case out <- e:
				return true
			case <-ctx.Done():
				return false
			}
		}
		if !send(StreamEvent{Type: "start"}) {
			return
		}
		for c := range chunks {
			if c.Error != nil {
				send(StreamEvent{Type: "error", Error: c.Error})
				return
			}
			if c.Delta != "" && !send(StreamEvent{Type: "token", Content: c.Delta}) {
				return
			}
			if c.Done {
				meta := map[string]any{}
				if c.Usage != nil {
					meta["total_tokens"] = c.Usage.TotalTokens
					meta["usage"] = geminiUsage(c.Usage)
				}
				send(StreamEvent{Type: "complete", Metadata: meta})
				return
			}
		}
	}()
	return out, nil
}

type StreamEvent struct {
	Type      string
	Content   string
	Error     error
	Timestamp time.Time
	Metadata  map[string]interface{}
}
