package providers

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/kart-io/goagent/codec/json"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	shared "github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
)

var _ agentllm.ToolStreamClient = (*AnthropicProvider)(nil)

// AnthropicProvider 基于官方 SDK 调用 Messages API。SDK 发送的 API 版本不接受采样参数，
// 因此温度与 top_p 不随请求发送；思考块经 ProviderState 原样回放。
type AnthropicProvider struct {
	*common.BaseProvider
	*common.ProviderCapabilities
	client anthropic.Client
}

// NewAnthropicWithOptions creates a new Anthropic provider using options pattern.
func NewAnthropicWithOptions(opts ...agentllm.ClientOption) (*AnthropicProvider, error) {
	base := common.NewBaseProvider(opts...)
	base.ApplyProviderDefaults(shared.ProviderAnthropic, shared.AnthropicBaseURL, shared.AnthropicDefaultModel, shared.EnvAnthropicBaseURL, shared.EnvAnthropicModel)
	if err := base.EnsureAPIKey(shared.EnvAnthropicAPIKey, shared.ProviderAnthropic); err != nil {
		return nil, err
	}
	// 关闭 SDK 隐式重试：一次调用对应一次上游请求，重试由 Builder 的 RequestError 钩子决定。
	client := anthropic.NewClient(
		option.WithAPIKey(base.Config.APIKey),
		option.WithBaseURL(base.Config.BaseURL),
		option.WithMaxRetries(0),
		option.WithHTTPClient(responseHeaderTimeoutClient(base.GetTimeout(), base.Config.CustomHeaders)),
	)
	return &AnthropicProvider{
		BaseProvider: base,
		ProviderCapabilities: common.NewProviderCapabilities(
			agentllm.CapabilityCompletion,
			agentllm.CapabilityChat,
			agentllm.CapabilityStreaming,
			agentllm.CapabilityStructuredStreaming,
			agentllm.CapabilityToolStreaming,
			agentllm.CapabilityToolCalling,
		),
		client: client,
	}, nil
}

func (p *AnthropicProvider) params(req *agentllm.CompletionRequest, tools []interfaces.Tool) (anthropic.MessageNewParams, error) {
	if req == nil || len(req.Messages) == 0 {
		return anthropic.MessageNewParams{}, agentErrors.Newf(agentErrors.CodeInvalidInput, "anthropic: messages are required").
			WithComponent("anthropic").
			WithOperation("build_request")
	}
	if err := p.RejectReasoning(req); err != nil {
		return anthropic.MessageNewParams{}, err
	}
	messages, system, err := anthropicMessages(req.Messages, p.Config.SystemPrompt)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}
	params := anthropic.MessageNewParams{
		Model:         p.GetModel(req.Model),
		MaxTokens:     int64(p.GetMaxTokens(req.MaxTokens)),
		Messages:      messages,
		StopSequences: append([]string(nil), req.Stop...),
	}
	if system != "" {
		params.System = []anthropic.TextBlockParam{{Text: system}}
	}
	for _, tool := range tools {
		params.Tools = append(params.Tools, anthropicTool(tool))
	}
	return params, nil
}

// anthropicTool 把 JSON Schema 拆成 SDK 的 properties/required，其余关键字（如 additionalProperties）原样附加。
func anthropicTool(tool interfaces.Tool) anthropic.ToolUnionParam {
	schema := map[string]any{}
	if raw := tool.ArgsSchema(); raw != "" {
		_ = json.Unmarshal([]byte(raw), &schema)
	}
	input := anthropic.ToolInputSchemaParam{Properties: schema["properties"], ExtraFields: map[string]any{}}
	if input.Properties == nil {
		input.Properties = map[string]any{}
	}
	if required, ok := schema["required"].([]any); ok {
		for _, name := range required {
			if s, ok := name.(string); ok {
				input.Required = append(input.Required, s)
			}
		}
	}
	for key, value := range schema {
		if key != "type" && key != "properties" && key != "required" {
			input.ExtraFields[key] = value
		}
	}
	param := anthropic.ToolParam{Name: tool.Name(), InputSchema: input}
	if desc := tool.Description(); desc != "" {
		param.Description = anthropic.String(desc)
	}
	return anthropic.ToolUnionParam{OfTool: &param}
}

// anthropicMessages 合并 system 消息；连续的工具结果合并为一条 user 消息，满足并行工具调用的回传要求。
func anthropicMessages(messages []agentllm.Message, configuredSystem string) ([]anthropic.MessageParam, string, error) {
	var system []string
	if configuredSystem != "" {
		system = append(system, configuredSystem)
	}
	var result []anthropic.MessageParam
	var results []anthropic.ContentBlockParamUnion
	pending := map[string]bool{}
	flush := func() error {
		if len(pending) > 0 {
			return agentErrors.Newf(agentErrors.CodeInvalidInput, "anthropic: missing tool responses before next message").
				WithComponent("anthropic").
				WithOperation("build_request")
		}
		if len(results) > 0 {
			result = append(result, anthropic.NewUserMessage(results...))
			results = nil
		}
		return nil
	}
	for _, msg := range messages {
		if msg.Role != "tool" {
			if err := flush(); err != nil {
				return nil, "", err
			}
		}
		switch msg.Role {
		case "system":
			system = append(system, msg.Content)
		case "user":
			result = append(result, anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content)))
		case "assistant":
			param, err := anthropicAssistant(msg)
			if err != nil {
				return nil, "", err
			}
			result = append(result, param)
			for _, call := range msg.ToolCalls {
				if call.ID == "" || pending[call.ID] {
					return nil, "", agentErrors.Newf(agentErrors.CodeInvalidInput, "anthropic: missing or duplicate tool call ID").
						WithComponent("anthropic").
						WithOperation("build_request")
				}
				pending[call.ID] = true
			}
		case "tool":
			if !pending[msg.ToolCallID] {
				return nil, "", agentErrors.Newf(agentErrors.CodeInvalidInput, "anthropic: unmatched or duplicate tool response").
					WithComponent("anthropic").
					WithOperation("build_request")
			}
			delete(pending, msg.ToolCallID)
			results = append(results, anthropic.NewToolResultBlock(msg.ToolCallID, msg.Content, false))
		default:
			return nil, "", agentErrors.Newf(agentErrors.CodeInvalidInput, "anthropic: unsupported message role %q", msg.Role).
				WithComponent("anthropic").
				WithOperation("build_request").
				WithContext("role", msg.Role)
		}
	}
	if err := flush(); err != nil {
		return nil, "", err
	}
	if len(result) == 0 {
		return nil, "", agentErrors.Newf(agentErrors.CodeInvalidInput, "anthropic: at least one user message is required").
			WithComponent("anthropic").
			WithOperation("build_request")
	}
	return result, strings.Join(system, "\n\n"), nil
}

// anthropicAssistant 优先回放原生内容（含思考块签名），否则由文本与工具调用重建。
func anthropicAssistant(msg agentllm.Message) (anthropic.MessageParam, error) {
	if state := msg.ProviderState; state != nil && state.Provider == shared.ProviderAnthropic {
		var param anthropic.MessageParam
		if err := json.Unmarshal(state.Data, &param); err != nil || param.OfAssistant == nil {
			return anthropic.MessageParam{}, agentErrors.Newf(agentErrors.CodeInvalidInput, "anthropic: invalid provider state").
				WithComponent("anthropic").
				WithOperation("build_request")
		}
		// SDK 自定义解码把 tool_use.input 解成含 float64 的 any；另外读取原文并覆盖，
		// 避免原生状态回放绕过无状态分支的精度保护，同时保留思考签名等原生字段。
		var raw struct {
			Content []struct {
				Input json.RawMessage `json:"input"`
			} `json:"content"`
		}
		if err := json.Unmarshal(state.Data, &raw); err != nil || len(raw.Content) != len(param.OfAssistant.Content) {
			return anthropic.MessageParam{}, agentErrors.Newf(agentErrors.CodeInvalidInput, "anthropic: invalid provider state").
				WithComponent("anthropic").
				WithOperation("build_request")
		}
		var ids []string
		for i, block := range param.OfAssistant.Content {
			if block.OfToolUse != nil {
				block.OfToolUse.Input = raw.Content[i].Input
				ids = append(ids, block.OfToolUse.ID)
			}
		}
		if len(ids) != len(msg.ToolCalls) {
			return anthropic.MessageParam{}, agentErrors.Newf(agentErrors.CodeInvalidInput, "anthropic: provider state does not match tool calls").
				WithComponent("anthropic").
				WithOperation("build_request")
		}
		for i, id := range ids {
			if id != msg.ToolCalls[i].ID {
				return anthropic.MessageParam{}, agentErrors.Newf(agentErrors.CodeInvalidInput, "anthropic: provider state does not match tool calls").
					WithComponent("anthropic").
					WithOperation("build_request")
			}
		}
		return param, nil
	}
	var blocks []anthropic.ContentBlockParamUnion
	if msg.Content != "" {
		blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
	}
	for _, call := range msg.ToolCalls {
		if _, ok := call.ArgumentsObject(); !ok || call.Function.Name == "" {
			return anthropic.MessageParam{}, agentErrors.Newf(agentErrors.CodeInvalidInput, "anthropic: tool arguments must be an object with a function name").
				WithComponent("anthropic").
				WithOperation("build_request")
		}
		// 发送参数原文：解到 map 会把大整数与高精度小数改写为 float64，SDK 又会把 json.Number 编码成字符串。
		raw, err := json.Compact([]byte(call.Function.Arguments))
		if err != nil {
			return anthropic.MessageParam{}, err
		}
		blocks = append(blocks, anthropic.NewToolUseBlock(call.ID, raw, call.Function.Name))
	}
	return anthropic.NewAssistantMessage(blocks...), nil
}

// anthropicResult 把完整消息转换为终止块；未正常结束（截断、拒答等）返回错误。
func anthropicResult(message *anthropic.Message) (*agentllm.StreamChunk, error) {
	chunk := &agentllm.StreamChunk{Done: true, ResponseModel: string(message.Model), Role: "assistant"}
	u := message.Usage
	cached := int(u.CacheReadInputTokens)
	prompt := int(u.InputTokens) + cached + int(u.CacheCreationInputTokens)
	completion := int(u.OutputTokens)
	if prompt != 0 || completion != 0 {
		chunk.Usage = sdkUsage(prompt, completion, prompt+completion, cached)
	}
	for _, block := range message.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			chunk.Content += b.Text
		case anthropic.ToolUseBlock:
			call := agentllm.ToolCall{ID: b.ID, Type: "function"}
			call.Function.Name, call.Function.Arguments = b.Name, string(b.Input)
			if call.Function.Arguments == "" {
				call.Function.Arguments = "{}"
			}
			chunk.ToolCalls = append(chunk.ToolCalls, call)
		}
	}
	switch message.StopReason {
	case anthropic.StopReasonEndTurn, anthropic.StopReasonStopSequence:
		chunk.FinishReason = "stop"
	case anthropic.StopReasonToolUse:
		chunk.FinishReason = "tool_calls"
	case anthropic.StopReasonRefusal:
		chunk.ToolCalls = nil
		return chunk, agentErrors.Newf(agentErrors.CodeLLMResponse, "anthropic: request refused (category=%q)", message.StopDetails.Category).
			WithComponent("anthropic").
			WithOperation("parse_response").
			WithContext("category", message.StopDetails.Category)
	default:
		chunk.ToolCalls = nil
		return chunk, agentErrors.Newf(agentErrors.CodeLLMResponse, "anthropic: incomplete response (stop_reason=%q)", message.StopReason).
			WithComponent("anthropic").
			WithOperation("parse_response").
			WithContext("stop_reason", message.StopReason)
	}
	data, err := json.Marshal(message.ToParam())
	if err != nil {
		return chunk, err
	}
	chunk.ProviderState = &agentllm.ProviderState{Provider: shared.ProviderAnthropic, Data: data}
	return chunk, nil
}

// anthropicProviderError 保留状态码与 Retry-After，供 ClassifyError 判断是否可重试；billing_error 视为额度耗尽。
func anthropicProviderError(err error) *agentllm.ProviderError {
	e := &agentllm.ProviderError{Err: err}
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		e.StatusCode = apiErr.StatusCode
		e.QuotaExhausted = string(apiErr.Type()) == "billing_error"
		if apiErr.Response != nil {
			e.RetryAfter = common.RetryAfterDuration(apiErr.Response.Header.Get("Retry-After"), time.Now())
		}
	}
	return e
}

func (p *AnthropicProvider) CompleteStream(ctx context.Context, req *agentllm.CompletionRequest) (<-chan *agentllm.StreamChunk, error) {
	return p.stream(ctx, req, nil)
}

func (p *AnthropicProvider) CompleteToolsStream(ctx context.Context, req *agentllm.CompletionRequest, tools []interfaces.Tool) (<-chan *agentllm.StreamChunk, error) {
	return p.stream(ctx, req, tools)
}

// stream 先读取首个事件，使连接与 HTTP 错误同步返回；工具调用只在成功的终止块中给出。
func (p *AnthropicProvider) stream(ctx context.Context, req *agentllm.CompletionRequest, tools []interfaces.Tool) (<-chan *agentllm.StreamChunk, error) {
	params, err := p.params(req, tools)
	if err != nil {
		return nil, err
	}
	watch := newStreamWatchdog(ctx, p.Config.StreamIdleTimeout, p.Config.StreamTimeout)
	wrap := func(err error) error {
		if stall := watch.stalled(); stall != nil {
			err = stall
		}
		return wrapProviderError(p.ProviderName(), params.Model, anthropicProviderError(err)).WithContext("stream", true)
	}
	stream := p.client.Messages.NewStreaming(watch.ctx, params)
	if !stream.Next() {
		err := stream.Err()
		_ = stream.Close()
		watch.stop()
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return nil, wrap(err)
	}
	out := make(chan *agentllm.StreamChunk)
	go func() {
		defer close(out)
		defer watch.stop()
		defer func() { _ = stream.Close() }()
		message := anthropic.Message{}
		var content strings.Builder
		index := 0
		send := func(chunk *agentllm.StreamChunk) bool {
			chunk.Index, chunk.Timestamp = index, time.Now()
			index++
			select {
			case out <- chunk:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for {
			watch.touch()
			event := stream.Current()
			if err := message.Accumulate(event); err != nil {
				send(&agentllm.StreamChunk{Done: true, Error: err, Content: content.String()})
				return
			}
			if delta, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
				if text, ok := delta.Delta.AsAny().(anthropic.TextDelta); ok && text.Text != "" {
					content.WriteString(text.Text)
					if !send(&agentllm.StreamChunk{Delta: text.Text, Content: content.String(), Role: "assistant"}) {
						return
					}
				}
			}
			if !stream.Next() {
				break
			}
		}
		if err := stream.Err(); err != nil {
			send(&agentllm.StreamChunk{Done: true, Error: wrap(err), Content: content.String()})
			return
		}
		terminal, err := anthropicResult(&message)
		if err != nil {
			terminal.ProviderState, terminal.Error = nil, err
		}
		send(terminal)
	}()
	return out, nil
}

// Provider returns the provider type.
func (p *AnthropicProvider) Provider() shared.Provider {
	return shared.ProviderAnthropic
}

// ProviderName returns the provider name as a string
func (p *AnthropicProvider) ProviderName() string {
	return string(shared.ProviderAnthropic)
}
